package gitsvc

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// engineTask makes a task worktree with one engine commit and returns it.
func engineTask(t *testing.T, s *Service, repo, task string, head HeadInfo) Worktree {
	t.Helper()
	wt, err := s.AddTaskWorktree(bg, repo, "p", "e", task, head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	write(t, wt.Path, task+".txt", "engine work\n")
	if _, err := s.CommitAll(bg, repo, wt.Path, "work", Trailers{Task: task, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	return wt
}

func integration(t *testing.T, s *Service, repo string, head HeadInfo) Worktree {
	t.Helper()
	br, err := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, "")
	if err != nil {
		t.Fatal(err)
	}
	wt, err := s.AddIntegrationWorktree(bg, repo, "p", "e", br)
	if err != nil {
		t.Fatal(err)
	}
	return wt
}

// TestDetectAndRevertCommit covers AC-GIT1 (unit): a simulated model runs
// `git commit -am x`; one violation, the commit is reverted, and the task
// branch holds only commits authored by agentflow.
func TestDetectAndRevertCommit(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt := engineTask(t, s, repo, "t1", head)
	engineHead := tgit(t, wt.Path, "rev-parse", "HEAD")

	snap, err := s.Snapshot(bg, repo, wt.Path)
	if err != nil {
		t.Fatal(err)
	}
	write(t, wt.Path, "t1.txt", "model edit\n")
	tgit(t, wt.Path, "commit", "-q", "-am", "x")

	v, err := s.DetectAndRevert(bg, snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 1 || v[0].Kind != ViolationHead {
		t.Fatalf("violations = %+v, want exactly one head violation", v)
	}
	if got := tgit(t, wt.Path, "rev-parse", "HEAD"); got != engineHead {
		t.Fatalf("HEAD = %s, want %s", got, engineHead)
	}
	if got := tgit(t, wt.Path, "rev-parse", wt.Branch); got != engineHead {
		t.Fatalf("branch = %s, want %s", got, engineHead)
	}
	for _, a := range strings.Fields(tgit(t, repo, "log", "--format=%an", head.Commit+".."+wt.Branch)) {
		if a != "agentflow" {
			t.Fatalf("task branch has a commit by %q", a)
		}
	}
	// The model's file edit survives as an unstaged change and the engine commits it.
	if st := tgit(t, wt.Path, "status", "--porcelain"); st != "M t1.txt" && st != " M t1.txt" {
		t.Fatalf("status = %q", st)
	}
	if _, err := s.CommitAll(bg, repo, wt.Path, "fix", Trailers{Task: "t1", Attempt: 2}); err != nil {
		t.Fatal(err)
	}
	if v, err := s.DetectAndRevert(bg, snap); err == nil && len(v) == 0 {
		t.Fatal("a later engine commit must differ from the old snapshot")
	}
}

func TestDetectAndRevertCleanStage(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt := engineTask(t, s, repo, "t1", head)
	snap, _ := s.Snapshot(bg, repo, wt.Path)
	write(t, wt.Path, "new.txt", "only a file edit\n")
	tgit(t, wt.Path, "status")
	tgit(t, wt.Path, "diff")
	if v, err := s.DetectAndRevert(bg, snap); err != nil || len(v) != 0 {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	if _, err := os.Stat(filepath.Join(wt.Path, "new.txt")); err != nil {
		t.Fatal("file edit was lost")
	}
}

func TestDetectAndRevertKinds(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt := engineTask(t, s, repo, "t1", head)
	other := engineTask(t, s, repo, "t2", head)
	engineHead := tgit(t, wt.Path, "rev-parse", "HEAD")

	cases := []struct {
		name string
		kind string
		act  func()
	}{
		{"add", ViolationIndex, func() {
			write(t, wt.Path, "staged.txt", "s\n")
			tgit(t, wt.Path, "add", "staged.txt")
		}},
		{"rm", ViolationIndex, func() { tgit(t, wt.Path, "rm", "-q", "-f", "t1.txt") }},
		{"reset", ViolationHead, func() { tgit(t, wt.Path, "reset", "-q", "--soft", head.Commit) }},
		{"checkout-branch", ViolationHead, func() { tgit(t, wt.Path, "checkout", "-q", "-b", "rogue") }},
		{"detach", ViolationHead, func() { tgit(t, wt.Path, "checkout", "-q", "--detach", head.Commit) }},
		{"merge", ViolationMerge, func() {
			tgit(t, wt.Path, "merge", "-q", "--no-commit", "--no-ff", other.Branch)
		}},
	}
	for _, c := range cases {
		snap, err := s.Snapshot(bg, repo, wt.Path)
		if err != nil {
			t.Fatal(err)
		}
		c.act()
		v, err := s.DetectAndRevert(bg, snap)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		found := false
		for _, x := range v {
			found = found || x.Kind == c.kind
		}
		if !found {
			t.Fatalf("%s: violations %+v lack %s", c.name, v, c.kind)
		}
		after, _ := s.Snapshot(bg, repo, wt.Path)
		if after.headRef != snap.headRef || after.headCommit != engineHead || after.index != snap.index {
			t.Fatalf("%s: state not restored: %+v", c.name, after)
		}
		for _, f := range stateFiles {
			if after.markers[f] {
				t.Fatalf("%s: %s left behind", c.name, f)
			}
		}
		if got := tgit(t, wt.Path, "symbolic-ref", "HEAD"); got != "refs/heads/"+wt.Branch {
			t.Fatalf("%s: HEAD = %s", c.name, got)
		}
		os.Remove(filepath.Join(wt.Path, "staged.txt"))
		tgit(t, wt.Path, "checkout", "-q", "--", ".")
	}
}

// TestSnapshotIgnoresConcurrentEngineWrites: other tasks' commits, merges and
// new branches while a stage runs are not violations and are never reverted.
func TestSnapshotIgnoresConcurrentEngineWrites(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt := engineTask(t, s, repo, "t1", head)
	other := engineTask(t, s, repo, "t2", head)
	integ := integration(t, s, repo, head)

	snap, _ := s.Snapshot(bg, repo, wt.Path)
	write(t, other.Path, "more.txt", "m\n")
	if _, err := s.CommitAll(bg, repo, other.Path, "more", Trailers{Task: "t2", Attempt: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, other.Branch); err != nil {
		t.Fatal(err)
	}
	tgit(t, repo, "branch", "agentflow-task/e/new", head.Commit)
	otherHead := tgit(t, repo, "rev-parse", other.Branch)
	integHead := tgit(t, repo, "rev-parse", integ.Branch)

	if v, err := s.DetectAndRevert(bg, snap); err != nil || len(v) != 0 {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	if tgit(t, repo, "rev-parse", other.Branch) != otherHead || tgit(t, repo, "rev-parse", integ.Branch) != integHead {
		t.Fatal("engine writes were reverted")
	}
	tgit(t, repo, "rev-parse", "--verify", "agentflow-task/e/new")
}

func TestSnapshotRefusesUnregistered(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	if _, err := s.Snapshot(bg, repo, filepath.Join(s.WorktreeRoot(), "nope")); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := s.Snapshot(bg, repo, repo); !errors.Is(err, ErrOutsideDataDir) {
		t.Fatalf("err = %v", err)
	}
}

func TestMergeClean(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	t1 := engineTask(t, s, repo, "t1", head)
	t2 := engineTask(t, s, repo, "t2", head)
	integ := integration(t, s, repo, head)
	before := treeState(t, repo)
	for _, b := range []string{t1.Branch, t2.Branch} {
		if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, b); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"t1.txt", "t2.txt"} {
		if _, err := os.Stat(filepath.Join(integ.Path, f)); err != nil {
			t.Fatal(err)
		}
	}
	if tgit(t, integ.Path, "log", "-1", "--format=%an %p") == "" || strings.Count(tgit(t, integ.Path, "log", "-1", "--format=%p"), " ") != 1 {
		t.Fatal("last commit is not a merge commit")
	}
	if tgit(t, integ.Path, "log", "-1", "--format=%an") != "agentflow" {
		t.Fatal("merge not authored by agentflow")
	}
	if treeState(t, repo) != before {
		t.Fatal("main tree changed")
	}
	if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, "main"); !errors.Is(err, ErrForbiddenRef) {
		t.Fatalf("merging a non-task branch: %v", err)
	}
}

func conflictSetup(t *testing.T) (repo string, s *Service, t1, t2, integ Worktree) {
	t.Helper()
	repo = newRepo(t)
	tgit(t, repo, "stash", "-u", "-q")
	s, _ = newSvc(t)
	head, _ := s.Head(bg, repo)
	mk := func(task, content string) Worktree {
		wt, err := s.AddTaskWorktree(bg, repo, "p", "e", task, head.Commit)
		if err != nil {
			t.Fatal(err)
		}
		write(t, wt.Path, "a.txt", content)
		if _, err := s.CommitAll(bg, repo, wt.Path, "w", Trailers{Task: task, Attempt: 1}); err != nil {
			t.Fatal(err)
		}
		return wt
	}
	t1, t2 = mk("t1", "from t1\n"), mk("t2", "from t2\n")
	integ = integration(t, s, repo, head)
	return
}

// TestMergeConflict: ErrConflict with the file list, integration worktree clean.
func TestMergeConflict(t *testing.T) {
	repo, s, t1, t2, integ := conflictSetup(t)
	if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, t1.Branch); err != nil {
		t.Fatal(err)
	}
	headBefore := tgit(t, integ.Path, "rev-parse", "HEAD")
	_, err := s.Merge(bg, repo, integ.Path, integ.Branch, t2.Branch)
	var ce *ConflictError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &ce) || len(ce.Files) != 1 || ce.Files[0] != "a.txt" {
		t.Fatalf("err = %v", err)
	}
	if st := tgit(t, integ.Path, "status", "--porcelain"); st != "" {
		t.Fatalf("integration worktree not clean: %q", st)
	}
	if tgit(t, integ.Path, "rev-parse", "HEAD") != headBefore {
		t.Fatal("integration HEAD moved")
	}
	if _, err := os.Stat(filepath.Join(tgit(t, integ.Path, "rev-parse", "--absolute-git-dir"), "MERGE_HEAD")); err == nil {
		t.Fatal("MERGE_HEAD left behind")
	}
}

// TestConflictRepairFlow: PrepareConflictRepair leaves markers in the task
// worktree, the repair resolves them, and the merge then succeeds.
func TestConflictRepairFlow(t *testing.T) {
	repo, s, t1, t2, integ := conflictSetup(t)
	if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, t1.Branch); err != nil {
		t.Fatal(err)
	}
	files, err := s.PrepareConflictRepair(bg, repo, t2.Path, integ.Branch)
	if err != nil || len(files) != 1 || files[0] != "a.txt" {
		t.Fatalf("files=%v err=%v", files, err)
	}
	b, _ := os.ReadFile(filepath.Join(t2.Path, "a.txt"))
	if !strings.Contains(string(b), "<<<<<<<") {
		t.Fatalf("no conflict markers:\n%s", b)
	}
	// The integration branch is untouched by the preparation.
	if tgit(t, repo, "rev-parse", integ.Branch) != tgit(t, integ.Path, "rev-parse", "HEAD") {
		t.Fatal("integration moved")
	}
	write(t, t2.Path, "a.txt", "resolved\n")
	if _, err := s.CommitAll(bg, repo, t2.Path, "resolve", Trailers{Task: "t2", Attempt: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, t2.Branch); err != nil {
		t.Fatalf("merge after repair: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(integ.Path, "a.txt")); string(b) != "resolved\n" {
		t.Fatalf("a.txt = %q", b)
	}
}

// TestMergesSerialised: concurrent merges into one integration worktree all
// land (no index.lock races), because Merge takes the per-repo lock.
func TestMergesSerialised(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	integ := integration(t, s, repo, head)
	var tasks []Worktree
	for _, n := range []string{"t1", "t2", "t3", "t4", "t5"} {
		tasks = append(tasks, engineTask(t, s, repo, n, head))
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(tasks))
	for _, w := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Merge(bg, repo, integ.Path, integ.Branch, w.Branch)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range tasks {
		tgit(t, repo, "merge-base", "--is-ancestor", w.Branch, integ.Branch)
	}
}

func TestPushInstructions(t *testing.T) {
	got := PushInstructions("/home/u/my repo", "agentflow/e", "main")
	want := []string{
		"cd '/home/u/my repo'",
		"git log --oneline 'main'..'agentflow/e'",
		"git switch 'main'",
		"git merge --no-ff 'agentflow/e'",
		"git push origin 'main'",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q", got)
	}
	if q := shQuote("a'b"); q != `'a'\''b'` {
		t.Fatalf("quote = %s", q)
	}
}

// TestPushOnlyInInstructions enforces the plan's grep: no non-test Go source
// under internal/agentflow has a "push" string literal (a git argv word). The
// push command text lives only in gitsvc/instructions.go, as one longer string.
// The one exception is the coordinator module for the hand-run seats (AF-13):
// like coordinator.py it pushes a merge that passed its gates to the repository's
// base branch, and only when coord.json sets push for that repository.
func TestPushOnlyInInstructions(t *testing.T) {
	root := filepath.Join("..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if lit.Value == `"push"` && filepath.ToSlash(p) != "../coord/git.go" {
				t.Errorf("%s: push literal: %s", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestMergeAbortRestored: a model's `git merge --abort` during conflict repair
// is a violation, and the merge state is put back.
func TestMergeAbortRestored(t *testing.T) {
	repo, s, t1, t2, integ := conflictSetup(t)
	if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, t1.Branch); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareConflictRepair(bg, repo, t2.Path, integ.Branch); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(bg, repo, t2.Path)
	if err != nil {
		t.Fatal(err)
	}
	admin := tgit(t, t2.Path, "rev-parse", "--absolute-git-dir")
	wantHead, _ := os.ReadFile(filepath.Join(admin, "MERGE_HEAD"))
	tgit(t, t2.Path, "merge", "--abort")
	v, err := s.DetectAndRevert(bg, snap)
	if err != nil || len(v) != 1 || v[0].Kind != ViolationMerge || !strings.Contains(v[0].Detail, "MERGE_HEAD removed") {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	for _, f := range []string{"MERGE_HEAD", "MERGE_MSG", "MERGE_MODE"} {
		if _, err := os.Stat(filepath.Join(admin, f)); err != nil {
			t.Fatalf("%s not restored: %v", f, err)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(admin, "MERGE_HEAD")); string(got) != string(wantHead) {
		t.Fatal("MERGE_HEAD content differs")
	}
	if after, _ := s.Snapshot(bg, repo, t2.Path); after.index != snap.index {
		t.Fatal("conflicted index not restored")
	}
}

// TestMergeCancelledStillAborts: cancelling ctx once the merge has conflicted
// cannot leave MERGE_HEAD behind in the integration worktree.
func TestMergeCancelledStillAborts(t *testing.T) {
	repo, s, t1, t2, integ := conflictSetup(t)
	if _, err := s.Merge(bg, repo, integ.Path, integ.Branch, t1.Branch); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	defer cancel()
	s.argvHook = func(a []string) {
		for _, w := range a {
			if w == "--diff-filter=U" {
				cancel()
			}
		}
	}
	_, err := s.Merge(ctx, repo, integ.Path, integ.Branch, t2.Branch)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("ctx was not cancelled")
	}
	if st := tgit(t, integ.Path, "status", "--porcelain"); st != "" {
		t.Fatalf("not clean: %q", st)
	}
	if _, err := os.Stat(filepath.Join(tgit(t, integ.Path, "rev-parse", "--absolute-git-dir"), "MERGE_HEAD")); err == nil {
		t.Fatal("MERGE_HEAD left behind")
	}
}

func TestMergeWrongIntegrationBranch(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	t1 := engineTask(t, s, repo, "t1", head)
	t2 := engineTask(t, s, repo, "t2", head)
	if _, err := s.Merge(bg, repo, t2.Path, "agentflow/e", t1.Branch); err == nil {
		t.Fatal("merge into a task worktree must be refused")
	}
	integ := integration(t, s, repo, head)
	if _, err := s.Merge(bg, repo, integ.Path, "agentflow/other", t1.Branch); err == nil {
		t.Fatal("wrong integration branch must be refused")
	}
}

// Tool state (.omc, .claude) written into a worktree is never committed.
func TestCommitAllNeverStagesToolState(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt, err := s.AddTaskWorktree(bg, repo, "p", "e", "t1", head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	write(t, wt.Path, "real.txt", "work\n")
	write(t, wt.Path, ".omc/state/x.json", "{}\n")
	write(t, wt.Path, ".claude/settings.local.json", "{}\n")
	sha, err := s.CommitAll(bg, repo, wt.Path, "w", Trailers{Task: "t1", Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	files := tgit(t, repo, "show", "--name-only", "--format=", sha)
	if !strings.Contains(files, "real.txt") || strings.Contains(files, ".omc") || strings.Contains(files, ".claude") {
		t.Fatalf("commit files = %q", files)
	}
}
