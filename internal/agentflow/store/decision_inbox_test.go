package store

import (
	"errors"
	"testing"
)

func decisionStatus(t *testing.T, s *Store, id int64) string {
	t.Helper()
	var status string
	if err := s.db.QueryRow(`SELECT status FROM decision_inbox WHERE id = ?`, id).Scan(&status); err != nil {
		t.Fatalf("status of %d: %v", id, err)
	}
	return status
}

// Confirm and reject must change a pending row. An UPDATE scanned with
// QueryRowContext never returns a row, so both used to report ErrNotFound
// for an entry that was just inserted.
func TestDecisionInboxConfirmAndReject(t *testing.T) {
	s, e, ids := queueFixture(t)
	if err := s.InsertDecisionInbox(ctx, e.ID, ids["A"], "R-1", "alice", "keep the first"); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertDecisionInbox(ctx, e.ID, ids["B"], "R-2", "bob", "drop the second"); err != nil {
		t.Fatal(err)
	}
	pending, err := s.ListPendingDecisionInbox(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want 2", len(pending))
	}
	confirmID, rejectID := pending[0].ID, pending[1].ID

	if err := s.ConfirmDecisionInbox(ctx, confirmID); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if err := s.RejectDecisionInbox(ctx, rejectID); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if got := decisionStatus(t, s, confirmID); got != "confirmed" {
		t.Fatalf("confirmed status = %q", got)
	}
	if got := decisionStatus(t, s, rejectID); got != "rejected" {
		t.Fatalf("rejected status = %q", got)
	}
	left, err := s.ListPendingDecisionInbox(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("still pending: %+v", left)
	}

	if err := s.ConfirmDecisionInbox(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("confirm missing = %v, want ErrNotFound", err)
	}
	if err := s.RejectDecisionInbox(ctx, 999998); !errors.Is(err, ErrNotFound) {
		t.Fatalf("reject missing = %v, want ErrNotFound", err)
	}

	// The row is still there, so a second confirm matches it and leaves the
	// status confirmed. It is not a second "not found".
	if err := s.ConfirmDecisionInbox(ctx, confirmID); err != nil {
		t.Fatalf("second confirm = %v, want idempotent success", err)
	}
	if got := decisionStatus(t, s, confirmID); got != "confirmed" {
		t.Fatalf("status after second confirm = %q", got)
	}
}
