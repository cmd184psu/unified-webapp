package haproxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageRewriteShiftsPortsAndSocket(t *testing.T) {
	in := "global\n    stats socket /run/haproxy.sock mode 660 level user\nfrontend a\n    bind *:443 ssl crt-list /x\nfrontend b\n  bind *:8443 ssl crt /c.pem\nfrontend stats\n    bind *:1936\nfrontend c\n    bind [::]:443,0.0.0.0:80\n    bind unix@/run/x.sock\n"
	out := StageRewrite(in)
	for _, want := range []string{"bind *:10443 ssl", "bind *:18443 ssl", "bind *:11936", "bind [::]:10443,0.0.0.0:10080", "bind unix@/run/x.sock"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, ":443 ") || strings.Contains(out, "stats socket") {
		t.Errorf("a live port or socket survived:\n%s", out)
	}
}


func TestApplyNeedsCertsIsCalmAndTouchesNothing(t *testing.T) {
	drv := newFakeDriver()
	ap, _, _, _ := newTestApplier(t, drv, ApplyOptions{RequireCerts: true})
	res, err := ap.Apply(context.Background())
	if err != nil || res.Outcome != OutcomeNeedsCerts || res.Applied {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	for _, c := range calls(drv) {
		if strings.HasPrefix(c, "write ") || c == "reload" || c == "restart" || strings.HasPrefix(c, "remove ") {
			t.Errorf("a live change was made: %v", calls(drv))
		}
	}
	chk, err := ap.Check(context.Background())
	if err != nil || chk.OK || !chk.NeedsCerts {
		t.Fatalf("check = %+v, %v", chk, err)
	}
}

func TestStagedFilesAreKeptAndPortsShifted(t *testing.T) {
	drv := newFakeDriver()
	stage := t.TempDir()
	ap, _, certs, _ := newTestApplier(t, drv, ApplyOptions{StagingDir: stage})
	_ = certs
	if r, err := ap.Check(context.Background()); err != nil || !r.OK {
		t.Fatalf("check = %+v, %v", r, err)
	}
	cfg, err := os.ReadFile(filepath.Join(stage, "haproxy.cfg"))
	if err != nil {
		t.Fatal("staged haproxy.cfg was not kept:", err)
	}
	if !strings.Contains(string(cfg), "bind *:10443") || strings.Contains(string(cfg), "bind *:443") {
		t.Errorf("staged config still binds the live port:\n%s", cfg)
	}
	if _, err := os.Stat(filepath.Join(stage, "crt-list.txt")); err != nil {
		t.Error("staged crt-list.txt was not kept")
	}
}

func TestFriendlyProbeBlocksOnUntrusted(t *testing.T) {
	drv := newFakeDriver()
	tester := func(ctx context.Context, cfg string, hosts []string) ([]ProbeResult, error) {
		return []ProbeResult{{Host: "a.example.com", Trusted: false, Detail: "not trusted"}}, nil
	}
	ap, _, _, _ := newTestApplier(t, drv, ApplyOptions{StageTest: tester})
	r, err := ap.Check(context.Background())
	if err != nil || r.OK || !strings.Contains(r.Message, "a.example.com") {
		t.Fatalf("check = %+v, %v", r, err)
	}
}

func TestEverythingInCertsDirIsIncludedAutomatically(t *testing.T) {
	drv := newFakeDriver()
	drv.Files[drv.CertsDir()+"/a.example.com.pem"] = []byte("x")
	drv.Files[drv.CertsDir()+"/b.example.com.pem"] = []byte("x")
	drv.Files[drv.CertsDir()+"/notes.txt"] = []byte("x")
	ap, _, certs, _ := newTestApplier(t, drv, ApplyOptions{RequireCerts: true})
	chk, err := ap.Check(context.Background())
	if err != nil || chk.NeedsCerts {
		t.Fatalf("check = %+v, %v", chk, err)
	}
	list, err := certs.CrtList()
	if err != nil || !strings.Contains(list, "a.example.com.pem") || !strings.Contains(list, "b.example.com.pem") || strings.Contains(list, "notes.txt") {
		t.Fatalf("crt-list = %q, %v", list, err)
	}
}

func TestShortVersionIsJustTheNumber(t *testing.T) {
	raw := "HAProxy version 2.4.22-f8e3218 2023/02/14 - https://haproxy.org/\nStatus: long-term supported branch\nRunning on: Linux 5.14.0"
	if got := shortVersion(raw); got != "2.4.22" {
		t.Errorf("shortVersion = %q", got)
	}
	if got := shortVersion("weird output\nsecond line"); got != "weird output" {
		t.Errorf("fallback = %q", got)
	}
}

func TestHTTPServerCloseIsOffUnlessChosen(t *testing.T) {
	m := DefaultModel()
	off := Render(m, nil, "", "", "/x")
	if strings.Contains(off, "\n    option http-server-close") || !strings.Contains(off, "# option http-server-close") {
		t.Errorf("off by default, as a comment:\n%s", off)
	}
	m.HTTPServerClose = true
	on := Render(m, nil, "", "", "/x")
	if !strings.Contains(on, "\n    option http-server-close\n") {
		t.Errorf("not written when on:\n%s", on)
	}
}

func TestOlderCopyOfACertMachineHostComesInDisabled(t *testing.T) {
	drv := newFakeDriver()
	drv.Files[drv.CertsDir()+"/smb.example.com.pem"] = []byte("x")
	_, _, certs, dataDir := newTestApplier(t, drv, ApplyOptions{})
	data := certStoreData{Certs: []CertEntry{{Name: "smb.example.com-aaaaaaaaaaaa.pem", Enabled: true, CertMachine: CertSource{ID: 7, FQDN: "smb.example.com"}}}}
	if err := certs.save(data); err != nil {
		t.Fatal(err)
	}
	_ = dataDir
	if _, err := certs.Import(context.Background(), []string{"smb.example.com.pem"}); err != nil {
		t.Fatal(err)
	}
	d, _ := certs.load()
	for _, e := range d.Certs {
		if e.Name == "smb.example.com.pem" && e.Enabled {
			t.Errorf("the older duplicate came in enabled: %+v", e)
		}
	}
}
