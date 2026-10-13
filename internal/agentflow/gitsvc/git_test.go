package gitsvc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var bg = context.Background()

// tgit runs git directly (as a test or a simulated model would), with no engine env.
func tgit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=user", "GIT_AUTHOR_EMAIL=user@example.com", "GIT_COMMITTER_NAME=user", "GIT_COMMITTER_EMAIL=user@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// newRepo makes a repo on main with one commit, a dirty tracked file and an untracked file.
func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	tgit(t, repo, "init", "-q", "-b", "main")
	write(t, repo, "a.txt", "one\n")
	tgit(t, repo, "add", "-A")
	tgit(t, repo, "commit", "-q", "-m", "init")
	write(t, repo, "a.txt", "one dirty\n")
	write(t, repo, "untracked/u.txt", "untracked\n")
	return repo
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// treeState captures the main working tree: every file outside .git by content
// hash, the index file, HEAD, the current branch and `status --porcelain`.
func treeState(t *testing.T, repo string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(repo, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(repo, p)
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s %x", rel, sha256.Sum256(b)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
	lines = append(lines,
		fmt.Sprintf("index %x", sha256.Sum256(idx)),
		"head "+tgit(t, repo, "rev-parse", "HEAD"),
		"branch "+tgit(t, repo, "symbolic-ref", "HEAD"),
		"status "+tgit(t, repo, "status", "--porcelain"))
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func newSvc(t *testing.T) (*Service, *[][]string) {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var argvs [][]string
	s.argvHook = func(a []string) {
		mu.Lock()
		argvs = append(argvs, a)
		mu.Unlock()
	}
	return s, &argvs
}

// TestMainTreeUntouched covers AC-PX1 (unit half), GIT2, PX2, ST4: every
// operation leaves the dirty main tree, HEAD and branch byte-identical.
func TestMainTreeUntouched(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	before := treeState(t, repo)
	check := func(step string) {
		t.Helper()
		if after := treeState(t, repo); after != before {
			t.Fatalf("main tree changed after %s:\nbefore:\n%s\nafter:\n%s", step, before, after)
		}
	}

	head, err := s.Head(bg, repo)
	if err != nil || head.Branch != "main" {
		t.Fatalf("Head = %+v, %v", head, err)
	}
	check("Head")
	if !s.IsRepo(bg, repo) {
		t.Fatal("IsRepo false")
	}
	check("IsRepo")

	br, err := s.EnsureIntegrationBranch(bg, repo, "e1", head.Commit, "")
	if err != nil || br != "agentflow/e1" {
		t.Fatalf("EnsureIntegrationBranch = %q, %v", br, err)
	}
	if again, err := s.EnsureIntegrationBranch(bg, repo, "e1", head.Commit, ""); err != nil || again != br {
		t.Fatalf("second Ensure = %q, %v", again, err)
	}
	check("EnsureIntegrationBranch")

	integ, err := s.AddIntegrationWorktree(bg, repo, "p", "e1", br)
	if err != nil {
		t.Fatal(err)
	}
	check("AddIntegrationWorktree")
	if _, err := s.AddIntegrationWorktree(bg, repo, "p", "e1", br); err != nil {
		t.Fatalf("integration worktree not reusable: %v", err)
	}

	wt, err := s.AddTaskWorktree(bg, repo, "p", "e1", "t1", br)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(s.WorktreeRoot(), "p", "e1", "t1"); wt.Path != want || wt.Branch != "agentflow-task/e1/t1" {
		t.Fatalf("worktree = %+v, want path %s", wt, want)
	}
	if want := filepath.Join(s.WorktreeRoot(), "p", "e1", "_integration"); integ.Path != want {
		t.Fatalf("integration path %s, want %s", integ.Path, want)
	}
	check("AddTaskWorktree")

	write(t, wt.Path, "new.go", "package x\n")
	sha, err := s.CommitAll(bg, repo, wt.Path, "add new.go", Trailers{Task: "t1", Attempt: 1})
	if err != nil || sha == "" {
		t.Fatalf("CommitAll = %q, %v", sha, err)
	}
	check("CommitAll")

	re, err := s.ReuseWorktree(bg, repo, "p", "e1", "t1")
	if err != nil || re != wt {
		t.Fatalf("ReuseWorktree = %+v, %v", re, err)
	}
	check("ReuseWorktree")

	if err := s.RemoveWorktree(bg, repo, wt.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still exists: %v", err)
	}
	check("RemoveWorktree")
	if err := s.RemoveWorktree(bg, repo, integ.Path); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBranch(bg, repo, wt.Branch); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteBranch(bg, repo, br); err != nil {
		t.Fatal(err)
	}
	check("DeleteBranch")
	if list := tgit(t, repo, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("worktrees left behind:\n%s", list)
	}
}

// TestCommitAuthorAndTrailers covers GIT3.
func TestCommitAuthorAndTrailers(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAll(bg, repo, wt.Path, "nothing here", Trailers{Task: "t", Attempt: 1}); !errors.Is(err, ErrNothingToCommit) {
		t.Fatalf("want ErrNothingToCommit, got %v", err)
	}
	write(t, wt.Path, "f.txt", "x\n")
	sha, err := s.CommitAll(bg, repo, wt.Path, "implement f", Trailers{Task: "t", Attempt: 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, repo, "log", "-1", "--format=%an <%ae>|%cn <%ce>", sha); got != "agentflow <noreply@agentflow.local>|agentflow <noreply@agentflow.local>" {
		t.Fatalf("ident = %q", got)
	}
	body := tgit(t, repo, "log", "-1", "--format=%B", sha)
	for _, want := range []string{"implement f", "Agentflow-Task: t", "Agentflow-Attempt: 3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("message missing %q:\n%s", want, body)
		}
	}
	if tr := tgit(t, repo, "log", "-1", "--format=%(trailers:key=Agentflow-Attempt,valueonly)", sha); tr != "3" {
		t.Fatalf("trailer parse = %q", tr)
	}
	if tgit(t, repo, "rev-parse", wt.Branch) != sha {
		t.Fatal("task branch did not advance to the engine commit")
	}
	// Repository config was not modified.
	if out, err := exec.Command("git", "-C", repo, "config", "--local", "--get", "user.name").Output(); err == nil {
		t.Fatalf("local user.name was set: %s", out)
	}
	if _, err := s.CommitAll(bg, repo, wt.Path, "  ", Trailers{Task: "t", Attempt: 1}); err == nil {
		t.Fatal("empty message accepted")
	}
}

// TestCommitIgnoresWorktreeDotGit covers S4: a model-rewritten $WT/.git (and a
// hostile hook) is never trusted.
func TestCommitIgnoresWorktreeDotGit(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	// Point the worktree's .git at the user's main repo: trusting it would commit onto main.
	if err := os.WriteFile(filepath.Join(wt.Path, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "hook-ran")
	hooks := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"pre-commit", "commit-msg", "post-commit"} {
		os.WriteFile(filepath.Join(hooks, h), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	}
	mainHead := tgit(t, repo, "rev-parse", "HEAD")
	write(t, wt.Path, "f.txt", "x\n")
	sha, err := s.CommitAll(bg, repo, wt.Path, "m", Trailers{Task: "t", Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, repo, "rev-parse", wt.Branch); got != sha {
		t.Fatalf("commit did not land on the task branch: %s vs %s", got, sha)
	}
	if got := tgit(t, repo, "rev-parse", "HEAD"); got != mainHead {
		t.Fatal("main HEAD moved")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a git hook ran")
	}
}

// TestPathAssertions: write operations refuse paths outside the worktree root,
// including the main working tree.
func TestPathAssertions(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	before := treeState(t, repo)
	if err := s.RemoveWorktree(bg, repo, repo); !errors.Is(err, ErrOutsideDataDir) {
		t.Fatalf("RemoveWorktree(main) = %v", err)
	}
	if _, err := s.CommitAll(bg, repo, repo, "m", Trailers{Task: "t", Attempt: 1}); !errors.Is(err, ErrOutsideDataDir) {
		t.Fatalf("CommitAll(main) = %v", err)
	}
	inside := filepath.Join(s.WorktreeRoot(), "p", "e", "ghost")
	if err := s.RemoveWorktree(bg, repo, inside); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("RemoveWorktree(unregistered) = %v", err)
	}
	if after := treeState(t, repo); after != before {
		t.Fatal("main tree changed")
	}
}

func TestNameValidation(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	for _, bad := range []string{"", "..", "a/b", "_integration", "-x", "a..b", "x.lock", "a b", "a\nb"} {
		if _, err := s.AddTaskWorktree(bg, repo, "p", "e", bad, head.Commit); !errors.Is(err, ErrInvalidName) {
			t.Errorf("task %q: %v", bad, err)
		}
	}
	if _, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", "--upload-pack=x"); err == nil {
		t.Error("option-like revision accepted")
	}
	for _, b := range []string{"main", "feature/x", "agentflow"} {
		if err := s.DeleteBranch(bg, repo, b); !errors.Is(err, ErrForbiddenRef) {
			t.Errorf("DeleteBranch(%q) = %v", b, err)
		}
	}
	if err := s.DeleteBranch(bg, repo, "agentflow/none"); err == nil {
		t.Error("deleting a missing branch should fail")
	}
}

// TestRefusesDeletingCheckedOutBranch: even an agentflow-named branch the user checked out is protected.
func TestRefusesDeletingCheckedOutBranch(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	tgit(t, repo, "branch", "agentflow/mine")
	tgit(t, repo, "symbolic-ref", "HEAD", "refs/heads/agentflow/mine")
	if err := s.DeleteBranch(bg, repo, "agentflow/mine"); !errors.Is(err, ErrForbiddenRef) {
		t.Fatalf("got %v", err)
	}
}

// TestIntegrationBranchOverride covers the user-named integration branch (GIT2).
func TestIntegrationBranchOverride(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	br, err := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, "release/work")
	if err != nil || br != "release/work" {
		t.Fatalf("%q %v", br, err)
	}
	if _, err := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, "bad..name"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("bad override: %v", err)
	}
	if _, err := s.EnsureIntegrationBranch(bg, repo, "e", "nonexistent-rev", ""); err == nil {
		t.Fatal("bad base accepted")
	}
}

