package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ServiceHold is a hold on the whole service (AF-10): while it is set nothing
// new is dispatched in any project, a run/start is refused, and the running
// stages finish. It is stored, so it survives a restart, and it keeps the
// projects it paused so lifting it starts them again.
type ServiceHold struct {
	// Active is false when there is no hold; the other fields are then zero.
	Active bool   `json:"active"`
	Reason string `json:"reason,omitempty"`
	// Since is when the hold was placed (Unix ms), kept when the reason changes.
	Since int64 `json:"since,omitempty"`
	// Paused are the projects the hold paused, started again when it is lifted.
	Paused []string `json:"paused,omitempty"`
	// Restart is set when the hold came from a drain restart that ran out of
	// time: the last open stage closing finishes that restart.
	Restart bool `json:"restart,omitempty"`
}

func readHold(ctx context.Context, q querier) (ServiceHold, error) {
	var h ServiceHold
	var paused string
	var restart int
	err := q.QueryRowContext(ctx, `SELECT reason, since, paused, restart FROM service_hold WHERE id = 1`).
		Scan(&h.Reason, &h.Since, &paused, &restart)
	if errors.Is(err, sql.ErrNoRows) {
		return ServiceHold{}, nil
	}
	if err != nil {
		return ServiceHold{}, err
	}
	h.Active, h.Restart = true, restart == 1
	if paused != "" {
		if err := json.Unmarshal([]byte(paused), &h.Paused); err != nil {
			return ServiceHold{}, err
		}
	}
	return h, nil
}

// ServiceHold reads the hold; a service that is not held reports Active false.
func (s *Store) ServiceHold(ctx context.Context) (ServiceHold, error) {
	return readHold(ctx, s.db)
}

// HoldService places the hold with the projects it paused. Placing a hold that
// is already there keeps the projects it paused and its start time and updates
// the reason; restart stays set once it was. Both are audited.
func (s *Store) HoldService(ctx context.Context, actor, reason string, paused []string, restart bool) (ServiceHold, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ServiceHold{}, Invalid("reason", "required")
	}
	var out ServiceHold
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cur, err := readHold(ctx, tx)
		if err != nil {
			return err
		}
		h := ServiceHold{Active: true, Reason: reason, Since: time.Now().UnixMilli(), Paused: paused, Restart: restart}
		if cur.Active {
			h.Since, h.Paused, h.Restart = cur.Since, cur.Paused, cur.Restart || restart
		}
		if h.Paused == nil {
			h.Paused = []string{}
		}
		b, err := json.Marshal(h.Paused)
		if err != nil {
			return err
		}
		restartInt := 0
		if h.Restart {
			restartInt = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO service_hold (id, reason, since, paused, restart) VALUES (1, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET reason = excluded.reason, since = excluded.since, paused = excluded.paused, restart = excluded.restart`,
			h.Reason, h.Since, string(b), restartInt); err != nil {
			return err
		}
		if _, err := AppendEvent(tx, actor, "service.hold", "service", "hold",
			map[string]any{"reason": h.Reason, "paused": h.Paused, "restart": h.Restart}); err != nil {
			return err
		}
		out = h
		return nil
	})
	return out, err
}

// LiftServiceHold removes the hold and returns what it was, so the caller can
// start again the projects it paused. Lifting a service that is not held is
// not an error. Both are audited.
func (s *Store) LiftServiceHold(ctx context.Context, actor string) (ServiceHold, error) {
	var out ServiceHold
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cur, err := readHold(ctx, tx)
		if err != nil {
			return err
		}
		out = cur
		if _, err := tx.ExecContext(ctx, `DELETE FROM service_hold WHERE id = 1`); err != nil {
			return err
		}
		if !cur.Active {
			return nil
		}
		_, err = AppendEvent(tx, actor, "service.unhold", "service", "hold",
			map[string]any{"reason": cur.Reason, "paused": cur.Paused})
		return err
	})
	return out, err
}

// SetServiceHoldRestart flips the restart flag of an existing hold: cleared
// when the process is back and the restart it was waiting for already happened.
func (s *Store) SetServiceHoldRestart(ctx context.Context, restart bool) error {
	restartInt := 0
	if restart {
		restartInt = 1
	}
	_, err := s.db.ExecContext(ctx, `UPDATE service_hold SET restart = ? WHERE id = 1`, restartInt)
	return err
}
