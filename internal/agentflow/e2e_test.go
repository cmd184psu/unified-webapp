package agentflow

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

const (
	e2ePlan    = `{"principles":["p"],"drivers":["d"],"options":[{"name":"A","description":"a"},{"name":"B","description":"b"}],"phases":[{"id":"P1","name":"n","tasks_description":"t"}],"adr":{"decision":"d","drivers":["x"],"alternatives":["y"],"why":"w","consequences":["c"],"followups":["f"]}}`
	e2eApprove = `{"verdict":"approve","issues":[]}`
	e2eRevise  = `{"verdict":"revise","summary":"needs a verification phase","issues":[{"description":"no phase verifies R1","severity":"major"}]}`
	e2eCompile = `{"tasks":[` +
		`{"id":"T001","phase":"P1","title":"first","prompt":"create T001.txt","acceptance":["R1: T001.txt exists"],"files_touched":["T001.txt"]},` +
		`{"id":"T002","phase":"P1","title":"second","prompt":"create T002.txt","acceptance":["R1: T002.txt exists"],"files_touched":["T002.txt"],"depends_on":["T001"]},` +
		`{"id":"T003","phase":"P1","title":"third","prompt":"create T003.txt","acceptance":["R1: T003.txt exists"],"files_touched":["T003.txt"]}]}`
	e2eWorker = `{"summary":"did it","files_changed":[],"learnings":[]}`
	e2eReview = `{"verdict":"pass","summary":"fine","issues":[],"repair_prompt":""}`
	// e2eXSS is an FRD title that must stay literal text everywhere.
	e2eXSS = `<img src=x onerror=alert(1)>`
)

// e2e drives the module through HTTP only, as the signed-in principal alice.
type e2e struct {
	t    *testing.T
	h    http.Handler // the Build result (an io.Closer)
	api  http.Handler // h with a principal attached to every request
	host http.Handler // the taskmaster host's own handler
}

func newE2E(t *testing.T, cfg config.AgentflowConfig, fake runner.Runner) *e2e {
	t.Helper()
	tm := openHost(t)
	h, err := buildWith(cfg, tm, fake)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { _ = h.(io.Closer).Close() })
	return &e2e{t: t, h: h, host: tm.Handler(), api: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{Method: "ldap", Subject: "alice"})))
	})}
}

// call sends one JSON request and returns status and body.
func (e *e2e) call(method, path, body string) (int, []byte) {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.api.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec.Code, rec.Body.Bytes()
}

// must calls and requires the wanted status.
func (e *e2e) must(method, path, body string, want int) []byte {
	e.t.Helper()
	code, out := e.call(method, path, body)
	if code != want {
		e.t.Fatalf("%s %s = %d, want %d: %s", method, path, code, want, out)
	}
	return out
}

func (e *e2e) effortStatus(slug string, id int64) string {
	e.t.Helper()
	var ef struct{ Status string }
	if err := json.Unmarshal(e.must("GET", "/api/projects/"+slug+"/efforts/"+strconv.FormatInt(id, 10), "", 200), &ef); err != nil {
		e.t.Fatal(err)
	}
	return ef.Status
}

func (e *e2e) waitEffort(slug string, id int64, want string) {
	e.t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for e.effortStatus(slug, id) != want {
		if time.Now().After(deadline) {
			e.t.Fatalf("effort %d never reached %s (now %s)", id, want, e.effortStatus(slug, id))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// e2eGit runs git with no user or system configuration.
func e2eGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=u", "-c", "user.email=u@example.com", "-C", dir}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func e2eWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// userTree snapshots everything the user could notice about their repository.
func userTree(t *testing.T, repo string) string {
	t.Helper()
	var b strings.Builder
	for _, args := range [][]string{
		{"rev-parse", "HEAD"}, {"symbolic-ref", "HEAD"}, {"status", "--porcelain=v1", "-uall"},
		{"diff", "HEAD"}, {"ls-files", "--stage"}, {"stash", "list"}, {"rev-parse", "refs/heads/main"},
	} {
		b.WriteString(e2eGit(t, repo, args...) + "\n--\n")
	}
	for _, f := range []string{"README", "scratch.dat", "staged.dat"} {
		data, err := os.ReadFile(filepath.Join(repo, f))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(string(data) + "\n--\n")
	}
	return b.String()
}

// sseCollector reads /api/events and keeps every message's data line.
type sseCollector struct {
	mu   sync.Mutex
	data []string
}

func (c *sseCollector) count(sub string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, d := range c.data {
		if strings.Contains(d, sub) {
			n++
		}
	}
	return n
}

func subscribe(t *testing.T, url string) *sseCollector {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", url+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatalf("SSE connect: %v", err)
	}
	c := &sseCollector{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				c.mu.Lock()
				c.data = append(c.data, d)
				c.mu.Unlock()
			}
		}
	}()
	t.Cleanup(func() { cancel(); resp.Body.Close(); <-done })
	return c
}

