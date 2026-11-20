package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/dockervm"
	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// countingVM is a container runtime that counts starts and shares one directory.
type countingVM struct {
	shared []string
	starts int
}

func (c *countingVM) Name() string                           { return "counting" }
func (c *countingVM) Start(context.Context) error            { c.starts++; return nil }
func (c *countingVM) Stop(context.Context) error             { return nil }
func (c *countingVM) Running(context.Context) (int, error)   { return 0, nil }
func (c *countingVM) Status(context.Context) (string, error) { return "up", nil }
func (c *countingVM) Shared() []string                       { return c.shared }

// A docker project whose worktree the VM does not share is refused before the VM is booted:
// it would be an empty directory in the container (AF-12 review finding 2).
func TestDockerSpecRefusesUnsharedPathsBeforeStartingTheVM(t *testing.T) {
	root := t.TempDir()
	spec := sandbox.Spec{
		Worktree: filepath.Join(root, "wt"), StageHome: filepath.Join(root, "home"),
		RepoGitDir: filepath.Join(root, "repo", ".git"), WorktreeGitDir: filepath.Join(root, "repo", ".git", "worktrees", "w"),
		Argv: []string{"true"},
	}
	for _, d := range []string{spec.Worktree, spec.StageHome, spec.WorktreeGitDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	vm := &countingVM{shared: []string{t.TempDir()}}
	e := &Engine{deps: Deps{Containers: dockervm.NewWithDriver(vm, dockervm.Config{Instance: "i1"})}}
	p := store.Project{Slug: "p", Sandbox: "docker"}
	spec.ExtraROBinds = []sandbox.Bind{{Src: t.TempDir(), Dst: "/opt/host-go-mod"}} // the host template's
	if err := e.dockerSpec(context.Background(), p, "af-p-1-2-3", &spec); err == nil || !strings.Contains(err.Error(), "not shared") {
		t.Fatalf("an unshared worktree must be refused: %v", err)
	}
	if vm.starts != 0 {
		t.Fatal("the VM was started for a stage that cannot run")
	}
	vm.shared = []string{root}
	spec.ExtraROBinds = []sandbox.Bind{{Src: t.TempDir(), Dst: "/opt/host-go-mod"}} // not shared, and dropped
	if err := e.dockerSpec(context.Background(), p, "af-p-1-2-3", &spec); err != nil || spec.Docker == nil || spec.Docker.Instance != "i1" {
		t.Fatalf("a shared tree runs: %+v %v", spec.Docker, err)
	}
	if len(spec.ExtraROBinds) != 0 {
		t.Fatalf("the host template's binds must not reach a container: %v", spec.ExtraROBinds)
	}
	if vm.starts != 1 {
		t.Fatalf("starts = %d, want 1", vm.starts)
	}
}
