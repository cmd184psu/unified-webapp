package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

const (
	workerOK   = `{"summary":"did it","files_changed":["a.go"],"learnings":[]}`
	reviewPass = `{"verdict":"pass","summary":"fine","issues":[],"repair_prompt":""}`
	reviewBad  = `{"verdict":"maybe","summary":"hm","issues":[],"repair_prompt":""}`
)

// fixture is an attempt stage on a backend: project, approved one-task queue,
// open attempt and a queued stage carrying the queue hash.
type fixture struct {
	payload  StagePayload
	stage    model.Stage
	attempt  model.Attempt
	hash     string
	worktree string
	lane     golane.Lane
}

var fixtureSeq atomic.Int64

// attemptStage builds a fixture for role on a backend lane (added if needed).
func (h *harness) attemptStage(backend, role string) fixture {
	h.t.Helper()
	ctx := context.Background()
	if _, err := h.st.GetBackend(ctx, backend); err != nil {
		h.mustAdd(backend, 1)
		h.mustResume()
	}
	repo := h.t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	slug := fmt.Sprintf("p%d", fixtureSeq.Add(1))
	if err := h.st.CreateProject(ctx, "t", store.Project{Slug: slug, RepoPath: repo, ModelScope: "any"}); err != nil {
		h.t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		h.t.Fatal(err)
	}
	def := model.TaskDef{Key: "T1", Phase: "P1", Title: "first", Prompt: "do the first thing", Acceptance: []string{"it works"}}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, []model.TaskDef{def})
	if err != nil {
		h.t.Fatal(err)
	}
	if q, err = h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		h.t.Fatal(err)
	}
	tasks, err := h.st.ListTasks(ctx, ef.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	wt := h.t.TempDir()
	att, err := h.st.StartAttempt(ctx, tasks[0].ID, model.AttemptNormal, wt, "m1")
	if err != nil {
		h.t.Fatal(err)
	}
	sg, err := h.st.CreateStage(ctx, store.StageSpec{Attempt: att.ID, Role: role, Backend: backend, Model: "m1", QueueHash: q.Hash})
	if err != nil {
		h.t.Fatal(err)
	}
	return fixture{
		payload: StagePayload{Project: slug, Effort: ef.ID, Task: tasks[0].ID, Stage: sg.ID, Attempt: att.ID, QueueHash: q.Hash},
		stage:   sg, attempt: att, hash: q.Hash, worktree: wt, lane: h.lane(BackendLane(backend)),
	}
}

func kindFor(role string) string {
	switch role {
	case "worker":
		return KindWork
	case "reviewer":
		return KindReview
	case "verify":
		return KindVerify
	case "merge":
		return KindMerge
	}
	return KindPlan
}

func (h *harness) submitFixture(f fixture) string {
	h.t.Helper()
	l := f.lane
	if f.stage.Role == "verify" {
		l = h.lane(VerifyLane)
	}
	job, err := l.Submit(kindFor(f.stage.Role), "stage", f.payload)
	if err != nil {
		h.t.Fatal(err)
	}
	return job.ID
}

func (h *harness) waitDone(id int64) model.Stage {
	h.t.Helper()
	var st model.Stage
	waitFor(h.t, fmt.Sprintf("stage %d done", id), func() bool {
		var err error
		if st, err = h.st.GetStage(context.Background(), id); err != nil {
			h.t.Fatal(err)
		}
		return st.State == model.StageDone
	})
	return st
}

// run submits the fixture and waits for its stage outcome.
func (h *harness) run(f fixture) model.Stage {
	h.t.Helper()
	h.submitFixture(f)
	return h.waitDone(f.stage.ID)
}

func (h *harness) attemptsOf(f fixture) []model.Attempt {
	h.t.Helper()
	as, err := h.st.ListAttempts(context.Background(), f.payload.Task)
	if err != nil {
		h.t.Fatal(err)
	}
	return as
}

func (h *harness) assertAttemptUntouched(f fixture) {
	h.t.Helper()
	as := h.attemptsOf(f)
	if len(as) != 1 || as[0].Outcome != "" {
		h.t.Fatalf("attempts changed: %+v", as)
	}
}

func newStageHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.start()
	return h
}

// AC-SO1 (retry half): verdict "maybe" twice gives exactly two runner calls,
// an infra outcome and an unchanged attempt count.
func TestSO1_UnparseableTwiceIsInfra(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("reviewer", runner.Step{Reply: reviewBad}, runner.Step{Reply: reviewBad})
	f := h.attemptStage("alpha", "reviewer")
	st := h.run(f)

	if st.Outcome != OutcomeInfraUnparseable || !IsInfra(st.Outcome) {
		t.Fatalf("outcome = %q, want %s", st.Outcome, OutcomeInfraUnparseable)
	}
	calls := h.fake.CallsFor("reviewer")
	if len(calls) != 2 {
		t.Fatalf("runner calls = %d, want exactly 2", len(calls))
	}
	if calls[1].Request.Model != calls[0].Request.Model || calls[1].Request.Backend != calls[0].Request.Backend {
		t.Fatalf("corrective used a different model: %+v vs %+v", calls[0].Request, calls[1].Request)
	}
	if !strings.Contains(calls[1].Prompt, reviewBad) || !strings.Contains(calls[1].Prompt, "could not be accepted") {
		t.Fatalf("re-run prompt lacks the previous output and the correction:\n%s", calls[1].Prompt)
	}
	if st.RawOutput == "" {
		t.Fatal("raw output not stored")
	}
	h.assertAttemptUntouched(f)
}

// SO2 with a session: the follow-up continues the runner's session with a
// short corrective prompt, and a good second reply is stored, parsed.
func TestSO2_CorrectiveFollowUpUsesSession(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("reviewer", runner.Step{Reply: reviewBad, SessionID: "sess-1"}, runner.Step{Reply: reviewPass})
	f := h.attemptStage("alpha", "reviewer")
	st := h.run(f)

	if st.Outcome != OutcomePass {
		t.Fatalf("outcome = %q, want pass (raw %q)", st.Outcome, st.RawOutput)
	}
	calls := h.fake.CallsFor("reviewer")
	if len(calls) != 2 || calls[1].Request.SessionID != "sess-1" {
		t.Fatalf("calls = %d, second session = %q", len(calls), calls[len(calls)-1].Request.SessionID)
	}
	if strings.Contains(calls[1].Prompt, reviewBad) || !strings.Contains(calls[1].Prompt, "could not be accepted") {
		t.Fatalf("session follow-up prompt should be the short correction only:\n%s", calls[1].Prompt)
	}
	if st.RawOutput != reviewPass || !strings.Contains(string(st.Parsed), `"verdict":"pass"`) {
		t.Fatalf("raw/parsed not stored: %q / %s", st.RawOutput, st.Parsed)
	}
}

// AC-SO1 (first half) and SO3: a fenced reply parses on the first call and
// both raw and parsed output are stored; a repair verdict is outcome repair.
func TestSO3_FencedReplyParsesAndStoresRawAndParsed(t *testing.T) {
	h := newStageHarness(t)
	fenced := "Here you go:\n```json\n" + `{"verdict":"repair","summary":"s","issues":[{"description":"d","severity":"major"}],"repair_prompt":"fix it"}` + "\n```"
	h.fake.Script("reviewer", runner.Step{Reply: fenced})
	f := h.attemptStage("alpha", "reviewer")
	st := h.run(f)

	if st.Outcome != OutcomeRepair {
		t.Fatalf("outcome = %q, want repair", st.Outcome)
	}
	if len(h.fake.CallsFor("reviewer")) != 1 {
		t.Fatal("a valid fenced reply must not trigger a follow-up")
	}
	if st.RawOutput != fenced || !strings.Contains(string(st.Parsed), `"repair_prompt":"fix it"`) {
		t.Fatalf("raw/parsed: %q / %s", st.RawOutput, st.Parsed)
	}
}

