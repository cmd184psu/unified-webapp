package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

const (
	frdBody = `{"id":"FRD-T","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}],"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"c"}]}`
	// The replies the planning loop, compiler and worker expect.
	planReply    = `{"principles":["p"],"drivers":["d"],"options":[{"name":"A","description":"a"},{"name":"B","description":"b"}],"phases":[{"id":"P1","name":"n","tasks_description":"t"}],"adr":{"decision":"d","drivers":["x"],"alternatives":["y"],"why":"w","consequences":["c"],"followups":["f"]}}`
	approveReply = `{"verdict":"approve","issues":[]}`
	compileReply = `{"tasks":[` +
		`{"id":"T001","phase":"P1","title":"first","prompt":"create a.txt","acceptance":["R1: a.txt exists"],"files_touched":["a.txt"]},` +
		`{"id":"T002","phase":"P1","title":"second","prompt":"create b.txt","acceptance":["R1: b.txt exists","AC1: it exists"],"files_touched":["b.txt"],"depends_on":["T001"]}]}`
	workerReply = `{"summary":"did it","files_changed":["a.go"],"learnings":[]}`
)

// sse is one open /api/events connection; frames arrive decoded on events.
type sse struct {
	events chan map[string]any
	cancel context.CancelFunc
}

// dialSSE connects to url. It returns the response status and, on 200, the
// connection with the snapshot already received.
func dialSSE(t *testing.T, url string) (int, *sse) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return resp.StatusCode, nil
	}
	c := &sse{events: make(chan map[string]any, 64), cancel: cancel}
	t.Cleanup(cancel)
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var m map[string]any
				if json.Unmarshal([]byte(data), &m) == nil {
					c.events <- m
				}
			}
		}
	}()
	if first := c.next(t, func(m map[string]any) bool { return true }); first["type"] != "snapshot" {
		t.Fatalf("first frame = %v, want a snapshot", first)
	}
	return resp.StatusCode, c
}

// next returns the first frame satisfying pred, within 15 s.
func (c *sse) next(t *testing.T, pred func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.After(15 * time.Second)
	for {
		select {
		case m := <-c.events:
			if pred(m) {
				return m
			}
		case <-deadline:
			t.Fatal("timed out waiting for an event")
		}
	}
}

// flow is a started environment with a pool, a project and an effort holding
// an FRD, driven only through HTTP.
type flow struct {
	*env
	srv    *httptest.Server
	base   string // /api/projects/p1/efforts/<id>
	effort int64
}

func newFlow(t *testing.T, opt envOpt) *flow {
	t.Helper()
	opt.fastPoll = true
	e := newEnv(t, opt)
	f := &flow{env: e, srv: httptest.NewServer(e.mux)}
	t.Cleanup(f.srv.Close)
	b := backendReq("gpu")
	b["models"] = []map[string]any{{"model": "m1"}, {"model": "m2"}, {"model": "m3"}}
	e.ok("POST", "/api/backends", b, http.StatusCreated)
	for role, m := range map[string]string{"planner": "m1", "architect": "m2", "critic": "m3", "compiler": "m1", "worker": "m1", "reviewer": "m2"} {
		e.ok("PUT", "/api/candidates/"+role, map[string]any{"candidates": []map[string]string{{"backend": "gpu", "model": m}}}, http.StatusOK)
	}
	e.ok("POST", "/api/projects", map[string]any{"slug": "p1", "repo_path": repo(t)}, http.StatusCreated)
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "first"}, http.StatusCreated))
	f.effort, f.base = ef.ID, "/api/projects/p1/efforts/"+itoa(ef.ID)
	return f
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func (f *flow) effortStatus() string {
	return decode[EffortView](f.t, f.ok("GET", f.base, nil, http.StatusOK)).Status
}

func (f *flow) waitStatus(want string) {
	f.t.Helper()
	waitFor(f.t, "effort status "+want, func() bool { return f.effortStatus() == want })
}

