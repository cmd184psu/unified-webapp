package jobs

import (
	"errors"
	"sync"
)

var (
	// ErrNotFound: no job has that ID.
	ErrNotFound = errors.New("job not found")
	// ErrRunning: the job is being processed and can't be removed.
	ErrRunning = errors.New("job is running")
)

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

// Remove deletes a job that isn't running: a queued one never starts (the
// worker that later receives it from the channel finds it gone and skips it),
// and a finished one leaves the list. A running job is refused with
// ErrRunning, since stopping it mid-download needs proper cancellation. Its
// downloaded file, if any, is not touched.
func (q *Queue) Remove(id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	j, ok := q.jobs[id]
	if !ok {
		return ErrNotFound
	}
	if j.Status == Running {
		return ErrRunning
	}
	delete(q.jobs, id)
	for i, o := range q.order {
		if o == j {
			q.order = append(q.order[:i:i], q.order[i+1:]...)
			break
		}
	}
	return nil
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
