package store

import (
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// attemptBy runs one attempt of task by backend/model, with a worker stage that
// ended in stageOutcome, and finishes the attempt with outcome and fp.
func attemptBy(t *testing.T, s *Store, task int64, backend, mdl, stageOutcome, outcome, fp string) {
	t.Helper()
	a, err := s.StartAttempt(ctx, task, model.AttemptNormal, "wt", mdl)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.CreateStage(ctx, StageSpec{Attempt: a.ID, Role: "worker", Backend: backend, Model: mdl})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishStage(ctx, st.ID, stageOutcome, "", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAttempt(ctx, a.ID, outcome, fp, ""); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, s *Store, k StrikeKey) int {
	t.Helper()
	m, err := s.StrikeCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return m[k]
}

// H7: a strike is an unreadable output, a git policy violation, or the same
// failure twice in a row from the same worker. A different worker repeating a
// failure, or a different failure, is not a strike. Clearing forgives them.
func TestH7StrikesAreDerivedAndCanBeCleared(t *testing.T) {
	s, _, ids := queueFixture(t)
	k := StrikeKey{"gpu", "dim", "worker"}

	attemptBy(t, s, ids["A"], "gpu", "dim", "infra:unparseable", model.OutcomeFailed, "u1")
	if n := count(t, s, k); n != 1 {
		t.Fatalf("unparseable output: %d strikes, want 1", n)
	}
	attemptBy(t, s, ids["A"], "gpu", "dim", "pass", model.OutcomeFailed, "v:x")
	if n := count(t, s, k); n != 1 {
		t.Fatalf("a first failure is not a strike: %d", n)
	}
	attemptBy(t, s, ids["A"], "gpu", "other", "pass", model.OutcomeFailed, "v:x")
	if n := count(t, s, StrikeKey{"gpu", "other", "worker"}); n != 0 {
		t.Fatalf("another model hitting the same failure is not a repeat: %d", n)
	}
	attemptBy(t, s, ids["A"], "gpu", "dim", "pass", model.OutcomeFailed, "v:x")
	attemptBy(t, s, ids["A"], "gpu", "dim", "pass", model.OutcomeFailed, "v:x")
	if n := count(t, s, k); n != 2 {
		t.Fatalf("a repeat by the same worker (u1 + one repeat) = %d, want 2", n)
	}
	attemptBy(t, s, ids["C"], "gpu", "dim", "pass", model.OutcomePolicyViolation, "")
	if n := count(t, s, k); n != 3 {
		t.Fatalf("policy violation = %d strikes, want 3", n)
	}

	time.Sleep(3 * time.Millisecond)
	if err := s.ClearStrikes(ctx, "owner", k); err != nil {
		t.Fatal(err)
	}
	if n := count(t, s, k); n != 0 {
		t.Fatalf("after clear: %d", n)
	}
	time.Sleep(3 * time.Millisecond)
	attemptBy(t, s, ids["C"], "gpu", "dim", "infra:unparseable", model.OutcomeFailed, "u2")
	if n := count(t, s, k); n != 1 {
		t.Fatalf("a new strike after the clear = %d, want 1", n)
	}
	if err := s.ClearStrikes(ctx, "owner", StrikeKey{Backend: "gpu"}); err == nil {
		t.Fatal("clear without model and role accepted")
	}
}