// TestTaskBranchCoexistsWithIntegration documents the corrected branch name: git cannot hold
// agentflow/<effort> and agentflow/<effort>/<task> together.
func TestTaskBranchCoexistsWithIntegration(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	tgit(t, repo, "branch", "agentflow/e", head.Commit)
	if out, err := exec.Command("git", "-C", repo, "branch", "agentflow/e/t", head.Commit).CombinedOutput(); err == nil {
		t.Fatalf("expected git ref D/F conflict, got success: %s", out)
	}
	if _, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit); !errors.Is(err, ErrBranchExists) {
		t.Fatalf("duplicate task worktree: %v", err)
	}
}

// TestPruneOnlyOwnStaleWorktrees covers the pre-mortem guard on `worktree prune`.
func TestPruneOnlyOwnStaleWorktrees(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	// A user worktree whose directory is temporarily gone must survive any prune.
	userWT := filepath.Join(t.TempDir(), "userwt")
	tgit(t, repo, "worktree", "add", "-q", "-b", "userbranch", userWT)
	moved := userWT + ".moved"
	if err := os.Rename(userWT, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneWorktrees(bg, repo); err != nil {
		t.Fatal(err)
	}
	list := tgit(t, repo, "worktree", "list", "--porcelain")
	if !strings.Contains(list, "userwt") {
		t.Fatalf("user worktree was pruned:\n%s", list)
	}
	if !strings.Contains(list, "/t\n") {
		t.Fatalf("expected the stale agentflow entry kept while a foreign stale entry exists:\n%s", list)
	}
	// Once the foreign entry is healthy again, our stale entry is pruned.
	if err := os.Rename(moved, userWT); err != nil {
		t.Fatal(err)
	}
	if err := s.PruneWorktrees(bg, repo); err != nil {
		t.Fatal(err)
	}
	list = tgit(t, repo, "worktree", "list", "--porcelain")
	if strings.Contains(list, wt.Path) || !strings.Contains(list, "userwt") {
		t.Fatalf("prune result wrong:\n%s", list)
	}
}

// TestInit covers the PJ1 offer.
func TestInit(t *testing.T) {
	s, _ := newSvc(t)
	dir := t.TempDir()
	if s.IsRepo(bg, dir) {
		t.Fatal("empty dir is a repo")
	}
	if err := s.Init(bg, dir); err != nil {
		t.Fatal(err)
	}
	h, err := s.Head(bg, dir)
	if err != nil || h.Branch != "main" {
		t.Fatalf("%+v %v", h, err)
	}
	if got := tgit(t, dir, "log", "-1", "--format=%an <%ae>"); got != "agentflow <noreply@agentflow.local>" {
		t.Fatal(got)
	}
	if err := s.Init(bg, dir); err == nil {
		t.Fatal("second Init succeeded")
	}
	if s.IsRepo(bg, filepath.Join(dir, "missing")) {
		t.Fatal("missing path is a repo")
	}
}

// TestRepoMutexSerialises: concurrent worktree and branch operations on one repo all succeed.
func TestRepoMutexSerialises(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	var wg sync.WaitGroup
	var fails atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task := fmt.Sprintf("t%d", i)
			wt, err := s.AddTaskWorktree(bg, repo, "p", "e", task, head.Commit)
			if err == nil {
				err = s.RemoveWorktree(bg, repo, wt.Path)
			}
			if err == nil {
				err = s.DeleteBranch(bg, repo, wt.Branch)
			}
			if err != nil {
				t.Errorf("%s: %v", task, err)
				fails.Add(1)
			}
		}()
	}
	wg.Wait()
	if fails.Load() == 0 && strings.Count(tgit(t, repo, "worktree", "list"), "\n") != 0 {
		t.Fatal("worktrees left behind")
	}
}

