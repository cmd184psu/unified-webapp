package jobs

import "sync"

type Queue struct {
	ch    chan *Job
	jobs  map[string]*Job
	order []*Job
	mu    sync.RWMutex
}

func New(buffer int) *Queue {
	return &Queue{
		ch:   make(chan *Job, buffer),
		jobs: make(map[string]*Job),
	}
}

func (q *Queue) Enqueue(j *Job) {
	q.mu.Lock()
	q.jobs[j.ID] = j
	q.order = append(q.order, j)
	q.mu.Unlock()
	q.ch <- j
}

func (q *Queue) All() []*Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]*Job, len(q.order))
	copy(out, q.order)
	return out
}
