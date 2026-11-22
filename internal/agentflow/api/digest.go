package api

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerDigest(mux muxer) {
	mux.HandleFunc("GET /api/digest", a.getDigest)
}

func (a *API) getDigest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{}
	// waiting for a human
	if a.deps.Engine != nil {
		if items, err := a.deps.Engine.Attention(ctx); err == nil {
			out["attention"] = items
		}
	}
	// recent automatic actions
	evs, _ := a.deps.Store.ListEvents(ctx, "", "", 500)
	var auto []store.Event
	for _, e := range evs {
		if len(e.Action) >= 5 && e.Action[:5] == "auto_" {
			auto = append(auto, e)
		}
	}
	out["auto_actions"] = auto
	// budgets used
	type budgetView struct {
		Project      string  `json:"project"`
		Effort       int64   `json:"effort"`
		Title        string  `json:"title"`
		Fraction     float64 `json:"fraction"`
		Tokens       int64   `json:"tokens"`
		TokenLimit   int64   `json:"token_limit"`
		Seconds      int64   `json:"seconds"`
		SecondsLimit int64   `json:"seconds_limit"`
	}
	var budgets []budgetView
	projects, _ := a.deps.Store.ListProjects(ctx)
	for _, p := range projects {
		efforts, _ := a.deps.Store.ListEfforts(ctx, p.Slug)
		for _, ef := range efforts {
			if ef.Status == model.EffortComplete || ef.Status == model.EffortAbandoned {
				continue
			}
			bv := budgetView{Project: p.Slug, Effort: ef.ID, Title: ef.Title}
			if a.deps.Engine != nil {
				if b, err := a.deps.Engine.EffortBudget(ctx, ef); err == nil {
					bv.Fraction = b.Fraction()
					bv.Tokens = b.Tokens
					bv.TokenLimit = b.TokenLimit
					bv.Seconds = b.Seconds
					bv.SecondsLimit = b.SecondsLimit
				}
			}
			budgets = append(budgets, bv)
		}
	}
	out["budgets"] = budgets
	// resting models
	if rests, err := a.deps.Store.ModelRests(ctx); err == nil {
		out["resting_models"] = rests
	}
	writeJSON(w, http.StatusOK, out)
}
