package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/prompts"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The acceptance check (H31). Each task is reviewed alone, so a requirement
// that cuts across tasks (where the arithmetic is done, how every error is
// reported) can be missed by every review and still fail the whole. When the
// last task has passed, a reviewer reads the merged result against the
// approved FRD, requirement by requirement. A gap it finds becomes a follow-up
// task in the same queue, which goes through the usual worker, verify and
// review; when those pass, the check runs again. acceptance_rounds bounds the
// rounds, and the check can never keep an effort from completing: a check that
// cannot run, or gaps left after the last round, are recorded and the effort
// completes.

const (
	acceptanceRef          = "acceptance"
	eventAcceptanceFound   = "acceptance.found"
	eventAcceptancePassed  = "acceptance.passed"
	eventAcceptanceLeft    = "acceptance.gaps_left"
	eventAcceptanceSkipped = "acceptance.skipped"
	maxAcceptanceGaps      = 6
	// acceptanceStrikes is how many failed runs in a row end the check: it
	// is a safety net, so it gives up sooner than a task does.
	acceptanceStrikes = 2
)

// isAcceptance reports whether st is an acceptance check: a reviewer-role
// stage of an effort, not of an attempt.
func isAcceptance(st model.Stage) bool {
	return st.Role == "reviewer" && st.PromptRef == acceptanceRef
}

// parseAcceptance is parseFinal for an acceptance check.
func parseAcceptance(final string) (json.RawMessage, string, error) {
	raw, err := schema.ExtractFinal(final)
	if err != nil {
		return nil, "", err
	}
	if _, err := schema.Parse[schema.AcceptanceOut](raw); err != nil {
		return nil, "", err
	}
	return raw, OutcomePass, nil
}

// acceptancePrompt is the prompt of an acceptance check: the approved FRD.
func (e *Engine) acceptancePrompt(ctx context.Context, st model.Stage) (string, error) {
	text, err := prompts.Load(acceptanceRef)
	if err != nil {
		return "", err
	}
	frd := "null"
	if d, err := e.deps.Store.GetDoc(ctx, st.Effort, model.DocFRD, 0); err == nil {
		frd = compactJSON(d.JSON)
	}
	return strings.ReplaceAll(text, "{{FRD_JSON}}", frd), nil
}

