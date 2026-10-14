package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// FRDPart is one part of a split FRD that waits to become an effort of its
// own. Part 1 of a split is the splitting effort's FRD, so stored parts start
// at 2. StartedEffort is the effort the part was started as, 0 until then.
type FRDPart struct {
	Effort        int64
	N             int
	Title         string
	Summary       string
	JSON          json.RawMessage
	Tokens        int
	StartedEffort int64
}

// AddFRDPartsTx stores the parts of a split inside the caller's transaction.
// Parts already stored are kept as they are, started or not: the new parts
// are appended after the highest stored number (their N is ignored, they are
// numbered in order), so a re-split of part 1 never loses a pending part or
// the record of one that was started.
func AddFRDPartsTx(ctx context.Context, tx *sql.Tx, actor string, effort int64, parts []FRDPart) error {
	if _, err := requireEffort(ctx, tx, effort); err != nil {
		return err
	}
	var top int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(n), 1) FROM frd_parts WHERE effort = ?`, effort).Scan(&top); err != nil {
		return err
	}
	for i, p := range parts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO frd_parts (effort, n, title, summary, json, tokens) VALUES (?, ?, ?, ?, ?, ?)`,
			effort, top+1+i, p.Title, p.Summary, string(p.JSON), p.Tokens); err != nil {
			return err
		}
	}
	_, err := AppendEvent(tx, actor, "frd.split", "effort", fmt.Sprint(effort), map[string]any{"parts": len(parts)})
	return err
}

// AddFRDParts is AddFRDPartsTx in a transaction of its own.
func (s *Store) AddFRDParts(ctx context.Context, actor string, effort int64, parts []FRDPart) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error { return AddFRDPartsTx(ctx, tx, actor, effort, parts) })
}

// ListFRDParts returns the effort's stored parts in order.
func (s *Store) ListFRDParts(ctx context.Context, effort int64) ([]FRDPart, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT effort, n, title, summary, json, tokens, started_effort FROM frd_parts WHERE effort = ? ORDER BY n`, effort)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FRDPart
	for rows.Next() {
		var p FRDPart
		var raw string
		if err := rows.Scan(&p.Effort, &p.N, &p.Title, &p.Summary, &raw, &p.Tokens, &p.StartedEffort); err != nil {
			return nil, err
		}
		p.JSON = json.RawMessage(raw)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetFRDPart returns one stored part or ErrNotFound.
func (s *Store) GetFRDPart(ctx context.Context, effort int64, n int) (FRDPart, error) {
	var p FRDPart
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT effort, n, title, summary, json, tokens, started_effort FROM frd_parts WHERE effort = ? AND n = ?`,
		effort, n).Scan(&p.Effort, &p.N, &p.Title, &p.Summary, &raw, &p.Tokens, &p.StartedEffort)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("effort %d has no part %d: %w", effort, n, ErrNotFound)
	}
	p.JSON = json.RawMessage(raw)
	return p, err
}

// MarkFRDPartStarted records the effort a part was started as.
func (s *Store) MarkFRDPartStarted(ctx context.Context, actor string, effort int64, n int, started int64) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE frd_parts SET started_effort = ? WHERE effort = ? AND n = ?`, started, effort, n)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return fmt.Errorf("effort %d has no part %d: %w", effort, n, ErrNotFound)
		}
		_, err = AppendEvent(tx, actor, "frd.part_start", "effort", fmt.Sprint(effort), map[string]any{"part": n, "started_effort": started})
		return err
	})
}