// AC-SV1 (panic half): a runner panic is infra:panic, the process lives and
// the lane runs the next stage.
func TestSV1_PanicIsInfraAndLaneSurvives(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("worker", runner.Step{Panic: "boom"}, runner.Step{Reply: workerOK})
	f1 := h.attemptStage("alpha", "worker")
	st := h.run(f1)
	if st.Outcome != OutcomeInfraPanic {
		t.Fatalf("outcome = %q, want %s", st.Outcome, OutcomeInfraPanic)
	}
	if !strings.Contains(st.RawOutput, "boom") {
		t.Fatalf("panic not recorded in raw output: %q", st.RawOutput)
	}
	h.assertAttemptUntouched(f1)

	f2 := h.attemptStage("alpha", "worker")
	if st := h.run(f2); st.Outcome != OutcomePass {
		t.Fatalf("stage after the panic: %q", st.Outcome)
	}
}

// AC-QA2 (discard half): the queue is re-approved with a new hash mid-run and
// the stage ends discarded with no attempt counted.
func TestQA2_StaleHashMidRunIsDiscarded(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("worker", runner.Step{Reply: workerOK, Delay: 400 * time.Millisecond})
	f := h.attemptStage("alpha", "worker")
	h.submitFixture(f)
	waitFor(t, "stage running", func() bool {
		st, _ := h.st.GetStage(context.Background(), f.stage.ID)
		return st.State == model.StageRunning
	})
	ctx := context.Background()
	edited := model.TaskDef{Key: "T1", Phase: "P1", Title: "first", Prompt: "do something else", Acceptance: []string{"it works"}}
	q, err := h.st.EditTask(ctx, "t", f.payload.Effort, edited)
	if err != nil {
		t.Fatal(err)
	}
	if q.Hash == f.hash {
		t.Fatal("edit did not change the hash")
	}
	if _, err := h.st.ApproveQueue(ctx, "t", f.payload.Effort, q.Hash); err != nil {
		t.Fatal(err)
	}
	st := h.waitDone(f.stage.ID)
	if st.Outcome != OutcomeDiscarded {
		t.Fatalf("outcome = %q, want discarded", st.Outcome)
	}
	h.assertAttemptUntouched(f)
}

// A stage whose hash is already stale when its job starts never calls the
// runner.
func TestQA2_StaleHashAtStartSkipsRunner(t *testing.T) {
	h := newStageHarness(t)
	f := h.attemptStage("alpha", "worker")
	edited := model.TaskDef{Key: "T1", Phase: "P1", Title: "first", Prompt: "changed", Acceptance: []string{"x"}}
	if _, err := h.st.EditTask(context.Background(), "t", f.payload.Effort, edited); err != nil {
		t.Fatal(err)
	}
	st := h.run(f) // the queue is a draft now: no approved hash matches
	if st.Outcome != OutcomeDiscarded {
		t.Fatalf("outcome = %q, want discarded", st.Outcome)
	}
	if n := len(h.fake.Calls()); n != 0 {
		t.Fatalf("runner called %d times for a stale stage", n)
	}
	h.assertAttemptUntouched(f)
}

// TM5: events written by the runtime stream through lane.StreamOutput.
func TestTM5_OutputStreamsThroughLane(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("worker", runner.Step{Reply: workerOK, Events: []runner.Event{
		{Type: "text", Text: "thinking about marker-alpha"}, {Type: "text", Text: "editing marker-beta"},
	}})
	f := h.attemptStage("alpha", "worker")
	id := h.submitFixture(f)
	h.waitStatus(f.lane, id, "success")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := f.lane.StreamOutput(w, r, id); err != nil {
			http.Error(w, err.Error(), 500)
		}
	}))
	defer srv.Close()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	for _, want := range []string{"marker-alpha", "marker-beta"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("stream lacks %q:\n%s", want, body)
		}
	}
}

