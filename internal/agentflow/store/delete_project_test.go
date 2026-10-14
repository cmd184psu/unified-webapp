package store

import (
	"errors"
	"fmt"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// seedJourney gives a project a completed effort with a row in every table
// that hangs off an effort, plus limits, plugins and candidates.
func seedJourney(t *testing.T, s *Store, slug string) {
	t.Helper()
	newProject(t, s, slug)
	e := newEffort(t, s, slug)
	if _, err := s.SaveQueue(ctx, "tester", e.ID, []model.TaskDef{qdef("A"), qdef("B", "A")}); err != nil {
		t.Fatal(err)
	}
	var a, b int64
	must := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	one := func(q string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := s.db.QueryRow(q, args...).Scan(&id); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return id
	}
	a = one(`SELECT t.id FROM tasks t JOIN queues q ON q.id = t.queue WHERE q.effort = ? AND t.task_key = 'A'`, e.ID)
	b = one(`SELECT t.id FROM tasks t JOIN queues q ON q.id = t.queue WHERE q.effort = ? AND t.task_key = 'B'`, e.ID)
	must(`INSERT INTO documents (effort, kind, revision, json, created_by, created) VALUES (?, 'frd', 1, '{}', 'x', 1)`, e.ID)
	must(`INSERT INTO approvals (effort, kind, revision, approved_by, at) VALUES (?, 'frd', 1, 'x', 1)`, e.ID)
	must(`INSERT INTO attempts (task, n, kind, started) VALUES (?, 1, 'normal', 1)`, a)
	att := one(`SELECT id FROM attempts WHERE task = ?`, a)
	must(`INSERT INTO stages (attempt, role, state) VALUES (?, 'worker', 'done')`, att)
	must(`INSERT INTO stages (effort, role, state) VALUES (?, 'planner', 'done')`, e.ID)
	st := one(`SELECT id FROM stages WHERE attempt = ?`, att)
	must(`INSERT INTO infra_retries (task, stage, reason, at) VALUES (?, ?, 'r', 1)`, a, st)
	must(`INSERT INTO overrides (task, actor, reason, at) VALUES (?, 'x', 'r', 1)`, b)
	must(`INSERT INTO task_resets (task, after_attempt_id, after_infra_id, actor, at) VALUES (?, 0, 0, 'x', 1)`, a)
	must(`INSERT INTO progress_log (task, attempt, entry, at) VALUES (?, ?, 'e', 1)`, a, att)
	must(`INSERT INTO baselines (effort, commit_sha, report_json, at) VALUES (?, 'c', '{}', 1)`, e.ID)
	must(`INSERT INTO frd_parts (effort, n, title, json) VALUES (?, 1, 't', '{}')`, e.ID)
	must(`INSERT INTO project_limits (project, key, value) VALUES (?, 'max_attempts', '3')`, slug)
	must(`INSERT INTO project_plugins (project, name, version) VALUES (?, 'p', '1.0.0')`, slug)
	_ = s.CreateBackend(ctx, "t", goodBackend("bk")) // second call is a harmless duplicate
	if err := s.SetRoleCandidates(ctx, "t", "project", slug, "worker", []CandidateRef{{Backend: "bk", Model: "free-model"}}); err != nil {
		t.Fatal(err)
	}
	must(`UPDATE efforts SET status = 'complete' WHERE id = ?`, e.ID)
}

func tableCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// referencing lists every table with a foreign key into the effort tree or the
// projects table, so a table added later without cleanup fails this test.
func referencing(t *testing.T, s *Store) []string {
	t.Helper()
	parents := map[string]bool{"projects": true, "efforts": true, "documents": true, "queues": true, "tasks": true, "attempts": true, "stages": true}
	rows, err := s.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	var out []string
	for _, n := range names {
		fk, err := s.db.Query(fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, n))
		if err != nil {
			t.Fatal(err)
		}
		hit := false
		for fk.Next() {
			var id, seq int
			var table, from, to, onu, ond, match string
			if err := fk.Scan(&id, &seq, &table, &from, &to, &onu, &ond, &match); err != nil {
				t.Fatal(err)
			}
			hit = hit || parents[table]
		}
		fk.Close()
		if hit {
			out = append(out, n)
		}
	}
	return out
}

func TestDeleteProject_RemovesEveryRowOfThatProjectOnly(t *testing.T) {
	s, _ := openTemp(t)
	seedJourney(t, s, "gone")
	seedJourney(t, s, "kept")
	tables := append(referencing(t, s), "projects", "role_candidates")
	if len(tables) < 17 {
		t.Fatalf("only %d referencing tables found: %v", len(tables), tables)
	}
	before := map[string]int{}
	for _, tb := range tables {
		before[tb] = tableCount(t, s, tb)
		if before[tb] == 0 || before[tb]%2 != 0 {
			t.Fatalf("table %s has %d seeded rows, want a positive even count (seed both projects)", tb, before[tb])
		}
	}
	if err := s.DeleteProject(ctx, "tester", "gone"); err != nil {
		t.Fatal(err)
	}
	for _, tb := range tables {
		if got := tableCount(t, s, tb); got != before[tb]/2 {
			t.Errorf("table %s: %d rows after delete, want %d (only the other project's)", tb, got, before[tb]/2)
		}
	}
	if _, err := s.GetProject(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetProject after delete = %v, want ErrNotFound", err)
	}
	if n := tableCount(t, s, "events"); n == 0 {
		t.Errorf("events were wiped")
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE action = 'project.delete' AND subject_id = 'gone'`).Scan(&n)
	if n != 1 {
		t.Errorf("project.delete events = %d, want 1", n)
	}
	if rows, err := s.db.Query(`PRAGMA foreign_key_check`); err != nil || rows.Next() {
		t.Errorf("foreign_key_check reports dangling rows (%v)", err)
	}
	// The slug can be registered again with a clean slate.
	newProject(t, s, "gone")
	if _, err := s.ActiveEffort(ctx, "gone"); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-registered project has an effort: %v", err)
	}
}

func TestDeleteProject_RefusesAnActiveEffort(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "busy")
	e := newEffort(t, s, "busy")
	err := s.DeleteProject(ctx, "tester", "busy")
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("DeleteProject with an active effort = %v, want ErrConflict", err)
	}
	if _, err := s.GetEffort(ctx, e.ID); err != nil {
		t.Errorf("effort lost after refused delete: %v", err)
	}
}
