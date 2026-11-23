package store

import (
	"context"
	"fmt"
	"time"
)

// DecisionInboxEntry is one pending ruling addition awaiting confirmation.
type DecisionInboxEntry struct {
	ID           int64
	Effort       int64
	Task         int64
	RulingID     string
	ProposedBy   string
	ProposedText string
	Status       string
	Created      time.Time
}

// InsertDecisionInbox adds a pending ruling-addition entry to the decision inbox.
// The coordinator/resolver calls this via the guarded add_ruling action; they
// never write the rulings file directly.
func (s *Store) InsertDecisionInbox(ctx context.Context, effort, task int64, rulingID, proposedBy, proposedText string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO decision_inbox (effort, task, ruling_id, proposed_by, proposed_text, status, created) VALUES (?, ?, ?, ?, ?, 'pending', ?)`, effort, task, rulingID, proposedBy, proposedText, time.Now().UnixMilli())
	return err
}

// ListPendingDecisionInbox returns every pending decision-inbox entry for an
// effort, for display on the dashboard / coordinator review.
func (s *Store) ListPendingDecisionInbox(ctx context.Context, effort int64) ([]DecisionInboxEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, effort, task, ruling_id, proposed_by, proposed_text, status, created FROM decision_inbox WHERE effort = ? AND status = 'pending' ORDER BY id`, effort)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DecisionInboxEntry
	for rows.Next() {
		var e DecisionInboxEntry
		var created int64
		if err := rows.Scan(&e.ID, &e.Effort, &e.Task, &e.RulingID, &e.ProposedBy, &e.ProposedText, &e.Status, &created); err != nil {
			return nil, err
		}
		e.Created = time.UnixMilli(created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ConfirmDecisionInbox marks a pending entry as confirmed (the ruling addition
// is approved and may be written to the rulings file).
func (s *Store) ConfirmDecisionInbox(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE decision_inbox SET status = 'confirmed' WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("decision_inbox %d: not found: %w", id, ErrNotFound)
	}
	return nil
}

// RejectDecisionInbox marks a pending entry as rejected (the proposed ruling
// addition is discarded).
func (s *Store) RejectDecisionInbox(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE decision_inbox SET status = 'rejected' WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("decision_inbox %d: not found: %w", id, ErrNotFound)
	}
	return nil
}
