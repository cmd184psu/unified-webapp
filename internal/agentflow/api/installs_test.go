package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// fakeOllama serves the three routes an install touches. The pull streams
// progress for one layer, then either success or the host's own error.
func fakeOllama(t *testing.T, lines []string, hold <-chan struct{}) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/pull":
			w.WriteHeader(http.StatusOK)
			for _, l := range lines {
				fmt.Fprintln(w, l)
				w.(http.Flusher).Flush()
			}
			if hold != nil {
				select {
				case <-hold:
				case <-r.Context().Done():
				}
			}
		case "/api/show":
			_, _ = w.Write([]byte(`{"model_info":{"llama.context_length":32768}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func oneInstall(t *testing.T, e *env, backend string) store.Install {
	t.Helper()
	list := decode[[]store.Install](t, e.ok("GET", "/api/backends/"+backend+"/installs", nil, http.StatusOK))
	if len(list) == 0 {
		t.Fatal("no installs")
	}
	return list[0]
}

// H6: an install pulls through the host's API, tracks progress, adds the model
// to the backend with the chosen tier and its context window, and reports the
// host's own error when the pull fails.
func TestH6_InstallPullsAndAddsTheModel(t *testing.T) {
	srv := fakeOllama(t, []string{
		`{"status":"pulling manifest"}`,
		`{"status":"pulling abc123","total":1000,"completed":400}`,
		`{"status":"pulling abc123","total":1000,"completed":1000}`,
		`{"status":"verifying sha256 digest"}`,
		`{"status":"success"}`,
	}, nil)
	e := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	e.ok("POST", "/api/backends", b, http.StatusCreated)

	in := decode[store.Install](t, e.ok("POST", "/api/backends/gpu/install", map[string]any{"model": "devstral:24b", "tier": 1}, http.StatusAccepted))
	if in.State != store.InstallRunning || in.Model != "devstral:24b" {
		t.Fatalf("install = %+v", in)
	}
	waitFor(t, "install done", func() bool { return oneInstall(t, e, "gpu").State == store.InstallDone })
	bs := decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, http.StatusOK))
	var added *BackendModelView
	for _, bk := range bs {
		for i, m := range bk.Models {
			if bk.Name == "gpu" && m.Model == "devstral:24b" {
				added = &bk.Models[i]
			}
		}
	}
	if added == nil || added.Tier != 1 || added.ContextTokens != 32768 {
		t.Fatalf("the model was not added as chosen: %+v", added)
	}
	if last := oneInstall(t, e, "gpu"); last.Status != "success" || last.Finished == 0 || last.Error != "" {
		t.Fatalf("finished install = %+v", last)
	}
}

func TestH6_InstallShowsTheHostsError(t *testing.T) {
	srv := fakeOllama(t, []string{`{"status":"pulling manifest"}`, `{"error":"pull model manifest: file does not exist"}`}, nil)
	e := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	e.ok("POST", "/api/backends", b, http.StatusCreated)
	e.ok("POST", "/api/backends/gpu/install", map[string]any{"model": "seed-oss:36b"}, http.StatusAccepted)
	waitFor(t, "install failed", func() bool { return oneInstall(t, e, "gpu").State == store.InstallFailed })
	if in := oneInstall(t, e, "gpu"); in.Error != "pull model manifest: file does not exist" {
		t.Fatalf("error = %q", in.Error)
	}
}

func TestH6_InstallRejectsBadRequestsAndCancels(t *testing.T) {
	hold := make(chan struct{})
	srv := fakeOllama(t, []string{`{"status":"pulling abc","total":10,"completed":1}`}, hold)
	defer close(hold)
	e := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	e.ok("POST", "/api/backends", b, http.StatusCreated)
	e.field("POST", "/api/backends/gpu/install", map[string]any{"model": ""}, "model")
	e.field("POST", "/api/backends/gpu/install", map[string]any{"model": "x", "tier": 9}, "tier")
	e.ok("POST", "/api/backends/ghost/install", map[string]any{"model": "x"}, http.StatusNotFound)
	in := decode[store.Install](t, e.ok("POST", "/api/backends/gpu/install", map[string]any{"model": "slow"}, http.StatusAccepted))
	waitFor(t, "progress", func() bool { return oneInstall(t, e, "gpu").Completed == 1 })
	e.ok("POST", "/api/backends/gpu/install", map[string]any{"model": "slow"}, http.StatusConflict)
	e.ok("POST", fmt.Sprintf("/api/installs/%d/cancel", in.ID), nil, http.StatusNoContent)
	waitFor(t, "cancelled", func() bool { return oneInstall(t, e, "gpu").State == store.InstallCancelled })
	e.ok("POST", fmt.Sprintf("/api/installs/%d/cancel", in.ID), nil, http.StatusConflict)
}
