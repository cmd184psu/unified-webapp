package worker

import (
	"context"
	"strings"
	"testing"
	"time"
)

// These tests are white-box (package worker) because they assert on
// buildCmd's unexported behavior directly, per the plan's instruction to
// verify sudo-wrapping "on buildCmd output, don't actually run sudo".

func TestBuildCmd_SudoDeniedWhenNotAllowed(t *testing.T) {
	te := &TaskExecutor{AllowSudo: false}
	_, err := te.buildCmd(context.Background(), true, "echo", "hi")
	if err == nil {
		t.Fatal("expected error when sudo is requested and AllowSudo is false")
	}
	if !strings.Contains(err.Error(), "allow_sudo") {
		t.Fatalf("expected error to mention allow_sudo, got: %v", err)
	}
}

func TestBuildCmd_SudoPrependedWhenAllowed(t *testing.T) {
	te := &TaskExecutor{AllowSudo: true}
	cmd, err := te.buildCmd(context.Background(), true, "echo", "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(cmd.Path, "sudo") {
		t.Fatalf("expected command path to resolve to sudo, got %q", cmd.Path)
	}
	if len(cmd.Args) < 3 || cmd.Args[1] != "echo" || cmd.Args[2] != "hi" {
		t.Fatalf("expected sudo-wrapped args [sudo echo hi], got %v", cmd.Args)
	}
	if cmd.WaitDelay != waitDelay {
		t.Fatalf("expected WaitDelay %v, got %v", waitDelay, cmd.WaitDelay)
	}
}

func TestBuildCmd_NoSudoPassthrough(t *testing.T) {
	te := &TaskExecutor{}
	cmd, err := te.buildCmd(context.Background(), false, "echo", "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.HasSuffix(cmd.Path, "sudo") {
		t.Fatalf("did not expect sudo wrapping, got path %q", cmd.Path)
	}
	if cmd.WaitDelay != waitDelay {
		t.Fatalf("expected WaitDelay %v, got %v", waitDelay, cmd.WaitDelay)
	}
}

// TestOutputRegistry_GC_FinishedAtExpiry is white-box because it backdates
// FinishedAt and calls gc directly rather than waiting on StartGC's 5-minute
// ticker.
func TestOutputRegistry_GC_FinishedAtExpiry(t *testing.T) {
	r := NewRegistry()

	stdoutDone, stderrDone := r.Register(1)
	stdoutDone.MarkDone()
	stderrDone.MarkDone()
	r.MarkFinished(1)
	r.mu.Lock()
	r.captures[1].FinishedAt = time.Now().Add(-2 * time.Hour)
	r.mu.Unlock()

	r.Register(2) // still running: zero FinishedAt

	r.gc(time.Hour)

	if _, _, ok := r.Get(1); ok {
		t.Fatal("expected finished capture past TTL to be reaped by gc")
	}
	if _, _, ok := r.Get(2); !ok {
		t.Fatal("expected still-running capture (zero FinishedAt) to survive gc")
	}
}
