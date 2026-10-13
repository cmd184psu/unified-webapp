package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// runFix is a project with an effort and a queue of independent tasks.
type runFix struct {
	slug   string
	effort int64
	hash   string
	tasks  []model.Task
}

// runProject creates a project with n independent tasks; the queue is approved
// unless approve is false. repo "" uses a stub repository directory.
func (h *harness) runProject(slug string, n int, approve bool, repo string) runFix {
	h.t.Helper()
	ctx := context.Background()
	if repo == "" {
		repo = h.t.TempDir()
		if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
			h.t.Fatal(err)
		}
	}
	if err := h.st.CreateProject(ctx, "t", store.Project{Slug: slug, RepoPath: repo, ModelScope: "any"}); err != nil {
		h.t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		h.t.Fatal(err)
	}
	var defs []model.TaskDef
	for i := 1; i <= n; i++ {
		defs = append(defs, model.TaskDef{Key: fmt.Sprintf("T%d", i), Phase: "P1", Title: "task", Prompt: "do it", Acceptance: []string{"works"}})
	}
	if n == 0 {
		return runFix{slug: slug, effort: ef.ID}
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, defs)
	if err != nil {
		h.t.Fatal(err)
	}
	if approve {
		if q, err = h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
			h.t.Fatal(err)
		}
	}
	tasks, err := h.st.ListTasks(ctx, ef.ID)
	if err != nil {
		h.t.Fatal(err)
	}
	return runFix{slug: slug, effort: ef.ID, hash: q.Hash, tasks: tasks}
}

// dispatchPass stands in for the C5 dispatcher: for each task with no attempt
// in flight or passed it asks the gate, opens an attempt and submits a worker
// stage. It returns the stages it submitted.
func (h *harness) dispatchPass(f runFix, backend string) []model.Stage {
	h.t.Helper()
	ctx := context.Background()
	var out []model.Stage
	for _, tk := range f.tasks {
		as, err := h.st.ListAttempts(ctx, tk.ID)
		if err != nil {
			h.t.Fatal(err)
		}
		busy := false
		for _, a := range as {
			busy = busy || a.Outcome == "" || a.Outcome == model.OutcomePassed
		}
		if busy {
			continue
		}
		done, ok := h.eng.Runs().Admit(ctx, f.slug)
		if !ok {
			break
		}
		att, err := h.st.StartAttempt(ctx, tk.ID, model.AttemptNormal, h.t.TempDir(), "m1")
		if err != nil {
			h.t.Fatal(err)
		}
		sg, _, err := h.eng.Submit(ctx, BackendLane(backend),
			store.StageSpec{Attempt: att.ID, Role: "worker", Backend: backend, Model: "m1", QueueHash: f.hash},
			StagePayload{Project: f.slug, Effort: f.effort, Task: tk.ID, Attempt: att.ID, QueueHash: f.hash})
		done()
		if err != nil {
			h.t.Fatal(err)
		}
		out = append(out, sg)
	}
	return out
}

func (h *harness) statuses(f runFix) map[string]model.Status {
	h.t.Helper()
	m, err := h.st.TaskStatuses(context.Background(), f.effort, testLimits)
	if err != nil {
		h.t.Fatal(err)
	}
	return m
}

// runAudit lists the project's run-controller audit events as action:actor.
func (h *harness) runAudit(project string) []string {
	h.t.Helper()
	evs, err := h.st.ListEvents(context.Background(), "project", project, 100)
	if err != nil {
		h.t.Fatal(err)
	}
	var out []string
	for _, ev := range evs {
		if strings.HasPrefix(ev.Action, "run.") {
			out = append(out, ev.Action+":"+ev.Actor)
		}
	}
	return out
}

func (h *harness) runHarness() *harness {
	h.start()
	h.mustAdd("alpha", 1)
	h.startEngine()
	return h
}

// AC-QA1 (engine): start on a draft queue, or an effort with no queue, is a
// conflict and the project stays stopped.
func TestQA1_StartOnDraftQueueIsConflict(t *testing.T) {
	h := newHarness(t).runHarness()
	draft := h.runProject("draft", 1, false, "")
	if err := h.eng.Runs().Start(context.Background(), "ann", draft.slug); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Start on a draft queue = %v, want ErrConflict", err)
	}
	if err := h.eng.Runs().RunOne(context.Background(), "ann", draft.slug); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("RunOne on a draft queue = %v, want ErrConflict", err)
	}
	if s := h.eng.Runs().State(draft.slug); s != model.RunStopped {
		t.Fatalf("state = %s", s)
	}
	if _, ok := h.eng.Runs().Admit(context.Background(), draft.slug); ok {
		t.Fatal("a stopped project admitted a dispatch")
	}
	bare := h.runProject("noqueue", 0, false, "")
	if err := h.eng.Runs().Start(context.Background(), "ann", bare.slug); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Start without tasks/queue = %v", err)
	}
	if err := h.eng.Runs().Start(context.Background(), "ann", "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Start on an unknown project = %v, want ErrNotFound", err)
	}
}

