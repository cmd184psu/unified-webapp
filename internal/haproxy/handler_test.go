package haproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

const testCMKey = "cm-api-key-s3cret-77"

type hx struct {
	t       *testing.T
	h       http.Handler
	drv     *fakeDriver
	cm      *fakeCertMachine
	cfg     config.HaproxyConfig
	dataDir string
}

// newHX builds the real module handler over the fake driver. withCM wires a
// real CertMachine client at the fake CertMachine; otherwise no client.
func newHX(t *testing.T, withCM bool) *hx {
	t.Helper()
	cfg := goodConfig(t)
	cfg.CertMachine.APIKey = testCMKey
	cfg.ExpiryWarnDays = 30
	cfg.SSEMaxSubscribers = 2
	drv := newFakeDriver()
	cm := newFakeCertMachine()
	cm.expectBearer = testCMKey
	var client *CertMachineClient
	if withCM {
		srv := cm.start(t)
		var err error
		client, err = CertMachineNewClient(CertMachineSettings{URL: srv.URL, APIKey: CertMachineAPIKey(testCMKey)})
		if err != nil {
			t.Fatalf("client: %v", err)
		}
	}
	h, err := buildWithDriver(cfg, drv, client)
	if err != nil {
		t.Fatalf("buildWithDriver: %v", err)
	}
	return &hx{t: t, h: h, drv: drv, cm: cm, cfg: cfg, dataDir: cfg.DataDir}
}

func (x *hx) do(method, path, body string) *httptest.ResponseRecorder {
	x.t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	x.h.ServeHTTP(rec, r)
	return rec
}

func (x *hx) json(method, path, body string, wantStatus int, out any) {
	x.t.Helper()
	rec := x.do(method, path, body)
	if rec.Code != wantStatus {
		x.t.Fatalf("%s %s: status %d, want %d; body %s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			x.t.Fatalf("%s %s: decode: %v; body %s", method, path, err, rec.Body.String())
		}
	}
}

func (x *hx) certPath(name string) string { return filepath.Join(x.drv.CertsDir(), name) }

// pull pulls a cert from the fake CertMachine and returns the staged name.
func (x *hx) pull(id string) (name string, superseded []string) {
	x.t.Helper()
	var out struct {
		Name       string   `json:"name"`
		Superseded []string `json:"superseded"`
	}
	x.json("POST", "/api/certs/pull", `{"certmachineId":`+id+`,"note":"n"}`, 200, &out)
	return out.Name, out.Superseded
}

func TestHandlerStatus(t *testing.T) {
	x := newHX(t, false)
	var st struct {
		Module  string `json:"module"`
		Version string `json:"version"`
		Service struct {
			Active bool   `json:"active"`
			Detail string `json:"detail"`
		} `json:"service"`
		LastApply *struct{} `json:"lastApply"`
		Pending   bool      `json:"pending"`
	}
	x.json("GET", "/api/status", "", 200, &st)
	if st.Module != "haproxy" || !st.Service.Active || st.Version == "" {
		t.Fatalf("status = %+v", st)
	}
	if st.LastApply != nil {
		t.Errorf("lastApply = %+v before any apply, want null", st.LastApply)
	}
	if !st.Pending {
		t.Errorf("pending = false for a never-applied default model, want true")
	}
	x.json("POST", "/api/apply", "", 200, nil)
	x.json("GET", "/api/status", "", 200, &st)
	if st.Pending || st.LastApply == nil {
		t.Errorf("after apply: pending=%v lastApply=%v, want false/non-null", st.Pending, st.LastApply)
	}
}

