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

// H20: a backend's duty cap is set with a PATCH that names nothing else, is
// shown as 100 when there is none, and a value outside 10..100 is refused.
func TestH20_DutyCapIsSetByAPatchAndShownBack(t *testing.T) {
	srv := fakeOllamaCaps(t, map[string][]string{"m1": {"completion", "tools"}})
	a := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	b["models"] = []map[string]any{{"model": "m1"}}
	a.ok("POST", "/api/backends", b, http.StatusCreated)
	duty := func() int {
		for _, v := range decode[[]BackendView](t, a.ok("GET", "/api/backends", nil, http.StatusOK)) {
			if v.Name == "gpu" {
				return v.DutyPercent
			}
		}
		t.Fatal("no backend gpu")
		return 0
	}
	if got := duty(); got != 100 {
		t.Fatalf("a new backend's duty = %d, want 100", got)
	}
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"duty_percent": 50}, http.StatusOK)
	if got := duty(); got != 50 {
		t.Fatalf("duty = %d, want 50", got)
	}
	a.field("PATCH", "/api/backends/gpu", map[string]any{"duty_percent": 5}, "duty_percent")
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"slots": 2}, http.StatusOK)
	if got := duty(); got != 50 {
		t.Fatalf("a save that did not mention the cap changed it to %d", got)
	}
	a.ok("PATCH", "/api/backends/gpu", map[string]any{"duty_percent": 100}, http.StatusOK)
	if got := duty(); got != 100 {
		t.Fatalf("duty = %d after clearing, want 100", got)
	}
}