// AC-RC3: run-one with three ready tasks dispatches exactly one attempt, and
// the project ends paused once it drains. Each transition is audited with its
// actor and published.
func TestRC3_RunOneDispatchesOneAttemptThenPauses(t *testing.T) {
	var mu sync.Mutex
	var published []RunEvent
	h := newHarness(t)
	h.tweak = func(d *Deps) {
		d.PublishRun = func(ev RunEvent) { mu.Lock(); published = append(published, ev); mu.Unlock() }
	}
	h.runHarness()
	f := h.runProject("proj", 3, true, "")
	ctl := h.eng.Runs()
	if err := ctl.RunOne(context.Background(), "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	if err := ctl.RunOne(context.Background(), "ann", f.slug); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second RunOne while running = %v, want ErrConflict", err)
	}

	var started []model.Stage
	for i := 0; i < 3; i++ { // three dispatcher passes
		started = append(started, h.dispatchPass(f, "alpha")...)
	}
	if len(started) != 1 {
		t.Fatalf("RunOne dispatched %d stages, want exactly 1", len(started))
	}
	if s := ctl.State(f.slug); s != model.RunPausing {
		t.Fatalf("state with the attempt in flight = %s, want pausing", s)
	}
	h.waitDone(started[0].ID)
	ctl.Settle(context.Background())
	if s := ctl.State(f.slug); s != model.RunPausing {
		t.Fatalf("state with the attempt still open = %s, want pausing", s)
	}
	// The attempt chain (C6) finishes the attempt after its last stage.
	st, _ := h.st.GetStage(context.Background(), started[0].ID)
	if err := h.st.FinishAttempt(context.Background(), st.Attempt, model.OutcomePassed, "", ""); err != nil {
		t.Fatal(err)
	}
	ctl.Settle(context.Background())
	if s := ctl.State(f.slug); s != model.RunPaused {
		t.Fatalf("state after drain = %s, want paused", s)
	}
	if more := h.dispatchPass(f, "alpha"); len(more) != 0 {
		t.Fatalf("paused project dispatched %d stages", len(more))
	}
	n := 0
	for _, as := range h.statuses(f) {
		if as == model.StatusPassed {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("passed tasks = %d, want 1", n)
	}

	want := []string{"run.one:ann", "run.pausing:system", "run.paused:system"}
	if got := h.runAudit(f.slug); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("audit = %v, want %v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(published) != 3 || published[2].State != model.RunPaused || published[0].Actor != "ann" {
		t.Fatalf("published = %+v", published)
	}
}