// planAndCompile runs the planning loop, approves the plan and compiles it,
// leaving the effort at queue_draft.
func (f *flow) planAndCompile() {
	f.t.Helper()
	f.fake.Script("planner", runner.Step{Reply: planReply, Events: []runner.Event{{Type: "text", Text: "hello-stream-line"}}})
	f.fake.Script("architect", runner.Step{Reply: approveReply})
	f.fake.Script("critic", runner.Step{Reply: approveReply})
	f.fake.Script("compiler", runner.Step{Reply: compileReply})
	f.ok("PUT", f.base+"/frd", frdBody, http.StatusOK)
	f.ok("POST", f.base+"/plan/generate", nil, http.StatusAccepted)
	f.waitStatus(model.EffortPlanReady)
	f.ok("POST", f.base+"/plan/approve", nil, http.StatusOK) // approval starts the compile
	f.waitStatus(model.EffortQueueDraft)
}

// The whole planning path over HTTP: FRD revisions, the ralplan loop, plan
// approval, DBS3/DBS4 revisions, the stage stream, compile and the draft
// queue (AC-DBS3 HTTP, AC-QA1).
func TestD3_FRDPlanQueueFlow(t *testing.T) {
	f := newFlow(t, envOpt{})
	f.ok("GET", f.base+"/frd", nil, http.StatusNotFound)
	f.field("PUT", f.base+"/frd", `{"id":"X","title":"t","requirements":[{"id":"R1","description":"d"}]}`, "objective")
	f.field("PUT", f.base+"/frd", `{"id":"X","title":"t","objective":"o","requirements":[],"surprise":1}`, "surprise")
	f.field("PUT", f.base+"/frd", `{"id":"X","title":5}`, "title")
	f.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "second"}, http.StatusConflict) // PJ4
	f.ok("GET", "/api/projects/other/efforts/"+itoa(f.effort), nil, http.StatusNotFound)
	f.ok("GET", "/api/projects/p1/efforts/abc", nil, http.StatusBadRequest)
	f.ok("POST", f.base+"/plan/generate", nil, http.StatusConflict) // no FRD yet

	f.ok("PUT", f.base+"/frd", frdBody, http.StatusOK)
	d := decode[DocView](t, f.ok("PUT", f.base+"/frd", strings.Replace(frdBody, `"title":"t"`, `"title":"t2"`, 1), http.StatusOK))
	if d.Revision != 2 {
		t.Fatalf("second save = revision %d", d.Revision)
	}
	f.ok("POST", f.base+"/frd/approve", map[string]any{"revision": 1}, http.StatusOK)
	revs := decode[[]DocView](t, f.ok("GET", f.base+"/frd/revisions", nil, http.StatusOK))
	if len(revs) != 2 || !revs[0].Approved || revs[1].Approved || revs[0].JSON != nil {
		t.Fatalf("frd revisions = %+v", revs)
	}
	f.ok("GET", f.base+"/frd/revisions/3", nil, http.StatusNotFound)

	f.fake.Script("planner", runner.Step{Reply: planReply, Events: []runner.Event{{Type: "text", Text: "hello-stream-line"}}})
	f.fake.Script("architect", runner.Step{Reply: approveReply})
	f.fake.Script("critic", runner.Step{Reply: approveReply})
	f.fake.Script("compiler", runner.Step{Reply: compileReply}, runner.Step{Reply: compileReply}) // approval compiles, then a recompile
	f.ok("POST", f.base+"/plan/generate", nil, http.StatusAccepted)
	f.waitStatus(model.EffortPlanReady)

	pv := decode[struct {
		Revision          int  `json:"revision"`
		Rounds            int  `json:"rounds"`
		NotCriticApproved bool `json:"not_critic_approved"`
	}](t, f.ok("GET", f.base+"/plan", nil, http.StatusOK))
	if pv.Revision < 1 || pv.Rounds != 1 || pv.NotCriticApproved {
		t.Fatalf("plan view = %+v", pv)
	}
	rounds := decode[struct{ Rounds []Round }](t, f.ok("GET", f.base+"/plan/rounds", nil, http.StatusOK))
	if len(rounds.Rounds) != 1 || len(rounds.Rounds[0].Reviews) != 2 {
		t.Fatalf("rounds = %+v", rounds)
	}

	// The planner stage ran on a lane: its output streams back through the API.
	var planner int64
	for _, st := range mustStages(t, f) {
		if st.Role == "planner" {
			planner = st.ID
		}
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Get(f.srv.URL + "/api/stages/" + itoa(planner) + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "hello-stream-line") {
		t.Fatalf("stage stream = %d %q", resp.StatusCode, body)
	}
	f.ok("GET", "/api/stages/99999/stream", nil, http.StatusNotFound)

	// AC-DBS3 (HTTP): approve, then edit.
	f.ok("POST", f.base+"/plan/approve", nil, http.StatusOK)
	f.waitStatus(model.EffortQueueDraft) // approval started the compile by itself
	ev := decode[EffortView](t, f.ok("GET", f.base, nil, http.StatusOK))
	n := pv.Revision
	if ev.Plan.Approved != n {
		t.Fatalf("after approve: %+v", ev)
	}
	before := f.ok("GET", f.base+"/plan/revisions/"+itoa(int64(n)), nil, http.StatusOK)
	edited := decode[DocView](t, f.ok("PUT", f.base+"/plan", map[string]any{"principles": []string{"edited"}}, http.StatusOK))
	if edited.Revision != n+1 || edited.Approved {
		t.Fatalf("edit = %+v, want revision %d unapproved", edited, n+1)
	}
	if after := f.ok("GET", f.base+"/plan/revisions/"+itoa(int64(n)), nil, http.StatusOK); string(after) != string(before) {
		t.Fatalf("approved revision changed:\n%s\n%s", before, after)
	}
	if !decode[DocView](t, before).Approved {
		t.Fatal("revision n lost its approval")
	}
	f.ok("GET", f.base+"/plan/revisions/0", nil, http.StatusBadRequest)

	// Compile, then the draft queue; AC-QA1.
	f.ok("POST", f.base+"/compile", nil, http.StatusAccepted)
	f.waitStatus(model.EffortQueueDraft)
	q := decode[struct {
		Hash   string
		Status string
		Tasks  []model.Task
	}](t, f.ok("GET", f.base+"/queue", nil, http.StatusOK))
	if q.Status != "draft" || len(q.Tasks) != 2 || q.Tasks[0].Prompt == "" || q.Tasks[1].DependsOn[0] != "T001" {
		t.Fatalf("queue = %+v", q)
	}
	f.ok("POST", "/api/projects/p1/run/start", nil, http.StatusConflict)
	f.ok("POST", "/api/projects/p1/run/run-one", nil, http.StatusConflict)
	f.ok("POST", "/api/projects/p1/run/fly", nil, http.StatusNotFound)
	f.ok("POST", "/api/projects/ghost/run/start", nil, http.StatusNotFound)
	f.field("POST", f.base+"/queue/approve", map[string]any{}, "hash")
	f.ok("POST", f.base+"/queue/approve", map[string]any{"hash": "stale"}, http.StatusConflict)

	// Editing a task returns the queue to draft with a new hash.
	tv := decode[[]TaskView](t, f.ok("GET", f.base+"/tasks", nil, http.StatusOK))
	if len(tv) != 2 || tv[0].Status != model.StatusReady || tv[1].Status != model.StatusBlocked || tv[0].Attempts != 0 {
		t.Fatalf("tasks = %+v", tv)
	}
	f.field("PUT", f.base+"/tasks/T001", map[string]any{"key": "T009", "title": "x"}, "key")
	f.ok("PUT", f.base+"/tasks/T404", map[string]any{"phase": "P1", "title": "x", "prompt": "p", "acceptance": []string{"a"}}, http.StatusNotFound)
	nq := decode[struct{ Hash string }](t, f.ok("PUT", f.base+"/tasks/T001", map[string]any{
		"phase": "P1", "title": "first, edited", "prompt": "create a.txt, carefully", "acceptance": []string{"R1: a.txt exists"}, "files_touched": []string{"a.txt"},
	}, http.StatusOK))
	if nq.Hash == "" || nq.Hash == q.Hash {
		t.Fatalf("hash after edit = %q (before %q)", nq.Hash, q.Hash)
	}
	f.ok("POST", f.base+"/queue/approve", map[string]any{"hash": q.Hash}, http.StatusConflict)
	f.ok("POST", f.base+"/queue/approve", map[string]any{"hash": nq.Hash}, http.StatusOK)
	if s := f.effortStatus(); s != model.EffortQueueAppr {
		t.Fatalf("effort status = %s, want queue_approved", s)
	}

	// Halted-task actions on a task that is not halted are conflicts.
	f.ok("POST", f.base+"/tasks/T001/retry", map[string]any{"clean": true}, http.StatusConflict)
	f.ok("POST", f.base+"/tasks/T001/override", map[string]any{"reason": "because"}, http.StatusConflict)
	f.ok("POST", f.base+"/tasks/T404/retry", nil, http.StatusNotFound)
	f.ok("GET", f.base+"/tasks/T404", nil, http.StatusNotFound)

	// YOLO needs confirmation to turn on; the invariants are listed.
	f.field("POST", f.base+"/yolo", map[string]any{"on": true}, "confirm")
	f.ok("POST", f.base+"/yolo", map[string]any{"on": true, "confirm": true}, http.StatusOK)
	f.ok("POST", f.base+"/yolo", map[string]any{"on": false}, http.StatusOK)
	if inv := decode[[]map[string]string](t, f.ok("GET", "/api/yolo/invariants", nil, http.StatusOK)); len(inv) == 0 || inv[0]["id"] == "" {
		t.Fatalf("invariants = %v", inv)
	}

	// Abandoning ends the effort; a project with an active effort cannot be unregistered.
	f.ok("POST", f.base+"/abandon", nil, http.StatusOK)
	if s := f.effortStatus(); s != model.EffortAbandoned {
		t.Fatalf("status = %s", s)
	}
	f.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "again"}, http.StatusCreated)
	f.ok("DELETE", "/api/projects/p1", nil, http.StatusConflict)
	if list := decode[[]EffortView](t, f.ok("GET", "/api/projects/p1/efforts", nil, http.StatusOK)); len(list) != 2 || list[1].Status != model.EffortAbandoned {
		t.Fatalf("efforts = %+v", list)
	}
}

