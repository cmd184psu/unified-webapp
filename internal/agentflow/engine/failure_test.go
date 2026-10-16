package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
)

// taskStatus derives the first task's status the way the dispatcher does.
func (c *chainRun) taskStatus() model.Status {
	c.t.Helper()
	ctx := context.Background()
	set, err := c.st.EffectiveSettings(ctx, c.slug)
	if err != nil {
		c.t.Fatal(err)
	}
	st, err := c.st.TaskStatuses(ctx, c.effort, c.eng.limitsFor(set))
	if err != nil {
		c.t.Fatal(err)
	}
	return st[c.tasks[0].Key]
}

// runAttempts lets the dispatcher start exactly n more attempts of the first
// task, one at a time, waiting for the project to drain between them. It
// returns the task's status after each.
func (c *chainRun) runAttempts(n int) []model.Status {
	c.t.Helper()
	ctx := context.Background()
	if err := c.eng.Start(ctx); err != nil {
		c.t.Fatal(err)
	}
	var out []model.Status
	for i := 0; i < n; i++ {
		want := len(c.attempts(0)) + 1
		if err := c.eng.Runs().RunOne(ctx, "t", c.slug); err != nil {
			c.t.Fatal(err)
		}
		waitFor(c.t, "attempt closed and project drained", func() bool {
			as := c.attempts(0)
			return len(as) == want && as[want-1].Outcome != "" && c.eng.Runs().State(c.slug) == model.RunPaused
		})
		out = append(out, c.taskStatus())
	}
	return out
}

func (c *chainRun) setLimits(kv map[string]string) {
	c.t.Helper()
	if err := c.st.UpdateSettings(context.Background(), "t", kv); err != nil {
		c.t.Fatal(err)
	}
}

func (c *chainRun) task0() model.Task {
	c.t.Helper()
	tk, err := c.st.GetTask(context.Background(), c.tasks[0].ID)
	if err != nil {
		c.t.Fatal(err)
	}
	return tk
}

func writeWork(r runner.Request) error {
	return os.WriteFile(filepath.Join(r.Workdir, filepath.Base(r.Workdir)+".txt"), []byte("work of "+filepath.Base(r.Workdir)+"\n"), 0o644)
}

// AC-AT1 (AT4): the same verify failure three times halts the task as stuck
// after attempt 3. After attempts 1 and 2 the derived status is repair, never
// a failed state, and each repair attempt reuses the first attempt's worktree.
func TestC7_ACAT1_SameVerifyFailureThriceIsStuck(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	for i := 0; i < 3; i++ {
		c.fake.Script("worker", runner.Step{Reply: workerOK}) // changes nothing: verify no_changes
	}
	got := c.runAttempts(3)
	want := []model.Status{model.StatusRepair, model.StatusRepair, model.StatusStuck}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("status after attempt %d = %s, want %s (all: %v)", i+1, got[i], want[i], got)
		}
	}
	as := c.attempts(0)
	if as[0].Kind != model.AttemptNormal || as[1].Kind != model.AttemptRepair || as[2].Kind != model.AttemptRepair {
		t.Fatalf("attempt kinds = %s %s %s", as[0].Kind, as[1].Kind, as[2].Kind)
	}
	if as[0].Fingerprint == "" || as[0].Fingerprint != as[1].Fingerprint || as[1].Fingerprint != as[2].Fingerprint {
		t.Fatalf("fingerprints = %q %q %q", as[0].Fingerprint, as[1].Fingerprint, as[2].Fingerprint)
	}
	if as[0].Worktree != as[2].Worktree {
		t.Fatalf("repair attempts left the worktree: %s vs %s", as[0].Worktree, as[2].Worktree)
	}
	if n := len(c.fake.CallsFor("reviewer")); n != 0 {
		t.Fatalf("reviewer calls = %d, want 0 (V6)", n)
	}
	if r := c.task0().WaitingReason; !strings.HasPrefix(r, "stuck") {
		t.Fatalf("waiting reason = %q", r)
	}
	if err := c.eng.DispatchOnce(context.Background()); err != nil || len(c.attempts(0)) != 3 {
		t.Fatalf("a stuck task was dispatched again: %v, %d attempts", err, len(c.attempts(0)))
	}
}

