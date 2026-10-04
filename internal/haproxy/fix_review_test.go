package haproxy

import (
	"encoding/json"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Defect 1: Remove must not delete a cert the LIVE crt-list references ---

func TestCertRemoveRefusedWhileLiveCrtListReferences(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
	name, _ := x.pull("7")
	x.json("POST", "/api/apply", "", 200, nil)
	live := string(x.drv.Files[x.drv.CrtListPath()])
	if !strings.Contains(live, name) {
		t.Fatalf("live crt-list does not list the cert: %q", live)
	}
	x.json("PUT", "/api/certs/"+name+"/enabled", `{"enabled":false}`, 200, nil)

	rec := x.do("DELETE", "/api/certs/"+name, "")
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "Apply the disable first") {
		t.Fatalf("remove = %d %s, want 409 'Apply the disable first'", rec.Code, rec.Body.String())
	}
	if _, ok := x.drv.Files[x.certPath(name)]; !ok {
		t.Fatal("cert file deleted while the live crt-list references it")
	}

	x.json("POST", "/api/apply", "", 200, nil)
	if strings.Contains(string(x.drv.Files[x.drv.CrtListPath()]), name) {
		t.Fatal("crt-list still lists the disabled cert after apply")
	}
	x.json("DELETE", "/api/certs/"+name, "", 200, nil)
	if _, ok := x.drv.Files[x.certPath(name)]; ok {
		t.Error("file survived remove after the disable was applied")
	}
	var list struct {
		Certs []struct{ Name string } `json:"certs"`
	}
	x.json("GET", "/api/certs", "", 200, &list)
	if len(list.Certs) != 0 {
		t.Errorf("row survived remove: %+v", list)
	}
}

func TestCertRemoveSerializedWithApply(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
	name, _ := x.pull("7")
	x.json("PUT", "/api/certs/"+name+"/enabled", `{"enabled":false}`, 200, nil)

	drv := &blockingWriteDriver{
		fakeDriver: x.drv, blockPath: x.drv.ConfigPath(),
		reached: make(chan struct{}), release: make(chan struct{}),
	}
	h, err := buildWithDriver(x.cfg, drv, nil)
	if err != nil {
		t.Fatal(err)
	}
	x.h = h

	applyDone := make(chan int, 1)
	go func() { applyDone <- x.do("POST", "/api/apply", "").Code }()
	<-drv.reached

	delDone := make(chan int, 1)
	go func() { delDone <- x.do("DELETE", "/api/certs/"+name, "").Code }()
	time.Sleep(100 * time.Millisecond)
	if firstIndex(calls(x.drv), "remove "+x.certPath(name)) >= 0 {
		t.Fatal("cert removed while Apply was mid-install (Remove not serialized with Apply)")
	}
	close(drv.release)
	if c := <-applyDone; c != 200 {
		t.Errorf("apply = %d", c)
	}
	if c := <-delDone; c != 200 {
		t.Errorf("delete = %d", c)
	}
}

// --- Defect 2: the stats socket is never above level user -------------------

func TestRenderIgnoresModelStatsSocket(t *testing.T) {
	m := &Model{Global: []Directive{{Key: "stats socket", Value: "/x level admin"}}}
	out := Render(m, nil, "/run/haproxy.sock", "", "/etc/haproxy/crt-list.txt")
	if strings.Contains(out, "level admin") || strings.Contains(out, "/x") {
		t.Errorf("model stats socket leaked into render:\n%s", out)
	}
	if n := strings.Count(out, "stats socket"); n != 1 || !strings.Contains(out, "level user") {
		t.Errorf("want exactly one generated level user socket line, got %d:\n%s", n, out)
	}
}

func TestRefcheckRejectsModelStatsSocket(t *testing.T) {
	m := &Model{Global: []Directive{{Key: "stats socket", Value: "/x level admin"}}}
	issues := CheckModel(m, nil)
	if !hasErrorIssue(issues) {
		t.Fatalf("no error for a global stats socket: %+v", issues)
	}
}

func TestApplyRejectsModelStatsSocket(t *testing.T) {
	x := newHX(t, false)
	var m Model
	x.json("GET", "/api/model", "", 200, &m)
	m.Global = append(m.Global, Directive{Key: "stats socket", Value: "/x level admin"})
	b, _ := json.Marshal(m)
	x.json("PUT", "/api/model", string(b), 200, nil)
	rec := x.do("POST", "/api/apply", "")
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "stats socket") {
		t.Fatalf("apply = %d %s, want 409 naming the stats socket", rec.Code, rec.Body.String())
	}
}

// --- Finding 3: tee must not echo key bytes into captured stdout ------------

func TestDiscardsStdoutFor(t *testing.T) {
	yes := [][]string{{"tee", "f"}, {"sudo", "tee", "f"}, {"sudo", "-n", "tee", "f"}}
	for _, c := range yes {
		if !discardsStdout(c[0], c[1:]...) {
			t.Errorf("%v: want stdout discarded", c)
		}
	}
	no := [][]string{{"cat", "f"}, {"sudo", "cat", "f"}, {"sudo", "-n", "install", "x"}, {"sudo"}, {"haproxy", "-c"}}
	for _, c := range no {
		if discardsStdout(c[0], c[1:]...) {
			t.Errorf("%v: want stdout kept", c)
		}
	}
}

func TestRealExecTeeCapturesNoStdout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "out")
	const secret = "SENTINEL-PRIVATE-KEY-BYTES"
	out, _, err := NewExec().Run(context.Background(), []byte(secret), "tee", p)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("tee stdout captured %q", out)
	}
	if b, _ := os.ReadFile(p); string(b) != secret {
		t.Errorf("file = %q", b)
	}
}
