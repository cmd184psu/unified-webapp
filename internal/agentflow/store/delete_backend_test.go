package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// Deleting a backend: only a disabled one that no candidate list names, its
// models and checks go with it, and the history that names it as plain text
// stays.
func TestDeleteBackend(t *testing.T) {
	s, _ := openTemp(t)
	for _, n := range []string{"gone", "other"} {
		if err := s.CreateBackend(ctx, "t", goodBackend(n)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteBackend(ctx, "t", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown = %v, want ErrNotFound", err)
	}
	err := s.DeleteBackend(ctx, "t", "gone")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "disable") {
		t.Fatalf("an enabled backend = %v, want ErrConflict that says to disable it", err)
	}

	// A global list and a project list both block it, and the reason names them.
	// (Lists are set while the backend is enabled; disabling it leaves them.)
	for _, c := range []struct{ scope, project, role string }{{"global", "", "worker"}, {"project", "p", "reviewer"}} {
		if err := s.SetRoleCandidates(ctx, "t", c.scope, c.project, c.role, []CandidateRef{{"gone", "free-model"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RetireBackend(ctx, "t", "gone"); err != nil {
		t.Fatal(err)
	}
	err = s.DeleteBackend(ctx, "t", "gone")
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "global worker") || !strings.Contains(err.Error(), "project p reviewer") {
		t.Fatalf("named by lists = %v, want ErrConflict naming both lists", err)
	}
	uses, err := s.BackendUses(ctx, "gone")
	if err != nil || len(uses) != 2 {
		t.Fatalf("BackendUses = %v, %v", uses, err)
	}

	// History that names the backend as text, and per-backend leftovers.
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO stages (effort, role, backend, model, state, started, finished) VALUES (1, 'worker', 'gone', 'free-model', 'done', 1, 2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO model_checks (backend, model, ok, checked) VALUES ('gone', 'free-model', 1, ?)`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}

	// Once the lists name another backend, it can go.
	for _, c := range []struct{ scope, project, role string }{{"global", "", "worker"}, {"project", "p", "reviewer"}} {
		if err := s.SetRoleCandidates(ctx, "t", c.scope, c.project, c.role, []CandidateRef{{"other", "free-model"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteBackend(ctx, "t", "gone"); err != nil {
		t.Fatalf("delete = %v", err)
	}
	if _, err := s.GetBackend(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: get = %v, want ErrNotFound", err)
	}
	var n int
	for _, q := range []string{
		`SELECT COUNT(*) FROM backend_models WHERE backend = 'gone'`,
		`SELECT COUNT(*) FROM model_checks WHERE backend = 'gone'`,
	} {
		if err := s.db.QueryRow(q).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s = %d, %v, want 0", q, n, err)
		}
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM stages WHERE backend = 'gone'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("stage history = %d, %v, want it kept", n, err)
	}
	if other, err := s.GetBackend(ctx, "other"); err != nil || len(other.Models) != 2 {
		t.Fatalf("the other backend was touched: %+v, %v", other, err)
	}
	evs, _ := s.ListEvents(ctx, "backend", "gone", 0)
	last := evs[len(evs)-1]
	if last.Action != "backend.delete" {
		t.Fatalf("last event = %q, want backend.delete", last.Action)
	}
	if err := s.DeleteBackend(ctx, "t", "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}
