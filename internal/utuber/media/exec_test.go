package media

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeExec struct {
	lines []string
}

func (f fakeExec) Run(
	ctx context.Context,
	name string,
	args []string,
	onStdout, onStderr func(string),
) error {
	for _, l := range f.lines {
		onStdout(l)
	}
	return nil
}

func TestProgressCapture(t *testing.T) {
	exec := fakeExec{lines: []string{"10%", "42%", "100%"}}

	var seen []string
	exec.Run(context.Background(), "x", nil, func(s string) {
		seen = append(seen, s)
	}, nil)

	if len(seen) != 3 {
		t.Fatal("expected progress lines")
	}
}

// TestOSExecutorRunJoinsOutput proves the P15 rewrite delivers every line
// synchronously: all 500 lines are present the instant Run returns, because
// cmd.Run (with io.Writer Stdout/Stderr) only returns after output copying
// finishes — there are no scanner goroutines left running.
func TestOSExecutorRunJoinsOutput(t *testing.T) {
	var mu sync.Mutex
	var lines []string
	err := OSExecutor{}.Run(context.Background(), "sh",
		[]string{"-c", "i=1; while [ $i -le 500 ]; do echo line-$i; i=$((i+1)); done"},
		func(s string) {
			mu.Lock()
			lines = append(lines, s)
			mu.Unlock()
		}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	n := len(lines)
	mu.Unlock()
	if n != 500 {
		t.Fatalf("got %d lines immediately after Run returned, want 500 (synchronous join)", n)
	}
}

// TestOSExecutorRunSeparatesStreams proves the two-callback split routes each
// stream to its own callback (and, like TestOSExecutorRunJoinsOutput, that
// every line is delivered synchronously by the time Run returns): stdout lines
// land only in onStdout in order, stderr lines only in onStderr.
func TestOSExecutorRunSeparatesStreams(t *testing.T) {
	var mu sync.Mutex
	var out, errs []string
	err := OSExecutor{}.Run(context.Background(), "sh",
		[]string{"-c", "echo out1; echo err1 >&2; echo out2"},
		func(s string) {
			mu.Lock()
			out = append(out, s)
			mu.Unlock()
		},
		func(s string) {
			mu.Lock()
			errs = append(errs, s)
			mu.Unlock()
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	wantOut := []string{"out1", "out2"}
	if len(out) != len(wantOut) {
		t.Fatalf("stdout lines = %v, want %v", out, wantOut)
	}
	for i := range wantOut {
		if out[i] != wantOut[i] {
			t.Fatalf("stdout lines = %v, want %v", out, wantOut)
		}
	}
	wantErr := []string{"err1"}
	if len(errs) != len(wantErr) || errs[0] != wantErr[0] {
		t.Fatalf("stderr lines = %v, want %v", errs, wantErr)
	}
}

// TestOSExecutorCancelKillsGroup proves that canceling the context kills the
// whole process group (Setpgid + group SIGKILL) and that WaitDelay caps how
// long Run blocks afterward: a nested sleep tree must not keep Run alive.
func TestOSExecutorCancelKillsGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- OSExecutor{}.Run(ctx, "sh",
			[]string{"-c", "sleep 30 & sleep 30; wait"}, func(string) {}, func(string) {})
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Returned promptly after the group kill.
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s after cancel — group SIGKILL/WaitDelay failed")
	}
}
