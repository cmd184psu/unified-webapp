package haproxy

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Compile-time proof that the in-package fakes satisfy the interfaces the
// model/render, driver and cert tasks all build against. If the Driver or Exec
// surface changes, these fail to compile here first.
var (
	_ Driver = (*fakeDriver)(nil)
	_ Exec   = (*fakeExec)(nil)
)

// fakeExec is a no-op command layer for tests above the driver: it never runs
// a real process. Per-OS driver tests (B3) use their own scripted exec.
type fakeExec struct{}

func (fakeExec) Run(ctx context.Context, stdin []byte, name string, args ...string) (stdout, stderr []byte, err error) {
	return nil, nil, nil
}

// fakeDriver is the stateful in-memory Driver for every test above the OS
// abstraction (model, apply, cert install, HTTP handlers). It keeps "files" in
// a map instead of touching the real filesystem, sudo or haproxy, records every
// operation in Calls, and fails on demand via the *Err fields so rollback and
// failure paths can be exercised. Safe for concurrent use.
type fakeDriver struct {
	mu sync.Mutex

	// Files holds the contents of every privileged-written path; Modes the mode
	// each was written with; ModTimes when.
	Files    map[string][]byte
	Modes    map[string]os.FileMode
	ModTimes map[string]time.Time
	// Calls records operations in order, e.g. "write /etc/haproxy/crt-list.txt",
	// "remove ...", "validate <path>", "reload".
	Calls []string

	// Inject failures: a non-nil error is returned by that operation.
	ReloadErr, RestartErr, StartErr, ValidateErr, WriteErr, RemoveErr error
	// WriteErrFor fails only writes to the given path (for install-order and
	// partial-failure tests).
	WriteErrFor map[string]error
	// Active is what Status reports.
	Active bool
	// StatsPath overrides StatsSocketPath (tests point it at a temp socket);
	// StatsOwner is what StatsSocketOwner reports.
	StatsPath, StatsOwner string
	// DiagnosticLines is what Diagnostics reports; DiagnosticsPanic makes it
	// panic (the status route must survive that).
	DiagnosticLines  []string
	DiagnosticsPanic bool
}

func newFakeDriver() *fakeDriver {
	return &fakeDriver{
		Files: map[string][]byte{}, Modes: map[string]os.FileMode{}, ModTimes: map[string]time.Time{},
		WriteErrFor: map[string]error{}, Active: true,
	}
}

func (f *fakeDriver) record(s string) { f.Calls = append(f.Calls, s) }

func (*fakeDriver) OSKind() OS { return OSRocky }

func (*fakeDriver) ConfigPath() string  { return "/etc/haproxy/haproxy.cfg" }
func (*fakeDriver) CertsDir() string    { return "/etc/haproxy/certs" }
func (*fakeDriver) CrtListPath() string { return "/etc/haproxy/crt-list.txt" }
func (f *fakeDriver) StatsSocketPath() string {
	if f.StatsPath != "" {
		return f.StatsPath
	}
	return "/run/haproxy/admin.sock"
}
func (f *fakeDriver) StatsSocketOwner() string { return f.StatsOwner }
func (*fakeDriver) BackupDir() string          { return "/etc/haproxy/backups" }

func (*fakeDriver) ServiceName() string { return "haproxy" }

func (f *fakeDriver) Reload(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("reload")
	return f.ReloadErr
}
func (f *fakeDriver) Restart(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("restart")
	return f.RestartErr
}
func (f *fakeDriver) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("start")
	return f.StartErr
}
func (f *fakeDriver) Status(ctx context.Context) (ServiceStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Active {
		return ServiceStatus{Active: true, Detail: "fake: active"}, nil
	}
	return ServiceStatus{Active: false, Detail: "fake: inactive"}, nil
}

func (*fakeDriver) Ownership() FileOwnership {
	return FileOwnership{User: "root", Group: "haproxy", ConfigMode: 0o644, CertMode: 0o640}
}

func (f *fakeDriver) PrivilegedRead(ctx context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("read " + path)
	b, ok := f.Files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), b...), nil
}

func (f *fakeDriver) PrivilegedList(ctx context.Context, dir string) ([]FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("list " + dir)
	out := []FileInfo{}
	for p, b := range f.Files {
		if filepath.Dir(p) == dir {
			out = append(out, FileInfo{Name: filepath.Base(p), Size: int64(len(b)), ModTime: f.ModTimes[p]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeDriver) PrivilegedRemove(ctx context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("remove " + path)
	if f.RemoveErr != nil {
		return f.RemoveErr
	}
	delete(f.Files, path)
	delete(f.Modes, path)
	delete(f.ModTimes, path)
	return nil
}

func (f *fakeDriver) PrivilegedWrite(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("write " + path)
	if err := f.WriteErrFor[path]; err != nil {
		return err
	}
	if f.WriteErr != nil {
		return f.WriteErr
	}
	f.Files[path] = append([]byte(nil), data...)
	f.Modes[path] = mode
	f.ModTimes[path] = time.Now()
	return nil
}

func (*fakeDriver) BaselineGlobal() []Directive {
	return []Directive{
		{Key: "maxconn", Value: "2000"},
		{Key: "log", Value: "127.0.0.1 local0 notice"},
	}
}

func (f *fakeDriver) Validate(ctx context.Context, configPath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record("validate " + configPath)
	return f.ValidateErr
}

func (f *fakeDriver) Diagnostics(ctx context.Context) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.DiagnosticsPanic {
		panic("fake: diagnostics exploded")
	}
	return append([]string(nil), f.DiagnosticLines...)
}

func (*fakeDriver) Version(ctx context.Context) (string, error) {
	return "HAProxy version fake", nil
}
