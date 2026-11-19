package verify

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestStartFailureCodes: only a command that could not run at all counts as a
// start failure. 126 and 127 are the shell's "not executable" and "not found";
// 125 is the docker CLI refusing the run. An ordinary non-zero exit is a
// result.
func TestStartFailureCodes(t *testing.T) {
	for _, c := range []struct {
		code   int
		docker bool
		want   bool
	}{
		{0, false, false}, {1, false, false}, {126, false, true}, {127, false, true},
		{125, false, false}, {125, true, true},
	} {
		if got := startFailure(c.code, c.docker); got != c.want {
			t.Errorf("startFailure(%d, %v) = %v, want %v", c.code, c.docker, got, c.want)
		}
	}
}

// TestCommandThatCannotStartIsAnError: a feedback command that exits 127 never
// produces a result, so it cannot enter the baseline and pass silently later.
func TestCommandThatCannotStartIsAnError(t *testing.T) {
	f := newFB(t)
	cfg := f.worktree(t, 60*time.Second)
	results, err := RunFeedback(context.Background(), cfg, []Argv{{"sh", "-c", "exit 127"}})
	if err == nil {
		t.Fatalf("a command that could not start was a result: %+v", results)
	}
	if !strings.Contains(err.Error(), "could not start") {
		t.Fatalf("error = %v", err)
	}
}
