package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Settings is the typed view of the settings table (CF1). The same shape,
// after project overrides are applied, is a project's effective limits (CF2).
type Settings struct {
	MaxAttempts     int
	StuckThreshold  int
	MaxInfraRetries int
	PlanMaxRounds   int
	WorkerTimeout   time.Duration
	ReviewTimeout   time.Duration
	PlanTimeout     time.Duration
	VerifyTimeout   time.Duration
	OpencodePath    string
	VerifySlots     int
	// BackendCooldown is how long an unreachable backend is skipped (MB7).
	BackendCooldown time.Duration
}

// limitKeys are the settings a project may override.
var limitKeys = []string{
	"max_attempts", "stuck_threshold", "max_infra_retries", "plan_max_rounds",
	"worker_timeout", "review_timeout", "plan_timeout", "verify_timeout",
}

func isLimitKey(k string) bool {
	for _, l := range limitKeys {
		if l == k {
			return true
		}
	}
	return false
}

// querier is satisfied by *sql.Tx and *sql.DB.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func readKV(ctx context.Context, q querier, query string, args ...any) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// parseSettings validates a full key/value map (the global rows, possibly
// overlaid with a project's overrides) and returns the typed value. Every
// key is checked, including the cross-field rule stuck_threshold <= max_attempts.
// max_attempts therefore has a floor of 2: stuck_threshold is at least 2, so
// a single attempt could never satisfy the rule.
func parseSettings(m map[string]string) (Settings, error) {
	var s Settings
	var err error
	if s.MaxAttempts, err = validateInt("max_attempts", m["max_attempts"], 2, 50); err != nil {
		return s, err
	}
	if s.StuckThreshold, err = validateInt("stuck_threshold", m["stuck_threshold"], 2, 50); err != nil {
		return s, err
	}
	if s.StuckThreshold > s.MaxAttempts {
		return s, Invalid("stuck_threshold", fmt.Sprintf("must not exceed max_attempts (%d)", s.MaxAttempts))
	}
	if s.MaxInfraRetries, err = validateInt("max_infra_retries", m["max_infra_retries"], 0, 50); err != nil {
		return s, err
	}
	if s.PlanMaxRounds, err = validateInt("plan_max_rounds", m["plan_max_rounds"], 1, 50); err != nil {
		return s, err
	}
	if s.WorkerTimeout, err = validateTimeout("worker_timeout", m["worker_timeout"]); err != nil {
		return s, err
	}
	if s.ReviewTimeout, err = validateTimeout("review_timeout", m["review_timeout"]); err != nil {
		return s, err
	}
	if s.PlanTimeout, err = validateTimeout("plan_timeout", m["plan_timeout"]); err != nil {
		return s, err
	}
	if s.VerifyTimeout, err = validateTimeout("verify_timeout", m["verify_timeout"]); err != nil {
		return s, err
	}
	s.OpencodePath = strings.TrimSpace(m["opencode_path"])
	if s.OpencodePath == "" {
		return s, Invalid("opencode_path", "required")
	}
	if s.VerifySlots, err = validateInt("verify_slots", m["verify_slots"], 1, 64); err != nil {
		return s, err
	}
	if s.BackendCooldown, err = validateTimeout("backend_cooldown", m["backend_cooldown"]); err != nil {
		return s, err
	}
	return s, nil
}

func loadSettings(ctx context.Context, q querier) (Settings, error) {
	m, err := readKV(ctx, q, `SELECT key, value FROM settings`)
	if err != nil {
		return Settings{}, err
	}
	return parseSettings(m)
}

func loadEffective(ctx context.Context, q querier, slug string) (Settings, error) {
	m, err := readKV(ctx, q, `SELECT key, value FROM settings`)
	if err != nil {
		return Settings{}, err
	}
	over, err := readKV(ctx, q, `SELECT key, value FROM project_limits WHERE project = ?`, slug)
	if err != nil {
		return Settings{}, err
	}
	for k, v := range over {
		m[k] = v
	}
	return parseSettings(m)
}

// GetSettings returns the global settings.
func (s *Store) GetSettings(ctx context.Context) (Settings, error) {
	return loadSettings(ctx, s.db)
}

// UpdateSettings applies patch (key -> value; ints as decimals, timeouts as Go
// durations such as "45m") in one transaction. The merged result must be
// valid both globally and for every project with overrides (CF1, SV1); an
// invalid patch changes nothing and returns an ErrInvalid naming the field.
func (s *Store) UpdateSettings(ctx context.Context, actor string, patch map[string]string) error {
	if len(patch) == 0 {
		return Invalid("settings", "no changes given")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		m, err := readKV(ctx, tx, `SELECT key, value FROM settings`)
		if err != nil {
			return err
		}
		for k, v := range patch {
			if _, known := m[k]; !known {
				return Invalid(k, "unknown setting")
			}
			m[k] = strings.TrimSpace(v)
		}
		if _, err := parseSettings(m); err != nil {
			return err
		}
		keys := make([]string, 0, len(patch))
		for k := range patch {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, err := tx.ExecContext(ctx, `UPDATE settings SET value = ? WHERE key = ?`, m[k], k); err != nil {
				return err
			}
		}
		// A project override may now conflict with the new global value.
		slugs, err := projectSlugs(ctx, tx)
		if err != nil {
			return err
		}
		for _, slug := range slugs {
			if _, err := loadEffective(ctx, tx, slug); err != nil {
				var fe *FieldError
				if errors.As(err, &fe) {
					return Invalid("projects."+slug+"."+fe.Field, "conflicts with this project's overrides: "+fe.Msg)
				}
				return err
			}
		}
		_, err = AppendEvent(tx, actor, "settings.update", "settings", "global", patch)
		return err
	})
}

// EffectiveSettings returns the global settings with the project's overrides
// applied; a project without an override for a key gets the global value.
func (s *Store) EffectiveSettings(ctx context.Context, slug string) (Settings, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	var found int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE slug = ?`, slug).Scan(&found); err != nil {
		return Settings{}, err
	}
	if found == 0 {
		return Settings{}, fmt.Errorf("project %q: %w", slug, ErrNotFound)
	}
	return loadEffective(ctx, tx, slug)
}
