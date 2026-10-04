package haproxy

import (
	"context"
	"strings"
	"testing"
)

func TestMacOSDefaultPaths(t *testing.T) {
	d := NewMacOS(newScriptedExec(), DriverOptions{})
	if d.OSKind() != OSMacOS {
		t.Errorf("OSKind = %q, want macos", d.OSKind())
	}
	if d.ConfigPath() != "/opt/homebrew/etc/haproxy.cfg" {
		t.Errorf("ConfigPath = %q", d.ConfigPath())
	}
	if d.CertsDir() != "/opt/homebrew/etc/haproxy/certs" {
		t.Errorf("CertsDir = %q", d.CertsDir())
	}
	if d.CrtListPath() != "/opt/homebrew/etc/haproxy/crt-list.txt" {
		t.Errorf("CrtListPath = %q", d.CrtListPath())
	}
	if !strings.HasPrefix(d.StatsSocketPath(), "/opt/homebrew/var") {
		t.Errorf("StatsSocketPath = %q, want under /opt/homebrew/var", d.StatsSocketPath())
	}
}

// macOS installs as the running user with no chown and 0600 certs.
func TestMacOSOwnershipNoChown(t *testing.T) {
	o := NewMacOS(newScriptedExec(), DriverOptions{}).Ownership()
	if o.User != "" || o.Group != "" {
		t.Errorf("macOS ownership must be empty (no chown): %+v", o)
	}
	if o.CertMode != 0o600 {
		t.Errorf("macOS cert mode = %o, want 0600", o.CertMode)
	}
}

func TestMacOSWriteNoSudoNoChown(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewMacOS(x, DriverOptions{})
	pem := []byte("cert\n" + scriptedPEMMarker + "\nkey\n")
	if err := d.PrivilegedWrite(ctx, "/opt/homebrew/etc/haproxy/certs/x.pem", pem, 0o600); err != nil {
		t.Fatalf("PrivilegedWrite: %v", err)
	}
	if x.argvContains("sudo") {
		t.Errorf("macOS write must not use sudo: %v", x.argvStrings())
	}
	if x.argvContains("chown") {
		t.Errorf("macOS write must not chown: %v", x.argvStrings())
	}
	if !x.stdinContains(scriptedPEMMarker) {
		t.Error("contents must travel over stdin")
	}
	if x.argvContains(scriptedPEMMarker) {
		t.Error("contents must never appear in argv")
	}
	got, err := d.PrivilegedRead(ctx, "/opt/homebrew/etc/haproxy/certs/x.pem")
	if err != nil || string(got) != string(pem) {
		t.Fatalf("read back: %v / %q", err, got)
	}
}

// The driver writes and owns a launchd plist running `haproxy -W -f <cfg> -p
// <pidfile>` with a raised open-files limit, and never the deprecated
// master-worker global keyword.
func TestMacOSPlistShape(t *testing.T) {
	d := NewMacOS(newScriptedExec(), DriverOptions{}).(*macosDriver)
	plist := d.macosRenderPlist()
	for _, want := range []string{"<string>-W</string>", "<string>-p</string>", "SoftResourceLimits", "HardResourceLimits", "NumberOfFiles", "net.cmdhome.unified.haproxy"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
	if strings.Contains(plist, "master-worker") {
		t.Errorf("plist must NOT use the removed master-worker keyword:\n%s", plist)
	}
}

// The macOS baseline and anything the driver introduces must never carry a
// non-wildcard bind (a non-root process cannot bind a specific address).
func TestMacOSNoNonWildcardBind(t *testing.T) {
	d := NewMacOS(newScriptedExec(), DriverOptions{})
	for _, dir := range d.BaselineGlobal() {
		if strings.Contains(dir.Key, "bind") || strings.Contains(dir.Value, "bind") {
			if strings.Contains(dir.Value, ":") && !strings.Contains(dir.Value, "*:") {
				t.Errorf("macOS baseline introduced a non-wildcard bind: %+v", dir)
			}
		}
	}
	// The plist command line must not contain a bind at all, and certainly not a
	// specific-address one.
	plist := d.(*macosDriver).macosRenderPlist()
	if strings.Contains(plist, "127.0.0.1:") || strings.Contains(plist, "bind") {
		t.Errorf("macOS launchd command line must not contain a bind:\n%s", plist)
	}
}

func TestMacOSBaselineLogStdout(t *testing.T) {
	d := NewMacOS(newScriptedExec(), DriverOptions{})
	foundLog := false
	for _, dir := range d.BaselineGlobal() {
		if dir.Key == "log" && strings.Contains(dir.Value, "stdout format raw") {
			foundLog = true
		}
		if dir.Key == "user" || dir.Key == "group" || dir.Key == "chroot" {
			t.Errorf("macOS baseline must not carry %q", dir.Key)
		}
	}
	if !foundLog {
		t.Errorf("macOS baseline should use `log stdout format raw local0`: %+v", d.BaselineGlobal())
	}
}

// Reload sends SIGUSR2 to the master pid read from the pidfile, with no sudo.
func TestMacOSReloadSIGUSR2(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	x.pid = "4242"
	d := NewMacOS(x, DriverOptions{})
	if err := d.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !x.issued("kill -USR2 4242") {
		t.Errorf("macOS reload must kill -USR2 <master pid>: %v", x.argvStrings())
	}
	if x.argvContains("sudo") {
		t.Errorf("macOS reload (user job) must not use sudo: %v", x.argvStrings())
	}
	if !atoiOK("4242") {
		t.Error("sanity")
	}
}

func TestMacOSRestartKickstart(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewMacOS(x, DriverOptions{})
	if err := d.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !x.issued("launchctl kickstart -k") {
		t.Errorf("macOS restart must `launchctl kickstart -k`: %v", x.argvStrings())
	}
	if !strings.Contains(strings.Join(x.argvStrings(), " "), "net.cmdhome.unified.haproxy") {
		t.Errorf("restart must target the job label: %v", x.argvStrings())
	}
}

func TestMacOSStartBootstrapsJob(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewMacOS(x, DriverOptions{})
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !x.issued("launchctl bootstrap") {
		t.Errorf("macOS start must bootstrap the job: %v", x.argvStrings())
	}
	// Start writes/owns the plist first.
	if !x.issued("tee ") {
		t.Errorf("macOS start must write the plist it owns: %v", x.argvStrings())
	}
}

func TestMacOSStatus(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewMacOS(x, DriverOptions{})
	st, err := d.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Active {
		t.Error("Status should report active when launchctl print shows running")
	}
	if !x.issued("launchctl print") {
		t.Errorf("Status should use launchctl print: %v", x.argvStrings())
	}
}

func TestMacOSValidateNoSudo(t *testing.T) {
	x := newScriptedExec()
	d := NewMacOS(x, DriverOptions{})
	if err := d.Validate(context.Background(), "/opt/homebrew/etc/haproxy.cfg"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if x.argvContains("sudo") {
		t.Errorf("macOS validate must not use sudo (files are user-owned): %v", x.argvStrings())
	}
	if !x.issued("haproxy -c -f /opt/homebrew/etc/haproxy.cfg") {
		t.Errorf("Validate command: %v", x.argvStrings())
	}
}

func TestMacOSStatsSocketOwnerEmpty(t *testing.T) {
	if got := NewMacOS(newScriptedExec(), DriverOptions{}).StatsSocketOwner(); got != "" {
		t.Errorf("macOS StatsSocketOwner = %q, want empty (same user, `user` option needs root)", got)
	}
}
