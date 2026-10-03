package haproxy

// Importing certificates that already sit in the certs directory (for example
// from a hand-written haproxy.cfg). They are tracked as-is: the file is never
// renamed, rewritten or deleted by the editor. The module does not read inside
// certificates (D15), so the host name comes from the file name, and whether the
// bundle really is trusted and serves is settled by the staged test.

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ImportableCert is one untracked .pem file found in the certs directory.
type ImportableCert struct {
	Name string `json:"name"`
	FQDN string `json:"fqdn"`
}

// fqdnFromFileName guesses the host a bundle is for: "app.example.com.pem" is
// app.example.com, and a "_wildcard." prefix (CertMachine's naming) is "*.".
func fqdnFromFileName(name string) string {
	f := strings.TrimSuffix(name, ".pem")
	if strings.HasPrefix(f, "_wildcard.") {
		f = "*." + strings.TrimPrefix(f, "_wildcard.")
	}
	return strings.ToLower(f)
}

// Importable lists untracked .pem files in the certs directory.
func (s *CertStore) Importable(ctx context.Context) ([]ImportableCert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	tracked := map[string]bool{}
	for _, e := range data.Certs {
		tracked[e.Name] = true
	}
	files, err := s.driver.PrivilegedList(ctx, s.driver.CertsDir())
	if err != nil {
		return nil, fmt.Errorf("haproxy: list certs dir: %w", err)
	}
	out := []ImportableCert{}
	for _, f := range files {
		if tracked[f.Name] || !strings.HasSuffix(f.Name, ".pem") || NamingValidateName(f.Name) != nil {
			continue
		}
		out = append(out, ImportableCert{Name: f.Name, FQDN: fqdnFromFileName(f.Name)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Import starts tracking the named untracked files, enabled, exactly where they
// are. It returns the names it adopted; names already tracked or missing from
// the directory are skipped.
func (s *CertStore) Import(ctx context.Context, names []string) ([]string, error) {
	have, err := s.Importable(ctx)
	if err != nil {
		return nil, err
	}
	avail := map[string]ImportableCert{}
	for _, c := range have {
		avail[c.Name] = c
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.load()
	if err != nil {
		return nil, err
	}
	var adopted []string
	for _, n := range names {
		c, ok := avail[n]
		if !ok {
			continue
		}
		data.Certs = append(data.Certs, CertEntry{
			Name: n, Enabled: true, Note: "imported from " + s.driver.CertsDir(),
			CertMachine: CertSource{FQDN: c.FQDN}, Adopted: true,
		})
		adopted = append(adopted, n)
	}
	if len(adopted) == 0 {
		return nil, nil
	}
	return adopted, s.save(data)
}
