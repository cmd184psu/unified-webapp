package haproxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DriverOptions overrides a driver's FRD §4 defaults (the haproxy.* settings).
// Zero values keep the OS default.
type DriverOptions struct {
	ConfigPath      string
	CertsDir        string
	CrtListPath     string
	StatsSocketPath string
	BackupDir       string
	ServiceName     string
}

// currentUsername is the running app user; a variable so tests can inject one.
var currentUsername = func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.Username
}

func driverOr(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

// NewDriver returns the real driver for osKind over exec, or an error for an
// unsupported OS.
func NewDriver(osKind OS, exec Exec, opts DriverOptions) (Driver, error) {
	switch osKind {
	case OSUbuntu:
		return NewUbuntu(exec, opts), nil
	case OSRocky:
		return NewRocky(exec, opts), nil
	case OSMacOS:
		return NewMacOS(exec, opts), nil
	}
	return nil, fmt.Errorf("unsupported haproxy os %q", osKind)
}

// linuxFileIO is the Exec-backed file I/O shared by the Linux (sudo) and macOS
// (running user, no sudo) drivers. File contents only ever travel over stdin.
type linuxFileIO struct {
	x    Exec
	sudo bool
	own  FileOwnership
	// list is the argv template for listing: %s is replaced by the directory.
	// It must print "path<TAB>size<TAB>mtime-epoch" per regular file.
	list []string
}

func (f *linuxFileIO) run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	if f.sudo {
		args = append([]string{name}, args...)
		name = "sudo"
	}
	out, errOut, err := f.x.Run(ctx, stdin, name, args...)
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(errOut)))
	}
	return out, nil
}

func (f *linuxFileIO) read(ctx context.Context, path string) ([]byte, error) {
	out, err := f.run(ctx, nil, "cat", path)
	if err != nil && strings.Contains(err.Error(), "No such file or directory") {
		return nil, os.ErrNotExist
	}
	return out, err
}

func (f *linuxFileIO) remove(ctx context.Context, path string) error {
	_, err := f.run(ctx, nil, "rm", "-f", path)
	return err
}

// ensureDir creates dir when it is missing (first run on a fresh machine), with
// the same privilege as every other operation, so the temp write beside the
// target can succeed. The directory mode follows the file's: read bits become
// read+traverse (0640 -> 0750 for the certs dir, 0644 -> 0755 for config and
// backups, 0600 -> 0700 on macOS). On Linux it is chowned to the ownership
// policy's user:group so haproxy can enter the certs dir. An existing
// directory is never touched.
func (f *linuxFileIO) ensureDir(ctx context.Context, dir string, fileMode os.FileMode) error {
	if _, err := f.run(ctx, nil, "test", "-d", dir); err == nil {
		return nil
	}
	p := fileMode.Perm()
	dirMode := p | (p&0o444)>>2
	if _, err := f.run(ctx, nil, "mkdir", "-p", "-m", fmt.Sprintf("%04o", dirMode), dir); err != nil {
		return err
	}
	if f.own.User != "" {
		owner := f.own.User
		if f.own.Group != "" {
			owner += ":" + f.own.Group
		}
		if _, err := f.run(ctx, nil, "chown", owner, dir); err != nil {
			return err
		}
	}
	return nil
}

// EnsureDirs creates the backup folder (same mode Apply writes backups with) so
// a fresh install lists an empty folder, not a missing one.
func (d *linuxDriver) EnsureDirs(ctx context.Context) error {
	return d.io.ensureDir(ctx, d.backup, d.io.own.ConfigMode)
}

func (f *linuxFileIO) write(ctx context.Context, path string, data []byte, mode os.FileMode) error {
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := path + ".tmp-" + hex.EncodeToString(rnd[:])
	fail := func(err error) error {
		_, _ = f.run(ctx, nil, "rm", "-f", tmp)
		return err
	}
	if err := f.ensureDir(ctx, filepath.Dir(path), mode); err != nil {
		return err
	}
	// Create the temp file 0600 first so key bytes are never briefly readable.
	if _, err := f.run(ctx, nil, "install", "-m", "0600", "/dev/null", tmp); err != nil {
		return fail(err)
	}
	if _, err := f.run(ctx, data, "tee", tmp); err != nil {
		return fail(err)
	}
	if f.own.User != "" {
		owner := f.own.User
		if f.own.Group != "" {
			owner += ":" + f.own.Group
		}
		if _, err := f.run(ctx, nil, "chown", owner, tmp); err != nil {
			return fail(err)
		}
	}
	if _, err := f.run(ctx, nil, "chmod", fmt.Sprintf("%04o", mode.Perm()), tmp); err != nil {
		return fail(err)
	}
	if _, err := f.run(ctx, nil, "mv", "-f", tmp, path); err != nil {
		return fail(err)
	}
	return nil
}

