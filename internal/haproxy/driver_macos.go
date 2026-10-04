package haproxy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	macosLabel   = "net.cmdhome.unified.haproxy"
	macosBin     = "/opt/homebrew/bin/haproxy"
	macosPidfile = "/opt/homebrew/var/run/haproxy.pid"
	macosLog     = "/opt/homebrew/var/log/haproxy.log"
)

// macosDriver drives Apple-silicon Homebrew HAProxy through a launchd job the
// driver writes and owns. Files are the running user's (no sudo, no chown).
type macosDriver struct {
	x                                 Exec
	io                                *linuxFileIO
	cfg, certs, crtList, sock, backup string
	service, plist, pidfile           string
}

// NewMacOS returns the macOS driver; commands go through x.
func NewMacOS(x Exec, opts DriverOptions) Driver {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/var/empty"
	}
	return &macosDriver{
		x: x,
		io: &linuxFileIO{x: x, own: FileOwnership{ConfigMode: 0o644, CertMode: 0o600},
			list: []string{"find", "%s", "-maxdepth", "1", "-type", "f", "-exec", "stat", "-f", "%N\t%z\t%m", "{}", "+"}},
		cfg:     driverOr(opts.ConfigPath, "/opt/homebrew/etc/haproxy.cfg"),
		certs:   driverOr(opts.CertsDir, "/opt/homebrew/etc/haproxy/certs"),
		crtList: driverOr(opts.CrtListPath, "/opt/homebrew/etc/haproxy/crt-list.txt"),
		sock:    driverOr(opts.StatsSocketPath, "/opt/homebrew/var/run/haproxy.sock"),
		backup:  driverOr(opts.BackupDir, "/opt/homebrew/etc/haproxy/backups"),
		service: driverOr(opts.ServiceName, macosLabel),
		plist:   filepath.Join(home, "Library", "LaunchAgents", macosLabel+".plist"),
		pidfile: macosPidfile,
	}
}

func (d *macosDriver) OSKind() OS               { return OSMacOS }
func (d *macosDriver) ConfigPath() string       { return d.cfg }
func (d *macosDriver) CertsDir() string         { return d.certs }
func (d *macosDriver) CrtListPath() string      { return d.crtList }
func (d *macosDriver) StatsSocketPath() string  { return d.sock }
func (d *macosDriver) StatsSocketOwner() string { return "" }
func (d *macosDriver) BackupDir() string        { return d.backup }
func (d *macosDriver) ServiceName() string      { return d.service }
func (d *macosDriver) Ownership() FileOwnership { return d.io.own }

// BaselineGlobal: no user/group/chroot (non-root), stdout logging, and never a
// bind (a non-root process can only bind the wildcard address).
func (d *macosDriver) BaselineGlobal() []Directive {
	return []Directive{
		{Key: "log", Value: "stdout format raw local0"},
		{Key: "stats socket", Value: d.sock + " mode 600 level user"},
		{Key: "pidfile", Value: d.pidfile},
	}
}

func macosXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// macosRenderPlist renders the launchd job: `haproxy -W -f <cfg> -p <pidfile>`
// with a raised open-files limit. -W is on the command line; the removed
// master-worker keyword is never emitted.
func (d *macosDriver) macosRenderPlist() string {
	args := []string{macosBin, "-W", "-f", d.cfg, "-p", d.pidfile}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + macosXML(d.service) + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range args {
		b.WriteString("\t\t<string>" + macosXML(a) + "</string>\n")
	}
	limits := "\t\t<key>NumberOfFiles</key>\n\t\t<integer>65536</integer>\n"
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>SoftResourceLimits</key>
	<dict>
` + limits + `	</dict>
	<key>HardResourceLimits</key>
	<dict>
` + limits + `	</dict>
	<key>StandardOutPath</key>
	<string>` + macosXML(macosLog) + `</string>
	<key>StandardErrorPath</key>
	<string>` + macosXML(macosLog) + `</string>
</dict>
</plist>
`)
	return b.String()
}

func (d *macosDriver) macosDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
func (d *macosDriver) macosTarget() string { return d.macosDomain() + "/" + d.service }

func (d *macosDriver) Start(ctx context.Context) error {
	if _, err := d.io.run(ctx, nil, "mkdir", "-p", filepath.Dir(d.plist)); err != nil {
		return err
	}
	if err := d.io.write(ctx, d.plist, []byte(d.macosRenderPlist()), 0o644); err != nil {
		return err
	}
	_, err := d.io.run(ctx, nil, "launchctl", "bootstrap", d.macosDomain(), d.plist)
	if err == nil {
		return nil
	}
	// Already loaded: just start it.
	if _, perr := d.io.run(ctx, nil, "launchctl", "print", d.macosTarget()); perr == nil {
		_, kerr := d.io.run(ctx, nil, "launchctl", "kickstart", d.macosTarget())
		return kerr
	}
	return err
}

func (d *macosDriver) Restart(ctx context.Context) error {
	_, err := d.io.run(ctx, nil, "launchctl", "kickstart", "-k", d.macosTarget())
	return err
}

// Reload sends SIGUSR2 to the master process named in the pidfile (the job runs
// as the user, so no sudo).
func (d *macosDriver) Reload(ctx context.Context) error {
	out, err := d.io.read(ctx, d.pidfile)
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return fmt.Errorf("haproxy pidfile %s: invalid pid %q", d.pidfile, strings.TrimSpace(string(out)))
	}
	_, err = d.io.run(ctx, nil, "kill", "-USR2", strconv.Itoa(pid))
	return err
}

func (d *macosDriver) Status(ctx context.Context) (ServiceStatus, error) {
	out, _, err := d.x.Run(ctx, nil, "launchctl", "print", d.macosTarget())
	if err != nil {
		return ServiceStatus{Detail: d.service + ": not loaded"}, nil
	}
	active := strings.Contains(string(out), "state = running")
	detail := d.service + ": loaded"
	if active {
		detail = d.service + ": running"
	}
	return ServiceStatus{Active: active, Detail: detail}, nil
}

func (d *macosDriver) PrivilegedRead(ctx context.Context, p string) ([]byte, error) {
	return d.io.read(ctx, p)
}
func (d *macosDriver) PrivilegedWrite(ctx context.Context, p string, data []byte, m os.FileMode) error {
	return d.io.write(ctx, p, data, m)
}
func (d *macosDriver) PrivilegedList(ctx context.Context, dir string) ([]FileInfo, error) {
	return d.io.listDir(ctx, dir)
}
func (d *macosDriver) PrivilegedRemove(ctx context.Context, p string) error {
	return d.io.remove(ctx, p)
}

func (d *macosDriver) Validate(ctx context.Context, configPath string) error {
	_, err := d.io.run(ctx, nil, "haproxy", "-c", "-f", configPath)
	return err
}

// Diagnostics: nothing to report on macOS.
func (d *macosDriver) Diagnostics(ctx context.Context) []string { return nil }

func (d *macosDriver) Version(ctx context.Context) (string, error) {
	out, errOut, err := d.x.Run(ctx, nil, "haproxy", "-v")
	if err != nil {
		return "", fmt.Errorf("haproxy -v: %w: %s", err, strings.TrimSpace(string(errOut)))
	}
	return strings.TrimSpace(string(out)), nil
}
