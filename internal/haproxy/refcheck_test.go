package haproxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func rcCert(fqdn string, enabled bool) CertManaged {
	return CertManaged{CertEntry: CertEntry{Name: "c-" + fqdn, Enabled: enabled, CertMachine: CertSource{ID: 1, FQDN: fqdn}}}
}

func rcSvc(id, name, port string, fqdns ...string) Service {
	p := 443
	if port != "" {
		p = atoiMust(port)
	}
	return Service{ID: id, Name: name, Enabled: true, FQDNs: fqdns, ExposedPort: p,
		Upstream: Upstream{Host: "127.0.0.1", Port: 9000}}
}

func atoiMust(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

func TestCheckModel(t *testing.T) {
	withCert := func(s Service, fqdn string) Service { s.Cert = CertRef{FQDN: fqdn}; return s }
	cases := []struct {
		name     string
		model    func() *Model
		certs    []CertManaged
		severity string // "" = no issues expected
		where    string
		contains string
	}{
		{"clean", func() *Model {
			m := DefaultModel()
			m.Services = []Service{withCert(rcSvc("s1", "a", "", "a.x.com"), "a.x.com")}
			return m
		}, []CertManaged{rcCert("a.x.com", true)}, "", "", ""},
		{"duplicate service name", func() *Model {
			m := DefaultModel()
			m.Services = []Service{rcSvc("s1", "a", "", "a.x.com"), rcSvc("s2", "a", "", "b.x.com")}
			return m
		}, nil, "error", "service:s2", "duplicate service name"},
		{"unsafe service name", func() *Model {
			m := DefaultModel()
			m.Services = []Service{rcSvc("s1", "Bad Name", "", "a.x.com")}
			return m
		}, nil, "error", "service:s1", "not a safe"},
		{"duplicate FQDN across ports", func() *Model {
			m := DefaultModel()
			m.Services = []Service{rcSvc("s1", "a", "", "a.x.com"), rcSvc("s2", "b", "8443", "A.X.com")}
			return m
		}, nil, "error", "service:s2", "also claimed"},
		{"FQDN claimed twice on one port", func() *Model {
			m := DefaultModel()
			m.Services = []Service{rcSvc("s1", "a", "8443", "a.x.com"), rcSvc("s2", "b", "8443", "a.x.com")}
			return m
		}, nil, "error", "service:s2", "on port 8443"},
		{"port default not on that port", func() *Model {
			m := DefaultModel()
			m.Ports = []Port{{Port: 8443, DefaultService: "a"}}
			m.Services = []Service{rcSvc("s1", "a", "", "a.x.com")}
			return m
		}, nil, "error", "port:8443", "default service"},
		{"port default unknown", func() *Model {
			m := DefaultModel()
			m.Ports = []Port{{Port: 8443, DefaultService: "ghost"}}
			return m
		}, nil, "error", "port:8443", "default service"},
		{"port default ok", func() *Model {
			m := DefaultModel()
			m.Ports = []Port{{Port: 8443, DefaultService: "a"}}
			m.Services = []Service{rcSvc("s1", "a", "8443", "a.x.com")}
			return m
		}, nil, "", "", ""},
		{"port out of range", func() *Model {
			m := DefaultModel()
			m.Ports = []Port{{Port: 70000}}
			return m
		}, nil, "error", "port:70000", "invalid"},
		{"extra port collides with 443 default", func() *Model {
			m := DefaultModel()
			m.Ports = []Port{{Port: 443, DefaultService: "x"}}
			return m
		}, nil, "error", "port:443", "default service"},
		{"duplicate extra port", func() *Model {
			m := DefaultModel()
			m.Ports = []Port{{Port: 8443}, {Port: 8443}}
			return m
		}, nil, "error", "port:8443", "more than once"},
		{"service exposed port invalid", func() *Model {
			m := DefaultModel()
			s := rcSvc("s1", "a", "", "a.x.com")
			s.ExposedPort = 99999
			m.Services = []Service{s}
			return m
		}, nil, "error", "service:s1", "exposed port"},
		{"cert missing", func() *Model {
			m := DefaultModel()
			m.Services = []Service{withCert(rcSvc("s1", "a", "", "a.x.com"), "a.x.com")}
			return m
		}, nil, "error", "service:s1", "no tracked certificate"},
		{"cert disabled warns", func() *Model {
			m := DefaultModel()
			m.Services = []Service{withCert(rcSvc("s1", "a", "", "a.x.com"), "a.x.com")}
			return m
		}, []CertManaged{rcCert("a.x.com", false)}, "warning", "service:s1", "disabled"},
		{"cert disabled but another enabled", func() *Model {
			m := DefaultModel()
			m.Services = []Service{withCert(rcSvc("s1", "a", "", "a.x.com"), "a.x.com")}
			return m
		}, []CertManaged{rcCert("a.x.com", false), rcCert("a.x.com", true)}, "", "", ""},
		{"no cert named is fine", func() *Model {
			m := DefaultModel()
			m.Services = []Service{rcSvc("s1", "a", "", "a.x.com")}
			return m
		}, nil, "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := CheckModel(c.model(), c.certs)
			if c.severity == "" {
				if len(issues) != 0 {
					t.Fatalf("issues = %+v, want none", issues)
				}
				return
			}
			for _, i := range issues {
				if i.Severity == c.severity && i.Where == c.where && strings.Contains(i.Message, c.contains) {
					return
				}
			}
			t.Fatalf("no %s issue at %q containing %q in %+v", c.severity, c.where, c.contains, issues)
		})
	}
}

