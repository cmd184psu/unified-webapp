package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// fakeOllamaCaps answers /api/show with the capabilities of each model (a model
// missing from the map is unknown to the host), and /api/tags and /api/pull so
// the same host can be probed and installed from.
func fakeOllamaCaps(t *testing.T, caps map[string][]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			var b struct{ Model string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			c, ok := caps[b.Model]
			if !ok {
				http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": c, "model_info": map[string]any{"x.context_length": 8192}})
		case "/api/tags":
			ms := []map[string]string{}
			for m := range caps {
				ms = append(ms, map[string]string{"name": m})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"models": ms})
		case "/api/pull":
			_, _ = w.Write([]byte(`{"status":"success"}` + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func modelTools(t *testing.T, e *env, backend string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, b := range decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, http.StatusOK)) {
		if b.Name == backend {
			for _, m := range b.Models {
				out[m.Model] = m.Tools
			}
		}
	}
	return out
}

// H10: a backend cannot be saved with a model its host says has no tool
// support, whatever the client claims; the server records what the host says.
func TestH10_BackendRefusesAModelWithoutTools(t *testing.T) {
	srv := fakeOllamaCaps(t, map[string][]string{"good": {"completion", "tools"}, "gemma": {"completion", "vision"}})
	e := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	b["models"] = []map[string]any{{"model": "good"}, {"model": "gemma", "tools": "yes"}} // the claim is ignored
	e.field("POST", "/api/backends", b, "models")
	b["models"] = []map[string]any{{"model": "good", "tools": "no"}}
	e.ok("POST", "/api/backends", b, http.StatusCreated)
	if got := modelTools(t, e, "gpu"); got["good"] != "yes" {
		t.Fatalf("tools = %v, want good=yes from the host", got)
	}
	// Adding the bad one by edit is refused the same way and nothing is saved.
	e.field("PATCH", "/api/backends/gpu", map[string]any{"models": []map[string]any{{"model": "good"}, {"model": "gemma"}}}, "models")
	if got := modelTools(t, e, "gpu"); len(got) != 1 {
		t.Fatalf("a refused edit changed the backend: %v", got)
	}
}

// The probe reports tool support so the form can warn before anything is saved.
func TestH10_ProbeReportsTools(t *testing.T) {
	srv := fakeOllamaCaps(t, map[string][]string{"good": {"completion", "tools"}, "gemma": {"completion"}})
	e := newEnv(t, envOpt{})
	got := decode[struct {
		Models []string          `json:"models"`
		Tools  map[string]string `json:"tools"`
	}](t, e.ok("POST", "/api/backends/probe", map[string]any{"host": srv.URL, "api_type": "ollama"}, http.StatusOK))
	if got.Tools["good"] != "yes" || got.Tools["gemma"] != "no" || len(got.Models) != 2 {
		t.Fatalf("probe = %+v", got)
	}
}

// A host that cannot be asked leaves support unknown instead of blocking the
// save, and the start-up refresh fills it in once the host answers.
func TestH10_UnknownIsAllowedAndRefreshFillsItIn(t *testing.T) {
	caps := map[string][]string{}
	srv := fakeOllamaCaps(t, caps) // knows no model yet: /api/show answers 404
	e := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	b["models"] = []map[string]any{{"model": "late"}}
	e.ok("POST", "/api/backends", b, http.StatusCreated)
	if got := modelTools(t, e, "gpu"); got["late"] != "" {
		t.Fatalf("tools = %v, want unknown", got)
	}
	caps["late"] = []string{"completion"} // the host now says it has no tools
	e.api.RefreshCapabilities(context.Background())
	if got := modelTools(t, e, "gpu"); got["late"] != "no" {
		t.Fatalf("after refresh tools = %v, want no", got)
	}
	// A stored "no" is refused as a candidate.
	e.field("PUT", "/api/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "gpu", "model": "late"}}}, "candidates[0]")
}

// A model installed from the UI that turns out to lack tools is not added.
func TestH10_InstallRefusesAModelWithoutTools(t *testing.T) {
	srv := fakeOllamaCaps(t, map[string][]string{"keep": {"completion", "tools"}, "gemma3:27b": {"completion", "vision"}})
	e := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	b["models"] = []map[string]any{{"model": "keep"}}
	e.ok("POST", "/api/backends", b, http.StatusCreated)
	e.ok("POST", "/api/backends/gpu/install", map[string]any{"model": "gemma3:27b", "tier": 1}, http.StatusAccepted)
	waitFor(t, "install failed", func() bool { return oneInstall(t, e, "gpu").State == store.InstallFailed })
	if in := oneInstall(t, e, "gpu"); in.Error == "" || len(modelTools(t, e, "gpu")) != 1 {
		t.Fatalf("install = %+v, models %v", in, modelTools(t, e, "gpu"))
	}
}