func TestHandlerModelGetPutAndValidation(t *testing.T) {
	x := newHX(t, false)
	var m struct {
		Model
		Imported bool `json:"imported"`
	}
	x.json("GET", "/api/model", "", 200, &m)
	if m.Imported {
		t.Error("imported = true with no state.json")
	}
	if m.DefaultService.Name != DefaultModel().DefaultService.Name {
		t.Errorf("not the default model: %+v", m.Model)
	}

	m.DefaultService.Name = "renamed"
	body, _ := json.Marshal(m.Model)
	x.json("PUT", "/api/model", string(body), 200, nil)
	x.json("GET", "/api/model", "", 200, &m)
	if !m.Imported || m.DefaultService.Name != "renamed" {
		t.Errorf("after PUT: imported=%v name=%q", m.Imported, m.DefaultService.Name)
	}

	m.DefaultService.Name = ""
	bad, _ := json.Marshal(m.Model)
	rec := x.do("PUT", "/api/model", string(bad))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "name is required") {
		t.Errorf("invalid PUT: %d %s", rec.Code, rec.Body.String())
	}
	if x.do("PUT", "/api/model", "{not json").Code != 400 {
		t.Error("malformed PUT body not 400")
	}
	big := `{"x":"` + strings.Repeat("a", 2<<20) + `"}`
	if c := x.do("PUT", "/api/model", big).Code; c != 413 && c != 400 {
		t.Errorf("oversized PUT: %d, want 413/400", c)
	}
}

const liveCfg = `global
    maxconn 4000

defaults
    mode http

frontend https
    bind :443 ssl crt-list /etc/haproxy/crt-list.txt
    default_backend be_unified

backend be_unified
    server unified 127.0.0.1:8787 check
`

func TestHandlerImportCommitFalseAndTrue(t *testing.T) {
	x := newHX(t, false)
	x.drv.Files[x.drv.ConfigPath()] = []byte(liveCfg)

	var out struct {
		Model  Model        `json:"model"`
		Report ImportReport `json:"report"`
	}
	x.json("POST", "/api/import", `{"commit":false}`, 200, &out)
	if out.Model.DefaultService.Upstream.Port != 8787 {
		t.Errorf("imported model default upstream = %+v", out.Model.DefaultService.Upstream)
	}
	var m struct {
		Imported bool `json:"imported"`
	}
	x.json("GET", "/api/model", "", 200, &m)
	if m.Imported {
		t.Error("commit=false stored the model")
	}
	x.json("POST", "/api/import", `{"commit":true}`, 200, &out)
	x.json("GET", "/api/model", "", 200, &m)
	if !m.Imported {
		t.Error("commit=true did not store the model")
	}

	y := newHX(t, false) // no live config at all
	if c := y.do("POST", "/api/import", `{"commit":false}`).Code; c != 404 {
		t.Errorf("import with no live config: %d, want 404", c)
	}
}

func TestHandlerChangesRawCheck(t *testing.T) {
	x := newHX(t, false)
	var ch Changes
	x.json("GET", "/api/changes", "", 200, &ch)
	if !ch.HasChanges || ch.ConfigDiff == "" {
		t.Errorf("changes = %+v", ch)
	}
	var raw struct {
		Config  string `json:"config"`
		CrtList string `json:"crtList"`
	}
	x.json("GET", "/api/raw", "", 200, &raw)
	if !strings.Contains(raw.Config, "frontend") {
		t.Errorf("raw config = %q", raw.Config)
	}
	var cr CheckResult
	x.json("POST", "/api/check", "", 200, &cr)
	if !cr.OK {
		t.Errorf("check = %+v", cr)
	}
	x.drv.ValidateErr = errors.New("bad config line 3")
	x.json("POST", "/api/check", "", 200, &cr)
	if cr.OK || !strings.Contains(cr.Message, "bad config line 3") {
		t.Errorf("failing check = %+v", cr)
	}
}

