package botchatseat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
)

// fakeBot is the smallest BotChat a seat stage needs: one room, tasks, transitions, room
// messages, and the seat's reply scripted by the test.
type fakeBot struct {
	mu          sync.Mutex
	reply       string
	created     []map[string]any
	transitions []map[string]any
	posted      []map[string]any
}

func (f *fakeBot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/rooms":
		_ = json.NewEncoder(w).Encode(map[string]any{"rooms": []map[string]string{{"id": "room-seats", "name": Room}}})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/rooms":
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "room-seats"})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.created = append(f.created, in)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "bt-1"})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transition"):
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.transitions = append(f.transitions, in)
		_ = json.NewEncoder(w).Encode(map[string]string{"state": "ready"})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.posted = append(f.posted, in)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/messages"):
		msgs := []botchat.Message{{Seq: 1, From: botchat.Coordinator, Content: "TASK-x is yours"}}
		if f.reply != "" {
			msgs = append(msgs, botchat.Message{Seq: 2, From: "deepseek4", Content: f.reply})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": msgs})
	default:
		http.NotFound(w, r)
	}
}

func newSeat(t *testing.T, reply string) (*Adapter, *fakeBot) {
	t.Helper()
	f := &fakeBot{reply: reply}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Adapter{
		Settings: func(context.Context) (string, string) { return srv.URL, key },
		Poll:     time.Millisecond,
	}, f
}

// gitAt answers as a worktree on branch@sha that already holds any commit.
func gitAt(branch, sha string, missing string) func(context.Context, string, ...string) (string, error) {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		switch args[0] {
		case "rev-parse":
			if len(args) > 1 && args[1] == "--abbrev-ref" {
				return branch, nil
			}
			return sha, nil
		case "cat-file":
			if missing != "" && strings.Contains(args[2], missing) {
				return "", errors.New("missing")
			}
			return "", nil
		}
		return "", nil
	}
}

func seatRequest(t *testing.T, prompt string) runner.Request {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(p, []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}
	return runner.Request{
		Role: "worker", Model: "deepseek4",
		Backend:    runner.Backend{Name: "deepseek4", APIType: APIType},
		Workdir:    "/wt",
		PromptPath: p,
		Job:        runner.Job{Effort: 2, Stage: 17, Task: "T346"},
	}
}

func TestSeatStageBecomesTaskAndFetchesTheCommit(t *testing.T) {
	a, f := newSeat(t, "DONE TASK-T346 feature/af9@abcdef1234")
	a.Git = gitAt("feature/af9", "deadbeefcafe", "")

	res, err := a.Run(context.Background(), seatRequest(t, "Implement AF-9."))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Final, "DONE TASK-T346 feature/af9@abcdef1234") {
		t.Fatalf("final = %q", res.Final)
	}
	if len(f.created) != 1 {
		t.Fatalf("created %d tasks", len(f.created))
	}
	task := f.created[0]
	if task["assignee"] != "deepseek4" {
		t.Errorf("assignee = %v", task["assignee"])
	}
	obj, _ := task["objective"].(string)
	if !strings.Contains(obj, "Implement AF-9.") || !strings.Contains(obj, "feature/af9@deadbeefcafe") {
		t.Errorf("objective lacks the prompt or the branch: %q", obj)
	}
	if len(f.transitions) != 1 || f.transitions[0]["action"] != "promote" {
		t.Errorf("transitions = %v", f.transitions)
	}
	if len(f.posted) != 1 || f.posted[0]["to"] != "deepseek4" {
		t.Errorf("announcements = %v", f.posted)
	}
}

func TestSeatReviewBecomesAVerdict(t *testing.T) {
	for reply, want := range map[string]string{
		"REVIEW TASK-T346: PASS":              "pass",
		"REVIEW TASK-T346: CHANGES-REQUESTED": "repair",
	} {
		a, _ := newSeat(t, reply)
		res, err := a.Run(context.Background(), seatRequest(t, "Review it."))
		if err != nil {
			t.Fatalf("%s: %v", reply, err)
		}
		var out struct {
			Verdict string `json:"verdict"`
		}
		if err := json.Unmarshal([]byte(res.Final), &out); err != nil {
			t.Fatalf("%s: final is not JSON: %q", reply, res.Final)
		}
		if out.Verdict != want {
			t.Errorf("%s: verdict = %q, want %q", reply, out.Verdict, want)
		}
	}
}

func TestSeatSilenceIsATimeoutNotARetry(t *testing.T) {
	a, _ := newSeat(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res, err := a.Run(ctx, seatRequest(t, "Work."))
	var re *runner.Error
	if !errors.As(err, &re) || re.Class != runner.ErrTimeout {
		t.Fatalf("err = %v, want ErrTimeout", err)
	}
	if !res.Incomplete {
		t.Error("a silent seat should leave the result incomplete")
	}
}

func TestSeatReadyLimitRefusesAnotherTask(t *testing.T) {
	a, f := newSeat(t, "DONE TASK-T346 feature/af9@abcdef1234")
	a.ReadyLimit = 1
	a.pending = map[string]int{"deepseek4": 1}
	_, err := a.Run(context.Background(), seatRequest(t, "Work."))
	var re *runner.Error
	if !errors.As(err, &re) || re.Class != runner.ErrRateLimit {
		t.Fatalf("err = %v, want ErrRateLimit", err)
	}
	if len(f.created) != 0 {
		t.Errorf("created %d tasks over the limit", len(f.created))
	}
}

func TestSeatCommitThatNeverArrivesFailsTheStage(t *testing.T) {
	a, _ := newSeat(t, "DONE TASK-T346 feature/af9@abcdef1234")
	a.Git = gitAt("feature/af9", "deadbeefcafe", "abcdef1234")
	_, err := a.Run(context.Background(), seatRequest(t, "Work."))
	var re *runner.Error
	if !errors.As(err, &re) || re.Class != runner.ErrCrash {
		t.Fatalf("err = %v, want ErrCrash", err)
	}
}
