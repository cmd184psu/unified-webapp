package obsidianoid_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/obsidianoid"
)

func TestGitIsAvailable(t *testing.T) {
	t.Run("true when .git directory present", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.Mkdir(filepath.Join(dir, ".git"), 0o755)
		if !obsidianoid.GitIsAvailable(dir) {
			t.Error("expected true")
		}
	})

	t.Run("true when .git file present (worktree)", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: ../main/.git/worktrees/x"), 0o644)
		if !obsidianoid.GitIsAvailable(dir) {
			t.Error("expected true")
		}
	})

	t.Run("false when .git absent", func(t *testing.T) {
		dir := t.TempDir()
		if obsidianoid.GitIsAvailable(dir) {
			t.Error("expected false")
		}
	})
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func defaultBranch(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
}

func TestGitSync(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}

	remote := t.TempDir()
	mustGit(t, remote, "init", "--bare")

	work := t.TempDir()
	mustGit(t, work, "init")
	mustGit(t, work, "config", "user.email", "test@obsidianoid.test")
	mustGit(t, work, "config", "user.name", "Obsidianoid Test")
	mustGit(t, work, "remote", "add", "origin", remote)

	_ = os.WriteFile(filepath.Join(work, "README.md"), []byte("# vault"), 0o644)
	mustGit(t, work, "add", "-A")
	mustGit(t, work, "commit", "-m", "init")
	branch := defaultBranch(t, work)
	mustGit(t, work, "push", "--set-upstream", "origin", branch)

	t.Run("stages, commits and pushes a new file", func(t *testing.T) {
		_ = os.WriteFile(filepath.Join(work, "Note.md"), []byte("# Hello"), 0o644)
		out, err := obsidianoid.GitSync(work, "add note")
		if err != nil {
			t.Fatalf("GitSync error: %v\nOutput: %s", err, out)
		}
		cmd := exec.Command("git", "log", "--oneline", "-1")
		cmd.Dir = work
		log, _ := cmd.Output()
		if !strings.Contains(string(log), "add note") {
			t.Errorf("expected 'add note' in log, got: %s", log)
		}
	})

	t.Run("nothing to commit is not an error", func(t *testing.T) {
		_, err := obsidianoid.GitSync(work, "empty sync")
		if err != nil {
			t.Fatalf("expected no error on clean tree, got: %v", err)
		}
	})
}
