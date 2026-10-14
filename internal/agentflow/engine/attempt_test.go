package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// git runs git in dir for a test with no user or system configuration.
func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=u", "-c", "user.email=u@example.com", "-C", dir}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// chainRun is a real taskmaster, store, git service and a project over a temp
// repository, with workers and reviewers scripted on the fake runner.
type chainRun struct {
	*harness
	repo   string
	slug   string
	effort int64
	tasks  []model.Task
}

// newChainRun registers a project over a fresh repository holding README and
// check.sh, approves the queue and starts the engine (not the run).
func newChainRun(t *testing.T, feedback []string, defs []model.TaskDef) *chainRun {
	t.Helper()
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README"), "readme\n")
	writeFile(t, filepath.Join(repo, "check.sh"), "if [ -e a.txt ] && [ -e b.txt ]; then echo 'both a.txt and b.txt exist'; exit 1; fi\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "initial")

	h := newHarness(t)
	dataDir := t.TempDir()
	git, err := gitsvc.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	h.tweak = func(d *Deps) { d.Git, d.DataDir = git, dataDir }
	h.start()
	t.Cleanup(h.eng.Close)
	h.addBackend("gpu", 2, false, false, "m1", "m2")
	h.candidates("global", "", "worker", "gpu/m1")
	h.candidates("global", "", "reviewer", "gpu/m2")

	ctx := context.Background()
	slug := "proj"
	if err := h.st.CreateProject(ctx, "t", store.Project{Slug: slug, RepoPath: repo, ModelScope: "any", FeedbackCmds: feedback}); err != nil {
		t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, defs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}
	tasks, err := h.st.ListTasks(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &chainRun{harness: h, repo: repo, slug: slug, effort: ef.ID, tasks: tasks}
}

// run starts the engine and the project's run.
func (c *chainRun) run() {
	c.t.Helper()
	ctx := context.Background()
	if err := c.eng.Start(ctx); err != nil {
		c.t.Fatal(err)
	}
	if err := c.eng.Runs().Start(ctx, "t", c.slug); err != nil {
		c.t.Fatal(err)
	}
}

// script queues n workers that each write <task>.txt in their worktree and n
// reviewers with verdict.
func (c *chainRun) script(n int, review string, edit func(t *testing.T, r runner.Request) error) {
	for i := 0; i < n; i++ {
		c.fake.Script("worker", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
			name := filepath.Base(r.Workdir)
			if edit != nil {
				if err := edit(c.t, r); err != nil {
					return err
				}
			}
			return os.WriteFile(filepath.Join(r.Workdir, name+".txt"), []byte("work of "+name+"\n"), 0o644)
		}})
		c.fake.Script("reviewer", runner.Step{Reply: review})
	}
}

func (c *chainRun) attempts(task int) []model.Attempt {
	c.t.Helper()
	as, err := c.st.ListAttempts(context.Background(), c.tasks[task].ID)
	if err != nil {
		c.t.Fatal(err)
	}
	return as
}

