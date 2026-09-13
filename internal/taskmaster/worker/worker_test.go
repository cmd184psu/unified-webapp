package worker_test

import (
	"context"
	"io"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type mockExecutor struct {
	fn func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error
}

func (m *mockExecutor) Execute(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
	return m.fn(ctx, task, stdout, stderr)
}

func newTestWorker(t *testing.T, exec worker.Executor) (*worker.Worker, *db.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	err = d.UpsertLane(&models.Lane{Name: "test", Width: 2})
	require.NoError(t, err)

	w := worker.NewWithExecutor(d, worker.NewRegistry(), "test-worker", exec)
	return w, d
}

func addTask(t *testing.T, d *db.DB, name string) *models.Task {
	t.Helper()
	task := &models.Task{
		Name: name, LaneName: "test",
		Enabled: true, Position: 50, Command: "echo hi",
	}
	id, err := d.AddTask(task)
	require.NoError(t, err)
	task.ID = id
	return task
}

func TestWorker_PicksEligibleTask(t *testing.T) {
	executed := make(chan string, 1)
	exec := &mockExecutor{fn: func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
		executed <- task.Name
		return nil
	}}

	w, d := newTestWorker(t, exec)
	addTask(t, d, "my-task")

	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)

	select {
	case name := <-executed:
		require.Equal(t, "my-task", name)
	case <-time.After(15 * time.Second):
		t.Fatal("task was not executed within timeout")
	}
	cancel()
	w.Wait()
}

func TestWorker_RespectsWidth(t *testing.T) {
	started := make(chan string, 10)
	release := make(chan struct{})
	exec := &mockExecutor{fn: func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
		started <- task.Name
		<-release
		return nil
	}}

	_, d := newTestWorker(t, exec)
	w2 := worker.NewWithExecutor(d, worker.NewRegistry(), "w2", exec)

	addTask(t, d, "task-a")
	addTask(t, d, "task-b")
	addTask(t, d, "task-c")

	ctx, cancel := context.WithCancel(context.Background())
	go w2.Start(ctx)

	// Wait for 2 to start
	<-started
	<-started

	// Third should not start yet
	select {
	case <-started:
		t.Fatal("lane width exceeded — third task started while two were running")
	case <-time.After(7 * time.Second):
		// correct: no third task started
	}
	close(release)
	cancel()
	w2.Wait()
}

func TestWorker_SkipsPausedLane(t *testing.T) {
	executed := make(chan string, 1)
	exec := &mockExecutor{fn: func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
		executed <- task.Name
		return nil
	}}

	w, d := newTestWorker(t, exec)
	addTask(t, d, "task-in-paused-lane")
	require.NoError(t, d.SetLanePaused("test", true, "admin"))

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	go w.Start(ctx)

	select {
	case <-executed:
		t.Fatal("task in paused lane should not have run")
	case <-ctx.Done():
		// correct
	}
	w.Wait()
}

func TestWorker_SkipsPausedTask(t *testing.T) {
	executed := make(chan string, 1)
	exec := &mockExecutor{fn: func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
		executed <- task.Name
		return nil
	}}

	w, d := newTestWorker(t, exec)
	task := addTask(t, d, "paused-task")
	require.NoError(t, d.SetTaskPaused(task.Name, true))

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	go w.Start(ctx)

	select {
	case <-executed:
		t.Fatal("paused task should not have run")
	case <-ctx.Done():
		// correct
	}
	w.Wait()
}

func TestWorker_RunsInPositionOrder(t *testing.T) {
	var mu = make(chan struct{}, 1)
	mu <- struct{}{}
	var order []string
	done := make(chan struct{})

	exec := &mockExecutor{fn: func(ctx context.Context, task *models.Task, stdout, stderr io.Writer) error {
		<-mu
		order = append(order, task.Name)
		mu <- struct{}{}
		if len(order) == 2 {
			close(done)
		}
		return nil
	}}

	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "seq", Width: 1}))

	// Insert "second" first (so id ordering alone wouldn't produce the
	// expected result) with a higher position, then "first" with a lower
	// position — the ready-next order must follow position, not insertion
	// order or priority.
	_, err = d.AddTask(&models.Task{Name: "second", LaneName: "seq", Enabled: true, Position: 90, Command: "echo hi"})
	require.NoError(t, err)
	_, err = d.AddTask(&models.Task{Name: "first", LaneName: "seq", Enabled: true, Position: 10, Command: "echo hi"})
	require.NoError(t, err)

	w := worker.NewWithExecutor(d, worker.NewRegistry(), "w", exec)
	ctx, cancel := context.WithCancel(context.Background())
	go w.Start(ctx)

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("tasks did not both run within timeout")
	}
	cancel()
	w.Wait()

	require.Equal(t, []string{"first", "second"}, order)
}

// ─── OutputCapture tests ─────────────────────────────────────────────────────

