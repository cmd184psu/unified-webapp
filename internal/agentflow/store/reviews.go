package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

const reviewCols = `id, project, repo_path, branch, base, commit_sha, task_id, title, objective,
	acceptance, exclusions, author_model, author_family, state, verify_json, verdict, findings,
	reviewer_backend, reviewer_model, notify_room, notify_prefix, error, created, finished`

func scanReview(r interface{ Scan(...any) error }) (model.Review, error) {
	var v model.Review
	var verify string
	err := r.Scan(&v.ID, &v.Project, &v.RepoPath, &v.Branch, &v.Base, &v.Commit, &v.TaskID, &v.Title,
		&v.Objective, &v.Acceptance, &v.Exclusions, &v.AuthorModel, &v.AuthorFamily, &v.State, &verify,
		&v.Verdict, &v.Findings, &v.ReviewerBackend, &v.ReviewerModel, &v.NotifyRoom, &v.NotifyPrefix,
		&v.Error, &v.Created, &v.Finished)
	if verify != "" {
		v.Verify = json.RawMessage(verify)
	}
	return v, err
}

// CreateReview stores a review request. The caller supplies the project, branch
// and task text; state starts as queued and the engine runs it.
func (s *Store) CreateReview(ctx context.Context, in model.Review) (model.Review, error) {
	if strings.TrimSpace(in.Project) == "" {
		return model.Review{}, Invalid("project", "required")
	}
	if strings.TrimSpace(in.Branch) == "" {
		return model.Review{}, Invalid("branch", "required")
	}
	if strings.TrimSpace(in.TaskID) == "" {
		return model.Review{}, Invalid("task.id", "required")
	}
	if in.State == "" {
		in.State = "queued"
	}
	if in.State != "queued" {
		return model.Review{}, Invalid("state", "a new review starts queued")
	}
	var out model.Review
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if err := requireProject(ctx, tx, in.Project); err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		res, err := tx.ExecContext(ctx, `INSERT INTO reviews
			(project, repo_path, branch, base, commit_sha, task_id, title, objective, acceptance, exclusions,
			 author_model, author_family, state, notify_room, notify_prefix, created)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			in.Project, in.RepoPath, in.Branch, in.Base, in.Commit, in.TaskID, in.Title, in.Objective,
			in.Acceptance, in.Exclusions, in.AuthorModel, in.AuthorFamily, in.State, in.NotifyRoom,
			in.NotifyPrefix, now)
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		if _, err := AppendEvent(tx, "review-api", "review.create", "review", fmt.Sprint(id),
			map[string]any{"project": in.Project, "branch": in.Branch, "task": in.TaskID}); err != nil {
			return err
		}
		out, err = requireReview(ctx, tx, id)
		return err
	})
	return out, err
}

// ReviewPatch is a partial update; nil fields are left alone.
type ReviewPatch struct {
	RepoPath        *string
	Commit          *string
	State           *string
	Verify          *json.RawMessage
	Verdict         *string
	Findings        *string
	ReviewerBackend *string
	ReviewerModel   *string
	Error           *string
	Finished        *int64
}

// UpdateReview applies p. A finished review cannot change its verdict or state
// back to queued or running.
func (s *Store) UpdateReview(ctx context.Context, id int64, p ReviewPatch) (model.Review, error) {
	if p.State != nil {
		switch *p.State {
		case "queued", "running", "done", "failed":
		default:
			return model.Review{}, Invalid("state", "must be queued, running, done or failed")
		}
	}
	if p.Verdict != nil && *p.Verdict != "" && *p.Verdict != "PASS" && *p.Verdict != "CHANGES-REQUESTED" {
		return model.Review{}, Invalid("verdict", "must be PASS or CHANGES-REQUESTED")
	}
	var out model.Review
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		cur, err := requireReview(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.State == "done" || cur.State == "failed" {
			if p.State != nil && *p.State != cur.State {
				return fmt.Errorf("review %d is %s: %w", id, cur.State, ErrConflict)
			}
			if p.Verdict != nil && *p.Verdict != cur.Verdict {
				return fmt.Errorf("review %d is %s: %w", id, cur.State, ErrConflict)
			}
		}
		if p.RepoPath != nil {
			cur.RepoPath = *p.RepoPath
		}
		if p.Commit != nil {
			cur.Commit = *p.Commit
		}
		if p.State != nil {
			cur.State = *p.State
		}
		if p.Verify != nil {
			cur.Verify = *p.Verify
		}
		if p.Verdict != nil {
			cur.Verdict = *p.Verdict
		}
		if p.Findings != nil {
			cur.Findings = *p.Findings
		}
		if p.ReviewerBackend != nil {
			cur.ReviewerBackend = *p.ReviewerBackend
		}
		if p.ReviewerModel != nil {
			cur.ReviewerModel = *p.ReviewerModel
		}
		if p.Error != nil {
			cur.Error = *p.Error
		}
		if p.Finished != nil {
			cur.Finished = *p.Finished
		}
		if cur.State == "done" || cur.State == "failed" {
			if cur.Finished == 0 {
				cur.Finished = time.Now().UnixMilli()
			}
		}
		var verify string
		if len(cur.Verify) > 0 {
			verify = string(cur.Verify)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reviews SET repo_path = ?, commit_sha = ?, state = ?,
			verify_json = ?, verdict = ?, findings = ?, reviewer_backend = ?, reviewer_model = ?,
			error = ?, finished = ? WHERE id = ?`,
			cur.RepoPath, cur.Commit, cur.State, verify, cur.Verdict, cur.Findings, cur.ReviewerBackend,
			cur.ReviewerModel, cur.Error, cur.Finished, id); err != nil {
			return err
		}
		out, err = requireReview(ctx, tx, id)
		return err
	})
	return out, err
}

func requireReview(ctx context.Context, q querier, id int64) (model.Review, error) {
	v, err := scanReview(q.QueryRowContext(ctx, `SELECT `+reviewCols+` FROM reviews WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("review %d: %w", id, ErrNotFound)
	}
	return v, err
}

// GetReview returns one review or ErrNotFound.
func (s *Store) GetReview(ctx context.Context, id int64) (model.Review, error) {
	return requireReview(ctx, s.db, id)
}

// ListReviews returns reviews newest first, optionally filtered by project.
func (s *Store) ListReviews(ctx context.Context, project string) ([]model.Review, error) {
	q := `SELECT ` + reviewCols + ` FROM reviews`
	var args []any
	if project != "" {
		q += ` WHERE project = ?`
		args = append(args, project)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Review
	for rows.Next() {
		v, err := scanReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