// AT5: max_attempts different failures halt the task as exhausted.
func TestC7_AT5_MaxAttemptsExhausts(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.setLimits(map[string]string{"max_attempts": "2", "stuck_threshold": "2"})
	c.fake.Script("worker", runner.Step{Reply: workerOK}, runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: `{"verdict":"repair","summary":"no","issues":[{"description":"wrong","severity":"major"}],"repair_prompt":"fix it"}`})
	got := c.runAttempts(2)
	if got[0] != model.StatusRepair || got[1] != model.StatusExhausted {
		t.Fatalf("statuses = %v", got)
	}
	as := c.attempts(0)
	if as[0].Fingerprint == as[1].Fingerprint || as[0].Fingerprint[:2] != "v:" || as[1].Fingerprint[:2] != "r:" {
		t.Fatalf("fingerprints = %q %q", as[0].Fingerprint, as[1].Fingerprint)
	}
}

// AT6 (H12): a worker that is blocked twice in a row halts the task as
// needs_input with the worker's reason; the first claim goes to another model.
func TestC7_AT6_BlockedWorkerNeedsInput(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	blocked := runner.Step{Reply: `{"summary":"stopped","files_changed":[],"learnings":[],"blocked":"need the staging API key"}`}
	c.fake.Script("worker", blocked, blocked)
	got := c.runAttempts(2)
	if got[0] != model.StatusRepair || got[1] != model.StatusNeedsInput {
		t.Fatalf("statuses = %v, want repair then needs_input", got)
	}
	if r := c.task0().WaitingReason; r != "need the staging API key" {
		t.Fatalf("waiting reason = %q", r)
	}
	as := c.attempts(0)
	if as[0].Outcome != model.OutcomeFailed || as[0].Fingerprint != "blocked" || as[1].Outcome != model.OutcomeBlocked {
		t.Fatalf("attempts = %+v", as)
	}
}

