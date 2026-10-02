package haproxy

import "testing"

func TestModelDefaultIsValid(t *testing.T) {
	m := DefaultModel()
	if m.Version != 1 {
		t.Fatalf("version = %d, want 1", m.Version)
	}
	if m.DefaultService.Name != "unified" {
		t.Fatalf("default service name = %q, want unified", m.DefaultService.Name)
	}
	if m.DefaultService.Upstream.Host != "127.0.0.1" || m.DefaultService.Upstream.Port != 8787 {
		t.Fatalf("default upstream = %+v, want 127.0.0.1:8787", m.DefaultService.Upstream)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("default model should validate: %v", err)
	}
}

func TestModelValidateStructural(t *testing.T) {
	// A service with no name is structurally invalid.
	m := DefaultModel()
	m.Services = []Service{{Name: "", ExposedPort: 443, Upstream: Upstream{Host: "127.0.0.1", Port: 8181}}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for empty service name")
	}

	// A service with an out-of-range port is invalid.
	m = DefaultModel()
	m.Services = []Service{{Name: "x", ExposedPort: 70000, Upstream: Upstream{Host: "127.0.0.1", Port: 8181}}}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for out-of-range exposed port")
	}

	// A well-formed service validates. (Uniqueness is B6's referential check,
	// not tested here.)
	m = DefaultModel()
	m.Services = []Service{{Name: "brandx", Enabled: true, FQDNs: []string{"brandx.cmdhome.net"}, ExposedPort: 443, Upstream: Upstream{Host: "hero.cmdhome.net", Port: 8181}}}
	if err := m.Validate(); err != nil {
		t.Fatalf("well-formed service should validate: %v", err)
	}
}
