package agentflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// extractJSONObject returns the substring from the first '{' to the last '}'.
// Reviewer models often wrap their JSON in prose or markdown fences.
func extractJSONObject(out string) (string, error) {
	start := strings.Index(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end < 0 || end <= start {
		return "", fmt.Errorf("no JSON object in reviewer output")
	}
	return out[start : end+1], nil
}

// buildPlanReviewPrompt fills the plan-reviewer template with the FRD and plan.
func (e *Engine) buildPlanReviewPrompt(frd *FRD, plan *Plan) string {
	frdJSON, _ := json.Marshal(frd)
	planJSON, _ := json.Marshal(plan)
	prompt := e.prompts.PlanReviewer
	prompt = strings.ReplaceAll(prompt, "{{FRD_JSON}}", string(frdJSON))
	prompt = strings.ReplaceAll(prompt, "{{PLAN_JSON}}", string(planJSON))
	return prompt
}

// reviewPlanOnce runs one reviewer pass over the given plan using the active
// profile's review model. The FRD comes from the store; the plan comes from the
// caller (the frontend sends its current draft).
func (e *Engine) reviewPlanOnce(ctx context.Context, plan *Plan) (*PlanReview, error) {
	frd, err := e.store.GetFRD()
	if err != nil {
		return nil, err
	}
	prof := e.resolveProfile("")
	prompt := e.buildPlanReviewPrompt(frd, plan)
	rctx, cancel := context.WithTimeout(ctx, prof.EffectiveTimeout)
	defer cancel()
	out, err := e.runModel(rctx, prof.ReviewModel, prompt, prof.OllamaHost)
	if err != nil {
		return nil, fmt.Errorf("reviewer model: %w", err)
	}
	raw, err := extractJSONObject(out)
	if err != nil {
		return nil, err
	}
	var review PlanReview
	if err := json.Unmarshal([]byte(raw), &review); err != nil {
		return nil, fmt.Errorf("parse reviewer output: %w", err)
	}
	switch review.Verdict {
	case "pass", "repair", "fail":
	default:
		return nil, fmt.Errorf("reviewer returned invalid verdict %q", review.Verdict)
	}
	if review.Issues == nil {
		review.Issues = []ReviewIssue{}
	}
	return &review, nil
}