// H12: the first claim of being blocked is checked by another model, which is
// told the claim may be a mistake; if it does the work the owner is never asked.
func TestH12_FirstBlockGoesToAnotherModel(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.addBackend("gpu2", 1, false, false, "m3", "m4")
	c.candidates("global", "", "worker", "gpu/m1", "gpu/m2")
	c.candidates("global", "", "reviewer", "gpu2/m4")
	c.fake.Script("worker",
		runner.Step{Reply: `{"summary":"stopped","files_changed":[],"learnings":[],"blocked":"The file internal/api/main.go does not exist."}`},
		runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	c.run()
	waitFor(t, "task passed", func() bool { return c.taskStatus() == model.StatusPassed })
	as := c.attempts(0)
	if len(as) != 2 || as[0].WorkerModel != "m1" || as[1].WorkerModel != "m2" {
		t.Fatalf("attempts = %+v: the second should have gone to m2", as)
	}
	calls := c.fake.CallsFor("worker")
	if len(calls) != 2 || !strings.Contains(calls[1].Prompt, "said it was blocked") || !strings.Contains(calls[1].Prompt, "internal/api/main.go") {
		t.Fatalf("the second prompt does not carry the doubtful claim: %d calls", len(calls))
	}
}

// AC-AT3 (AT2, AT10) and AC-OC1 (engine half): the repair attempt's prompt
// carries the reviewer's issue, the verify check name, the attempt-1 progress
// entry and the previous diff; with a 500 KB repair prompt it still reaches
// the runner intact, by path.
func TestC7_ACAT3_RepairPromptCarriesEvidence_ACOC1_500KB(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	big := strings.Repeat("Release the handle before returning. ", (500<<10)/37+1)
	review, _ := json.Marshal(schema.ReviewOut{
		Verdict: "repair", Summary: "leaks", RepairPrompt: big,
		Issues: []schema.Issue{{Description: "the frobnicator leaks its handle", Severity: "major", File: "T1.txt", Line: 3}},
	})
	c.fake.Script("worker",
		runner.Step{Reply: `{"summary":"added T1","files_changed":["T1.txt"],"learnings":["the cache key needs the tenant id"]}`, Edit: writeWork},
		runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: string(review)}, runner.Step{Reply: reviewPass})
	c.run()
	waitFor(t, "task passed", func() bool { return c.taskStatus() == model.StatusPassed })

	calls := c.fake.CallsFor("worker")
	if len(calls) != 2 {
		t.Fatalf("worker calls = %d", len(calls))
	}
	p1, p2 := calls[0].Prompt, calls[1].Prompt
	if strings.Contains(p1, "Progress log") || strings.Contains(p1, "Repair attempt") {
		t.Fatal("the first attempt's prompt carries repair context")
	}
	for _, want := range []string{
		"the frobnicator leaks its handle",  // reviewer issue
		verify.CheckNoChanges,               // verify check name from the report
		"the cache key needs the tenant id", // attempt-1 progress entry
		"+work of T1",                       // previous diff, new file included
		"Release the handle before returning.",
	} {
		if !strings.Contains(p2, want) {
			t.Fatalf("attempt 2 prompt lacks %q", want)
		}
	}
	if len(p2) < 500<<10 {
		t.Fatalf("prompt = %d bytes, want at least 500 KB", len(p2))
	}
	req := calls[1].Request
	if st, err := os.Stat(req.PromptPath); err != nil || st.Size() != int64(len(p2)) {
		t.Fatalf("prompt.md = %v, %v", st, err)
	}
	if len(req.Message) > 200 || calls[1].PromptErr != nil {
		t.Fatalf("the prompt must travel by path, message %d bytes, err %v", len(req.Message), calls[1].PromptErr)
	}
	as := c.attempts(0)
	if len(as) != 2 || as[0].Outcome != model.OutcomeFailed || as[1].Kind != model.AttemptRepair || as[1].Outcome != model.OutcomePassed {
		t.Fatalf("attempts = %+v", as)
	}
	entries, err := c.st.ListProgress(context.Background(), c.tasks[0].ID, 10)
	if err != nil || len(entries) != 2 || !strings.Contains(entries[0], "failed") || !strings.Contains(entries[1], "passed") {
		t.Fatalf("progress = %q (%v)", entries, err)
	}
}

