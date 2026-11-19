// Package runner defines the runner-agnostic AgentRunner interface through
// which every agentflow model call goes (AR1, MB1). Nothing here knows about a
// concrete agent program; adapters live in subpackages.
package runner

import (
	"context"
	"fmt"
	"time"
)

// ErrClass classifies why a run failed. The engine maps these to infra versus
// attempt failures; the adapter decides which class a raw failure belongs to.
type ErrClass string

const (
	ErrNone        ErrClass = ""
	ErrRateLimit   ErrClass = "rate_limit"
	ErrUnreachable ErrClass = "unreachable"
	ErrCrash       ErrClass = "crash"
	ErrTimeout     ErrClass = "timeout"
	// ErrContext: the model ran out of context window mid-task (finish reason
	// "length"). The host's setting is at fault, not the model.
	ErrContext ErrClass = "context_full"
	// ErrDetached: the call stopped waiting but the work goes on outside this
	// process (a seat in a terminal session that outlives a restart). The engine
	// writes no outcome; the stage is adopted again when the process is back.
	ErrDetached ErrClass = "detached"
)

// Valid reports whether c is one of the defined classes.
func (c ErrClass) Valid() bool {
	switch c {
	case ErrNone, ErrRateLimit, ErrUnreachable, ErrCrash, ErrTimeout, ErrContext, ErrDetached:
		return true
	}
	return false
}

// Backend is a model host in agentflow terms (AR3). Adapters translate it into
// whatever their agent program needs.
type Backend struct {
	Name    string
	Host    string
	APIType string
	// Slots is how many stages the backend runs at once; for a seat it is how many
	// open tasks the seat may hold.
	Slots int
}

// Request is one model call.
type Request struct {
	Role    string
	Backend Backend
	Model   string
	// Workdir is the directory the agent works in (the task worktree).
	Workdir string
	// StageDir is the per-stage directory holding prompt.md, the frozen runner
	// config and the agent's private home.
	StageDir string
	// PromptPath is the prompt file the engine wrote before the call. The prompt
	// can exceed 500 KB, so it is a file and never an argument or a field.
	PromptPath string
	// Message is a short instruction accompanying the prompt. Adapters that pipe
	// the prompt on stdin ignore it.
	Message string
	// SessionID, when set, continues an earlier session of the same adapter for
	// the corrective follow-up (SO2); PromptPath then holds the new prompt.
	SessionID string
	// Plugins are the project's pinned, opted-in runner plugins (OA3); empty
	// for a project that has opted into none, which is the default.
	Plugins []Plugin
	Env     []string
	// Job names the engine job this call serves, for runners that route work through a
	// message service; zero for calls that belong to no stage (a model check).
	Job Job
	// Budget is the role's time budget for runners that keep their own clock (seats):
	// the engine then sets no deadline on the call, and a seat silent past the budget
	// is reported as needing attention instead of failing. Zero means no budget.
	Budget time.Duration
	// OnEvent, when set, receives events as they arrive so the engine can stream
	// them to the job log. Result.Events still holds the full list.
	OnEvent func(Event)
}

// Job identifies the stage a model call belongs to.
type Job struct {
	// Project is the project slug, empty for calls outside a project.
	Project string
	Effort  int64
	Stage   int64
	Task    string // the task's key, empty for the planning roles
	Attempt int64
}

// Event is one runner-neutral entry of the event stream.
type Event struct {
	Type string
	Text string
	Raw  []byte
}

// Usage is token accounting for a run.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
}

// Result is the outcome of a call. When Run returns a non-nil error, Final and
// Events may still hold partial output. Result.ErrClass mirrors the class of
// the returned *Error, which is the source of truth.
//
// Timeouts: the engine derives the role timeout in its own context and checks
// the parent context separately. Parent cancel is infra; a role deadline is a
// timeout (attempt failure).
type Result struct {
	Final  string
	Events []Event
	Usage  *Usage
	// SessionID identifies the session for a later corrective follow-up.
	SessionID string
	// Incomplete is true when the run ended without a final answer although
	// nothing failed (for example a rejected permission prompt).
	Incomplete bool
	ErrClass   ErrClass
}

// Runner executes model calls.
type Runner interface {
	Run(ctx context.Context, req Request) (Result, error)
	// Version reports the underlying agent program's version.
	Version(ctx context.Context) (string, error)
}

// Error is the error a Runner returns for a classified failure.
type Error struct {
	Class ErrClass
	Msg   string
	Cause error
	// Tokens is the context size at which an ErrContext run was cut off: the
	// window the host served the model with.
	Tokens int64
}

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.Cause }

func (e *Error) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("runner: %s", e.Class)
	}
	return fmt.Sprintf("runner: %s: %s", e.Class, e.Msg)
}
