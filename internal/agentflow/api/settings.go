package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerSettings(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", a.getSettings)
	mux.HandleFunc("PUT /api/settings", a.putSettings)
	mux.HandleFunc("GET /api/opencode/version", a.getOpencodeVersion)
}

// limitMap renders settings the way the store takes them back: integers as
// decimal text and durations as Go duration strings.
func limitMap(s store.Settings) map[string]string {
	dur := func(d time.Duration) string { return d.String() }
	return map[string]string{
		"max_attempts":      strconv.Itoa(s.MaxAttempts),
		"stuck_threshold":   strconv.Itoa(s.StuckThreshold),
		"max_infra_retries": strconv.Itoa(s.MaxInfraRetries),
		"plan_max_rounds":   strconv.Itoa(s.PlanMaxRounds),
		"worker_timeout":    dur(s.WorkerTimeout),
		"review_timeout":    dur(s.ReviewTimeout),
		"plan_timeout":      dur(s.PlanTimeout),
		"verify_timeout":    dur(s.VerifyTimeout),
	}
}

// settingsView is the global settings: the overridable limits plus the
// opencode path, verify slots and the backend cooldown.
func settingsView(s store.Settings) map[string]any {
	return map[string]any{
		"max_attempts":      s.MaxAttempts,
		"stuck_threshold":   s.StuckThreshold,
		"max_infra_retries": s.MaxInfraRetries,
		"plan_max_rounds":   s.PlanMaxRounds,
		"worker_timeout":    s.WorkerTimeout.String(),
		"review_timeout":    s.ReviewTimeout.String(),
		"plan_timeout":      s.PlanTimeout.String(),
		"verify_timeout":    s.VerifyTimeout.String(),
		"opencode_path":     s.OpencodePath,
		"verify_slots":      s.VerifySlots,
		"backend_cooldown":  s.BackendCooldown.String(),
	}
}

func (a *API) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := a.deps.Store.GetSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsView(s))
}

// putSettings applies the keys it is given (numbers or strings) in one
// transaction; the store validates them and names the field that is wrong.
func (a *API) putSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	if !decodeBody(w, r, &body, false) {
		return
	}
	patch, ok := flexStrings(w, body, "")
	if !ok {
		return
	}
	if err := a.deps.Store.UpdateSettings(r.Context(), actorOf(r), patch); err != nil {
		writeErr(w, err)
		return
	}
	a.emit("settings.update", nil)
	a.getSettings(w, r)
}

// getOpencodeVersion reports the cached opencode probe; ?refresh=1 asks the
// runner again (OC1).
func (a *API) getOpencodeVersion(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		writeJSON(w, http.StatusOK, a.probe(r.Context()))
		return
	}
	writeJSON(w, http.StatusOK, a.openCode())
}