// AC-PX1, AC-ST2, AC-DBS1, AC-RC1, AC-DBS2 (restart half), AC-UI1 (API level):
// the whole journey through HTTP. A real taskmaster host, a real
// agentflow store, real git and the real bwrap feedback sandbox; only the
// agent is the scripted fake runner. The user's repository has a dirty main
// tree throughout.
func TestE2E_FullJourneyOverHTTP(t *testing.T) {
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })

	repo := t.TempDir()
	e2eGit(t, repo, "init", "-q", "-b", "main")
	e2eWrite(t, filepath.Join(repo, "README"), "readme\n")
	// The feedback command runs in bwrap, where /home holds only the sandbox user.
	e2eWrite(t, filepath.Join(repo, "check.sh"), "test \"$(ls /home)\" = agent || { echo host home is visible; exit 1; }\n")
	e2eGit(t, repo, "add", "-A")
	e2eGit(t, repo, "commit", "-q", "-m", "initial")
	e2eWrite(t, filepath.Join(repo, "README"), "readme, edited and uncommitted\n")
	e2eWrite(t, filepath.Join(repo, "scratch.dat"), "untracked\n")
	e2eWrite(t, filepath.Join(repo, "staged.dat"), "staged\n")
	e2eGit(t, repo, "add", "staged.dat")
	before := userTree(t, repo)

	fake := runner.NewFake()
	fake.Script("planner", runner.Step{Reply: e2ePlan}, runner.Step{Reply: e2ePlan})
	fake.Script("architect", runner.Step{Reply: e2eApprove}, runner.Step{Reply: e2eApprove})
	fake.Script("critic", runner.Step{Reply: e2eRevise}, runner.Step{Reply: e2eApprove})
	fake.Script("compiler", runner.Step{Reply: e2eCompile})
	for i := 0; i < 3; i++ {
		fake.Script("worker", runner.Step{Reply: e2eWorker, Edit: func(r runner.Request) error {
			name := filepath.Base(r.Workdir)
			return os.WriteFile(filepath.Join(r.Workdir, name+".txt"), []byte("work of "+name+"\n"), 0o644)
		}})
		fake.Script("reviewer", runner.Step{Reply: e2eReview})
	}

	dataDir := filepath.Join(t.TempDir(), "af")
	cfg := config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: dataDir, SSEMaxSubscribers: 4}
	e := newE2E(t, cfg, fake)
	srv := httptest.NewServer(e.api)
	t.Cleanup(srv.Close)
	events := subscribe(t, srv.URL)

	// Backend, candidates, settings.
	e.must("POST", "/api/backends", `{"name":"gpu","host":"http://127.0.0.1:11434","api_type":"ollama","slots":2,"models":[{"model":"m1"},{"model":"m2"},{"model":"m3"}]}`, 201)
	for role, m := range map[string]string{"planner": "m1", "architect": "m2", "critic": "m3", "compiler": "m1", "worker": "m1", "reviewer": "m2"} {
		e.must("PUT", "/api/candidates/"+role, `{"candidates":[{"backend":"gpu","model":"`+m+`"}]}`, 200)
	}
	e.must("PUT", "/api/settings", `{"max_attempts":3,"plan_max_rounds":3}`, 200)

	// Project on the temp repo, effort, FRD.
	e.must("POST", "/api/projects", `{"slug":"demo","repo_path":`+strconv.Quote(repo)+`,"feedback_cmds":["sh check.sh"]}`, 201)
	var ef struct{ ID int64 }
	if err := json.Unmarshal(e.must("POST", "/api/projects/demo/efforts", `{"title":"journey"}`, 201), &ef); err != nil {
		t.Fatal(err)
	}
	base := "/api/projects/demo/efforts/" + strconv.FormatInt(ef.ID, 10)
	frd := `{"id":"FRD-E2E","title":` + strconv.Quote(e2eXSS) + `,"objective":"o","requirements":[{"id":"R1","description":"d"}]}`
	e.must("PUT", base+"/frd", frd, 200)
	// UI1 at the API level: the payload is data in a JSON response, never HTML.
	rec := httptest.NewRecorder()
	e.api.ServeHTTP(rec, httptest.NewRequest("GET", base+"/frd", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") || !strings.Contains(rec.Body.String(), `"\u003cimg src=x onerror=alert(1)\u003e"`) {
		t.Errorf("FRD response: content-type %q, body %s", ct, rec.Body)
	}
	e.must("POST", base+"/frd/approve", ``, 200)

	// Plan: the critic revises once, then approves; a human approves.
	e.must("POST", base+"/plan/generate", ``, 202)
	e.waitEffort("demo", ef.ID, model.EffortPlanReady)
	if n := len(fake.CallsFor("planner")); n != 2 {
		t.Errorf("planner calls = %d, want 2 (one revision round)", n)
	}
	if code, _ := e.call("GET", base+"/git-commands", ""); code != http.StatusConflict {
		t.Errorf("git-commands before completion = %d, want 409", code)
	}
	e.must("POST", base+"/plan/approve", ``, 200)

	// Compile and approve the queue by hash.
	e.must("POST", base+"/compile", ``, 202)
	e.waitEffort("demo", ef.ID, model.EffortQueueDraft)
	var q struct {
		Hash  string
		Tasks []struct {
			Key       string
			DependsOn []string `json:"depends_on"`
		}
	}
	if err := json.Unmarshal(e.must("GET", base+"/queue", "", 200), &q); err != nil || len(q.Tasks) != 3 || q.Hash == "" {
		t.Fatalf("queue = %+v (%v)", q, err)
	}
	e.must("POST", base+"/queue/approve", `{"hash":`+strconv.Quote(q.Hash)+`}`, 200)
	var pj struct {
		RunState string `json:"run_state"`
	}
	if err := json.Unmarshal(e.must("GET", "/api/projects/demo", "", 200), &pj); err != nil || pj.RunState != model.RunStopped {
		t.Fatalf("project after approval = %s (%v): nothing may run until started (RC1)", pj.RunState, err)
	}
	if n := len(fake.CallsFor("worker")); n != 0 {
		t.Fatalf("%d worker stages ran before run/start", n)
	}

	// Run to completion.
	e.must("POST", "/api/projects/demo/run/start", ``, 200)
	e.waitEffort("demo", ef.ID, model.EffortComplete)

	// AC-PX1 and AC-ST2: the user's branch, tree, index and stash are untouched.
	if after := userTree(t, repo); after != before {
		t.Fatalf("user repo changed (AC-PX1/AC-ST2):\n--- before\n%s\n--- after\n%s", before, after)
	}
	branch := "agentflow/effort-" + strconv.FormatInt(ef.ID, 10)
	files := e2eGit(t, repo, "ls-tree", "-r", "--name-only", branch)
	for _, f := range []string{"T001.txt", "T002.txt", "T003.txt", "README", "check.sh"} {
		if !strings.Contains(files, f) {
			t.Errorf("%s missing from %s:\n%s", f, branch, files)
		}
	}
	if strings.Contains(files, ".dat") {
		t.Errorf("uncommitted user files leaked into %s:\n%s", branch, files)
	}
	if got := e2eGit(t, repo, "show", branch+":README"); got != "readme" {
		t.Errorf("integration README = %q: the dirty user tree leaked into the branch", got)
	}
	if heads := e2eGit(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads"); heads != branch+"\nmain" {
		t.Errorf("branches = %q, want only main and %s", heads, branch)
	}
	if wts := e2eGit(t, repo, "worktree", "list", "--porcelain"); strings.Count(wts, "worktree ") != 1 {
		t.Errorf("task worktrees remain:\n%s", wts)
	}
	if left, _ := os.ReadDir(filepath.Join(dataDir, "worktrees", "demo", "effort-"+strconv.FormatInt(ef.ID, 10))); len(left) != 0 {
		t.Errorf("worktree directories remain: %v", left)
	}
	// AC-DBS1/AC-ST2: no state file of any kind was written into the repository.
	entries, _ := os.ReadDir(repo)
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	if got := strings.Join(names, " "); got != ".git README check.sh scratch.dat staged.dat" {
		t.Errorf("repository entries = %q", got)
	}

	// Push instructions are exposed now (and were a 409 before).
	var gc struct{ Commands []string }
	if err := json.Unmarshal(e.must("GET", base+"/git-commands", "", 200), &gc); err != nil || !strings.Contains(strings.Join(gc.Commands, "\n"), branch) {
		t.Errorf("git-commands = %+v (%v)", gc, err)
	}

	// Every task passed, with the worker and reviewer on different models.
	for _, key := range []string{"T001", "T002", "T003"} {
		var tk struct{ Status string }
		if err := json.Unmarshal(e.must("GET", base+"/tasks/"+key, "", 200), &tk); err != nil || tk.Status != string(model.StatusPassed) {
			t.Errorf("task %s = %+v (%v)", key, tk, err)
		}
	}
	for _, c := range fake.CallsFor("worker") {
		if c.Request.Model == "m2" {
			t.Errorf("worker ran on the reviewer's model")
		}
	}

	// SSE delivered run-state events.
	waitUntil(t, "SSE run-state events", func() bool {
		return events.count(`"type":"project.run_state"`) > 0 && events.count(`"type":"snapshot"`) > 0
	})

	// The host shows the agentflow lanes (TM).
	rec = httptest.NewRecorder()
	e.host.ServeHTTP(rec, httptest.NewRequest("GET", "/api/lanes", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "agentflow-gpu") || !strings.Contains(rec.Body.String(), "agentflow_verify") {
		t.Errorf("GET /api/lanes = %d %s", rec.Code, rec.Body)
	}

	// Close the module and read the audit trail from its database.
	if err := e.h.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	evs, err := st.ListEvents(context.Background(), "", "", 100000)
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	actors := map[string]string{}
	for _, ev := range evs {
		actors[ev.Action] = actors[ev.Action] + "," + ev.Actor
	}
	for _, action := range []string{"backend.create", "candidates.set", "settings.update", "project.create", "effort.create", "doc.approve", "plan.start", "compile.start", "queue.approve", "run.start"} {
		if !strings.Contains(actors[action], "alice") {
			t.Errorf("audit action %q actors = %q, want alice (actions seen: %v)", action, actors[action], actors)
		}
	}

	// AC-RC1/AC-DBS2: a rebuild on the same data_dir boots the project stopped
	// and dispatches nothing.
	fake2 := runner.NewFake()
	again := newE2E(t, cfg, fake2)
	if err := json.Unmarshal(again.must("GET", "/api/projects/demo", "", 200), &pj); err != nil || pj.RunState != model.RunStopped {
		t.Errorf("project after restart = %q (%v), want stopped", pj.RunState, err)
	}
	if got := again.effortStatus("demo", ef.ID); got != model.EffortComplete {
		t.Errorf("effort after restart = %s, want complete", got)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(fake2.Calls()); n != 0 {
		t.Errorf("%d runner calls after restart, want none", n)
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// AC-YO1 (HTTP): with YOLO on, one plan/generate call takes an effort from the
// saved FRD to a completed run with no further request. Every gate is audited
// with the actor yolo, and the effort branch holds all three tasks.
func TestE2E_YOLOJourneyNeedsNoGateClicks(t *testing.T) {
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })

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
	dataDir := filepath.Join(t.TempDir(), "af")
	e := newE2E(t, config.AgentflowConfig{StaticDir: t.TempDir(), DataDir: dataDir}, fake)

	e.must("POST", "/api/backends", `{"name":"gpu","host":"http://127.0.0.1:11434","api_type":"ollama","slots":2,"models":[{"model":"m1"},{"model":"m2"},{"model":"m3"}]}`, 201)
	for role, m := range map[string]string{"planner": "m1", "architect": "m2", "critic": "m3", "compiler": "m1", "worker": "m1", "reviewer": "m2"} {
		e.must("PUT", "/api/candidates/"+role, `{"candidates":[{"backend":"gpu","model":"`+m+`"}]}`, 200)
	}
	e.must("POST", "/api/projects", `{"slug":"demo","repo_path":`+strconv.Quote(repo)+`}`, 201)
	var ef struct{ ID int64 }
	if err := json.Unmarshal(e.must("POST", "/api/projects/demo/efforts", `{"title":"yolo"}`, 201), &ef); err != nil {
		t.Fatal(err)
	}
	base := "/api/projects/demo/efforts/" + strconv.FormatInt(ef.ID, 10)
	e.must("PUT", base+"/frd", `{"id":"FRD-Y","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}]}`, 200)
	e.must("POST", base+"/yolo", `{"on":true}`, 400) // enabling needs confirmation
	e.must("POST", base+"/yolo", `{"on":true,"confirm":true}`, 200)

	e.must("POST", base+"/plan/generate", ``, 202) // the only other request
	e.waitEffort("demo", ef.ID, model.EffortComplete)

	branch := "agentflow/effort-" + strconv.FormatInt(ef.ID, 10)
	if files := e2eGit(t, repo, "ls-tree", "-r", "--name-only", branch); strings.Count(files, ".txt") != 3 {
		t.Errorf("%s holds:\n%s", branch, files)
	}
	if err := e.h.(io.Closer).Close(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	evs, err := st.ListEvents(context.Background(), "", "", 100000)
	st.Close()
	if err != nil {
		t.Fatal(err)
	}
	actors := map[string][]string{}
	for _, ev := range evs {
		actors[ev.Action] = append(actors[ev.Action], ev.Actor)
	}
	for _, action := range []string{"compile.start", "queue.approve", "run.start"} {
		if got := actors[action]; len(got) != 1 || got[0] != "yolo" {
			t.Errorf("%s actors = %v, want exactly [yolo]", action, got)
		}
	}
	if got := actors["yolo.enable"]; len(got) != 1 || got[0] != "alice" {
		t.Errorf("yolo.enable actors = %v, want [alice]", got)
	}
}
