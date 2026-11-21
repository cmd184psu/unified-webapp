package coord

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeBus struct {
	tasks  []Task
	rooms  map[string][]Msg
	writes []string
}

func (f *fakeBus) Tasks(context.Context) ([]Task, error) { return f.tasks, nil }
func (f *fakeBus) Task(_ context.Context, id string) (Task, error) {
	for _, t := range f.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return Task{}, os.ErrNotExist
}
func (f *fakeBus) Messages(_ context.Context, room string) ([]Msg, error) { return f.rooms[room], nil }
func (f *fakeBus) Transition(_ context.Context, id, from, action, _ string) error {
	f.writes = append(f.writes, "transition "+id+" "+action+" as "+from)
	return nil
}
func (f *fakeBus) CreateTask(_ context.Context, m map[string]any) (string, error) {
	f.writes = append(f.writes, "create "+m["title"].(string)+" for "+m["assignee"].(string))
	return "TASK-900", nil
}
func (f *fakeBus) Post(_ context.Context, room, from, to, content string) error {
	f.writes = append(f.writes, "post to "+to)
	return nil
}

type fakeGit struct{ merged []string }

func (g *fakeGit) RemoteHas(_ context.Context, r Repo, b string) bool { return r.Name == "web" }

func (g *fakeGit) Deliver(_ context.Context, _ Repo, _, _ string) error { return nil }
func (g *fakeGit) Merged(context.Context, Repo, string, string) bool    { return false }
func (g *fakeGit) Merge(_ context.Context, r Repo, base, b, tid string, _ bool) error {
	g.merged = append(g.merged, r.Name+":"+b+"->"+base)
	return nil
}
func (g *fakeGit) Contains(_ context.Context, sha string) []string {
	if sha == "6de6155" {
		return []string{"origin/feature/hold"}
	}
	return nil
}

const (
	engineRoom = "room_engine"
	reviewRoom = "room_review"
)

func testConfig(t *testing.T) *Config {
	dir := t.TempDir()
	return &Config{BotChatURL: "x", KeyFile: "x", Me: "coord-fs3", ReviewPlan: "plan_rev", ReviewRoom: reviewRoom, EngineRoom: engineRoom,
		Rooms: []string{engineRoom}, OurPlans: []string{"plan_eng"}, Reviewers: []string{"fb-beam", "deepseek4"},
		Family: map[string]string{"fb-beam": "beam", "deepseek4": "deepseek"}, Authors: []string{"fb-beam", "deepseek4"},
		Base: "feature/diplomacy-screen", Repos: []Repo{{Name: "web", Path: "/nonexistent"}, {Name: "cs", Path: "/nonexistent2"}},
		Seats:    []Seat{{Callsign: "deepseek4", Session: "deepseek4", Feed: true, Nudge: true}, {Callsign: "fb-beam", Session: "freebuff-beam", Feed: true, Nudge: true}},
		QueueDir: filepath.Join(dir, "queues"), StateFile: filepath.Join(dir, "state.json")}
}

func writeState(t *testing.T, path, js string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
}

func kinds(r *Report) string {
	var k []string
	for _, d := range r.Decisions {
		k = append(k, d.Kind+":"+d.Task)
	}
	return strings.Join(k, " ")
}

