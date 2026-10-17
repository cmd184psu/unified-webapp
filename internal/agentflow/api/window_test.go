package api

import (
	"context"
	"net/http"
	"testing"
)

func modelView(t *testing.T, e *env, backend, model string) BackendModelView {
	t.Helper()
	for _, b := range decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, http.StatusOK)) {
		if b.Name != backend {
			continue
		}
		for _, m := range b.Models {
			if m.Model == model {
				return m
			}
		}
	}
	t.Fatalf("no model %s/%s", backend, model)
	return BackendModelView{}
}

// H15: the window a model is served with is the owner's to set; a save that
// leaves it blank keeps what the server holds (so a UI that does not know
// about it cannot wipe a value learned from a cut-off run), the same number
// keeps its source, and a different one becomes the owner's own.
func TestH15_WindowSurvivesASaveThatLeavesItBlank(t *testing.T) {
	srv := fakeOllamaCaps(t, map[string][]string{"m1": {"completion", "tools"}})
	a := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	b["models"] = []map[string]any{{"model": "m1", "context_tokens": 262144, "window_tokens": 32768}}
	a.ok("POST", "/api/backends", b, http.StatusCreated)
	if m := modelView(t, a, "gpu", "m1"); m.WindowTokens != 32768 || m.WindowSource != "set" || m.ContextTokens != 262144 {
		t.Fatalf("after create = %+v", m)
	}
	// the engine learns a smaller window from a cut-off run
	if err := a.st.SetModelWindow(context.Background(), "gpu", "m1", 16384, "observed"); err != nil {
		t.Fatal(err)
	}
	// a save that does not mention the window keeps it, and its source
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"models": []map[string]any{{"model": "m1", "context_tokens": 262144}}}, http.StatusOK)
	if m := modelView(t, a, "gpu", "m1"); m.WindowTokens != 16384 || m.WindowSource != "observed" {
		t.Fatalf("a blank window wiped the learned one: %+v", m)
	}
	// the page sends the same number back: it stays learned
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"models": []map[string]any{{"model": "m1", "window_tokens": 16384}}}, http.StatusOK)
	if m := modelView(t, a, "gpu", "m1"); m.WindowSource != "observed" {
		t.Fatalf("a round trip turned a learned window into %q", m.WindowSource)
	}
	// a different number is the owner's
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"models": []map[string]any{{"model": "m1", "window_tokens": 65536}}}, http.StatusOK)
	if m := modelView(t, a, "gpu", "m1"); m.WindowTokens != 65536 || m.WindowSource != "set" {
		t.Fatalf("the owner's window = %+v", m)
	}
}
