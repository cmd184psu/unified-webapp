package api

import (
	"net/http"
	"sort"
	"strconv"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// statsScope joins a stage to its effort (directly, or through its attempt's
// task and queue) so the stats can be limited to one project.
const statsScope = ` FROM stages s
	LEFT JOIN attempts a ON a.id = s.attempt
	LEFT JOIN tasks t ON t.id = a.task
	LEFT JOIN queues q ON q.id = t.queue
	LEFT JOIN efforts e ON e.id = COALESCE(s.effort, q.effort) `

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
	// TotalMS is the time the model's stages ran, AvgMS the mean per stage; Tokens what they used.
	TotalMS int64 `json:"total_ms"`
	AvgMS   int64 `json:"avg_ms"`
	Tokens  int64 `json:"tokens"`
	// Tier is how the owner rated the model (0 = not rated); Family is derived from its name.
	Tier   int    `json:"tier"`
	Family string `json:"family"`
	// Strikes is the model's current streak in this role, across every project (H7): strikes since its
	// last success or clear. Benched is set at store.StrikeLimit, and the dispatcher then skips the
	// model. StrikesTotal is every strike it has ever had; GET /api/strikes has the history.
	Strikes      int  `json:"strikes"`
	StrikesTotal int  `json:"strikes_total"`
	Benched      bool `json:"benched"`
}

func (a *API) registerStats(mux muxer) {
	mux.HandleFunc("GET /api/stats/models", a.modelStats)
	mux.HandleFunc("GET /api/strikes", a.strikeHistory)
	mux.HandleFunc("POST /api/strikes/clear", a.clearStrikes)
}

// StrikeDay is one day's strikes for a model, role and kind: the series a chart
// of failures over time is drawn from.
type StrikeDay struct {
	Day     string `json:"day"`
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Role    string `json:"role"`
	Kind    string `json:"kind"`
	Count   int    `json:"count"`
}

// StrikeHistory is GET /api/strikes: the strikes newest first (limit, default
// 100, at most 1000) and per-day counts over all of them.
type StrikeHistory struct {
	Events []store.Strike `json:"events"`
	Daily  []StrikeDay    `json:"daily"`
	Total  int            `json:"total"`
}

// strikeHistory answers GET /api/strikes?project=&model=&limit=. Nothing is
// ever deleted from the history; Clear and later successes only change whether
// a strike still counts toward the bench.
func (a *API) strikeHistory(w http.ResponseWriter, r *http.Request) {
	all, err := a.deps.Store.Strikes(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	project, mdl := r.URL.Query().Get("project"), r.URL.Query().Get("model")
	limit := 100
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, 1000)
	}
	out := StrikeHistory{Events: []store.Strike{}, Daily: []StrikeDay{}}
	days := map[StrikeDay]int{}
	for i := len(all) - 1; i >= 0; i-- { // newest first
		s := all[i]
		if (project != "" && s.Project != project) || (mdl != "" && s.Model != mdl) {
			continue
		}
		out.Total++
		if len(out.Events) < limit {
			out.Events = append(out.Events, s)
		}
		days[StrikeDay{Day: time.UnixMilli(s.At).Format("2006-01-02"), Backend: s.Backend, Model: s.Model, Role: s.Role, Kind: s.Kind}]++
	}
	for d, n := range days {
		d.Count = n
		out.Daily = append(out.Daily, d)
	}
	sort.Slice(out.Daily, func(i, j int) bool {
		a, b := out.Daily[i], out.Daily[j]
		if a.Day != b.Day {
			return a.Day > b.Day
		}
		return a.Model+a.Role+a.Kind < b.Model+b.Role+b.Kind
	})
	writeJSON(w, http.StatusOK, out)
}

type strikeKeyBody struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
	Role    string `json:"role"`
}

// clearStrikes answers POST /api/strikes/clear: the owner forgives a model in a
// role, so it can be dispatched again.
func (a *API) clearStrikes(w http.ResponseWriter, r *http.Request) {
	var body strikeKeyBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	k := store.StrikeKey{Backend: body.Backend, Model: body.Model, Role: body.Role}
	if err := a.deps.Store.ClearStrikes(r.Context(), actorOf(r), k); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("strikes.update", map[string]any{"backend": k.Backend, "model": k.Model, "role": k.Role})
	w.WriteHeader(http.StatusNoContent)
}

// modelStats answers GET /api/stats/models: one row per backend, model and
// role that has a finished stage.
func (a *API) modelStats(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	rows, err := a.deps.Store.DB().QueryContext(r.Context(), `
		SELECT s.model, s.backend, s.role,
			SUM(CASE WHEN s.outcome = 'pass' THEN 1 ELSE 0 END),
			SUM(CASE WHEN s.outcome LIKE 'infra:%' OR s.outcome = 'discarded' THEN 1 ELSE 0 END),
			SUM(CASE WHEN s.outcome NOT LIKE 'infra:%' AND s.outcome NOT IN ('discarded', 'pass') THEN 1 ELSE 0 END),
			SUM(CASE WHEN s.finished > s.started THEN s.finished - s.started ELSE 0 END), SUM(s.tokens)`+statsScope+`
		WHERE s.state = 'done' AND s.model <> '' AND (? = '' OR e.project = ?)
		GROUP BY s.model, s.backend, s.role`, project, project)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer rows.Close()
	out := []ModelStat{}
	for rows.Next() {
		var s ModelStat
		if err := rows.Scan(&s.Model, &s.Backend, &s.Role, &s.Passed, &s.Infra, &s.Failed, &s.TotalMS, &s.Tokens); err != nil {
			writeErr(w, err)
			return
		}
		s.Stages = s.Passed + s.Failed + s.Infra
		if s.Stages > 0 {
			s.AvgMS = s.TotalMS / int64(s.Stages)
		}
		s.Family = model.ModelFamily(s.Model)
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
		SELECT at.worker_model, COUNT(*), SUM(CASE WHEN at.outcome = 'passed' THEN 1 ELSE 0 END)
		FROM attempts at JOIN tasks t ON t.id = at.task JOIN queues q ON q.id = t.queue JOIN efforts e ON e.id = q.effort
		WHERE at.outcome NOT IN ('', 'abandoned') AND at.worker_model <> '' AND (? = '' OR e.project = ?)
		GROUP BY at.worker_model`, project, project)
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
	tiers := map[string]int{}
	if bs, err := a.deps.Store.ListBackends(r.Context()); err == nil {
		for _, b := range bs {
			for _, m := range b.Models {
				tiers[b.Name+"/"+m.Model] = m.Tier
			}
		}
	}
	history, err := a.deps.Store.Strikes(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	streak, total := map[store.StrikeKey]int{}, map[store.StrikeKey]int{}
	for _, k := range history {
		total[k.StrikeKey]++
		if k.Counted {
			streak[k.StrikeKey]++
		}
	}
	for i := range out {
		key := store.StrikeKey{Backend: out[i].Backend, Model: out[i].Model, Role: out[i].Role}
		out[i].Strikes, out[i].StrikesTotal = streak[key], total[key]
		out[i].Benched = out[i].Strikes >= store.StrikeLimit
		out[i].Tier = tiers[out[i].Backend+"/"+out[i].Model]
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
