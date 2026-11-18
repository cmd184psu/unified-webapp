package store

import (
	"errors"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

func finish(t *testing.T, s *Store, task int64, outcome, fp string) int64 {
	t.Helper()
	a, err := s.StartAttempt(ctx, task, model.AttemptNormal, "wt", "worker-m")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAttempt(ctx, a.ID, outcome, fp, "rev-m"); err != nil {
		t.Fatal(err)
	}
	return a.ID
}

func statuses(t *testing.T, s *Store, e int64) map[string]model.Status {
	t.Helper()
	m, err := s.TaskStatuses(ctx, e, testLim)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestTaskStatusesDerivedFromStoredRows(t *testing.T) {
	s, e, ids := queueFixture(t)
	m := statuses(t, s, e.ID)
	if m["A"] != model.StatusReady || m["B"] != model.StatusBlocked || m["C"] != model.StatusReady {
		t.Fatalf("initial: %v", m)
	}
	// A running stage makes A running.
	a, _ := s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "wt", "m")
	st, err := s.CreateStage(ctx, StageSpec{Attempt: a.ID, Role: "worker", Backend: "b", Model: "m", QueueHash: "h"})
	if err != nil {
		t.Fatal(err)
	}
	if statuses(t, s, e.ID)["A"] != model.StatusRunning {
		t.Fatal("A should be running")
	}
	s.StartStage(ctx, st.ID, "job-1")
	s.FinishStage(ctx, st.ID, "ok", "raw", nil)
	s.FinishAttempt(ctx, a.ID, model.OutcomeFailed, "fp", "")
	if statuses(t, s, e.ID)["A"] != model.StatusRepair {
		t.Fatalf("A = %v, want repair", statuses(t, s, e.ID)["A"])
	}
	// Three identical failures halt A as stuck; B is blocked, C is not (AT8).
	finish(t, s, ids["A"], model.OutcomeFailed, "fp")
	finish(t, s, ids["A"], model.OutcomeFailed, "fp")
	m = statuses(t, s, e.ID)
	if m["A"] != model.StatusStuck || m["B"] != model.StatusBlocked || m["C"] != model.StatusReady {
		t.Fatalf("after stuck: %v", m)
	}
	// Override unblocks B; the effort completes once everything is passed or overridden.
	if err := s.OverrideTask(ctx, "alice", ids["A"], "fixed by hand"); err != nil {
		t.Fatal(err)
	}
	finish(t, s, ids["B"], model.OutcomePassed, "")
	m = statuses(t, s, e.ID)
	if m["A"] != model.StatusOverridden || m["B"] != model.StatusPassed || model.AllDone(m) {
		t.Fatalf("after override: %v", m)
	}
	finish(t, s, ids["C"], model.OutcomePassed, "")
	if !model.AllDone(statuses(t, s, e.ID)) {
		t.Fatal("effort should be complete")
	}
}

func TestResetEpochAndInfraRetries(t *testing.T) {
	s, e, ids := queueFixture(t)
	for i := 0; i < 3; i++ {
		finish(t, s, ids["C"], model.OutcomeFailed, "same")
	}
	if statuses(t, s, e.ID)["C"] != model.StatusStuck {
		t.Fatal("C should be stuck")
	}
	if err := s.ResetTask(ctx, "alice", ids["C"], false, testLim); err != nil {
		t.Fatal(err)
	}
	if statuses(t, s, e.ID)["C"] != model.StatusReady {
		t.Fatal("reset should clear stuck")
	}
	if a, _ := s.StartAttempt(ctx, ids["C"], model.AttemptNormal, "", ""); a.N != 4 {
		t.Fatalf("attempt n after reset = %d, want 4 (history is kept)", a.N)
	}
	for i := 0; i < testLim.MaxInfraRetries; i++ {
		if err := s.RecordInfraRetry(ctx, ids["A"], 0, "unreachable"); err != nil {
			t.Fatal(err)
		}
	}
	if statuses(t, s, e.ID)["A"] != model.StatusNeedsAttention {
		t.Fatal("A should need attention")
	}
	s.ResetTask(ctx, "alice", ids["A"], true, testLim)
	if statuses(t, s, e.ID)["A"] != model.StatusReady {
		t.Fatal("reset should clear infra count")
	}
}

func TestConcurrentAttemptsOnDifferentTasks(t *testing.T) { // PX1
	s, e, ids := queueFixture(t)
	a1, err := s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "wt-a", "m1")
	if err != nil {
		t.Fatal(err)
	}
	a2, err := s.StartAttempt(ctx, ids["C"], model.AttemptNormal, "wt-c", "m2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartAttempt(ctx, ids["A"], model.AttemptRepair, "wt-a", "m1"); !errors.Is(err, ErrConflict) {
		t.Fatalf("two in-flight attempts on one task: %v", err)
	}
	m := statuses(t, s, e.ID)
	if m["A"] != model.StatusRunning || m["C"] != model.StatusRunning {
		t.Fatalf("both should run: %v", m)
	}
	if a1.ID == a2.ID {
		t.Fatal("same attempt")
	}
	if err := s.FinishAttempt(ctx, a1.ID, model.OutcomePassed, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAttempt(ctx, a1.ID, model.OutcomePassed, "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("double finish: %v", err)
	}
	if err := s.FinishAttempt(ctx, 999, model.OutcomePassed, "", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("finish unknown: %v", err)
	}
	if err := s.FinishAttempt(ctx, a2.ID, "weird", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad outcome: %v", err)
	}
	if _, err := s.StartAttempt(ctx, ids["B"], "weird", "", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad kind: %v", err)
	}
}

func TestStagesBelongToAttemptOrEffort(t *testing.T) {
	s, e, ids := queueFixture(t)
	a, _ := s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "", "")
	if _, err := s.CreateStage(ctx, StageSpec{Role: "worker"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no owner: %v", err)
	}
	if _, err := s.CreateStage(ctx, StageSpec{Attempt: a.ID, Effort: e.ID, Role: "worker"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("both owners: %v", err)
	}
	if _, err := s.CreateStage(ctx, StageSpec{Attempt: a.ID, Role: "wizard"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad role: %v", err)
	}
	if _, err := s.CreateStage(ctx, StageSpec{Attempt: 999, Role: "worker"}); err == nil {
		t.Fatal("dangling attempt accepted")
	}
	plan, err := s.CreateStage(ctx, StageSpec{Effort: e.ID, Role: "planner", Backend: "b", Model: "m"})
	if err != nil || plan.Effort != e.ID || plan.Attempt != 0 || plan.State != model.StageQueued {
		t.Fatalf("plan stage: %+v %v", plan, err)
	}
	work, _ := s.CreateStage(ctx, StageSpec{Attempt: a.ID, Role: "worker", QueueHash: "h1"})
	if open, _ := s.OpenStages(ctx); len(open) != 2 {
		t.Fatalf("open stages = %d", len(open))
	}
	if err := s.FinishStage(ctx, work.ID, "", "", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank outcome: %v", err)
	}
	if err := s.StartStage(ctx, work.ID, "job-7"); err != nil {
		t.Fatal(err)
	}
	if err := s.StartStage(ctx, work.ID, "job-8"); !errors.Is(err, ErrConflict) {
		t.Fatalf("restart: %v", err)
	}
	if err := s.FinishStage(ctx, work.ID, "done", "raw text", []byte(`{"summary":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishStage(ctx, work.ID, "done", "", nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("double finish: %v", err)
	}
	if err := s.FinishStage(ctx, 999, "done", "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("finish unknown: %v", err)
	}
	got, _ := s.GetStage(ctx, work.ID)
	if got.State != model.StageDone || got.LaneJobID != "job-7" || got.RawOutput != "raw text" || string(got.Parsed) != `{"summary":"x"}` || got.QueueHash != "h1" {
		t.Fatalf("stage = %+v", got)
	}
	if l, _ := s.ListAttemptStages(ctx, a.ID); len(l) != 1 {
		t.Fatalf("attempt stages = %d", len(l))
	}
	if l, _ := s.ListEffortStages(ctx, e.ID); len(l) != 1 || l[0].Role != "planner" {
		t.Fatalf("effort stages = %+v", l)
	}
}

func TestWaitingReasonProgressAndBaseline(t *testing.T) {
	s, e, ids := queueFixture(t)
	if err := s.SetWaitingReason(ctx, ids["A"], "no eligible backend for role reviewer"); err != nil {
		t.Fatal(err)
	}
	tk, _ := s.GetTask(ctx, ids["A"])
	if tk.WaitingReason == "" || tk.Key != "A" {
		t.Fatalf("task = %+v", tk)
	}
	// The waiting reason is not part of the queue hash and does not change status.
	if q, _ := s.GetQueue(ctx, e.ID); q.Hash != model.QueueHash(q.Tasks) {
		t.Fatal("waiting reason leaked into the hash")
	}
	s.SetWaitingReason(ctx, ids["A"], "")
	if tk, _ = s.GetTask(ctx, ids["A"]); tk.WaitingReason != "" {
		t.Fatal("reason not cleared")
	}
	if err := s.SetWaitingReason(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
	if _, err := s.GetTask(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetTask: %v", err)
	}
	for _, p := range []string{"one", "two", "three"} {
		s.AppendProgress(ctx, ids["A"], 0, p)
	}
	if l, _ := s.ListProgress(ctx, ids["A"], 2); len(l) != 2 || l[0] != "two" || l[1] != "three" {
		t.Fatalf("progress = %v", l)
	}
	if _, _, err := s.LatestBaseline(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("baseline: %v", err)
	}
	s.SaveBaseline(ctx, e.ID, "abc", `{"failures":[]}`)
	s.SaveBaseline(ctx, e.ID, "def", `{"failures":["x"]}`)
	if c, r, _ := s.LatestBaseline(ctx, e.ID); c != "def" || r != `{"failures":["x"]}` {
		t.Fatalf("baseline = %s %s", c, r)
	}
}

func TestEffortDeleteCascadesAndProjectRestricted(t *testing.T) {
	s, e, ids := queueFixture(t)
	a, _ := s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "", "")
	s.CreateStage(ctx, StageSpec{Attempt: a.ID, Role: "worker"})
	if _, err := s.db.Exec(`DELETE FROM projects WHERE slug = 'proj'`); err == nil {
		t.Fatal("project deleted under an effort")
	}
	if _, err := s.db.Exec(`DELETE FROM efforts WHERE id = ?`, e.ID); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"documents", "queues", "tasks", "task_deps", "attempts", "stages"} {
		if n := countRows(t, s, tbl); n != 0 {
			t.Errorf("%s has %d rows after effort delete", tbl, n)
		}
	}
}

func TestResetClosesOpenAttemptSoANewOneCanStart(t *testing.T) {
	s, e, ids := queueFixture(t)
	open, err := s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "wt", "m")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < testLim.MaxInfraRetries; i++ {
		s.RecordInfraRetry(ctx, ids["A"], 0, "unreachable")
	}
	if statuses(t, s, e.ID)["A"] != model.StatusNeedsAttention {
		t.Fatal("A should need attention with an attempt still open")
	}
	if _, err := s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "", ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("start before reset: %v", err)
	}
	if err := s.ResetTask(ctx, "alice", ids["A"], false, testLim); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAttempt(ctx, open.ID); got.Outcome != model.OutcomeAbandoned || got.Finished == 0 {
		t.Fatalf("open attempt = %+v", got)
	}
	if statuses(t, s, e.ID)["A"] != model.StatusReady {
		t.Fatal("A should be ready after reset")
	}
	if _, err := s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "", ""); err != nil {
		t.Fatalf("start after reset: %v", err)
	}
}

func TestAbandonedAttemptsDoNotCount(t *testing.T) { // AT7: stop/cancel/restart consume no attempt
	s, e, ids := queueFixture(t)
	for i := 0; i < testLim.MaxAttempts+2; i++ {
		finish(t, s, ids["C"], model.OutcomeAbandoned, "")
	}
	if statuses(t, s, e.ID)["C"] != model.StatusReady {
		t.Fatalf("abandoned attempts changed status: %v", statuses(t, s, e.ID)["C"])
	}
	finish(t, s, ids["C"], model.OutcomeFailed, "x")
	if statuses(t, s, e.ID)["C"] != model.StatusRepair {
		t.Fatal("one real failure should repair")
	}
}

func TestResetRefusesNonHaltedTask(t *testing.T) {
	s, _, ids := queueFixture(t)
	if err := s.ResetTask(ctx, "alice", ids["C"], false, testLim); !errors.Is(err, ErrConflict) {
		t.Fatalf("reset of ready task: %v", err)
	}
	s.StartAttempt(ctx, ids["C"], model.AttemptNormal, "", "")
	if err := s.ResetTask(ctx, "alice", ids["C"], false, testLim); !errors.Is(err, ErrConflict) {
		t.Fatalf("reset of running task: %v", err)
	}
	if err := s.ResetTask(ctx, "alice", 999, false, testLim); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reset unknown: %v", err)
	}
}

func TestOneOpenAttemptIndex(t *testing.T) {
	s, _, ids := queueFixture(t)
	s.StartAttempt(ctx, ids["A"], model.AttemptNormal, "", "")
	if _, err := s.db.Exec(`INSERT INTO attempts (task, n, kind, started) VALUES (?, 9, 'normal', 0)`, ids["A"]); err == nil {
		t.Fatal("index allowed a second open attempt")
	}
}
