package agentflow

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/api"
)

// G1: the agentflow docs may only name routes the API registers, and may not
// carry the owner's addresses, hostnames or host-specific model names.

var docFiles = []string{
	"../../docs/agentflow-design.md",
	"../../docs/guides/agentflow.md",
}

const readmePath = "../../README.md"

var (
	// "POST /api/x" pins the method; a bare /api/x token accepts any method.
	methodPath  = regexp.MustCompile("\\b(GET|POST|PUT|PATCH|DELETE)\\s+`?(/api/[^\\s`'\"),;]+)")
	bareAPI     = regexp.MustCompile("(/api/[^\\s`'\"),;]+)")
	placeholder = regexp.MustCompile(`\{[^}]*\}`)

	forbidden = []*regexp.Regexp{
		regexp.MustCompile(`\b192\.168\.\d+\.\d+`),
		regexp.MustCompile(`\b10\.\d+\.\d+\.\d+`),
		regexp.MustCompile(`\b172\.(1[6-9]|2\d|3[01])\.\d+\.\d+`),
		regexp.MustCompile(`(?i)cmdhome`),
		regexp.MustCompile(`(?i)qwen`),
		regexp.MustCompile(`(?i)mistral`),
		regexp.MustCompile(`(?i)mxfp8`),
		regexp.MustCompile(`(?i)cdelezenski`),
	}
)

type docRoute struct {
	file, method, path string
}

// readmeAgentflow returns the agentflow section of the README.
func readmeAgentflow(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, "## AgentFlow")
	if i < 0 {
		t.Fatal("README has no AgentFlow section")
	}
	rest := s[i+3:]
	if j := strings.Index(rest, "\n---"); j >= 0 {
		return s[i : i+3+j]
	}
	return s[i:]
}

func docRoutes(file, text string) []docRoute {
	var out []docRoute
	seen := map[string]bool{}
	clean := func(p string) string {
		p, _, _ = strings.Cut(p, "?")
		p = strings.TrimRight(p, ".:")
		return placeholder.ReplaceAllString(p, "x")
	}
	for _, m := range methodPath.FindAllStringSubmatch(text, -1) {
		p := clean(m[2])
		out = append(out, docRoute{file, m[1], p})
		seen[p] = true
	}
	for _, m := range bareAPI.FindAllStringSubmatch(text, -1) {
		p := clean(m[1])
		if p == "/api/" || seen[p] {
			continue
		}
		out = append(out, docRoute{file, "", p})
	}
	return out
}

// TestDocsRoutesAreRegistered builds the real API mux (no store, no engine, no
// network: handlers never run) and resolves every documented route against it.
// A path that only the /api/ catch-all claims is not a real route.
func TestDocsRoutesAreRegistered(t *testing.T) {
	mux := http.NewServeMux()
	api.New(api.Deps{}).Register(mux)

	registered := func(method, path string) bool {
		req := httptest.NewRequest(method, path, nil)
		_, pattern := mux.Handler(req)
		return pattern != "" && pattern != "/api/"
	}

	var routes []docRoute
	for _, f := range docFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		routes = append(routes, docRoutes(f, string(b))...)
	}
	routes = append(routes, docRoutes("README.md", readmeAgentflow(t))...)
	if len(routes) < 20 {
		t.Fatalf("found only %d documented routes: the extractor is broken or the docs lost their route tables", len(routes))
	}
	for _, r := range routes {
		ok := false
		if r.method != "" {
			ok = registered(r.method, r.path)
		} else {
			for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
				if registered(m, r.path) {
					ok = true
					break
				}
			}
		}
		if !ok {
			t.Errorf("%s documents %s %s, which is not a registered route", r.file, r.method, r.path)
		}
	}
}

// TestDocsHaveNoPersonalDetails keeps private addresses, hostnames and the
// owner's host-specific model names out of the agentflow docs.
func TestDocsHaveNoPersonalDetails(t *testing.T) {
	texts := map[string]string{"README.md (AgentFlow section)": readmeAgentflow(t)}
	for _, f := range docFiles {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		texts[f] = string(b)
	}
	for name, text := range texts {
		for _, re := range forbidden {
			if loc := re.FindStringIndex(text); loc != nil {
				t.Errorf("%s contains %q (matches %s)", name, text[loc[0]:loc[1]], re)
			}
		}
	}
}
