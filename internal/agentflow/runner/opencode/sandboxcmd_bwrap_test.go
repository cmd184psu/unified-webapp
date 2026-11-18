//go:build !darwin

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

func TestSandboxedRunUsesWorktreeAsCwd(t *testing.T) {
	root := t.TempDir()
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
	factory := SandboxCommand(func(runner.Request) (string, string, error) { return repo, wtGit, nil })
	for role, readOnly := range map[string]bool{"worker": false, "reviewer": true} {
		req := runner.Request{Role: role, Backend: runner.Backend{Name: "local"}, Model: "m", Workdir: wt, StageDir: stage, PromptPath: "p"}
		cmd, err := factory(context.Background(), req, Argv(bin, req), IsolationEnv(stage, []string{"PATH=/usr/bin"}, nil))
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(cmd.Args, "\x00")
		if !strings.Contains(args, "--chdir\x00"+wt+"\x00") {
			t.Errorf("%s: sandbox does not chdir to the worktree: %q", role, cmd.Args)
		}
		bindKind := "--bind\x00" + wt + "\x00" + wt
		if readOnly {
			bindKind = "--ro-bind\x00" + wt + "\x00" + wt
		}
		if !strings.Contains(args, bindKind) {
			t.Errorf("%s: worktree bind mode wrong: %q", role, cmd.Args)
		}
		for _, want := range []string{sandboxBin + "\x00run\x00--pure", "--bind\x00" + HomeDir(stage) + "\x00/home/agent"} {
			if !strings.Contains(args, want) {
				t.Errorf("%s: sandbox args lack %q", role, want)
			}
		}
		frozen, _ := os.ReadFile(ConfigPath(stage))
		if copied, err := os.ReadFile(filepath.Join(HomeDir(stage), ".config", "opencode", "opencode.json")); err != nil || !bytes.Equal(copied, frozen) {
			t.Errorf("%s: sandbox config copy missing or different: %v", role, err)
		}
		if !strings.Contains(args, "XDG_CONFIG_HOME\x00/home/agent/.config\x00") || strings.Contains(args, stage+"/xdg") {
			t.Errorf("%s: XDG_CONFIG_HOME should be the sandbox default", role)
		}
		if strings.Contains(args, "OPENCODE_CONFIG_DIR") || strings.Contains(args, "OPENCODE_CONFIG\x00") {
			t.Errorf("%s: leaked opencode config variable", role)
		}
	}
}
