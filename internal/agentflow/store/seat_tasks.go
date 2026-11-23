package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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
	FedTo         string   `json:"fed_to"`
	FedAt         int64    `json:"fed_at"`
	DoneAt        int64    `json:"done_at"`
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

const seatTaskCols = `id, title, objective, acceptance, exclusions, eligible_seats, eligible_kinds, priority, state, fed_to, fed_at, done_at, created, updated`

func scanSeatTask(scan func(dest ...any) error) (SeatTask, error) {
	var t SeatTask
	var seats, kinds string
	if err := scan(&t.ID, &t.Title, &t.Objective, &t.Acceptance, &t.Exclusions, &seats, &kinds, &t.Priority, &t.State, &t.FedTo, &t.FedAt, &t.DoneAt, &t.Created, &t.Updated); err != nil {
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

// MarkFed records that callsign is working this ready task.
// A missing id is ErrNotFound. A task that is not ready is rejected.
func (s *Store) MarkFed(ctx context.Context, id int64, callsign string, now int64) (SeatTask, error) {
	callsign = strings.TrimSpace(callsign)
	if callsign == "" {
		return SeatTask{}, Invalid("callsign", "callsign is required")
	}
	if _, err := s.GetSeatTask(ctx, id); err != nil {
		return SeatTask{}, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE seat_tasks SET state = 'fed', fed_to = ?, fed_at = ?, updated = ? WHERE id = ? AND state = 'ready'`, callsign, now, now, id)
	if err != nil {
		return SeatTask{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return SeatTask{}, err
	}
	if n == 0 {
		return SeatTask{}, Invalid("state", "task is not ready")
	}
	return s.GetSeatTask(ctx, id)
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

// backlogItem is one queues/backlog/*.json file.
type backlogItem struct {
	Title              string          `json:"title"`
	Objective          string          `json:"objective"`
	AcceptanceCriteria string          `json:"acceptanceCriteria"`
	Exclusions         string          `json:"exclusions"`
	Seats              []string        `json:"seats"`
	Kinds              []string        `json:"kinds"`
	Priority           json.RawMessage `json:"priority"`
	Status             string          `json:"status"`
}

// backlogPriority reads a number, a numeric string, or the word "first" (1).
// Anything else, including a missing field, is 0.
func backlogPriority(raw json.RawMessage) int {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return int(f)
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0
	}
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "first") {
		return 1
	}
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	return 0
}

// backlogState maps READY and PROPOSED to ready. Every other status is done.
func backlogState(status string) string {
	s := strings.ToUpper(strings.TrimSpace(status))
	if s == "READY" || s == "PROPOSED" || strings.HasPrefix(s, "READY ") || strings.HasPrefix(s, "PROPOSED ") {
		return "ready"
	}
	return "done"
}

// ImportSeatTasksDir reads every *.json file in dir (not subdirectories).
// seats becomes eligible_seats. READY and PROPOSED become ready; any other
// status becomes done. A title already stored, or repeated in this directory,
// is skipped. A second call imports nothing.
func (s *Store) ImportSeatTasksDir(ctx context.Context, dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, Invalid("dir", "directory not found")
		}
		return 0, err
	}
	existing, err := s.ListSeatTasks(ctx, "", "")
	if err != nil {
		return 0, err
	}
	have := map[string]bool{}
	for _, t := range existing {
		have[t.Title] = true
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	n := 0
	for _, name := range names {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return n, err
		}
		var item backlogItem
		if err := json.Unmarshal(b, &item); err != nil {
			return n, fmt.Errorf("%s: %w", name, err)
		}
		title := strings.TrimSpace(item.Title)
		if title == "" || have[title] {
			continue
		}
		if _, err := s.CreateSeatTask(ctx, SeatTask{
			Title:         title,
			Objective:     item.Objective,
			Acceptance:    item.AcceptanceCriteria,
			Exclusions:    item.Exclusions,
			EligibleSeats: item.Seats,
			EligibleKinds: item.Kinds,
			Priority:      backlogPriority(item.Priority),
			State:         backlogState(item.Status),
		}); err != nil {
			return n, err
		}
		have[title] = true
		n++
	}
	return n, nil
}
