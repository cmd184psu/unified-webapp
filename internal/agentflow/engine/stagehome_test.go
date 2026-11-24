// Copyright 2026 FreeSociety3 contributors
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func stageHomeFixture(t *testing.T) (*Engine, *store.Store, model.Effort, model.Attempt) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateProject(ctx, "test", store.Project{Slug: "test", RepoPath: dir, ModelScope: "any"}); err != nil {
		t.Fatal(err)
	}
	effort, err := st.CreateEffort(ctx, "test", "test", "test", false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := st.SaveQueue(ctx, "test", effort.ID, []model.TaskDef{{Key: "T1", Phase: "P1", Title: "task", Prompt: "work", Acceptance: []string{"done"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApproveQueue(ctx, "test", effort.ID, q.Hash); err != nil {
		t.Fatal(err)
	}
	tasks, err := st.ListTasks(ctx, effort.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks: %v, %v", tasks, err)
	}
	attempt, err := st.StartAttempt(ctx, tasks[0].ID, model.AttemptNormal, dir, "worker")
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{deps: Deps{Store: st, DataDir: dir}}, st, effort, attempt
}

func stageHomeCompleted(t *testing.T, e *Engine, st *store.Store, spec store.StageSpec) model.Stage {
	t.Helper()
	ctx := context.Background()
	stage, err := st.CreateStage(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishStage(ctx, stage.ID, "done", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB().ExecContext(ctx, `UPDATE stages SET finished = ? WHERE id = ?`, time.Now().Add(-365*24*time.Hour).UnixMilli(), stage.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(e.StageHomeDir(stage.ID), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"prompt.md", "events.jsonl"} {
		if err := os.WriteFile(filepath.Join(e.StageDir(stage.ID), name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return stage
}

func stageHomeExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Stat(path)
	if want && err != nil || !want && !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v, want exists=%v", path, err, want)
	}
}

func TestStageHomePruneSkipsReviewPendingAttempt(t *testing.T) {
	ctx := context.Background()
	e, st, _, attempt := stageHomeFixture(t)
	stage := stageHomeCompleted(t, e, st, store.StageSpec{Attempt: attempt.ID, Role: "worker"})
	for _, prune := range []struct {
		name string
		run  func(context.Context) (int, error)
	}{
		{"force", e.PruneStageHomesNow},
		{"age", func(ctx context.Context) (int, error) { return e.PruneExpiredStageHomes(ctx, false) }},
	} {
		t.Run(prune.name, func(t *testing.T) {
			n, err := prune.run(ctx)
			if err != nil || n != 0 {
				t.Fatalf("open attempt prune = %d, %v; want 0", n, err)
			}
			stageHomeExists(t, e.StageHomeDir(stage.ID), true)
		})
	}
	if err := st.FinishAttempt(ctx, attempt.ID, model.OutcomePassed, "", ""); err != nil {
		t.Fatal(err)
	}
	if n, err := e.PruneStageHomesNow(ctx); err != nil || n != 1 {
		t.Fatalf("closed attempt prune = %d, %v; want 1", n, err)
	}
	stageHomeExists(t, e.StageHomeDir(stage.ID), false)
	for _, name := range []string{"prompt.md", "events.jsonl"} {
		stageHomeExists(t, filepath.Join(e.StageDir(stage.ID), name), true)
	}
	if n, err := e.PruneStageHomesNow(ctx); err != nil || n != 0 {
		t.Fatalf("repeat prune = %d, %v; want 0", n, err)
	}
}

func TestStageHomePruneAttemptLookupErrorAndEffort(t *testing.T) {
	ctx := context.Background()
	e, st, effort, attempt := stageHomeFixture(t)
	missing := stageHomeCompleted(t, e, st, store.StageSpec{Attempt: attempt.ID, Role: "worker"})
	effortStage := stageHomeCompleted(t, e, st, store.StageSpec{Effort: effort.ID, Role: "planner"})
	// Leave the stage intact but point it at a missing attempt in this temporary DB.
	for _, query := range []string{"PRAGMA foreign_keys = OFF", `UPDATE stages SET attempt = 999999 WHERE id = ?`, "PRAGMA foreign_keys = ON"} {
		var err error
		if query == `UPDATE stages SET attempt = 999999 WHERE id = ?` {
			_, err = st.DB().ExecContext(ctx, query, missing.ID)
		} else {
			_, err = st.DB().ExecContext(ctx, query)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.GetAttempt(ctx, 999999); err == nil {
		t.Fatal("expected missing attempt lookup to fail")
	}
	if n, err := e.PruneExpiredStageHomes(ctx, false); err != nil || n != 1 {
		t.Fatalf("age prune = %d, %v; want 1", n, err)
	}
	stageHomeExists(t, e.StageHomeDir(missing.ID), true)
	stageHomeExists(t, e.StageHomeDir(effortStage.ID), false)
	if _, err := st.GetStage(ctx, missing.ID); err != nil {
		t.Fatalf("stage row lost: %v", err)
	}
	if n, err := e.PruneStageHomesNow(ctx); err != nil || n != 0 {
		t.Fatalf("force prune with lookup error = %d, %v; want 0", n, err)
	}
	stageHomeExists(t, e.StageHomeDir(missing.ID), true)
}