// TestRepoLockExcludes: lockRepo is mutual exclusion keyed by the resolved repo path.
func TestRepoLockExcludes(t *testing.T) {
	repo := newRepo(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	s, _ := newSvc(t)
	unlock := s.lockRepo(bg, repo)
	got := make(chan struct{})
	go func() {
		u := s.lockRepo(bg, alias)
		close(got)
		u()
	}()
	select {
	case <-got:
		t.Fatal("second lock acquired while first held")
	case <-time.After(100 * time.Millisecond):
	}
	unlock()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("lock never released")
	}
}

// TestEnvScrubbed: git sees only the fixed env, and every argv carries -C.
func TestEnvScrubbed(t *testing.T) {
	s, _ := newSvc(t)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent")
	t.Setenv("GIT_DIR", "/nonexistent")
	t.Setenv("GIT_ASKPASS", "evil")
	env := strings.Join(s.gitEnv(), "\n")
	for _, bad := range []string{"SSH_AUTH_SOCK", "GIT_DIR", "GIT_ASKPASS", "GIT_SSH"} {
		if strings.Contains(env, bad) {
			t.Fatalf("env leaks %s", bad)
		}
	}
	for _, want := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"} {
		if !strings.Contains(env, want) {
			t.Fatalf("env missing %s", want)
		}
	}
}

