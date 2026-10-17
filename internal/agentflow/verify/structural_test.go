package verify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newRepo returns a repo with one commit and that commit's id.
func newRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "init", "-q")
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "docs/readme.md", "hi\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "base")
	return dir, run(t, dir, "rev-parse", "HEAD")
}

func check(t *testing.T, dir, base string, touched ...string) VerifyReport {
	t.Helper()
	rep, err := Structural(context.Background(), Input{Worktree: dir, GitDir: filepath.Join(dir, ".git"), BaseCommit: base, FilesTouched: touched})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func details(rep VerifyReport, name string) string {
	for _, c := range rep.Checks {
		if c.Name == name {
			return strings.Join(c.Details, "|")
		}
	}
	return ""
}

func TestNoChangesFails(t *testing.T) {
	dir, base := newRepo(t)
	rep := check(t, dir, base, "a.go")
	if rep.Pass || !rep.Failed(CheckNoChanges) {
		t.Fatalf("want no_changes failure, got %+v", rep)
	}
}

func TestChangeInCommitAndUntrackedCount(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "a.go", "package a\n\nfunc F() {}\n")
	run(t, dir, "commit", "-qam", "w")
	write(t, dir, "new.go", "package a\n")
	rep := check(t, dir, base, "a.go", "new.go")
	if !rep.Pass || len(rep.Warnings) != 0 {
		t.Fatalf("want clean pass, got %+v", rep)
	}
}

func TestOutOfScopeIsWarningNotFailure(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "a.go", "package a\n\nvar X = 1\n")
	write(t, dir, "docs/readme.md", "changed\n")
	write(t, dir, "other/z.go", "package z\n")
	rep := check(t, dir, base, "a.go", "docs")
	if !rep.Pass {
		t.Fatalf("scope must not fail: %+v", rep)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "other/z.go") {
		t.Fatalf("want one warning naming other/z.go, got %v", rep.Warnings)
	}
}

func TestBrokenGoFailsParse(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "a.go", "package a\nfunc {\n")
	rep := check(t, dir, base, "a.go")
	if rep.Pass || !rep.Failed(CheckParse) || !strings.Contains(details(rep, CheckParse), "a.go") {
		t.Fatalf("want parse failure on a.go, got %+v", rep)
	}
}

func TestParseByExtension(t *testing.T) {
	cases := []struct {
		name, body string
		bad        bool
	}{
		{"ok.json", `{"a":1}`, false},
		{"bad.json", `{"a":`, true},
		{"ok.yaml", "a: 1\n---\nb: [1, 2]\n", false},
		{"bad.yml", "a: [1, 2\nb: : :\n", true},
		{"ok.toml", "a = 1\n[t]\nb = \"x\"\n", false},
		{"bad.toml", "a = = 1\n", true},
		{"notes.txt", "{{{ not parsed", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, base := newRepo(t)
			write(t, dir, c.name, c.body)
			rep := check(t, dir, base, c.name)
			if got := rep.Failed(CheckParse); got != c.bad {
				t.Fatalf("parse failed=%v want %v: %+v", got, c.bad, rep)
			}
		})
	}
}

func TestConflictMarkersFail(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "docs/readme.md", "x\n<<<<<<< HEAD\na\n=======\nb\n>>>>>>> branch\n")
	rep := check(t, dir, base, "docs")
	if rep.Pass || !rep.Failed(CheckConflicts) || !strings.Contains(details(rep, CheckConflicts), "docs/readme.md:2") {
		t.Fatalf("want conflict failure at line 2, got %+v", rep)
	}
}

func TestSetextRuleIsNotAConflict(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "docs/readme.md", "Title\n=======\ntext\n")
	if rep := check(t, dir, base, "docs"); !rep.Pass {
		t.Fatalf("heading underline flagged: %+v", rep)
	}
}

func TestEscapingSymlinkFails(t *testing.T) {
	dir, base := newRepo(t)
	outside := t.TempDir()
	write(t, outside, "secret", "s")
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	rep := check(t, dir, base, "link")
	if rep.Pass || !rep.Failed(CheckContainment) || !strings.Contains(details(rep, CheckContainment), "link") {
		t.Fatalf("want containment failure, got %+v", rep)
	}
}

func TestEscapingDirSymlinkFails(t *testing.T) {
	dir, base := newRepo(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "d")); err != nil {
		t.Fatal(err)
	}
	if rep := check(t, dir, base, "d"); !rep.Failed(CheckContainment) {
		t.Fatalf("want containment failure, got %+v", rep)
	}
}

