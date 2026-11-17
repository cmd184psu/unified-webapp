package botchat

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

type fake struct {
	mu    sync.Mutex
	rooms map[string]string // name -> id
	posts []map[string]string
	auth  string
}

func (f *fake) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = r.Header.Get("Authorization")
	switch {
	case r.Method == "GET" && r.URL.Path == "/api/v1/rooms":
		var rooms []map[string]string
		for n, id := range f.rooms {
			rooms = append(rooms, map[string]string{"id": id, "name": n})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": rooms})
	case r.Method == "POST" && r.URL.Path == "/api/v1/rooms":
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		id := "room-" + b["name"]
		f.rooms[b["name"]] = id
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/messages"):
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		b["room"] = strings.Split(r.URL.Path, "/")[4]
		f.posts = append(f.posts, b)
		w.WriteHeader(http.StatusCreated)
	default:
		http.NotFound(w, r)
	}
}

func event(t *testing.T, s *store.Store, action, typ, id, detail string) {
	t.Helper()
	err := s.WithTx(context.Background(), func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, "t", action, typ, id, json.RawMessage(detail))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRelayStaysOffUntilConfiguredThenPostsNewEventsOnly(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "af.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	f := &fake{rooms: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer srv.Close()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("sekret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	event(t, s, "run.start", "project", "p1", `{"effort":7,"state":"running"}`) // before the relay is on: never posted
	r := &Relay{Store: s}
	if err := r.Step(ctx); err != nil || len(f.posts) != 0 {
		t.Fatalf("off relay: err %v posts %v", err, f.posts)
	}
	if err := s.UpdateSettings(ctx, "t", map[string]string{"botchat_url": srv.URL + "/", "botchat_key_file": keyFile}); err != nil {
		t.Fatal(err)
	}
	if err := r.Step(ctx); err != nil || len(f.posts) != 0 {
		t.Fatalf("first step must start at the present, not replay history: err %v posts %v", err, f.posts)
	}
	event(t, s, "queue.approve", "effort", "7", `{"hash":"abc"}`)
	event(t, s, "backend.update", "backend", "x", `{}`) // not worth telling
	event(t, s, "stall.detected", "effort", "7", `{"minutes":10}`)
	if err := r.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.posts) != 2 || f.posts[0]["content"] != "queue approved" || !strings.Contains(f.posts[1]["content"], "stalled") {
		t.Fatalf("posts = %v", f.posts)
	}
	if f.posts[0]["from"] != Coordinator || f.posts[0]["to"] != "everyone" || f.posts[0]["room"] != "room-agentflow-effort-7" || f.auth != "Bearer sekret" {
		t.Fatalf("post = %v auth %q", f.posts[0], f.auth)
	}
	if err := r.Step(ctx); err != nil || len(f.posts) != 2 {
		t.Fatalf("nothing new must post nothing: err %v posts %d", err, len(f.posts))
	}
	// A new relay (a restart) carries on from the stored cursor.
	if err := (&Relay{Store: s}).Step(ctx); err != nil || len(f.posts) != 2 {
		t.Fatalf("after restart: err %v posts %d", err, len(f.posts))
	}
}

func TestMessageFormats(t *testing.T) {
	s := store.FinishedStage{ID: 5, Effort: 23, Task: "T004", Attempt: 2, Role: "worker", Backend: "opencode-free", Model: "big-pickle", Outcome: "infra:timeout", Tokens: 12500, Started: 0, Finished: 0}
	if got := StageCallsign(s); got != "af-23-worker-T004" {
		t.Fatalf("callsign = %q", got)
	}
	if got := StageText(s); got != "T004 attempt 2 · worker big-pickle (opencode-free): infra:timeout · 12.5k tokens" {
		t.Fatalf("text = %q", got)
	}
	v := store.FinishedStage{Effort: 3, Role: "verify", Outcome: "repair", Started: 1000, Finished: 1000 + 90000}
	if got := StageText(v); got != "verify: repair · 1.5 min" {
		t.Fatalf("text = %q", got)
	}
	if _, _, ok := EventText(store.RelayEvent{Action: "doc.save", SubjectType: "effort", SubjectID: "1"}); ok {
		t.Fatal("doc.save is not worth telling")
	}
	ef, txt, ok := EventText(store.RelayEvent{Action: "plan.halted", SubjectType: "effort", SubjectID: "10", DetailJSON: `{"reason":"5 failures"}`})
	if !ok || ef != 10 || txt != "planning halted: 5 failures" {
		t.Fatalf("%d %q %v", ef, txt, ok)
	}
}