// TestNoPushAndNoShell covers GIT4 and the B9 grep: no argv contains push, and
// the package source has no push, sh/bash or non-config "-c" literal.
func TestNoPushAndNoShell(t *testing.T) {
	repo := newRepo(t)
	s, argvs := newSvc(t)
	head, _ := s.Head(bg, repo)
	br, _ := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, "")
	s.AddIntegrationWorktree(bg, repo, "p", "e", br)
	wt, _ := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit)
	write(t, wt.Path, "f", "x")
	s.CommitAll(bg, repo, wt.Path, "m", Trailers{Task: "t", Attempt: 1})
	s.ReuseWorktree(bg, repo, "p", "e", "t")
	s.RemoveWorktree(bg, repo, wt.Path)
	s.DeleteBranch(bg, repo, wt.Branch)
	s.PruneWorktrees(bg, repo)
	empty := t.TempDir()
	s.Init(bg, empty)

	if len(*argvs) < 20 {
		t.Fatalf("only %d argvs recorded", len(*argvs))
	}
	for _, a := range *argvs {
		if a[0] != "git" {
			t.Fatalf("not git: %v", a)
		}
		hasC := false
		for i, w := range a {
			if w == "push" {
				t.Fatalf("push in argv: %v", a)
			}
			if w == "-C" && i+1 < len(a) {
				hasC = true
			}
		}
		if !hasC {
			t.Fatalf("argv without -C: %v", a)
		}
	}

	files, _ := filepath.Glob("*.go")
	bad := regexp.MustCompile(`"push"|"sh"|"bash"|"-c"[^,]|"-c",\s*"[^"=]*"`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, _ := os.ReadFile(f)
		for n, line := range strings.Split(string(b), "\n") {
			if bad.MatchString(line) && !strings.Contains(line, `"-c", "filter."`) {
				t.Errorf("%s:%d forbidden literal: %s", f, n+1, line)
			}
		}
	}
}