func TestHandlerApplyStatusMapping(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(x *hx)
		status  int
		outcome Outcome
	}{
		{"applied", func(x *hx) {}, 200, OutcomeApplied},
		{"validation_failed", func(x *hx) { x.drv.ValidateErr = errors.New("nope") }, 200, OutcomeValidationFailed},
		{"rolled_back", func(x *hx) { x.drv.Active = false }, 200, OutcomeRolledBack},
		{"rollback_failed", func(x *hx) { x.drv.ReloadErr = errors.New("reload boom") }, 500, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newHX(t, false)
			c.setup(x)
			rec := x.do("POST", "/api/apply", "")
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d; %s", rec.Code, c.status, rec.Body.String())
			}
			var res ApplyResult
			var e struct {
				Error string `json:"error"`
			}
			if c.status == 200 {
				_ = json.Unmarshal(rec.Body.Bytes(), &res)
				if res.Outcome != c.outcome || res.Message == "" {
					t.Errorf("result = %+v", res)
				}
			} else {
				_ = json.Unmarshal(rec.Body.Bytes(), &e)
				if e.Error == "" {
					t.Errorf("500 without message: %s", rec.Body.String())
				}
			}
		})
	}

	t.Run("started_when_inactive", func(t *testing.T) {
		cfg := goodConfig(t)
		drv := newFakeDriver()
		drv.Active = false
		h, err := buildWithDriver(cfg, &startFlips{drv}, nil)
		if err != nil {
			t.Fatal(err)
		}
		x := &hx{t: t, h: h, drv: drv, cfg: cfg, dataDir: cfg.DataDir}
		rec := x.do("POST", "/api/apply", "")
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || raw["applied"] != true || raw["started"] != true {
			t.Errorf("status %d body %v, want applied+started", rec.Code, raw)
		}
		if countCall(calls(drv), "start") != 1 || countCall(calls(drv), "reload") != 0 {
			t.Errorf("inactive apply must start, not reload: %v", calls(drv))
		}
	})

	t.Run("no_changes", func(t *testing.T) {
		x := newHX(t, false)
		x.json("POST", "/api/apply", "", 200, nil)
		var res ApplyResult
		x.json("POST", "/api/apply", "", 200, &res)
		if res.Outcome != OutcomeNoChanges {
			t.Errorf("second apply = %+v", res)
		}
	})
}

func TestHandlerServiceActions(t *testing.T) {
	x := newHX(t, false)
	for _, p := range []string{"reload", "restart", "start"} {
		var out struct {
			OK bool `json:"ok"`
		}
		x.json("POST", "/api/"+p, "", 200, &out)
		if !out.OK {
			t.Errorf("%s: ok=false", p)
		}
	}
	x.drv.ReloadErr = errors.New("systemd said no")
	rec := x.do("POST", "/api/reload", "")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("failing reload: %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerBackupsListViewRestore(t *testing.T) {
	x := newHX(t, false)
	x.drv.Files[x.drv.ConfigPath()] = []byte(liveCfg)
	x.json("POST", "/api/apply", "", 200, nil)

	var list []Backup
	x.json("GET", "/api/backups", "", 200, &list)
	var cfgName string
	for _, b := range list {
		if b.Kind == backupKindCfg && b.Orig {
			cfgName = b.Name
		}
	}
	if cfgName == "" {
		t.Fatalf("no orig cfg backup in %+v", list)
	}
	var view struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	x.json("GET", "/api/backups/"+cfgName, "", 200, &view)
	if view.Name != cfgName || !strings.Contains(view.Content, "maxconn 4000") {
		t.Errorf("view = %+v", view)
	}
	if c := x.do("GET", "/api/backups/not-a-backup", "").Code; c != 400 && c != 404 {
		t.Errorf("view of a non-backup: %d", c)
	}
	var res ApplyResult
	x.json("POST", "/api/backups/"+cfgName+"/restore", "", 200, &res)
	if res.Outcome != OutcomeApplied {
		t.Errorf("restore = %+v", res)
	}
	x.drv.Active = false
	x.json("POST", "/api/backups/"+cfgName+"/restore", "", 200, &res)
	if res.Outcome == OutcomeApplied {
		t.Errorf("restore with inactive service = %+v, want not applied", res)
	}
}

func TestHandlerOpsSnapshot(t *testing.T) {
	x := newHX(t, false)
	x.json("POST", "/api/reload", "", 200, nil)
	var ops []OpEntry
	x.json("GET", "/api/ops", "", 200, &ops)
	if len(ops) == 0 {
		t.Error("ops snapshot empty after a reload")
	}
}

func TestHandlerOpsStreamEventAndCancel(t *testing.T) {
	x := newHX(t, false)
	srv := httptest.NewServer(x.h)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/ops/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		x.do("POST", "/api/reload", "")
	}()
	got := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "data:") && strings.Contains(sc.Text(), "reload") {
				got <- sc.Text()
				return
			}
		}
	}()
	select {
	case <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("no ops event streamed")
	}
	cancel()
	// After cancel the server-side subscription must be released: two more
	// streams (cap 2) must be accepted.
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 2; i++ {
		c2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		r2, _ := http.NewRequestWithContext(c2, "GET", srv.URL+"/api/ops/stream", nil)
		rs, err := http.DefaultClient.Do(r2)
		if err != nil || rs.StatusCode != 200 {
			t.Fatalf("stream %d after cancel: %v %v", i, err, rs)
		}
		defer rs.Body.Close()
	}
}

