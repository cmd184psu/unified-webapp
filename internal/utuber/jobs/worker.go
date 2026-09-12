package jobs

import (
	"context"
)

type Processor interface {
	Process(context.Context, *Job) error
}

func StartWorkers(ctx context.Context, q *Queue, p Processor, n int) {
	for i := 0; i < n; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-q.ch:
					job.Status = Running
					err := p.Process(ctx, job)
					if err != nil {
						job.Status = Failed
						job.Error = err.Error()
					} else {
						job.Status = Completed
					}
				}
			}
		}()
	}
}
