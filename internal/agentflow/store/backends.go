package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// BackendModel is one model a backend serves. Paid marks a model that costs
// money even on an otherwise free backend (MB5).
type BackendModel struct {
	Model string
	Paid  bool
}

// Backend is a model server in agentflow's own terms (AR3): where it is, which
// API it speaks and how many concurrent jobs it takes. Hosted backends are
// not operator-run; Paid backends cost money. Retired backends keep their
// row (history refers to them) but are never used.
type Backend struct {
	Name    string
	Host    string
	APIType string
	Hosted  bool
	Paid    bool
	Slots   int
	Retired bool
	Models  []BackendModel
	Created time.Time
	Updated time.Time
}

func validateBackend(b Backend) error {
	if err := validateBackendName("name", b.Name); err != nil {
		return err
	}
	if err := validateHostURL("host", b.Host); err != nil {
		return err
	}
	if !apiTypes[b.APIType] {
		return Invalid("api_type", "must be ollama, openai_compatible or anthropic")
	}
	if b.Slots < 1 {
		return Invalid("slots", "must be at least 1")
	}
	seen := map[string]bool{}
	for _, m := range b.Models {
		if m.Model == "" || m.Model != strings.TrimSpace(m.Model) {
			return Invalid("models", "model name required, without leading or trailing whitespace")
		}
		if seen[m.Model] {
			return Invalid("models", fmt.Sprintf("duplicate model %q", m.Model))
		}
		seen[m.Model] = true
	}
	return nil
}

// backendDetail is the audit detail for a backend write: the validated fields.
func backendDetail(b Backend) map[string]any {
	return map[string]any{
		"host": b.Host, "api_type": b.APIType, "hosted": b.Hosted, "paid": b.Paid,
		"slots": b.Slots, "models": b.Models,
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func insertModels(ctx context.Context, tx *sql.Tx, b Backend) error {
	for _, m := range b.Models {
		if _, err := tx.ExecContext(ctx, `INSERT INTO backend_models (backend, model, paid) VALUES (?, ?, ?)`,
			b.Name, m.Model, b2i(m.Paid)); err != nil {
			return err
		}
	}
	return nil
}

// CreateBackend validates and stores a new backend. A name already in use
// (including a retired one) returns ErrConflict.
func (s *Store) CreateBackend(ctx context.Context, actor string, b Backend) error {
	if err := validateBackend(b); err != nil {
		return err
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM backends WHERE name = ?`, b.Name).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("backend %q: %w", b.Name, ErrConflict)
		}
		now := time.Now().UnixMilli()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO backends (name, host, api_type, hosted, paid, slots, retired, created, updated)
			VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			b.Name, b.Host, b.APIType, b2i(b.Hosted), b2i(b.Paid), b.Slots, now, now); err != nil {
			return err
		}
		if err := insertModels(ctx, tx, b); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "backend.create", "backend", b.Name, backendDetail(b))
		return err
	})
}

// UpdateBackend replaces a backend's host, API type, flags, slots and model
// list (name and retired state are unchanged). It returns ErrNotFound for an
// unknown name and ErrConflict when it would drop a model that a role
// candidate list still uses.
func (s *Store) UpdateBackend(ctx context.Context, actor string, b Backend) error {
	if err := validateBackend(b); err != nil {
		return err
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE backends SET host = ?, api_type = ?, hosted = ?, paid = ?, slots = ?, updated = ?
			WHERE name = ?`,
			b.Host, b.APIType, b2i(b.Hosted), b2i(b.Paid), b.Slots, time.Now().UnixMilli(), b.Name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("backend %q: %w", b.Name, ErrNotFound)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM backend_models WHERE backend = ?`, b.Name); err != nil {
			return err
		}
		if err := insertModels(ctx, tx, b); err != nil {
			return err
		}
		var orphan string
		err = tx.QueryRowContext(ctx, `
			SELECT c.model FROM role_candidates c
			WHERE c.backend = ? AND NOT EXISTS
			  (SELECT 1 FROM backend_models m WHERE m.backend = c.backend AND m.model = c.model)
			LIMIT 1`, b.Name).Scan(&orphan)
		if err == nil {
			return fmt.Errorf("model %q is still used by a role candidate list: %w", orphan, ErrConflict)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = AppendEvent(tx, actor, "backend.update", "backend", b.Name, backendDetail(b))
		return err
	})
}

// RetireBackend marks a backend unusable. Retiring twice is a no-op.
func (s *Store) RetireBackend(ctx context.Context, actor, name string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE backends SET retired = 1, updated = ? WHERE name = ?`,
			time.Now().UnixMilli(), name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("backend %q: %w", name, ErrNotFound)
		}
		_, err = AppendEvent(tx, actor, "backend.retire", "backend", name, map[string]any{"retired": true})
		return err
	})
}

// EnableBackend reverses RetireBackend (BK3). Enabling an active backend is a
// no-op.
func (s *Store) EnableBackend(ctx context.Context, actor, name string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE backends SET retired = 0, updated = ? WHERE name = ?`,
			time.Now().UnixMilli(), name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("backend %q: %w", name, ErrNotFound)
		}
		_, err = AppendEvent(tx, actor, "backend.enable", "backend", name, map[string]any{"retired": false})
		return err
	})
}

func loadBackends(ctx context.Context, q querier, name string) ([]Backend, error) {
	query := `SELECT name, host, api_type, hosted, paid, slots, retired, created, updated FROM backends`
	var args []any
	if name != "" {
		query += ` WHERE name = ?`
		args = append(args, name)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY name`, args...)
	if err != nil {
		return nil, err
	}
	var out []Backend
	for rows.Next() {
		var b Backend
		var hosted, paid, retired int
		var created, updated int64
		if err := rows.Scan(&b.Name, &b.Host, &b.APIType, &hosted, &paid, &b.Slots, &retired, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		b.Hosted, b.Paid, b.Retired = hosted != 0, paid != 0, retired != 0
		b.Created, b.Updated = time.UnixMilli(created), time.UnixMilli(updated)
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	// The single connection serves one result set at a time, so models are
	// read after the backend rows are closed.
	for i := range out {
		mrows, err := q.QueryContext(ctx, `SELECT model, paid FROM backend_models WHERE backend = ? ORDER BY model`, out[i].Name)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			var m BackendModel
			var paid int
			if err := mrows.Scan(&m.Model, &paid); err != nil {
				mrows.Close()
				return nil, err
			}
			m.Paid = paid != 0
			out[i].Models = append(out[i].Models, m)
		}
		if err := mrows.Err(); err != nil {
			mrows.Close()
			return nil, err
		}
		mrows.Close()
	}
	return out, nil
}

// GetBackend returns one backend or ErrNotFound.
func (s *Store) GetBackend(ctx context.Context, name string) (Backend, error) {
	bs, err := loadBackends(ctx, s.db, name)
	if err != nil {
		return Backend{}, err
	}
	if len(bs) == 0 {
		return Backend{}, fmt.Errorf("backend %q: %w", name, ErrNotFound)
	}
	return bs[0], nil
}

// ListBackends returns every backend (retired ones too), ordered by name.
func (s *Store) ListBackends(ctx context.Context) ([]Backend, error) {
	return loadBackends(ctx, s.db, "")
}
