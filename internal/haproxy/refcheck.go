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
	// Cert is the certificate FQDN a service names that has no tracked row, and
	// Suggest the untracked files in the certs directory that look like it.
	Cert    string   `json:"cert,omitempty"`
	Suggest []string `json:"suggest,omitempty"`
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
				issues[len(issues)-1].Cert = fq
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
	issues := CheckModel(s.models.Snapshot(), list.Certs)
	found, _ := rt.certs.Importable(ctx) // best effort: without it the issue just has no suggestion
	return hintMissingCerts(issues, found), nil
}

// hintMissingCerts turns the "no tracked certificate" errors into a calm
// did-you-mean: the untracked files in the certs directory that look like the
// certificate the service names, which the UI can adopt in one click.
func hintMissingCerts(issues []Issue, found []ImportableCert) []Issue {
	for i := range issues {
		is := &issues[i]
		if is.Cert == "" {
			continue
		}
		is.Suggest = suggestCerts(is.Cert, found)
		who := "A service"
		if m := regexp.MustCompile(`^service "([^"]*)"`).FindStringSubmatch(is.Message); m != nil {
			who = m[1]
		}
		is.Message = fmt.Sprintf("%s needs a certificate for %s.", who, is.Cert)
		if len(is.Suggest) == 0 {
			is.Message += " Pull one from CertMachine."
		}
	}
	return issues
}

// suggestCerts ranks untracked files for a wanted FQDN: an exact name first,
// then files for the same host with something extra (a hash or date suffix),
// then files sharing the first label. At most three.
func suggestCerts(want string, found []ImportableCert) []string {
	want = strings.ToLower(want)
	first, _, _ := strings.Cut(want, ".")
	var exact, near, loose []string
	for _, c := range found {
		switch {
		case c.FQDN == want:
			exact = append(exact, c.Name)
		case strings.HasPrefix(c.FQDN, want+"-") || strings.HasPrefix(c.FQDN, want+"."):
			near = append(near, c.Name)
		case first != "" && strings.HasPrefix(c.FQDN, first+"."):
			loose = append(loose, c.Name)
		}
	}
	out := append(append(exact, near...), loose...)
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}
