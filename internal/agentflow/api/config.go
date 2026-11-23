package api

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The configuration file (export and import) carries what makes a lineup:
// backends with their models, tiers and tool flags, the global candidate list
// of each role, and the settings that are about models and limits. It carries
// no project, effort, task or statistic, and no API key (keys are write-only
// everywhere). Project-scoped candidate lists belong to a project and are left
// out too.
const (
	configFormat  = "agentflow-config"
	configVersion = 1
)

func (a *API) registerConfig(mux muxer) {
	mux.HandleFunc("GET /api/config/export", a.exportConfig)
	mux.HandleFunc("POST /api/config/import", a.importConfig)
}

type configRef struct {
	Backend string `json:"backend"`
	Model   string `json:"model"`
}

type configBackend struct {
	Name            string `json:"name"`
	Host            string `json:"host"`
	APIType         string `json:"api_type"`
	Hosted          bool   `json:"hosted"`
	Paid            bool   `json:"paid"`
	Metered         bool   `json:"metered,omitempty"`
	DailyTokenCap   int64  `json:"daily_token_cap,omitempty"`
	MonthlyTokenCap int64  `json:"monthly_token_cap,omitempty"`
	Slots           int    `json:"slots"`
	MemoryBudgetMB  int    `json:"memory_budget_mb,omitempty"`
	WindowCapTokens int    `json:"window_cap_tokens,omitempty"`
	DutyPercent     int    `json:"duty_percent,omitempty"`
	Retired         bool   `json:"retired,omitempty"`
	// HasKey is information only: a key is never exported or imported.
	HasKey bool               `json:"has_key,omitempty"`
	Models []BackendModelView `json:"models"`
}

// ConfigDoc is the configuration file.
type ConfigDoc struct {
	Format     string                 `json:"format"`
	Version    int                    `json:"version"`
	ExportedAt string                 `json:"exported_at,omitempty"`
	Settings   map[string]any         `json:"settings,omitempty"`
	Backends   []configBackend        `json:"backends"`
	Candidates map[string][]configRef `json:"candidates"`
}

// portableSettings are the settings an import applies: limits and timeouts,
// which describe how models are used. Where opencode lives, the directory
// picker's roots and the verify slots belong to the machine and stay as they
// are on the machine that imports.
var portableSettings = []string{"max_attempts", "stuck_threshold", "max_infra_retries", "plan_max_rounds", "stall_minutes", "acceptance_rounds", "review_panel",
	"worker_timeout", "review_timeout", "plan_timeout", "verify_timeout", "merger_timeout", "backend_cooldown", "context_floor_tokens", "resolver_mode"}

// configRoles are the roles that have a global candidate list.
var configRoles = []string{"planner", "architect", "critic", "compiler", "worker", "reviewer", "resolver", "merger"}

