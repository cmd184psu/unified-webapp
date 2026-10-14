package agentflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/api"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// TestAPIWalk_EveryRouteIsExercised is the API-only equivalent of clicking
// through every tab. It drives the module the way the web UI does, over HTTP
// with the scripted fake runner (no browser), and fails if any registered
// route was never reached, or if any call fell through to the /api/ catch-all.
// A new route without a place in this walk fails the test.
func TestAPIWalk_EveryRouteIsExercised(t *testing.T) {
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })

	ref := http.NewServeMux()
	api.New(api.Deps{}).Register(ref)
	visited := map[string]bool{}
	pattern := func(method, path string) string {
		req := httptest.NewRequest(method, path, nil)
		_, p := ref.Handler(req)
		return p
	}

	repo := t.TempDir()
	e2eGit(t, repo, "init", "-q", "-b", "main")
	e2eWrite(t, filepath.Join(repo, "README"), "readme\n")
	e2eGit(t, repo, "add", "-A")
	e2eGit(t, repo, "commit", "-q", "-m", "initial")

	fake := runner.NewFake()
	fake.Script("planner", runner.Step{Reply: e2ePlan})
	fake.Script("architect", runner.Step{Reply: e2eApprove})
	fake.Script("critic", runner.Step{Reply: e2eApprove})
	fake.Script("compiler", runner.Step{Reply: e2eCompile})
	for i := 0; i < 3; i++ {
		fake.Script("worker", runner.Step{Reply: e2eWorker, Edit: func(r runner.Request) error {
			name := filepath.Base(r.Workdir)
			return os.WriteFile(filepath.Join(r.Workdir, name+".txt"), []byte("work of "+name+"\n"), 0o644)
		}})
		fake.Script("reviewer", runner.Step{Reply: e2eReview})
	}
	cfg := config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: filepath.Join(t.TempDir(), "af"), SSEMaxSubscribers: 8}
	e := newE2E(t, cfg, fake)
	srv := httptest.NewServer(e.api)
	t.Cleanup(srv.Close)

	// do sends one request and records the route it reached.
	do := func(method, path, body string) (int, []byte) {
		t.Helper()
		p := pattern(method, path)
		if p == "" || p == "/api/" {
			t.Fatalf("%s %s reaches no registered route (the UI would call something that does not exist)", method, path)
		}
		visited[p] = true
		return e.call(method, path, body)
	}
	// ok requires a status below 500 and, when want is set, exactly want.
	ok := func(method, path, body string, want int) []byte {
		t.Helper()
		code, out := do(method, path, body)
		t.Logf("%-6s %-62s -> %d %.90s", method, path, code, strings.ReplaceAll(string(out), "\n", " "))
		if code >= 500 || (want != 0 && code != want) {
			t.Fatalf("%s %s = %d, want %d: %s", method, path, code, want, out)
		}
		return out
	}

	// Health and the opencode status the page header shows.
	ok("GET", "/api/health", "", 200)
	ok("GET", "/api/opencode/version", "", 0)

	// Backends: a host to probe, add, edit, retire, enable.
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"m1"},{"name":"m2"},{"name":"m3"}]}`))
	}))
	t.Cleanup(ollama.Close)
	ok("POST", "/api/backends/probe", `{"host":`+strconv.Quote(ollama.URL)+`,"api_type":"ollama"}`, 200)
	ok("POST", "/api/backends", `{"name":"gpu","host":"http://127.0.0.1:11434","api_type":"ollama","slots":2,"models":[{"model":"m1"},{"model":"m2"},{"model":"m3"}]}`, 201)
	ok("POST", "/api/backends", `{"name":"spare","host":"http://127.0.0.1:11435","api_type":"ollama","slots":1,"models":[{"model":"m1"}]}`, 201)
	ok("GET", "/api/backends", "", 200)
	ok("PATCH", "/api/backends/spare", `{"slots":2}`, 200)
	ok("POST", "/api/backends/spare/retire", `{"cancel_running":false}`, 200)
	ok("POST", "/api/backends/spare/enable", ``, 200)

	// Settings, candidates, statistics.
	ok("GET", "/api/settings", "", 200)
	ok("PUT", "/api/settings", `{"max_attempts":3,"plan_max_rounds":3}`, 200)
	for role, m := range map[string]string{"planner": "m1", "architect": "m2", "critic": "m3", "compiler": "m1", "worker": "m1", "reviewer": "m2"} {
		ok("PUT", "/api/candidates/"+role, `{"candidates":[{"backend":"gpu","model":"`+m+`"}]}`, 200)
	}
	ok("GET", "/api/candidates", "", 200)
	ok("GET", "/api/stats/models", "", 200)
	ok("GET", "/api/yolo/invariants", "", 200)

	// Projects: git-init, create, list, get, patch, limits, project candidates.
	fresh := t.TempDir()
	ok("POST", "/api/projects/git-init", `{"repo_path":`+strconv.Quote(fresh)+`,"confirm":true}`, 0)
	ok("POST", "/api/projects", `{"slug":"demo","repo_path":`+strconv.Quote(repo)+`}`, 201)
	ok("POST", "/api/projects", `{"slug":"other","repo_path":`+strconv.Quote(fresh)+`}`, 0)
	ok("GET", "/api/projects", "", 200)
	ok("GET", "/api/projects/demo", "", 200)
	ok("PATCH", "/api/projects/demo", `{"cleanup_enabled":false}`, 200)
	ok("GET", "/api/projects/demo/limits", "", 200)
	ok("PUT", "/api/projects/demo/limits", `{"limits":{"max_attempts":"4"}}`, 200)
	ok("PUT", "/api/projects/demo/candidates/worker", `{"candidates":[{"backend":"gpu","model":"m1"}]}`, 200)
	ok("DELETE", "/api/projects/demo/candidates/worker", "", 204)

	// Effort: FRD, plan, queue, tasks.
	var ef struct{ ID int64 }
	if err := json.Unmarshal(ok("POST", "/api/projects/demo/efforts", `{"title":"walk"}`, 201), &ef); err != nil {
		t.Fatal(err)
	}
	base := "/api/projects/demo/efforts/" + strconv.FormatInt(ef.ID, 10)
	id := strconv.FormatInt(ef.ID, 10)
	ok("GET", "/api/projects/demo/efforts", "", 200)
	ok("GET", base, "", 200)
	ok("PATCH", base, `{"deliberate":false}`, 200)
	ok("PUT", base+"/frd", `{"id":"FRD-W","title":"walk","objective":"o","requirements":[{"id":"R1","description":"d"}],"acceptance_criteria":[{"id":"AC1","criterion":"c"}]}`, 200)
	ok("GET", base+"/frd", "", 200)
	ok("POST", base+"/frd/approve", ``, 200)
	ok("GET", base+"/frd/revisions", "", 200)
	ok("GET", base+"/frd/revisions/1", "", 200)
	ok("GET", "/api/efforts/"+id+"/frd/export", "", 200)
	ok("POST", base+"/plan/generate", ``, 202)
	e.waitEffort("demo", ef.ID, model.EffortPlanReady)
	ok("GET", base+"/plan", "", 200)
	ok("GET", base+"/plan/rounds", "", 200)
	ok("GET", base+"/plan/revisions", "", 200)
	ok("GET", base+"/plan/revisions/1", "", 200)
	ok("GET", "/api/efforts/"+id+"/plan/export", "", 200)
	var pl json.RawMessage
	var planDoc struct {
		Plan json.RawMessage `json:"plan"`
	}
	if err := json.Unmarshal(ok("GET", base+"/plan", "", 200), &planDoc); err == nil && len(planDoc.Plan) > 0 {
		pl = planDoc.Plan
	}
	if pl != nil {
		ok("PUT", base+"/plan", string(pl), 0)
	} else {
		ok("PUT", base+"/plan", e2ePlan, 0)
	}
	ok("POST", base+"/plan/approve", ``, 0)
	ok("POST", base+"/compile", ``, 202)
	e.waitEffort("demo", ef.ID, model.EffortQueueDraft)
	ok("GET", base+"/tasks", "", 200)
	var tk struct {
		Key          string   `json:"key"`
		Phase        string   `json:"phase"`
		Title        string   `json:"title"`
		Prompt       string   `json:"prompt"`
		Acceptance   []string `json:"acceptance"`
		FilesTouched []string `json:"files_touched"`
		DependsOn    []string `json:"depends_on"`
	}
	if err := json.Unmarshal(ok("GET", base+"/tasks/T001", "", 200), &tk); err != nil {
		t.Fatal(err)
	}
	edit, _ := json.Marshal(map[string]any{"key": tk.Key, "phase": tk.Phase, "title": tk.Title, "prompt": tk.Prompt,
		"acceptance": tk.Acceptance, "files_touched": tk.FilesTouched, "depends_on": tk.DependsOn})
	ok("PUT", base+"/tasks/T001", string(edit), 200)
	ok("POST", base+"/tasks/T001/retry", `{"clean":false}`, 409) // not halted
	ok("POST", base+"/tasks/T001/override", `{"reason":"walk"}`, 409) // not halted
	var q struct{ Hash string }
	if err := json.Unmarshal(ok("GET", base+"/queue", "", 200), &q); err != nil || q.Hash == "" {
		t.Fatalf("queue: %v", err)
	}
	ok("POST", base+"/yolo", `{"on":false}`, 0)
	ok("POST", base+"/queue/approve", `{"hash":`+strconv.Quote(q.Hash)+`}`, 200)

	// Live streams: the event stream and one stage's output.
	for _, path := range []string{"/api/events", "/api/stages/1/stream"} {
		visited[pattern("GET", path)] = true
		ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
		cancel()
	}

	// Run controls.
	ok("POST", "/api/projects/demo/run/pause", ``, 409) // not running
	ok("POST", "/api/projects/demo/run/stop", ``, 0)
	ok("POST", "/api/projects/demo/run/start", ``, 200)
	e.waitEffort("demo", ef.ID, model.EffortComplete)
	ok("POST", "/api/projects/demo/run/run-one", ``, 409) // nothing left to run
	ok("GET", base+"/git-commands", "", 200)
	ok("GET", "/api/efforts/"+id+"/export", "", 200)

	// A second effort: import an FRD, then abandon it.
	imp := ok("POST", "/api/projects/demo/efforts/import", `{"id":"FRD-I","title":"imported","objective":"o","requirements":[{"id":"R1","description":"d"}],"acceptance_criteria":[{"id":"AC1","criterion":"c"}]}`, 0)
	var impEf struct{ ID int64 }
	if json.Unmarshal(imp, &impEf) == nil && impEf.ID != 0 {
		ok("POST", "/api/projects/demo/efforts/"+strconv.FormatInt(impEf.ID, 10)+"/abandon", `{"confirm":true}`, 0)
	} else {
		ok("POST", base+"/abandon", `{"confirm":true}`, 0)
	}
	ok("DELETE", "/api/projects/other", "", 0)

	var missing []string
	for _, p := range api.Patterns() {
		if !visited[p] {
			missing = append(missing, p)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d route(s) were never exercised by the API walk:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
}
