package verify

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/sandbox"
)

// Feedback commands run at once; in a docker project each gets its own container name, or
// the second docker run of the stage's name fails with a conflict (found on dev, AF-12).
func TestDockerFeedbackCommandsGetTheirOwnContainers(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "names")
	script := "#!/bin/sh\nif [ \"$1\" = run ]; then while [ $# -gt 0 ]; do [ \"$1\" = --name ] && echo \"$2\" >> " + log + "; shift; done; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	root := t.TempDir()
	spec := sandbox.Spec{Worktree: filepath.Join(root, "wt"), StageHome: filepath.Join(root, "home"),
		RepoGitDir: filepath.Join(root, "repo", ".git"), WorktreeGitDir: filepath.Join(root, "repo", ".git", "worktrees", "w"),
		ReadOnlyWorktree: true,
		Docker:           &sandbox.Docker{Image: "agentflow-worker:0.1", Name: "af-p-1-2-9", Network: sandbox.NetworkNone}}
	for _, d := range []string{spec.Worktree, spec.StageHome, spec.WorktreeGitDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RunFeedback(context.Background(), FeedbackConfig{Spec: spec}, []Argv{{"true"}, {"uname", "-s"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(log)
	names := strings.Fields(string(b))
	sort.Strings(names)
	if strings.Join(names, ",") != "af-p-1-2-9-c0,af-p-1-2-9-c1" {
		t.Fatalf("container names %v, want one per command", names)
	}
	if spec.Docker.Name != "af-p-1-2-9" {
		t.Fatal("the caller's spec must not change")
	}
}
