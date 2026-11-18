package engine

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// YOLO mode (YO1 to YO5) auto-approves the human gates of one effort. It only
// stands in for a human click: it adds no capability. Sandbox, git, MB5 and
// PP1 code never consult it, and yolo.go, plan.go and compile.go are checked
// not to call them (TestYOLOCallsNoSandboxGitPushOrScopeOverride).

// yoloActor is recorded on every auto-approval (YO5).
const yoloActor = "yolo"

// Invariant is a rule YOLO cannot relax, for the UI to list beside the toggle
// (YO3).
type Invariant struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// EnforcedInvariants lists what stays enforced under YOLO.
func EnforcedInvariants() []Invariant {
	return []Invariant{
		{"GIT2", "The repository's main working tree and your checked-out branch are never modified; work happens on agentflow branches in agentflow worktrees."},
		{"GIT4", "Nothing is ever pushed. Merging and pushing the integration branch stays a command you run yourself."},
		{"GIT5", "Models get read-only git; any ref, HEAD, index or stash change is reverted, audited and counted as a policy violation."},
		{"GIT6", "Stage jobs run with no git or forge credentials reachable."},
		{"WC1", "Every stage runs in its worktree, in its own process group, with an allowlisted environment."},
		{"WC2", "Every model stage runs inside the bwrap sandbox: no SSH agent, ~/.ssh, git credential helpers or forge configuration is reachable."},
		{"WC3", "A project whose repo_path is, contains or is inside the server's working directory or data directory is refused."},
		{"MB5", "Paid providers stay disabled unless a paid model is explicitly listed for a role."},
		{"PP1", "Under model scope local no stage is dispatched to a hosted backend."},
	}
}

// SetYOLO turns YOLO on or off for an effort. Turning it on needs confirm
// (YO1). Both directions are audited with the actor. It takes effect at the
// next gate: auto-approvals already made stand (YO4).
func (e *Engine) SetYOLO(ctx context.Context, actor string, effort int64, on, confirm bool) error {
	s := e.deps.Store
	ef, err := s.GetEffort(ctx, effort)
	if err != nil {
		return err
	}
	if !model.IsActiveEffortStatus(ef.Status) {
		return fmt.Errorf("effort %d is %s: %w", effort, ef.Status, store.ErrConflict)
	}
	if on && !confirm {
		return store.Invalid("confirm", "enabling YOLO needs explicit confirmation")
	}
	if ef.YOLO == on {
		return nil
	}
	if _, err := s.UpdateEffort(ctx, actor, effort, store.EffortPatch{YOLO: &on}); err != nil {
		return err
	}
	action := "yolo.disable"
	if on {
		action = "yolo.enable"
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, actor, action, "effort", strconv.FormatInt(effort, 10), map[string]any{"project": ef.Project})
		return err
	})
}

// yoloGate reads the effort's current YOLO flag. Every gate calls it when it
// is reached, never earlier, so a switch-off applies from the next gate (YO4).
func (e *Engine) yoloGate(ctx context.Context, effort int64) bool {
	ef, err := e.deps.Store.GetEffort(ctx, effort)
	return err == nil && ef.YOLO && model.IsActiveEffortStatus(ef.Status)
}

// yoloRetry gives a task that just became stuck or exhausted one automatic
// clean-worktree retry (YO2). A task YOLO already retried waits for a human.
func (e *Engine) yoloRetry(ctx context.Context, c *chain) error {
	if !e.yoloGate(ctx, c.eff.ID) {
		return nil
	}
	evs, err := e.deps.Store.ListEvents(ctx, "task", strconv.FormatInt(c.task.ID, 10), 10000)
	if err != nil {
		return err
	}
	for _, ev := range evs {
		if ev.Action == "task.reset" && ev.Actor == yoloActor {
			return nil
		}
	}
	return e.Retry(ctx, yoloActor, c.task.ID, true)
}
