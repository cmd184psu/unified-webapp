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

// Enqueue registers j and hands it to a worker. Ownership of the *Job
// transfers to the queue: callers must not retain or mutate the pointer
// after enqueueing — all later reads go through All/Get, all writes
// through Update.
func (q *Queue) Enqueue(j *Job) {
	q.mu.Lock()
	q.jobs[j.ID] = j
	q.order = append(q.order, j)
	q.mu.Unlock()
	q.ch <- j
}

// All returns value snapshots of every job in queue order.
func (q *Queue) All() []Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]Job, len(q.order))
	for i, j := range q.order {
		out[i] = *j
	}
	return out
}

// Update applies fn to the job with the given ID under the write lock and
// reports whether the ID was known. fn runs while the lock is held and must
// not call any other Queue method (Update is non-reentrant; doing so
// deadlocks). Compound mutations inside one fn are atomic to readers.
func (q *Queue) Update(id string, fn func(*Job)) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[id]
	if !ok {
		return false
	}
	fn(j)
	return true
}

// Get returns a value snapshot of the job with the given ID.
func (q *Queue) Get(id string) (Job, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	j, ok := q.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}
