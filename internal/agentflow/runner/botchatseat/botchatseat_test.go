package botchatseat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
)

// fakeBot is the BotChat a seat stage needs: one room with a seq-numbered history, tasks with
// states, transitions and the task list. Tests script the seat by appending messages or
// moving a task.
type fakeBot struct {
	mu          sync.Mutex
	seq         int64
	msgs        []botchat.Message
	tasks       map[string]*botchat.TaskInfo
	order       []string
	created     []map[string]any
	transitions []map[string]any
	nextID      int
}

func (f *fakeBot) say(from, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.msgs = append(f.msgs, botchat.Message{Seq: f.seq, From: from, Content: content})
}

func (f *fakeBot) setState(id, state, blocker string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[id].State, f.tasks[id].Blocker = state, blocker
}

func (f *fakeBot) addTask(id, assignee, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tasks[id] = &botchat.TaskInfo{ID: id, Assignee: assignee, State: state, RoomID: "room-seats"}
	f.order = append(f.order, id)
}

func (f *fakeBot) announced() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs {
		if m.From == botchat.Coordinator {
			return true
		}
	}
	return false
}

func (f *fakeBot) nCreated() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

func (f *fakeBot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	enc := json.NewEncoder(w)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/rooms":
		_ = enc.Encode(map[string]any{"rooms": []map[string]string{{"id": "room-seats", "name": Room}}})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/tasks":
		var out []botchat.TaskInfo
		for _, id := range f.order {
			t := f.tasks[id]
			if t.Assignee == r.URL.Query().Get("assignee") {
				out = append(out, *t)
			}
		}
		_ = enc.Encode(map[string]any{"tasks": out})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/tasks":
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.created = append(f.created, in)
		f.nextID++
		id := "TASK-" + strconv.Itoa(400+f.nextID)
		f.tasks[id] = &botchat.TaskInfo{ID: id, Assignee: in["assignee"].(string), State: "assigned", RoomID: "room-seats"}
		f.order = append(f.order, id)
		_ = enc.Encode(map[string]string{"id": id})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/"):
		t, ok := f.tasks[strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_ = enc.Encode(t)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/transition"):
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		in["task"] = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/"), "/transition")
		f.transitions = append(f.transitions, in)
		if t, ok := f.tasks[in["task"].(string)]; ok {
			switch in["action"] {
			case "cancel":
				t.State = "cancelled"
			case "accept":
				t.State = "accepted"
			case "close":
				t.State = "closed"
			}
		}
		_ = enc.Encode(map[string]string{"state": "ready"})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.seq++
		f.msgs = append(f.msgs, botchat.Message{Seq: f.seq, From: in["from"], Content: in["content"]})
		w.WriteHeader(http.StatusCreated)
		_ = enc.Encode(map[string]any{"ok": true})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/messages"):
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var out []botchat.Message
		if r.URL.Query().Get("after") == "" { // the newest `limit`
			start := max(len(f.msgs)-limit, 0)
			out = append(out, f.msgs[start:]...)
		} else {
			for _, m := range f.msgs {
				if m.Seq > after && (limit <= 0 || len(out) < limit) {
					out = append(out, m)
				}
			}
		}
		_ = enc.Encode(map[string]any{"messages": out})
	default:
		http.NotFound(w, r)
	}
}

func newSeat(t *testing.T) (*Adapter, *fakeBot) {
	t.Helper()
	f := &fakeBot{tasks: map[string]*botchat.TaskInfo{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &Adapter{
		Settings: func(context.Context) (string, string) { return srv.URL, key },
		Poll:     2 * time.Millisecond,
		Git:      gitAt("af/t346", "deadbeefcafe"),
	}, f
}

func gitAt(branch, sha string) func(context.Context, string, ...string) (string, error) {
	return func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "rev-parse" {
			if len(args) > 1 && args[1] == "--abbrev-ref" {
				return branch, nil
			}
			return sha, nil
		}
		return "", nil
	}
}

func seatRequest(t *testing.T, role, prompt string) runner.Request {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "prompt.md")
	if err := os.WriteFile(p, []byte(prompt), 0o600); err != nil {
		t.Fatal(err)
	}
	return runner.Request{
		Role: role, Model: "deepseek4",
		Backend:    runner.Backend{Name: "deepseek4", APIType: APIType, Slots: 1},
		Workdir:    "/wt",
		StageDir:   dir,
		PromptPath: p,
		Job:        runner.Job{Effort: 2, Stage: 17, Task: "T346"},
	}
}

type outcome struct {
	res runner.Result
	err error
}

