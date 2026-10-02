package haproxy

import (
	"context"
	"fmt"
	"strings"
)

// rockyDriver is the systemd driver for Rocky/RHEL (FRD §4), plus a read-only
// SELinux detect-and-report helper.
type rockyDriver struct{ *linuxDriver }

// NewRocky returns the Rocky driver; every privileged operation goes through x.
func NewRocky(x Exec, opts DriverOptions) Driver {
	d := newLinuxDriver(OSRocky, x, opts, "/var/lib/haproxy/stats")
	d.baseline = d.linuxBaseline()
	return &rockyDriver{d}
}

// Diagnostics reports SELinux conditions that will make HAProxy misbehave
// (FR-H33): enforcing with haproxy_connect_any off. Read-only; if SELinux
// cannot be inspected (e.g. not installed) there is nothing to report.
func (d *rockyDriver) Diagnostics(ctx context.Context) []string {
	mode, _, err := d.x.Run(ctx, nil, "getenforce")
	if err != nil || !strings.EqualFold(strings.TrimSpace(string(mode)), "enforcing") {
		return nil
	}
	out, _, err := d.x.Run(ctx, nil, "getsebool", "haproxy_connect_any")
	if err != nil || !strings.HasSuffix(strings.TrimSpace(string(out)), "off") {
		return nil
	}
	return []string{"SELinux is enforcing and haproxy_connect_any is off: backends on non-standard ports will be refused"}
}

// SELinuxReport describes the SELinux mode and the haproxy_connect_any boolean.
// It only reads; it never runs setsebool or setenforce.
func (d *rockyDriver) SELinuxReport(ctx context.Context) (string, error) {
	mode, errOut, err := d.x.Run(ctx, nil, "getenforce")
	if err != nil {
		return "", fmt.Errorf("getenforce: %w: %s", err, strings.TrimSpace(string(errOut)))
	}
	boolOut, errOut, err := d.x.Run(ctx, nil, "getsebool", "haproxy_connect_any")
	if err != nil {
		return "", fmt.Errorf("getsebool haproxy_connect_any: %w: %s", err, strings.TrimSpace(string(errOut)))
	}
	return fmt.Sprintf("SELinux %s; %s", strings.TrimSpace(string(mode)), strings.TrimSpace(string(boolOut))), nil
}
