package agentflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// buildCompilePrompt fills the task-compiler template with the approved plan.
func (e *Engine) buildCompilePrompt(plan *Plan) string {
	planJSON, _ := json.Marshal(plan)
	prompt := e.prompts.TaskCompiler
	prompt = strings.ReplaceAll(prompt, "{{PLAN_JSON}}", string(planJSON))
	prompt = strings.ReplaceAll(prompt, "{{FRD_ID}}", plan.FRDID)
	prompt = strings.ReplaceAll(prompt, "{{TODAY}}", time.Now().Format("2006-01-02"))
	return prompt
}

// compileTasksOnce runs the task-compiler model over the approved plan and
// returns a validated queue plus normalization warnings. Structural breaks
// (bad IDs, forward deps, absolute paths, empty required fields) are errors;
// status mismatches are normalized with a warning.
func (e *Engine) compileTasksOnce(ctx context.Context, plan *Plan) (*TaskQueue, []string, error) {
	prof := e.resolveProfile("")
	prompt := e.buildCompilePrompt(plan)
	rctx, cancel := context.WithTimeout(ctx, prof.EffectiveTimeout)
	defer cancel()
	// Compilation expands the plan, which is planner work, so it runs on the
	// profile's planner model rather than the worker model.
	out, err := e.runModel(rctx, prof.PlannerModel, prompt, prof.OllamaHost)
	if err != nil {
		return nil, nil, fmt.Errorf("compiler model: %w", err)
	}
	raw, err := extractJSONObject(out)
	if err != nil {
		return nil, nil, err
	}
	var queue TaskQueue
	if err := json.Unmarshal([]byte(raw), &queue); err != nil {
		return nil, nil, fmt.Errorf("parse compiler output: %w", err)
	}
	warnings, err := validateQueue(plan, &queue)
	if err != nil {
		return nil, nil, err
	}
	return &queue, warnings, nil
}

// validateQueue checks the compiler's structural contract and normalizes the
// fields the driver owns (frd_id, plan_status, compiled_date, ready/blocked
// status). It returns warnings for each normalization.
func validateQueue(plan *Plan, q *TaskQueue) ([]string, error) {
	var warnings []string
	if len(q.Tasks) == 0 {
		return nil, fmt.Errorf("compiler produced no tasks")
	}
	if q.FRDID != plan.FRDID {
		warnings = append(warnings, fmt.Sprintf("frd_id corrected %q to plan's %q", q.FRDID, plan.FRDID))
		q.FRDID = plan.FRDID
	}
	q.PlanStatus = "approved"
	if q.CompiledDate == "" {
		q.CompiledDate = time.Now().Format("2006-01-02")
	}
	seen := make(map[string]bool, len(q.Tasks))
	for i := range q.Tasks {
		t := &q.Tasks[i]
		wantID := fmt.Sprintf("T%03d", i+1)
		if t.ID != wantID {
			return nil, fmt.Errorf("task %d has id %q, want sequential %q", i+1, t.ID, wantID)
		}
		if t.Title == "" {
			return nil, fmt.Errorf("task %s has empty title", t.ID)
		}
		if t.Phase == "" {
			return nil, fmt.Errorf("task %s has empty phase", t.ID)
		}
		if t.Prompt == "" {
			return nil, fmt.Errorf("task %s has empty prompt", t.ID)
		}
		if t.Verify == "" {
			return nil, fmt.Errorf("task %s has empty verify", t.ID)
		}
		if len(t.Acceptance) == 0 {
			return nil, fmt.Errorf("task %s has empty acceptance", t.ID)
		}
		if len(t.FilesTouched) == 0 {
			return nil, fmt.Errorf("task %s has empty files_touched", t.ID)
		}
		if t.WorkingDir != "." {
			return nil, fmt.Errorf("task %s has working_dir %q, want %q", t.ID, t.WorkingDir, ".")
		}
		for _, dep := range t.DependsOn {
			if !seen[dep] {
				return nil, fmt.Errorf("task %s depends on %q, which is not an earlier task", t.ID, dep)
			}
		}
		for _, field := range append(append([]string{t.Prompt, t.Verify}, t.FilesTouched...), t.WorkingDir) {
			if strings.HasPrefix(field, "/") {
				return nil, fmt.Errorf("task %s contains absolute path %q", t.ID, field)
			}
		}
		want := "blocked"
		if len(t.DependsOn) == 0 {
			want = "ready"
		}
		if t.Status != want {
			warnings = append(warnings, fmt.Sprintf("%s: status corrected %q to %q", t.ID, t.Status, want))
			t.Status = want
		}
		seen[t.ID] = true
	}
	return warnings, nil
}
