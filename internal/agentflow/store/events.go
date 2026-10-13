package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Event is one audit row: who (actor) did what (action) to which subject.
type Event struct {
	ID          int64
	TS          time.Time
	Actor       string
	Action      string
	SubjectType string
	SubjectID   string
	DetailJSON  string
}

// AppendEvent writes an audit row inside the caller's transaction, so the
// event commits or rolls back with the state change it records. detail is
// marshalled to JSON (nil becomes "{}"). actor, action and subject_type are
// required.
func AppendEvent(tx *sql.Tx, actor, action, subjectType, subjectID string, detail any) (int64, error) {
	switch {
	case actor == "":
		return 0, Invalid("actor", "required")
	case action == "":
		return 0, Invalid("action", "required")
	case subjectType == "":
		return 0, Invalid("subject_type", "required")
	}
	detailJSON := "{}"
	if detail != nil {
		b, err := json.Marshal(detail)
		if err != nil {
			return 0, Invalid("detail", fmt.Sprintf("not JSON-encodable: %v", err))
		}
		detailJSON = string(b)
	}
	res, err := tx.Exec(`
		INSERT INTO events (ts, actor, action, subject_type, subject_id, detail_json)
		VALUES (?, ?, ?, ?, ?, ?)`,
		time.Now().UnixMilli(), actor, action, subjectType, subjectID, detailJSON)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListEvents returns events oldest first. A non-empty subjectType filters on
// it (and on subjectID too when that is non-empty); limit <= 0 means 100.
func (s *Store) ListEvents(ctx context.Context, subjectType, subjectID string, limit int) ([]Event, error) {
	if limit <= 0 {
		limit = 100
	}
	q := `SELECT id, ts, actor, action, subject_type, subject_id, detail_json FROM events`
	var args []any
	if subjectType != "" {
		q += ` WHERE subject_type = ?`
		args = append(args, subjectType)
		if subjectID != "" {
			q += ` AND subject_id = ?`
			args = append(args, subjectID)
		}
	}
	q += ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var ts int64
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.Action, &e.SubjectType, &e.SubjectID, &e.DetailJSON); err != nil {
			return nil, err
		}
		e.TS = time.UnixMilli(ts)
		out = append(out, e)
	}
	return out, rows.Err()
}
