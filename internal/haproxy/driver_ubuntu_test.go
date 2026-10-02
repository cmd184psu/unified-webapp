package haproxy

import (
	"context"
	"testing"
)

const scriptedPEMMarker = "-----BEGIN PRIVATE KEY-----"

func TestUbuntuDefaultPaths(t *testing.T) {
	d := NewUbuntu(newScriptedExec(), DriverOptions{})
	if got := d.OSKind(); got != OSUbuntu {
		t.Errorf("OSKind = %q, want ubuntu", got)
	}
	if got := d.ConfigPath(); got != "/etc/haproxy/haproxy.cfg" {
		t.Errorf("ConfigPath = %q", got)
	}
	if got := d.CertsDir(); got != "/etc/haproxy/certs" {
		t.Errorf("CertsDir = %q", got)
	}
	if got := d.CrtListPath(); got != "/etc/haproxy/crt-list.txt" {
		t.Errorf("CrtListPath = %q", got)
	}
	if got := d.ServiceName(); got != "haproxy" {
		t.Errorf("ServiceName = %q", got)
	}
	if got := d.StatsSocketPath(); got == "" || got[:1] != "/" {
		t.Errorf("StatsSocketPath = %q", got)
	}
}

func TestUbuntuOptionOverrides(t *testing.T) {
	d := NewUbuntu(newScriptedExec(), DriverOptions{
		ConfigPath:  "/custom/haproxy.cfg",
		CertsDir:    "/custom/certs",
		ServiceName: "haproxy-custom",
	})
	if d.ConfigPath() != "/custom/haproxy.cfg" {
		t.Errorf("override ConfigPath not applied: %q", d.ConfigPath())
	}
	if d.CertsDir() != "/custom/certs" {
		t.Errorf("override CertsDir not applied: %q", d.CertsDir())
	}
	if d.ServiceName() != "haproxy-custom" {
		t.Errorf("override ServiceName not applied: %q", d.ServiceName())
	}
}

func TestUbuntuServiceCommands(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewUbuntu(x, DriverOptions{})

	if err := d.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !x.issued("sudo systemctl reload haproxy") {
		t.Errorf("Reload did not issue `sudo systemctl reload haproxy`; got %v", x.argvStrings())
	}
	x.reset()
	if err := d.Restart(ctx); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !x.issued("sudo systemctl restart haproxy") {
		t.Errorf("Restart commands: %v", x.argvStrings())
	}
	x.reset()
	if err := d.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !x.issued("sudo systemctl start haproxy") {
		t.Errorf("Start commands: %v", x.argvStrings())
	}
	x.reset()
	st, err := d.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.Active {
		t.Errorf("Status should report active")
	}
	if !x.issued("sudo systemctl is-active haproxy") {
		t.Errorf("Status commands: %v", x.argvStrings())
	}
}

func TestUbuntuStatusInactiveNoError(t *testing.T) {
	x := newScriptedExec()
	x.serviceActive = false
	d := NewUbuntu(x, DriverOptions{})
	st, err := d.Status(context.Background())
	if err != nil {
		t.Fatalf("Status of an inactive service must not error: %v", err)
	}
	if st.Active {
		t.Errorf("Status should report inactive")
	}
}

func TestUbuntuValidateUsesSudoHaproxy(t *testing.T) {
	x := newScriptedExec()
	d := NewUbuntu(x, DriverOptions{})
	if err := d.Validate(context.Background(), "/staging/haproxy.cfg"); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !x.issued("sudo haproxy -c -f /staging/haproxy.cfg") {
		t.Errorf("Validate command: %v", x.argvStrings())
	}
}

func TestUbuntuValidatePropagatesError(t *testing.T) {
	x := newScriptedExec()
	x.validateFail = true
	d := NewUbuntu(x, DriverOptions{})
	if err := d.Validate(context.Background(), "/staging/haproxy.cfg"); err == nil {
		t.Fatal("Validate must return the haproxy -c failure")
	}
}

func TestUbuntuVersionNoSudo(t *testing.T) {
	x := newScriptedExec()
	d := NewUbuntu(x, DriverOptions{})
	v, err := d.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v == "" {
		t.Error("Version returned empty")
	}
	if x.argvContains("sudo") {
		t.Errorf("Version must not use sudo: %v", x.argvStrings())
	}
}