func runAsync(a *Adapter, ctx context.Context, req runner.Request) chan outcome {
	ch := make(chan outcome, 1)
	go func() {
		res, err := a.Run(ctx, req)
		ch <- outcome{res, err}
	}()
	return ch
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func get(t *testing.T, ch chan outcome) outcome {
	t.Helper()
	select {
	case o := <-ch:
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	return outcome{}
}

func TestWorkerDoneWithTheBotChatIDBecomesAWorkerReport(t *testing.T) {
	a, f := newSeat(t)
	ch := runAsync(a, context.Background(), seatRequest(t, "worker", "Implement AF-9."))
	waitFor(t, "the task", func() bool { return f.nCreated() == 1 })
	f.say("deepseek4", "DONE TASK-401")
	o := get(t, ch)
	if o.err != nil {
		t.Fatalf("Run: %v", o.err)
	}
	w, err := schema.Parse[schema.WorkerOut](json.RawMessage(o.res.Final))
	if err != nil {
		t.Fatalf("the final is not a worker report the engine accepts: %v: %s", err, o.res.Final)
	}
	if w.Blocked != "" {
		t.Errorf("blocked = %q", w.Blocked)
	}
	obj, _ := f.created[0]["objective"].(string)
	if !strings.Contains(obj, "Implement AF-9.") || !strings.Contains(obj, "Do not commit") || !strings.Contains(obj, "af/t346@deadbeefcafe") {
		t.Errorf("objective: %q", obj)
	}
	var actions []string
	for _, tr := range f.transitions {
		actions = append(actions, tr["action"].(string))
	}
	if strings.Join(actions, ",") != "promote,accept,close" {
		t.Errorf("transitions = %v, want promote, accept, close", actions)
	}
}

func TestReviewVerdictsAreValidReviews(t *testing.T) {
	for reply, want := range map[string]string{
		"REVIEW TASK-401: PASS": "pass",
		"REVIEW TASK-401: CHANGES-REQUESTED\nthe error is swallowed": "repair",
	} {
		a, f := newSeat(t)
		ch := runAsync(a, context.Background(), seatRequest(t, "reviewer", "Review it."))
		waitFor(t, "the task", func() bool { return f.nCreated() == 1 })
		f.say("deepseek4", reply)
		o := get(t, ch)
		if o.err != nil {
			t.Fatalf("%q: %v", reply, o.err)
		}
		r, err := schema.Parse[schema.ReviewOut](json.RawMessage(o.res.Final))
		if err != nil {
			t.Fatalf("%q: not a review the engine accepts: %v: %s", reply, err, o.res.Final)
		}
		if r.Verdict != want {
			t.Errorf("%q: verdict = %q, want %q", reply, r.Verdict, want)
		}
		if want == "repair" && !strings.Contains(r.RepairPrompt, "swallowed") {
			t.Errorf("repair prompt lost the seat's notes: %q", r.RepairPrompt)
		}
	}
}

// AF-9 review finding 2: only a report naming this stage's BotChat task, posted after the
// announcement, ends the stage.
func TestOldOrForeignReportsDoNotEndTheStage(t *testing.T) {
	a, f := newSeat(t)
	f.say("deepseek4", "DONE TASK-401") // history before this stage: same id a later task gets
	ctx, cancel := context.WithCancel(context.Background())
	req := seatRequest(t, "worker", "Work.")
	ch := runAsync(a, ctx, req)
	waitFor(t, "the task", func() bool { return f.nCreated() == 1 })
	f.say("deepseek4", "DONE TASK-T346 af/t346@abcdef1234") // the agentflow key is not the task id
	f.say("muse", "DONE TASK-401")                          // another callsign
	time.Sleep(30 * time.Millisecond)
	select {
	case o := <-ch:
		t.Fatalf("the stage ended on a report that is not its own: %+v", o)
	default:
	}
	cancel()
	o := get(t, ch)
	var re *runner.Error
	if !errors.As(o.err, &re) || re.Class != runner.ErrDetached {
		t.Fatalf("err = %v, want ErrDetached (the task stays with the seat)", o.err)
	}
}

// AF-9 review finding 1: silence is never a timeout; it is a notice for the owner.
func TestSilenceIsANoticeNotATimeout(t *testing.T) {
	a, f := newSeat(t)
	a.Board = &runner.SeatBoard{}
	req := seatRequest(t, "worker", "Work.")
	req.Budget = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the silence notice", func() bool {
		n, ok := a.Board.Get(17)
		return ok && n.Kind == runner.SeatSilent
	})
	select {
	case o := <-ch:
		t.Fatalf("a silent seat ended the stage: %+v", o)
	default:
	}
	f.say("deepseek4", "still on it")
	waitFor(t, "the notice to clear", func() bool { _, ok := a.Board.Get(17); return !ok })
	cancel()
	if o := get(t, ch); o.err == nil || errors.Is(o.err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", o.err)
	}
}

