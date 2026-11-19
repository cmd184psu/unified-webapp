package engine

import (
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// TestVerifyWorktreeCopiesOnlyWhenAsked: verify_in_copy runs the feedback
// commands on a copy of the worktree, every other project on the worktree
// itself (AF-6).
func TestVerifyWorktreeCopiesOnlyWhenAsked(t *testing.T) {
	wt, home := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, writable, err := verifyWorktree(store.Project{}, wt, home); err != nil || got != wt || writable {
		t.Fatalf("without verify_in_copy: %q writable=%v err=%v", got, writable, err)
	}
	got, writable, err := verifyWorktree(store.Project{VerifyInCopy: true}, wt, home)
	if err != nil {
		t.Fatal(err)
	}
	if !writable || got != filepath.Join(home, "verify-copy") {
		t.Fatalf("copy = %q writable=%v", got, writable)
	}
	if _, err := os.Stat(filepath.Join(got, "a.txt")); err != nil {
		t.Fatalf("copy is missing the sources: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must not be copied")
	}
}

// TestFeedbackSpecAppliesProjectSettings: the project's toolchains and feedback
// environment reach the sandbox, and the project cannot override HOME (AF-6).
func TestFeedbackSpecAppliesProjectSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{".dotnet", ".nuget/packages"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".dotnet", "dotnet"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := &Engine{deps: Deps{Sandbox: sandbox.Spec{Env: map[string]string{"GOPROXY": "off"}}}}
	admin := filepath.Join(t.TempDir(), "repo", ".git", "worktrees", "w")
	p := store.Project{Toolchains: []string{"dotnet"}, FeedbackEnv: map[string]string{"CIV3_PATH": "/civ3"}}
	spec, err := e.feedbackSpec(p, t.TempDir(), admin, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if spec.Env["CIV3_PATH"] != "/civ3" || spec.Env["DOTNET_ROOT"] != "/sandbox/dotnet" || spec.Env["GOPROXY"] != "off" {
		t.Fatalf("Env = %v", spec.Env)
	}
	if len(spec.PathDirs) != 1 || spec.PathDirs[0] != "/sandbox/dotnet" {
		t.Fatalf("PathDirs = %v", spec.PathDirs)
	}
	if !spec.ReadOnlyWorktree {
		t.Fatal("a feedback worktree is read-only unless the caller runs in a copy")
	}
}
