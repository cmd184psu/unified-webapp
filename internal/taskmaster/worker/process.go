package worker

import (
	"errors"
	"sync"
	"syscall"
)

// ErrProcessNotRunning is returned by Suspend/Resume (and reported via Get)
// when execID has no registered process-group leader — either it never
// started, or it already finished and was unregistered. Handlers map this to
// 404; any other error from Suspend/Resume is a signal failure (e.g. EPERM
// on a root-owned sudo child whose sudo escalation also failed) and maps to
// a 4xx with the underlying message.
var ErrProcessNotRunning = errors.New("execution not running")

type procEntry struct {
	pid       int
	sudo      bool
	suspended bool
}

// ProcessRegistry maps a running execution's ID to its process-group
// leader's PID and whether it was launched with sudo (which determines
// whether Suspend/Resume must escalate the signal via `sudo kill`).
// Instance-scoped: one is created per Worker/Coordinator pair in build.go,
// mirroring CancelRegistry.
type ProcessRegistry struct {
	mu      sync.Mutex
	entries map[int64]*procEntry
}

// NewProcessRegistry returns an empty ProcessRegistry.
func NewProcessRegistry() *ProcessRegistry {
	return &ProcessRegistry{entries: make(map[int64]*procEntry)}
}

// Register records execID's process-group leader PID and sudo flag.
func (r *ProcessRegistry) Register(execID int64, pid int, sudo bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[execID] = &procEntry{pid: pid, sudo: sudo}
}

// Unregister clears execID's entry. Call once the execution has finished.
func (r *ProcessRegistry) Unregister(execID int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, execID)
}

// Get returns execID's process-group leader PID, if registered (i.e.
// currently running).
func (r *ProcessRegistry) Get(execID int64) (pid int, ok bool) {
	if r == nil {
		return 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[execID]
	if !ok {
		return 0, false
	}
	return e.pid, true
}

// Suspended reports whether execID is currently marked suspended. In-memory
// only — deliberately not persisted, since a suspended process (and its
// state) is gone on restart.
func (r *ProcessRegistry) Suspended(execID int64) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[execID]
	return ok && e.suspended
}

// Suspend sends SIGSTOP to execID's whole process group (escalated via
// `sudo kill` when the task ran with sudo). Returns ErrProcessNotRunning if
// execID isn't registered, or the signal error otherwise (e.g. sudo itself
// unavailable/denied).
func (r *ProcessRegistry) Suspend(execID int64) error {
	return r.signal(execID, syscall.SIGSTOP, true)
}

// Resume sends SIGCONT to execID's whole process group, mirroring Suspend.
func (r *ProcessRegistry) Resume(execID int64) error {
	return r.signal(execID, syscall.SIGCONT, false)
}

func (r *ProcessRegistry) signal(execID int64, sig syscall.Signal, suspended bool) error {
	if r == nil {
		return ErrProcessNotRunning
	}
	r.mu.Lock()
	e, ok := r.entries[execID]
	r.mu.Unlock()
	if !ok {
		return ErrProcessNotRunning
	}
	if err := sendGroupSignal(e.pid, e.sudo, sig); err != nil {
		return err
	}
	r.mu.Lock()
	e.suspended = suspended
	r.mu.Unlock()
	return nil
}