// AF-9 review finding 3: the limit is the backend's slots, counted on BotChat.
func TestSlotsCountTheSeatsOpenTasksOnBotChat(t *testing.T) {
	a, f := newSeat(t)
	f.addTask("TASK-300", "deepseek4", "working")
	f.addTask("TASK-301", "deepseek4", "closed")
	f.addTask("TASK-302", "muse", "working")
	_, err := a.Run(context.Background(), seatRequest(t, "worker", "Work."))
	var re *runner.Error
	if !errors.As(err, &re) || re.Class != runner.ErrRateLimit {
		t.Fatalf("err = %v, want ErrRateLimit at 1 open task with 1 slot", err)
	}
	if f.nCreated() != 0 {
		t.Fatal("created a task over the limit")
	}
	req := seatRequest(t, "worker", "Work.")
	req.Backend.Slots = 2
	ch := runAsync(a, context.Background(), req)
	waitFor(t, "the task", func() bool { return f.nCreated() == 1 })
	f.say("deepseek4", "DONE TASK-401")
	if o := get(t, ch); o.err != nil {
		t.Fatalf("with 2 slots: %v", o.err)
	}
}

// AF-9 review finding 4: the task's state is polled too.
func TestTaskStateIsPolled(t *testing.T) {
	t.Run("done ends a worker", func(t *testing.T) {
		a, f := newSeat(t)
		ch := runAsync(a, context.Background(), seatRequest(t, "worker", "Work."))
		waitFor(t, "the task", func() bool { return f.nCreated() == 1 })
		f.setState("TASK-401", "done", "")
		if o := get(t, ch); o.err != nil {
			t.Fatalf("Run: %v", o.err)
		}
	})
	t.Run("blocked asks the owner", func(t *testing.T) {
		a, f := newSeat(t)
		a.Board = &runner.SeatBoard{}
		ctx, cancel := context.WithCancel(context.Background())
		ch := runAsync(a, ctx, seatRequest(t, "worker", "Work."))
		waitFor(t, "the task", func() bool { return f.nCreated() == 1 })
		f.setState("TASK-401", "blocked", "which database?")
		waitFor(t, "the question", func() bool {
			n, ok := a.Board.Get(17)
			return ok && n.Kind == runner.SeatQuestion && strings.Contains(n.Message, "which database?")
		})
		cancel()
		get(t, ch)
	})
	t.Run("cancelled is blocked", func(t *testing.T) {
		a, f := newSeat(t)
		ch := runAsync(a, context.Background(), seatRequest(t, "worker", "Work."))
		waitFor(t, "the task", func() bool { return f.nCreated() == 1 })
		f.setState("TASK-401", "cancelled", "")
		o := get(t, ch)
		w, err := schema.Parse[schema.WorkerOut](json.RawMessage(o.res.Final))
		if err != nil || w.Blocked == "" {
			t.Fatalf("final = %s (%v), want a blocked worker report", o.res.Final, err)
		}
	})
}

// A restart adopts the open task instead of making another.
func TestARestartAdoptsTheTask(t *testing.T) {
	a, f := newSeat(t)
	req := seatRequest(t, "worker", "Work.")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the announcement", f.announced)
	cancel()
	if o := get(t, ch); !isDetached(o.err) {
		t.Fatalf("err = %v, want detached", o.err)
	}
	if !a.Alive(context.Background(), req.StageDir) {
		t.Fatal("the stage's task should be adoptable")
	}
	b := &Adapter{Settings: a.Settings, Poll: a.Poll, Git: a.Git} // the next process
	ch = runAsync(b, context.Background(), req)
	time.Sleep(20 * time.Millisecond)
	f.say("deepseek4", "DONE TASK-401")
	if o := get(t, ch); o.err != nil {
		t.Fatalf("adopted Run: %v", o.err)
	}
	if f.nCreated() != 1 {
		t.Fatalf("created %d tasks; the restart should adopt the first", f.nCreated())
	}
}

func TestCancelEndsTheCallAsCancelled(t *testing.T) {
	a, f := newSeat(t)
	req := seatRequest(t, "worker", "Work.")
	ctx, cancel := context.WithCancel(context.Background())
	ch := runAsync(a, ctx, req)
	waitFor(t, "the announcement", f.announced)
	if err := a.Cancel(context.Background(), 17, req.StageDir); err != nil {
		t.Fatal(err)
	}
	cancel()
	o := get(t, ch)
	if isDetached(o.err) || !errors.Is(o.err, context.Canceled) {
		t.Fatalf("err = %v, want a plain cancel", o.err)
	}
	if f.tasks["TASK-401"].State != "cancelled" {
		t.Errorf("BotChat task state = %s", f.tasks["TASK-401"].State)
	}
}

func isDetached(err error) bool {
	var re *runner.Error
	return errors.As(err, &re) && re.Class == runner.ErrDetached
}
