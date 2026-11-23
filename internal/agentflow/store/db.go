// Package store is agentflow's SQLite persistence layer: one database file
// (<data_dir>/agentflow.db), numbered transactional migrations, typed errors
// and an audit events table.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Store owns the agentflow database handle.
type Store struct {
	db *sql.DB
	// dataDir is the directory holding the database file ("" for :memory:);
	// project repositories may not overlap it.
	dataDir string
	// protected is captured once at Open; see captureProtected.
	protected []protectedPath
}

// dsnFor builds the sqlite DSN for path: WAL, foreign keys on and a 5s busy
// timeout, set per connection so a driver-forced reconnect keeps them.
func dsnFor(path string) string {
	if path == ":memory:" {
		return path
	}
	return path + "?_journal_mode=WAL&_foreign_keys=1&_busy_timeout=5000"
}

// Open opens (creating if needed) the database at path and brings its schema
// up to date. It returns ErrSchemaTooNew, and leaves the file untouched, when
// the database is newer than this build.
func Open(path string) (*Store, error) {
	return openWith(path, migrations)
}

func openWith(path string, migs []migration) (*Store, error) {
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One connection: a transaction holds it, so code inside WithTx must use
	// only the *sql.Tx it is handed.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := migrate(db, migs); err != nil {
		db.Close()
		return nil, err
	}
	st := &Store{db: db}
	if path != ":memory:" {
		st.dataDir = filepath.Dir(path)
	}
	if st.protected, err = captureProtected(st.dataDir); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for read queries by sibling store files and tests.
// Multi-statement writes must go through WithTx; the engine's single-statement
// writes (stage and attempt bookkeeping) use it directly, where one statement
// is atomic on its own.
func (s *Store) DB() *sql.DB { return s.db }

// WithTx runs fn in a transaction, committing if it returns nil and rolling
// back on error or panic. fn must use only tx, never s.DB(): the pool has a
// single connection, so a second request for it would block forever.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// DataDir returns the directory holding the database file.
func (s *Store) DataDir() string { return s.dataDir }
