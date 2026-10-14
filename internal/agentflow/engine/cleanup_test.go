package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// enableCleanup switches the project's cleanup stage on (AT11).
func (c *chainRun) enableCleanup() {
	c.t.Helper()
	ctx := context.Background()
	p, err := c.st.GetProject(ctx, c.slug)
	if err != nil {
		c.t.Fatal(err)
	}
	p.CleanupEnabled = true
	if err := c.st.UpdateProject(ctx, "t", p); err != nil {
		c.t.Fatal(err)
	}
}

func (c *chainRun) waitComplete() model.Effort {
	c.t.Helper()
	waitFor(c.t, "effort complete", func() bool {
		ef, err := c.st.GetEffort(context.Background(), c.effort)
		return err == nil && ef.Status == model.EffortComplete
	})
	ef, _ := c.st.GetEffort(context.Background(), c.effort)
	return ef
}

func roles(t *testing.T, c *chainRun) string {
	t.Helper()
	stages, err := c.st.ListAttemptStages(context.Background(), c.attempts(0)[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var rs []string
	for _, st := range stages {
		rs = append(rs, st.Role)
	}
	return strings.Join(rs, ",")
}

// AC-AT11: a cleanup that breaks verify is rolled back to the pre-cleanup
// checkpoint, and the merged content equals the reviewed content.
func TestAT11_CleanupBreakingVerifyIsRolledBack(t *testing.T) {
	c := newChainRun(t, []string{"sh check.sh"}, chainDefs()[:1])
	c.enableCleanup()
	c.script(1, reviewPass, nil)
	c.fake.Script("cleanup", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		// Makes check.sh fail (a new failure against the baseline) and rewrites
		// the reviewed file.
		for _, f := range []string{"a.txt", "b.txt"} {
			if err := os.WriteFile(filepath.Join(r.Workdir, f), []byte("x\n"), 0o644); err != nil {
				return err
			}
		}
		return os.WriteFile(filepath.Join(r.Workdir, "T1.txt"), []byte("broken by cleanup\n"), 0o644)
	}})
	c.run()
	ef := c.waitComplete()

	if got := gitT(t, c.repo, "show", ef.IntegrationBranch+":T1.txt"); got != "work of T1" {
		t.Fatalf("merged T1.txt = %q, want the reviewed content", got)
	}
	files := gitT(t, c.repo, "ls-tree", "-r", "--name-only", ef.IntegrationBranch)
	if strings.Contains(files, "a.txt") || strings.Contains(files, "b.txt") {
		t.Fatalf("cleanup leftovers were merged:\n%s", files)
	}
	if got := roles(t, c); got != "worker,verify,reviewer,cleanup,verify,merge" {
		t.Fatalf("stage chain = %s", got)
	}
	if as := c.attempts(0); len(as) != 1 || as[0].Outcome != model.OutcomePassed {
		t.Fatalf("attempts = %+v", as)
	}
	logs, err := c.st.ListProgress(context.Background(), c.tasks[0].ID, 50)
	if err != nil || !strings.Contains(strings.Join(logs, "\n"), "cleanup rolled back") {
		t.Fatalf("progress = %v (%v)", logs, err)
	}
}

// AT11: a cleanup that keeps verify green is merged.
func TestAT11_GoodCleanupIsMerged(t *testing.T) {
	c := newChainRun(t, []string{"sh check.sh"}, chainDefs()[:1])
	c.enableCleanup()
	c.script(1, reviewPass, nil)
	c.fake.Script("cleanup", runner.Step{Reply: workerOK, Edit: func(r runner.Request) error {
		return os.WriteFile(filepath.Join(r.Workdir, "T1.txt"), []byte("tidied\n"), 0o644)
	}})
	c.run()
	ef := c.waitComplete()

	if got := gitT(t, c.repo, "show", ef.IntegrationBranch+":T1.txt"); got != "tidied" {
		t.Fatalf("merged T1.txt = %q, want the cleaned content", got)
	}
	if got := roles(t, c); got != "worker,verify,reviewer,cleanup,verify,merge" {
		t.Fatalf("stage chain = %s", got)
	}
	// The cleanup agent gets the cleanup prompt and is not a worker call.
	if calls := c.fake.CallsFor("cleanup"); len(calls) != 1 || !strings.Contains(calls[0].Prompt, "# Cleanup") {
		t.Fatalf("cleanup calls = %+v", calls)
	}
}

// AT11: with cleanup disabled (the default) the chain is unchanged.
func TestAT11_DisabledByDefault(t *testing.T) {
	c := newChainRun(t, nil, chainDefs()[:1])
	c.script(1, reviewPass, nil)
	c.run()
	c.waitComplete()
	if got := roles(t, c); got != "worker,verify,reviewer,merge" {
		t.Fatalf("stage chain = %s", got)
	}
}