// MB8 and AC-TM1 (engine half): the stage row records backend and model, and
// its job is listed on the lane with kind agentflow.work. DBS5: the moment the
// job is a success, the outcome is already stored.
func TestMB8_TM1_StageRecordsBackendModelAndJobIsListed(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("worker", runner.Step{Reply: workerOK})
	f := h.attemptStage("alpha", "worker")
	id := h.submitFixture(f)
	h.waitStatus(f.lane, id, "success")

	st, err := h.st.GetStage(context.Background(), f.stage.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != model.StageDone || st.Outcome != OutcomePass {
		t.Fatalf("job finished but stage is %s/%q: outcome not written before return", st.State, st.Outcome)
	}
	if st.Backend != "alpha" || st.Model != "m1" || st.LaneJobID != id {
		t.Fatalf("stage row: backend %q model %q job %q (want alpha, m1, %s)", st.Backend, st.Model, st.LaneJobID, id)
	}
	jobs, err := f.lane.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, j := range jobs {
		found = found || (j.ID == id && j.Kind == KindWork)
	}
	if !found {
		t.Fatalf("job %s with kind %s not in lane.List: %+v", id, KindWork, jobs)
	}
}

// OC2 / BK5 / TM2: prompt.md is exactly what the model saw, the config is
// frozen for the stage's backend, the worker runs in the attempt worktree and
// the dispatcher is kicked only after the outcome is stored (DBS5).
func TestRuntime_PromptFreezeWorkdirAndKick(t *testing.T) {
	h := newHarness(t)
	var frozenBackend FrozenBackend
	var frozenDir string
	var kicks, watched atomic.Int64
	var outcomeAtKick atomic.Value
	h.tweak = func(d *Deps) {
		d.FreezeConfig = func(b FrozenBackend, dir string) (string, error) {
			frozenBackend, frozenDir = b, dir
			return filepath.Join(dir, "cfg.json"), nil
		}
		d.Kick = func() {
			st, _ := h.st.GetStage(context.Background(), watched.Load())
			outcomeAtKick.Store(st.State)
			kicks.Add(1)
		}
	}
	h.start()
	h.fake.Script("worker", runner.Step{Reply: workerOK})
	f := h.attemptStage("alpha", "worker")
	watched.Store(f.stage.ID)
	h.run(f)
	waitFor(t, "kick", func() bool { return kicks.Load() >= 1 })

	if frozenBackend.Name != "alpha" || len(frozenBackend.Models) != 1 || frozenBackend.Models[0] != "m1" {
		t.Fatalf("frozen backend = %+v", frozenBackend)
	}
	dir := h.eng.StageDir(f.stage.ID)
	if frozenDir != dir {
		t.Fatalf("frozen into %q, want %q", frozenDir, dir)
	}
	calls := h.fake.CallsFor("worker")
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	c := calls[0]
	if c.Request.PromptPath != filepath.Join(dir, "prompt.md") || c.Request.StageDir != dir || c.Request.Workdir != f.worktree {
		t.Fatalf("request paths: %+v", c.Request)
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, "prompt.md"))
	if err != nil || string(onDisk) != c.Prompt {
		t.Fatalf("prompt.md differs from what the runner read (err %v)", err)
	}
	if !strings.Contains(c.Prompt, "do the first thing") || !strings.Contains(c.Prompt, "it works") {
		t.Fatalf("prompt lacks the task text:\n%s", c.Prompt)
	}
	if got := outcomeAtKick.Load(); got != model.StageDone {
		t.Fatalf("stage state at kick time = %v, want done", got)
	}
}

