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
	Run(ctx context.Context, name string, args []string, onLine func(string)) error
}

type OSExecutor struct{}

// lineWriter is an io.Writer that splits the bytes written to it into complete
// lines and calls onLine synchronously (under its own lock) for each one. A
// single lineWriter is shared by a command's stdout and stderr, so onLine is
// serialized across both streams and never runs concurrently — the callback
// needs no locking of its own, and every line it emits has been delivered by
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

func (OSExecutor) Run(ctx context.Context, name string, args []string, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	cmd := exec.CommandContext(ctx, name, args...)

	// One shared lineWriter for both streams: stdout and stderr merge through a
	// single serialized onLine. Because Stdout/Stderr are io.Writers (not
	// pipes), cmd.Wait only returns after all output has been copied into the
	// writer, so there are no scanner goroutines to join (P15).
	lw := &lineWriter{onLine: onLine}
	cmd.Stdout = lw
	cmd.Stderr = lw

	// Put the child in its own process group and, on context cancel, SIGKILL
	// the whole group so yt-dlp's ffmpeg grandchildren die too. WaitDelay caps
	// how long Wait blocks for output copying after the kill (P15).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 10 * time.Second

	err := cmd.Run()
	lw.flush()
	return err
}
