package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
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
	// StallMinutes is how long a stage may say nothing, and how long a running
	// effort may sit with nothing in flight, before it counts as stalled (H30).
	StallMinutes int
	// AcceptanceRounds is how many times the merged result of a finished effort
	// is checked against the FRD and the gaps turned into follow-up tasks; 0
	// turns the check off (H31).
	AcceptanceRounds int
	// ReviewPanel is how many different reviewer models must each pass a
	// task before it merges; 1 is a single reviewer.
	ReviewPanel   int
	WorkerTimeout time.Duration
	ReviewTimeout time.Duration
	PlanTimeout   time.Duration
	VerifyTimeout time.Duration
	OpencodePath  string
	// BotchatURL is the BotChat service address and BotchatKeyFile the file holding the
	// API key every agent shares; both empty means the relay is off.
	BotchatURL     string
	BotchatKeyFile string
	// BotchatMode is "mirror" (post what happens) or "dispatch" (also hand each stage to an
	// agent through BotChat).
	BotchatMode string
	VerifySlots int
	// ContextFloor is the context window assumed for a model whose served
	// window is not known (H15).
	ContextFloor int
	// BackendCooldown is how long an unreachable backend is skipped (MB7).
	BackendCooldown time.Duration
	// SeatMux is the multiplexer interactive seats run in ("tmux").
	SeatMux string
	// SeatProfiles is a JSON object of interactive seat profiles by name, overriding the
	// built-in ones; empty uses the built-ins.
	SeatProfiles string
	// NotifyMacOS shows a macOS notification when something new needs attention.
	NotifyMacOS bool
	// NotifyCommand is run with the notice text as its one argument; NotifyURL is POSTed
	// {"text": ...}. Empty means off.
	NotifyCommand string
	NotifyURL     string
	// BrowseRoots are the directories the server-side directory picker may
	// list; an empty stored value means the server user's home directory.
	BrowseRoots []string
}

// limitKeys are the settings a project may override.
var limitKeys = []string{
	"max_attempts", "stuck_threshold", "max_infra_retries", "plan_max_rounds", "stall_minutes", "acceptance_rounds", "review_panel",
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
	if s.StallMinutes, err = validateInt("stall_minutes", m["stall_minutes"], 1, 100); err != nil {
		return s, err
	}
	if s.AcceptanceRounds, err = validateInt("acceptance_rounds", m["acceptance_rounds"], 0, 5); err != nil {
		return s, err
	}
	if s.ReviewPanel, err = validateInt("review_panel", m["review_panel"], 1, 3); err != nil {
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
	if s.BotchatURL, err = validateBotchatURL(m["botchat_url"]); err != nil {
		return s, err
	}
	s.BotchatKeyFile = strings.TrimSpace(m["botchat_key_file"])
	if s.BotchatKeyFile != "" && !filepath.IsAbs(s.BotchatKeyFile) && !strings.HasPrefix(s.BotchatKeyFile, "~/") {
		return s, Invalid("botchat_key_file", "must be an absolute path or start with ~/")
	}
	s.BotchatMode = strings.TrimSpace(m["botchat_mode"])
	if s.BotchatMode == "" {
		s.BotchatMode = "mirror"
	}
	if s.BotchatMode != "mirror" && s.BotchatMode != "dispatch" {
		return s, Invalid("botchat_mode", "must be mirror or dispatch")
	}
	if s.VerifySlots, err = validateInt("verify_slots", m["verify_slots"], 1, 64); err != nil {
		return s, err
	}
	if s.ContextFloor, err = validateInt("context_floor_tokens", m["context_floor_tokens"], 2048, 4194304); err != nil {
		return s, err
	}
	if s.BackendCooldown, err = validateTimeout("backend_cooldown", m["backend_cooldown"]); err != nil {
		return s, err
	}
	if s.BrowseRoots, err = parseBrowseRoots(m["browse_roots"]); err != nil {
		return s, err
	}
	s.SeatMux = strings.TrimSpace(m["seat_mux"])
	if s.SeatMux == "" {
		s.SeatMux = "tmux"
	}
	if s.SeatMux != "tmux" {
		return s, Invalid("seat_mux", "must be tmux")
	}
	s.SeatProfiles = strings.TrimSpace(m["seat_profiles"])
	if s.SeatProfiles != "" {
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(s.SeatProfiles), &probe); err != nil {
			return s, Invalid("seat_profiles", "must be a JSON object of profiles by name")
		}
	}
	switch strings.ToLower(strings.TrimSpace(m["notify_macos"])) {
	case "", "on", "true", "1", "yes":
		s.NotifyMacOS = true
	case "off", "false", "0", "no":
	default:
		return s, Invalid("notify_macos", "must be on or off")
	}
	s.NotifyCommand = strings.TrimSpace(m["notify_command"])
	if s.NotifyURL, err = validateNotifyURL(m["notify_url"]); err != nil {
		return s, err
	}
	return s, nil
}

// validateNotifyURL accepts an empty value (off) or an http(s) address.
func validateNotifyURL(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", Invalid("notify_url", "must be an http or https address")
	}
	return v, nil
}

// validateBotchatURL accepts an empty value (off) or an http(s) address with a host, no login
// and no query.
func validateBotchatURL(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", nil
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" {
		return "", Invalid("botchat_url", "must be an http or https address like https://botchat.example.net")
	}
	return strings.TrimRight(v, "/"), nil
}

// parseBrowseRoots reads the stored browse_roots value: a JSON list of
// absolute paths, or empty for the whole filesystem ("/"): the picker is
// unrestricted until the owner narrows it.
func parseBrowseRoots(v string) ([]string, error) {
	var roots []string
	if v != "" {
		if err := json.Unmarshal([]byte(v), &roots); err != nil {
			return nil, Invalid("browse_roots", "must be a list of absolute paths")
		}
	}
	if len(roots) == 0 {
		return []string{"/"}, nil
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		if !filepath.IsAbs(r) {
			return nil, Invalid("browse_roots", fmt.Sprintf("%q is not an absolute path", r))
		}
		out = append(out, filepath.Clean(r))
	}
	return out, nil
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
