package coord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
)

// Task is a BotChat task as the coordinator reads it.
type Task struct {
	ID                 string   `json:"id"`
	Num                int      `json:"num"`
	Title              string   `json:"title"`
	Objective          string   `json:"objective"`
	AcceptanceCriteria string   `json:"acceptanceCriteria"`
	Exclusions         string   `json:"exclusions"`
	State              string   `json:"state"`
	Assignee           string   `json:"assignee"`
	ClaimedBy          string   `json:"claimedBy"`
	PlanID             string   `json:"planId"`
	RoomID             string   `json:"roomId"`
	Capabilities       []string `json:"capabilities"`
	Creator            string   `json:"creator"`
}

// Msg is a BotChat message.
type Msg struct {
	ID       string `json:"id"`
	Seq      int64  `json:"seq"`
	From     string `json:"from"`
	Callsign string `json:"callsign"`
	Content  string `json:"content"`
}

// Sender is the callsign that posted the message.
func (m Msg) Sender() string {
	if m.Callsign != "" {
		return m.Callsign
	}
	return m.From
}

// Bus is the part of BotChat the coordinator uses. Reads go to the service; every write goes
// through Transition, CreateTask, Promote or Post, which a dry run records instead.
type Bus interface {
	Tasks(ctx context.Context) ([]Task, error)
	Task(ctx context.Context, id string) (Task, error)
	Messages(ctx context.Context, room string) ([]Msg, error)
	Transition(ctx context.Context, id, from, action, note string) error
	CreateTask(ctx context.Context, fields map[string]any) (string, error)
	Post(ctx context.Context, room, from, to, content string) error
}

// CursorBus is the optional read API that avoids downloading a room's whole
// history each pass. The coordinator stores each room's newest seq and asks
// only for messages after it (AF-15).
type CursorBus interface {
	MessagesAfter(ctx context.Context, room string, after int64, limit int) ([]Msg, error)
}

// PlannedBus is the optional task API that reads only the coordinator's own
// plans, rather than every task on the board.
type PlannedBus interface {
	TasksForPlans(ctx context.Context, plans []string) ([]Task, error)
}

// PullBus is the optional queue API an idle seat uses to claim its next task
// itself (AF-15 item 9). A plan's claim is atomic.
type PullBus interface {
	PlanEligible(ctx context.Context, plan, callsign string, capabilities []string) ([]Task, error)
	ClaimNext(ctx context.Context, plan, callsign string, capabilities []string) (Task, bool, error)
}

// HTTPBus is the real BotChat.
type HTTPBus struct{ C *botchat.Client }

func (b HTTPBus) Tasks(ctx context.Context) ([]Task, error) {
	var out struct {
		Tasks []Task `json:"tasks"`
	}
	err := b.C.Do(ctx, http.MethodGet, "/api/v1/tasks", nil, &out)
	return out.Tasks, err
}

func (b HTTPBus) Task(ctx context.Context, id string) (Task, error) {
	var raw json.RawMessage
	if err := b.C.Do(ctx, http.MethodGet, "/api/v1/tasks/"+id, nil, &raw); err != nil {
		return Task{}, err
	}
	var wrap struct {
		Task *Task `json:"task"`
	}
	if json.Unmarshal(raw, &wrap) == nil && wrap.Task != nil {
		return *wrap.Task, nil
	}
	var t Task
	return t, json.Unmarshal(raw, &t)
}

func (b HTTPBus) TasksForPlans(ctx context.Context, plans []string) ([]Task, error) {
	seen := map[string]bool{}
	var out []Task
	for _, id := range plans {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ts, err := b.C.PlanTasks(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			out = append(out, Task{ID: t.ID, Num: t.Num, Title: t.Title, Objective: t.Objective, AcceptanceCriteria: t.AcceptanceCriteria,
				Exclusions: t.Exclusions, State: t.State, Assignee: t.Assignee, ClaimedBy: t.ClaimedBy, PlanID: t.PlanID,
				RoomID: t.RoomID, Capabilities: t.Capabilities, Creator: t.Creator})
		}
	}
	return out, nil
}

func taskFromInfo(t botchat.TaskInfo) Task {
	return Task{ID: t.ID, Num: t.Num, Title: t.Title, Objective: t.Objective, AcceptanceCriteria: t.AcceptanceCriteria,
		Exclusions: t.Exclusions, State: t.State, Assignee: t.Assignee, ClaimedBy: t.ClaimedBy, PlanID: t.PlanID,
		RoomID: t.RoomID, Capabilities: t.Capabilities, Creator: t.Creator}
}

func (b HTTPBus) PlanEligible(ctx context.Context, plan, callsign string, capabilities []string) ([]Task, error) {
	ts, err := b.C.Eligible(ctx, plan, callsign, capabilities)
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(ts))
	for _, t := range ts {
		out = append(out, taskFromInfo(t))
	}
	return out, nil
}

