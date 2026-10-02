package haproxy

import (
	"context"
	"strings"
	"testing"
)

// driverCase pairs a driver with the scripted exec backing it, plus a path
// inside that driver's own certs dir, so the conformance assertions run
// identically against every OS implementation through the Driver interface.
type driverCase struct {
	name string
	new  func(x *scriptedExec) Driver
}

func conformanceDrivers() []driverCase {
	return []driverCase{
		{"ubuntu", func(x *scriptedExec) Driver { return NewUbuntu(x, DriverOptions{}) }},
		{"rocky", func(x *scriptedExec) Driver { return NewRocky(x, DriverOptions{}) }},
		{"macos", func(x *scriptedExec) Driver { return NewMacOS(x, DriverOptions{}) }},
	}
}

// All three drivers must behave identically through the interface: a write is
// readable back, lists by name/size, removes, and propagates validate and
// reload failures. No OS-specific knowledge in the assertions.
func TestDriverConformance(t *testing.T) {
	ctx := context.Background()
	for _, dc := range conformanceDrivers() {
		t.Run(dc.name, func(t *testing.T) {
			x := newScriptedExec()
			x.pid = "100"
			d := dc.new(x)
			certPath := d.CertsDir() + "/svc.example.net-abc123def456.pem"
			body := []byte("cert\n" + scriptedPEMMarker + "\nkey\n")

			// write then read back
			if err := d.PrivilegedWrite(ctx, certPath, body, d.Ownership().CertMode); err != nil {
				t.Fatalf("write: %v", err)
			}
			got, err := d.PrivilegedRead(ctx, certPath)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(got) != string(body) {
				t.Fatalf("read back mismatch: %q", got)
			}
			if x.argvContains(scriptedPEMMarker) {
				t.Fatalf("contents leaked into argv")
			}

			// list
			infos, err := d.PrivilegedList(ctx, d.CertsDir())
			if err != nil {
				t.Fatalf("list: %v", err)
			}
			if len(infos) != 1 || infos[0].Size != int64(len(body)) {
				t.Fatalf("list wrong: %+v", infos)
			}

			// remove
			if err := d.PrivilegedRemove(ctx, certPath); err != nil {
				t.Fatalf("remove: %v", err)
			}
			if _, err := d.PrivilegedRead(ctx, certPath); err == nil {
				t.Fatalf("read after remove should fail")
			}

			// validate error propagation
			x.validateFail = true
			if err := d.Validate(ctx, d.ConfigPath()); err == nil {
				t.Fatalf("validate failure not propagated")
			}
			x.validateFail = false
			if err := d.Validate(ctx, d.ConfigPath()); err != nil {
				t.Fatalf("validate should pass when not failing: %v", err)
			}

			// reload error propagation
			x.reloadFail = true
			if err := d.Reload(ctx); err == nil {
				t.Fatalf("reload failure not propagated")
			}
			x.reloadFail = false
			if err := d.Reload(ctx); err != nil {
				t.Fatalf("reload should pass when not failing: %v", err)
			}
		})
	}
}

// NewDriver dispatches to the right implementation and refuses an unsupported
// OS without panicking.
func TestDriverNewDriverDispatch(t *testing.T) {
	x := newScriptedExec()
	cases := map[OS]OS{OSUbuntu: OSUbuntu, OSRocky: OSRocky, OSMacOS: OSMacOS}
	for in, want := range cases {
		d, err := NewDriver(in, x, DriverOptions{})
		if err != nil {
			t.Fatalf("NewDriver(%q): %v", in, err)
		}
		if d.OSKind() != want {
			t.Errorf("NewDriver(%q).OSKind = %q", in, d.OSKind())
		}
	}
	if _, err := NewDriver(OS("plan9"), x, DriverOptions{}); err == nil {
		t.Error("NewDriver must reject an unsupported OS")
	}
}

// First run on a fresh machine: the target's parent directory does not exist,
// so the driver must create it (through the exec layer, before the temp write),
// and file contents must still never reach argv.
func TestDriverWriteCreatesMissingParentDir(t *testing.T) {
	ctx := context.Background()
	for _, dc := range conformanceDrivers() {
		t.Run(dc.name, func(t *testing.T) {
			x := newScriptedExec()
			d := dc.new(x)
			certPath := d.CertsDir() + "/svc.example.net-abc123def456.pem"
			body := []byte("cert\n" + scriptedPEMMarker + "\nkey\n")
			if err := d.PrivilegedWrite(ctx, certPath, body, d.Ownership().CertMode); err != nil {
				t.Fatalf("write into a missing directory: %v", err)
			}
			calls := x.argvStrings()
			mk, tee := -1, -1
			for i, c := range calls {
				if strings.Contains(c, "mkdir -p") && strings.Contains(c, d.CertsDir()) && mk < 0 {
					mk = i
				}
				if strings.Contains(c, "tee ") && tee < 0 {
					tee = i
				}
			}
			if mk < 0 || tee < 0 || mk > tee {
				t.Fatalf("mkdir must precede the temp write: %v", calls)
			}
			wantMode := "-m 0750"
			if dc.name == "macos" {
				wantMode = "-m 0700"
			}
			if !strings.Contains(calls[mk], wantMode) {
				t.Errorf("certs dir mode: want %s in %q", wantMode, calls[mk])
			}
			if dc.name != "macos" && !strings.HasPrefix(calls[mk], "sudo ") {
				t.Errorf("mkdir must use sudo on Linux: %q", calls[mk])
			}
			if x.argvContains(scriptedPEMMarker) {
				t.Fatalf("contents leaked into argv")
			}
			// An existing directory is left alone: no second mkdir.
			x.reset()
			if err := d.PrivilegedWrite(ctx, certPath, body, d.Ownership().CertMode); err != nil {
				t.Fatalf("second write: %v", err)
			}
			if x.issued("mkdir") {
				t.Errorf("existing dir must not be re-created: %v", x.argvStrings())
			}
			// A config-mode file gets a world-readable directory.
			cfgDir := "/srv/other"
			x.reset()
			if err := d.PrivilegedWrite(ctx, cfgDir+"/haproxy.cfg", []byte("x"), 0o644); err != nil {
				t.Fatalf("config write: %v", err)
			}
			if !x.issued("-m 0755") {
				t.Errorf("config dir mode 0755 expected: %v", x.argvStrings())
			}
		})
	}
}
