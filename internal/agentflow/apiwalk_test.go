package agentflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/api"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// Drafter replies for the walk: a draft, a polish and a split into two parts.
const (
	walkDraft = `{"title":"walk","objective":"Walk the API.","requirements":[{"description":"Every route answers"}],"acceptance_criteria":[{"criterion":"A route answers with JSON"},{"criterion":"go test passes"}]}`
	walkSplit = `{"parts":[{"title":"first half","objective":"Do the first half.","requirements":[{"description":"First half works"}],"acceptance_criteria":[{"criterion":"First half answers"}]},` +
		`{"title":"second half","objective":"Do the second half.","requirements":[{"description":"Second half works"}],"acceptance_criteria":[{"criterion":"Second half answers"}]}]}`
)

// TestAPIWalk_EveryRouteIsExercised is the API-only equivalent of clicking
// through every tab. It drives the module the way the web UI does, over HTTP
// with the scripted fake runner (no browser), and fails if any registered
// route was never reached, or if any call fell through to the /api/ catch-all.
// A new route without a place in this walk fails the test.
func TestAPIWalk_EveryRouteIsExercised(t *testing.T) {
	var terminated atomic.Int32
	SetTerminateForTest(func() { terminated.Add(1) })
	t.Cleanup(func() { SetTerminateForTest(nil) })
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
	// The planner role also serves the FRD drafter: draft, polish and split come
	// first, then the plan.
	fake.Script("planner", runner.Step{Reply: walkDraft}, runner.Step{Reply: walkDraft}, runner.Step{Reply: walkSplit}, runner.Step{Reply: e2ePlan})
	fake.Script("architect", runner.Step{Reply: e2eApprove})
	fake.Script("critic", runner.Step{Reply: e2eApprove})
	fake.Script("compiler", runner.Step{Reply: e2eCompile}, runner.Step{Reply: e2eCompile}) // approve compiles, then a recompile
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
		if r.URL.Path == "/api/show" {
			_, _ = w.Write([]byte(`{"model_info":{"general.architecture":"x","x.context_length":8192}}`))
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"m1"},{"name":"m2"},{"name":"m3"}]}`))
	}))
	t.Cleanup(ollama.Close)
	if out := ok("POST", "/api/backends/probe", `{"host":`+strconv.Quote(ollama.URL)+`,"api_type":"ollama"}`, 200); !strings.Contains(string(out), `"m1":8192`) {
		t.Fatalf("probe answered no context size: %s", out)
	}
	ok("POST", "/api/backends", `{"name":"gpu","host":"http://127.0.0.1:11434","api_type":"ollama","slots":2,"models":[{"model":"m1","context_tokens":8192,"window_tokens":65536},{"model":"m2","context_tokens":8192,"window_tokens":65536},{"model":"m3","window_tokens":65536}]}`, 201)
	ok("POST", "/api/backends", `{"name":"spare","host":"http://127.0.0.1:11435","api_type":"ollama","slots":1,"models":[{"model":"m1","window_tokens":65536}]}`, 201)
	ok("GET", "/api/backends", "", 200)
	ok("PATCH", "/api/backends/spare", `{"slots":2}`, 200)
	ok("POST", "/api/backends/spare/extend-windows", `{}`, 400) // no memory budget set
	ok("POST", "/api/backends/spare/retire", `{"cancel_running":false}`, 200)
	ok("POST", "/api/backends/spare/install", `{"model":"walk"}`, 400) // retired: refused before any pull
	ok("GET", "/api/backends/spare/installs", "", 200)
	ok("POST", "/api/installs/999/cancel", "", 404)
	ok("GET", "/api/config/export", "", 200)
	ok("POST", "/api/config/import", `{"config":{"format":"nope","version":1},"apply":false}`, 400)
	ok("POST", "/api/backends/spare/enable", ``, 200)
	// Delete: refused while enabled, then removed once disabled and on no candidate list.
	ok("POST", "/api/backends", `{"name":"scrap","host":"http://127.0.0.1:11436","api_type":"ollama","slots":1,"models":[{"model":"m1","window_tokens":65536}]}`, 201)
	ok("DELETE", "/api/backends/scrap", "", 409)
	ok("POST", "/api/backends/scrap/retire", `{"cancel_running":false}`, 200)
	ok("DELETE", "/api/backends/scrap", "", 204)
	ok("GET", "/api/rests", "", 200)
	ok("DELETE", "/api/rests?backend=scrap&model=m1", "", 404) // nothing resting
	ok("DELETE", "/api/rests", "", 400)
	ok("GET", "/api/holds", "", 200)
	ok("PUT", "/api/holds?backend=gpu&model=m1&hold=true", "", 204)
	ok("PUT", "/api/holds?backend=gpu&model=m1&hold=maybe", "", 400)
	ok("PUT", "/api/holds?backend=gpu&model=m1&hold=false", "", 204)

	// Settings, candidates, statistics.
	ok("GET", "/api/settings", "", 200)
	ok("GET", "/api/digest", "", 200)
	ok("PUT", "/api/settings", `{"max_attempts":3,"plan_max_rounds":3,"acceptance_rounds":0}`, 200)
	ok("PUT", "/api/settings", `{"browse_roots":[`+strconv.Quote(repo)+`]}`, 200)
	ok("GET", "/api/fs/dirs", "", 200)
	ok("GET", "/api/fs/dirs?path="+url.QueryEscape(repo), "", 200)
	ok("GET", "/api/fs/dirs?path=/", "", 400) // outside the browse roots
	for role, m := range map[string]string{"planner": "m1", "architect": "m2", "critic": "m3", "compiler": "m1", "worker": "m1", "reviewer": "m2"} {
		ok("PUT", "/api/candidates/"+role, `{"candidates":[{"backend":"gpu","model":"`+m+`"}]}`, 200)
	}
	ok("GET", "/api/candidates", "", 200)
	ok("GET", "/api/stats/models", "", 200)
	ok("POST", "/api/strikes/clear", `{"backend":"gpu","model":"m1","role":"worker"}`, 204)
	ok("GET", "/api/strikes", "", 200)
	ok("GET", "/api/attention", "", 200)
	ok("GET", "/api/seats", "", 200)
	// AF-13: the coordinator module is off without coord.json: every route says so.
	ok("GET", "/api/coord", "", 404)
	ok("POST", "/api/coord/run", `{"dry":true}`, 404)
	ok("GET", "/api/seats/external", "", 404)
	ok("POST", "/api/seats/external", `{}`, 404)
	ok("PATCH", "/api/seats/external/deepseek4", `{}`, 404)
	ok("DELETE", "/api/seats/external/deepseek4", "", 404)
	ok("POST", "/api/seats/external/deepseek4/pause", `{}`, 404)
	ok("GET", "/api/seats/deepseek4/queue", "", 404)
	ok("POST", "/api/seats/deepseek4/nudge", "{}", 404)
	ok("POST", "/api/handoffs/TASK-1/merge", `{"dry":true}`, 404)
	ok("POST", "/api/seats/999999/answer", `{"text":"go on"}`, 404)
	ok("POST", "/api/seats/999999/cancel", "", 404)
	// Seat tasks (AF-21): create one, list it, patch its state, import a batch.
	var walkSeat struct{ ID int64 }
	if err := json.Unmarshal(ok("POST", "/api/seat-tasks", `{"title":"walk seat task","objective":"walk the seats API","priority":1}`, 201), &walkSeat); err != nil || walkSeat.ID == 0 {
		t.Fatalf("create seat task: %v", err)
	}
	if out := ok("GET", "/api/seat-tasks", "", 200); !strings.Contains(string(out), `"walk seat task"`) {
		t.Fatalf("seat tasks list: %s", out)
	}
	ok("PATCH", "/api/seat-tasks/"+strconv.FormatInt(walkSeat.ID, 10), `{"state":"fed"}`, 200)
	if out := ok("POST", "/api/seat-tasks/import", `[{"title":"imported one","objective":"o"},{"title":"imported two","objective":"o","priority":2}]`, 200); !strings.Contains(string(out), `"imported":2`) {
		t.Fatalf("seat task import: %s", out)
	}
	// Remote seats without a bearer token: the routes exist and refuse (401)
	// before the coordinator check. The ack carries a real branch@sha from the
	// scratch repo, as it would against a shared remote with coord on.
	ok("GET", "/api/seat/deepseek4/next", "", 401)
	sha := e2eGit(t, repo, "rev-parse", "HEAD")
	ok("POST", "/api/seat/deepseek4/ack", `{"task":"TASK-1","branch":"main","sha":`+strconv.Quote(sha)+`}`, 401)
	ok("POST", "/api/seat/deepseek4/blocked", `{"task":"TASK-1","question":"walk: is this blocked?"}`, 401)
	// Stage homes: nothing has completed yet, so the prune is a safe no-op.
	ok("POST", "/api/stages/prune", `{}`, 200)
	ok("GET", "/api/yolo/invariants", "", 200)

	// Projects: git-init, create, list, get, patch, limits, project candidates.
	fresh := t.TempDir()
	ok("POST", "/api/projects/git-init", `{"repo_path":`+strconv.Quote(fresh)+`,"confirm":true}`, 0)
	ok("POST", "/api/projects", `{"slug":"demo","repo_path":`+strconv.Quote(repo)+`}`, 201)
	ok("POST", "/api/projects", `{"slug":"other","repo_path":`+strconv.Quote(fresh)+`}`, 0)
	ok("GET", "/api/projects", "", 200)
	ok("GET", "/api/projects/demo", "", 200)
	ok("PATCH", "/api/projects/demo", `{"cleanup_enabled":false}`, 200)
	if out := ok("GET", "/api/projects/demo/definition-of-done", "", 200); !strings.Contains(string(out), `"source":"detected"`) {
		t.Fatalf("definition of done: %s", out)
	}
	ok("PATCH", "/api/projects/demo", `{"definition_of_done":["The changelog is updated."]}`, 200)
	if out := ok("GET", "/api/projects/demo/definition-of-done", "", 200); !strings.Contains(string(out), `"source":"project"`) {
		t.Fatalf("definition of done override: %s", out)
	}
	ok("PATCH", "/api/projects/demo", `{"definition_of_done":null}`, 200)
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
	// FRD automation: draft, polish, split into two parts, size.
	waitDraft := func() {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			var d struct{ State, Error string }
			if err := json.Unmarshal(ok("GET", base+"/frd/draft", "", 200), &d); err == nil && d.State != "running" {
				if d.State != "idle" {
					t.Fatalf("draft state %s: %s", d.State, d.Error)
				}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("draft never finished")
	}
	ok("GET", base+"/frd/draft", "", 200)
	ok("GET", base+"/size", "", 200)
	ok("POST", base+"/frd/draft", `{"description":"walk the API"}`, 202)
	waitDraft()
	ok("POST", base+"/frd/draft", `{"description":"","polish":true}`, 202)
	waitDraft()
	ok("POST", base+"/frd/split", `{"max_parts":2}`, 202)
	waitDraft()
	if out := ok("GET", base+"/frd/parts", "", 200); !strings.Contains(string(out), `"n":2`) {
		t.Fatalf("parts: %s", out)
	}
	ok("POST", base+"/frd/parts/2/start", ``, 409) // this effort is still active
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
	e.waitEffort("demo", ef.ID, model.EffortQueueDraft) // approval started the compile
	ok("POST", base+"/compile", ``, 202)                // recompile
	e.waitEffort("demo", ef.ID, model.EffortQueueDraft)
	ok("GET", base+"/tasks", "", 200)
	var tk struct {
		ID           int64    `json:"id"`
		Key          string   `json:"key"`
		Phase        string   `json:"phase"`
		Title        string   `json:"title"`
		Prompt       string   `json:"prompt"`
		Acceptance   []string `json:"acceptance"`
		FilesTouched []string `json:"files_touched"`
		DependsOn    []string `json:"depends_on"`
	}
	if err := json.Unmarshal(ok("GET", base+"/tasks/T001", "", 200), &tk); err != nil || tk.ID == 0 {
		t.Fatalf("task T001: %v id=%d", err, tk.ID)
	}
	// AF-22: add_ruling is a real route. It records a pending inbox entry.
	ok("POST", "/api/projects/demo/rulings", `{"effort":`+id+`,"task":`+strconv.FormatInt(tk.ID, 10)+`,"ruling_id":"R-WALK","proposed_text":"a walk ruling"}`, 201)
	edit, _ := json.Marshal(map[string]any{"key": tk.Key, "phase": tk.Phase, "title": tk.Title, "prompt": tk.Prompt,
		"acceptance": tk.Acceptance, "files_touched": tk.FilesTouched, "depends_on": tk.DependsOn})
	ok("PUT", base+"/tasks/T001", string(edit), 200)
	ok("PUT", base+"/tasks/T001?approve=true", string(edit), 200) // a draft queue stays a draft
	ok("POST", base+"/tasks/T001/retry", `{"clean":false}`, 409)  // not halted
	ok("POST", base+"/tasks/T001/resolution/apply", "", 409)
	ok("POST", base+"/tasks/T001/allow-files", `{}`, 409)             // not waiting for files
	ok("POST", base+"/tasks/T001/override", `{"reason":"walk"}`, 409) // not halted
	var q struct{ Hash string }
	if err := json.Unmarshal(ok("GET", base+"/queue", "", 200), &q); err != nil || q.Hash == "" {
		t.Fatalf("queue: %v", err)
	}
	ok("POST", base+"/yolo", `{"on":false}`, 0)
	ok("POST", base+"/queue/seed", `{"tasks":[]}`, 403) // not a smoke-* project
	ok("POST", base+"/queue/approve", `{"hash":`+strconv.Quote(q.Hash)+`}`, 200)

	// AF-15: park the prepared effort, list the backlog, and return it.
	ok("POST", base+"/park", `{"priority":4}`, 200)
	ok("GET", "/api/backlog", "", 200)
	ok("POST", base+"/unpark", ``, 200)

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

	// Service hold (AF-10): nothing new starts while it is set, a run/start is
	// refused, and lifting it starts again the projects it paused.
	ok("POST", "/api/service/hold", `{"reason":"walking the API"}`, 200)
	ok("POST", "/api/projects/demo/run/start", ``, 409) // the hold refuses it
	ok("POST", "/api/service/hold", `{}`, 400)          // a reason is required
	ok("GET", "/api/service/hold", "", 200)
	ok("DELETE", "/api/service/hold", "", 200)

	// Service restart: nothing is running, so the restart ends the process at once
	// (here a counter); a second request is refused and the pending one can be cancelled.
	ok("GET", "/api/service/restart", "", 200)
	ok("POST", "/api/service/restart", ``, 202)
	for deadline := time.Now().Add(10 * time.Second); terminated.Load() == 0; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the restart did not end the process")
		}
	}
	ok("POST", "/api/service/restart", ``, 409)
	ok("DELETE", "/api/service/restart", "", 200)

	// Review API (AF-14): one branch reviewed through verify and one reviewer.
	fake.Script("reviewer", runner.Step{Reply: e2eReview})
	var rv struct {
		ID      int64  `json:"id"`
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(ok("POST", "/api/reviews", `{"project":"demo","branch":"main","task":{"id":"TASK-REVIEW","title":"walk","acceptance":["it works"]}}`, 201), &rv); err != nil {
		t.Fatal(err)
	}
	if rv.ID == 0 || rv.Verdict != "PASS" {
		t.Fatalf("review = %+v", rv)
	}
	ok("GET", "/api/reviews", "", 200)
	ok("GET", "/api/reviews/"+strconv.FormatInt(rv.ID, 10), "", 200)

	// Run controls.
	ok("POST", "/api/projects/demo/run/pause", ``, 409) // not running
	ok("POST", "/api/projects/demo/run/stop", ``, 0)
	ok("POST", "/api/projects/demo/run/start", ``, 200)
	e.waitEffort("demo", ef.ID, model.EffortComplete)
	ok("POST", "/api/projects/demo/run/run-one", ``, 409) // nothing left to run
	ok("GET", base+"/git-commands", "", 200)
	var bundle []byte
	bundle = ok("GET", "/api/efforts/"+id+"/export", "", 200)
	var importedBundle struct{ ID int64 }
	if err := json.Unmarshal(ok("POST", "/api/import?project=other", string(bundle), 201), &importedBundle); err != nil || importedBundle.ID == 0 {
		t.Fatalf("bundle import: %v", err)
	}
	ok("POST", "/api/projects/other/efforts/"+strconv.FormatInt(importedBundle.ID, 10)+"/abandon", `{"confirm":true}`, 0)

	// The second half of the split FRD becomes an effort of its own.
	var part struct{ ID int64 }
	if err := json.Unmarshal(ok("POST", base+"/frd/parts/2/start", ``, 201), &part); err != nil || part.ID == 0 {
		t.Fatalf("start part: %v", err)
	}
	ok("POST", base+"/frd/parts/2/start", ``, 409) // already started
	ok("POST", "/api/projects/demo/efforts/"+strconv.FormatInt(part.ID, 10)+"/abandon", `{"confirm":true}`, 0)

	// A second effort: import an FRD, then abandon it.
	imp := ok("POST", "/api/projects/demo/efforts/import", `{"id":"FRD-I","title":"imported","objective":"o","requirements":[{"id":"R1","description":"d"}],"acceptance_criteria":[{"id":"AC1","criterion":"c"}]}`, 0)
	var impEf struct{ ID int64 }
	if json.Unmarshal(imp, &impEf) == nil && impEf.ID != 0 {
		ok("POST", "/api/projects/demo/efforts/"+strconv.FormatInt(impEf.ID, 10)+"/abandon", `{"confirm":true}`, 0)
	} else {
		ok("POST", base+"/abandon", `{"confirm":true}`, 0)
	}
	var gone struct {
		Deleted  string   `json:"deleted"`
		Branches []string `json:"branches"`
		Commands []string `json:"commands"`
	}
	if err := json.Unmarshal(ok("DELETE", "/api/projects/other", "", 200), &gone); err != nil || gone.Deleted != "other" || gone.Branches == nil || gone.Commands == nil {
		t.Errorf("DELETE project body = %+v (%v), want deleted=other and empty (non-null) branches and commands", gone, err)
	}
	ok("GET", "/api/projects/other", "", 404)
	// The check uses the critic's fake script, so it runs after planning is done.
	ok("GET", "/api/backends/gpu/check", "", 200)
	ok("GET", "/api/backends/gpu/usage", "", 200)
	ok("GET", "/api/projects/demo/preflight", "", 200)
	ok("POST", "/api/backends/gpu/check", "", 202)
	ok("POST", "/api/backends/nope/check", "", 404)
	ok("GET", "/api/backends/gpu/evals", "", 200)
	ok("POST", "/api/backends/gpu/evals", `{"cases":["nope"]}`, 400)
	ok("POST", "/api/backends/nope/evals", "", 404)
	ok("POST", "/api/backends/gpu/evals", `{"models":["m1"],"cases":["page-through-api"]}`, 202)
	ok("POST", "/api/projects/_all_", `{}`, 400)
	ok("POST", "/api/projects/_all_", `{"confirm":true}`, 200)
	ok("GET", "/api/projects/demo", "", 404)

	// AF-24 peer routes (peers disabled by default -> 404, except unpair/status).
	ok("GET", "/api/peers", "", 404)
	ok("POST", "/api/peers/invite", `{"name":"x"}`, 404)
	ok("POST", "/api/peers/accept", `{"a_url":"http://x","code":"y","name":"z"}`, 404)
	ok("GET", "/api/peers/foo/status", "", 404)
	ok("POST", "/api/peers/foo/rotate", "", 404)
	ok("DELETE", "/api/peers/foo", "", 404)
	ok("POST", "/api/peer/pair", `{"code":"x","base_url":"http://y","key":"z"}`, 404)
	ok("POST", "/api/peer/unpair", `{}`, 404)
	ok("POST", "/api/peer/rotate", `{}`, 404)
	ok("GET", "/api/peer/status", "", 401)

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
