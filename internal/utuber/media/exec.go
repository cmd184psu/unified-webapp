package media

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type Executor interface {
	Run(ctx context.Context, name string, args []string, onStdout, onStderr func(string)) error
}

type OSExecutor struct{}

// lineWriter is an io.Writer that splits the bytes written to it into complete
// lines and calls onLine synchronously (under its own lock) for each one. Run
// creates two independent lineWriters, one per stream (stdout and stderr), so
// each stream's onLine is serialized within that stream and the callback needs
// no locking of its own. Every line each writer emits has been delivered by
// the time Run returns (P15: no scanner goroutines to join). flush emits any
// trailing partial line after the process has exited.
type lineWriter struct {
	mu     sync.Mutex
	buf    []byte
	onLine func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(w.buf[:i])
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(w.buf)
		w.buf = nil
	}
}

// emit delivers one line, trimming a trailing '\r' so CRLF output yields clean
// lines (matching the old bufio.Scanner behavior). The caller holds w.mu.
func (w *lineWriter) emit(line []byte) {
	w.onLine(string(bytes.TrimSuffix(line, []byte("\r"))))
}

func (OSExecutor) Run(ctx context.Context, name string, args []string, onStdout, onStderr func(string)) error {
	if onStdout == nil {
		onStdout = func(string) {}
	}
	if onStderr == nil {
		onStderr = func(string) {}
	}
	cmd := exec.CommandContext(ctx, name, args...)

	// Two independent lineWriters, one per stream: stdout and stderr are
	// captured separately, each serialized within its own stream. Because
	// Stdout/Stderr are io.Writers (not pipes), cmd.Run internally starts one
	// copy-goroutine per distinct writer and joins ALL of them before it
	// returns, so all output has been copied into the writers by the time Run
	// returns — there are no scanner goroutines of our own to join (P15). Two
	// writers instead of one does not reintroduce the pre-P15 goroutine leak:
	// that bug was manual StdoutPipe() + unjoined scanner goroutines, which
	// this still avoids.
	stdoutLW := &lineWriter{onLine: onStdout}
	stderrLW := &lineWriter{onLine: onStderr}
	cmd.Stdout = stdoutLW
	cmd.Stderr = stderrLW

	// Put the child in its own process group and, on context cancel, SIGKILL
	// the whole group so yt-dlp's ffmpeg grandchildren die too. WaitDelay caps
	// how long Wait blocks for output copying after the kill (P15).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second

	err := cmd.Run()
	stdoutLW.flush()
	stderrLW.flush()
	return err
}