func (f *linuxFileIO) listDir(ctx context.Context, dir string) ([]FileInfo, error) {
	args := make([]string, len(f.list))
	for i, a := range f.list {
		args[i] = a
		if a == "%s" { // only the whole-argument placeholder: -printf has its own %s (size)
			args[i] = dir
		}
	}
	out, err := f.run(ctx, nil, args[0], args[1:]...)
	if err != nil {
		if strings.Contains(err.Error(), "No such file or directory") {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	infos := []FileInfo{}
	for _, line := range strings.Split(string(out), "\n") {
		p := strings.Split(strings.TrimSpace(line), "\t")
		if len(p) != 3 {
			continue
		}
		size, err1 := strconv.ParseInt(p[1], 10, 64)
		mt, err2 := strconv.ParseFloat(p[2], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		infos = append(infos, FileInfo{Name: filepath.Base(p[0]), Size: size, ModTime: time.Unix(int64(mt), 0)})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })
	return infos, nil
}

// linuxDriver is the systemd driver shared by Ubuntu and Rocky.
type linuxDriver struct {
	kind                              OS
	x                                 Exec
	io                                *linuxFileIO
	cfg, certs, crtList, sock, backup string
	service                           string
	baseline                          []Directive
}

func newLinuxDriver(kind OS, x Exec, o DriverOptions, sock string) *linuxDriver {
	own := FileOwnership{User: "root", Group: "haproxy", ConfigMode: 0o644, CertMode: 0o640}
	return &linuxDriver{
		kind: kind, x: x,
		io: &linuxFileIO{x: x, sudo: true, own: own,
			list: []string{"find", "%s", "-maxdepth", "1", "-type", "f", "-printf", "%p\t%s\t%T@\n"}},
		cfg:     driverOr(o.ConfigPath, "/etc/haproxy/haproxy.cfg"),
		certs:   driverOr(o.CertsDir, "/etc/haproxy/certs"),
		crtList: driverOr(o.CrtListPath, "/etc/haproxy/crt-list.txt"),
		sock:    driverOr(o.StatsSocketPath, sock),
		backup:  driverOr(o.BackupDir, "/etc/haproxy/backups"),
		service: driverOr(o.ServiceName, "haproxy"),
	}
}

func (d *linuxDriver) linuxBaseline() []Directive {
	return []Directive{
		{Key: "log", Value: "/dev/log local0"},
		{Key: "chroot", Value: "/var/lib/haproxy"},
		{Key: "stats socket", Value: statsSocketValue(d.sock, currentUsername())},
		{Key: "user", Value: "haproxy"},
		{Key: "group", Value: "haproxy"},
	}
}

func (d *linuxDriver) OSKind() OS                  { return d.kind }
func (d *linuxDriver) ConfigPath() string          { return d.cfg }
func (d *linuxDriver) CertsDir() string            { return d.certs }
func (d *linuxDriver) CrtListPath() string         { return d.crtList }
func (d *linuxDriver) StatsSocketPath() string     { return d.sock }
func (d *linuxDriver) StatsSocketOwner() string    { return currentUsername() }
func (d *linuxDriver) BackupDir() string           { return d.backup }
func (d *linuxDriver) ServiceName() string         { return d.service }
func (d *linuxDriver) Ownership() FileOwnership    { return d.io.own }
func (d *linuxDriver) BaselineGlobal() []Directive { return append([]Directive(nil), d.baseline...) }

func (d *linuxDriver) systemctl(ctx context.Context, action string) error {
	_, err := d.io.run(ctx, nil, "systemctl", action, d.service)
	return err
}

func (d *linuxDriver) Reload(ctx context.Context) error  { return d.systemctl(ctx, "reload") }
func (d *linuxDriver) Restart(ctx context.Context) error { return d.systemctl(ctx, "restart") }
func (d *linuxDriver) Start(ctx context.Context) error   { return d.systemctl(ctx, "start") }

func (d *linuxDriver) Status(ctx context.Context) (ServiceStatus, error) {
	out, errOut, err := d.x.Run(ctx, nil, "sudo", "systemctl", "is-active", d.service)
	state := strings.TrimSpace(string(out))
	if err != nil && state == "" {
		return ServiceStatus{}, fmt.Errorf("systemctl is-active %s: %w: %s", d.service, err, strings.TrimSpace(string(errOut)))
	}
	return ServiceStatus{Active: err == nil && state == "active", Detail: d.service + ": " + state}, nil
}

func (d *linuxDriver) PrivilegedRead(ctx context.Context, p string) ([]byte, error) {
	return d.io.read(ctx, p)
}
func (d *linuxDriver) PrivilegedWrite(ctx context.Context, p string, data []byte, m os.FileMode) error {
	return d.io.write(ctx, p, data, m)
}
func (d *linuxDriver) PrivilegedList(ctx context.Context, dir string) ([]FileInfo, error) {
	return d.io.listDir(ctx, dir)
}
func (d *linuxDriver) PrivilegedRemove(ctx context.Context, p string) error {
	return d.io.remove(ctx, p)
}

func (d *linuxDriver) Validate(ctx context.Context, configPath string) error {
	_, err := d.io.run(ctx, nil, "haproxy", "-c", "-f", configPath)
	return err
}

// Diagnostics: nothing to report on the base Linux driver (Rocky overrides it).
func (d *linuxDriver) Diagnostics(ctx context.Context) []string { return nil }

func (d *linuxDriver) Version(ctx context.Context) (string, error) {
	out, errOut, err := d.x.Run(ctx, nil, "haproxy", "-v")
	if err != nil {
		return "", fmt.Errorf("haproxy -v: %w: %s", err, strings.TrimSpace(string(errOut)))
	}
	return strings.TrimSpace(string(out)), nil
}
