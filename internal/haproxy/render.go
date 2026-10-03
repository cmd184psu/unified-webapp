package haproxy

import (
	"fmt"
	"sort"
	"strings"
)

// repeatableGlobal keys may appear several times, so a baseline entry is never
// dropped in favour of a model entry with the same key.
var repeatableGlobal = map[string]bool{"log": true, "stats": true}

// statsSocketValue is the value of the generated `stats socket` line. The
// level is always `user` (read-only): the app only reads the socket and must
// never be able to change HAProxy through it. owner hands the socket to the app
// user so it can connect without sudo.
func statsSocketValue(path, owner string) string {
	v := path + " mode 660 level user"
	if owner != "" {
		v += " user " + owner
	}
	return v
}

func renderPort(p int) int {
	if p == 0 {
		return 443
	}
	return p
}

func directiveLines(b *strings.Builder, ds []Directive) {
	for _, d := range ds {
		if d.Value == "" {
			fmt.Fprintf(b, "    %s\n", d.Key)
		} else {
			fmt.Fprintf(b, "    %s %s\n", d.Key, d.Value)
		}
	}
}

// Render produces the full haproxy.cfg for m. baseline is the driver's
// platform global baseline, statsSocket the admin socket path ("" omits it),
// statsOwner the OS user the socket is handed to ("" adds no `user` option) and
// crtListPath the crt-list every TLS bind references. Output is
// deterministic; disabled services are omitted.
func Render(m *Model, baseline []Directive, statsSocket, statsOwner, crtListPath string) string {
	var b strings.Builder
	b.WriteString("# managed by the haproxy editor - manual edits will be overwritten\n\n")

	// global: baseline entries not overridden by the model, then the model's.
	modelKeys := map[string]bool{}
	for _, d := range m.Global {
		modelKeys[d.Key] = true
	}
	b.WriteString("global\n")
	for _, d := range baseline {
		if modelKeys[d.Key] && !repeatableGlobal[d.Key] {
			continue
		}
		// The generated socket line below is authoritative (read-only level,
		// owner); a baseline copy would bind the same path twice.
		if d.Key == "stats socket" && statsSocket != "" {
			continue
		}
		directiveLines(&b, []Directive{d})
	}
	// The generator owns the stats socket line: a model copy (say, level admin)
	// would break the always-read-only guarantee.
	for _, d := range m.Global {
		if d.Key != "stats socket" {
			directiveLines(&b, []Directive{d})
		}
	}
	if statsSocket != "" {
		fmt.Fprintf(&b, "    stats socket %s\n", statsSocketValue(statsSocket, statsOwner))
	}

	b.WriteString("\ndefaults\n")
	directiveLines(&b, m.Defaults)

	for _, rs := range m.RawSections {
		fmt.Fprintf(&b, "\n%s\n", rs.Header)
		for _, l := range rs.Lines {
			fmt.Fprintf(&b, "    %s\n", l)
		}
	}

	// Collect ports.
	byName := map[string]*Service{}
	portSet := map[int]bool{443: true}
	for _, p := range m.Ports {
		portSet[renderPort(p.Port)] = true
	}
	for i := range m.Services {
		s := &m.Services[i]
		if !s.Enabled {
			continue
		}
		byName[s.Name] = s
		portSet[renderPort(s.ExposedPort)] = true
	}
	ports := make([]int, 0, len(portSet))
	for p := range portSet {
		ports = append(ports, p)
	}
	sort.Ints(ports)
	portDefault := map[int]string{}
	for _, p := range m.Ports {
		portDefault[renderPort(p.Port)] = p.DefaultService
	}

	backends := []string{}
	seen := map[string]bool{}
	addBackend := func(name string) {
		if !seen[name] {
			seen[name] = true
			backends = append(backends, name)
		}
	}

	for _, port := range ports {
		fmt.Fprintf(&b, "\nfrontend fe_%d\n", port)
		fmt.Fprintf(&b, "    bind *:%d ssl crt-list %s\n", port, crtListPath)
		b.WriteString("    mode http\n")
		// Fixed frontend policy: the proxy fronts apps that need the real client
		// address (X-Forwarded-For) and the HTTP log format, as hero's config does.
		b.WriteString("    option httplog\n")
		b.WriteString("    option forwardfor\n")
		if m.HTTPServerClose {
			b.WriteString("    option http-server-close\n")
		} else {
			b.WriteString("    # option http-server-close  (off: backend connections are reused; on: closed after each response)\n")
		}
		var rules []string
		for i := range m.Services {
			s := &m.Services[i]
			if !s.Enabled || renderPort(s.ExposedPort) != port || len(s.FQDNs) == 0 {
				continue
			}
			var hosts []string
			for _, f := range s.FQDNs {
				hosts = append(hosts, f, fmt.Sprintf("%s:%d", f, port))
			}
			acl := "host_" + s.Name
			fmt.Fprintf(&b, "    acl %s hdr(host) -i %s\n", acl, strings.Join(hosts, " "))
			rules = append(rules, fmt.Sprintf("    use_backend %s_backend if %s\n", s.Name, acl))
		}
		for _, r := range rules {
			b.WriteString(r)
		}
		if port == 443 {
			fmt.Fprintf(&b, "    default_backend %s_backend\n", m.DefaultService.Name)
		} else if d := portDefault[port]; d != "" && byName[d] != nil {
			fmt.Fprintf(&b, "    default_backend %s_backend\n", d)
		}
	}

	addBackend(m.DefaultService.Name)
	for i := range m.Services {
		if m.Services[i].Enabled {
			addBackend(m.Services[i].Name)
		}
	}
	for _, name := range backends {
		fmt.Fprintf(&b, "\nbackend %s_backend\n", name)
		if s := byName[name]; s != nil && name != m.DefaultService.Name {
			writeServer(&b, s.Name, s.Upstream, s.Check)
			for _, e := range s.Extra {
				fmt.Fprintf(&b, "    %s\n", e)
			}
		} else {
			writeServer(&b, m.DefaultService.Name, m.DefaultService.Upstream, m.DefaultService.Check)
		}
	}
	return b.String()
}

func writeServer(b *strings.Builder, name string, u Upstream, check bool) {
	fmt.Fprintf(b, "    server %s %s:%d", name, u.Host, u.Port)
	if check {
		b.WriteString(" check")
	}
	b.WriteString("\n")
}
