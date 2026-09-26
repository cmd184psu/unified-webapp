package jobs

import (
	"context"
)

// Processor handles one job: a value snapshot for reads, the queue as the
// mutation handle (all writes go through q.Update).
type Processor interface {
	Process(ctx context.Context, job Job, q *Queue) error
}

func StartWorkers(ctx context.Context, q *Queue, p Processor, n int) {
	for i := 0; i < n; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-q.ch:
					// Reading the write-once ID off the channel pointer is
					// the only permitted direct access; every other field is
					// reached through the queue's lock.
					id := job.ID
					// A job removed while queued is gone from the queue, so
					// the Update reports false and the job is skipped. Running
					// is set under the same lock Remove takes, so a job can't
					// be removed after it starts.
					if !q.Update(id, func(j *Job) { j.Status = Running }) {
						continue
					}
					snap, ok := q.Get(id)
					if !ok {
						continue
					}
					if err := p.Process(ctx, snap, q); err != nil {
						q.Update(id, func(j *Job) {
							j.Error = err.Error()
							j.Status = Failed
						})
					} else {
						q.Update(id, func(j *Job) { j.Status = Completed })
					}
				}
			}
		}()
	}
}