// AC-AT2 (AT7, H8): the runner reports the backend unreachable mid-stage. The
// attempt count is unchanged and the stage is retried at once on the other
// backend; an unreachable host is nobody's fault, so no infra retry is counted.
func TestC7_ACAT2_UnreachableRetriesOnAnotherBackend(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.addBackend("gpu2", 1, false, false, "m3", "m4")
	c.candidates("global", "", "worker", "gpu/m1", "gpu2/m3")
	c.candidates("global", "", "reviewer", "gpu/m2", "gpu2/m4") // gpu is suspended by then (H8)
	c.fake.Script("worker",
		runner.Step{ErrClass: runner.ErrUnreachable, Err: os.ErrDeadlineExceeded},
		runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	c.run()
	ctx := context.Background()
	waitFor(t, "task passed", func() bool { return c.taskStatus() == model.StatusPassed })

	ss, err := c.st.ListAttemptStages(ctx, c.attempts(0)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if ss[0].Backend != "gpu" || ss[0].Outcome != OutcomeInfraUnreachable || ss[1].Role != "worker" || ss[1].Backend != "gpu2" || ss[1].Model != "m3" {
		t.Fatalf("stages = %+v", ss)
	}
	if as := c.attempts(0); len(as) != 1 || as[0].N != 1 || as[0].Outcome != model.OutcomePassed {
		t.Fatalf("attempts = %+v", as)
	} else if as[0].WorkerModel != "m3" {
		t.Fatalf("the attempt names worker %q, but m3 did the work", as[0].WorkerModel)
	}
	ins, err := c.st.TaskInputs(ctx, c.effort)
	if err != nil || len(ins[0].InfraIDs) != 0 {
		t.Fatalf("an unreachable host was counted as %v infra retries (%v)", ins[0].InfraIDs, err)
	}
}

// AT7: infrastructure failures stop at max_infra_retries with needs_attention,
// the attempts they interrupted never count, and the reason is shown.
func TestC7_AT7_InfraRetriesExhaustNeedsAttention(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.setLimits(map[string]string{"max_infra_retries": "2"})
	c.eng.deps.Clock = func() time.Time { return time.Now().Add(time.Hour) }
	crash := runner.Step{ErrClass: runner.ErrCrash, Err: os.ErrInvalid}
	c.fake.Script("worker", crash, crash, crash)
	c.run()
	waitFor(t, "needs_attention", func() bool { return c.taskStatus() == model.StatusNeedsAttention })
	// The status flips when the last strike is recorded; the attempt is
	// abandoned a moment later.
	waitFor(t, "attempt abandoned", func() bool { as := c.attempts(0); return len(as) == 1 && as[0].Outcome != "" })
	if n := len(c.fake.CallsFor("worker")); n != 2 {
		t.Fatalf("worker calls = %d, want 2", n)
	}
	as := c.attempts(0)
	if len(as) != 1 || as[0].Outcome != model.OutcomeAbandoned {
		t.Fatalf("attempts = %+v", as)
	}
	if r := c.task0().WaitingReason; !strings.Contains(r, "2 infrastructure failures") {
		t.Fatalf("waiting reason = %q", r)
	}
}

// AT7: a stop is not the backend's fault; it counts no infra retry.
func TestC7_AT7_StopDoesNotCountAsInfraRetry(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.fake.Script("worker", runner.Step{Block: true})
	c.run()
	waitFor(t, "worker running", func() bool { return len(c.fake.CallsFor("worker")) == 1 })
	ctx := context.Background()
	if err := c.eng.Runs().Stop(ctx, "t", c.slug); err != nil {
		t.Fatal(err)
	}
	ss, err := c.st.ListAttemptStages(ctx, c.attempts(0)[0].ID)
	if err != nil || len(ss) != 1 || ss[0].Outcome != OutcomeInfraCancel {
		t.Fatalf("stages = %+v (%v)", ss, err)
	}
	if err := c.eng.Advance(ctx); err != nil {
		t.Fatal(err)
	}
	ins, err := c.st.TaskInputs(ctx, c.effort)
	if err != nil || len(ins[0].InfraIDs) != 0 {
		t.Fatalf("infra retries = %v (%v)", ins[0].InfraIDs, err)
	}
	if as := c.attempts(0); len(as) != 1 || as[0].Outcome != model.OutcomeAbandoned {
		t.Fatalf("attempts = %+v", as)
	}
}

func gitErr(dir string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-c", "user.name=u", "-c", "user.email=u@example.com", "-C", dir}, args...)...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	if out, err := cmd.CombinedOutput(); err != nil {
		return &exec.ExitError{Stderr: out}
	}
	return nil
}

// AC-GIT1 (GIT5): a worker that runs `git commit -am x` in its worktree (what
// a model with a shell would do) is a policy violation: the commit is
// reverted, audited and flagged on the stage, the attempt closes as
// policy_violation, and the task branch holds only engine commits. Two
// violations (the limit set here) halt the task as needs_attention.
func TestC7_ACGIT1_WorkerCommitIsAViolation(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.eng.deps.MaxPolicyViolations = 2
	commit := runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		if err := writeWork(r); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(r.Workdir, "README"), []byte("changed by the model\n"), 0o644); err != nil {
			return err
		}
		return gitErr(r.Workdir, "commit", "-q", "-am", "x")
	}}
	c.fake.Script("worker", commit, commit, commit)
	c.run()
	waitFor(t, "needs_attention", func() bool { return c.taskStatus() == model.StatusNeedsAttention })
	if err := c.eng.DispatchOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	as := c.attempts(0)
	if len(as) != 2 || as[0].Outcome != model.OutcomePolicyViolation || as[1].Outcome != model.OutcomePolicyViolation {
		t.Fatalf("attempts = %+v", as)
	}
	ctx := context.Background()
	ss, err := c.st.ListAttemptStages(ctx, as[0].ID)
	if err != nil || len(ss) != 1 || ss[0].Outcome != OutcomePolicyViolation || !strings.Contains(ss[0].RawOutput, "policy violation (reverted)") {
		t.Fatalf("stages = %+v (%v)", ss, err)
	}
	evs, err := c.st.ListEvents(ctx, "stage", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Action == "policy.violation" {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("policy.violation events = %d, want 2", n)
	}
	branch := "agentflow-task/" + effortName(c.effort) + "/T1"
	if log := gitT(t, c.repo, "log", "--format=%s", branch); log != "initial" {
		t.Fatalf("task branch history = %q, want only the base commit", log)
	}
	if n := len(c.fake.CallsFor("reviewer")); n != 0 {
		t.Fatalf("reviewer calls = %d", n)
	}
	if r := c.task0().WaitingReason; !strings.HasPrefix(r, "needs attention") {
		t.Fatalf("waiting reason = %q", r)
	}
}

