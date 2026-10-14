package engine

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

const compileOne = `{"tasks":[{"id":"T001","phase":"P1","title":"first","prompt":"create a.txt","acceptance":["R1: a.txt exists"],"files_touched":["a.txt"]}]}`

// sandboxRunner runs worker stages the way the real adapter does: through
// sandbox.Command, in bwrap, with no credentials reachable. The script it
// runs fails if a credential path is visible or the host home is mounted, and
// otherwise answers with a valid worker reply that changes nothing. Every
// other role is the scripted fake.
type sandboxRunner struct {
	*runner.Fake
	git       *gitsvc.Service
	repo      string
	credPaths []string
	sandboxed atomic.Int32
	bwrap     atomic.Int32
}

func (r *sandboxRunner) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	if req.Role != "worker" {
		return r.Fake.Run(ctx, req)
	}
	home := filepath.Join(req.StageDir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		return runner.Result{}, err
	}
	admin, err := r.git.AdminGitDir(ctx, r.repo, req.Workdir)
	if err != nil {
		return runner.Result{}, err
	}
	script := `for p in "$@"; do if test -e "$p" || test -r "$p"; then echo "reachable: $p" >&2; exit 3; fi; done
test "$(ls /home)" = agent || { echo "host home is visible" >&2; exit 4; }
printf '%s' '{"summary":"sandboxed","files_changed":[],"learnings":[]}'`
	cmd, err := sandbox.Command(ctx, sandbox.Spec{
		Worktree: req.Workdir, RepoGitDir: filepath.Join(r.repo, ".git"), WorktreeGitDir: admin,
		StageHome: home, Net: true, Argv: append([]string{"/bin/sh", "-c", script, "sh"}, r.credPaths...),
	})
	if err != nil {
		return runner.Result{}, err
	}
	if filepath.Base(cmd.Path) == "bwrap" {
		r.bwrap.Add(1)
	}
	var out, errb strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return runner.Result{}, fmt.Errorf("sandboxed worker: %w: %s", err, errb.String())
	}
	r.sandboxed.Add(1)
	return runner.Result{Final: out.String()}, nil
}

func actorsOf(t *testing.T, st *store.Store, action string) []string {
	t.Helper()
	evs, err := st.ListEvents(context.Background(), "", "", 100000)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ev := range evs {
		if ev.Action == action {
			out = append(out, ev.Actor)
		}
	}
	return out
}

// AC-YO1 (engine), YO1, YO2, YO3, YO5, RC1: after enabling YOLO (which needs
// confirmation and is audited) and one GeneratePlan call, the effort goes from
// the saved FRD through plan approval, compile, queue approval and run start
// to a running worker with no further call. Every gate is audited with actor
// yolo. The worker stage ran through sandbox.Command in bwrap, with the
// credential paths of the B8 test (a fake home and the real one) unreachable.
// A task that gets stuck is retried once with a clean worktree and then waits
// for a human.
func TestC12_YOLOEffortRunsFromTheSavedFRDToASandboxedWorker(t *testing.T) {
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README"), "readme\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "initial")
	dataDir := t.TempDir()
	git, err := gitsvc.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	fastPoll(t)
	h := newHarness(t)
	sr := &sandboxRunner{Fake: h.fake, git: git, repo: repo}
	realHome, _ := os.UserHomeDir()
	fakeHome := t.TempDir()
	for _, home := range []string{fakeHome, realHome} {
		for _, rel := range []string{".ssh/id_ed25519", ".config/gh/hosts.yml", ".git-credentials"} {
			sr.credPaths = append(sr.credPaths, filepath.Join(home, rel))
		}
	}
	for _, p := range sr.credPaths[:3] {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, p, "secret")
	}
	h.tweak = func(d *Deps) { d.Git, d.DataDir, d.Runner = git, dataDir, sr }
	h.start()
	f := seedPlanFix(t, h, repo)
	f.standardPool()
	f.candidates("global", "", "worker", "gpu/m1")
	f.candidates("global", "", "reviewer", "gpu/m2")
	ctx := context.Background()
	if err := f.st.UpdateSettings(ctx, "t", map[string]string{"max_attempts": "2", "stuck_threshold": "2"}); err != nil {
		t.Fatal(err)
	}
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.fake.Script("compiler", runner.Step{Reply: compileOne})
	f.startEngine()

	if err := f.eng.SetYOLO(ctx, "alice", f.effort, true, false); !errors.Is(err, store.ErrInvalid) {
		t.Fatalf("enabling without confirmation = %v, want invalid", err)
	}
	if ef, _ := f.st.GetEffort(ctx, f.effort); ef.YOLO {
		t.Fatal("YOLO was enabled without confirmation")
	}
	if err := f.eng.SetYOLO(ctx, "alice", f.effort, true, true); err != nil {
		t.Fatal(err)
	}
	if got := actorsOf(t, f.st, "yolo.enable"); !slices.Equal(got, []string{"alice"}) {
		t.Fatalf("yolo.enable actors = %v", got)
	}

	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	resets := func() []string { return actorsOf(t, f.st, "task.reset") }
	waitFor(t, "four sandboxed worker runs and the task stuck again after one retry", func() bool {
		if sr.sandboxed.Load() < 4 || len(resets()) != 1 {
			return false
		}
		ts, err := f.st.ListTasks(ctx, f.effort)
		if err != nil || len(ts) != 1 {
			return false
		}
		set, _ := f.st.EffectiveSettings(ctx, f.slug)
		st, err := f.st.TaskStatuses(ctx, f.effort, f.eng.limitsFor(set))
		return err == nil && st[ts[0].Key] == model.StatusStuck
	})

	for _, action := range []string{"doc.approve", "compile.start", "queue.approve", "run.start", "task.reset"} {
		if got := actorsOf(t, f.st, action); len(got) != 1 || got[0] != yoloActor {
			t.Fatalf("%s actors = %v, want exactly [yolo]", action, got)
		}
	}
	if ef, _ := f.st.GetEffort(ctx, f.effort); ef.Status != model.EffortRunning || !ef.YOLO {
		t.Fatalf("effort = %+v, want running with YOLO still on", ef)
	}
	workers := 0
	for _, st := range f.effortAttemptStages() {
		if st.Role == "worker" {
			workers++
		}
	}
	if n := int(sr.bwrap.Load()); n != workers || n != int(sr.sandboxed.Load()) || n < 4 {
		t.Fatalf("worker stages = %d, via sandbox.Command/bwrap = %d, credential-free runs = %d", workers, n, sr.sandboxed.Load())
	}
	// The one automatic retry asked for a clean worktree.
	evs, err := f.st.ListEvents(ctx, "task", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	clean := 0
	for _, ev := range evs {
		if ev.Action == "task.reset" && strings.Contains(ev.DetailJSON, `"clean":true`) {
			clean++
		}
	}
	if clean != 1 {
		t.Fatalf("clean resets = %d, want 1", clean)
	}
}

