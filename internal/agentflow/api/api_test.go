package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// env is a real store, taskmaster and engine in temp dirs with the fake
// runner, behind the API's own mux.
type env struct {
	t       *testing.T
	st      *store.Store
	tm      *taskmaster.Engine
	eng     *engine.Engine
	api     *API
	mux     *http.ServeMux
	fake    *runner.Fake
	dataDir string
}

type envOpt struct {
	maxSubs   int
	widthZero bool
	// fastPoll makes taskmaster's worker poll every 20 ms instead of 5 s.
	fastPoll bool
}

func newEnv(t *testing.T, opt envOpt) *env {
	t.Helper()
	if opt.fastPoll {
		worker.SetPollIntervalForTest(20 * time.Millisecond)
		t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	}
	e := &env{t: t, dataDir: t.TempDir(), fake: runner.NewFake()}
	cfg := config.TaskmasterConfig{StaticDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "taskmaster.db"), ProgressIntervalMs: 50}
	if err := os.WriteFile(filepath.Join(cfg.StaticDir, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	if e.tm, err = taskmaster.Open(cfg, taskmaster.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	if e.st, err = store.Open(filepath.Join(e.dataDir, "agentflow.db")); err != nil {
		t.Fatal(err)
	}
	git, err := gitsvc.New(e.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	var api *API
	if e.eng, err = engine.New(engine.Deps{
		Store: e.st, Host: e.tm, Runner: e.fake, DataDir: e.dataDir, TestWidthZero: opt.widthZero,
		PublishRun: func(ev engine.RunEvent) {
			if api != nil {
				api.PublishRun(ev)
			}
		},
		Kick: func() {
			if api != nil {
				api.Notify()
			}
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.api = New(Deps{Store: e.st, Engine: e.eng, Git: git, Version: e.fake.Version, SSEMaxSubscribers: opt.maxSubs})
	api = e.api
	e.mux = http.NewServeMux()
	e.api.Register(e.mux)
	t.Cleanup(func() {
		e.eng.Close()
		e.tm.Close()
		e.st.Close()
	})
	return e
}

// call sends a JSON request as principal p (nil: no principal) and returns the
// status and body.
func (e *env) call(p *auth.Principal, method, path string, body any) (int, []byte) {
	e.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if p != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), *p))
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// ok calls and requires the wanted status.
func (e *env) ok(method, path string, body any, want int) []byte {
	e.t.Helper()
	code, out := e.call(nil, method, path, body)
	if code != want {
		e.t.Fatalf("%s %s = %d, want %d: %s", method, path, code, want, out)
	}
	return out
}

// field requires a 400 that names field.
func (e *env) field(method, path string, body any, field string) {
	e.t.Helper()
	code, out := e.call(nil, method, path, body)
	var eb errorBody
	_ = json.Unmarshal(out, &eb)
	if code != http.StatusBadRequest || eb.Field != field {
		e.t.Fatalf("%s %s = %d %s, want 400 naming field %q", method, path, code, out, field)
	}
}

func decode[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

// repo makes a directory that passes the repo_path checks (it has a .git).
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func backendReq(name string) map[string]any {
	return map[string]any{"name": name, "host": "http://127.0.0.1:11434", "api_type": "ollama", "slots": 2,
		"models": []map[string]any{{"model": "m1"}, {"model": "m2", "paid": true}}}
}

func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func newRequest(method, path string) *http.Request { return httptest.NewRequest(method, path, nil) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