func mustStages(t *testing.T, f *flow) []model.Stage {
	t.Helper()
	sts, err := f.st.ListEffortStages(t.Context(), f.effort)
	if err != nil {
		t.Fatal(err)
	}
	return sts
}

// AC-RC3 (HTTP): run-one returns, one attempt is dispatched, and when it has
// drained the event stream delivers project.run_state=paused.
func TestRC3_RunOneOverHTTPDeliversPaused(t *testing.T) {
	f := newFlow(t, envOpt{})
	f.planAndCompile()
	f.fake.Script("worker", runner.Step{Reply: workerReply})
	q := decode[struct{ Hash string }](t, f.ok("GET", f.base+"/queue", nil, http.StatusOK))
	f.ok("POST", f.base+"/queue/approve", map[string]any{"hash": q.Hash}, http.StatusOK)

	code, c := dialSSE(t, f.srv.URL+"/api/events")
	if code != http.StatusOK {
		t.Fatalf("events = %d", code)
	}
	pv := decode[ProjectView](t, f.ok("POST", "/api/projects/p1/run/run-one", nil, http.StatusOK))
	if pv.RunState == model.RunStopped {
		t.Fatalf("run-one left the project %s", pv.RunState)
	}
	f.ok("POST", "/api/projects/p1/run/run-one", nil, http.StatusConflict)
	c.next(t, func(m map[string]any) bool { return m["type"] == "project.run_state" && m["state"] == model.RunPausing })

	// The worker stage ends; this engine has no git service, so nothing
	// chains onwards and the test closes the attempt the way the chain would.
	var attempt int64
	waitFor(t, "the worker stage", func() bool {
		tasks, _ := f.st.ListTasks(t.Context(), f.effort)
		for _, tk := range tasks {
			atts, _ := f.st.ListAttempts(t.Context(), tk.ID)
			for _, a := range atts {
				sts, _ := f.st.ListAttemptStages(t.Context(), a.ID)
				if len(sts) > 0 && sts[0].State == model.StageDone {
					attempt = a.ID
				}
			}
		}
		return attempt != 0
	})
	if err := f.st.FinishAttempt(t.Context(), attempt, model.OutcomePassed, "", ""); err != nil {
		t.Fatal(err)
	}
	f.eng.Runs().Settle(t.Context())
	ev := c.next(t, func(m map[string]any) bool { return m["type"] == "project.run_state" && m["state"] == model.RunPaused })
	if ev["project"] != "p1" || ev["action"] != "run.paused" {
		t.Fatalf("paused event = %v", ev)
	}

	d := decode[TaskDetail](t, f.ok("GET", f.base+"/tasks/T001", nil, http.StatusOK))
	if d.Status != model.StatusPassed || len(d.AttemptList) != 1 || len(d.AttemptList[0].Stages) == 0 || d.AttemptList[0].Stages[0].Role != "worker" {
		t.Fatalf("task detail = %+v", d)
	}
	pv = decode[ProjectView](t, f.ok("GET", "/api/projects/p1", nil, http.StatusOK))
	if pv.RunState != model.RunPaused || pv.Progress["passed"] != 1 || pv.Progress["total"] != 2 || pv.Effort == nil {
		t.Fatalf("overview = %+v", pv)
	}
	f.ok("POST", "/api/projects/p1/run/stop", nil, http.StatusOK)
	if st := f.eng.Runs().State("p1"); st != model.RunStopped {
		t.Fatalf("state after stop = %s", st)
	}
}

