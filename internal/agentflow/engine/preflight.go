package engine

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// PreflightRole is what a project can use for one role.
type PreflightRole struct {
	Role     string   `json:"role"`
	Eligible []string `json:"eligible"` // backend/model, in candidate order
	Skipped  []string `json:"skipped,omitempty"`
}

// PreflightView is the answer to "can this project's run get through?" before
// it starts (H29): every role has a model that can serve it, every worker
// has a reviewer, and the models in use have been checked lately.
type PreflightView struct {
	OK       bool            `json:"ok"`
	Problems []string        `json:"problems"`
	Warnings []string        `json:"warnings"`
	Roles    []PreflightRole `json:"roles"`
}

// preflightRoles are the roles a run needs, in pipeline order.
var preflightRoles = []string{"planner", "architect", "critic", "compiler", "worker", "reviewer"}

// preflightFresh is how recent a passed check must be to count as checked.
const preflightFresh = 24 * time.Hour

// Preflight looks at a project's candidates, scope, strikes, tiers and model
// checks the way the pickers will, and says what would leave a task or a
// planning stage waiting before the run is started. It starts nothing.
func (e *Engine) Preflight(ctx context.Context, slug string) (PreflightView, error) {
	p, err := e.deps.Store.GetProject(ctx, slug)
	if err != nil {
		return PreflightView{}, err
	}
	v, err := e.newView(ctx)
	if err != nil {
		return PreflightView{}, err
	}
	checks, err := e.deps.Store.ModelChecks(ctx)
	if err != nil {
		return PreflightView{}, err
	}
	checked := map[string]bool{}
	for _, c := range checks {
		if c.OK && e.deps.Clock().Sub(time.UnixMilli(c.Checked)) < preflightFresh {
			checked[modelKey(c.Backend, c.Model)] = true
		}
	}
	out := PreflightView{Problems: []string{}, Warnings: []string{}, Roles: []PreflightRole{}}
	unchecked := map[string]bool{}
	for _, role := range preflightRoles {
		r := PreflightRole{Role: role, Eligible: []string{}}
		for _, c := range v.snap.CandidatesFor(role, p.Slug) {
			ref := c.Backend + "/" + c.Model
			if why := skipReason(v, p, c, false); why != "" {
				r.Skipped = append(r.Skipped, ref+": "+why)
				continue
			}
			r.Eligible = append(r.Eligible, ref)
			for _, b := range v.snap.Backends {
				if b.Name == c.Backend && b.Hosted && !checked[modelKey(c.Backend, c.Model)] {
					unchecked[ref] = true
				}
			}
		}
		if len(r.Eligible) == 0 {
			out.Problems = append(out.Problems, fmt.Sprintf("no model can serve %s: %s", role, firstOr(r.Skipped, "no candidates configured")))
		}
		out.Roles = append(out.Roles, r)
	}
	for _, r := range out.Roles {
		if r.Role != "worker" {
			continue
		}
		for _, c := range v.snap.CandidatesFor("worker", p.Slug) {
			if skipReason(v, p, c, false) != "" {
				continue
			}
			if _, why := reviewerFor(v, p, "", c.Model, false); why != "" {
				out.Problems = append(out.Problems, fmt.Sprintf("worker %s/%s would have no reviewer: %s", c.Backend, c.Model, why))
			}
		}
	}
	for ref := range unchecked {
		out.Warnings = append(out.Warnings, "hosted model "+ref+" has not passed Check models in the last day")
	}
	out.OK = len(out.Problems) == 0
	return out, nil
}

func firstOr(list []string, d string) string {
	if len(list) == 0 {
		return d
	}
	return strings.Join(list[:min(len(list), 3)], "; ")
}