// exportConfig answers GET /api/config/export with the configuration as a download.
func (a *API) exportConfig(w http.ResponseWriter, r *http.Request) {
	snap, err := a.deps.Store.Snapshot(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	doc := ConfigDoc{Format: configFormat, Version: configVersion, ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Settings: map[string]any{}, Backends: []configBackend{}, Candidates: map[string][]configRef{}}
	all := settingsView(snap.Settings)
	for _, k := range portableSettings {
		doc.Settings[k] = all[k]
	}
	for _, b := range snap.Backends {
		cb := configBackend{Name: b.Name, Host: b.Host, APIType: b.APIType, Hosted: b.Hosted, Paid: b.Paid, Slots: b.Slots,
			Metered: b.Metered, DailyTokenCap: b.DailyTokenCap, MonthlyTokenCap: b.MonthlyTokenCap,
			MemoryBudgetMB: b.MemoryBudgetMB, WindowCapTokens: b.WindowCapTokens, DutyPercent: b.DutyPercent, Retired: b.Retired, HasKey: b.HasKey, Models: []BackendModelView{}}
		for _, m := range b.Models {
			cb.Models = append(cb.Models, BackendModelView{Model: m.Model, Paid: m.Paid, MaxConcurrent: m.MaxConcurrent,
				ContextTokens: m.ContextTokens, WindowTokens: m.WindowTokens, WindowSource: m.WindowSource, Tier: m.Tier, Tools: m.Tools})
		}
		doc.Backends = append(doc.Backends, cb)
	}
	cands := slices.Clone(snap.Candidates)
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Position < cands[j].Position })
	for _, c := range cands {
		if c.Scope == "global" {
			doc.Candidates[c.Role] = append(doc.Candidates[c.Role], configRef{c.Backend, c.Model})
		}
	}
	w.Header().Set("Content-Disposition", `attachment; filename="agentflow-config-`+time.Now().Format("2006-01-02")+`.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// ImportReport says what an import did (apply) or would do (dry run).
type ImportReport struct {
	Applied    bool     `json:"applied"`
	Created    []string `json:"created_backends"`
	Updated    []string `json:"updated_backends"`
	Candidates []string `json:"candidate_roles"`
	Settings   []string `json:"settings"`
	Warnings   []string `json:"warnings"`
}

type importBody struct {
	Config ConfigDoc `json:"config"`
	Apply  bool      `json:"apply"`
}

// importConfig answers POST /api/config/import. Without apply it only checks
// the file and reports; with apply it creates or updates the backends (each
// through the same checks as editing one by hand, so a model without tool
// support is refused), replaces the global candidate list of every role the
// file has, and sets the portable settings. Projects are never touched, and a
// backend's key is kept as it is.
func (a *API) importConfig(w http.ResponseWriter, r *http.Request) {
	var body importBody
	if !decodeBody(w, r, &body, false) {
		return
	}
	doc := body.Config
	if doc.Format != configFormat || doc.Version != configVersion {
		badRequest(w, "config", fmt.Sprintf("not an agentflow configuration file (format %q, version %d)", doc.Format, doc.Version))
		return
	}
	ctx := r.Context()
	cur, err := a.deps.Store.ListBackends(ctx)
	if err != nil {
		writeErr(w, err)
		return
	}
	existing := map[string]store.Backend{}
	for _, b := range cur {
		existing[b.Name] = b
	}
	rep := ImportReport{Applied: body.Apply, Created: []string{}, Updated: []string{}, Candidates: []string{}, Settings: []string{}, Warnings: []string{}}

	known := map[string]bool{} // backend/model pairs that will exist after the import
	for _, b := range existing {
		for _, m := range b.Models {
			known[b.Name+"/"+m.Model] = true
		}
	}
	var plan []store.Backend
	for _, cb := range doc.Backends {
		if cb.Retired {
			rep.Warnings = append(rep.Warnings, "skipped retired backend "+cb.Name)
			continue
		}
		b := store.Backend{Name: cb.Name, Host: cb.Host, APIType: cb.APIType, Hosted: cb.Hosted, Paid: cb.Paid, Metered: cb.Metered, DailyTokenCap: cb.DailyTokenCap, MonthlyTokenCap: cb.MonthlyTokenCap, Slots: cb.Slots, MemoryBudgetMB: cb.MemoryBudgetMB, WindowCapTokens: cb.WindowCapTokens, DutyPercent: cb.DutyPercent}
		for _, m := range cb.Models {
			bm := store.BackendModel{Model: m.Model, Paid: m.Paid, MaxConcurrent: m.MaxConcurrent, ContextTokens: m.ContextTokens, WindowTokens: m.WindowTokens, Tier: m.Tier}
			if bm.WindowTokens > 0 {
				bm.WindowSource = "set"
			}
			b.Models = append(b.Models, bm)
		}
		if err := needModels(b); err != nil {
			writeErr(w, store.Invalid("config", "backend "+cb.Name+": "+err.Error()))
			return
		}
		// Models listed here replace the backend's list, so only these will exist for it.
		for k := range known {
			if strings.HasPrefix(k, b.Name+"/") {
				delete(known, k)
			}
		}
		for _, m := range b.Models {
			known[b.Name+"/"+m.Model] = true
		}
		if prev, ok := existing[b.Name]; ok {
			rep.Updated = append(rep.Updated, b.Name)
			if cb.HasKey && !prev.HasKey {
				rep.Warnings = append(rep.Warnings, "backend "+b.Name+" had an API key where it was exported; none is imported, enter it on the Backends tab")
			}
		} else {
			rep.Created = append(rep.Created, b.Name)
			if cb.HasKey {
				rep.Warnings = append(rep.Warnings, "backend "+b.Name+" had an API key where it was exported; none is imported, enter it on the Backends tab")
			}
		}
		plan = append(plan, b)
	}
	for role, refs := range doc.Candidates {
		if !slices.Contains(configRoles, role) {
			badRequest(w, "config", "unknown role "+strconv.Quote(role)+" in candidates")
			return
		}
		if len(refs) == 0 {
			continue
		}
		for _, c := range refs {
			if !known[c.Backend+"/"+c.Model] {
				badRequest(w, "config", fmt.Sprintf("candidates for %s name %s/%s, which no backend in the file or here has", role, c.Backend, c.Model))
				return
			}
		}
		rep.Candidates = append(rep.Candidates, role)
	}
	sort.Strings(rep.Candidates)
	patch := map[string]string{}
	for _, k := range portableSettings {
		switch v := doc.Settings[k].(type) {
		case nil:
		case string:
			patch[k] = v
		case float64:
			patch[k] = strconv.Itoa(int(v))
		}
	}
	for k := range patch {
		rep.Settings = append(rep.Settings, k)
	}
	sort.Strings(rep.Settings)
	if !body.Apply {
		writeJSON(w, http.StatusOK, rep)
		return
	}

	for _, b := range plan {
		var prev []store.BackendModel
		prevB, isOld := existing[b.Name]
		if isOld {
			prev = prevB.Models
			b.Retired = prevB.Retired
		}
		if err := a.checkTools(ctx, &b, prev); err != nil {
			writeErr(w, err)
			return
		}
		if isOld {
			err = a.deps.Engine.UpdateBackend(ctx, actorOf(r), b)
		} else {
			err = a.deps.Engine.AddBackend(ctx, actorOf(r), b)
		}
		if err != nil {
			writeErr(w, store.Invalid("config", "backend "+b.Name+": "+err.Error()))
			return
		}
	}
	for _, role := range rep.Candidates {
		var refs []store.CandidateRef
		for _, c := range doc.Candidates[role] {
			refs = append(refs, store.CandidateRef{Backend: c.Backend, Model: c.Model})
		}
		if err := a.deps.Store.SetRoleCandidates(ctx, actorOf(r), "global", "", role, refs); err != nil {
			writeErr(w, store.Invalid("config", "candidates for "+role+": "+err.Error()))
			return
		}
	}
	if len(patch) > 0 {
		if err := a.deps.Store.UpdateSettings(ctx, actorOf(r), patch); err != nil {
			writeErr(w, err)
			return
		}
	}
	a.emit("backend.update", map[string]any{"backend": ""})
	a.emit("candidates.update", map[string]any{"role": ""})
	a.emit("settings.update", map[string]any{})
	writeJSON(w, http.StatusOK, rep)
}
