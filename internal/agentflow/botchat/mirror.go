package botchat

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The mirror shows agentflow's queue on BotChat's task board. Agentflow stays the source of
// truth: tasks only move in step with what the engine did, and a transition BotChat refuses
// (the task is already there, or someone moved it by hand) is ignored rather than fought.
//
//	queue approved   -> a plan for the effort and a task per queue task, then promoted to ready
//	worker starts    -> claimed and started by the task's worker callsign
//	attempt passes   -> done (worker), accepted and closed (coordinator)
//	attempt blocked  -> blocked on the coordinator
//	effort complete  -> plan closed

func workerCallsign(effort int64, task string) string {
	return "af-" + strconv.FormatInt(effort, 10) + "-worker-" + task
}

func planKey(effort int64) string           { return "plan:" + strconv.FormatInt(effort, 10) }
func taskKey(effort int64, k string) string { return "task:" + strconv.FormatInt(effort, 10) + ":" + k }

// mirrorEffort makes the effort's plan and tasks on BotChat when they do not exist yet, and
// returns the BotChat id of the plan. It is safe to call again: what exists is kept.
func (r *Relay) mirrorEffort(ctx context.Context, c *Client, effort int64) (string, error) {
	if effort <= 0 {
		return "", nil
	}
	if id, err := r.Store.BotchatState(ctx, planKey(effort)); err != nil || id != "" {
		return id, err
	}
	q, err := r.Store.GetQueue(ctx, effort)
	if err != nil || len(q.Tasks) == 0 {
		return "", err
	}
	roomID, err := r.roomFor(ctx, c, effort)
	if err != nil {
		return "", err
	}
	planID, err := c.CreatePlan(ctx, roomID, Coordinator, fmt.Sprintf("agentflow effort %d", effort), "Queue of agentflow effort "+strconv.FormatInt(effort, 10), 4)
	if err != nil {
		return "", err
	}
	ids := map[string]string{}
	remaining := q.Tasks
	for len(remaining) > 0 {
		var next []model.TaskDef
		progressed := false
		for _, t := range remaining {
			deps, ready := []string{}, true
			for _, d := range t.DependsOn {
				if id, ok := ids[d]; ok {
					deps = append(deps, id)
				} else {
					ready = false
				}
			}
			if !ready {
				next = append(next, t)
				continue
			}
			fields := map[string]any{
				"from": Coordinator, "roomId": roomID, "planId": planID,
				"title":     t.Key + " " + t.Title,
				"assignee":  workerCallsign(effort, t.Key),
				"objective": clip(t.Prompt, 3000),
			}
			if len(t.Acceptance) > 0 {
				fields["acceptanceCriteria"] = "- " + strings.Join(t.Acceptance, "\n- ")
			}
			if len(t.FilesTouched) > 0 {
				fields["exclusions"] = "Only touch: " + strings.Join(t.FilesTouched, ", ")
			}
			if len(deps) > 0 {
				fields["dependsOn"] = deps
			}
			id, err := c.CreateTask(ctx, fields)
			if err != nil {
				return "", err
			}
			ids[t.Key] = id
			if err := r.Store.SetBotchatState(ctx, taskKey(effort, t.Key), id); err != nil {
				return "", err
			}
			_, _ = c.Transition(ctx, id, Coordinator, "promote", nil)
			progressed = true
		}
		if !progressed { // a dependency cycle cannot happen in an approved queue; do not loop on one
			break
		}
		remaining = next
	}
	return planID, r.Store.SetBotchatState(ctx, planKey(effort), planID)
}

// taskID is the BotChat id of an agentflow task, or "" when it is not mirrored.
func (r *Relay) taskID(ctx context.Context, effort int64, key string) (string, error) {
	return r.Store.BotchatState(ctx, taskKey(effort, key))
}

// mirrorWorkerStart moves the task to working when its worker stage is handed to a model.
func (r *Relay) mirrorWorkerStart(ctx context.Context, c *Client, s store.FinishedStage) error {
	if s.Task == "" || (s.Role != "worker" && s.Role != "cleanup") {
		return nil
	}
	if _, err := r.mirrorEffort(ctx, c, s.Effort); err != nil {
		return err
	}
	id, err := r.taskID(ctx, s.Effort, s.Task)
	if err != nil || id == "" {
		return err
	}
	who := workerCallsign(s.Effort, s.Task)
	if st, err := c.TaskState(ctx, id); err == nil && st == "ready" {
		_, _ = c.Transition(ctx, id, who, "claim", nil)
	}
	_, _ = c.Transition(ctx, id, who, "start", nil) // refused when it is already working: fine
	return nil
}

// mirrorAttemptEnd moves the task on when an attempt ends: a pass is done, accepted and closed.
func (r *Relay) mirrorAttemptEnd(ctx context.Context, c *Client, a store.AttemptEnd) error {
	if _, err := r.mirrorEffort(ctx, c, a.Effort); err != nil {
		return err
	}
	id, err := r.taskID(ctx, a.Effort, a.Task)
	if err != nil || id == "" {
		return err
	}
	who := workerCallsign(a.Effort, a.Task)
	switch a.Outcome {
	case "passed":
		_, _ = c.Transition(ctx, id, who, "done", map[string]any{
			"changedFiles": []string{"(see the effort's git branch)"},
			"tests":        fmt.Sprintf("attempt %d passed agentflow's verify stage and review", a.N),
			"limitations":  "none reported",
		})
		_, _ = c.Transition(ctx, id, Coordinator, "accept", nil)
		_, _ = c.Transition(ctx, id, Coordinator, "close", map[string]any{"summary": fmt.Sprintf("merged after attempt %d (worker %s)", a.N, a.WorkerModel)})
	case "blocked":
		_, _ = c.Transition(ctx, id, who, "block", map[string]any{"waitingOn": Coordinator, "note": fmt.Sprintf("attempt %d reported it is blocked", a.N)})
	}
	return nil
}

// mirrorEffortDone closes the effort's plan once the run is complete.
func (r *Relay) mirrorEffortDone(ctx context.Context, c *Client, effort int64) {
	if id, _ := r.Store.BotchatState(ctx, planKey(effort)); id != "" {
		_ = c.ClosePlan(ctx, id, Coordinator)
	}
}
