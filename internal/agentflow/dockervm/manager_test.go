package dockervm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeDriver struct {
	starts, stops int
	startErr      error
	stopErr       error
	running       int
	runningErr    error
}

func (f *fakeDriver) Name() string { return "fake" }
func (f *fakeDriver) Start(context.Context) error {
	f.starts++
	return f.startErr
}
func (f *fakeDriver) Stop(context.Context) error {
	f.stops++
	return f.stopErr
}
func (f *fakeDriver) Running(context.Context) (int, error) { return f.running, f.runningErr }
func (f *fakeDriver) Status(context.Context) (string, error) {
	return "", nil
}

func testManager(drv *fakeDriver, idle time.Duration, clock *time.Time) *Manager {
	m := NewWithDriver(drv, Config{Idle: idle})
	m.now = func() time.Time { return *clock }
	return m
}

// TestEnsureStartsOnce: concurrent stages share one VM start.
func TestEnsureStartsOnce(t *testing.T) {
	drv := &fakeDriver{}
	m := NewWithDriver(drv, Config{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.Ensure(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if drv.starts != 1 {
		t.Fatalf("starts = %d, want 1", drv.starts)
	}
}

// TestStopAfterIdle: nothing is stopped before the idle period, and the stop
// happens once after it.
func TestStopAfterIdle(t *testing.T) {
	drv := &fakeDriver{}
	clock := time.Unix(0, 0)
	m := testManager(drv, time.Minute, &clock)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(30 * time.Second)
	if stopped, err := m.StopIfIdle(context.Background()); err != nil || stopped {
		t.Fatalf("stopped early: %v, %v", stopped, err)
	}
	clock = clock.Add(31 * time.Second)
	if stopped, err := m.StopIfIdle(context.Background()); err != nil || !stopped {
		t.Fatalf("did not stop after idle: %v, %v", stopped, err)
	}
	if drv.stops != 1 {
		t.Fatalf("stops = %d, want 1", drv.stops)
	}
	if stopped, _ := m.StopIfIdle(context.Background()); stopped {
		t.Fatal("stopped twice")
	}
}

// TestNoStopWhileContainerRuns: a running agentflow container holds the VM
// open, and the idle clock starts when it ends, not when the stage began.
func TestNoStopWhileContainerRuns(t *testing.T) {
	drv := &fakeDriver{running: 1}
	clock := time.Unix(0, 0)
	m := testManager(drv, time.Minute, &clock)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(10 * time.Minute)
	if stopped, _ := m.StopIfIdle(context.Background()); stopped {
		t.Fatal("stopped while a container runs")
	}
	drv.running = 0
	clock = clock.Add(30 * time.Second)
	if stopped, _ := m.StopIfIdle(context.Background()); stopped {
		t.Fatal("stopped 30s after the container ended")
	}
	clock = clock.Add(31 * time.Second)
	if stopped, _ := m.StopIfIdle(context.Background()); !stopped {
		t.Fatal("did not stop after the idle period since the container ended")
	}
}

// TestStartFailureIsAnAttentionItem: a VM that cannot start returns the reason
// and tells the owner, and a later stage may try again.
func TestStartFailureIsAnAttentionItem(t *testing.T) {
	drv := &fakeDriver{startErr: errors.New("no virtualization")}
	var attention []string
	m := NewWithDriver(drv, Config{Attention: func(msg string) { attention = append(attention, msg) }})
	if err := m.Ensure(context.Background()); err == nil || !strings.Contains(err.Error(), "no virtualization") {
		t.Fatalf("Ensure error = %v", err)
	}
	if len(attention) != 1 || !strings.Contains(attention[0], "could not start") {
		t.Fatalf("attention = %v", attention)
	}
	if err := m.Ensure(context.Background()); err == nil {
		t.Fatal("a failed start was remembered as started")
	}
	if drv.starts != 2 {
		t.Fatalf("starts = %d, want a retry", drv.starts)
	}
}

// TestStopFailureKeepsTheVMOpen: a failed stop keeps the state, so the next
// Ensure does not start a VM that is still up.
func TestStopFailureKeepsTheVMOpen(t *testing.T) {
	drv := &fakeDriver{stopErr: errors.New("busy")}
	clock := time.Unix(0, 0)
	m := testManager(drv, time.Minute, &clock)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := m.StopIfIdle(context.Background()); err == nil {
		t.Fatal("stop error was swallowed")
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if drv.starts != 1 {
		t.Fatalf("starts = %d, want 1: the VM is still up", drv.starts)
	}
}

// TestRuntimeGoneIsTreatedAsStopped: docker not answering means there is
// nothing to stop, and the manager does not keep believing it is up.
func TestRuntimeGoneIsTreatedAsStopped(t *testing.T) {
	drv := &fakeDriver{runningErr: errors.New("no daemon")}
	clock := time.Unix(0, 0)
	m := testManager(drv, time.Minute, &clock)
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	if stopped, err := m.StopIfIdle(context.Background()); err != nil || stopped {
		t.Fatalf("StopIfIdle = %v, %v", stopped, err)
	}
	if drv.stops != 0 {
		t.Fatal("tried to stop an unreachable runtime")
	}
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	if drv.starts != 2 {
		t.Fatalf("starts = %d, want a fresh start after the runtime went away", drv.starts)
	}
}

// fakeColima writes docker and colima scripts that share a state file: docker
// info answers only after colima start has created it.
func fakeColima(t *testing.T) (docker, colima, state string) {
	t.Helper()
	dir := t.TempDir()
	state = filepath.Join(dir, "vm-state")
	docker = filepath.Join(dir, "docker")
	colima = filepath.Join(dir, "colima")
	writeExec(t, docker, `#!/bin/sh
case "$1" in
  info) [ -f "$FAKE_STATE" ] && exit 0 || exit 1 ;;
  ps) exit 0 ;;
esac
exit 0
`)
	writeExec(t, colima, `#!/bin/sh
case "$1" in
  start) touch "$FAKE_STATE" ;;
  stop) rm -f "$FAKE_STATE" ;;
  status) [ -f "$FAKE_STATE" ] || exit 1 ;;
esac
exit 0
`)
	t.Setenv("FAKE_STATE", state)
	return docker, colima, state
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestColimaDriverStartsOnDemand: a down VM is started once and is not
// restarted once docker answers.
func TestColimaDriverStartsOnDemand(t *testing.T) {
	docker, colima, state := fakeColima(t)
	d := &colimaDriver{docker: docker, colima: colima, timeout: 10 * time.Second}
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("colima start did not run: %v", err)
	}
	// A second start sees a working docker and leaves the VM alone: removing
	// the state would make this fail.
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("a running VM was stopped or restarted: %v", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("colima stop did not run: %v", err)
	}
}

// TestColimaDriverStartFailure: colima failing to start is reported with its
// output.
func TestColimaDriverStartFailure(t *testing.T) {
	docker, colima, _ := fakeColima(t)
	writeExec(t, colima, "#!/bin/sh\necho 'no hypervisor' >&2\nexit 1\n")
	d := &colimaDriver{docker: docker, colima: colima, timeout: 5 * time.Second}
	err := d.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no hypervisor") {
		t.Fatalf("Start error = %v", err)
	}
}

// TestDaemonDriverNeverStopsTheHost: Linux has no VM; an unreachable daemon is
// an error telling the owner what to start, and Stop is a no-op.
func TestDaemonDriverNeverStopsTheHost(t *testing.T) {
	dir := t.TempDir()
	docker := filepath.Join(dir, "docker")
	writeExec(t, docker, "#!/bin/sh\nexit 1\n")
	d := &daemonDriver{docker: docker}
	if err := d.Start(context.Background()); err == nil || !strings.Contains(err.Error(), "docker.socket") {
		t.Fatalf("Start error = %v", err)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Fatalf("Stop = %v", err)
	}
}

// notOwned is a driver whose VM someone else started.
type notOwned struct{ fakeDriver }

func (n *notOwned) Owned() bool { return false }

// TestIdleStopLeavesAVMItDidNotStart: a VM that was already up when agentflow needed it is
// never stopped by the idle timer (AF-12 review finding 4).
func TestIdleStopLeavesAVMItDidNotStart(t *testing.T) {
	drv := &notOwned{}
	clock := time.Unix(1000, 0)
	m := NewWithDriver(drv, Config{Idle: time.Minute})
	m.now = func() time.Time { return clock }
	if err := m.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	if stopped, err := m.StopIfIdle(context.Background()); err != nil || stopped {
		t.Fatalf("StopIfIdle = %v, %v", stopped, err)
	}
	if drv.stops != 0 {
		t.Fatal("stopped a VM agentflow did not start")
	}
}

// TestColimaOwnershipAndAllContainers: the driver owns a VM only when it ran colima start,
// and counts every running container, not only agentflow's, before a stop.
func TestColimaOwnershipAndAllContainers(t *testing.T) {
	docker, colima, state := fakeColima(t)
	if err := os.WriteFile(state, nil, 0o644); err != nil { // already up: someone else's
		t.Fatal(err)
	}
	d := &colimaDriver{docker: docker, colima: colima, timeout: 5 * time.Second}
	if err := d.Start(context.Background()); err != nil || d.Owned() {
		t.Fatalf("an already running VM is not ours: owned=%v err=%v", d.Owned(), err)
	}
	if err := os.Remove(state); err != nil {
		t.Fatal(err)
	}
	if err := d.Start(context.Background()); err != nil || !d.Owned() {
		t.Fatalf("a VM agentflow started is ours: owned=%v err=%v", d.Owned(), err)
	}
	writeExec(t, docker, `#!/bin/sh
case "$1" in
  info) exit 0 ;;
  ps) case "$*" in *label=agentflow*) ;; *) printf 'owners-db\nowners-cache\n' ;; esac ;;
esac
exit 0
`)
	if n, err := d.Running(context.Background()); err != nil || n != 2 {
		t.Fatalf("Running = %d, %v; want every container counted", n, err)
	}
}
