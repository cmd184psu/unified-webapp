package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// A task saved with risk:risky and a reason reads back both after a reload;
// a task saved without a risk is normal (AF-26).
func TestTaskRiskRoundTrips(t *testing.T) {
	s, path := openTemp(t)
	newProject(t, s, "proj")
	e := newEffort(t, s, "proj")

	risky := qdef("R1")
	risky.Risk, risky.RiskReason = model.RiskRisky, "touches the migration"
	if _, err := s.SaveQueue(ctx, "tester", e.ID, []model.TaskDef{qdef("N1"), risky}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	ts, err := s.ListTasks(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]model.Task{}
	for _, tk := range ts {
		got[tk.Key] = tk
	}
	if got["R1"].Risk != model.RiskRisky || got["R1"].RiskReason != "touches the migration" {
		t.Fatalf("risky task after reload = %+v", got["R1"].TaskDef)
	}
	if got["N1"].Risk != model.RiskNormal || got["N1"].RiskReason != "" {
		t.Fatalf("unset task after reload = %+v", got["N1"].TaskDef)
	}

	// Editing sets both fields and a second read sees them.
	edited := got["N1"].TaskDef
	edited.Risk, edited.RiskReason = model.RiskRisky, "edited by hand"
	if _, err := s.EditTask(ctx, "tester", e.ID, edited); err != nil {
		t.Fatalf("edit: %v", err)
	}
	one, err := s.GetTask(ctx, got["N1"].ID)
	if err != nil || one.Risk != model.RiskRisky || one.RiskReason != "edited by hand" {
		t.Fatalf("edited task = %+v (%v)", one.TaskDef, err)
	}
}

// Anything outside normal|risky is refused with a field error naming risk.
func TestTaskRiskRejectsUnknownValue(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "proj")
	e := newEffort(t, s, "proj")
	bad := qdef("T1")
	bad.Risk = "spicy"
	_, err := s.SaveQueue(ctx, "tester", e.ID, []model.TaskDef{bad})
	var fe *FieldError
	if !errors.Is(err, ErrInvalid) || !errors.As(err, &fe) || fe.Field != "risk" {
		t.Fatalf("SaveQueue = %v, want an invalid error naming risk", err)
	}
}

// Migration 56 adds the two columns to a database at af/af-next3's version and
// is a no-op the second time it is reached.
func TestMigration56TaskRisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentflow.db")
	s1, err := openWith(path, migrations[:55])
	if err != nil {
		t.Fatal(err)
	}
	if v := schemaVersion(t, s1.db); v != 55 {
		t.Fatalf("base database at version %d, want 55", v)
	}
	s1.Close()

	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apply 56 (pass %d): %v", i+1, err)
		}
		if v := schemaVersion(t, s.db); v != 56 {
			t.Fatalf("version after pass %d = %d, want 56", i+1, v)
		}
		var risk, reason string
		if err := s.db.QueryRow(`SELECT risk, risk_reason FROM tasks WHERE 0`).Scan(&risk, &reason); err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("read the new columns: %v", err)
		}
		s.Close()
	}
}