// victimSetup returns a repo, service and attacker worktree "zz" (admin dir
// sorts last) plus a user linked worktree "aa-victim" on branch feature (admin
// dir sorts first), and a second agentflow worktree "ta".
func victimSetup(t *testing.T) (repo string, s *Service, atk, userWT, other Worktree, head HeadInfo) {
	t.Helper()
	repo = newRepo(t)
	s, _ = newSvc(t)
	head, _ = s.Head(bg, repo)
	userWT.Path = filepath.Join(t.TempDir(), "aa-victim")
	userWT.Branch = "feature"
	tgit(t, repo, "worktree", "add", "-q", "-b", "feature", userWT.Path)
	var err error
	if other, err = s.AddTaskWorktree(bg, repo, "p", "e", "ta", head.Commit); err != nil {
		t.Fatal(err)
	}
	if atk, err = s.AddTaskWorktree(bg, repo, "p", "e", "zz", head.Commit); err != nil {
		t.Fatal(err)
	}
	return
}

// TestCommitRefusesSymlinkedDotGit: the model symlinks $WT/.git to another
// worktree's .git (the user's own linked worktree, or another agentflow task).
// The victim's branch must not move and CommitAll must refuse.
func TestCommitRefusesSymlinkedDotGit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		victim func(userWT, other Worktree) Worktree
	}{
		{"user-linked-worktree", func(u, _ Worktree) Worktree { return u }},
		{"other-agentflow-task", func(_, o Worktree) Worktree { return o }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, s, atk, userWT, other, _ := victimSetup(t)
			v := tc.victim(userWT, other)
			victimBefore := tgit(t, repo, "rev-parse", v.Branch)
			if err := os.Remove(filepath.Join(atk.Path, ".git")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(v.Path, ".git"), filepath.Join(atk.Path, ".git")); err != nil {
				t.Fatal(err)
			}
			write(t, atk.Path, "evil.txt", "x\n")
			_, err := s.CommitAll(bg, repo, atk.Path, "m", Trailers{Task: "zz", Attempt: 1})
			if !errors.Is(err, ErrTamperedWorktree) {
				t.Fatalf("CommitAll = %v, want ErrTamperedWorktree", err)
			}
			if got := tgit(t, repo, "rev-parse", v.Branch); got != victimBefore {
				t.Fatalf("victim branch %s moved", v.Branch)
			}
			if _, err := s.AdminGitDir(bg, repo, atk.Path); !errors.Is(err, ErrTamperedWorktree) {
				t.Fatalf("AdminGitDir = %v", err)
			}
		})
	}
}