// acceptance runs the check for an effort whose tasks are all done. It reports
// whether the effort may complete now; false means a check is running or
// follow-up tasks were added.
func (e *Engine) acceptance(ctx context.Context, proj store.Project, ef model.Effort) (bool, error) {
	set, err := e.deps.Store.EffectiveSettings(ctx, proj.Slug)
	if err != nil {
		return false, err
	}
	if set.AcceptanceRounds == 0 || e.deps.Git == nil {
		return true, nil
	}
	if _, ok, err := e.effortFRD(ctx, ef.ID); err != nil || !ok {
		return true, err
	}
	for _, a := range []string{eventAcceptancePassed, eventAcceptanceLeft, eventAcceptanceSkipped} {
		if evs, err := e.effortEvents(ctx, ef.ID, a); err != nil || len(evs) > 0 {
			return err == nil, err
		}
	}
	found, err := e.effortEvents(ctx, ef.ID, eventAcceptanceFound)
	if err != nil {
		return false, err
	}
	round := len(found)
	floor, err := e.eventFloor(ctx, ef.ID, eventAcceptanceFound)
	if err != nil {
		return false, err
	}
	all, err := e.deps.Store.ListEffortStages(ctx, ef.ID)
	if err != nil {
		return false, err
	}
	var stages []model.Stage
	for _, st := range all {
		if isAcceptance(st) && st.ID > floor {
			stages = append(stages, st)
		}
	}
	for _, st := range stages {
		if st.State != model.StageDone {
			return false, nil // in flight
		}
	}
	skip := func(reason string) (bool, error) {
		return true, e.noteEffort(ctx, SystemActor, eventAcceptanceSkipped, ef.ID, map[string]any{"reason": reason})
	}
	if len(stages) == 0 {
		return false, e.startEffortStageRef(ctx, proj, ef.ID, "reviewer", nil, "", acceptanceRef)
	}
	last := stages[len(stages)-1]
	if IsInfra(last.Outcome) {
		if infraRun(stages) >= acceptanceStrikes {
			return skip("the check could not run: " + last.Outcome + " on " + last.Backend)
		}
		skipped := false
		err := e.effortInfra(ctx, proj, stages, last,
			func(avoid string) error {
				if avoid != "" {
					avoid = avoidTried(stages)
				}
				return e.startEffortStageRef(ctx, proj, ef.ID, "reviewer", nil, avoid, acceptanceRef)
			},
			func(reason string) error {
				skipped = true
				_, err := skip("the check could not run: " + reason)
				return err
			})
		return skipped, err
	}
	if last.Outcome != OutcomePass {
		return skip("the check ended " + last.Outcome)
	}
	out, err := schema.Parse[schema.AcceptanceOut](last.Parsed)
	if err != nil {
		return skip(err.Error())
	}
	if out.Verdict == "pass" || len(out.Gaps) == 0 {
		return true, e.noteEffort(ctx, SystemActor, eventAcceptancePassed, ef.ID, map[string]any{"round": round + 1, "stage": last.ID, "summary": out.Summary})
	}
	gaps := out.Gaps
	if len(gaps) > maxAcceptanceGaps {
		gaps = gaps[:maxAcceptanceGaps]
	}
	if round >= set.AcceptanceRounds {
		msg := fmt.Sprintf("the finished work still has %d gap(s) against the FRD after %d round(s) of the acceptance check: %s", len(gaps), round, gapSummary(gaps))
		if err := e.warnOnce(ctx, ef.ID, msg); err != nil {
			return false, err
		}
		return true, e.noteEffort(ctx, SystemActor, eventAcceptanceLeft, ef.ID, map[string]any{"round": round + 1, "stage": last.ID, "gaps": gaps})
	}
	defs := followUpTasks(round+1, gaps)
	err = e.deps.Store.AddTasks(ctx, SystemActor, ef.ID, defs, eventAcceptanceFound,
		map[string]any{"round": round + 1, "after_stage": last.ID, "stage": last.ID, "gaps": gaps})
	if err != nil {
		return false, err
	}
	e.changed()
	return false, nil
}

func gapSummary(gaps []schema.Gap) string {
	parts := make([]string, len(gaps))
	for i, g := range gaps {
		parts[i] = g.Requirement + ": " + g.Problem
	}
	return strings.Join(parts, "; ")
}

// followUpTasks turns the gaps of one round into tasks, one after another so
// that two fixes never edit the same file in parallel. The criterion names the
// requirement, so the worker and the reviewer are shown its exact wording.
func followUpTasks(round int, gaps []schema.Gap) []model.TaskDef {
	defs := make([]model.TaskDef, len(gaps))
	for i, g := range gaps {
		d := model.TaskDef{
			Key:   fmt.Sprintf("A%d-%d", round, i+1),
			Phase: "acceptance",
			Title: "Close gap in " + g.Requirement + ": " + clip(g.Problem, 80),
			Prompt: fmt.Sprintf("A check of the finished work against the approved requirements found a gap. Close it, and change nothing else: every other task is done and passed.\n\nRequirement: %s\n\nWhat is wrong: %s\n\nEvidence: %s\n\nWhat to change: %s\n",
				g.Requirement, g.Problem, g.Evidence, g.Fix),
			Acceptance: []string{g.Requirement + ": the gap is closed: " + g.Problem},
		}
		for _, f := range g.Files {
			if f = strings.TrimPrefix(strings.TrimSpace(f), "./"); f != "" && !strings.HasPrefix(f, "/") {
				d.FilesTouched = append(d.FilesTouched, f)
			}
		}
		if i > 0 {
			d.DependsOn = []string{defs[i-1].Key}
		}
		defs[i] = d
	}
	return defs
}

// infraRun counts the infrastructure failures that end the run of stages.
func infraRun(stages []model.Stage) int {
	n := 0
	for i := len(stages) - 1; i >= 0 && IsInfra(stages[i].Outcome); i-- {
		n++
	}
	return n
}
