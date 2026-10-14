package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

func nowMS() int64 { return time.Now().UnixMilli() }

const effortCols = `id, project, title, status, run_state, integration_branch, base_commit, yolo, deliberate, created, updated`

func scanEffort(r interface{ Scan(...any) error }) (model.Effort, error) {
	var e model.Effort
	var yolo, delib int
	err := r.Scan(&e.ID, &e.Project, &e.Title, &e.Status, &e.RunState, &e.IntegrationBranch, &e.BaseCommit, &yolo, &delib, &e.Created, &e.Updated)
	e.YOLO, e.Deliberate = yolo == 1, delib == 1
	return e, err
}

func requireEffort(ctx context.Context, q querier, id int64) (model.Effort, error) {
	e, err := scanEffort(q.QueryRowContext(ctx, `SELECT `+effortCols+` FROM efforts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, fmt.Errorf("effort %d: %w", id, ErrNotFound)
	}
	return e, err
}

// activeEffort returns the project's active effort id, or 0 (PJ4).
func activeEffort(ctx context.Context, q querier, project string, except int64) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM efforts WHERE project = ? AND id <> ?
		AND status NOT IN ('complete', 'abandoned')`, project, except).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// CreateEffort starts a draft effort on a project. A project may have at most
// one active effort (PJ4); a second is ErrConflict.
func (s *Store) CreateEffort(ctx context.Context, actor, project, title string, deliberate bool) (model.Effort, error) {
	if strings.TrimSpace(title) == "" {
		return model.Effort{}, Invalid("title", "required")
	}
	var out model.Effort
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireProject(ctx, tx, project); err != nil {
			return err
		}
		if id, err := activeEffort(ctx, tx, project, 0); err != nil {
			return err
		} else if id != 0 {
			return fmt.Errorf("project %q already has active effort %d: %w", project, id, ErrConflict)
		}
		now := nowMS()
		res, err := tx.ExecContext(ctx, `INSERT INTO efforts (project, title, status, deliberate, created, updated)
			VALUES (?, ?, 'draft', ?, ?, ?)`, project, title, b2i(deliberate), now, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := AppendEvent(tx, actor, "effort.create", "effort", fmt.Sprint(id), map[string]any{"project": project}); err != nil {
			return err
		}
		out, err = requireEffort(ctx, tx, id)
		return err
	})
	return out, err
}

// GetEffort returns one effort or ErrNotFound.
func (s *Store) GetEffort(ctx context.Context, id int64) (model.Effort, error) {
	return requireEffort(ctx, s.db, id)
}

// ActiveEffort returns the project's active effort or ErrNotFound.
func (s *Store) ActiveEffort(ctx context.Context, project string) (model.Effort, error) {
	id, err := activeEffort(ctx, s.db, project, 0)
	if err != nil {
		return model.Effort{}, err
	}
	if id == 0 {
		return model.Effort{}, fmt.Errorf("project %q has no active effort: %w", project, ErrNotFound)
	}
	return requireEffort(ctx, s.db, id)
}

// ListEfforts returns a project's efforts, newest first.
func (s *Store) ListEfforts(ctx context.Context, project string) ([]model.Effort, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+effortCols+` FROM efforts WHERE project = ? ORDER BY id DESC`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Effort
	for rows.Next() {
		e, err := scanEffort(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EffortPatch is a partial effort update; nil fields are left alone.
type EffortPatch struct {
	Status            *string
	RunState          *string
	IntegrationBranch *string
	BaseCommit        *string
	YOLO              *bool
	Deliberate        *bool
}

// UpdateEffort applies patch. Status and run state must be known values, and a
// finished effort (complete or abandoned) cannot change status again.
func (s *Store) UpdateEffort(ctx context.Context, actor string, id int64, p EffortPatch) (model.Effort, error) {
	if p.Status != nil && !slices.Contains(model.EffortStatuses, *p.Status) {
		return model.Effort{}, Invalid("status", "unknown effort status")
	}
	if p.RunState != nil && !slices.Contains(model.RunStates, *p.RunState) {
		return model.Effort{}, Invalid("run_state", "unknown run state")
	}
	var out model.Effort
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		e, err := requireEffort(ctx, tx, id)
		if err != nil {
			return err
		}
		if p.Status != nil && *p.Status != e.Status {
			if !model.IsActiveEffortStatus(e.Status) {
				return fmt.Errorf("effort %d is %s: %w", id, e.Status, ErrConflict)
			}
			e.Status = *p.Status
		}
		if p.RunState != nil {
			e.RunState = *p.RunState
		}
		if p.IntegrationBranch != nil {
			e.IntegrationBranch = *p.IntegrationBranch
		}
		if p.BaseCommit != nil {
			e.BaseCommit = *p.BaseCommit
		}
		if p.YOLO != nil {
			e.YOLO = *p.YOLO
		}
		if p.Deliberate != nil {
			e.Deliberate = *p.Deliberate
		}
		if _, err := tx.ExecContext(ctx, `UPDATE efforts SET status = ?, run_state = ?, integration_branch = ?,
			base_commit = ?, yolo = ?, deliberate = ?, updated = ? WHERE id = ?`,
			e.Status, e.RunState, e.IntegrationBranch, e.BaseCommit, b2i(e.YOLO), b2i(e.Deliberate), nowMS(), id); err != nil {
			return err
		}
		if _, err := AppendEvent(tx, actor, "effort.update", "effort", fmt.Sprint(id), map[string]any{"status": e.Status, "run_state": e.RunState, "yolo": e.YOLO}); err != nil {
			return err
		}
		out, err = requireEffort(ctx, tx, id)
		return err
	})
	return out, err
}

// UpdateEffortStatusIf sets the effort's status to next only while it is
// still expected, so a writer holding a stale read cannot overwrite a newer
// transition. It reports whether the write happened.
func (s *Store) UpdateEffortStatusIf(ctx context.Context, actor string, id int64, expected, next string) (bool, error) {
	if !slices.Contains(model.EffortStatuses, next) {
		return false, Invalid("status", "unknown effort status")
	}
	changed := false
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE efforts SET status = ?, updated = ? WHERE id = ? AND status = ?`, next, nowMS(), id, expected)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil
		}
		changed = true
		e, err := requireEffort(ctx, tx, id)
		if err != nil {
			return err
		}
		_, err = AppendEvent(tx, actor, "effort.update", "effort", fmt.Sprint(id), map[string]any{"status": e.Status, "run_state": e.RunState, "yolo": e.YOLO})
		return err
	})
	return changed, err
}