// TestAdminGitDirMatchesExactlyOne: a plain worktree resolves to its own admin dir.
func TestAdminGitDirMatchesExactlyOne(t *testing.T) {
	repo, s, atk, _, other, _ := victimSetup(t)
	a, err := s.AdminGitDir(bg, repo, atk.Path)
	if err != nil || filepath.Base(a) != "zz" {
		t.Fatalf("%q %v", a, err)
	}
	b, err := s.AdminGitDir(bg, repo, other.Path)
	if err != nil || filepath.Base(b) != "ta" {
		t.Fatalf("%q %v", b, err)
	}
}

// filterRepo configures a filter that drops a marker file when it runs and
// returns the marker path.
func filterRepo(t *testing.T, repo, kind string) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), kind+"-ran")
	tgit(t, repo, "config", "filter.mark.clean", "touch "+marker+"-clean; cat")
	tgit(t, repo, "config", "filter.mark.smudge", "touch "+marker+"-smudge; cat")
	tgit(t, repo, "config", "filter.mark.required", "true")
	return marker
}

func gone(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err == nil {
		t.Fatalf("filter ran: %s exists", p)
	}
}

// TestFiltersNeverRun: a model-written .gitattributes (or info/attributes)
// must not run the repo's clean/smudge drivers in engine git.
func TestFiltersNeverRun(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	marker := filterRepo(t, repo, "x")

	// clean on add -A
	wt, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	write(t, wt.Path, ".gitattributes", "* filter=mark\n")
	write(t, wt.Path, "f.dat", "data\n")
	info := filepath.Join(repo, ".git", "info")
	os.MkdirAll(info, 0o755)
	os.WriteFile(filepath.Join(info, "attributes"), []byte("* filter=mark\n"), 0o644)
	sha, err := s.CommitAll(bg, repo, wt.Path, "m", Trailers{Task: "t", Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := tgit(t, repo, "show", sha+":f.dat"); got != "data" {
		t.Fatalf("content = %q", got)
	}
	gone(t, marker+"-clean")
	gone(t, marker+"-smudge")

	// smudge on worktree add of a commit that carries .gitattributes
	os.Remove(filepath.Join(info, "attributes"))
	tgit(t, repo, "branch", "carrier", sha)
	if _, err := s.AddTaskWorktree(bg, repo, "p", "e", "t2", sha); err != nil {
		t.Fatal(err)
	}
	gone(t, marker+"-smudge")
	gone(t, marker+"-clean")

	// Teeth: plain git with the same config does run the smudge filter.
	tgit(t, repo, "worktree", "add", "-q", "--detach", filepath.Join(t.TempDir(), "plain"), sha)
	if _, err := os.Stat(marker + "-smudge"); err != nil {
		t.Fatalf("sanity check failed: plain git did not run the filter: %v", err)
	}
}

func TestHardenedPrefixCoversFilters(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	filterRepo(t, repo, "x")
	h, err := s.Hardened(bg, repo)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(h.Args, " ")
	for _, want := range []string{"filter.mark.clean=", "filter.mark.smudge=", "filter.mark.process=", "filter.mark.required=false",
		"--attr-source=4b825dc642cb6eb9a060e54bf8d69288fbee4904", "core.attributesFile=/dev/null", "core.sshCommand=false", "protocol.file.allow=user"} {
		if !strings.Contains(joined, want) {
			t.Errorf("hardening missing %q in %s", want, joined)
		}
	}
	if !strings.Contains(strings.Join(h.Env, " "), "GIT_ATTR_NOSYSTEM=1") {
		t.Error("env missing GIT_ATTR_NOSYSTEM")
	}
}

// TestRemoveWorktreeTampered: git refuses to remove a worktree whose .git was
// rewritten; the engine still cleans up.
func TestRemoveWorktreeTampered(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt, err := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wt.Path, ".git"), []byte("gitdir: /nonexistent\n"), 0o644)
	write(t, wt.Path, "sub/x", "x")
	before := treeState(t, repo)
	if err := s.RemoveWorktree(bg, repo, wt.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatal("worktree dir remains")
	}
	if list := tgit(t, repo, "worktree", "list", "--porcelain"); strings.Count(list, "worktree ") != 1 {
		t.Fatalf("admin entry remains:\n%s", list)
	}
	if treeState(t, repo) != before {
		t.Fatal("main tree changed")
	}
}

