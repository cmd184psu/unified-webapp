package worker

import (
	"cmd184psu/unified-webapp/internal/taskmaster/db"
)

// ReconcileOrphans checks every execution still recorded "running" — called
// once at boot (build.go), and reusable on demand for a manual sweep — and
// closes out any whose process is confirmed gone (or whose PID was never
// persisted, an accepted case with no way to check) as "failed", releasing
// its lock so the task is immediately re-schedulable. A genuinely-still-
// alive process (see ProcessAlive — a real process is isolated into its own
// process group specifically so it survives its parent dying) is left
// completely untouched: same row, same lock, exactly as if no restart had
// happened. Lives here (not in package db, which stays OS-agnostic) because
// liveness checking is inherently OS-level.
func ReconcileOrphans(database *db.DB) (skipped, failed int, err error) {
	candidates, err := database.ListOrphanCandidates()
	if err != nil {
		return 0, 0, err
	}
	for _, c := range candidates {
		if c.PID != nil && ProcessAlive(*c.PID) {
			skipped++
			continue
		}
		if err := database.MarkOrphanFailed(c.ExecID, c.TaskID); err != nil {
			return skipped, failed, err
		}
		failed++
	}
	return skipped, failed, nil
}