func TestHandlerModelCheckRoute(t *testing.T) {
	x := newHX(t, false)
	var out struct {
		Issues []Issue `json:"issues"`
	}
	x.json("GET", "/api/model/check", "", 200, &out)
	if out.Issues == nil || len(out.Issues) != 0 {
		t.Fatalf("default model issues = %+v, want empty non-nil", out.Issues)
	}
	m := DefaultModel()
	m.Services = []Service{rcSvc("s1", "a", "", "a.x.com"), rcSvc("s2", "a", "", "b.x.com")}
	body, _ := json.Marshal(m)
	x.json("PUT", "/api/model", string(body), 200, nil)
	x.json("GET", "/api/model/check", "", 200, &out)
	if len(out.Issues) != 1 || out.Issues[0].Severity != "error" {
		t.Fatalf("issues = %+v", out.Issues)
	}
	if rec := x.do("POST", "/api/model/check", ""); rec.Code != 405 {
		t.Errorf("POST check: %d", rec.Code)
	}
}

func TestHandlerApplyBlocksOnErrorIssues(t *testing.T) {
	put := func(x *hx, m *Model) {
		b, _ := json.Marshal(m)
		x.json("PUT", "/api/model", string(b), 200, nil)
	}
	t.Run("error blocks with 409", func(t *testing.T) {
		x := newHX(t, false)
		m := DefaultModel()
		m.Services = []Service{rcSvc("s1", "a", "", "a.x.com"), rcSvc("s2", "a", "", "b.x.com")}
		put(x, m)
		var out struct {
			Error  string  `json:"error"`
			Issues []Issue `json:"issues"`
		}
		x.json("POST", "/api/apply", "", 409, &out)
		if out.Error == "" || len(out.Issues) == 0 {
			t.Fatalf("409 body = %+v", out)
		}
		for p := range x.drv.Files {
			if strings.HasSuffix(p, "haproxy.cfg") && strings.Contains(string(x.drv.Files[p]), "a.x.com") {
				t.Error("a blocked apply wrote the config")
			}
		}
	})
	t.Run("warning does not block", func(t *testing.T) {
		x := newHX(t, true)
		x.cm.addCert(1, "a.x.com", "f1", "active", nil)
		name, _ := x.pull("1")
		x.json("PUT", "/api/certs/"+name+"/enabled", `{"enabled":false}`, 200, nil)
		m := DefaultModel()
		s := rcSvc("s1", "a", "", "a.x.com")
		s.Cert = CertRef{FQDN: "a.x.com"}
		m.Services = []Service{s}
		put(x, m)
		var out ApplyResult
		x.json("POST", "/api/apply", "", 200, &out)
		if out.Outcome != OutcomeApplied {
			t.Fatalf("outcome = %+v", out)
		}
	})
}
