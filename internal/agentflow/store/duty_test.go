package store

import (
	"path/filepath"
	"testing"
	"time"
)

// H20: a backend under a duty cap rests after a stage for the stage's running
// time times (100-duty)/duty, up to the longest rest, and a cap of 100 is no cap.
func TestH20_DutyCapRestsABackendAfterAStage(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// The rest depends only on the stage rows, so they are written directly.
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	add := func(backend string, ran, endedAgo time.Duration) {
		end := now.Add(-endedAgo).UnixMilli()
		if _, err := s.db.Exec(`INSERT INTO stages (effort, role, backend, model, state, started, finished) VALUES (1, 'planner', ?, 'm', 'done', ?, ?)`,
			backend, end-ran.Milliseconds(), end); err != nil {
			t.Fatal(err)
		}
	}
	ctx := t.Context()
	rest := func(backend string, duty int) time.Duration {
		t.Helper()
		got, err := s.BackendRestUntil(ctx, backend, duty, now)
		if err != nil {
			t.Fatal(err)
		}
		if got == 0 {
			return 0
		}
		return time.UnixMilli(got).Sub(now).Round(time.Second)
	}
	if got := rest("gpu", 50); got != 0 {
		t.Fatalf("no stages: rest %v", got)
	}
	add("gpu", 10*time.Minute, 4*time.Minute) // 10 min at 50% owes 10 min, 4 already passed
	add("other", 20*time.Minute, time.Minute)
	if got := rest("gpu", 50); got != 6*time.Minute {
		t.Fatalf("50%%: rest %v, want 6m", got)
	}
	if got := rest("gpu", 25); got != 26*time.Minute { // owes 30 min (capped), 4 passed
		t.Fatalf("25%%: rest %v, want 26m", got)
	}
	if got := rest("gpu", 100); got != 0 {
		t.Fatalf("100%%: rest %v, want none", got)
	}
	if got := rest("gpu", 90); got != 0 { // owes ~1m, passed
		t.Fatalf("90%%: rest %v, want none", got)
	}
	if got := rest("quiet", 50); got != 0 {
		t.Fatalf("a backend with no stages rests %v", got)
	}
}

func TestH20_DutyPercentIsStoredAndValidated(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	b := Backend{Name: "gpu", Host: "http://localhost:11434", APIType: "ollama", Slots: 1, DutyPercent: 50}
	if err := s.CreateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBackend(ctx, "gpu")
	if got.DutyPercent != 50 {
		t.Fatalf("duty = %d, want 50", got.DutyPercent)
	}
	got.DutyPercent = 100
	if err := s.UpdateBackend(ctx, "t", got); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetBackend(ctx, "gpu")
	if got.DutyPercent != 0 {
		t.Fatalf("100 is stored as no cap: got %d", got.DutyPercent)
	}
	got.DutyPercent = 5
	if err := s.UpdateBackend(ctx, "t", got); err == nil {
		t.Fatal("a cap below 10 was accepted")
	}
}
