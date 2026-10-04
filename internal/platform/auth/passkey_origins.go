package auth

import (
	"net"
	"sort"
	"strings"
)

// PasskeyOrigins returns the origins a passkey ceremony may come from: the
// configured extras plus https://<host> for every routed host that sits at or
// under rpID. A host the app itself serves is by definition its own origin, so
// a new module or host works without touching the passkey settings. The result
// is computed from config only, never from the request.
func PasskeyOrigins(rpID string, extras, hosts []string) []string {
	id := strings.ToLower(strings.TrimSpace(rpID))
	seen := map[string]bool{}
	var out []string
	add := func(o string) {
		if o != "" && !seen[o] {
			seen[o] = true
			out = append(out, o)
		}
	}
	for _, o := range extras {
		add(strings.TrimSpace(o))
	}
	var routed []string
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if host, _, err := net.SplitHostPort(h); err == nil {
			h = host
		}
		if h == "" || id == "" || net.ParseIP(h) != nil {
			continue
		}
		if h == id || strings.HasSuffix(h, "."+id) {
			routed = append(routed, "https://"+h)
		}
	}
	sort.Strings(routed)
	for _, o := range routed {
		add(o)
	}
	return out
}

// SuggestRPID is the longest parent domain, of at least two labels, that every
// routed host shares ("" when they share none). It is only a default offered in
// the admin UI.
func SuggestRPID(hosts []string) string {
	var common []string
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if host, _, err := net.SplitHostPort(h); err == nil {
			h = host
		}
		if h == "" || net.ParseIP(h) != nil {
			continue
		}
		labels := strings.Split(h, ".")
		if common == nil {
			common = labels
			continue
		}
		// keep the shared suffix
		i, j := len(common)-1, len(labels)-1
		n := 0
		for i >= 0 && j >= 0 && common[i] == labels[j] {
			i, j, n = i-1, j-1, n+1
		}
		common = common[len(common)-n:]
	}
	if len(common) < 2 {
		return ""
	}
	return strings.Join(common, ".")
}
