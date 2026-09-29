package db

import "testing"

// TestDSNFor_SetsWALAndForeignKeys pins the DSN shape dsnFor builds: a real
// file gets WAL mode and modernc sqlite's per-connection foreign-key pragma
// (fact 1 in the plan's §3 — the one-off PRAGMA
// exec doesn't survive a driver-forced reconnect, but this DSN param does).
// A behavioural FK test would already pass today via that one-off PRAGMA,
// so it could not by itself detect a regression in this DSN string.
func TestDSNFor_SetsWALAndForeignKeys(t *testing.T) {
	if got, want := dsnFor("/x/t.db"), "/x/t.db?_journal_mode=WAL&_foreign_keys=1"; got != want {
		t.Errorf("dsnFor(%q) = %q, want %q", "/x/t.db", got, want)
	}
	if got, want := dsnFor(":memory:"), ":memory:"; got != want {
		t.Errorf("dsnFor(%q) = %q, want %q", ":memory:", got, want)
	}
}
