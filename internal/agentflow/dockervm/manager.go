// Package dockervm owns the container runtime a docker project needs (AF-12).
//
// On macOS the runtime is a Colima VM, and the owner does not want it running
// unless work needs it: the manager starts it before the first container,
// reports an attention item when it cannot, and stops it after the last
// agentflow container has been gone for the idle period. That behaviour is a
// property of the driver, so the same manager works on Linux, where Docker is
// native: there the "start" is a daemon reachability check and the daemon is
// never stopped.
//
// Every start and stop is serialized: a stop never races a start or a stage.
// The manager is safe for concurrent use.
package dockervm

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"
)

// Driver is the container runtime on one host: Colima on macOS, the Docker
// daemon on Linux.
type Driver interface {
	// Name is colima or docker, for logs.
	Name() string
	// Start makes the runtime answer. A runtime that is already up is not
	// restarted.
	Start(ctx context.Context) error
	// Stop stops the runtime. It is a no-op on a native Linux daemon.
	Stop(ctx context.Context) error
	// Running counts this instance's containers (label agentflow=1).
	Running(ctx context.Context) (int, error)
	// Status is a human-readable state for the API and logs.
	Status(ctx context.Context) (string, error)
}

// Config is the manager's knobs. The defaults are for the owner's machine:
// agentflow-worker:0.1, a 15 minute idle stop and a two minute start.
type Config struct {
	// Idle is how long the runtime may sit with no agentflow container before
	// it is stopped. Zero means DefaultIdle.
	Idle time.Duration
	// StartTimeout bounds waiting for the runtime to answer. Zero means
	// DefaultStartTimeout.
	StartTimeout time.Duration
	// Logf receives start and stop lines. Nil discards them.
	Logf func(format string, args ...any)
	// Attention receives a human-readable line when the runtime cannot start or
	// stop. Nil discards it.
	Attention func(message string)
	// DockerBin and ColimaBin are for tests; nil uses PATH.
	DockerBin string
	ColimaBin string
}

// Defaults for Config.
const (
	DefaultIdle         = 15 * time.Minute
	DefaultStartTimeout = 2 * time.Minute
)

func (c Config) idle() time.Duration {
	if c.Idle > 0 {
		return c.Idle
	}
	return DefaultIdle
}

func (c Config) startTimeout() time.Duration {
	if c.StartTimeout > 0 {
		return c.StartTimeout
	}
	return DefaultStartTimeout
}

func (c Config) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

func (c Config) attention(msg string) {
	if c.Attention != nil {
		c.Attention(msg)
	}
}

// Manager starts the runtime when a docker stage needs it and stops it when it
// has been idle.
type Manager struct {
	cfg Config
	drv Driver

	// startMu serializes Start and Stop; it is held across a slow Colima boot.
	startMu sync.Mutex
	// mu guards the fields below and is never held across a command.
	mu           sync.Mutex
	started      bool
	lastUsed     time.Time
	lastSeenBusy time.Time
	now          func() time.Time
}

// New returns the manager for this host: Colima on darwin, the Docker daemon
// everywhere else.
func New(cfg Config) *Manager {
	docker := cfg.DockerBin
	if docker == "" {
		docker = "docker"
	}
	colima := cfg.ColimaBin
	if colima == "" {
		colima = "colima"
	}
	var drv Driver
	if runtime.GOOS == "darwin" {
		drv = &colimaDriver{docker: docker, colima: colima, timeout: cfg.startTimeout()}
	} else {
		drv = &daemonDriver{docker: docker}
	}
	return NewWithDriver(drv, cfg)
}

// NewWithDriver returns a manager over drv. Tests use it with a fake driver.
func NewWithDriver(drv Driver, cfg Config) *Manager {
	return &Manager{cfg: cfg, drv: drv, now: time.Now}
}

// DriverName is the runtime's name (colima or docker).
func (m *Manager) DriverName() string { return m.drv.Name() }

// Ensure makes the runtime ready for a container. Concurrent callers wait for
// one start; a runtime that is already up costs nothing.
func (m *Manager) Ensure(ctx context.Context) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.Lock()
	if m.started {
		m.lastUsed = m.now()
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	if err := m.drv.Start(ctx); err != nil {
		msg := fmt.Sprintf("agentflow: the docker runtime (%s) could not start: %v", m.drv.Name(), err)
		m.cfg.attention(msg)
		return errors.New(msg)
	}
	m.mu.Lock()
	m.started = true
	m.lastUsed = m.now()
	m.mu.Unlock()
	m.cfg.logf("%s: started for a docker stage", m.drv.Name())
	return nil
}

// Release records that a stage using the runtime has finished. The idle timer
// also watches for containers, so a stage that never calls it still delays the
// stop while its container runs.
func (m *Manager) Release() {
	m.mu.Lock()
	m.lastUsed = m.now()
	m.mu.Unlock()
}

// Status reports the runtime state without starting it.
func (m *Manager) Status(ctx context.Context) (string, error) { return m.drv.Status(ctx) }

// StopIfIdle stops the runtime when it is up, no agentflow container is
// running, and the idle period has passed since the last use or the last time
// a container was seen. It reports whether it stopped.
func (m *Manager) StopIfIdle(ctx context.Context) (bool, error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return false, nil
	}
	last := m.lastUsed
	if m.lastSeenBusy.After(last) {
		last = m.lastSeenBusy
	}
	if m.now().Sub(last) < m.cfg.idle() {
		m.mu.Unlock()
		return false, nil
	}
	m.mu.Unlock()

	n, err := m.drv.Running(ctx)
	if err != nil {
		// The runtime not answering means there is nothing to stop.
		m.mu.Lock()
		m.started = false
		m.mu.Unlock()
		m.cfg.logf("%s: not answering while idle-checking, treating it as stopped: %v", m.drv.Name(), err)
		return false, nil
	}
	if n > 0 {
		m.mu.Lock()
		m.lastSeenBusy = m.now()
		m.mu.Unlock()
		return false, nil
	}
	if err := m.drv.Stop(ctx); err != nil {
		msg := fmt.Sprintf("agentflow: the docker runtime (%s) could not stop: %v", m.drv.Name(), err)
		m.cfg.attention(msg)
		return false, err
	}
	m.mu.Lock()
	m.started = false
	m.mu.Unlock()
	m.cfg.logf("%s: stopped after %s idle", m.drv.Name(), m.cfg.idle())
	return true, nil
}

// Run watches the idle timer until ctx is done. It is the manager's only
// background goroutine; the composition root starts it once.
func (m *Manager) Run(ctx context.Context) {
	tick := m.cfg.idle() / 4
	if tick < time.Second {
		tick = time.Second
	}
	if tick > time.Minute {
		tick = time.Minute
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := m.StopIfIdle(ctx); err != nil {
				continue // already reported as an attention item
			}
		}
	}
}

// AttachCommand is the interface AF-11's seat mode uses: a session runs inside
// the stage's container, not beside it. The seat runs this and attaches its
// tmux client; AF-11 owns the tmux session name. Ensure is called first so a
// seat does not need to know about Colima.
func (m *Manager) AttachCommand(ctx context.Context, container, session string) (*exec.Cmd, error) {
	if err := m.Ensure(ctx); err != nil {
		return nil, err
	}
	docker := m.cfg.DockerBin
	if docker == "" {
		docker = "docker"
	}
	if session == "" {
		session = "af"
	}
	cmd := exec.Command(docker, "exec", "-it", container, "tmux", "attach", "-t", session)
	return cmd, nil
}
