package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// fillFRDJSON runs model.FillFRD over a stored FRD body. A body that needs
// nothing filled is returned byte for byte, so FRDs saved before the filler
// existed are not rewritten.
func fillFRDJSON(raw []byte, effort int64, now time.Time) ([]byte, error) {
	var f, filled model.FRD
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("frd: %w", err)
	}
	if err := json.Unmarshal(raw, &filled); err != nil {
		return nil, fmt.Errorf("frd: %w", err)
	}
	model.FillFRD(&filled, effort, now)
	if reflect.DeepEqual(f, filled) {
		return raw, nil
	}
	return json.Marshal(filled)
}

func validDocJSON(kind string, raw []byte) error {
	if !slices.Contains([]string{model.DocFRD, model.DocPlan, model.DocReview}, kind) {
		return Invalid("kind", "must be frd, plan or review")
	}
	if kind == model.DocFRD {
		if _, err := model.ParseFRD(raw); err != nil {
			return Invalid("json", err.Error())
		}
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return Invalid("json", "must be a JSON object")
	}
	return nil
}

// SaveDoc stores raw as the next revision of the effort's document of kind.
// Revisions are immutable: an edit after approval is a new revision and leaves
// the approved one untouched (DBS4).
func (s *Store) SaveDoc(ctx context.Context, actor string, effort int64, kind string, raw []byte) (model.Doc, error) {
	return s.SaveDocWith(ctx, actor, effort, kind, raw, nil)
}

// SaveDocWith is SaveDoc that runs also in the same transaction once the
// revision is written: if it fails nothing is saved, and what it writes
// commits with the revision.
func (s *Store) SaveDocWith(ctx context.Context, actor string, effort int64, kind string, raw []byte, also func(tx *sql.Tx, d model.Doc) error) (model.Doc, error) {
	if kind == model.DocFRD {
		filled, err := fillFRDJSON(raw, effort, time.UnixMilli(nowMS()).UTC())
		if err != nil {
			return model.Doc{}, Invalid("json", err.Error())
		}
		raw = filled
	}
	if err := validDocJSON(kind, raw); err != nil {
		return model.Doc{}, err
	}
	var out model.Doc
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := requireEffort(ctx, tx, effort); err != nil {
			return err
		}
		var rev int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision), 0) + 1 FROM documents WHERE effort = ? AND kind = ?`, effort, kind).Scan(&rev); err != nil {
			return err
		}
		now := nowMS()
		if _, err := tx.ExecContext(ctx, `INSERT INTO documents (effort, kind, revision, json, created_by, created) VALUES (?, ?, ?, ?, ?, ?)`,
			effort, kind, rev, string(raw), actor, now); err != nil {
			return err
		}
		if _, err := AppendEvent(tx, actor, "doc.save", "effort", fmt.Sprint(effort), map[string]any{"kind": kind, "revision": rev}); err != nil {
			return err
		}
		out = model.Doc{Effort: effort, Kind: kind, Revision: rev, JSON: raw, CreatedBy: actor, Created: now}
		if also != nil {
			return also(tx, out)
		}
		return nil
	})
	return out, err
}

// GetDoc returns a revision, or the latest when revision is 0.
func (s *Store) GetDoc(ctx context.Context, effort int64, kind string, revision int) (model.Doc, error) {
	if revision < 0 {
		return model.Doc{}, Invalid("revision", "must not be negative")
	}
	q := `SELECT revision, json, created_by, created FROM documents WHERE effort = ? AND kind = ?`
	args := []any{effort, kind}
	if revision > 0 {
		q += ` AND revision = ?`
		args = append(args, revision)
	} else {
		q += ` ORDER BY revision DESC LIMIT 1`
	}
	d := model.Doc{Effort: effort, Kind: kind}
	var raw string
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&d.Revision, &raw, &d.CreatedBy, &d.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("%s revision %d of effort %d: %w", kind, revision, effort, ErrNotFound)
	}
	d.JSON = json.RawMessage(raw)
	return d, err
}

// ApproveDoc records approval of a frd or plan revision.
func (s *Store) ApproveDoc(ctx context.Context, actor string, effort int64, kind string, revision int) (model.Approval, error) {
	if kind != model.DocFRD && kind != model.DocPlan {
		return model.Approval{}, Invalid("kind", "only frd and plan are approved")
	}
	a := model.Approval{Effort: effort, Kind: kind, Revision: revision, ApprovedBy: actor, At: nowMS()}
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents WHERE effort = ? AND kind = ? AND revision = ?`, effort, kind, revision).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%s revision %d of effort %d: %w", kind, revision, effort, ErrNotFound)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO approvals (effort, kind, revision, approved_by, at) VALUES (?, ?, ?, ?, ?)`,
			effort, kind, revision, actor, a.At); err != nil {
			return err
		}
		_, err := AppendEvent(tx, actor, "doc.approve", "effort", fmt.Sprint(effort), map[string]any{"kind": kind, "revision": revision})
		return err
	})
	return a, err
}

// ApprovedRevision returns the most recently approved revision of kind, or
// ErrNotFound when none was approved.
func (s *Store) ApprovedRevision(ctx context.Context, effort int64, kind string) (model.Approval, error) {
	a := model.Approval{Effort: effort, Kind: kind}
	err := s.db.QueryRowContext(ctx, `SELECT revision, approved_by, at FROM approvals WHERE effort = ? AND kind = ?
		ORDER BY at DESC, rowid DESC LIMIT 1`, effort, kind).Scan(&a.Revision, &a.ApprovedBy, &a.At)
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("no approved %s for effort %d: %w", kind, effort, ErrNotFound)
	}
	return a, err
}
