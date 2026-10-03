package haproxy

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Issue is one referential-check finding (FR-H6). Where names the thing it
// concerns: "service:<id>" (the name when a service has no id) or "port:<n>".
type Issue struct {
	Severity string `json:"severity"` // issueError | issueWarning
	Where    string `json:"where"`
	Message  string `json:"message"`
}

const (
	issueError   = "error"
	issueWarning = "warning"
)

var safeServiceName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func hasErrorIssue(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == issueError {
			return true
		}
	}
	return false
}

// CheckModel runs the referential checks over a model and the cert tracking
// rows. Errors block Apply; warnings do not.
func CheckModel(m *Model, certs []CertManaged) []Issue {
	issues := []Issue{}
	add := func(sev, where, format string, a ...any) {
		issues = append(issues, Issue{Severity: sev, Where: where, Message: fmt.Sprintf(format, a...)})
	}
	svcWhere := func(s Service) string {
		if s.ID != "" {
			return "service:" + s.ID
		}
		return "service:" + s.Name
	}

	for _, d := range m.Global {
		if d.Key == "stats socket" {
			add(issueError, "global", "the stats socket is managed by the editor and is always read-only; remove the %q directive", d.Key)
		}
	}

	byName := map[string]*Service{}
	type claim struct {
		name string
		port int
	}
	fqdnOwner := map[string]claim{}
	for i := range m.Services {
		s := &m.Services[i]
		where := svcWhere(*s)
		port := renderPort(s.ExposedPort)

		if _, dup := byName[s.Name]; dup {
			add(issueError, where, "duplicate service name %q", s.Name)
		} else {
			byName[s.Name] = s
		}
		if !safeServiceName.MatchString(s.Name) {
			add(issueError, where, "service name %q is not a safe HAProxy identifier (lowercase letters, digits, - and _, starting with a letter or digit)", s.Name)
		}
		if !validPort(port) {
			add(issueError, where, "service %q: exposed port %d is invalid (1-65535)", s.Name, s.ExposedPort)
		}

		for _, f := range s.FQDNs {
			key := strings.ToLower(strings.TrimSpace(f))
			if key == "" {
				continue
			}
			prev, seen := fqdnOwner[key]
			switch {
			case !seen:
				fqdnOwner[key] = claim{s.Name, port}
			case prev.port == port:
				add(issueError, where, "FQDN %q is claimed by %q and %q on port %d", f, prev.name, s.Name, port)
			default:
				add(issueError, where, "FQDN %q is also claimed by service %q (port %d)", f, prev.name, prev.port)
			}
		}

		if fq := strings.TrimSpace(s.Cert.FQDN); fq != "" {
			rows, enabled := 0, 0
			for _, c := range certs {
				if strings.EqualFold(c.CertMachine.FQDN, fq) {
					rows++
					if c.Enabled {
						enabled++
					}
				}
			}
			switch {
			case rows == 0:
				add(issueError, where, "service %q names certificate %q but there is no tracked certificate for it; install one on the Certificates tab", s.Name, fq)
			case enabled == 0:
				add(issueWarning, where, "service %q names certificate %q, which is disabled", s.Name, fq)
			}
		}
	}

	seenPort := map[int]bool{}
	for _, p := range m.Ports {
		where := fmt.Sprintf("port:%d", p.Port)
		if !validPort(p.Port) {
			add(issueError, where, "port %d is invalid (1-65535)", p.Port)
			continue
		}
		if seenPort[p.Port] {
			add(issueError, where, "port %d is listed more than once", p.Port)
		}
		seenPort[p.Port] = true
		if p.Port == 443 {
			add(issueError, where, "port 443 is always served by the default service %q; remove it from the extra ports", m.DefaultService.Name)
			continue
		}
		if p.DefaultService == "" {
			continue
		}
		if s, ok := byName[p.DefaultService]; !ok || renderPort(s.ExposedPort) != p.Port {
			add(issueError, where, "the default service %q is not one of the services on port %d", p.DefaultService, p.Port)
		}
	}
	return issues
}

// checkStored runs CheckModel over the stored model and the tracked certs.
func (s *server) checkStored(rt *live, ctx context.Context) ([]Issue, error) {
	list, err := rt.certs.List(ctx)
	if err != nil {
		return nil, err
	}
	return CheckModel(s.models.Snapshot(), list.Certs), nil
}