// AT4, AT7: fingerprints ignore digits, paths and hex ids; backoff doubles
// from 10 s up to 5 min.
func TestC7_FingerprintsAndBackoff(t *testing.T) {
	rep := func(detail string) verify.VerifyReport {
		return verify.VerifyReport{Checks: []verify.Check{{Name: "go test", Details: []string{detail}}}}
	}
	a := verifyFingerprint(rep("pkg/a_test.go:42:7: expected 3, got 4 in /tmp/wt-12345/x at 0xdeadbeef"))
	b := verifyFingerprint(rep("pkg/a_test.go:99:1: expected 5, got 6 in /var/other/9/x at 0xcafef00d"))
	if a != b {
		t.Fatalf("same failure fingerprints differently: %s vs %s", a, b)
	}
	if c := verifyFingerprint(rep("pkg/a_test.go:42:7: undefined: foo")); c == a {
		t.Fatal("different failure fingerprints the same")
	}
	r1 := schema.ReviewOut{Issues: []schema.Issue{{Description: "leak at line 12", File: "a.go"}, {Description: "no test", File: "b.go"}}}
	r2 := schema.ReviewOut{Issues: []schema.Issue{{Description: "No  test", File: "b.go"}, {Description: "leak at line 99", File: "a.go"}}}
	if reviewFingerprint(r1) != reviewFingerprint(r2) {
		t.Fatal("issue order or numbers changed the review fingerprint")
	}
	want := []time.Duration{10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 300 * time.Second, 300 * time.Second}
	for i, w := range want {
		if got := infraBackoff(i); got != w {
			t.Fatalf("infraBackoff(%d) = %s, want %s", i, got, w)
		}
	}
}

// H11: after an unreadable reply the stage is retried on another model, not on
// the one that just produced it, even when that model is listed first.
func TestH11_RetryAfterGibberishUsesAnotherModel(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.addBackend("gpu2", 1, false, false, "m3", "m4")
	c.candidates("global", "", "worker", "gpu/m1", "gpu/m2")
	c.candidates("global", "", "reviewer", "gpu2/m4")
	c.fake.Script("worker",
		runner.Step{Reply: "Sorry, I can't do that."},
		runner.Step{Reply: workerOK, Edit: writeWork})
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	c.run()
	waitFor(t, "task passed", func() bool { return c.taskStatus() == model.StatusPassed })
	ss, err := c.st.ListAttemptStages(context.Background(), c.attempts(0)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if ss[0].Outcome != OutcomeInfraUnparseable || ss[0].Model != "m1" || ss[1].Role != "worker" || ss[1].Model != "m2" {
		t.Fatalf("stages = %+v: the retry should have gone to m2", ss)
	}
	if as := c.attempts(0); as[0].WorkerModel != "m2" {
		t.Fatalf("the attempt names worker %q, want m2", as[0].WorkerModel)
	}
}