func TestUbuntuOwnership(t *testing.T) {
	d := NewUbuntu(newScriptedExec(), DriverOptions{})
	o := d.Ownership()
	if o.User != "root" || o.Group != "haproxy" {
		t.Errorf("ownership = %s:%s, want root:haproxy", o.User, o.Group)
	}
	if o.ConfigMode != 0o644 || o.CertMode != 0o640 {
		t.Errorf("modes = %o/%o, want 0644/0640", o.ConfigMode, o.CertMode)
	}
}

func TestUbuntuBaselineGlobal(t *testing.T) {
	d := NewUbuntu(newScriptedExec(), DriverOptions{})
	base := d.BaselineGlobal()
	want := map[string]bool{"user": false, "group": false, "chroot": false, "log": false, "stats socket": false}
	for _, dir := range base {
		if _, ok := want[dir.Key]; ok {
			want[dir.Key] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("Ubuntu baseline missing %q directive; got %+v", k, base)
		}
	}
}

func TestUbuntuPrivilegedWriteStdinNotArgv(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewUbuntu(x, DriverOptions{})
	pem := []byte("cert-body\n" + scriptedPEMMarker + "\nkeybytes\n")
	if err := d.PrivilegedWrite(ctx, "/etc/haproxy/certs/x.pem", pem, 0o640); err != nil {
		t.Fatalf("PrivilegedWrite: %v", err)
	}
	if !x.stdinContains(scriptedPEMMarker) {
		t.Error("cert contents must travel over stdin")
	}
	if x.argvContains(scriptedPEMMarker) {
		t.Error("cert contents (private key marker) must NEVER appear in any argv")
	}
	if !x.issued("tee ") {
		t.Errorf("write should tee contents; got %v", x.argvStrings())
	}
	if !x.issued("mv ") {
		t.Errorf("write should rename atomically; got %v", x.argvStrings())
	}
	if !x.issued("chown root:haproxy") {
		t.Errorf("write should chown to the ownership policy; got %v", x.argvStrings())
	}
	// Read it back through the interface.
	got, err := d.PrivilegedRead(ctx, "/etc/haproxy/certs/x.pem")
	if err != nil {
		t.Fatalf("PrivilegedRead: %v", err)
	}
	if string(got) != string(pem) {
		t.Errorf("read back mismatch")
	}
}

func TestUbuntuPrivilegedListNamesSizesNeverContents(t *testing.T) {
	ctx := context.Background()
	x := newScriptedExec()
	d := NewUbuntu(x, DriverOptions{})
	if err := d.PrivilegedWrite(ctx, "/etc/haproxy/certs/a.pem", []byte("aaaa"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := d.PrivilegedWrite(ctx, "/etc/haproxy/certs/b.pem", []byte("bbbbbb"), 0o640); err != nil {
		t.Fatal(err)
	}
	x.reset()
	infos, err := d.PrivilegedList(ctx, "/etc/haproxy/certs")
	if err != nil {
		t.Fatalf("PrivilegedList: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("list returned %d files, want 2: %+v", len(infos), infos)
	}
	bySize := map[string]int64{}
	for _, fi := range infos {
		bySize[fi.Name] = fi.Size
		if fi.ModTime.IsZero() {
			t.Errorf("file %s has zero mtime", fi.Name)
		}
	}
	if bySize["a.pem"] != 4 || bySize["b.pem"] != 6 {
		t.Errorf("sizes wrong: %+v", bySize)
	}
	if x.issued("cat ") {
		t.Errorf("PrivilegedList must never read contents (no cat): %v", x.argvStrings())
	}
}

func TestStatsSocketOwnerIsCurrentAppUserOnLinux(t *testing.T) {
	old := currentUsername
	currentUsername = func() string { return "appuser" }
	defer func() { currentUsername = old }()
	for name, d := range map[string]Driver{
		"ubuntu": NewUbuntu(newScriptedExec(), DriverOptions{}),
		"rocky":  NewRocky(newScriptedExec(), DriverOptions{}),
	} {
		if got := d.StatsSocketOwner(); got != "appuser" {
			t.Errorf("%s StatsSocketOwner = %q, want appuser", name, got)
		}
	}
}
