package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

// harness is a real taskmaster engine plus a real agentflow store, both in
// temp dirs, with the fake runner. reopen simulates a restart on the same DBs.
type harness struct {
	t      *testing.T
	tmCfg  config.TaskmasterConfig
	afPath string
	st     *store.Store
	tm     *taskmaster.Engine
	srv    *httptest.Server
	fake   *runner.Fake
	eng    *Engine
	widthZ bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	static := t.TempDir()
	if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("index"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &harness{
		t: t,
		tmCfg: config.TaskmasterConfig{
			StaticDir: static, DBPath: filepath.Join(t.TempDir(), "taskmaster.db"), ProgressIntervalMs: 50,
		},
		afPath: filepath.Join(t.TempDir(), "agentflow.db"),
		fake:   runner.NewFake().Fallback(runner.Step{Reply: "done"}),
	}
	st, err := store.Open(h.afPath)
	if err != nil {
		t.Fatal(err)
	}
	h.st = st
	t.Cleanup(func() { h.closeAll() })
	h.openTM()
	return h
}

func (h *harness) openTM() {
	h.t.Helper()
	tm, err := taskmaster.Open(h.tmCfg, taskmaster.OpenOptions{})
	if err != nil {
		h.t.Fatal(err)
	}
	h.tm = tm
	h.srv = httptest.NewServer(tm.Handler())
}

func (h *harness) closeTM() {
	if h.srv != nil {
		h.srv.Close()
		h.srv = nil
	}
	if h.tm != nil {
		if err := h.tm.Close(); err != nil {
			h.t.Fatal(err)
		}
		h.tm = nil
	}
}

func (h *harness) closeAll() {
	h.closeTM()
	if h.st != nil {
		h.st.Close()
		h.st = nil
	}
}

// start builds the engine over the current taskmaster and store.
func (h *harness) start() {
	h.t.Helper()
	e, err := New(Deps{Store: h.st, Host: h.tm, Runner: h.fake, DataDir: h.t.TempDir(), TestWidthZero: h.widthZ})
	if err != nil {
		h.t.Fatal(err)
	}
	h.eng = e
}

// restart closes taskmaster and re-opens it and the store on the same files.
func (h *harness) restart() {
	h.t.Helper()
	h.closeAll()
	st, err := store.Open(h.afPath)
	if err != nil {
		h.t.Fatal(err)
	}
	h.st = st
	h.openTM()
	h.start()
}

func (h *harness) backend(name string, slots int) store.Backend {
	return store.Backend{Name: name, Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: slots,
		Models: []store.BackendModel{{Model: "m1"}}}
}

func (h *harness) lane(name string) golane.Lane {
	h.t.Helper()
	l, ok := h.eng.Lane(name)
	if !ok {
		h.t.Fatalf("no handle for lane %q", name)
	}
	return l
}

func (h *harness) settings(name string) golane.LaneSettings {
	h.t.Helper()
	s, err := h.lane(name).Settings()
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

// stageFor creates a queued worker stage on a backend and returns a payload.
func (h *harness) stageFor(backend string) StagePayload {
	h.t.Helper()
	ctx := context.Background()
	repo := h.t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	slug := "p" + backend
	if err := h.st.CreateProject(ctx, "t", store.Project{Slug: slug, RepoPath: repo, ModelScope: "any"}); err != nil {
		h.t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		h.t.Fatal(err)
	}
	sg, err := h.st.CreateStage(ctx, store.StageSpec{Effort: ef.ID, Role: "planner", Backend: backend, Model: "m1"})
	if err != nil {
		h.t.Fatal(err)
	}
	return StagePayload{Project: slug, Effort: ef.ID, Stage: sg.ID}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) waitStatus(l golane.Lane, id, want string) golane.Job {
	h.t.Helper()
	var j golane.Job
	waitFor(h.t, "job "+id+" to be "+want, func() bool {
		var err error
		if j, err = l.Get(id); err != nil {
			h.t.Fatal(err)
		}
		return j.Status == want
	})
	return j
}

type apiLane struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Paused bool   `json:"paused"`
}

// apiLanes reads taskmaster's own lane list (hidden lanes are not in it).
func (h *harness) apiLanes() map[string]apiLane {
	h.t.Helper()
	resp, err := http.Get(h.srv.URL + "/api/lanes")
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var ls []apiLane
	if err := json.NewDecoder(resp.Body).Decode(&ls); err != nil {
		h.t.Fatal(err)
	}
	out := map[string]apiLane{}
	for _, l := range ls {
		out[l.Name] = l
	}
	return out
}

func (h *harness) call(method, path string) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("%s %s: %d", method, path, resp.StatusCode)
	}
}

