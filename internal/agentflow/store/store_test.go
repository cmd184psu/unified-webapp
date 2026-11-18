package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agentflow.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func schemaVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestOpenPragmas(t *testing.T) {
	s, _ := openTemp(t)
	var mode string
	var fk, busy int
	s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode)
	s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	s.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busy)
	if mode != "wal" || fk != 1 || busy != 5000 {
		t.Fatalf("pragmas: journal=%s fk=%d busy=%d", mode, fk, busy)
	}
	if n := s.db.Stats().MaxOpenConnections; n != 1 {
		t.Fatalf("MaxOpenConnections = %d, want 1", n)
	}
}

func TestMigrateTwiceIsIdempotent(t *testing.T) {
	s, path := openTemp(t)
	want := migrations[len(migrations)-1].version
	if got := schemaVersion(t, s.db); got != want {
		t.Fatalf("version = %d, want %d", got, want)
	}
	if err := migrate(s.db, migrations); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	s.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	var rows int
	s2.db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&rows)
	if rows != len(migrations) || schemaVersion(t, s2.db) != want {
		t.Fatalf("schema_version rows = %d, version = %d", rows, schemaVersion(t, s2.db))
	}
}

func TestFailingMigrationRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentflow.db")
	good := []migration{{1, `CREATE TABLE a (x INTEGER);`}}
	s, err := openWith(path, good)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	// Version 2 creates a table, then fails on bad SQL: the whole step,
	// including the table and the version bump, must roll back.
	bad := append(append([]migration{}, good...), migration{2, `CREATE TABLE b (x INTEGER); THIS IS NOT SQL;`})
	if _, err := openWith(path, bad); err == nil {
		t.Fatal("expected failure from bad migration")
	}
	s, err = openWith(path, good)
	if err != nil {
		t.Fatalf("reopen after failed migration: %v", err)
	}
	defer s.Close()
	if v := schemaVersion(t, s.db); v != 1 {
		t.Fatalf("version = %d, want 1", v)
	}
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'b'`).Scan(&n)
	if n != 0 {
		t.Fatal("table b survived the rolled-back migration")
	}
}

func TestSchemaTooNewRefused(t *testing.T) {
	s, path := openTemp(t)
	if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (999)`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := Open(path); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open = %v, want ErrSchemaTooNew", err)
	}
}

func TestHTTPStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, http.StatusOK},
		{"not found", ErrNotFound, http.StatusNotFound},
		{"wrapped not found", fmt.Errorf("x: %w", ErrNotFound), http.StatusNotFound},
		{"conflict", ErrConflict, http.StatusConflict},
		{"double wrapped conflict", fmt.Errorf("a: %w", fmt.Errorf("b: %w", ErrConflict)), http.StatusConflict},
		{"invalid", ErrInvalid, http.StatusBadRequest},
		{"field error", Invalid("max_attempts", "out of range"), http.StatusBadRequest},
		{"wrapped field error", fmt.Errorf("save: %w", Invalid("slug", "taken")), http.StatusBadRequest},
		{"unknown", errors.New("boom"), http.StatusInternalServerError},
		{"schema too new", ErrSchemaTooNew, http.StatusInternalServerError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := HTTPStatus(c.err); got != c.want {
				t.Fatalf("HTTPStatus = %d, want %d", got, c.want)
			}
		})
	}
}

func TestFieldErrorNamesFieldAndMatchesInvalid(t *testing.T) {
	err := fmt.Errorf("save: %w", Invalid("slots", "must be >= 1"))
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "slots" {
		t.Fatalf("errors.As FieldError = %v", fe)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("FieldError must match ErrInvalid")
	}
}

func TestWithTxCommitRollbackPanic(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	if err := s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := AppendEvent(tx, "alice", "commit.me", "project", "p1", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := AppendEvent(tx, "alice", "rollback.me", "project", "p1", nil); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx = %v, want boom", err)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed")
			}
		}()
		s.WithTx(ctx, func(tx *sql.Tx) error {
			AppendEvent(tx, "alice", "panic.me", "project", "p1", nil)
			panic("kaboom")
		})
	}()

	evs, err := s.ListEvents(ctx, "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Action != "commit.me" {
		t.Fatalf("events = %+v, want only commit.me", evs)
	}
	// The single connection must be free again after rollback and panic.
	if err := s.WithTx(ctx, func(tx *sql.Tx) error { return nil }); err != nil {
		t.Fatalf("connection leaked: %v", err)
	}
}

func TestAppendEvent(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := AppendEvent(tx, "apikey:ci", "effort.approve", "effort", "e1", map[string]any{"revision": 2}); err != nil {
			return err
		}
		_, err := AppendEvent(tx, "ldap:bob", "project.create", "project", "p1", nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := s.ListEvents(ctx, "effort", "e1", 0)
	if err != nil || len(evs) != 1 {
		t.Fatalf("ListEvents = %v, %v", evs, err)
	}
	e := evs[0]
	if e.Actor != "apikey:ci" || e.Action != "effort.approve" || e.SubjectID != "e1" || e.DetailJSON != `{"revision":2}` || e.TS.IsZero() {
		t.Fatalf("event = %+v", e)
	}
	all, _ := s.ListEvents(ctx, "", "", 0)
	if len(all) != 2 || all[1].DetailJSON != "{}" {
		t.Fatalf("all = %+v", all)
	}
}

func TestAppendEventValidation(t *testing.T) {
	s, _ := openTemp(t)
	cases := []struct {
		field                      string
		actor, action, subjectType string
		detail                     any
	}{
		{"actor", "", "a", "t", nil},
		{"action", "u", "", "t", nil},
		{"subject_type", "u", "a", "", nil},
		{"detail", "u", "a", "t", make(chan int)},
	}
	for _, c := range cases {
		err := s.WithTx(context.Background(), func(tx *sql.Tx) error {
			_, err := AppendEvent(tx, c.actor, c.action, c.subjectType, "id", c.detail)
			return err
		})
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != c.field || !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v", c.field, err)
		}
	}
}
