package botchat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// bot is a small BotChat: rooms, messages with sequence numbers, inboxes, callsign leases.
type bot struct {
	mu      sync.Mutex
	rooms   map[string]string
	msgs    []map[string]any
	leases  map[string]bool
	acks    []int64
	deaf    bool // when set the inbox never shows anything (nobody can pick a job up)
	nextSeq int64
}

func newBot() *bot { return &bot{rooms: map[string]string{}, leases: map[string]bool{}, nextSeq: 100} }

func (b *bot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == "GET" && path == "/api/v1/rooms":
		var rooms []map[string]string
		for n, id := range b.rooms {
			rooms = append(rooms, map[string]string{"id": id, "name": n})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": rooms})
	case r.Method == "POST" && path == "/api/v1/rooms":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		b.rooms[in["name"]] = "room-" + in["name"]
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "room-" + in["name"]})
	case r.Method == "POST" && strings.HasSuffix(path, "/messages"):
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		b.nextSeq++
		in["seq"], in["callsign"] = b.nextSeq, in["from"]
		b.msgs = append(b.msgs, in)
		w.WriteHeader(http.StatusCreated)
	case r.Method == "GET" && strings.HasSuffix(path, "/messages"):
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": b.msgs})
	case r.Method == "POST" && path == "/api/v1/callsign":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		b.leases[in["callsign"]] = true
		_ = json.NewEncoder(w).Encode(map[string]string{"callsign": in["callsign"], "instanceId": "inst-1"})
	case r.Method == "DELETE" && path == "/api/v1/callsign":
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		delete(b.leases, in["callsign"])
		w.WriteHeader(http.StatusNoContent)
	case r.Method == "GET" && path == "/api/v1/inbox":
		who := r.URL.Query().Get("callsign")
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		out := []map[string]any{}
		cursor := b.nextSeq
		if !b.deaf {
			for _, m := range b.msgs {
				if m["to"] == who && m["seq"].(int64) > after {
					out = append(out, m)
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cursor": cursor, "messages": out})
	case r.Method == "POST" && path == "/api/v1/inbox/ack":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		b.acks = append(b.acks, int64(in["seq"].(float64)))
		_ = json.NewEncoder(w).Encode(map[string]any{"ackSeq": in["seq"]})
	case r.Method == "POST" && path == "/api/v1/heartbeat":
		_ = json.NewEncoder(w).Encode(map[string]any{"presence": "online"})
	default:
		http.NotFound(w, r)
	}
}

func (b *bot) said(from, containing string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.msgs {
		if m["from"] == from && strings.Contains(m["content"].(string), containing) {
			return true
		}
	}
	return false
}

// inner is the model runner the seat calls: it records the prompt it was given.
type inner struct {
	mu      sync.Mutex
	prompts []string
	err     error
}

func (i *inner) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	b, _ := os.ReadFile(req.PromptPath)
	i.mu.Lock()
	i.prompts = append(i.prompts, string(b))
	i.mu.Unlock()
	return runner.Result{Final: "done"}, i.err
}
func (i *inner) Version(context.Context) (string, error) { return "x", nil }

func setup(t *testing.T, mode string, b *bot) (*Dispatcher, *inner, runner.Request, string) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "af.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(key, []byte("k"), 0o600)
	if err := s.UpdateSettings(context.Background(), "t", map[string]string{"botchat_url": srv.URL, "botchat_key_file": key, "botchat_mode": mode}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	prompt := filepath.Join(dir, "prompt.md")
	_ = os.WriteFile(prompt, []byte("DO THE TASK"), 0o600)
	in := &inner{}
	d := &Dispatcher{Inner: in, Store: s, Relay: &Relay{Store: s}, Poll: 20 * time.Millisecond, PickupTimeout: 600 * time.Millisecond, IdleStop: 250 * time.Millisecond}
	t.Cleanup(d.Close)
	req := runner.Request{Role: "worker", Backend: runner.Backend{Name: "grok"}, Model: "grok-4.7", StageDir: dir, PromptPath: prompt,
		Job: runner.Job{Effort: 7, Stage: 42, Task: "T004", Attempt: 2}}
	return d, in, req, dir
}

func TestDispatchPitchesPicksUpReadsTheRoomAndReports(t *testing.T) {
	b := newBot()
	d, in, req, dir := setup(t, "dispatch", b)
	// A message already in the room for everyone: the seat must read it into the prompt.
	b.rooms["agentflow-effort-7"] = "room-agentflow-effort-7"
	b.msgs = append(b.msgs, map[string]any{"from": "human", "callsign": "human", "to": "everyone", "content": "use the stdlib only", "seq": int64(50)})
	res, err := d.Run(context.Background(), req)
	if err != nil || res.Final != "done" {
		t.Fatalf("res %+v err %v", res, err)
	}
	seat := SeatCallsign("worker", "grok", "grok-4.7")
	if seat != "af-worker-grok-grok-4-7" {
		t.Fatalf("seat = %q", seat)
	}
	if !b.said(Coordinator, "[job:42]") || !b.said(seat, "picked up") || !b.said(seat, "[job:42] finished") {
		t.Fatalf("the pitch, the pickup and the report must all be on BotChat: %v", b.msgs)
	}
	if len(in.prompts) != 1 || !strings.Contains(in.prompts[0], "use the stdlib only") || !strings.HasSuffix(in.prompts[0], "DO THE TASK") {
		t.Fatalf("prompt = %q", in.prompts)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompt.botchat.md")); err != nil {
		t.Fatalf("the prompt with the room read into it is kept for inspection: %v", err)
	}
	b.mu.Lock()
	acked := len(b.acks) > 0
	b.mu.Unlock()
	if !acked {
		t.Fatal("the seat must ack the inbox")
	}
	// The seat stands down when its queue has been empty for a while, and gives its callsign back.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		held := b.leases[seat]
		b.mu.Unlock()
		if !held {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("an idle seat must release its callsign")
}

func TestDispatchFallsBackToADirectRunWhenNobodyAnswers(t *testing.T) {
	b := newBot()
	b.deaf = true
	d, in, req, _ := setup(t, "dispatch", b)
	res, err := d.Run(context.Background(), req)
	if err != nil || res.Final != "done" || len(in.prompts) != 1 || in.prompts[0] != "DO THE TASK" {
		t.Fatalf("a job nobody picks up runs directly with its own prompt: res %+v err %v prompts %q", res, err, in.prompts)
	}
}

func TestMirrorModeAndMissingJobRunDirectly(t *testing.T) {
	b := newBot()
	d, in, req, _ := setup(t, "mirror", b)
	if _, err := d.Run(context.Background(), req); err != nil || len(in.prompts) != 1 || in.prompts[0] != "DO THE TASK" || len(b.msgs) != 0 {
		t.Fatalf("mirror mode must not touch the call: prompts %q msgs %v err %v", in.prompts, b.msgs, err)
	}
	d2, in2, req2, _ := setup(t, "dispatch", newBot())
	req2.Job = runner.Job{} // a call that belongs to no stage (a model check)
	if _, err := d2.Run(context.Background(), req2); err != nil || len(in2.prompts) != 1 {
		t.Fatalf("a call with no stage runs directly: %v", err)
	}
}