func (h *harness) mustAdd(name string, slots int) {
	h.t.Helper()
	if err := h.eng.AddBackend(context.Background(), "t", h.backend(name, slots)); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) mustResume() {
	h.t.Helper()
	if err := h.eng.Resume(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) submit(backend string) (golane.Lane, string) {
	h.t.Helper()
	l := h.lane(BackendLane(backend))
	job, err := l.Submit(KindPlan, "stage", h.stageFor(backend))
	if err != nil {
		h.t.Fatal(err)
	}
	return l, job.ID
}

// AC-TMX1 (agentflow half): two backends produce two lanes that share
// agentflow.work, and a job on each runs.
func TestTMX1_TwoBackendsShareKinds(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.mustAdd("alpha", 1)
	h.mustAdd("beta", 1)
	h.mustResume()

	lanes := h.apiLanes()
	for _, n := range []string{"agentflow-alpha", "agentflow-beta"} {
		if _, ok := lanes[n]; !ok {
			t.Fatalf("lane %s missing from taskmaster lane list: %v", n, lanes)
		}
	}
	for _, b := range []string{"alpha", "beta"} {
		l, id := h.submit(b)
		h.waitStatus(l, id, "success")
	}
	if got := len(h.fake.Calls()); got != 2 {
		t.Fatalf("runner calls = %d, want 2", got)
	}
	// Each shared kind is accepted by both lanes.
	for _, n := range []string{"agentflow-alpha", "agentflow-beta"} {
		if _, err := h.lane(n).Submit(KindWork, "x", StagePayload{Stage: 1}); err != nil {
			t.Fatalf("%s rejects %s: %v", n, KindWork, err)
		}
	}
}

// AC-BK1 (engine): a backend added at runtime has a lane in the taskmaster
// list and runs a submitted stage, width 1, no restart.
func TestBK1_AddBackendAtRuntimeRunsStage(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.mustResume()
	if _, ok := h.apiLanes()["agentflow-late"]; ok {
		t.Fatal("lane exists before the backend was added")
	}
	h.mustAdd("late", 1)
	if lane, ok := h.apiLanes()["agentflow-late"]; !ok || lane.Width != 1 || lane.Paused {
		t.Fatalf("lane after add = %+v (present %v), want width 1, running", lane, ok)
	}
	l, id := h.submit("late")
	h.waitStatus(l, id, "success")
	calls := h.fake.Calls()
	if len(calls) != 1 || calls[0].Request.Backend.Name != "late" || calls[0].Request.Model != "m1" {
		t.Fatalf("runner calls = %+v", calls)
	}
}

// BK3: a slots change moves the lane width.
func TestBK3_SlotsChangeUpdatesWidth(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.mustAdd("a", 1)
	h.mustResume()
	if w := h.settings("agentflow-a").Width; w != 1 {
		t.Fatalf("width = %d, want 1", w)
	}
	if err := h.eng.UpdateBackend(context.Background(), "t", h.backend("a", 3)); err != nil {
		t.Fatal(err)
	}
	if w := h.settings("agentflow-a").Width; w != 3 {
		t.Fatalf("width after update = %d, want 3", w)
	}
	if w := h.apiLanes()["agentflow-a"].Width; w != 3 {
		t.Fatalf("taskmaster lane list width = %d, want 3", w)
	}
}

// BK3: retire leaves the lane paused and hidden with its handle kept;
// re-enable reverses it.
func TestBK3_RetireAndReenable(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.mustAdd("a", 2)
	h.mustResume()
	if err := h.eng.RetireBackend(context.Background(), "t", "a"); err != nil {
		t.Fatal(err)
	}
	s := h.settings("agentflow-a")
	if !s.Paused || !s.Hidden {
		t.Fatalf("retired lane = %+v, want paused and hidden", s)
	}
	if _, ok := h.apiLanes()["agentflow-a"]; ok {
		t.Fatal("retired lane still listed by taskmaster")
	}
	if err := h.eng.EnableBackend(context.Background(), "t", "a"); err != nil {
		t.Fatal(err)
	}
	s = h.settings("agentflow-a")
	if s.Paused || s.Hidden || s.Width != 2 {
		t.Fatalf("re-enabled lane = %+v, want running, visible, width 2", s)
	}
	l, id := h.submit("a")
	h.waitStatus(l, id, "success")
}

// BK2, TM3: after a restart on the same DBs retired backends stay retired and
// widths follow the agentflow DB's slots, not the lane's stored width.
func TestBK2_RestartKeepsRetiredAndSyncsWidth(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.mustAdd("live", 1)
	h.mustAdd("old", 2)
	h.mustResume()
	if err := h.eng.RetireBackend(context.Background(), "t", "old"); err != nil {
		t.Fatal(err)
	}
	// Slots are edited in the agentflow DB while the lane still says 1.
	if err := h.st.UpdateBackend(context.Background(), "t", h.backend("live", 4)); err != nil {
		t.Fatal(err)
	}
	h.restart()

	if s := h.settings("agentflow-live"); s.Width != 4 || !s.Paused || s.PausedBy != pausedByOwner {
		t.Fatalf("live after restart = %+v, want width 4, held paused by agentflow", s)
	}
	if s := h.settings("agentflow-old"); !s.Paused || !s.Hidden {
		t.Fatalf("retired after restart = %+v, want paused and hidden", s)
	}
	h.mustResume()
	if s := h.settings("agentflow-live"); s.Paused {
		t.Fatalf("live lane still paused after Resume: %+v", s)
	}
	if s := h.settings("agentflow-old"); !s.Paused || !s.Hidden {
		t.Fatalf("retired lane after Resume = %+v, want still paused and hidden", s)
	}
}

// RC4: every agentflow lane is paused before anything can run; a job queued
// meanwhile starts only after Resume.
func TestRC4_LanesPausedUntilResume(t *testing.T) {
	h := newHarness(t)
	if err := h.st.CreateBackend(context.Background(), "t", h.backend("a", 1)); err != nil {
		t.Fatal(err)
	}
	h.start()
	for _, n := range []string{"agentflow-a", VerifyLane, MergeLane} {
		if s := h.settings(n); !s.Paused || s.PausedBy != pausedByOwner {
			t.Fatalf("%s at start = %+v, want paused by agentflow", n, s)
		}
	}
	l, id := h.submit("a")
	time.Sleep(300 * time.Millisecond)
	if j, _ := l.Get(id); j.Status != "queued" {
		t.Fatalf("job status while held = %q, want queued", j.Status)
	}
	h.mustResume()
	h.waitStatus(l, id, "success")
}

// S6: a pause someone else set is not lifted; and with the brake engaged at
// boot, nothing is unpaused and the brake stays engaged.
func TestPauseRules_ForeignPauseAndBrake(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.mustAdd("a", 1)
	h.mustAdd("b", 1)
	h.mustResume()

	h.call(http.MethodPost, "/api/lanes/agentflow-b/pause")
	h.mustResume()
	if s := h.settings("agentflow-b"); !s.Paused || s.PausedBy != "api" {
		t.Fatalf("foreign pause was touched: %+v", s)
	}
	h.call(http.MethodPost, "/api/lanes/agentflow-b/resume")

	h.call(http.MethodPost, "/api/brake")
	h.restart()
	for _, n := range []string{"agentflow-a", "agentflow-b", VerifyLane, MergeLane} {
		s := h.settings(n)
		if !s.BrakeEngaged || !s.Paused || s.PausedBy != "brake" {
			t.Fatalf("%s after boot under brake = %+v, want engaged and brake-paused", n, s)
		}
	}
	h.mustResume()
	for _, n := range []string{"agentflow-a", "agentflow-b", VerifyLane, MergeLane} {
		s := h.settings(n)
		if !s.BrakeEngaged || !s.Paused || s.PausedBy != "brake" {
			t.Fatalf("%s after Resume under brake = %+v, want unchanged", n, s)
		}
	}
	h.call(http.MethodDelete, "/api/brake")
	if s := h.settings("agentflow-a"); s.Paused {
		t.Fatalf("lane still paused after brake release: %+v", s)
	}
}

// TM3, TM1: engine lanes exist under non-colliding names; merge is width 1
// and verify follows the verify_slots setting.
func TestEngineLanes(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.mustResume()
	if s := h.settings(MergeLane); s.Width != 1 {
		t.Fatalf("merge width = %d, want 1", s.Width)
	}
	sn, err := h.st.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s := h.settings(VerifyLane); s.Width != sn.Settings.VerifySlots {
		t.Fatalf("verify width = %d, want %d", s.Width, sn.Settings.VerifySlots)
	}
	if err := h.st.UpdateSettings(context.Background(), "t", map[string]string{"verify_slots": "3"}); err != nil {
		t.Fatal(err)
	}
	if err := h.eng.SyncLanes(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := h.settings(VerifyLane); s.Width != 3 {
		t.Fatalf("verify width after setting change = %d, want 3", s.Width)
	}
	l := h.lane(MergeLane)
	if _, err := l.Submit(KindMerge, "m", StagePayload{Stage: 1}); err != nil {
		t.Fatalf("merge lane rejects %s: %v", KindMerge, err)
	}
	if _, err := l.Submit(KindWork, "m", StagePayload{Stage: 1}); err == nil {
		t.Fatal("merge lane accepted a backend kind")
	}
}

// TestWidthZero: lanes registered for tests never run anything.
func TestWidthZeroKeepsLanesIdle(t *testing.T) {
	h := newHarness(t)
	h.widthZ = true
	h.start()
	h.mustAdd("a", 2)
	h.mustResume()
	if w := h.settings("agentflow-a").Width; w != 0 {
		t.Fatalf("width = %d, want 0", w)
	}
}

// TM1: lanes.go owns no worker loop.
func TestLanesGoHasNoGoroutineLoop(t *testing.T) {
	src, err := os.ReadFile("lanes.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"go func", "for {"} {
		if strings.Contains(string(src), bad) {
			t.Fatalf("lanes.go contains %q", bad)
		}
	}
}
