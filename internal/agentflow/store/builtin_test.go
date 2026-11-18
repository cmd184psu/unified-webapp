package store

import (
	"path/filepath"
	"testing"
)

// Migration 11 widens backends.api_type to opencode's own provider and keeps
// every existing row, key and model.
func TestMigration11KeepsBackendsAndAllowsTheBuiltinProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentflow.db")
	s1, err := openWith(path, migrations[:10])
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO backends (name, host, api_type, hosted, paid, slots, retired, created, updated, api_key) VALUES ('b', 'http://localhost:11434', 'ollama', 1, 0, 2, 0, 1, 1, 'k')`,
		`INSERT INTO backend_models (backend, model, paid, max_concurrent) VALUES ('b', 'm', 0, 0)`,
	} {
		if _, err := s1.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s1.Close()
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer s2.Close()
	b, err := s2.GetBackend(ctx, "b")
	if err != nil || b.Host != "http://localhost:11434" || b.Slots != 2 || !b.Hosted || len(b.Models) != 1 {
		t.Fatalf("backend after upgrade = %+v (%v)", b, err)
	}
	if key, _ := s2.BackendKey(ctx, "b"); key != "k" {
		t.Fatalf("key = %q", key)
	}
	// The built-in provider needs no host; any other type still does.
	if err := s2.CreateBackend(ctx, "t", Backend{Name: "free", APIType: "opencode", Hosted: true, Slots: 1,
		Models: []BackendModel{{Model: "big-pickle"}}}); err != nil {
		t.Fatalf("opencode backend: %v", err)
	}
	if err := s2.CreateBackend(ctx, "t", Backend{Name: "nohost", APIType: "ollama", Slots: 1}); err == nil {
		t.Fatal("ollama backend without a host accepted")
	}
}