// AT7: classification of runner failures. Worker timeout is an attempt
// failure, every other class is infra, and none touch the attempt.
func TestAT7_ClassificationThroughTheRuntime(t *testing.T) {
	cases := []struct {
		name  string
		role  string
		step  runner.Step
		want  string
		infra bool
	}{
		{"rate limit", "worker", runner.Step{ErrClass: runner.ErrRateLimit, Err: errors.New("429")}, OutcomeInfraRateLimit, true},
		{"unreachable", "worker", runner.Step{ErrClass: runner.ErrUnreachable, Err: errors.New("refused")}, OutcomeInfraUnreachable, true},
		{"crash", "worker", runner.Step{ErrClass: runner.ErrCrash, Err: errors.New("exit 2")}, OutcomeInfraCrash, true},
		{"plain error", "reviewer", runner.Step{Err: errors.New("weird")}, OutcomeInfraCrash, true},
		{"worker timeout", "worker", runner.Step{ErrClass: runner.ErrTimeout, Reply: "partial"}, OutcomeTimeout, false},
		{"reviewer timeout", "reviewer", runner.Step{ErrClass: runner.ErrTimeout}, OutcomeInfraTimeout, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newStageHarness(t)
			h.fake.Script(tc.role, tc.step)
			f := h.attemptStage("alpha", tc.role)
			st := h.run(f)
			if st.Outcome != tc.want || IsInfra(st.Outcome) != tc.infra {
				t.Fatalf("outcome = %q, want %q (infra %v)", st.Outcome, tc.want, tc.infra)
			}
			if n := len(h.fake.CallsFor(tc.role)); n != 1 {
				t.Fatalf("runner calls = %d: failures are not retried by the stage itself", n)
			}
			h.assertAttemptUntouched(f)
		})
	}
}

// AT7 cancel: cancelling the job while the runner blocks is infra:cancel, even
// though the outcome is written after the job context is done.
func TestAT7_CancelIsInfra(t *testing.T) {
	h := newStageHarness(t)
	h.fake.Script("worker", runner.Step{Block: true})
	f := h.attemptStage("alpha", "worker")
	id := h.submitFixture(f)
	waitFor(t, "runner call", func() bool { return len(h.fake.CallsFor("worker")) == 1 })
	if _, err := f.lane.Cancel(id); err != nil {
		t.Fatal(err)
	}
	st := h.waitDone(f.stage.ID)
	if st.Outcome != OutcomeInfraCancel {
		t.Fatalf("outcome = %q, want %s", st.Outcome, OutcomeInfraCancel)
	}
	h.assertAttemptUntouched(f)
}

