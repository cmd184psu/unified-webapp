package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Project is a registered repository (PJ1). ModelScope "local" restricts it
// to operator-run backends (PP1); "any" also allows hosted ones. Limits holds
// only the overrides; a key that is absent uses the global value (CF2).
type Project struct {
	Slug         string
	RepoPath     string
	ModelScope   string
	FeedbackCmds []string
	Limits       map[string]string
	// CleanupEnabled runs the optional cleanup stage after a review pass (AT11).
	CleanupEnabled bool
	Created        time.Time
	Updated        time.Time
}

func projectSlugs(ctx context.Context, q querier) ([]string, error) {
	rows, err := q.QueryContext(ctx, `SELECT slug FROM projects ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func requireProject(ctx context.Context, tx *sql.Tx, slug string) error {
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE slug = ?`, slug).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("project %q: %w", slug, ErrNotFound)
	}
	return nil
}

func (s *Store) validateProject(p Project) (resolved string, err error) {
	if !slugRE.MatchString(p.Slug) {
		return "", Invalid("slug", "must match "+slugRE.String())
	}
	if p.ModelScope != "local" && p.ModelScope != "any" {
		return "", Invalid("model_scope", "must be local or any")
	}
	for i, c := range p.FeedbackCmds {
		if strings.TrimSpace(c) == "" {
			return "", Invalid(fmt.Sprintf("feedback_cmds[%d]", i), "must not be blank")
		}
	}
	for k := range p.Limits {
		if !isLimitKey(k) {
			return "", Invalid("limits."+k, "not an overridable limit")
		}
	}
	return s.validateRepoPath(p.RepoPath)
}

// checkOverrides validates p's overrides on top of the global settings, so
// range and cross-field rules (stuck_threshold <= max_attempts) hold for the
// project's effective limits. Blank values are not overrides.
func checkOverrides(ctx context.Context, tx *sql.Tx, p Project) error {
	m, err := readKV(ctx, tx, `SELECT key, value FROM settings`)
	if err != nil {
		return err
	}
	for k, v := range p.Limits {
		if strings.TrimSpace(v) != "" {
			m[k] = strings.TrimSpace(v)
		}
	}
	if _, err := parseSettings(m); err != nil {
		var fe *FieldError
		if errors.As(err, &fe) && isLimitKey(fe.Field) {
			return Invalid("limits."+fe.Field, fe.Msg)
		}
		return err
	}
	return nil
}

func writeProjectBody(ctx context.Context, tx *sql.Tx, p Project) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_limits WHERE project = ?`, p.Slug); err != nil {
		return err
	}
	keys := make([]string, 0, len(p.Limits))
	for k, v := range p.Limits {
		if strings.TrimSpace(v) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_limits (project, key, value) VALUES (?, ?, ?)`,
			p.Slug, k, strings.TrimSpace(p.Limits[k])); err != nil {
			return err
		}
	}
	return nil
}

func feedbackJSON(cmds []string) (string, error) {
	if cmds == nil {
		cmds = []string{}
	}
	b, err := json.Marshal(cmds)
	return string(b), err
}

// repoPathTaken reports whether another project already holds repo (PJ4).
func repoPathTaken(ctx context.Context, tx *sql.Tx, repo, exceptSlug string) error {
	var owner string
	err := tx.QueryRowContext(ctx, `SELECT slug FROM projects WHERE repo_path = ? AND slug <> ?`, repo, exceptSlug).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("repo_path already registered by project %q: %w", owner, ErrConflict)
}