// One pass over a board like 11-20's: a PASS review whose author wrote "branch at sha" (bug i),
// a CHANGES-REQUESTED review, a new delivery that needs a reviewer of another family, and a
// seat whose fed task is finished, whose seat_order is empty, and whose plan has a next task
// (bug ii).
func TestARunRecordsReviewsMergesAndFeeds(t *testing.T) {
	cfg := testConfig(t)
	writeState(t, cfg.StateFile, `{"dev":{"TASK-343":{"status":"done","review":"TASK-501"},"TASK-342":{"status":"done","review":"TASK-502"}},"seat_order":{"deepseek4":[]},"fed":{"deepseek4":"TASK-444"},"fed_seen":{"deepseek4":[]},"fed_empty":{},"feed_skip":[],"af_delivered":{},"pane":{"x":1}}`)
	bus := &fakeBus{
		tasks: []Task{
			{ID: "TASK-501", Num: 501, Title: "Review TASK-343 orders", PlanID: "plan_rev", State: "done", Assignee: "fb-beam"},
			{ID: "TASK-343", Num: 343, Title: "orders", PlanID: "plan_eng", State: "done", Assignee: "deepseek4", ClaimedBy: "deepseek4"},
			{ID: "TASK-502", Num: 502, Title: "Review TASK-342 economy", PlanID: "plan_rev", State: "done", Assignee: "fb-beam"},
			{ID: "TASK-342", Num: 342, Title: "economy", PlanID: "plan_eng", State: "done", Assignee: "deepseek4", ClaimedBy: "deepseek4"},
			{ID: "TASK-358", Num: 358, Title: "pillage web", PlanID: "plan_eng", State: "done", Assignee: "deepseek4"},
			{ID: "TASK-444", Num: 444, Title: "current", PlanID: "plan_eng", State: "acknowledged", Assignee: "deepseek4"},
			{ID: "TASK-445", Num: 445, Title: "next in the plan", PlanID: "plan_eng", State: "draft", Assignee: "deepseek4"},
		},
		rooms: map[string][]Msg{
			reviewRoom: {
				{ID: "m1", Callsign: "fb-beam", Content: "REVIEW TASK-343: PASS\n1. fine"},
				{ID: "m2", Callsign: "fb-beam", Content: "REVIEW TASK-342: CHANGES-REQUESTED\n1. broken"},
			},
			engineRoom: {
				{ID: "m3", Callsign: "deepseek4", Content: "DONE TASK-343: branch feature/eng-orders at 1a2b3c4d, gates green"},
				{ID: "m4", Callsign: "deepseek4", Content: "DONE TASK-444 feature/current@abcdef1 done"},
			},
		},
	}
	git := &fakeGit{}
	c := &Coordinator{Cfg: cfg, Bus: bus, Git: git, Now: func() time.Time { return time.Unix(0, 0) }}
	rep := c.Run(context.Background(), false)
	if rep.Error != "" {
		t.Fatal(rep.Error)
	}
	want := "review_recorded:TASK-501 rework:TASK-342 review_recorded:TASK-502 merge:TASK-343 accept:TASK-343 review_created:TASK-358 finished:TASK-444 fed:TASK-445 alert:"
	if got := kinds(rep); got != want {
		t.Fatalf("decisions\n got %s\nwant %s", got, want)
	}
	if strings.Join(git.merged, ",") != "web:feature/eng-orders->feature/diplomacy-screen" {
		t.Fatalf("merged %v", git.merged)
	}
	w := strings.Join(bus.writes, "\n")
	for _, s := range []string{"transition TASK-342 rework as coord-fs3", "create Review TASK-358 pillage web for fb-beam",
		"transition TASK-444 start as deepseek4", "transition TASK-444 done as deepseek4", "transition TASK-445 promote as coord-fs3"} {
		if !strings.Contains(w, s) {
			t.Errorf("writes lack %q:\n%s", s, w)
		}
	}
	if strings.Contains(w, "TASK-444 claim") {
		t.Errorf("an acknowledged task is not claimed again (bug iii):\n%s", w)
	}
	q, err := os.ReadFile(filepath.Join(cfg.QueueDir, "deepseek4.md"))
	if err != nil || !strings.Contains(string(q), "## CURRENT TASK: TASK-445  next in the plan") {
		t.Fatalf("queue file: %v\n%s", err, q)
	}
	st, _ := LoadState(cfg.StateFile)
	if get(st.Dev["TASK-343"], "status") != "merged" || get(st.Dev["TASK-342"], "status") != "rework" || st.Fed["deepseek4"] != "TASK-445" {
		t.Fatalf("state %+v", st.Dev)
	}
	b, _ := os.ReadFile(cfg.StateFile)
	if !strings.Contains(string(b), `"pane"`) {
		t.Fatal("a key the port does not use must be kept")
	}
}

// A dry run decides the same and writes nothing: no BotChat write, no merge, no state, no queue.
func TestADryRunWritesNothing(t *testing.T) {
	cfg := testConfig(t)
	writeState(t, cfg.StateFile, `{"dev":{"TASK-343":{"status":"done","pass":"TASK-501"}},"fed":{}}`)
	bus := &fakeBus{tasks: []Task{{ID: "TASK-343", Num: 343, Title: "orders", PlanID: "plan_eng", State: "done", Assignee: "deepseek4"}},
		rooms: map[string][]Msg{engineRoom: {{ID: "m3", Callsign: "deepseek4", Content: "DONE TASK-343 pushed as commit 6de6155"}}}}
	git := &fakeGit{}
	c := &Coordinator{Cfg: cfg, Bus: bus, Git: git}
	before, _ := os.ReadFile(cfg.StateFile)
	rep := c.Run(context.Background(), true)
	if !strings.Contains(kinds(rep), "merge:TASK-343") || len(rep.Writes) == 0 {
		t.Fatalf("dry decisions %s writes %v", kinds(rep), rep.Writes)
	}
	if len(bus.writes) != 0 || len(git.merged) != 0 {
		t.Fatalf("a dry run wrote %v merged %v", bus.writes, git.merged)
	}
	after, _ := os.ReadFile(cfg.StateFile)
	if string(after) != string(before) {
		t.Fatal("a dry run changed state.json")
	}
	if _, err := os.Stat(cfg.QueueDir); err == nil {
		if es, _ := os.ReadDir(cfg.QueueDir); len(es) > 0 {
			t.Fatal("a dry run wrote queue files")
		}
	}
}

func TestFinishActions(t *testing.T) {
	for st, want := range map[string]string{"ready": "claim,start,done", "assigned": "claim,start,done", "acknowledged": "start,done", "working": "done", "done": ""} {
		if got := strings.Join(finishActions(st), ","); got != want {
			t.Errorf("%s: %s, want %s", st, got, want)
		}
	}
}
