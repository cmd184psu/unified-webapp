package haproxy

import (
	"context"
	"strings"
	"testing"
)

func TestRockyDefaultPathsAndSystemd(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewRocky(x, DriverOptions{})
	if d.OSKind() != OSRocky {
		t.Errorf("OSKind = %q, want rocky", d.OSKind())
	}
	if d.ConfigPath() != "/etc/haproxy/haproxy.cfg" {
		t.Errorf("ConfigPath = %q", d.ConfigPath())
	}
	if d.CertsDir() != "/etc/haproxy/certs" {
		t.Errorf("CertsDir = %q", d.CertsDir())
	}
	if err := d.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !x.issued("sudo systemctl reload haproxy") {
		t.Errorf("Rocky reload: %v", x.argvStrings())
	}
}

func TestRockyOwnershipAndBaseline(t *testing.T) {
	d := NewRocky(newScriptedExec(), DriverOptions{})
	o := d.Ownership()
	if o.User != "root" || o.Group != "haproxy" || o.ConfigMode != 0o644 || o.CertMode != 0o640 {
		t.Errorf("rocky ownership/modes wrong: %+v", o)
	}
	hasChroot := false
	for _, dir := range d.BaselineGlobal() {
		if dir.Key == "chroot" {
			hasChroot = true
		}
	}
	if !hasChroot {
		t.Error("rocky baseline should include chroot")
	}
}

// The SELinux helper must be read-only: it reads getenforce and the
// haproxy_connect_any boolean and returns a report, and it NEVER runs setsebool
// or setenforce.
func TestRockySELinuxReportIsReadOnly(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewRocky(x, DriverOptions{})

	r, ok := d.(interface {
		SELinuxReport(context.Context) (string, error)
	})
	if !ok {
		t.Fatal("Rocky driver must expose a SELinuxReport helper")
	}
	report, err := r.SELinuxReport(ctx)
	if err != nil {
		t.Fatalf("SELinuxReport: %v", err)
	}
	if report == "" {
		t.Error("SELinuxReport returned empty")
	}
	if !x.issued("getenforce") {
		t.Errorf("SELinuxReport should read getenforce: %v", x.argvStrings())
	}
	if !x.issued("getsebool") || !strings.Contains(strings.Join(x.argvStrings(), " "), "haproxy_connect_any") {
		t.Errorf("SELinuxReport should read haproxy_connect_any: %v", x.argvStrings())
	}
	if x.argvContains("setsebool") || x.argvContains("setenforce") {
		t.Errorf("SELinuxReport must NEVER change policy: %v", x.argvStrings())
	}
}

func rockyDiag(t *testing.T, mode, boolVal string) ([]string, *scriptedExec) {
	t.Helper()
	x := newScriptedExec()
	x.selinuxMode, x.selinuxBool = mode, boolVal
	return NewRocky(x, DriverOptions{}).Diagnostics(context.Background()), x
}

func TestRockyDiagnosticsReportsEnforcingBooleanOff(t *testing.T) {
	lines, x := rockyDiag(t, "Enforcing", "off")
	if len(lines) != 1 || !strings.Contains(lines[0], "haproxy_connect_any") || !strings.Contains(lines[0], "non-standard ports") {
		t.Fatalf("diagnostics = %q", lines)
	}
	if x.argvContains("setsebool") || x.argvContains("setenforce") {
		t.Errorf("Diagnostics must NEVER change policy: %v", x.argvStrings())
	}
}

func TestRockyDiagnosticsQuietWhenHealthy(t *testing.T) {
	if lines, _ := rockyDiag(t, "Permissive", "off"); len(lines) != 0 {
		t.Errorf("permissive: %q", lines)
	}
	if lines, _ := rockyDiag(t, "Enforcing", "on"); len(lines) != 0 {
		t.Errorf("boolean on: %q", lines)
	}
}

func TestUbuntuMacOSDiagnosticsNil(t *testing.T) {
	ctx := context.Background()
	if l := NewUbuntu(newScriptedExec(), DriverOptions{}).Diagnostics(ctx); l != nil {
		t.Errorf("ubuntu: %q", l)
	}
	if l := NewMacOS(newScriptedExec(), DriverOptions{}).Diagnostics(ctx); l != nil {
		t.Errorf("macos: %q", l)
	}
}
