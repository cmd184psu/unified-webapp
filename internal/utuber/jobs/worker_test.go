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

func (f *fakeProcessor) Process(ctx context.Context, j *Job) error {
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

	job := &Job{ID: "1", Status: Queued}
	q.Enqueue(job)

	waitOrFatal(t, &wg, "worker did not process job in time")

	if job.Status != Completed {
		t.Fatalf("expected %s, got %s", Completed, job.Status)
	}
}

func TestWorkerSetsFailedOnError(t *testing.T) {
	q := New(1)

	var wg sync.WaitGroup
	wg.Add(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	boom := errors.New("something broke")
	StartWorkers(ctx, q, &fakeProcessor{wg: &wg, err: boom}, 1)

	job := &Job{ID: "2", Status: Queued}
	q.Enqueue(job)

	waitOrFatal(t, &wg, "worker did not process job in time")

	if job.Status != Failed {
		t.Fatalf("expected %s, got %s", Failed, job.Status)
	}
	if job.Error != boom.Error() {
		t.Errorf("Error field: got %q, want %q", job.Error, boom.Error())
	}
}

func TestWorkerContextCancel(t *testing.T) {
	q := New(1)
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	StartWorkers(ctx, q, &fakeProcessor{wg: &wg}, 1)

	job := &Job{ID: "3", Status: Queued}
	q.Enqueue(job)
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

	jobs := make([]*Job, n)
	for i := range jobs {
		jobs[i] = &Job{ID: string(rune('a' + i)), Status: Queued}
		q.Enqueue(jobs[i])
	}

	waitOrFatal(t, &wg, "not all jobs processed")

	for _, j := range jobs {
		if j.Status != Completed {
			t.Errorf("job %s: expected %s, got %s", j.ID, Completed, j.Status)
		}
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
