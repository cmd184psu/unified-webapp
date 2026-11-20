package engine

import (
	"context"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/gitsvc"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/runner"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

const seatAPI = "freebuff_seat"

// fakeSeats stands in for the seat runners and their terminal sessions: a session per stage
// that outlives the engine (it lives in the test, like a tmux server outlives agentflow).
type fakeSeats struct {
	mu      sync.Mutex
	alive   map[int64]bool
	killed  map[int64]bool
	swept   []int64
	board   runner.SeatBoard
	calls   []runner.Request
	release map[int64]chan string // a reply sent here ends the stage's call
}

func newFakeSeats() *fakeSeats {
	return &fakeSeats{alive: map[int64]bool{}, killed: map[int64]bool{}, release: map[int64]chan string{}}
}

func (f *fakeSeats) ch(stage int64) chan string {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.release[stage]
	if !ok {
		c = make(chan string, 1)
		f.release[stage] = c
	}
	return c
}

// Run is the seat runner: it waits for the test to release the stage, and when its call is
// stopped it detaches unless the session was killed.
func (f *fakeSeats) Run(ctx context.Context, req runner.Request) (runner.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.alive[req.Job.Stage] = true
	f.mu.Unlock()
	if _, has := ctx.Deadline(); has {
		return runner.Result{}, &runner.Error{Class: runner.ErrCrash, Msg: "a seat call must have no deadline"}
	}
	select {
	case reply := <-f.ch(req.Job.Stage):
		f.mu.Lock()
		delete(f.alive, req.Job.Stage)
		f.mu.Unlock()
		return runner.Result{Final: reply}, nil
	case <-ctx.Done():
		f.mu.Lock()
		killed := f.killed[req.Job.Stage]
		f.mu.Unlock()
		if killed {
			return runner.Result{}, ctx.Err()
		}
		return runner.Result{Incomplete: true}, &runner.Error{Class: runner.ErrDetached, Msg: "keeps running", Cause: ctx.Err()}
	}
}

func (f *fakeSeats) Version(context.Context) (string, error) { return "fake seats", nil }

func (f *fakeSeats) IsSeat(t string) bool { return t == seatAPI }
func (f *fakeSeats) Alive(_ context.Context, _ string, stage int64, _ string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[stage]
}
func (f *fakeSeats) Cancel(_ context.Context, _ string, stage int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed[stage] = true
	delete(f.alive, stage)
	return nil
}
func (f *fakeSeats) Answer(context.Context, string, int64, string, string) error { return nil }
func (f *fakeSeats) Attach(_ context.Context, _ string, stage int64, _ string) string {
	return "tmux attach -t af-test-" + strconv.FormatInt(stage, 10)
}
func (f *fakeSeats) Sweep(_ context.Context, open func(int64) bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id := range f.alive {
		if !open(id) {
			delete(f.alive, id)
			f.swept = append(f.swept, id)
		}
	}
	return nil
}
func (f *fakeSeats) Notices() []runner.SeatNotice { return f.board.List() }

func (f *fakeSeats) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeSeats) lastCall() runner.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// newSeatChain is newChainRun with a seat backend "fb" as the only worker.
func newSeatChain(t *testing.T, seats *fakeSeats) *chainRun {
	t.Helper()
	worker.SetPollIntervalForTest(20 * time.Millisecond)
	t.Cleanup(func() { worker.SetPollIntervalForTest(5 * time.Second) })
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "README"), "readme\n")
	gitT(t, repo, "add", "-A")
	gitT(t, repo, "commit", "-q", "-m", "initial")
	h := newHarness(t)
	dataDir := t.TempDir()
	git, err := gitsvc.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	h.tweak = func(d *Deps) {
		d.Git, d.DataDir = git, dataDir
		d.Runner = &runner.Mux{Default: h.fake, ByType: map[string]runner.Runner{seatAPI: seats}}
		d.Seats = seats
	}
	h.start()
	t.Cleanup(func() {
		if h.eng != nil {
			h.eng.Close()
		}
	})
	ctx := context.Background()
	if err := h.eng.AddBackend(ctx, "t", store.Backend{Name: "fb", APIType: seatAPI, Slots: 1, Models: []store.BackendModel{{Model: "freebuff"}}}); err != nil {
		t.Fatal(err)
	}
	h.addBackend("gpu", 2, false, false, "m2")
	h.candidates("global", "", "worker", "fb/freebuff")
	h.candidates("global", "", "reviewer", "gpu/m2")
	slug := "proj"
	if err := h.st.CreateProject(ctx, "t", store.Project{Slug: slug, RepoPath: repo, ModelScope: "any"}); err != nil {
		t.Fatal(err)
	}
	ef, err := h.st.CreateEffort(ctx, "t", slug, "e", false)
	if err != nil {
		t.Fatal(err)
	}
	q, err := h.st.SaveQueue(ctx, "t", ef.ID, chainDefs()[:1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}
	tasks, err := h.st.ListTasks(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	return &chainRun{harness: h, repo: repo, slug: slug, effort: ef.ID, tasks: tasks}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c *chainRun) openSeatStage() model.Stage {
	c.t.Helper()
	open, err := c.st.OpenStages(context.Background())
	if err != nil {
		c.t.Fatal(err)
	}
	for _, st := range open {
		if st.Backend == "fb" {
			return st
		}
	}
	return model.Stage{}
}

// A seat call has no deadline and gets the role timeout as its budget; a seat that goes
// silent is an attention item for the owner and is never given a new attempt.
func TestSeatTimeoutIsAttentionNotARetry(t *testing.T) {
	seats := newFakeSeats()
	c := newSeatChain(t, seats)
	c.run()
	waitUntil(t, "the seat call", func() bool { return seats.callCount() == 1 })
	req := seats.lastCall()
	set, _ := c.st.EffectiveSettings(context.Background(), c.slug)
	if req.Budget != set.WorkerTimeout || req.Budget == 0 {
		t.Fatalf("budget = %s, want the worker timeout %s", req.Budget, set.WorkerTimeout)
	}
	if req.Job.Project != c.slug || req.Job.Task == "" {
		t.Fatalf("job = %+v", req.Job)
	}
	st := c.openSeatStage()
	seats.board.Set(runner.SeatNotice{Stage: st.ID, Kind: runner.SeatSilent, Message: "silent", Since: time.Now(), Attach: "tmux attach -t x"})
	items, err := c.eng.Attention(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range items {
		if it.Kind == runner.SeatSilent && it.Stage == st.ID && it.Task == c.tasks[0].Key && it.Project == c.slug {
			found = true
		}
	}
	if !found {
		t.Fatalf("no seat_silent attention item: %+v", items)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(c.attempts(0)); n != 1 {
		t.Fatalf("attempts = %d; a silent seat must not get another", n)
	}
	if seats.callCount() != 1 {
		t.Fatalf("calls = %d", seats.callCount())
	}
	list, err := c.eng.SeatStages(context.Background())
	if err != nil || len(list) != 1 || list[0].Attach == "" || list[0].Waiting == nil {
		t.Fatalf("seat stages = %+v (%v)", list, err)
	}
	if n, _ := c.eng.OpenStagesExceptSeats(context.Background()); n != 0 {
		t.Fatalf("a restart would wait for %d stages; seat stages do not count", n)
	}
	seats.ch(st.ID) <- workerOK
	waitUntil(t, "the stage outcome", func() bool {
		cur, _ := c.st.GetStage(context.Background(), st.ID)
		return cur.State == model.StageDone
	})
}

// A restart leaves the seat working: no outcome is written, the next process adopts the
// stage by giving it a new job, and the seat's report then moves the chain on.
func TestSeatStageSurvivesARestart(t *testing.T) {
	seats := newFakeSeats()
	c := newSeatChain(t, seats)
	c.run()
	waitUntil(t, "the seat call", func() bool { return seats.callCount() == 1 })
	st := c.openSeatStage()
	if st.ID == 0 {
		t.Fatal("no open seat stage")
	}
	c.eng.Close()
	c.restart()
	cur, err := c.st.GetStage(context.Background(), st.ID)
	if err != nil || cur.State == model.StageDone {
		t.Fatalf("the seat stage was closed by the restart: %+v %v", cur, err)
	}
	if err := c.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.eng.Runs().Start(context.Background(), "t", c.slug); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the adopted call", func() bool { return seats.callCount() == 2 })
	if got := seats.lastCall().Job.Stage; got != st.ID {
		t.Fatalf("adopted stage %d, want %d", got, st.ID)
	}
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	seats.ch(st.ID) <- workerOK
	waitUntil(t, "the worker outcome", func() bool {
		cur, _ := c.st.GetStage(context.Background(), st.ID)
		return cur.State == model.StageDone && cur.Outcome == OutcomePass
	})
	if n := len(c.attempts(0)); n != 1 {
		t.Fatalf("attempts = %d; the restart must not start another", n)
	}
	if _, ok := c.event("stage", st.ID, "stage.seat_adopted"); !ok {
		t.Error("no stage.seat_adopted event")
	}
}

// After a crash restart every project boots stopped while an adopted seat keeps working. A
// result it reports then is kept and applied once the project runs again; the attempt is not
// abandoned.
func TestASeatResultWhileStoppedIsKeptForTheResume(t *testing.T) {
	seats := newFakeSeats()
	c := newSeatChain(t, seats)
	c.run()
	waitUntil(t, "the seat call", func() bool { return seats.callCount() == 1 })
	st := c.openSeatStage()
	if st.ID == 0 {
		t.Fatal("no open seat stage")
	}
	c.eng.Close() // a crash: no drain, no pause
	c.restart()
	if err := c.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := c.eng.Runs().State(c.slug); got != model.RunStopped {
		t.Fatalf("the project boots %s, want stopped", got)
	}
	waitUntil(t, "the adopted call", func() bool { return seats.callCount() == 2 })
	seats.ch(st.ID) <- workerOK
	waitUntil(t, "the worker outcome", func() bool {
		cur, _ := c.st.GetStage(context.Background(), st.ID)
		return cur.State == model.StageDone && cur.Outcome == OutcomePass
	})
	for i := 0; i < 3; i++ {
		if err := c.eng.Advance(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	atts := c.attempts(0)
	if len(atts) != 1 || atts[0].Outcome != "" {
		t.Fatalf("attempts %+v: the seat's result must be kept while the project is stopped", atts)
	}
	c.fake.Script("reviewer", runner.Step{Reply: reviewPass})
	if err := c.eng.Runs().Start(context.Background(), "t", c.slug); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the attempt to go on past the seat stage", func() bool {
		_ = c.eng.Advance(context.Background())
		stages, _ := c.st.ListAttemptStages(context.Background(), atts[0].ID)
		return len(stages) > 1
	})
	if n := len(c.attempts(0)); n != 1 {
		t.Fatalf("attempts = %d; the resume must continue the same attempt", n)
	}
}

// Cancelling a seat stage stops the seat first, so the call ends as cancelled, not detached.
func TestCancelSeatKillsTheSession(t *testing.T) {
	seats := newFakeSeats()
	c := newSeatChain(t, seats)
	c.run()
	waitUntil(t, "the seat call", func() bool { return seats.callCount() == 1 })
	st := c.openSeatStage()
	if err := c.eng.CancelSeat(context.Background(), "owner", st.ID); err != nil {
		t.Fatal(err)
	}
	cur, _ := c.st.GetStage(context.Background(), st.ID)
	if cur.State != model.StageDone || cur.Outcome != OutcomeInfraCancel {
		t.Fatalf("stage = %s %s, want done infra:cancel", cur.State, cur.Outcome)
	}
	seats.mu.Lock()
	killed := seats.killed[st.ID]
	seats.mu.Unlock()
	if !killed {
		t.Fatal("the seat was not stopped")
	}
	if err := c.eng.CancelSeat(context.Background(), "owner", st.ID); err == nil {
		t.Fatal("cancelling a closed stage should fail")
	}
}

// Sessions whose stage is closed are ended at start.
func TestStartSweepsOrphanSeats(t *testing.T) {
	seats := newFakeSeats()
	c := newSeatChain(t, seats)
	seats.alive[999] = true
	if err := c.eng.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seats.swept) != 1 || seats.swept[0] != 999 {
		t.Fatalf("swept %v", seats.swept)
	}
}
