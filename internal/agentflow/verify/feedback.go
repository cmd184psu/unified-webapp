package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
)

// DefaultTimeout bounds one feedback command when FeedbackConfig.Timeout is
// unset. The engine passes the verify_timeout limit.
const DefaultTimeout = 10 * time.Minute

// maxOutputBytes bounds the output kept per command (head and tail halves). The
// rest is drained and counted so a chatty command can never exhaust memory or block on a pipe.
const maxOutputBytes = 256 << 10

// Argv is one feedback command as a fixed argument vector. It is never joined
// into a string and never reaches a shell.
type Argv []string

func (a Argv) String() string { return strings.Join(a, " ") }

// ParseArgv turns a stored project feedback command into an Argv by splitting
// on whitespace. There is no quoting and no expansion: what is stored is what
// runs, so an operator needing a quoted argument wraps it in a script.
func ParseArgv(s string) (Argv, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return nil, errors.New("verify: empty feedback command")
	}
	return Argv(f), nil
}

// FeedbackConfig describes where and how feedback commands run. Spec carries
// the worktree, trusted git dirs, stage home and any Go module cache binds or
// environment (for example GOPROXY=off); its Argv is replaced per command and
// the worktree is bound read-only unless WritableWorktree is set, because a
// feedback command has no business writing to the tree it verifies. Caches go
// to Spec.StageHome.
type FeedbackConfig struct {
	Spec sandbox.Spec
	// WritableWorktree binds the worktree read-write. Only verify_in_copy sets
	// it: the commands then run in a throwaway copy, because a toolchain such
	// as .NET writes build output next to the sources (AF-6).
	WritableWorktree bool
	Timeout          time.Duration
}

// CommandResult is the outcome of one feedback command. Output is the first
// maxOutputBytes of the combined stdout and stderr.
type CommandResult struct {
	Argv      Argv      `json:"argv"`
	ExitCode  int       `json:"exit_code"`
	TimedOut  bool      `json:"timed_out"`
	Output    string    `json:"output"`
	Truncated bool      `json:"truncated"`
	Failures  []Failure `json:"failures"`
}

// Failed reports whether the command did not succeed.
func (r CommandResult) Failed() bool { return r.TimedOut || r.ExitCode != 0 }

// RunFeedback runs every command in the sandbox concurrently, each under its
// own timeout, and returns the results in input order with failure signatures
// filled. A command that exits non-zero is a result, not an error; an error
// means a command could not be run at all or ctx was cancelled.
func RunFeedback(ctx context.Context, cfg FeedbackConfig, cmds []Argv) ([]CommandResult, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	module := modulePath(cfg.Spec.Worktree)
	results := make([]CommandResult, len(cmds))
	errs := make([]error, len(cmds))
	var wg sync.WaitGroup
	for i, argv := range cmds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = runOne(ctx, cfg.Spec, argv, timeout, cfg.WritableWorktree)
			if errs[i] == nil {
				results[i].Failures = fingerprint(results[i], module)
			}
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("verify: feedback command %q: %w", cmds[i], err)
		}
	}
	return results, nil
}

func runOne(ctx context.Context, spec sandbox.Spec, argv Argv, timeout time.Duration, writable bool) (CommandResult, error) {
	res := CommandResult{Argv: argv}
	if len(argv) == 0 {
		return res, errors.New("empty command")
	}
	spec.Argv = argv
	spec.ReadOnlyWorktree = !writable
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd, err := sandbox.Command(cctx, spec)
	if err != nil {
		return res, err
	}
	out := &limitedBuffer{max: maxOutputBytes}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	res.Output, res.Truncated = out.String(), out.truncated
	var exit *exec.ExitError
	switch {
	case err == nil:
	case cctx.Err() != nil && ctx.Err() == nil:
		res.TimedOut = true
		res.ExitCode = -1
	case errors.As(err, &exit):
		res.ExitCode = exit.ExitCode()
	default:
		return res, err
	}
	return res, nil
}

// limitedBuffer keeps the first and last max/2 bytes written and counts the
// rest. go test prints its FAIL summary at the end, so the tail matters as
// much as the head.
type limitedBuffer struct {
	mu        sync.Mutex
	head      []byte
	tail      []byte
	max       int
	dropped   int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	half := b.max / 2
	if room := half - len(b.head); room > 0 {
		k := min(room, len(p))
		b.head = append(b.head, p[:k]...)
		p = p[k:]
	}
	if len(p) > 0 {
		b.tail = append(b.tail, p...)
		if over := len(b.tail) - half; over > 0 {
			b.dropped += over
			b.truncated = true
			b.tail = append(b.tail[:0], b.tail[over:]...)
		}
	}
	return n, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.dropped == 0 {
		return string(b.head) + string(b.tail)
	}
	return fmt.Sprintf("%s\n[... %d bytes elided ...]\n%s", b.head, b.dropped, b.tail)
}

// modulePath returns the module path declared by root/go.mod, or "".
func modulePath(root string) string {
	b, err := readRegular(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`)
		}
	}
	return ""
}

// readRegular reads a small regular file.
func readRegular(p string) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > 1<<20 {
		return nil, errors.New("not a small regular file")
	}
	return os.ReadFile(p)
}