func TestHandlerOpsStreamRejectsPastCap(t *testing.T) {
	x := newHX(t, false) // cap 2
	srv := httptest.NewServer(x.h)
	defer srv.Close()
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/ops/stream", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("stream %d: %v %v", i, err, resp)
		}
		defer resp.Body.Close()
	}
	resp, err := http.Get(srv.URL + "/api/ops/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Errorf("third stream status %d, want 503", resp.StatusCode)
	}
}

func TestHandlerCertPullListToggleDelete(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(7, "app.example.com", "fp7", "active", []string{"app.example.com"})
	name, sup := x.pull("7")
	if name == "" || len(sup) != 0 {
		t.Fatalf("pull = %q %v", name, sup)
	}
	if _, ok := x.drv.Files[x.certPath(name)]; !ok {
		t.Fatal("pulled cert not written via the driver")
	}

	var list struct {
		Certs []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
			Missing bool   `json:"missing"`
			Details *struct {
				FQDN   string `json:"fqdn"`
				Status string `json:"status"`
			} `json:"details"`
			DetailsError string `json:"detailsError"`
		} `json:"certs"`
		Unmanaged int `json:"unmanaged"`
	}
	x.json("GET", "/api/certs", "", 200, &list)
	if len(list.Certs) != 1 || list.Certs[0].Name != name || !list.Certs[0].Enabled || list.Certs[0].Missing {
		t.Fatalf("list = %+v", list)
	}
	if d := list.Certs[0].Details; d == nil || d.FQDN != "app.example.com" || d.Status != "active" {
		t.Errorf("details = %+v", d)
	}

	x.json("PUT", "/api/certs/"+name+"/enabled", `{"enabled":false}`, 200, nil)
	x.json("GET", "/api/certs", "", 200, &list)
	if list.Certs[0].Enabled {
		t.Error("toggle did not disable")
	}
	if c := x.do("PUT", "/api/certs/nope/enabled", `{"enabled":true}`).Code; c != 404 {
		t.Errorf("toggle unknown: %d, want 404", c)
	}

	x.json("DELETE", "/api/certs/"+name, "", 200, nil)
	if _, ok := x.drv.Files[x.certPath(name)]; ok {
		t.Error("delete left the file")
	}
	if c := x.do("DELETE", "/api/certs/"+name, "").Code; c != 404 {
		t.Errorf("delete again: %d, want 404", c)
	}
}

func TestHandlerCertsListDetailsErrorNever500(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
	x.pull("7")
	delete(x.cm.entries, 7) // CertMachine now 404s the detail call
	var list struct {
		Certs []struct {
			DetailsError string `json:"detailsError"`
		} `json:"certs"`
	}
	x.json("GET", "/api/certs", "", 200, &list)
	if len(list.Certs) != 1 || list.Certs[0].DetailsError == "" {
		t.Errorf("list = %+v, want a detailsError", list)
	}
}

