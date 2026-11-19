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
	// MaxConcurrent caps how many stages may run this model at once on the
	// backend (PX5); 0 means only the backend's slots limit it.
	MaxConcurrent int
	// ContextTokens is the largest context window the model supports (what the
	// host reports); 0 means unknown. The host often serves less (H15).
	ContextTokens int
	// WindowTokens is the context window the model is served with, 0 when not
	// known; WindowSource is "set" (the owner's value), "observed" (a run was
	// cut off at this size) or "host" (the host said so).
	WindowTokens int
	WindowSource string
	// Tier rates the model: 1 strong, 2 capable, 3 light, 0 not rated yet (H3).
	Tier int
	// Tools is whether the host says the model supports tool calls, which
	// every runner role needs (H10): "yes", "no", or "" when not known. The
	// server fills it from the host; a client's value is not trusted.
	Tools string
	// TooBig is set by Extend windows when the model's weights leave no room
	// for the minimum context inside the host's memory budget (H15e); the
	// model is then passed over until a larger budget clears it.
	TooBig bool
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
	// MemoryBudgetMB is the memory the host may give one loaded model, weights and
	// context cache together (H15d); 0 means not set, so windows are not extended.
	MemoryBudgetMB int
	// WindowCapTokens caps an extended window below what memory allows, for hosts
	// where a long context is too slow to be useful; 0 means no cap.
	WindowCapTokens int
	// DutyPercent caps how much of the time the host is kept busy: after a stage
	// ends the backend is given no new one for as long as the stage ran times
	// (100-d)/d, up to dutyRestMax. 0 means no cap (100 is stored as 0).
	DutyPercent int
	Retired     bool
	// HasKey says an API key is stored; the key itself is never loaded with
	// the backend (see BackendKey).
	HasKey  bool
	Models  []BackendModel
	Created time.Time
	Updated time.Time
}

// dutyStored is the value kept for a duty cap: 100 and 0 both mean no cap.
func dutyStored(d int) int {
	if d >= 100 {
		return 0
	}
	return d
}

// HostlessAPIType says the api type is a CLI or built-in provider that has no host of its own.
func HostlessAPIType(t string) bool {
	switch t {
	case "opencode", "grok", "codex", "claude", "muse", "agy", "botchat_seat", "freebuff_seat":
		return true
	}
	return false
}