func (b HTTPBus) ClaimNext(ctx context.Context, plan, callsign string, capabilities []string) (Task, bool, error) {
	t, err := b.C.Claim(ctx, plan, callsign, capabilities)
	if errors.Is(err, botchat.ErrNotFound) {
		return Task{}, false, nil
	}
	if err != nil {
		return Task{}, false, err
	}
	if t.ID == "" {
		return Task{}, false, nil
	}
	return taskFromInfo(t), true, nil
}

func (b HTTPBus) MessagesAfter(ctx context.Context, room string, after int64, limit int) ([]Msg, error) {
	ms, err := b.C.MessagesAfter(ctx, room, after, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Msg, 0, len(ms))
	for _, m := range ms {
		out = append(out, Msg{Seq: m.Seq, From: m.From, Callsign: m.Callsign, Content: m.Content})
	}
	return out, nil
}

func (b HTTPBus) Messages(ctx context.Context, room string) ([]Msg, error) {
	var raw json.RawMessage
	if err := b.C.Do(ctx, http.MethodGet, "/api/v1/rooms/"+room+"/messages?limit="+strconv.Itoa(200), nil, &raw); err != nil {
		return nil, err
	}
	var msgs []Msg
	if json.Unmarshal(raw, &msgs) == nil {
		return msgs, nil
	}
	var wrap struct {
		Messages []Msg `json:"messages"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("coord: room %s messages: %w", room, err)
	}
	return wrap.Messages, nil
}

func (b HTTPBus) Transition(ctx context.Context, id, from, action, note string) error {
	_, err := b.C.Transition(ctx, id, from, action, map[string]any{"note": note})
	return err
}

func (b HTTPBus) CreateTask(ctx context.Context, fields map[string]any) (string, error) {
	return b.C.CreateTask(ctx, fields)
}

func (b HTTPBus) Post(ctx context.Context, room, from, to, content string) error {
	return b.C.Post(ctx, room, from, to, content)
}

// DryBus reads from Bus and records every write instead of sending it.
type DryBus struct {
	Bus Bus

	mu     sync.Mutex
	writes []string
	n      int
}

func (d *DryBus) record(s string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.writes = append(d.writes, s)
}

// Writes are the writes a dry run would have sent, in order.
func (d *DryBus) Writes() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.writes...)
}

func (d *DryBus) Tasks(ctx context.Context) ([]Task, error)         { return d.Bus.Tasks(ctx) }
func (d *DryBus) Task(ctx context.Context, id string) (Task, error) { return d.Bus.Task(ctx, id) }
func (d *DryBus) Messages(ctx context.Context, room string) ([]Msg, error) {
	return d.Bus.Messages(ctx, room)
}

func (d *DryBus) TasksForPlans(ctx context.Context, plans []string) ([]Task, error) {
	if p, ok := d.Bus.(PlannedBus); ok {
		return p.TasksForPlans(ctx, plans)
	}
	ts, err := d.Bus.Tasks(ctx)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range plans {
		want[id] = true
	}
	out := ts[:0]
	for _, t := range ts {
		if want[t.PlanID] {
			out = append(out, t)
		}
	}
	return out, nil
}

func (d *DryBus) MessagesAfter(ctx context.Context, room string, after int64, limit int) ([]Msg, error) {
	if c, ok := d.Bus.(CursorBus); ok {
		return c.MessagesAfter(ctx, room, after, limit)
	}
	ms, err := d.Bus.Messages(ctx, room)
	if err != nil {
		return nil, err
	}
	out := ms[:0]
	for _, m := range ms {
		if m.Seq > after {
			out = append(out, m)
		}
	}
	return out, nil
}

func (d *DryBus) PlanEligible(ctx context.Context, plan, callsign string, capabilities []string) ([]Task, error) {
	if p, ok := d.Bus.(PullBus); ok {
		return p.PlanEligible(ctx, plan, callsign, capabilities)
	}
	return nil, nil
}

func (d *DryBus) ClaimNext(ctx context.Context, plan, callsign string, capabilities []string) (Task, bool, error) {
	if p, ok := d.Bus.(PullBus); ok {
		t, ok, err := p.ClaimNext(ctx, plan, callsign, capabilities)
		if ok {
			d.record(fmt.Sprintf("claim next in %s as %s", plan, callsign))
		}
		return t, ok, err
	}
	return Task{}, false, nil
}

func (d *DryBus) Transition(_ context.Context, id, from, action, _ string) error {
	d.record(fmt.Sprintf("transition %s %s as %s", id, action, from))
	return nil
}

func (d *DryBus) CreateTask(_ context.Context, f map[string]any) (string, error) {
	d.mu.Lock()
	d.n++
	id := fmt.Sprintf("DRY-%d", d.n)
	d.mu.Unlock()
	d.record(fmt.Sprintf("create task %s %q for %v", id, f["title"], f["assignee"]))
	return id, nil
}

func (d *DryBus) Post(_ context.Context, room, from, to, content string) error {
	if len(content) > 120 {
		content = content[:120] + "..."
	}
	d.record(fmt.Sprintf("post in %s as %s to %s: %s", room, from, to, content))
	return nil
}