func TestHandlerDeleteRefusedWhileUsed(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
	name, _ := x.pull("7")
	var m Model
	x.json("GET", "/api/model", "", 200, &m)
	m.Services = append(m.Services, Service{ID: "s1", Name: "app", Enabled: true, FQDNs: []string{"app.example.com"},
		ExposedPort: 443, Upstream: Upstream{Host: "10.0.0.5", Port: 80}, Cert: CertRef{FQDN: "app.example.com"}})
	b, _ := json.Marshal(m)
	x.json("PUT", "/api/model", string(b), 200, nil)
	if c := x.do("DELETE", "/api/certs/"+name, "").Code; c != 409 {
		t.Fatalf("delete of an in-use cert: %d, want 409", c)
	}
	if _, ok := x.drv.Files[x.certPath(name)]; !ok {
		t.Error("refused delete still removed the file")
	}
}

func TestHandlerCertMachineCertsFiltering(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(1, "app.example.com", "f1", "active", []string{"app.example.com"})
	x.cm.addCert(2, "other.example.com", "f2", "active", []string{"other.example.com"})
	x.cm.addCert(3, "app.example.com", "f3", "archived", []string{"app.example.com"})
	x.cm.addCert(4, "wild.example.com", "f4", "active", []string{"*.example.com"})

	type row struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Covers bool   `json:"covers"`
	}
	var out struct {
		Certs []row `json:"certs"`
	}
	x.json("GET", "/api/certmachine/certs?forFqdn=app.example.com", "", 200, &out)
	ids := map[int64]bool{}
	for _, r := range out.Certs {
		ids[r.ID] = true
		if !r.Covers {
			t.Errorf("default list contains a non-covering cert %+v", r)
		}
	}
	if !ids[1] || !ids[4] || ids[2] || ids[3] || len(ids) != 2 {
		t.Errorf("default (covering, active) ids = %v, want {1,4}", ids)
	}

	x.json("GET", "/api/certmachine/certs?forFqdn=app.example.com&all=1", "", 200, &out)
	if len(out.Certs) != 4 {
		t.Fatalf("all=1 returned %d certs, want 4", len(out.Certs))
	}
	for _, r := range out.Certs {
		if r.ID == 2 && r.Covers {
			t.Error("cert 2 flagged covering")
		}
		if r.ID == 3 && r.Status != "archived" {
			t.Errorf("archived cert status = %q", r.Status)
		}
	}
}

func TestHandlerCertMachineUnconfigured409(t *testing.T) {
	x := newHX(t, false)
	for _, rt := range [][2]string{
		{"GET", "/api/certmachine/certs"},
		{"POST", "/api/certs/pull"},
		{"GET", "/api/certs/freshness"},
	} {
		rec := x.do(rt[0], rt[1], `{"certmachineId":1}`)
		if rec.Code != 409 || !strings.Contains(rec.Body.String(), "CertMachine is not configured") {
			t.Errorf("%s %s: %d %s", rt[0], rt[1], rec.Code, rec.Body.String())
		}
	}
	var list struct {
		Certs []any `json:"certs"`
	}
	x.json("GET", "/api/certs", "", 200, &list) // tracking list still works
}

func TestHandlerPullErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(x *hx)
		status int
		want   string
	}{
		{"tampered", func(x *hx) { x.cm.tamperBody = true }, 502, "integrity"},
		{"unauthorized", func(x *hx) { x.cm.status401 = true }, 502, "API key"},
		{"refused", func(x *hx) { x.cm.refuse409 = "export is disabled for this cert" }, 409, "export is disabled"},
		{"server", func(x *hx) { x.cm.status500 = true }, 502, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := newHX(t, true)
			x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
			c.setup(x)
			rec := x.do("POST", "/api/certs/pull", `{"certmachineId":7}`)
			if rec.Code != c.status {
				t.Fatalf("status %d, want %d; %s", rec.Code, c.status, rec.Body.String())
			}
			if !strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(c.want)) {
				t.Errorf("body %q lacks %q", rec.Body.String(), c.want)
			}
			if strings.Contains(rec.Body.String(), testCMKey) || strings.Contains(rec.Body.String(), "PRIVATE KEY") {
				t.Errorf("leak in %s", rec.Body.String())
			}
			for p := range x.drv.Files {
				if strings.HasPrefix(p, x.drv.CertsDir()) {
					t.Errorf("a failed pull wrote %s", p)
				}
			}
		})
	}

	x := newHX(t, true) // unreachable
	x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
	cl, _ := CertMachineNewClient(CertMachineSettings{URL: "http://127.0.0.1:1", APIKey: CertMachineAPIKey(testCMKey)})
	h, _ := buildWithDriver(x.cfg, x.drv, cl)
	x.h = h
	if c := x.do("POST", "/api/certs/pull", `{"certmachineId":7}`).Code; c != 502 {
		t.Errorf("unreachable: %d, want 502", c)
	}
}

