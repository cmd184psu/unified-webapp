package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SeatTask is one item in the seat task backlog (AF-21).
type SeatTask struct {
	ID            int64    `json:"id"`
	Title         string   `json:"title"`
	Objective     string   `json:"objective"`
	Acceptance    string   `json:"acceptance"`
	Exclusions    string   `json:"exclusions"`
	EligibleSeats []string `json:"eligible_seats"`
	EligibleKinds []string `json:"eligible_kinds"`
	Priority      int      `json:"priority"`
	State         string   `json:"state"`
	Created       int64    `json:"created"`
	Updated       int64    `json:"updated"`
}

func marshalStrings(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func unmarshalStrings(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func normalizeSeatTask(t *SeatTask) error {
	t.Title = strings.TrimSpace(t.Title)
	if t.Title == "" {
		return Invalid("title", "title is required")
	}
	if t.State == "" {
		t.State = "ready"
	}
	switch t.State {
	case "ready", "fed", "done":
	default:
		return Invalid("state", "state must be ready, fed, or done")
	}
	if t.EligibleSeats == nil {
		t.EligibleSeats = []string{}
	}
	if t.EligibleKinds == nil {
		t.EligibleKinds = []string{}
	}
	return nil
}

const seatTaskCols = `id, title, objective, acceptance, exclusions, eligible_seats, eligible_kinds, priority, state, created, updated`

func scanSeatTask(scan func(dest ...any) error) (SeatTask, error) {
	var t SeatTask
	var seats, kinds string
	if err := scan(&t.ID, &t.Title, &t.Objective, &t.Acceptance, &t.Exclusions, &seats, &kinds, &t.Priority, &t.State, &t.Created, &t.Updated); err != nil {
		return SeatTask{}, err
	}
	t.EligibleSeats = unmarshalStrings(seats)
	t.EligibleKinds = unmarshalStrings(kinds)
	return t, nil
}

// CreateSeatTask inserts a new seat task. An empty title is rejected.
func (s *Store) CreateSeatTask(ctx context.Context, t SeatTask) (SeatTask, error) {
	if err := normalizeSeatTask(&t); err != nil {
		return SeatTask{}, err
	}
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `INSERT INTO seat_tasks (title, objective, acceptance, exclusions, eligible_seats, eligible_kinds, priority, state, created, updated) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Title, t.Objective, t.Acceptance, t.Exclusions, marshalStrings(t.EligibleSeats), marshalStrings(t.EligibleKinds), t.Priority, t.State, now, now)
	if err != nil {
		return SeatTask{}, err
	}
	id, _ := res.LastInsertId()
	t.ID = id
	t.Created = now
	t.Updated = now
	return t, nil
}

// ListSeatTasks returns seat tasks, highest priority first.
// state and seat are optional filters; seat matches eligible_seats exactly.
func (s *Store) ListSeatTasks(ctx context.Context, state, seat string) ([]SeatTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+seatTaskCols+` FROM seat_tasks ORDER BY priority DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeatTask
	for rows.Next() {
		t, err := scanSeatTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		if state != "" && t.State != state {
			continue
		}
		if seat != "" && !containsString(t.EligibleSeats, seat) {
			continue
		}
		out = append(out, t)
	}
	if out == nil {
		out = []SeatTask{}
	}
	return out, rows.Err()
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// GetSeatTask returns one seat task. An unknown id is ErrNotFound.
func (s *Store) GetSeatTask(ctx context.Context, id int64) (SeatTask, error) {
	t, err := scanSeatTask(s.db.QueryRowContext(ctx, `SELECT `+seatTaskCols+` FROM seat_tasks WHERE id = ?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return SeatTask{}, fmt.Errorf("seat task %d: %w", id, ErrNotFound)
	}
	return t, err
}

// UpdateSeatTask replaces the editable fields of an existing seat task.
func (s *Store) UpdateSeatTask(ctx context.Context, t SeatTask) (SeatTask, error) {
	if _, err := s.GetSeatTask(ctx, t.ID); err != nil {
		return SeatTask{}, err
	}
	if err := normalizeSeatTask(&t); err != nil {
		return SeatTask{}, err
	}
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx, `UPDATE seat_tasks SET title = ?, objective = ?, acceptance = ?, exclusions = ?, eligible_seats = ?, eligible_kinds = ?, priority = ?, state = ?, updated = ? WHERE id = ?`,
		t.Title, t.Objective, t.Acceptance, t.Exclusions, marshalStrings(t.EligibleSeats), marshalStrings(t.EligibleKinds), t.Priority, t.State, now, t.ID)
	if err != nil {
		return SeatTask{}, err
	}
	return s.GetSeatTask(ctx, t.ID)
}

// DeleteSeatTask removes a seat task. An unknown id is ErrNotFound.
func (s *Store) DeleteSeatTask(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM seat_tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("seat task %d: %w", id, ErrNotFound)
	}
	return nil
}

// ImportSeatTasks inserts many seat tasks from imported data.
func (s *Store) ImportSeatTasks(ctx context.Context, tasks []SeatTask) (int, error) {
	n := 0
	for _, t := range tasks {
		if _, err := s.CreateSeatTask(ctx, t); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
