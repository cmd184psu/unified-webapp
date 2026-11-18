//go:build darwin

package opencode

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// The macOS counterpart of TestSandboxedRunUsesWorktreeAsCwd: the same
// guarantees, expressed for sandbox-exec (no path remapping, so HOME is the
// stage home itself and the binary keeps its host path).
func TestSandboxedRunUsesWorktreeAsCwd(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo", ".git")
	wtGit := filepath.Join(repo, "worktrees", "w1")
	wt := filepath.Join(root, "wt")
	stage := filepath.Join(root, "stage")
	for _, d := range []string{wtGit, wt, HomeDir(stage)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := GenerateConfig(specs(), stage); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	home := HomeDir(stage)
	factory := SandboxCommand(func(runner.Request) (string, string, error) { return repo, wtGit, nil })
	for role, readOnly := range map[string]bool{"worker": false, "reviewer": true} {
		req := runner.Request{Role: role, Backend: runner.Backend{Name: "local"}, Model: "m", Workdir: wt, StageDir: stage, PromptPath: "p"}
		cmd, err := factory(context.Background(), req, Argv(bin, req), IsolationEnv(stage, []string{"PATH=/usr/bin"}, nil))
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Dir != wt {
			t.Errorf("%s: cwd = %q, want the worktree %q", role, cmd.Dir, wt)
		}
		profile := cmd.Args[2]
		wtWrite := strings.Contains(profile, "(allow file-write* (subpath \""+home+"\") (subpath \""+wt+"\")")
		if wtWrite == readOnly {
			t.Errorf("%s: worktree writable = %v, want %v", role, wtWrite, !readOnly)
		}
		if got := strings.Join(cmd.Args[3:6], " "); got != bin+" run --pure" {
			t.Errorf("%s: argv = %q, want the real binary running opencode run --pure", role, got)
		}
		env := strings.Join(cmd.Env, "\n") + "\n"
		for _, want := range []string{"HOME=" + home + "\n", "XDG_CONFIG_HOME=" + home + "/.config\n"} {
			if !strings.Contains(env, want) {
				t.Errorf("%s: env lacks %q", role, strings.TrimSpace(want))
			}
		}
		if strings.Contains(env, stage+"/xdg") || strings.Contains(env, "OPENCODE_CONFIG") {
			t.Errorf("%s: leaked opencode config variable or stage xdg dir", role)
		}
		frozen, _ := os.ReadFile(ConfigPath(stage))
		if copied, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json")); err != nil || !bytes.Equal(copied, frozen) {
			t.Errorf("%s: sandbox config copy missing or different: %v", role, err)
		}
	}
}