func TestHandlerPullSupersedesAndApplyCleansUp(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(1, "app.example.com", "f1", "active", nil)
	old, _ := x.pull("1")
	x.cm.entries[1].cert.Status = "archived"
	e := x.cm.addCert(2, "app.example.com", "f2", "active", nil)
	e.body = append(e.body, []byte("v2")...)
	nw, sup := x.pull("2")
	if len(sup) != 1 || sup[0] != old || nw == old {
		t.Fatalf("superseded = %v (old %q new %q)", sup, old, nw)
	}
	x.json("POST", "/api/apply", "", 200, nil)
	if _, ok := x.drv.Files[x.certPath(old)]; ok {
		t.Error("superseded file survived a successful apply")
	}
	if _, ok := x.drv.Files[x.certPath(nw)]; !ok {
		t.Error("new cert file missing")
	}
	var list struct {
		Certs []struct{ Name string } `json:"certs"`
	}
	x.json("GET", "/api/certs", "", 200, &list)
	if len(list.Certs) != 1 || list.Certs[0].Name != nw {
		t.Errorf("certs after cleanup = %+v", list)
	}
}

func TestHandlerCoverageAndFreshness(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(1, "app.example.com", "f1", "active", []string{"app.example.com"})
	x.pull("1")
	var m Model
	x.json("GET", "/api/model", "", 200, &m)
	m.Services = []Service{
		{ID: "a", Name: "a", Enabled: true, FQDNs: []string{"app.example.com"}, ExposedPort: 443, Upstream: Upstream{Host: "h", Port: 1}, Cert: CertRef{FQDN: "app.example.com"}},
		{ID: "b", Name: "b", Enabled: true, FQDNs: []string{"nope.example.com"}, ExposedPort: 443, Upstream: Upstream{Host: "h", Port: 2}, Cert: CertRef{FQDN: "app.example.com"}},
	}
	b, _ := json.Marshal(m)
	x.json("PUT", "/api/model", string(b), 200, nil)

	var cov []CoverageResult
	x.json("GET", "/api/coverage", "", 200, &cov)
	got := map[string]CoverageStatus{}
	for _, c := range cov {
		got[c.Service] = c.Status
	}
	if got["a"] != CoverageCovered || got["b"] != CoverageWarning {
		t.Errorf("coverage = %v", got)
	}

	var fr []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	x.json("GET", "/api/certs/freshness", "", 200, &fr)
	if len(fr) != 1 || fr[0].Status != string(CertFreshUpToDate) {
		t.Errorf("freshness = %+v", fr)
	}
	x.cm.addCert(9, "app.example.com", "f9", "active", nil)
	x.cm.entries[1].cert.Status = "archived"
	x.json("GET", "/api/certs/freshness", "", 200, &fr)
	if fr[0].Status != string(CertFreshUpdateAvailable) {
		t.Errorf("freshness after reissue = %+v", fr)
	}

	// Unconfigured CertMachine: coverage is unknown, never an error.
	y := newHX(t, false)
	y.json("PUT", "/api/model", string(b), 200, nil)
	y.json("GET", "/api/coverage", "", 200, &cov)
	for _, c := range cov {
		if c.Status != CoverageUnknown {
			t.Errorf("unconfigured coverage = %+v, want unknown", c)
		}
	}
}