// TestCommitRefusesGitlinks: a nested repository becomes a typed error and the index is reset.
func TestCommitRefusesGitlinks(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	wt, _ := s.AddTaskWorktree(bg, repo, "p", "e", "t", head.Commit)
	sub := filepath.Join(wt.Path, "vendor", "lib")
	os.MkdirAll(sub, 0o755)
	tgit(t, sub, "init", "-q")
	write(t, sub, "f", "x")
	tgit(t, sub, "add", "-A")
	tgit(t, sub, "commit", "-q", "-m", "x")
	write(t, wt.Path, "ok.txt", "ok")
	_, err := s.CommitAll(bg, repo, wt.Path, "m", Trailers{Task: "t", Attempt: 1})
	var gl *GitlinkError
	if !errors.Is(err, ErrGitlink) || !errors.As(err, &gl) || len(gl.Paths) != 1 || gl.Paths[0] != "vendor/lib" {
		t.Fatalf("err = %v", err)
	}
	if tgit(t, repo, "rev-parse", wt.Branch) != head.Commit {
		t.Fatal("branch moved")
	}
}

func TestIntegrationOverrideOwnership(t *testing.T) {
	repo := newRepo(t)
	s, _ := newSvc(t)
	head, _ := s.Head(bg, repo)
	tgit(t, repo, "branch", "feature/x")
	tgit(t, repo, "branch", "mine")
	if _, err := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, "feature/x"); !errors.Is(err, ErrForeignBranch) {
		t.Fatalf("foreign = %v", err)
	}
	if _, err := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, "main"); !errors.Is(err, ErrForeignBranch) {
		t.Fatalf("main = %v", err)
	}
	for _, bad := range []string{"@{-1}", "-x", "a b", "a@{u}", "x..y", "a~1", "/a", "a/", "HEAD@{0}", "a;b"} {
		if _, err := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, bad); !errors.Is(err, ErrInvalidName) {
			t.Errorf("override %q: %v", bad, err)
		}
	}
	for i := 0; i < 2; i++ {
		if br, err := s.EnsureIntegrationBranch(bg, repo, "e", head.Commit, "work/e"); err != nil || br != "work/e" {
			t.Fatalf("own branch run %d: %q %v", i, br, err)
		}
	}
	if err := s.DeleteBranch(bg, repo, "work/e"); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/agentflow/owned/work/e").Run(); err == nil {
		t.Fatal("marker ref left behind")
	}
	if err := s.DeleteBranch(bg, repo, "mine"); !errors.Is(err, ErrForbiddenRef) {
		t.Fatalf("deleting user branch = %v", err)
	}
}

func TestMessageStripsAgentflowLines(t *testing.T) {
	m, err := message("do it\nagentflow-task: evil\n  Agentflow-Attempt: 99\nmore", Trailers{Task: "t", Attempt: 2})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m, "evil") || strings.Contains(m, "99") || strings.Count(m, "Agentflow-Task") != 1 || !strings.Contains(m, "more") {
		t.Fatalf("message = %q", m)
	}
}
