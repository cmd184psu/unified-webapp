package api

import (
	"net/http"
	"sort"
)

// ModelStat is how a model has done: its finished stages by outcome class
// (MB8) and, as a worker, how its attempts ended. Infrastructure outcomes
// (infra:*) and discarded stages say nothing about the model, so they are
// counted apart and left out of the pass rate.
type ModelStat struct {
	Model          string  `json:"model"`
	Backend        string  `json:"backend"`
	Role           string  `json:"role"`
	Stages         int     `json:"stages"`
	Passed         int     `json:"passed"`
	Failed         int     `json:"failed"`
	Infra          int     `json:"infra"`
	PassRate       float64 `json:"pass_rate"`
	AttemptsTotal  int     `json:"attempts_total"`
	AttemptsPassed int     `json:"attempts_passed"`
}

func (a *API) registerStats(mux muxer) {
	mux.HandleFunc("GET /api/stats/models", a.modelStats)
}

// modelStats answers GET /api/stats/models: one row per backend, model and
// role that has a finished stage.
func (a *API) modelStats(w http.ResponseWriter, r *http.Request) {
	rows, err := a.deps.Store.DB().QueryContext(r.Context(), `
		SELECT model, backend, role,
			SUM(CASE WHEN outcome = 'pass' THEN 1 ELSE 0 END),
			SUM(CASE WHEN outcome LIKE 'infra:%' OR outcome = 'discarded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN outcome NOT LIKE 'infra:%' AND outcome NOT IN ('discarded', 'pass') THEN 1 ELSE 0 END)
		FROM stages WHERE state = 'done' AND model <> ''
		GROUP BY model, backend, role`)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []ModelStat{}
	for rows.Next() {
		var s ModelStat
		if err := rows.Scan(&s.Model, &s.Backend, &s.Role, &s.Passed, &s.Infra, &s.Failed); err != nil {
			writeErr(w, err)
			return
		}
		s.Stages = s.Passed + s.Failed + s.Infra
		if judged := s.Passed + s.Failed; judged > 0 {
			s.PassRate = float64(s.Passed) / float64(judged)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		writeErr(w, err)
		return
	}
	att, err := a.deps.Store.DB().QueryContext(r.Context(), `
		SELECT worker_model, COUNT(*), SUM(CASE WHEN outcome = 'passed' THEN 1 ELSE 0 END)
		FROM attempts WHERE outcome NOT IN ('', 'abandoned') AND worker_model <> '' GROUP BY worker_model`)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer att.Close()
	type tally struct{ total, passed int }
	byModel := map[string]tally{}
	for att.Next() {
		var m string
		var t tally
		if err := att.Scan(&m, &t.total, &t.passed); err != nil {
			writeErr(w, err)
			return
		}
		byModel[m] = t
	}
	if err := att.Err(); err != nil {
		writeErr(w, err)
		return
	}
	for i := range out {
		if out[i].Role == "worker" {
			t := byModel[out[i].Model]
			out[i].AttemptsTotal, out[i].AttemptsPassed = t.total, t.passed
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Backend != b.Backend {
			return a.Backend < b.Backend
		}
		return a.Role < b.Role
	})
	writeJSON(w, http.StatusOK, out)
}