func TestHandlerMethodNotAllowed(t *testing.T) {
	x := newHX(t, true)
	for _, rt := range [][2]string{
		{"POST", "/api/status"}, {"DELETE", "/api/model"}, {"GET", "/api/apply"}, {"PUT", "/api/changes"},
		{"GET", "/api/check"}, {"GET", "/api/import"}, {"POST", "/api/ops"}, {"DELETE", "/api/backups"},
		{"GET", "/api/reload"}, {"POST", "/api/raw"}, {"GET", "/api/certs/pull"}, {"POST", "/api/coverage"},
		{"GET", "/api/backups/x/restore"}, {"POST", "/api/certs/freshness"}, {"POST", "/api/certmachine/certs"},
	} {
		if c := x.do(rt[0], rt[1], "").Code; c != 405 {
			t.Errorf("%s %s: %d, want 405", rt[0], rt[1], c)
		}
	}
}

// Every route's response, on a populated module, must carry no key material,
// no CertMachine API key and no OS-specific value.
func TestHandlerNoSecretOrOSLeakAcrossRoutes(t *testing.T) {
	x := newHX(t, true)
	x.drv.Files[x.drv.ConfigPath()] = []byte(liveCfg)
	x.cm.addCert(7, "app.example.com", "fp7", "active", []string{"app.example.com"})
	name, _ := x.pull("7")
	x.json("POST", "/api/apply", "", 200, nil)
	x.json("POST", "/api/reload", "", 200, nil)

	var bk []Backup
	x.json("GET", "/api/backups", "", 200, &bk)
	routes := []string{
		"GET /api/status", "GET /api/model", "GET /api/changes", "GET /api/raw", "GET /api/ops",
		"GET /api/backups", "GET /api/certs", "GET /api/certmachine/certs?forFqdn=app.example.com&all=1",
		"GET /api/coverage", "GET /api/certs/freshness",
	}
	for _, b := range bk {
		routes = append(routes, "GET /api/backups/"+b.Name)
	}
	routes = append(routes, "POST /api/check", "POST /api/apply", "POST /api/import", "POST /api/reload",
		"POST /api/certs/pull", "PUT /api/certs/"+name+"/enabled")
	banned := []string{"PRIVATE KEY", "secret-key-bytes", testCMKey, "rocky", "ubuntu", "macos", "darwin",
		"systemctl", "sudo", "launchd", "/opt/homebrew"}
	x.cm.tamperBody = true // also covers an error body
	for _, rt := range routes {
		parts := strings.SplitN(rt, " ", 2)
		body := ""
		switch {
		case strings.HasSuffix(parts[1], "/pull"):
			body = `{"certmachineId":7}`
		case strings.HasSuffix(parts[1], "/enabled"):
			body = `{"enabled":true}`
		case strings.HasSuffix(parts[1], "/import"):
			body = `{"commit":false}`
		}
		rec := x.do(parts[0], parts[1], body)
		lower := strings.ToLower(rec.Body.String())
		for _, bad := range banned {
			if strings.Contains(lower, strings.ToLower(bad)) {
				t.Errorf("%s leaks %q: %s", rt, bad, rec.Body.String())
			}
		}
	}
}

func TestHandlerUnsupportedOSStillScoped503(t *testing.T) {
	cfg := goodConfig(t)
	cfg.OS = "windows"
	h, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/model", nil))
	if rec.Code != 503 {
		t.Errorf("status %d, want 503", rec.Code)
	}
}

func TestBuildStateFilesUnderDataDir(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
	x.pull("7")
	if _, err := os.Stat(filepath.Join(x.dataDir, "certs.json")); err != nil {
		t.Errorf("certs.json: %v", err)
	}
}

