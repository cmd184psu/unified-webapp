package gitsvc

import (
	"path/filepath"
	"testing"
)

func TestAF15_BranchContainsAndTip(t *testing.T) {
	repo := newRepo(t)
	tgit(t, repo, "checkout", "-q", "-b", "feature/base")
	write(t, repo, "base.txt", "base work\n")
	tgit(t, repo, "add", "-A")
	tgit(t, repo, "commit", "-q", "-m", "base work")
	tgit(t, repo, "checkout", "-q", "-b", "feature/turn-loop")
	write(t, repo, "turn.txt", "turn work\n")
	tgit(t, repo, "add", "-A")
	tgit(t, repo, "commit", "-q", "-m", "turn work")
	s, err := New(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.BranchContains(bg, repo, "feature/turn-loop", "feature/base")
	if err != nil || !ok {
		t.Fatalf("feature/turn-loop should contain feature/base: %v %v", ok, err)
	}
	ok, err = s.BranchContains(bg, repo, "feature/base", "feature/turn-loop")
	if err != nil || ok {
		t.Fatalf("feature/base must not contain feature/turn-loop: %v %v", ok, err)
	}
	tip, err := s.BranchTip(bg, repo, "feature/turn-loop")
	if err != nil || tip != tgit(t, repo, "rev-parse", "feature/turn-loop") {
		t.Fatalf("tip = %q, %v", tip, err)
	}
}