func TestOutputCapture_RingBuffer(t *testing.T) {
	oc := worker.NewOutputCapture(1, "stdout")
	for i := 0; i < 5; i++ {
		oc.Write([]byte("line\n"))
	}
	lines := oc.Lines()
	require.Len(t, lines, 5)
	for _, l := range lines {
		require.Equal(t, "line", l.Line)
	}
}

func TestOutputCapture_Subscribe(t *testing.T) {
	oc := worker.NewOutputCapture(2, "stdout")
	ch := oc.Subscribe()

	go func() {
		time.Sleep(10 * time.Millisecond)
		oc.Write([]byte("hello\n"))
		oc.MarkDone()
	}()

	var received []worker.OutputLine
	for line := range ch {
		received = append(received, line)
	}
	require.Len(t, received, 1)
	require.Equal(t, "hello", received[0].Line)
}

func TestOutputCapture_MarkDone(t *testing.T) {
	oc := worker.NewOutputCapture(3, "stderr")
	require.False(t, oc.Done())
	oc.MarkDone()
	require.True(t, oc.Done())
}

// TestOutputCapture_SubscribeAfterMarkDone is the regression test for the
// subscribe-after-MarkDone race: a late subscriber must get an
// already-closed channel instead of hanging forever.
func TestOutputCapture_SubscribeAfterMarkDone(t *testing.T) {
	oc := worker.NewOutputCapture(4, "stdout")
	oc.MarkDone()

	ch := oc.Subscribe()
	select {
	case _, ok := <-ch:
		require.False(t, ok, "expected an already-closed channel")
	case <-time.After(time.Second):
		t.Fatal("Subscribe after MarkDone did not return a closed channel")
	}
}

func TestOutputRegistry_RegisterGet(t *testing.T) {
	r := worker.NewRegistry()
	const execID = int64(99901)
	stdout, stderr := r.Register(execID)
	require.NotNil(t, stdout)
	require.NotNil(t, stderr)

	so, se, ok := r.Get(execID)
	require.True(t, ok)
	require.NotNil(t, so)
	require.NotNil(t, se)

	r.Unregister(execID)
	_, _, ok = r.Get(execID)
	require.False(t, ok)
}

// TestOutputRegistry_StartGC_Stop verifies StartGC's stop func returns
// promptly and leaves no running goroutine behind (checked by TestMain's
// goleak.VerifyTestMain).
func TestOutputRegistry_StartGC_Stop(t *testing.T) {
	r := worker.NewRegistry()
	stop := r.StartGC(time.Hour)

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
		// correct: stop() returned
	case <-time.After(5 * time.Second):
		t.Fatal("stop() did not return promptly")
	}
}

// ─── Executor tests ──────────────────────────────────────────────────────────

func TestExecutor_SudoGating_Denied(t *testing.T) {
	te := &worker.TaskExecutor{Sudo: worker.NewSudoGate(false)}
	task := &models.Task{Sudo: true, Command: "echo hi"}

	err := te.Execute(context.Background(), task, io.Discard, io.Discard)
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_sudo")
}

func TestExecutor_EmptyCommand(t *testing.T) {
	te := &worker.TaskExecutor{}
	task := &models.Task{Command: ""}

	err := te.Execute(context.Background(), task, io.Discard, io.Discard)
	require.Error(t, err)
}

func TestExecutor_RunsCommand(t *testing.T) {
	te := &worker.TaskExecutor{}
	task := &models.Task{Command: "echo hi"}

	var out bytesBuf
	err := te.Execute(context.Background(), task, &out, io.Discard)
	require.NoError(t, err)
	require.Contains(t, out.String(), "hi")
}

// TestExecutor_ContextCancel verifies a long-running task's Execute call
// returns promptly once runCtx is cancelled.
func TestExecutor_ContextCancel(t *testing.T) {
	te := &worker.TaskExecutor{}
	task := &models.Task{Command: "sleep 300"}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- te.Execute(ctx, task, io.Discard, io.Discard)
	}()

	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	cancel()

	select {
	case err := <-errCh:
		require.Error(t, err)
		require.Less(t, time.Since(start), 15*time.Second)
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return promptly after ctx cancel")
	}
}

// TestExecutor_GrandchildPipe verifies that a backgrounded grandchild
// process holding the inherited stdout/stderr pipes open does not hang
// Execute forever after ctx cancel — WaitDelay must force it to return
// within its bound (~10s).
func TestExecutor_GrandchildPipe(t *testing.T) {
	te := &worker.TaskExecutor{}
	task := &models.Task{Command: "sleep 300 & wait"}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- te.Execute(ctx, task, io.Discard, io.Discard)
	}()

	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	cancel()

	select {
	case <-errCh:
		require.Less(t, time.Since(start), 15*time.Second)
	case <-time.After(15 * time.Second):
		t.Fatal("Execute did not return within the WaitDelay bound")
	}
}

// bytesBuf is a tiny concurrency-safe-enough buffer for capturing a single
// command's stdout in tests (no concurrent writers here).
type bytesBuf struct {
	data []byte
}

func (b *bytesBuf) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesBuf) String() string { return string(b.data) }
