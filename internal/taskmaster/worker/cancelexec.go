package worker

import (
	"log"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// CancelExecution is the single shared cancel protocol used by both the
// coordinator's HTTP cancel handler and golane.Lane.Cancel (plan §4.4). It is
// DB-first specifically to avoid a race with runTask, which registers its
// cancel func (CancelRegistry.Register) before claiming the execution: a
// registry-first protocol would report "canceling" for a still-pending row
// and leave it pending forever, with a canceled context but a Run that never
// happens. With this ordering every interleaving ends in exactly one
// terminal state (see the three cases documented in the plan):
//
//	(a) db.CancelPendingFuncExecution flips a still-pending row straight to
//	    canceled (with its metric) — the eventual claim then affects 0 rows
//	    and runFunc bails without Run.
//	(b) otherwise, if the execution is registered as running, cancels.Cancel
//	    cancels its context — runFunc's post-claim check (or Run itself)
//	    finishes it canceled.
//	(c) otherwise the execution is already finished: "not_running".
func CancelExecution(d *db.DB, c *CancelRegistry, b *broker.Broker, h *HiddenLanes, execID int64) (golane.CancelOutcome, error) {
	canceled, _, err := d.CancelPendingFuncExecution(execID)
	if err != nil {
		return "", err
	}
	if canceled {
		// Also flip the cancel registry in case this execution had somehow
		// already been registered (defensive; the DB-first ordering means
		// this is normally a no-op miss).
		c.Cancel(execID)

		lane, _, found, lerr := d.ExecutionLane(execID)
		if lerr != nil {
			log.Printf("resolve lane for canceled execution %d: %v", execID, lerr)
			return golane.CancelOutcome("canceled"), nil
		}
		if !found {
			return golane.CancelOutcome("canceled"), nil
		}
		exec, eerr := d.GetExecution(execID)
		if eerr != nil {
			log.Printf("load canceled execution %d: %v", execID, eerr)
			return golane.CancelOutcome("canceled"), nil
		}
		if exec != nil {
			PublishBoardEvent(b, h, BoardEvent{Type: "task-finished", Lane: lane, Task: exec.TaskName, ExecutionID: execID, Status: "canceled"})
		}
		return golane.CancelOutcome("canceled"), nil
	}

	if c.Cancel(execID) {
		return golane.CancelOutcome("canceling"), nil
	}
	return golane.CancelOutcome("not_running"), nil
}
