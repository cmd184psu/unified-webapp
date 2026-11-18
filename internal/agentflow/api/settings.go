package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func (a *API) registerSettings(mux muxer) {
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
		"stall_minutes":     strconv.Itoa(s.StallMinutes),
		"acceptance_rounds": strconv.Itoa(s.AcceptanceRounds),
		"review_panel":      strconv.Itoa(s.ReviewPanel),
		"worker_timeout":    dur(s.WorkerTimeout),
		"review_timeout":    dur(s.ReviewTimeout),
		"plan_timeout":      dur(s.PlanTimeout),
		"verify_timeout":    dur(s.VerifyTimeout),
	}
}

// settingsView is the global settings: the overridable limits plus the
// opencode path, verify slots, the backend cooldown and the directory picker's
// roots.
func settingsView(s store.Settings) map[string]any {
	return map[string]any{
		"max_attempts":         s.MaxAttempts,
		"stuck_threshold":      s.StuckThreshold,
		"max_infra_retries":    s.MaxInfraRetries,
		"plan_max_rounds":      s.PlanMaxRounds,
		"stall_minutes":        s.StallMinutes,
		"acceptance_rounds":    s.AcceptanceRounds,
		"review_panel":         s.ReviewPanel,
		"worker_timeout":       s.WorkerTimeout.String(),
		"review_timeout":       s.ReviewTimeout.String(),
		"plan_timeout":         s.PlanTimeout.String(),
		"verify_timeout":       s.VerifyTimeout.String(),
		"opencode_path":        s.OpencodePath,
		"botchat_url":          s.BotchatURL,
		"botchat_key_file":     s.BotchatKeyFile,
		"botchat_mode":         s.BotchatMode,
		"verify_slots":         s.VerifySlots,
		"context_floor_tokens": s.ContextFloor,
		"backend_cooldown":     s.BackendCooldown.String(),
		"browse_roots":         s.BrowseRoots,
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
	// browse_roots is a list; the store keeps it as JSON text.
	if raw, ok := body["browse_roots"]; ok {
		var roots []string
		if err := json.Unmarshal(raw, &roots); err != nil {
			badRequest(w, "browse_roots", "browse_roots must be a list of absolute paths")
			return
		}
		text, _ := json.Marshal(roots)
		body["browse_roots"], _ = json.Marshal(string(text))
	}
	patch, ok := flexStrings(w, body, "")
	if !ok {
		return
	}
	before, _ := a.deps.Store.GetSettings(r.Context())
	if err := a.deps.Store.UpdateSettings(r.Context(), actorOf(r), patch); err != nil {
		writeErr(w, err)
		return
	}
	// The page sends every key on each save; only a changed one needs a refresh.
	after, _ := a.deps.Store.GetSettings(r.Context())
	changed := map[string]string{}
	if before.VerifySlots != after.VerifySlots {
		changed["verify_slots"] = ""
	}
	if before.OpencodePath != after.OpencodePath {
		changed["opencode_path"] = ""
	}
	a.applySettings(r.Context(), changed)
	a.emit("settings.update", nil)
	a.getSettings(w, r)
}

// applySettings makes a saved patch take effect now. Limits, cooldown and
// browse roots are read from the store at use time; the verify lane width and
// the opencode probe are state held elsewhere, so they are refreshed here.
func (a *API) applySettings(ctx context.Context, patch map[string]string) {
	if _, ok := patch["verify_slots"]; ok && a.deps.Engine != nil {
		_ = a.deps.Engine.SyncLanes(ctx)
	}
	if _, ok := patch["opencode_path"]; ok {
		a.probe(ctx)
	}
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
