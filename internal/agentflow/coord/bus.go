package coord

import (
	"context"
	"encoding/json"
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