func validateBackend(b Backend) error {
	if err := validateBackendName("name", b.Name); err != nil {
		return err
	}
	if !apiTypes[b.APIType] {
		return Invalid("api_type", "must be ollama, openai_compatible, anthropic, opencode, grok, codex, claude, muse, agy, botchat_seat or freebuff_seat")
	}
	// opencode's own provider and the Grok and Codex CLIs have no host of their own.
	if !HostlessAPIType(b.APIType) {
		if err := validateHostURL("host", b.Host); err != nil {
			return err
		}
	}
	if b.MemoryBudgetMB < 0 || b.WindowCapTokens < 0 {
		return Invalid("memory_budget_mb", "memory budget and window cap must be 0 (not set) or more")
	}
	if b.DutyPercent != 0 && (b.DutyPercent < 10 || b.DutyPercent > 100) {
		return Invalid("duty_percent", "must be 100 (no cap) or between 10 and 99")
	}
	if b.Slots < 1 {
		return Invalid("slots", "must be at least 1")
	}
	seen := map[string]bool{}
	for _, m := range b.Models {
		if m.Model == "" || m.Model != strings.TrimSpace(m.Model) {
			return Invalid("models", "model name required, without leading or trailing whitespace")
		}
		if m.MaxConcurrent < 0 {
			return Invalid("models", fmt.Sprintf("model %q: max_concurrent must be 0 (no limit) or more", m.Model))
		}
		if m.Tools != "" && m.Tools != "yes" && m.Tools != "no" {
			return Invalid("models", fmt.Sprintf("model %q: tools must be yes, no or empty", m.Model))
		}
		if m.Tier < 0 || m.Tier > 3 {
			return Invalid("models", fmt.Sprintf("model %q: tier must be 0 (not rated), 1, 2 or 3", m.Model))
		}
		if m.ContextTokens < 0 {
			return Invalid("models", fmt.Sprintf("model %q: context_tokens must be 0 (unknown) or more", m.Model))
		}
		if m.WindowTokens < 0 {
			return Invalid("models", fmt.Sprintf("model %q: window_tokens must be 0 (unknown) or more", m.Model))
		}
		switch m.WindowSource {
		case "", "set", "observed", "host":
		default:
			return Invalid("models", fmt.Sprintf("model %q: window_source must be set, observed, host or empty", m.Model))
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
		if _, err := tx.ExecContext(ctx, `INSERT INTO backend_models (backend, model, paid, max_concurrent, context_tokens, tier, tools, window_tokens, window_source, too_big) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			b.Name, m.Model, b2i(m.Paid), m.MaxConcurrent, m.ContextTokens, m.Tier, m.Tools, m.WindowTokens, m.WindowSource, b2i(m.TooBig)); err != nil {
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
			INSERT INTO backends (name, host, api_type, hosted, paid, slots, memory_budget_mb, window_cap_tokens, duty_percent, retired, created, updated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			b.Name, b.Host, b.APIType, b2i(b.Hosted), b2i(b.Paid), b.Slots, b.MemoryBudgetMB, b.WindowCapTokens, dutyStored(b.DutyPercent), now, now); err != nil {
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
			UPDATE backends SET host = ?, api_type = ?, hosted = ?, paid = ?, slots = ?, memory_budget_mb = ?, window_cap_tokens = ?, duty_percent = ?, updated = ?
			WHERE name = ?`,
			b.Host, b.APIType, b2i(b.Hosted), b2i(b.Paid), b.Slots, b.MemoryBudgetMB, b.WindowCapTokens, dutyStored(b.DutyPercent), time.Now().UnixMilli(), b.Name)
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

// BackendUses names every candidate list that still lists the backend, as
// "global worker" or "project p reviewer", so a refusal can say what to change.
func (s *Store) BackendUses(ctx context.Context, name string) ([]string, error) {
	return backendUses(ctx, s.db, name)
}

func backendUses(ctx context.Context, q querier, name string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT DISTINCT scope, project_id, role FROM role_candidates
		WHERE backend = ? ORDER BY scope, project_id, role`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var scope, project, role string
		if err := rows.Scan(&scope, &project, &role); err != nil {
			return nil, err
		}
		if scope == "project" {
			out = append(out, fmt.Sprintf("project %s %s", project, role))
		} else {
			out = append(out, "global "+role)
		}
	}
	return out, rows.Err()
}

// DeleteBackend removes a backend, its models and its model checks and
// installs. It is refused (ErrConflict) while the backend is enabled, so its
// lane has already been paused and emptied, and while any global or project
// candidate list still names it. Stage rows keep the name as plain text, so
// history is untouched. It returns ErrNotFound for an unknown name.
func (s *Store) DeleteBackend(ctx context.Context, actor, name string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		var retired int
		err := tx.QueryRowContext(ctx, `SELECT retired FROM backends WHERE name = ?`, name).Scan(&retired)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("backend %q: %w", name, ErrNotFound)
		}
		if err != nil {
			return err
		}
		if retired == 0 {
			return fmt.Errorf("backend %q is enabled: disable it first: %w", name, ErrConflict)
		}
		uses, err := backendUses(ctx, tx, name)
		if err != nil {
			return err
		}
		if len(uses) > 0 {
			return fmt.Errorf("backend %q is still on the candidate lists %s: remove it from them first: %w", name, strings.Join(uses, ", "), ErrConflict)
		}
		for _, q := range []string{
			`DELETE FROM backend_models WHERE backend = ?`,
			`DELETE FROM model_checks WHERE backend = ?`,
			`DELETE FROM model_rest WHERE backend = ?`,
			`DELETE FROM model_installs WHERE backend = ?`,
			`DELETE FROM backends WHERE name = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, name); err != nil {
				return err
			}
		}
		_, err = AppendEvent(tx, actor, "backend.delete", "backend", name, map[string]any{"deleted": true})
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
	query := `SELECT name, host, api_type, hosted, paid, slots, memory_budget_mb, window_cap_tokens, duty_percent, retired, api_key <> '', created, updated FROM backends`
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
		var hosted, paid, retired, hasKey int
		var created, updated int64
		if err := rows.Scan(&b.Name, &b.Host, &b.APIType, &hosted, &paid, &b.Slots, &b.MemoryBudgetMB, &b.WindowCapTokens, &b.DutyPercent, &retired, &hasKey, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		b.Hosted, b.Paid, b.Retired, b.HasKey = hosted != 0, paid != 0, retired != 0, hasKey != 0
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
		mrows, err := q.QueryContext(ctx, `SELECT model, paid, max_concurrent, context_tokens, tier, tools, window_tokens, window_source, too_big FROM backend_models WHERE backend = ? ORDER BY model`, out[i].Name)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			var m BackendModel
			var paid, tooBig int
			if err := mrows.Scan(&m.Model, &paid, &m.MaxConcurrent, &m.ContextTokens, &m.Tier, &m.Tools, &m.WindowTokens, &m.WindowSource, &tooBig); err != nil {
				mrows.Close()
				return nil, err
			}
			m.Paid = paid != 0
			m.TooBig = tooBig != 0
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

// ValidateAPIKey checks a key before it is stored: it travels as an
// environment variable, so it must be one printable line. Empty clears.
func ValidateAPIKey(key string) error {
	if strings.TrimSpace(key) != key || strings.ContainsAny(key, "\x00\r\n") {
		return Invalid("api_key", "must be a single line without leading or trailing whitespace")
	}
	return nil
}

// SetBackendKey stores a backend's API key; an empty key clears it. The audit
// event records only that a key is set, never the value.
func (s *Store) SetBackendKey(ctx context.Context, actor, name, key string) error {
	if err := ValidateAPIKey(key); err != nil {
		return err
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE backends SET api_key = ?, updated = ? WHERE name = ?`,
			key, time.Now().UnixMilli(), name)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("backend %q: %w", name, ErrNotFound)
		}
		_, err = AppendEvent(tx, actor, "backend.key", "backend", name, map[string]any{"has_key": key != ""})
		return err
	})
}

// BackendKey returns a backend's stored API key, or "" when there is none.
// It is read at stage time only, so a change applies to the next stage.
func (s *Store) BackendKey(ctx context.Context, name string) (string, error) {
	var key string
	err := s.db.QueryRowContext(ctx, `SELECT api_key FROM backends WHERE name = ?`, name).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("backend %q: %w", name, ErrNotFound)
	}
	return key, err
}

// SetModelWindow records the context window a model is served with and where
// the number came from (H15). It is a no-op for a model the backend does not list.
func (s *Store) SetModelWindow(ctx context.Context, backend, model string, tokens int, source string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backend_models SET window_tokens = ?, window_source = ? WHERE backend = ? AND model = ?`,
		tokens, source, backend, model)
	return err
}

// SetModelTooBig marks, or clears the mark on, a model whose weights do not fit
// its host's memory budget (H15e). It is a no-op for a model the backend does
// not list.
func (s *Store) SetModelTooBig(ctx context.Context, backend, model string, tooBig bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE backend_models SET too_big = ? WHERE backend = ? AND model = ?`, b2i(tooBig), backend, model)
	return err
}