// CreateProject validates and registers a project. The stored repo_path is
// the symlink-resolved path. A duplicate slug or repo_path is ErrConflict.
func (s *Store) CreateProject(ctx context.Context, actor string, p Project) error {
	repo, err := s.validateProject(p)
	if err != nil {
		return err
	}
	fb, err := feedbackJSON(p.FeedbackCmds)
	if err != nil {
		return Invalid("feedback_cmds", "not JSON-encodable")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE slug = ?`, p.Slug).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("project %q: %w", p.Slug, ErrConflict)
		}
		if err := repoPathTaken(ctx, tx, repo, ""); err != nil {
			return err
		}
		if err := checkOverrides(ctx, tx, p); err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO projects (slug, repo_path, model_scope, feedback_cmds_json, cleanup_enabled, created, updated)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, p.Slug, repo, p.ModelScope, fb, b2i(p.CleanupEnabled), now, now); err != nil {
			return err
		}
		if err := writeProjectBody(ctx, tx, p); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "project.create", "project", p.Slug, map[string]string{"repo_path": repo})
		return err
	})
}

// UpdateProject replaces a project's repo_path, scope, feedback commands and
// limit overrides (the slug identifies it). Unknown slug is ErrNotFound.
func (s *Store) UpdateProject(ctx context.Context, actor string, p Project) error {
	repo, err := s.validateProject(p)
	if err != nil {
		return err
	}
	fb, err := feedbackJSON(p.FeedbackCmds)
	if err != nil {
		return Invalid("feedback_cmds", "not JSON-encodable")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireProject(ctx, tx, p.Slug); err != nil {
			return err
		}
		if err := repoPathTaken(ctx, tx, repo, p.Slug); err != nil {
			return err
		}
		if err := checkOverrides(ctx, tx, p); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE projects SET repo_path = ?, model_scope = ?, feedback_cmds_json = ?, cleanup_enabled = ?, updated = ?
			WHERE slug = ?`, repo, p.ModelScope, fb, b2i(p.CleanupEnabled), time.Now().UnixMilli(), p.Slug); err != nil {
			return err
		}
		if err := writeProjectBody(ctx, tx, p); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "project.update", "project", p.Slug, map[string]string{"repo_path": repo})
		return err
	})
}

func loadProjects(ctx context.Context, q querier, slug string) ([]Project, error) {
	query := `SELECT slug, repo_path, model_scope, feedback_cmds_json, cleanup_enabled, created, updated FROM projects`
	var args []any
	if slug != "" {
		query += ` WHERE slug = ?`
		args = append(args, slug)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY slug`, args...)
	if err != nil {
		return nil, err
	}
	var out []Project
	for rows.Next() {
		var p Project
		var fb string
		var cleanup int
		var created, updated int64
		if err := rows.Scan(&p.Slug, &p.RepoPath, &p.ModelScope, &fb, &cleanup, &created, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(fb), &p.FeedbackCmds); err != nil {
			rows.Close()
			return nil, fmt.Errorf("project %q feedback commands: %w", p.Slug, err)
		}
		p.CleanupEnabled = cleanup != 0
		p.Created, p.Updated = time.UnixMilli(created), time.UnixMilli(updated)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		lim, err := readKV(ctx, q, `SELECT key, value FROM project_limits WHERE project = ?`, out[i].Slug)
		if err != nil {
			return nil, err
		}
		out[i].Limits = lim
	}
	return out, nil
}

// GetProject returns one project or ErrNotFound.
func (s *Store) GetProject(ctx context.Context, slug string) (Project, error) {
	ps, err := loadProjects(ctx, s.db, slug)
	if err != nil {
		return Project{}, err
	}
	if len(ps) == 0 {
		return Project{}, fmt.Errorf("project %q: %w", slug, ErrNotFound)
	}
	return ps[0], nil
}

// ListProjects returns every project ordered by slug.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	return loadProjects(ctx, s.db, "")
}

// DeleteProject removes a project's registration: its limit overrides and
// per-project role candidates go with it. A project that has efforts keeps
// them as history, so it cannot be deleted (ErrConflict).
func (s *Store) DeleteProject(ctx context.Context, actor, slug string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireProject(ctx, tx, slug); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM efforts WHERE project = ?`, slug).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("project %q has %d effort(s): %w", slug, n, ErrConflict)
		}
		for _, q := range []string{
			`DELETE FROM project_limits WHERE project = ?`,
			`DELETE FROM role_candidates WHERE scope = 'project' AND project_id = ?`,
			`DELETE FROM projects WHERE slug = ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, slug); err != nil {
				return err
			}
		}
		_, err := AppendEvent(tx, actor, "project.delete", "project", slug, nil)
		return err
	})
}