// YO1, YO4, YO5, RC1: turning YOLO off while the compiler runs makes the queue
// gate wait for a human, while the plan approval already made stands. Enabling
// it again changes nothing for a queue that is already approved: the run does
// not start by itself.
func TestC12_YOLOOffMidRunMakesTheNextGateWait(t *testing.T) {
	f := newPlanFix(t, false)
	f.standardPool()
	ctx := context.Background()
	f.fake.Script("planner", runner.Step{Reply: planReply})
	f.fake.Script("compiler", runner.Step{Reply: compileClean, Edit: func(runner.Request) error {
		return f.eng.SetYOLO(ctx, "alice", f.effort, false, false)
	}})
	f.startEngine()
	if err := f.eng.SetYOLO(ctx, "alice", f.effort, true, true); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.GeneratePlan(ctx, "alice", f.effort); err != nil {
		t.Fatal(err)
	}
	f.waitStatus(model.EffortQueueDraft)
	if err := f.eng.AdvancePlans(ctx); err != nil { // waits for the pass that just saved the draft
		t.Fatal(err)
	}
	a, err := f.st.ApprovedRevision(ctx, f.effort, model.DocPlan)
	if err != nil || a.ApprovedBy != yoloActor {
		t.Fatalf("plan approval = %+v (%v), want the earlier gate approved by yolo", a, err)
	}
	if q, err := f.st.GetQueue(ctx, f.effort); err != nil || q.Status != model.QueueDraft {
		t.Fatalf("queue = %+v (%v), want a draft waiting for a human", q, err)
	}
	if got := actorsOf(t, f.st, "queue.approve"); len(got) != 0 {
		t.Fatalf("queue approved by %v after YOLO was turned off", got)
	}
	if got := actorsOf(t, f.st, "yolo.disable"); !slices.Equal(got, []string{"alice"}) {
		t.Fatalf("yolo.disable actors = %v", got)
	}
	q, _ := f.st.GetQueue(ctx, f.effort)
	if _, err := f.eng.ApproveQueue(ctx, "alice", f.effort, q.Hash); err != nil {
		t.Fatal(err)
	}
	if st := f.eng.Runs().State(f.slug); st != model.RunStopped {
		t.Fatalf("run state = %s: approving by hand with YOLO off must not start the run", st)
	}
	if err := f.eng.SetYOLO(ctx, "alice", f.effort, true, true); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.AdvancePlans(ctx); err != nil {
		t.Fatal(err)
	}
	if st := f.eng.Runs().State(f.slug); st != model.RunStopped {
		t.Fatalf("run state = %s: YOLO must not start a project on its own (RC1)", st)
	}
}

// YO3: the YOLO code path calls nothing of the sandbox, no git push or
// instruction function, and none of the functions that change model scope,
// backends, candidates or limits. yolo.go, plan.go and compile.go hold every
// auto-approval; they must not import sandbox or gitsvc, name no such
// function, and not reach the engine's Git or Sandbox dependencies.
func TestYOLOCallsNoSandboxGitPushOrScopeOverride(t *testing.T) {
	forbidden := []string{
		"Command", "Override", "OverrideTask", "PushInstructions", "UpdateProject", "CreateProject",
		"SetRoleCandidates", "ClearProjectRoleCandidates", "UpdateBackend", "CreateBackend", "AddBackend",
		"EnableBackend", "RetireBackend", "UpdateSettings", "Git", "Sandbox",
	}
	fset := token.NewFileSet()
	for _, name := range []string{"yolo.go", "plan.go", "compile.go"} {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); strings.HasSuffix(p, "/sandbox") || strings.HasSuffix(p, "/gitsvc") {
				t.Errorf("%s imports %s", name, p)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if slices.Contains(forbidden, sel.Sel.Name) || strings.HasPrefix(sel.Sel.Name, "Push") {
					t.Errorf("%s: %s reaches %s", name, fset.Position(sel.Pos()), sel.Sel.Name)
				}
			}
			return true
		})
	}
	var ids []string
	for _, inv := range EnforcedInvariants() {
		ids = append(ids, inv.ID)
	}
	for _, want := range []string{"GIT2", "GIT4", "GIT5", "GIT6", "WC1", "WC2", "WC3", "MB5", "PP1"} {
		if !slices.Contains(ids, want) {
			t.Errorf("EnforcedInvariants lacks %s", want)
		}
	}
}