func chainDefs() []model.TaskDef {
	return []model.TaskDef{
		{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"}},
		{Key: "T2", Phase: "P", Title: "second", Prompt: "do", Acceptance: []string{"ok"}, DependsOn: []string{"T1"}},
		{Key: "T3", Phase: "P", Title: "third", Prompt: "do", Acceptance: []string{"ok"}},
	}
}

// AC-PX1, AC-ST2, AT1, V5, GIT3, ST4, AT8, GIT4: three tasks (T2 needs T1) run
// work, verify, review, commit and merge against a repository with a dirty
// main tree. The user's branch, tree and index come out byte-identical, the
// project repo shows nothing new, agentflow/<effort> holds all three tasks,
// every worktree is gone and the effort is complete.
func TestC6_HappyPathLeavesTheUserTreeUntouched(t *testing.T) {
	c := newChainRun(t, nil, chainDefs())
	writeFile(t, filepath.Join(c.repo, "README"), "readme, edited and uncommitted\n")
	writeFile(t, filepath.Join(c.repo, "scratch.txt"), "untracked\n")
	writeFile(t, filepath.Join(c.repo, "staged.txt"), "staged\n")
	gitT(t, c.repo, "add", "staged.txt")
	snapshot := func() string {
		var b strings.Builder
		for _, args := range [][]string{
			{"rev-parse", "HEAD"}, {"symbolic-ref", "HEAD"}, {"status", "--porcelain=v1", "-uall"},
			{"diff", "HEAD"}, {"ls-files", "--stage"}, {"stash", "list"}, {"rev-parse", "refs/heads/main"},
		} {
			b.WriteString(gitT(t, c.repo, args...) + "\n--\n")
		}
		for _, f := range []string{"README", "scratch.txt", "staged.txt"} {
			data, err := os.ReadFile(filepath.Join(c.repo, f))
			if err != nil {
				t.Fatal(err)
			}
			b.WriteString(string(data) + "\n--\n")
		}
		return b.String()
	}
	before := snapshot()

	c.script(3, reviewPass, func(t *testing.T, r runner.Request) error {
		if filepath.Base(r.Workdir) == "T2" {
			if _, err := os.Stat(filepath.Join(r.Workdir, "T1.txt")); err != nil {
				return fmt.Errorf("T2 was forked before T1 was merged: %w", err)
			}
		}
		return nil
	})
	c.run()
	waitFor(t, "effort complete", func() bool {
		ef, err := c.st.GetEffort(context.Background(), c.effort)
		return err == nil && ef.Status == model.EffortComplete
	})

	if after := snapshot(); after != before {
		t.Fatalf("user repo changed (AC-PX1/AC-ST2):\n--- before\n%s\n--- after\n%s", before, after)
	}
	ef, _ := c.st.GetEffort(context.Background(), c.effort)
	if ef.IntegrationBranch != "agentflow/"+effortName(c.effort) {
		t.Fatalf("integration branch = %q", ef.IntegrationBranch)
	}
	files := gitT(t, c.repo, "ls-tree", "-r", "--name-only", ef.IntegrationBranch)
	for _, f := range []string{"T1.txt", "T2.txt", "T3.txt", "README", "check.sh"} {
		if !strings.Contains(files, f) {
			t.Fatalf("%s missing from %s:\n%s", f, ef.IntegrationBranch, files)
		}
	}
	if got := gitT(t, c.repo, "show", ef.IntegrationBranch+":README"); got != "readme" {
		t.Fatalf("integration README = %q: the dirty user tree leaked into the branch", got)
	}
	log := gitT(t, c.repo, "log", "--format=%an <%ae>%n%B", ef.IntegrationBranch)
	for _, want := range []string{"agentflow <noreply@agentflow.local>", "Agentflow-Task: T1", "Agentflow-Task: T3", "Agentflow-Attempt: 1"} {
		if !strings.Contains(log, want) {
			t.Fatalf("commit log lacks %q:\n%s", want, log)
		}
	}
	// Every branch other than main is agentflow's integration branch: task
	// branches were deleted with their worktrees.
	if heads := gitT(t, c.repo, "for-each-ref", "--format=%(refname:short)", "refs/heads"); heads != "agentflow/"+effortName(c.effort)+"\nmain" {
		t.Fatalf("branches = %q", heads)
	}
	if wts := gitT(t, c.repo, "worktree", "list", "--porcelain"); strings.Count(wts, "worktree ") != 1 {
		t.Fatalf("worktrees remain:\n%s", wts)
	}
	if left, _ := os.ReadDir(filepath.Join(c.deps().DataDir, "worktrees", c.slug, effortName(c.effort))); len(left) != 0 {
		t.Fatalf("worktree dirs remain: %v", left)
	}

	for i := range c.tasks {
		as := c.attempts(i)
		if len(as) != 1 || as[0].Outcome != model.OutcomePassed || as[0].ReviewerModel != "m2" || as[0].WorkerModel != "m1" {
			t.Fatalf("task %d attempts = %+v", i, as)
		}
	}
	if t1, t2 := c.attempts(0)[0], c.attempts(1)[0]; t2.Started < t1.Finished {
		t.Fatalf("T2 started (%d) before T1 passed (%d)", t2.Started, t1.Finished)
	}
	if n := len(c.fake.CallsFor("worker")) + len(c.fake.CallsFor("reviewer")); n != 6 {
		t.Fatalf("runner calls = %d, want 6", n)
	}
	rev := c.fake.CallsFor("reviewer")[0].Prompt
	if !strings.Contains(rev, "Verify report") || !strings.Contains(rev, `"pass":true`) {
		t.Fatalf("reviewer prompt lacks the VerifyReport:\n%s", rev)
	}
	commit, _, err := c.st.LatestBaseline(context.Background(), c.effort)
	if err != nil || commit != gitT(t, c.repo, "rev-parse", ef.IntegrationBranch) {
		t.Fatalf("baseline commit = %q (%v), want the integration tip", commit, err)
	}
	cmds, err := c.eng.PushInstructions(context.Background(), c.effort)
	if err != nil || !strings.Contains(strings.Join(cmds, "\n"), "git merge --no-ff 'agentflow/") {
		t.Fatalf("push instructions = %v (%v)", cmds, err)
	}
	// A completed effort leaves nothing to run: the project reads stopped, in
	// memory and on the effort.
	waitFor(t, "run state stopped after completion", func() bool {
		ef, err := c.st.GetEffort(context.Background(), c.effort)
		return err == nil && ef.RunState == model.RunStopped && c.eng.Runs().State(c.slug) == model.RunStopped
	})
}

// V6, AT1: a verify failure (the worker changed nothing) closes the attempt as
// failed, never calls the reviewer, never merges, and keeps the worktree for
// the repair attempt (PX2).
func TestC6_VerifyFailureNeverCallsTheReviewer(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.fake.Script("worker", runner.Step{Reply: workerOK})
	c.run()
	waitFor(t, "attempt failed", func() bool {
		as := c.attempts(0)
		return len(as) > 0 && as[0].Outcome == model.OutcomeFailed
	})
	if n := len(c.fake.CallsFor("reviewer")); n != 0 {
		t.Fatalf("reviewer calls = %d, want 0", n)
	}
	stages, err := c.st.ListAttemptStages(context.Background(), c.attempts(0)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 || stages[1].Role != "verify" || stages[1].Outcome != OutcomeRepair || !strings.Contains(stages[1].RawOutput, "no_changes") {
		t.Fatalf("stages = %+v", stages)
	}
	if _, err := os.Stat(c.attempts(0)[0].Worktree); err != nil {
		t.Fatalf("worktree of a failed attempt must stay for the repair: %v", err)
	}
	if ef, _ := c.st.GetEffort(context.Background(), c.effort); ef.Status == model.EffortComplete {
		t.Fatal("effort completed without a passed task")
	}
}

// PX3, V5: both tasks pass verify in their own worktrees, but together they
// break a feedback command that was green at the baseline. The second merge's
// post-merge verify fails, the integration branch is reset to its pre-merge
// commit with a clean worktree, and that attempt ends failed for repair.
func TestC6_PostMergeVerifyFailureResetsTheIntegrationBranch(t *testing.T) {
	defs := []model.TaskDef{
		{Key: "a", Phase: "P", Title: "a", Prompt: "do", Acceptance: []string{"ok"}},
		{Key: "b", Phase: "P", Title: "b", Prompt: "do", Acceptance: []string{"ok"}},
	}
	c := newChainRun(t, []string{"sh check.sh"}, defs)
	c.script(2, reviewPass, nil)
	c.run()
	finished := func() int {
		n := 0
		for i := range c.tasks {
			for _, a := range c.attempts(i) {
				if a.Outcome != "" {
					n++
				}
			}
		}
		return n
	}
	waitFor(t, "both attempts finished", func() bool { return finished() == 2 })

	var passed, failed int
	for i := range c.tasks {
		switch c.attempts(i)[0].Outcome {
		case model.OutcomePassed:
			passed++
		case model.OutcomeFailed:
			failed++
		}
	}
	if passed != 1 || failed != 1 {
		t.Fatalf("passed=%d failed=%d, want 1 and 1", passed, failed)
	}
	ef, _ := c.st.GetEffort(context.Background(), c.effort)
	if ef.Status == model.EffortComplete {
		t.Fatal("effort completed with a failed task")
	}
	files := gitT(t, c.repo, "ls-tree", "-r", "--name-only", ef.IntegrationBranch)
	if strings.Contains(files, "a.txt") == strings.Contains(files, "b.txt") {
		t.Fatalf("integration branch must hold exactly one of a.txt and b.txt:\n%s", files)
	}
	intWT := filepath.Join(c.deps().DataDir, "worktrees", c.slug, effortName(c.effort), "_integration")
	if st := gitT(t, intWT, "status", "--porcelain"); st != "" {
		t.Fatalf("integration worktree is dirty after the reset:\n%s", st)
	}
	if head, tip := gitT(t, intWT, "rev-parse", "HEAD"), gitT(t, c.repo, "rev-parse", ef.IntegrationBranch); head != tip {
		t.Fatalf("integration worktree HEAD %s != branch tip %s", head, tip)
	}
	var mergeStage model.Stage
	for i := range c.tasks {
		stages, _ := c.st.ListAttemptStages(context.Background(), c.attempts(i)[0].ID)
		if last := stages[len(stages)-1]; last.Role == "merge" && last.Outcome == OutcomeRepair {
			mergeStage = last
		}
	}
	var m mergeReport
	if err := json.Unmarshal(mergeStage.Parsed, &m); err != nil || m.Kind != "verify" || m.Report == nil || len(m.Report.NewFailures) == 0 {
		t.Fatalf("failed merge stage = %+v (%v)", mergeStage, err)
	}
}

func (h *harness) deps() Deps { return h.eng.deps }

// The reviewer must be handed the evidence: the task JSON filled in, the
// worker's log, and the content of the touched files (FILES), so a model with
// weak tool use does not have to open files to judge them. A missing file is
// marked so it is not guessed at.
func TestReviewerPromptCarriesTheFiles(t *testing.T) {
	defs := []model.TaskDef{{Key: "T1", Phase: "P", Title: "first", Prompt: "do", Acceptance: []string{"ok"},
		FilesTouched: []string{"T1.txt", "absent.go", "../escape"}}}
	c := newChainRun(t, nil, defs)
	c.script(1, reviewPass, nil)
	c.run()
	waitFor(t, "reviewer called", func() bool { return len(c.fake.CallsFor("reviewer")) > 0 })
	raw, err := os.ReadFile(c.fake.CallsFor("reviewer")[0].Request.PromptPath)
	if err != nil {
		t.Fatal(err)
	}
	p := string(raw)
	for _, want := range []string{"## FILES", "### T1.txt", "work of T1", "MISSING", "### absent.go", `"key":"T1"`} {
		if !strings.Contains(p, want) {
			t.Errorf("reviewer prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "{{TASK_JSON}}") || strings.Contains(p, "### ../escape") {
		t.Errorf("reviewer prompt has an unfilled placeholder or an escaped path:\n%s", p)
	}
}
