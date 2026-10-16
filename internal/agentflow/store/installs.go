package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Install states.
const (
	InstallRunning   = "running"
	InstallDone      = "done"
	InstallFailed    = "failed"
	InstallCancelled = "cancelled"
)

// Install is one pull of a model onto a backend's host (H6). Completed and
// Total are bytes of the layer being downloaded; Status is the host's own
// words for the step ("pulling 3eb75691d2db", "verifying sha256 digest").
type Install struct {
	ID        int64  `json:"id"`
	Backend   string `json:"backend"`
	Model     string `json:"model"`
	Tier      int    `json:"tier"`
	State     string `json:"state"`
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	Error     string `json:"error,omitempty"`
	Started   int64  `json:"started"`
	Finished  int64  `json:"finished,omitempty"`
}

const installCols = `id, backend, model, tier, state, status, completed, total, error, started, finished`

func scanInstall(r interface{ Scan(...any) error }) (Install, error) {
	var i Install
	err := r.Scan(&i.ID, &i.Backend, &i.Model, &i.Tier, &i.State, &i.Status, &i.Completed, &i.Total, &i.Error, &i.Started, &i.Finished)
	return i, err
}

// CreateInstall opens an install for a model on an Ollama backend. A model
// already being installed on that backend is ErrConflict; a backend that is
// not Ollama, or is retired, is invalid.
func (s *Store) CreateInstall(ctx context.Context, actor, backend, mdl string, tier int) (Install, error) {
	mdl = strings.TrimSpace(mdl)
	if mdl == "" {
		return Install{}, Invalid("model", "required")
	}
	if tier < 0 || tier > 3 {
		return Install{}, Invalid("tier", "must be 0 to 3")
	}
	var out Install
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var apiType string
		var retired int
		err := tx.QueryRowContext(ctx, `SELECT api_type, retired FROM backends WHERE name = ?`, backend).Scan(&apiType, &retired)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("backend %q: %w", backend, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if apiType != "ollama" {
			return Invalid("backend", "models can be installed only on an ollama backend")
		}
		if retired != 0 {
			return Invalid("backend", "backend is retired")
		}
		var running int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_installs WHERE backend = ? AND model = ? AND state = 'running'`, backend, mdl).Scan(&running); err != nil {
			return err
		}
		if running > 0 {
			return fmt.Errorf("%s is already being installed on %s: %w", mdl, backend, ErrConflict)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO model_installs (backend, model, tier, state, started) VALUES (?, ?, ?, 'running', ?)`, backend, mdl, tier, nowMS())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if out, err = scanInstall(tx.QueryRowContext(ctx, `SELECT `+installCols+` FROM model_installs WHERE id = ?`, id)); err != nil {
			return err
		}
		_, err = AppendEvent(tx, actor, "install.start", "backend", backend, map[string]any{"model": mdl, "tier": tier})
		return err
	})
	return out, err
}

// SetInstallProgress records how far a running install has got.
func (s *Store) SetInstallProgress(ctx context.Context, id int64, status string, completed, total int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE model_installs SET status = ?, completed = ?, total = ? WHERE id = ? AND state = 'running'`, status, completed, total, id)
	return err
}

// FinishInstall ends a running install as done, failed or cancelled. Ending
// one that is already over changes nothing.
func (s *Store) FinishInstall(ctx context.Context, id int64, state, errMsg string) error {
	if state != InstallDone && state != InstallFailed && state != InstallCancelled {
		return Invalid("state", "must be done, failed or cancelled")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE model_installs SET state = ?, error = ?, finished = ? WHERE id = ? AND state = 'running'`, state, errMsg, nowMS(), id)
	return err
}

// GetInstall returns one install or ErrNotFound.
func (s *Store) GetInstall(ctx context.Context, id int64) (Install, error) {
	i, err := scanInstall(s.db.QueryRowContext(ctx, `SELECT `+installCols+` FROM model_installs WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return i, fmt.Errorf("install %d: %w", id, ErrNotFound)
	}
	return i, err
}

// ListInstalls lists the newest installs first, for one backend or (empty
// name) all of them.
func (s *Store) ListInstalls(ctx context.Context, backend string, limit int) ([]Install, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+installCols+` FROM model_installs WHERE (? = '' OR backend = ?) ORDER BY id DESC LIMIT ?`, backend, backend, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Install{}
	for rows.Next() {
		i, err := scanInstall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// FailRunningInstalls ends every install still marked running, for the start
// of the service: the goroutine that was pulling died with the old process.
// Pulling again resumes where Ollama left off.
func (s *Store) FailRunningInstalls(ctx context.Context, reason string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE model_installs SET state = 'failed', error = ?, finished = ? WHERE state = 'running'`, reason, nowMS())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
