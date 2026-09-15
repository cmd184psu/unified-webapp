package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeProcessor struct {
	wg  *sync.WaitGroup
	err error
}

func (f *fakeProcessor) Process(ctx context.Context, j Job, q *Queue) error {
	defer f.wg.Done()
	return f.err
}

func TestWorkerProcessesJob(t *testing.T) {
	q := New(1)

	var wg sync.WaitGroup
	wg.Add(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartWorkers(ctx, q, &fakeProcessor{wg: &wg}, 1)

	q.Enqueue(&Job{ID: "1", Status: Queued})

	waitOrFatal(t, &wg, "worker did not process job in time")

	waitForStatus(t, q, "1", Completed)
}

func TestWorkerSetsFailedOnError(t *testing.T) {
	q := New(1)

	var wg sync.WaitGroup
	wg.Add(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boom := errors.New("something broke")
	StartWorkers(ctx, q, &fakeProcessor{wg: &wg, err: boom}, 1)

	q.Enqueue(&Job{ID: "2", Status: Queued})

	waitOrFatal(t, &wg, "worker did not process job in time")

	got := waitForStatus(t, q, "2", Failed)
	if got.Error != boom.Error() {
		t.Errorf("Error field: got %q, want %q", got.Error, boom.Error())
	}
}

func TestWorkerContextCancel(t *testing.T) {
	q := New(1)
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	StartWorkers(ctx, q, &fakeProcessor{wg: &wg}, 1)

	q.Enqueue(&Job{ID: "3", Status: Queued})
	waitOrFatal(t, &wg, "first job not processed")

	// cancel and ensure no panic / hang on a second job that never runs
	cancel()
}

func TestMultipleWorkers(t *testing.T) {
	const n = 5
	q := New(n)

	var wg sync.WaitGroup
	wg.Add(n)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartWorkers(ctx, q, &fakeProcessor{wg: &wg}, n)

	ids := make([]string, n)
	for i := range ids {
		ids[i] = string(rune('a' + i))
		q.Enqueue(&Job{ID: ids[i], Status: Queued})
	}

	waitOrFatal(t, &wg, "not all jobs processed")

	for _, id := range ids {
		waitForStatus(t, q, id, Completed)
	}
}

func waitOrFatal(t *testing.T, wg *sync.WaitGroup, msg string) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
}

// waitForStatus polls q.Get until the job reaches the wanted status and
// returns the observed job value. The worker's final status Update can land
// after the processor's wg.Done fires, so a plain read after waitOrFatal
// could still see Running; polling closes that gap.
func waitForStatus(t *testing.T, q *Queue, id string, want Status) Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	last := Status("<never seen>")
	for time.Now().Before(deadline) {
		if j, ok := q.Get(id); ok {
			last = j.Status
			if j.Status == want {
				return j
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %s: never reached status %s (last seen %s)", id, want, last)
	return Job{}
}
