package store

import (
	"context"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// AF-6b: a task's toolchains survive the queue round trip, and a stage keeps
// the relaxations recorded for it.
func TestTaskToolchainsAndStageRelaxationsRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.CreateProject(ctx, "t", Project{Slug: "cs", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	ef, err := s.CreateEffort(ctx, "t", "cs", "e", false)
	if err != nil {
		t.Fatal(err)
	}
	defs := []model.TaskDef{{
		Key: "T1", Phase: "P1", Title: "t", Prompt: "p", Acceptance: []string{"a"},
		FilesTouched: []string{"src/App.cs"}, Toolchains: []string{"dotnet"},
	}, {
		Key: "T2", Phase: "P1", Title: "t2", Prompt: "p", Acceptance: []string{"a"},
		FilesTouched: []string{"main.go"},
	}}
	q, err := s.SaveQueue(ctx, "t", ef.ID, defs)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Tasks) != 2 || len(q.Tasks[0].Toolchains) != 1 || q.Tasks[0].Toolchains[0] != "dotnet" || len(q.Tasks[1].Toolchains) != 0 {
		t.Fatalf("queue toolchains = %+v", q.Tasks)
	}
	tasks, err := s.ListTasks(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}

	a, err := s.StartAttempt(ctx, tasks[0].ID, model.AttemptNormal, "wt", "m")
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.CreateStage(ctx, StageSpec{Attempt: a.ID, Role: "verify", Backend: "verify", Relaxations: []string{"loopback", "verify_in_copy"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetStage(ctx, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Relaxations) != 2 || got.Relaxations[0] != "loopback" || got.Relaxations[1] != "verify_in_copy" {
		t.Fatalf("stage relaxations = %v", got.Relaxations)
	}

	// An unknown toolchain is refused when the queue is saved.
	bad := []model.TaskDef{{Key: "T3", Phase: "P1", Title: "t", Prompt: "p", Acceptance: []string{"a"},
		FilesTouched: []string{"a.cs"}, Toolchains: []string{"ruby"}}}
	if _, err := s.SaveQueue(ctx, "t", ef.ID, bad); err == nil {
		t.Fatal("an unknown toolchain was accepted")
	}
}