// classifyRun separates the role deadline from the parent context.
func TestClassifyRun_RoleDeadlineVersusParentCancel(t *testing.T) {
	live := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancel2 := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel2()
	<-expired.Done()
	rerr := func(c runner.ErrClass) error { return &runner.Error{Class: c} }
	cases := []struct {
		name   string
		parent context.Context
		role   string
		err    error
		want   string
	}{
		{"success", live, "worker", nil, ""},
		{"role deadline from the engine's own ctx", live, "worker", context.DeadlineExceeded, OutcomeTimeout},
		{"wrapped deadline", live, "worker", fmt.Errorf("x: %w", context.DeadlineExceeded), OutcomeTimeout},
		{"reviewer deadline", live, "reviewer", context.DeadlineExceeded, OutcomeInfraTimeout},
		{"parent cancelled wins over a timeout class", dead, "worker", rerr(runner.ErrTimeout), OutcomeInfraCancel},
		{"parent cancelled wins over a crash", dead, "worker", rerr(runner.ErrCrash), OutcomeInfraCancel},
		{"parent deadline is not a worker timeout", expired, "worker", context.DeadlineExceeded, OutcomeInfraCancel},
		{"bare cancel", live, "worker", context.Canceled, OutcomeInfraCancel},
		{"rate limit", live, "reviewer", rerr(runner.ErrRateLimit), OutcomeInfraRateLimit},
		{"unreachable", live, "planner", rerr(runner.ErrUnreachable), OutcomeInfraUnreachable},
		{"crash", live, "worker", rerr(runner.ErrCrash), OutcomeInfraCrash},
		{"unknown error", live, "worker", errors.New("x"), OutcomeInfraCrash},
	}
	for _, tc := range cases {
		if got := classifyRun(tc.parent, tc.role, tc.err); got != tc.want {
			t.Errorf("%s: classifyRun = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Per-role timeouts come from the settings snapshot.
func TestRoleTimeouts(t *testing.T) {
	set := store.Settings{WorkerTimeout: 1 * time.Minute, ReviewTimeout: 2 * time.Minute, PlanTimeout: 3 * time.Minute, VerifyTimeout: 4 * time.Minute}
	for role, want := range map[string]time.Duration{
		"worker": time.Minute, "reviewer": 2 * time.Minute, "verify": 4 * time.Minute,
		"planner": 3 * time.Minute, "architect": 3 * time.Minute, "critic": 3 * time.Minute, "compiler": 3 * time.Minute,
	} {
		if got := roleTimeout(set, role); got != want {
			t.Errorf("%s timeout = %v, want %v", role, got, want)
		}
	}
}

type fakeExec struct {
	res StageResult
	err error
	got model.Stage
}

func (f *fakeExec) Execute(_ context.Context, _ golane.RunContext, st model.Stage, _ StagePayload) (StageResult, error) {
	f.got = st
	return f.res, f.err
}

// verify and merge are delegated to executors, with the same outcome
// write-before-return, panic recovery and failure classification.
func TestEngineStagesDelegateToExecutors(t *testing.T) {
	ex := &fakeExec{res: StageResult{Outcome: OutcomePass, Raw: "all green"}}
	h := newHarness(t)
	h.tweak = func(d *Deps) { d.Verify = ex }
	h.start()

	f := h.attemptStage("alpha", "verify")
	st := h.run(f)
	if st.Outcome != OutcomePass || st.RawOutput != "all green" || ex.got.ID != f.stage.ID {
		t.Fatalf("verify stage: %+v (executor saw stage %d)", st, ex.got.ID)
	}
	if len(h.fake.Calls()) != 0 {
		t.Fatal("verify must not call the model runner")
	}

	ex.err = errors.New("checks exploded")
	if st := h.run(h.attemptStage("alpha", "verify")); st.Outcome != OutcomeInfraCrash {
		t.Fatalf("executor error outcome = %q", st.Outcome)
	}

	// merge has no executor registered: a recorded infra failure, not a hang.
	m := h.attemptStage("alpha", "merge")
	if _, err := h.lane(MergeLane).Submit(KindMerge, "m", m.payload); err != nil {
		t.Fatal(err)
	}
	if st := h.waitDone(m.stage.ID); st.Outcome != OutcomeInfraCrash || !strings.Contains(st.RawOutput, "no executor") {
		t.Fatalf("merge without executor: %q %q", st.Outcome, st.RawOutput)
	}
}

// AC-OA3: the project's pinned plugins reach the runner request and the frozen
// config; a project that opted into none sends none.
func TestOA3_OnlyTheProjectsPinnedPluginsReachTheRunner(t *testing.T) {
	h := newHarness(t)
	var frozen []FrozenBackend
	h.tweak = func(d *Deps) {
		d.FreezeConfig = func(b FrozenBackend, dir string) (string, error) {
			frozen = append(frozen, b)
			return filepath.Join(dir, "cfg.json"), nil
		}
	}
	h.start()
	h.fake.Script("worker", runner.Step{Reply: workerOK}, runner.Step{Reply: workerOK})
	with := h.attemptStage("alpha", "worker")
	without := h.attemptStage("alpha", "worker")
	ctx := context.Background()
	p, err := h.st.GetProject(ctx, with.payload.Project)
	if err != nil {
		t.Fatal(err)
	}
	p.Plugins = []runner.Plugin{{Name: "notify", Version: "1.2.3"}}
	if err := h.st.UpdateProject(ctx, "t", p); err != nil {
		t.Fatal(err)
	}
	h.run(with)
	h.run(without)

	calls := h.fake.CallsFor("worker")
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	if got := calls[0].Request.Plugins; len(got) != 1 || got[0] != (runner.Plugin{Name: "notify", Version: "1.2.3"}) {
		t.Fatalf("opted-in request plugins = %v", got)
	}
	if got := calls[1].Request.Plugins; len(got) != 0 {
		t.Fatalf("project without opt-in sent plugins %v", got)
	}
	if len(frozen) != 2 || len(frozen[0].Plugins) != 1 || len(frozen[1].Plugins) != 0 {
		t.Fatalf("frozen backends = %+v", frozen)
	}
}
