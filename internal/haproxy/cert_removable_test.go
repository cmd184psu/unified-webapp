package haproxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

const (
	reasonLive    = "Listed in the live configuration. Disable it, then Apply, before removing it."
	reasonService = "Enabled and used by service app."
	reasonUnread  = "Could not read the live configuration."
)

type removableRow struct {
	Name        string `json:"name"`
	Removable   bool   `json:"removable"`
	InUseReason string `json:"inUseReason"`
}

func (x *hx) removable() []removableRow {
	x.t.Helper()
	var list struct {
		Certs []removableRow `json:"certs"`
	}
	x.json("GET", "/api/certs", "", 200, &list)
	return list.Certs
}

// crtListFailDriver fails reads of the crt-list with a non-not-found error.
type crtListFailDriver struct{ *fakeDriver }

func (d *crtListFailDriver) PrivilegedRead(ctx context.Context, path string) ([]byte, error) {
	if path == d.CrtListPath() {
		d.mu.Lock()
		d.record("read " + path)
		d.mu.Unlock()
		return nil, errors.New("permission denied")
	}
	return d.fakeDriver.PrivilegedRead(ctx, path)
}

func (x *hx) addServiceUsing(fqdn string) {
	x.t.Helper()
	var m Model
	x.json("GET", "/api/model", "", 200, &m)
	m.Services = append(m.Services, Service{ID: "s1", Name: "app", Enabled: true, FQDNs: []string{fqdn},
		ExposedPort: 443, Upstream: Upstream{Host: "10.0.0.5", Port: 80}, Cert: CertRef{FQDN: fqdn}})
	b, _ := json.Marshal(m)
	x.json("PUT", "/api/model", string(b), 200, nil)
}

func (x *hx) countCrtListReads() int {
	n := 0
	for _, c := range calls(x.drv) {
		if c == "read "+x.drv.CrtListPath() {
			n++
		}
	}
	return n
}

// scenario sets up one cert and returns its name.
type removableScenario struct {
	name      string
	setup     func(x *hx) string
	removable bool
	reason    string
	failRead  bool
}

func removableScenarios() []removableScenario {
	return []removableScenario{
		{name: "plain disabled, not live", removable: true, setup: func(x *hx) string {
			n, _ := x.pull("7")
			x.json("PUT", "/api/certs/"+n+"/enabled", `{"enabled":false}`, 200, nil)
			return n
		}},
		{name: "in live crt-list", reason: reasonLive, setup: func(x *hx) string {
			n, _ := x.pull("7")
			x.json("POST", "/api/apply", "", 200, nil)
			x.json("PUT", "/api/certs/"+n+"/enabled", `{"enabled":false}`, 200, nil)
			return n
		}},
		{name: "live then applied away", removable: true, setup: func(x *hx) string {
			n, _ := x.pull("7")
			x.json("POST", "/api/apply", "", 200, nil)
			x.json("PUT", "/api/certs/"+n+"/enabled", `{"enabled":false}`, 200, nil)
			x.json("POST", "/api/apply", "", 200, nil)
			return n
		}},
		{name: "enabled and used by service", reason: reasonService, setup: func(x *hx) string {
			n, _ := x.pull("7")
			x.addServiceUsing("app.example.com")
			return n
		}},
		{name: "live crt-list unreadable", reason: reasonUnread, failRead: true, setup: func(x *hx) string {
			n, _ := x.pull("7")
			x.json("PUT", "/api/certs/"+n+"/enabled", `{"enabled":false}`, 200, nil)
			return n
		}},
	}
}

func TestCertListRemovableAndDeleteAgree(t *testing.T) {
	for _, sc := range removableScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			x := newHX(t, true)
			x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
			name := sc.setup(x)
			if sc.failRead {
				h, err := buildWithDriver(x.cfg, &crtListFailDriver{x.drv}, nil)
				if err != nil {
					t.Fatal(err)
				}
				x.h = h
			}
			rows := x.removable()
			if len(rows) != 1 || rows[0].Name != name {
				t.Fatalf("rows = %+v", rows)
			}
			if rows[0].Removable != sc.removable || rows[0].InUseReason != sc.reason {
				t.Fatalf("row = %+v, want removable=%v reason=%q", rows[0], sc.removable, sc.reason)
			}
			code := x.do("DELETE", "/api/certs/"+name, "").Code
			if sc.removable && code != 200 {
				t.Errorf("delete = %d, want 200 (list said removable)", code)
			}
			if !sc.removable && code == 200 {
				t.Errorf("delete = 200, but the list said in use")
			}
			if !sc.removable && !sc.failRead && code != 409 {
				t.Errorf("delete = %d, want 409 backstop", code)
			}
		})
	}
}

func TestCertListReadsLiveCrtListOncePerRequest(t *testing.T) {
	x := newHX(t, true)
	x.cm.addCert(7, "app.example.com", "fp7", "active", nil)
	x.cm.addCert(8, "other.example.com", "fp8", "active", nil)
	x.cm.addCert(9, "third.example.com", "fp9", "active", nil)
	x.pull("7")
	x.pull("8")
	x.pull("9")
	before := x.countCrtListReads()
	if rows := x.removable(); len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if got := x.countCrtListReads() - before; got != 1 {
		t.Errorf("crt-list reads per list request = %d, want 1", got)
	}
}
