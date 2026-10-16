package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// Migration 12 adds a model tier that defaults to 0 (not rated); a tier saved
// with a backend is read back, and anything outside 0..3 is refused.
func TestMigration12AddsModelTiers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentflow.db")
	s1, err := openWith(path, migrations[:11])
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO backends (name, host, api_type, hosted, paid, slots, retired, created, updated, api_key) VALUES ('b', 'http://localhost:11434', 'ollama', 0, 0, 1, 0, 1, 1, '')`,
		`INSERT INTO backend_models (backend, model, paid, max_concurrent) VALUES ('b', 'old', 0, 0)`,
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
	if err != nil || len(b.Models) != 1 || b.Models[0].Tier != 0 {
		t.Fatalf("existing model after upgrade = %+v (%v)", b.Models, err)
	}
	b.Models = append(b.Models, BackendModel{Model: "light", Tier: 3}, BackendModel{Model: "strong", Tier: 1})
	if err := s2.UpdateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	b, _ = s2.GetBackend(ctx, "b")
	got := map[string]int{}
	for _, m := range b.Models {
		got[m.Model] = m.Tier
	}
	if got["old"] != 0 || got["light"] != 3 || got["strong"] != 1 {
		t.Fatalf("tiers = %v", got)
	}
	for _, bad := range []int{-1, 4} {
		b.Models[0].Tier = bad
		if err := s2.UpdateBackend(ctx, "t", b); err == nil {
			t.Fatalf("tier %d accepted", bad)
		}
	}
}

// H10: whether a model supports tool calls is stored per model ("" when not
// known); a model known to lack them cannot be made a candidate for any role.
func TestH10ToolsAreStoredAndAModelWithoutThemCannotBeACandidate(t *testing.T) {
	s, _ := openTemp(t)
	b := Backend{Name: "b", Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: 1, Models: []BackendModel{
		{Model: "good", Tools: "yes"}, {Model: "bad", Tools: "no"}, {Model: "unknown"}}}
	if err := s.CreateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBackend(ctx, "b")
	tools := map[string]string{}
	for _, m := range got.Models {
		tools[m.Model] = m.Tools
	}
	if tools["good"] != "yes" || tools["bad"] != "no" || tools["unknown"] != "" {
		t.Fatalf("tools = %v", tools)
	}
	b.Models[0].Tools = "maybe"
	if err := s.UpdateBackend(ctx, "t", b); err == nil {
		t.Fatal("tools = maybe accepted")
	}
	for _, ok := range []string{"good", "unknown"} {
		if err := s.SetRoleCandidates(ctx, "t", "global", "", "worker", []CandidateRef{{Backend: "b", Model: ok}}); err != nil {
			t.Fatalf("%s as a candidate: %v", ok, err)
		}
	}
	err := s.SetRoleCandidates(ctx, "t", "global", "", "worker", []CandidateRef{{Backend: "b", Model: "bad"}})
	if err == nil || !strings.Contains(err.Error(), "tool calls") {
		t.Fatalf("a model without tool support became a candidate: %v", err)
	}
}
