package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Call is one recorded Fake invocation.
type Call struct {
	Request Request
	// Prompt is the content of Request.PromptPath read when the call began.
	Prompt string
	// PromptErr is the read error, if the prompt file could not be read.
	PromptErr error
}

// Step scripts the outcome of one Fake call. The fake never executes a shell.
type Step struct {
	Reply      string
	Events     []Event
	Usage      *Usage
	SessionID  string
	Incomplete bool
	// ErrClass, when not ErrNone, makes the call fail with *Error of that class.
	ErrClass ErrClass
	// Err, when set and ErrClass is ErrNone, is returned as is.
	Err error
	// Delay waits before answering; ctx cancellation ends it early.
	Delay time.Duration
	// Block waits until ctx is done and then fails with the context error.
	Block bool
	// Panic, when non-nil, is the value the call panics with.
	Panic any
	// Edit runs in place of the agent to simulate edits in the workdir.
	Edit func(Request) error
	// Match, when set, restricts the step to requests it accepts. Within a
	// role's queue a call consumes the first matching step that has a Match;
	// only if none matches does it consume the first step without a Match.
	// Steps whose Match rejects the request stay queued.
	Match func(Request) bool
	// Partial makes Reply and Events visible before Delay or Block: they are
	// emitted through OnEvent first and returned in the Result when ctx ends.
	Partial bool
	// EditBeforeBlock runs before Delay or Block, so a timed-out worker can
	// leave a dirty workdir. Its failure is reported as ErrCrash.
	EditBeforeBlock func(Request) error
}

// Fake is a scriptable, recording Runner, safe for concurrent use.
type Fake struct {
	mu       sync.Mutex
	scripts  map[string][]Step
	fallback *Step
	calls    []Call
	version  string
}

var _ Runner = (*Fake)(nil)

// NewFake returns a Fake with no script; unscripted calls fail with ErrCrash.
func NewFake() *Fake {
	return &Fake{scripts: map[string][]Step{}, version: "fake"}
}

// Script queues steps for a role; each call for that role consumes one.
func (f *Fake) Script(role string, steps ...Step) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[role] = append(f.scripts[role], steps...)
	return f
}

// Fallback sets the step used when a role's queue is empty.
func (f *Fake) Fallback(s Step) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fallback = &s
	return f
}

// SetVersion sets what Version reports.
func (f *Fake) SetVersion(v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version = v
}

// Calls returns a copy of the recorded calls in arrival order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	for i, c := range f.calls {
		c.Request.Env = append([]string(nil), c.Request.Env...)
		out[i] = c
	}
	return out
}

// CallsFor returns the recorded calls of one role.
func (f *Fake) CallsFor(role string) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if c.Request.Role == role {
			out = append(out, c)
		}
	}
	return out
}

// Version implements Runner.
func (f *Fake) Version(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version, nil
}

// Run implements Runner.
func (f *Fake) Run(ctx context.Context, req Request) (Result, error) {
	call := Call{Request: req}
	call.Request.Env = append([]string(nil), req.Env...)
	if req.PromptPath != "" {
		b, err := os.ReadFile(req.PromptPath)
		call.Prompt, call.PromptErr = string(b), err
	}

	f.mu.Lock()
	f.calls = append(f.calls, call)
	var step Step
	if i := pick(f.scripts[req.Role], req); i >= 0 {
		q := f.scripts[req.Role]
		step = q[i]
		f.scripts[req.Role] = append(append([]Step(nil), q[:i]...), q[i+1:]...)
	} else if f.fallback != nil {
		step = *f.fallback
	} else {
		step = Step{ErrClass: ErrCrash, Err: fmt.Errorf("fake runner: no script for role %q", req.Role)}
	}
	f.mu.Unlock()

	if step.Panic != nil {
		panic(step.Panic)
	}
	if step.EditBeforeBlock != nil {
		if err := step.EditBeforeBlock(req); err != nil {
			return Result{ErrClass: ErrCrash}, &Error{Class: ErrCrash, Msg: err.Error(), Cause: err}
		}
	}
	events := append([]Event(nil), step.Events...)
	var partial Result
	if step.Partial {
		emit(req, events)
		partial = Result{Final: step.Reply, Events: events}
	}
	if step.Block {
		<-ctx.Done()
		return ctxResult(ctx, partial)
	}
	if step.Delay > 0 {
		t := time.NewTimer(step.Delay)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctxResult(ctx, partial)
		}
	}
	if step.Edit != nil {
		if err := step.Edit(req); err != nil {
			return Result{ErrClass: ErrCrash}, &Error{Class: ErrCrash, Msg: err.Error(), Cause: err}
		}
	}
	if !step.Partial {
		emit(req, events)
	}
	res := Result{
		Final:      step.Reply,
		Events:     events,
		Usage:      step.Usage,
		SessionID:  step.SessionID,
		Incomplete: step.Incomplete,
		ErrClass:   step.ErrClass,
	}
	switch {
	case step.ErrClass != ErrNone:
		msg := ""
		if step.Err != nil {
			msg = step.Err.Error()
		}
		return res, &Error{Class: step.ErrClass, Msg: msg, Cause: step.Err}
	case step.Err != nil:
		return res, step.Err
	}
	return res, nil
}

func emit(req Request, events []Event) {
	if req.OnEvent == nil {
		return
	}
	for _, ev := range events {
		req.OnEvent(ev)
	}
}

// pick returns the index of the step a request consumes, or -1.
func pick(q []Step, req Request) int {
	plain := -1
	for i, s := range q {
		if s.Match == nil {
			if plain < 0 {
				plain = i
			}
		} else if s.Match(req) {
			return i
		}
	}
	return plain
}

// ctxResult maps a finished context to a Result and error: a deadline is a
// timeout; a plain cancel carries no class. partial output is kept.
func ctxResult(ctx context.Context, partial Result) (Result, error) {
	err := ctx.Err()
	if errors.Is(err, context.DeadlineExceeded) {
		partial.ErrClass = ErrTimeout
		return partial, &Error{Class: ErrTimeout, Msg: err.Error(), Cause: err}
	}
	return partial, err
}