// The event stream holds at most SSEMaxSubscribers connections; one more is
// 503 and a freed slot is usable again.
func TestSSECapReturns503AtCapPlusOne(t *testing.T) {
	e := newEnv(t, envOpt{maxSubs: 2})
	srv := httptest.NewServer(e.mux)
	defer srv.Close()
	_, a := dialSSE(t, srv.URL+"/api/events")
	_, b := dialSSE(t, srv.URL+"/api/events")
	if code, _ := dialSSE(t, srv.URL+"/api/events"); code != http.StatusServiceUnavailable {
		t.Fatalf("third subscriber = %d, want 503", code)
	}
	a.cancel()
	waitFor(t, "a freed subscriber slot", func() bool {
		code, c := dialSSE(t, srv.URL+"/api/events")
		if c != nil {
			c.cancel()
		}
		return code == http.StatusOK
	})
	b.cancel()
}

// POST /api/projects/_all_ drops every project and effort in one call: it needs
// confirmation, abandons active efforts, stops runs, keeps an archive of the
// database, and leaves backends and settings alone.
func TestDropAllProjects(t *testing.T) {
	f := newFlow(t, envOpt{})
	f.ok("POST", "/api/projects/_all_", map[string]any{}, http.StatusBadRequest)
	if list := decode[[]ProjectView](t, f.ok("GET", "/api/projects", nil, http.StatusOK)); len(list) == 0 {
		t.Fatal("a refused drop removed projects")
	}
	backends := len(decode[[]map[string]any](t, f.ok("GET", "/api/backends", nil, http.StatusOK)))
	res := decode[struct {
		Dropped []string `json:"dropped"`
		Archive string   `json:"archive"`
	}](t, f.ok("POST", "/api/projects/_all_", map[string]any{"confirm": true}, http.StatusOK))
	if len(res.Dropped) == 0 || res.Archive == "" {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(res.Archive); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
	if list := decode[[]ProjectView](t, f.ok("GET", "/api/projects", nil, http.StatusOK)); len(list) != 0 {
		t.Fatalf("projects left: %+v", list)
	}
	if n := len(decode[[]map[string]any](t, f.ok("GET", "/api/backends", nil, http.StatusOK))); n != backends {
		t.Fatalf("backends = %d, want %d kept", n, backends)
	}
	f.ok("POST", "/api/projects/_all_", map[string]any{"confirm": true}, http.StatusOK) // nothing left is not an error
}
