package utuber

import (
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"go.uber.org/goleak"
)

// TestMain speeds the shared worker's poll to 50ms (R8) so lane jobs run
// promptly under -race, and runs goleak so any orphaned goroutine — a worker
// or pruner left running because an engine was not Closed, or a yt-dlp
// grandchild holding a pipe (P15) — fails the package instead of leaking.
func TestMain(m *testing.M) {
	worker.SetPollIntervalForTest(50 * time.Millisecond)
	goleak.VerifyTestMain(m)
}