func TestInternalSymlinkAndDeletionPass(t *testing.T) {
	dir, base := newRepo(t)
	if err := os.Symlink("a.go", filepath.Join(dir, "alias.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "docs/readme.md")); err != nil {
		t.Fatal(err)
	}
	rep := check(t, dir, base, "alias.go", "docs")
	if !rep.Pass {
		t.Fatalf("want pass, got %+v", rep)
	}
}

func TestMissingBaseIsError(t *testing.T) {
	dir, _ := newRepo(t)
	if _, err := Structural(context.Background(), Input{Worktree: dir, GitDir: filepath.Join(dir, ".git")}); err == nil {
		t.Fatal("want error for empty base")
	}
	if _, err := Structural(context.Background(), Input{Worktree: dir, GitDir: filepath.Join(dir, ".git"), BaseCommit: "deadbeef"}); err == nil {
		t.Fatal("want error for unknown base")
	}
}

// linkedWorktree returns a registered linked worktree, its trusted admin dir,
// the base commit and the main repo path.
func linkedWorktree(t *testing.T) (wt, admin, base, main string) {
	t.Helper()
	main, base = newRepo(t)
	wt = filepath.Join(t.TempDir(), "wt")
	run(t, main, "worktree", "add", "-q", "-b", "task", wt, base)
	admin = filepath.Join(main, ".git", "worktrees", "wt")
	return
}

func checkAt(t *testing.T, wt, admin, base string, touched ...string) VerifyReport {
	t.Helper()
	rep, err := Structural(context.Background(), Input{Worktree: wt, GitDir: admin, BaseCommit: base, FilesTouched: touched})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func noFile(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err == nil {
		t.Fatalf("side effect: %s exists, a filter command ran", p)
	}
}

// statDirty rewrites a tracked file with identical content and a new mtime so
// git must re-read it (through any clean filter) to compare.
func statDirty(t *testing.T, dir, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
}

func TestCleanFilterInRepoConfigDoesNotRun(t *testing.T) {
	wt, admin, base, main := linkedWorktree(t)
	marker := filepath.Join(t.TempDir(), "ran")
	run(t, main, "config", "filter.x.clean", "touch "+marker+"; cat")
	write(t, wt, ".gitattributes", "*.go filter=x\n")
	statDirty(t, wt, "a.go")
	write(t, wt, "b.go", "package a\n")
	rep := checkAt(t, wt, admin, base, "b.go", ".gitattributes")
	noFile(t, marker)
	if !rep.Pass {
		t.Fatalf("want pass, got %+v", rep)
	}
}

func TestRewrittenDotGitFileIsNotFollowed(t *testing.T) {
	wt, admin, base, _ := linkedWorktree(t)
	marker := filepath.Join(t.TempDir(), "ran")
	evil := filepath.Join(wt, "evil")
	if err := os.MkdirAll(filepath.Join(evil, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, evil, "HEAD", "ref: refs/heads/x\n")
	write(t, evil, "config", "[core]\n\trepositoryformatversion = 0\n[filter \"x\"]\n\tclean = touch "+marker+"; cat\n")
	write(t, wt, ".gitattributes", "*.go filter=x\n")
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: ./evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	statDirty(t, wt, "a.go")
	write(t, wt, "b.go", "package a\n")
	rep := checkAt(t, wt, admin, base, "b.go")
	noFile(t, marker)
	if !strings.Contains(details(rep, CheckScope)+strings.Join(rep.Warnings, "|"), ".gitattributes") {
		t.Fatalf("report must reflect real changes (.gitattributes, b.go): %+v", rep)
	}
	if rep.Failed(CheckNoChanges) {
		t.Fatalf("real changes missed: %+v", rep)
	}
}

func TestNestedRepoAndGitlinkFail(t *testing.T) {
	dir, base := newRepo(t)
	inner := filepath.Join(dir, "inner")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, inner, "init", "-q")
	write(t, inner, "f", "x")
	run(t, inner, "add", "-A")
	run(t, inner, "commit", "-q", "-m", "i")
	if rep := check(t, dir, base, "inner"); !rep.Failed(CheckNestedRepo) {
		t.Fatalf("untracked nested repo not flagged: %+v", rep)
	}
	run(t, dir, "add", "inner")
	if rep := check(t, dir, base, "inner"); !rep.Failed(CheckNestedRepo) || rep.Pass {
		t.Fatalf("gitlink not flagged: %+v", rep)
	}
}

func TestRelativeAndChainedSymlinkEscape(t *testing.T) {
	dir, base := newRepo(t)
	outside := t.TempDir()
	write(t, outside, "secret", "s")
	rel, err := filepath.Rel(dir, filepath.Join(outside, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(rel, filepath.Join(dir, "rel")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "hop")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("hop", filepath.Join(dir, "chain")); err != nil {
		t.Fatal(err)
	}
	rep := check(t, dir, base)
	d := details(rep, CheckContainment)
	for _, n := range []string{"rel:", "hop:", "chain:"} {
		if !strings.Contains(d, n) {
			t.Fatalf("%s not flagged: %s", n, d)
		}
	}
}

func TestDeletedFileUnderEscapingParentFails(t *testing.T) {
	dir, base := newRepo(t)
	outside := t.TempDir()
	if err := os.RemoveAll(filepath.Join(dir, "docs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "docs")); err != nil {
		t.Fatal(err)
	}
	rep := check(t, dir, base)
	if !strings.Contains(details(rep, CheckContainment), "docs/readme.md") {
		t.Fatalf("deleted file under escaping parent not flagged: %+v", rep)
	}
}

func TestDotTouchedCoversEverythingAndScopeNotDuplicated(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "x/y.go", "package y\n")
	if rep := check(t, dir, base, "."); len(rep.Warnings) != 0 {
		t.Fatalf("'.' must cover all: %v", rep.Warnings)
	}
	rep := check(t, dir, base, "a.go")
	if len(rep.Warnings) != 1 || details(rep, CheckScope) != "" {
		t.Fatalf("scope warning must appear once, in Warnings: %+v", rep)
	}
}

func TestSkippedFilesWarn(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "big.json", strings.Repeat(" ", maxParseBytes+1))
	rep := check(t, dir, base, "big.json")
	if !strings.Contains(strings.Join(rep.Warnings, "|"), "big.json: larger than 8 MiB") {
		t.Fatalf("want skip warning, got %v", rep.Warnings)
	}
}

func TestFinalizeRecomputesPass(t *testing.T) {
	r := VerifyReport{Checks: []Check{{Name: "x", Pass: true}}, Pass: false}
	r.Finalize()
	if !r.Pass {
		t.Fatal("want pass")
	}
	r.NewFailures = []string{"TestX"}
	r.Finalize()
	if r.Pass {
		t.Fatal("new failure must fail")
	}
}

func TestNoChangesDeclaredPassesWithWarning(t *testing.T) {
	dir, base := newRepo(t)
	rep, err := Structural(context.Background(), Input{Worktree: dir, GitDir: filepath.Join(dir, ".git"), BaseCommit: base, FilesTouched: []string{"a.go"}, NoChangesReason: "a.go F exists"})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Pass || rep.Failed(CheckNoChanges) || len(rep.Warnings) != 1 || rep.Warnings[0] != "worker declares nothing to change: a.go F exists" {
		t.Fatalf("got %+v", rep)
	}
}

// H14: "nothing to change" is believed only when the files the task names exist.
func TestNoChangesDeclaredFailsWhenNamedFilesAreMissing(t *testing.T) {
	dir, base := newRepo(t)
	rep, err := Structural(context.Background(), Input{Worktree: dir, GitDir: filepath.Join(dir, ".git"), BaseCommit: base,
		FilesTouched: []string{"a.go", "tests/math_test.go"}, NoChangesReason: "all done"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pass || !rep.Failed(CheckNoChanges) || !strings.Contains(strings.Join(rep.Checks[0].Details, "|"), "tests/math_test.go") {
		t.Fatalf("got %+v", rep)
	}
}

func TestNoChangesReasonIgnoredWhenFilesChanged(t *testing.T) {
	dir, base := newRepo(t)
	write(t, dir, "a.go", "package a\n\nfunc F() {}\n")
	rep, err := Structural(context.Background(), Input{Worktree: dir, GitDir: filepath.Join(dir, ".git"), BaseCommit: base, FilesTouched: []string{"a.go"}, NoChangesReason: "x"})
	if err != nil || !rep.Pass || len(rep.Warnings) != 0 {
		t.Fatalf("got %+v %v", rep, err)
	}
}

// H11: a change to a file another task owns fails the scope check, because the
// two would conflict at merge; a new file nobody owns is only a warning, and a
// file the task lists itself is its own.
func TestScopeFailsOnAnotherTasksFile(t *testing.T) {
	run := func(touched, others []string, files ...string) VerifyReport {
		t.Helper()
		dir, base := newRepo(t)
		for _, f := range files {
			write(t, dir, f, "package x\n")
		}
		rep, err := Structural(context.Background(), Input{Worktree: dir, GitDir: filepath.Join(dir, ".git"), BaseCommit: base, FilesTouched: touched, OwnedByOthers: others})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	rep := run([]string{"main.go"}, []string{"internal/state.go"}, "main.go", "internal/state.go")
	if rep.Pass || !strings.Contains(details(rep, CheckScope), "internal/state.go") || !strings.Contains(details(rep, CheckScope), "belongs to another task") {
		t.Fatalf("another task's file was allowed: %+v", rep)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "internal/state.go") {
			t.Fatalf("a failed file should not also be a warning: %v", rep.Warnings)
		}
	}
	rep = run([]string{"main.go"}, []string{"internal/state.go"}, "main.go", "helper.go")
	if !rep.Pass || len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "helper.go") {
		t.Fatalf("a new unowned file should only warn: %+v", rep)
	}
	rep = run([]string{"main.go", "internal/state.go"}, []string{"internal/state.go"}, "main.go", "internal/state.go")
	if !rep.Pass || len(rep.Warnings) != 0 {
		t.Fatalf("a file the task lists itself must be allowed: %+v", rep)
	}
}