// A cert the owner disabled on purpose is never swept by the post-Apply
// cleanup, even when a newer enabled cert exists for the same FQDN.
func TestHandlerManuallyDisabledCertSurvivesApplyCleanup(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(1, "app.example.com", "f1", "active", nil)
	old, _ := x.pull("1")
	x.json("PUT", "/api/certs/"+old+"/enabled", `{"enabled":false}`, 200, nil)
	x.cm.entries[1].cert.Status = "archived"
	e := x.cm.addCert(2, "app.example.com", "f2", "active", nil)
	e.body = append(e.body, []byte("v2")...)
	nw, sup := x.pull("2")
	if nw == old || len(sup) != 0 {
		t.Fatalf("manually disabled row must not be superseded: new %q old %q sup %v", nw, old, sup)
	}
	x.json("POST", "/api/apply", "", 200, nil)
	if _, ok := x.drv.Files[x.certPath(old)]; !ok {
		t.Error("manually disabled cert file was deleted by cleanup")
	}
	var list struct {
		Certs []struct{ Name string } `json:"certs"`
	}
	x.json("GET", "/api/certs", "", 200, &list)
	if len(list.Certs) != 2 {
		t.Errorf("manually disabled row was dropped: %+v", list.Certs)
	}
}

func TestStatsRouteReportsRowsFromSocket(t *testing.T) {
	f := startFakeStats(t, map[string]string{
		"show stat": readTestdata(t, "show_stat.csv"),
		"show info": readTestdata(t, "show_info.txt"),
	}, false)
	x := newHX(t, false)
	x.drv.StatsPath = f.Path
	var out struct {
		Available bool      `json:"available"`
		Message   string    `json:"message"`
		Info      StatsInfo `json:"info"`
		Rows      []StatRow `json:"rows"`
	}
	x.json("GET", "/api/stats", "", 200, &out)
	if !out.Available || out.Info.Version == "" || len(out.Rows) != 6 {
		t.Errorf("stats = %+v", out)
	}
	if rec := x.do("POST", "/api/stats", ""); rec.Code != 405 {
		t.Errorf("POST /api/stats = %d, want 405", rec.Code)
	}
	for _, c := range f.Commands() {
		if c != "show stat" && c != "show info" {
			t.Errorf("non-read-only command %q", c)
		}
	}
}

func TestStatsRouteUnavailableIs200WithMessage(t *testing.T) {
	x := newHX(t, false)
	x.drv.StatsPath = filepath.Join(t.TempDir(), "none.sock")
	var out struct {
		Available bool       `json:"available"`
		Message   string     `json:"message"`
		Rows      []StatRow  `json:"rows"`
		Info      *StatsInfo `json:"info"`
	}
	x.json("GET", "/api/stats", "", 200, &out)
	if out.Available || out.Message == "" || out.Rows == nil {
		t.Errorf("unavailable = %+v (rows must be [] not null)", out)
	}
}

func TestRawUsesStatsSocketOwner(t *testing.T) {
	x := newHX(t, false)
	x.drv.StatsOwner = "appuser"
	var raw struct {
		Config string `json:"config"`
	}
	x.json("GET", "/api/raw", "", 200, &raw)
	if !strings.Contains(raw.Config, "level user user appuser") {
		t.Errorf("raw config lacks the owner:\n%s", raw.Config)
	}
}

func TestStatusIncludesDiagnostics(t *testing.T) {
	x := newHX(t, false)
	x.drv.DiagnosticLines = []string{"SELinux is enforcing and haproxy_connect_any is off: backends on non-standard ports will be refused"}
	var st struct {
		Diagnostics []string `json:"diagnostics"`
	}
	x.json("GET", "/api/status", "", 200, &st)
	if len(st.Diagnostics) != 1 || !strings.Contains(st.Diagnostics[0], "haproxy_connect_any") {
		t.Fatalf("diagnostics = %q", st.Diagnostics)
	}
}

func TestStatusDiagnosticsEmptyIsArray(t *testing.T) {
	x := newHX(t, false)
	rec := x.do("GET", "/api/status", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"diagnostics":[]`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestStatusSurvivesDiagnosticsPanic(t *testing.T) {
	x := newHX(t, false)
	x.drv.DiagnosticsPanic = true
	rec := x.do("GET", "/api/status", "")
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"diagnostics":[]`) {
		t.Errorf("want empty diagnostics: %s", rec.Body.String())
	}
	if ops := x.do("GET", "/api/ops", ""); !strings.Contains(ops.Body.String(), "diagnostics") {
		t.Errorf("failure not logged to the op log: %s", ops.Body.String())
	}
}
