package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Candidate is one entry of a role's ordered model list (MB4). Paid is true
// when the backend or the model is paid; such an entry exists only because it
// was listed explicitly (MB5).
type Candidate struct {
	Scope     string // "global" or "project"
	ProjectID string // project slug when Scope is "project"
	Role      string
	Position  int
	Backend   string
	Model     string
	Paid      bool
}

// CandidateRef names a backend model in a SetRoleCandidates call.
type CandidateRef struct {
	Backend string
	Model   string
}

// SetRoleCandidates replaces the ordered candidate list for role in scope
// ("global", or "project" with a project slug). The list must be non-empty
// and every entry must name a model of a live backend. Nothing is eligible
// unless listed here, so a paid model is used only when named (MB5).
func (s *Store) SetRoleCandidates(ctx context.Context, actor, scope, projectID, role string, refs []CandidateRef) error {
	if !validRole(role) {
		return Invalid("role", "unknown role")
	}
	if err := validateScope(scope, projectID); err != nil {
		return err
	}
	if len(refs) == 0 {
		return Invalid("candidates", "must not be empty")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if scope == "project" {
			if err := requireProject(ctx, tx, projectID); err != nil {
				return err
			}
		}
		seen := map[CandidateRef]bool{}
		for i, r := range refs {
			if seen[r] {
				return Invalid(fmt.Sprintf("candidates[%d]", i), fmt.Sprintf("duplicate %s/%s", r.Backend, r.Model))
			}
			seen[r] = true
			var retired int
			var tools string
			err := tx.QueryRowContext(ctx, `
				SELECT b.retired, m.tools FROM backend_models m JOIN backends b ON b.name = m.backend
				WHERE m.backend = ? AND m.model = ?`, r.Backend, r.Model).Scan(&retired, &tools)
			if errors.Is(err, sql.ErrNoRows) {
				return Invalid(fmt.Sprintf("candidates[%d]", i), fmt.Sprintf("unknown backend model %s/%s", r.Backend, r.Model))
			}
			if err != nil {
				return err
			}
			if retired != 0 {
				return Invalid(fmt.Sprintf("candidates[%d]", i), fmt.Sprintf("backend %q is retired", r.Backend))
			}
			if tools == "no" {
				return Invalid(fmt.Sprintf("candidates[%d]", i), fmt.Sprintf("%s/%s does not support tool calls, which every role needs", r.Backend, r.Model))
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM role_candidates WHERE scope = ? AND project_id = ? AND role = ?`,
			scope, projectID, role); err != nil {
			return err
		}
		for i, r := range refs {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO role_candidates (scope, project_id, role, position, backend, model)
				VALUES (?, ?, ?, ?, ?, ?)`, scope, projectID, role, i, r.Backend, r.Model); err != nil {
				return err
			}
		}
		_, err := AppendEvent(tx, actor, "candidates.set", "role_candidates", scope+":"+projectID+":"+role, refs)
		return err
	})
}

// ClearProjectRoleCandidates removes a project's override list for role so
// the project falls back to the global list.
func (s *Store) ClearProjectRoleCandidates(ctx context.Context, actor, projectID, role string) error {
	if !validRole(role) {
		return Invalid("role", "unknown role")
	}
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireProject(ctx, tx, projectID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM role_candidates WHERE scope = 'project' AND project_id = ? AND role = ?`,
			projectID, role); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "candidates.clear", "role_candidates", "project:"+projectID+":"+role, nil)
		return err
	})
}

func validateScope(scope, projectID string) error {
	switch scope {
	case "global":
		if projectID != "" {
			return Invalid("project_id", "must be empty for global scope")
		}
	case "project":
		if projectID == "" {
			return Invalid("project_id", "required for project scope")
		}
	default:
		return Invalid("scope", "must be global or project")
	}
	return nil
}

func loadCandidates(ctx context.Context, q querier) ([]Candidate, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT c.scope, c.project_id, c.role, c.position, c.backend, c.model, b.paid, COALESCE(m.paid, 0)
		FROM role_candidates c
		JOIN backends b ON b.name = c.backend
		LEFT JOIN backend_models m ON m.backend = c.backend AND m.model = c.model
		ORDER BY c.scope, c.project_id, c.role, c.position`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		var bp, mp int
		if err := rows.Scan(&c.Scope, &c.ProjectID, &c.Role, &c.Position, &c.Backend, &c.Model, &bp, &mp); err != nil {
			return nil, err
		}
		c.Paid = bp != 0 || mp != 0
		out = append(out, c)
	}
	return out, rows.Err()
}
