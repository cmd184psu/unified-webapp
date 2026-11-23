package store

import (
	"context"
	"encoding/json"
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

// CreateSeatTask inserts a new seat task.
func (s *Store) CreateSeatTask(ctx context.Context, t SeatTask) (SeatTask, error) {
	now := time.Now().Unix()
	if t.State == "" {
		t.State = "ready"
	}
	if t.EligibleSeats == nil {
		t.EligibleSeats = []string{}
	}
	if t.EligibleKinds == nil {
		t.EligibleKinds = []string{}
	}
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

// ListSeatTasks returns all seat tasks.
func (s *Store) ListSeatTasks(ctx context.Context) ([]SeatTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, objective, acceptance, exclusions, eligible_seats, eligible_kinds, priority, state, created, updated FROM seat_tasks ORDER BY priority DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SeatTask
	for rows.Next() {
		var t SeatTask
		var seats, kinds string
		if err := rows.Scan(&t.ID, &t.Title, &t.Objective, &t.Acceptance, &t.Exclusions, &seats, &kinds, &t.Priority, &t.State, &t.Created, &t.Updated); err != nil {
			return nil, err
		}
		t.EligibleSeats = unmarshalStrings(seats)
		t.EligibleKinds = unmarshalStrings(kinds)
		out = append(out, t)
	}
	if out == nil {
		out = []SeatTask{}
	}
	return out, rows.Err()
}

// GetSeatTask returns one seat task.
func (s *Store) GetSeatTask(ctx context.Context, id int64) (SeatTask, error) {
	var t SeatTask
	var seats, kinds string
	err := s.db.QueryRowContext(ctx, `SELECT id, title, objective, acceptance, exclusions, eligible_seats, eligible_kinds, priority, state, created, updated FROM seat_tasks WHERE id = ?`, id).Scan(&t.ID, &t.Title, &t.Objective, &t.Acceptance, &t.Exclusions, &seats, &kinds, &t.Priority, &t.State, &t.Created, &t.Updated)
	if err != nil {
		return SeatTask{}, err
	}
	t.EligibleSeats = unmarshalStrings(seats)
	t.EligibleKinds = unmarshalStrings(kinds)
	return t, nil
}

// UpdateSeatTaskState updates a seat task's state.
func (s *Store) UpdateSeatTaskState(ctx context.Context, id int64, state string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE seat_tasks SET state = ?, updated = ? WHERE id = ?`, state, time.Now().Unix(), id)
	return err
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