// Pause lets in-flight work finish and dispatches nothing new; Start resumes.
func TestRC2_PauseDrainsThenPaused(t *testing.T) {
	h := newHarness(t)
	h.fake.Script("worker", runner.Step{Reply: workerOK, Delay: 300 * time.Millisecond})
	h.runHarness()
	f := h.runProject("proj", 2, true, "")
	ctl := h.eng.Runs()
	ctx := context.Background()
	if err := ctl.Pause(ctx, "ann", f.slug); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("Pause on a stopped project = %v, want ErrConflict", err)
	}
	if err := ctl.Start(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	if got := h.dispatchPass(f, "alpha"); len(got) != 2 {
		t.Fatalf("running project dispatched %d, want 2", len(got))
	}
	if err := ctl.Pause(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	if s := ctl.State(f.slug); s != model.RunPausing {
		t.Fatalf("state = %s, want pausing", s)
	}
	if _, ok := ctl.Admit(ctx, f.slug); ok {
		t.Fatal("pausing project admitted a dispatch")
	}
	waitFor(t, "stages done", func() bool {
		open, _, _ := ctl.openWork(ctx, f.effort)
		return len(open) == 0
	})
	for _, tk := range f.tasks {
		as, _ := h.st.ListAttempts(ctx, tk.ID)
		if err := h.st.FinishAttempt(ctx, as[0].ID, model.OutcomePassed, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	ctl.Settle(ctx)
	if s := ctl.State(f.slug); s != model.RunPaused {
		t.Fatalf("state = %s, want paused", s)
	}
	if err := ctl.Start(ctx, "bob", f.slug); err != nil || ctl.State(f.slug) != model.RunRunning {
		t.Fatalf("Start from paused: %v, state %s", err, ctl.State(f.slug))
	}
	if err := ctl.Stop(ctx, "bob", f.slug); err != nil || ctl.State(f.slug) != model.RunStopped {
		t.Fatalf("Stop: %v, state %s", err, ctl.State(f.slug))
	}
}

// AC-RC1: a project that was running comes back stopped after a restart (the
// persisted state is display only), no job survives on any agentflow lane,
// and three dispatcher passes dispatch nothing. PJ2: the stopped project is
// still listed, with what it was doing.
func TestRC1_RestartComesBackStoppedWithNoJobs(t *testing.T) {
	h := newHarness(t)
	h.widthZ = true
	h.runHarness()
	f := h.runProject("proj", 2, true, "")
	ctx := context.Background()
	if err := h.eng.Runs().Start(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	if got := h.dispatchPass(f, "alpha"); len(got) != 2 {
		t.Fatalf("dispatched %d stages, want 2", len(got))
	}
	if n := h.agentflowJobs(); n != 2 {
		t.Fatalf("queued jobs before restart = %d", n)
	}

	h.eng.Close()
	h.restart()
	h.startEngine()
	ctl := h.eng.Runs()
	if s := ctl.State(f.slug); s != model.RunStopped {
		t.Fatalf("state after restart = %s, want stopped", s)
	}
	list, err := ctl.List(ctx)
	if err != nil || len(list) != 1 || list[0].Project != f.slug || list[0].State != model.RunStopped || list[0].Remembered != model.RunRunning {
		t.Fatalf("List = %+v, %v; want the stopped project remembered as running", list, err)
	}
	for i := 0; i < 3; i++ {
		if got := h.dispatchPass(f, "alpha"); len(got) != 0 {
			t.Fatalf("pass %d dispatched %d stages for a stopped project", i, len(got))
		}
	}
	if n := h.agentflowJobs(); n != 0 {
		t.Fatalf("jobs on agentflow lanes after restart = %d, want 0", n)
	}
	for k, s := range h.statuses(f) {
		if s != model.StatusReady {
			t.Fatalf("task %s = %s, want ready", k, s)
		}
	}
}

// sleepRunner runs `sleep <arg>` in the bwrap sandbox, like a real agent
// process tree would run.
type sleepRunner struct {
	arg  string
	repo string
}

func (r sleepRunner) Version(context.Context) (string, error) { return "sleep", nil }

func (r sleepRunner) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	home := filepath.Join(req.StageDir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return runner.Result{}, err
	}
	gitDir := filepath.Join(r.repo, ".git")
	cmd, err := sandbox.Command(ctx, sandbox.Spec{
		Worktree: req.Workdir, RepoGitDir: gitDir, WorktreeGitDir: filepath.Join(gitDir, "worktrees", filepath.Base(req.Workdir)),
		StageHome: home, Net: true, Argv: []string{"/bin/sh", "-c", fmt.Sprintf("sleep %s & sleep %s", r.arg, r.arg)},
	})
	if err != nil {
		return runner.Result{}, err
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	return runner.Result{Final: out.String()}, cmd.Run()
}

func sleepers(arg string) int {
	n := 0
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		if b, err := os.ReadFile("/proc/" + e.Name() + "/cmdline"); err == nil && bytes.Contains(b, []byte("sleep\x00"+arg)) {
			n++
		}
	}
	return n
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_CONFIG_GLOBAL=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// AC-RC2: Stop during a sandboxed stage leaves no child processes, closes the
// stage as infra:cancel, and the task is ready again with its attempt count
// unchanged (the interrupted attempt is abandoned, so it does not count).
func TestRC2_StopKillsSandboxedProcessTree(t *testing.T) {
	arg := fmt.Sprintf("%d.35", 600+os.Getpid()%300)
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "a.txt")
	gitIn(t, repo, "commit", "-q", "-m", "init")
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, "worktree", "add", "-q", "-b", "wtb", wt)

	h := newHarness(t)
	h.fake = nil
	h.tweak = func(d *Deps) { d.Runner = sleepRunner{arg: arg, repo: repo} }
	h.runHarness()
	f := h.runProject("proj", 1, true, repo)
	ctx := context.Background()
	if err := h.eng.Runs().Start(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	done, ok := h.eng.Runs().Admit(ctx, f.slug)
	if !ok {
		t.Fatal("running project refused a dispatch")
	}
	att, err := h.st.StartAttempt(ctx, f.tasks[0].ID, model.AttemptNormal, wt, "m1")
	if err != nil {
		t.Fatal(err)
	}
	sg, _, err := h.eng.Submit(ctx, BackendLane("alpha"), store.StageSpec{Attempt: att.ID, Role: "worker", Backend: "alpha", Model: "m1", QueueHash: f.hash},
		StagePayload{Project: f.slug, Effort: f.effort, Task: f.tasks[0].ID, Attempt: att.ID, QueueHash: f.hash})
	done()
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two sleeps running in the sandbox", func() bool { return sleepers(arg) >= 2 })

	if err := h.eng.Runs().Stop(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "no sleep left", func() bool { return sleepers(arg) == 0 })
	got, _ := h.st.GetStage(ctx, sg.ID)
	if got.State != model.StageDone || got.Outcome != OutcomeInfraCancel {
		t.Fatalf("stage = %s/%q, want done/%s", got.State, got.Outcome, OutcomeInfraCancel)
	}
	as, _ := h.st.ListAttempts(ctx, f.tasks[0].ID)
	if countingAttempts(as) != 0 || len(as) != 1 || as[0].Outcome != model.OutcomeAbandoned {
		t.Fatalf("attempts = %+v, want one abandoned and none counting", as)
	}
	if s := h.statuses(f)["T1"]; s != model.StatusReady {
		t.Fatalf("task status = %s, want ready", s)
	}
	if s := h.eng.Runs().State(f.slug); s != model.RunStopped {
		t.Fatalf("state = %s", s)
	}
}

// Stop also closes stages whose job is still queued (the job is cancelled
// before it ever ran, so nothing else would write an outcome).
func TestRC3_StopClosesQueuedStages(t *testing.T) {
	h := newHarness(t)
	h.widthZ = true
	h.runHarness()
	f := h.runProject("proj", 2, true, "")
	ctx := context.Background()
	if err := h.eng.Runs().Start(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	stages := h.dispatchPass(f, "alpha")
	if len(stages) != 2 {
		t.Fatalf("dispatched %d", len(stages))
	}
	if err := h.eng.Runs().Stop(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	for _, sg := range stages {
		if got, _ := h.st.GetStage(ctx, sg.ID); got.Outcome != OutcomeInfraCancel {
			t.Fatalf("stage %d outcome = %q", sg.ID, got.Outcome)
		}
	}
	for k, s := range h.statuses(f) {
		if s != model.StatusReady {
			t.Fatalf("task %s = %s, want ready", k, s)
		}
	}
	if got := h.runAudit(f.slug); fmt.Sprint(got) != "[run.start:ann run.stop:ann]" {
		t.Fatalf("audit = %v", got)
	}
}

// AC-TM2: POST /api/brake on taskmaster's handler cancels the running stage
// (infra:cancel); a reconcile pass abandons the interrupted attempt and the
// task is ready again.
func TestTM2_BrakeCancelsRunningStage(t *testing.T) {
	h := newHarness(t)
	h.fake.Script("worker", runner.Step{Block: true})
	h.runHarness()
	f := h.runProject("proj", 1, true, "")
	ctx := context.Background()
	if err := h.eng.Runs().Start(ctx, "ann", f.slug); err != nil {
		t.Fatal(err)
	}
	stages := h.dispatchPass(f, "alpha")
	if len(stages) != 1 {
		t.Fatalf("dispatched %d", len(stages))
	}
	waitFor(t, "runner started", func() bool { return len(h.fake.CallsFor("worker")) == 1 })

	h.call(http.MethodPost, "/api/brake")
	defer h.call(http.MethodDelete, "/api/brake")
	st := h.waitDone(stages[0].ID)
	if st.Outcome != OutcomeInfraCancel {
		t.Fatalf("outcome = %q, want %s", st.Outcome, OutcomeInfraCancel)
	}
	if _, err := h.eng.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	as, _ := h.st.ListAttempts(ctx, f.tasks[0].ID)
	if countingAttempts(as) != 0 {
		t.Fatalf("attempts = %+v, want none counting", as)
	}
	if s := h.statuses(f)["T1"]; s != model.StatusReady {
		t.Fatalf("task status = %s, want ready", s)
	}
}
