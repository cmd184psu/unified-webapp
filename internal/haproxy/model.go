package haproxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// modelVersion is the schema version written to state.json.
const modelVersion = 1

// Upstream is the host:port a backend server points at.
type Upstream struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// DefaultService is the service HAProxy routes unmatched :443 traffic to
// (the unified webapp).
type DefaultService struct {
	Name     string   `json:"name"`
	Upstream Upstream `json:"upstream"`
	Check    bool     `json:"check"`
}

// Port is an extra exposed TLS port beyond 443, with an optional default
// service (by name) that receives traffic matching no host rule.
type Port struct {
	Port           int    `json:"port"`
	DefaultService string `json:"defaultService,omitempty"`
}

// CertRef references the certificate (by FQDN) a service is served with. The
// bundle itself lives in the crt-list, not in the model.
type CertRef struct {
	FQDN string `json:"fqdn"`
}

// Service is one routed upstream.
type Service struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	FQDNs       []string `json:"fqdns"`
	ExposedPort int      `json:"exposedPort"`
	Upstream    Upstream `json:"upstream"`
	Check       bool     `json:"check"`
	Cert        CertRef  `json:"cert"`
	Extra       []string `json:"extra"`
}

// RawSection is a config section the model does not understand, preserved
// verbatim (header plus trimmed body lines).
type RawSection struct {
	Header string   `json:"header"`
	Lines  []string `json:"lines"`
}

// Model is the editable HAProxy configuration, persisted as state.json.
type Model struct {
	Version        int            `json:"version"`
	Global         []Directive    `json:"global"`
	Defaults       []Directive    `json:"defaults"`
	DefaultService DefaultService `json:"defaultService"`
	// HTTPServerClose closes the backend connection after every response instead
	// of reusing it. Off by default.
	HTTPServerClose bool `json:"httpServerClose,omitempty"`
	Ports          []Port         `json:"ports"`
	Services       []Service      `json:"services"`
	RawSections    []RawSection   `json:"rawSections"`
}

// DefaultModel returns a fresh model: unified on 127.0.0.1:8787 as the :443
// default service, no extra ports or services.
func DefaultModel() *Model {
	return &Model{
		Version: modelVersion,
		Global: []Directive{
			{Key: "maxconn", Value: "2000"},
		},
		Defaults: []Directive{
			{Key: "mode", Value: "http"},
			{Key: "timeout", Value: "connect 20000ms"},
			{Key: "timeout", Value: "server 1h"},
			{Key: "timeout", Value: "client 1h"},
			{Key: "timeout", Value: "tunnel 1h"},
		},
		DefaultService: DefaultService{
			Name:     "unified",
			Upstream: Upstream{Host: "127.0.0.1", Port: 8787},
			Check:    true,
		},
		Ports:       []Port{},
		Services:    []Service{},
		RawSections: []RawSection{},
	}
}

func validPort(p int) bool { return p >= 1 && p <= 65535 }

// Validate performs structural checks only (names present, ports in range,
// upstreams complete). Referential checks (uniqueness, FQDN clashes) belong
// to the validation layer above.
func (m *Model) Validate() error {
	if strings.TrimSpace(m.DefaultService.Name) == "" {
		return fmt.Errorf("default service: name is required")
	}
	if err := validUpstream(m.DefaultService.Upstream); err != nil {
		return fmt.Errorf("default service: %w", err)
	}
	for _, p := range m.Ports {
		if !validPort(p.Port) {
			return fmt.Errorf("port %d out of range 1-65535", p.Port)
		}
	}
	for i, s := range m.Services {
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("service %d: name is required", i+1)
		}
		if !validPort(s.ExposedPort) {
			return fmt.Errorf("service %q: exposed port %d out of range 1-65535", s.Name, s.ExposedPort)
		}
		if err := validUpstream(s.Upstream); err != nil {
			return fmt.Errorf("service %q: %w", s.Name, err)
		}
	}
	return nil
}

func validUpstream(u Upstream) error {
	if strings.TrimSpace(u.Host) == "" {
		return fmt.Errorf("upstream host is required")
	}
	if !validPort(u.Port) {
		return fmt.Errorf("upstream port %d out of range 1-65535", u.Port)
	}
	return nil
}

// clone returns a deep copy via JSON round trip.
func (m *Model) clone() *Model {
	b, _ := json.Marshal(m)
	out := &Model{}
	_ = json.Unmarshal(b, out)
	return out
}
