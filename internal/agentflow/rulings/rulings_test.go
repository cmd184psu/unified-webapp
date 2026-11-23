package rulings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_RulingsFile(t *testing.T) {
	body := `# RULINGS (the oracle): short, FINAL, with their authority

Only a RULING in this file is authority.

| id | topic | ruling | source | authority | areas | date |
|---|---|---|---|---|---|---|
| R-003 | Corruption: distance trade factor | The distance calculation keeps ConnectedToCapital = false (connectivity does NOT change the distance) | openciv3 | City.cs:972/981 | corruption,engine | 2026-11-22 |
| R-017 | Unsatisfiable reviewer demand | A reviewer demand that is provably unsatisfiable is overridden with a recorded reason, never looped | custom | process rule | process | 2026-11-22 |
| R-022 | Owner override | The owner may overrule any ruling at any time; it becomes a ruling | custom | owner | process | 2026-11-22 |
`
	rf := Parse(body)
	if rf.Preamble == "" {
		t.Fatal("expected non-empty preamble")
	}
	if len(rf.Rulings) != 3 {
		t.Fatalf("expected 3 rulings, got %d", len(rf.Rulings))
	}
	if rf.Rulings[0].ID != "R-003" {
		t.Fatalf("first ruling id = %q", rf.Rulings[0].ID)
	}
	if rf.Rulings[0].Areas[0] != "corruption" {
		t.Fatalf("R-003 areas[0] = %q", rf.Rulings[0].Areas[0])
	}
	if rf.Rulings[1].Areas[0] != "process" {
		t.Fatalf("R-017 areas[0] = %q", rf.Rulings[1].Areas[0])
	}
}

func TestParse_NoTable(t *testing.T) {
	rf := Parse("# Just a note\nNo table here.\n")
	if rf.Preamble == "" {
		t.Fatal("expected preamble")
	}
	if len(rf.Rulings) != 0 {
		t.Fatalf("expected no rulings, got %d", len(rf.Rulings))
	}
}

func TestHeader_EmptySetting(t *testing.T) {
	h := Header("/some/root", "", []string{"a.go"}, ParseAreaMap("{}"))
	if h != "" {
		t.Fatalf("expected empty header for empty setting, got %q", h)
	}
}

func TestHeader_MissingFile(t *testing.T) {
	h := Header("/nonexistent", "docs/rulings/RULINGS.md", []string{"a.go"}, ParseAreaMap("{}"))
	if h != "" {
		t.Fatalf("expected empty header for missing file, got %q", h)
	}
}

func TestHeader_CorruptionFiles(t *testing.T) {
	root := t.TempDir()
	rulingsPath := filepath.Join(root, "docs/rulings/RULINGS.md")
	if err := os.MkdirAll(filepath.Dir(rulingsPath), 0755); err != nil {
		t.Fatal(err)
	}
	body := `# RULINGS (the oracle): short, FINAL, with their authority

Only a RULING in this file is authority.

| id | topic | ruling | source | authority | areas | date |
|---|---|---|---|---|---|---|
| R-003 | Corruption: distance trade factor | The distance calculation keeps ConnectedToCapital = false (connectivity does NOT change the distance) | openciv3 | City.cs:972/981 | corruption,engine | 2026-11-22 |
| R-017 | Unsatisfiable reviewer demand | A reviewer demand that is provably unsatisfiable is overridden with a recorded reason, never looped | custom | process rule | process | 2026-11-22 |
`
	if err := os.WriteFile(rulingsPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	am := ParseAreaMap(`{"src/rules/corruption*": "corruption"}`)
	h := Header(root, "docs/rulings/RULINGS.md", []string{"src/rules/corruption/distance.go"}, am)
	if h == "" {
		t.Fatal("expected non-empty header")
	}
	if !contains(h, "## Rulings (the oracle) injected for this task") {
		t.Fatal("expected rulings section header")
	}
	if !contains(h, "R-003") {
		t.Fatal("expected R-003 in header")
	}
	if !contains(h, "R-017") {
		t.Fatal("expected R-017 (process) in header")
	}
	if !contains(h, filepath.ToSlash(rulingsPath)) {
		t.Fatalf("expected full file path in header, got %q", h)
	}
}

func TestHeader_NoAreaMatch(t *testing.T) {
	root := t.TempDir()
	rulingsPath := filepath.Join(root, "docs/rulings/RULINGS.md")
	if err := os.MkdirAll(filepath.Dir(rulingsPath), 0755); err != nil {
		t.Fatal(err)
	}
	body := `# RULINGS (the oracle): short, FINAL, with their authority

Only a RULING in this file is authority.

| id | topic | ruling | source | authority | areas | date |
|---|---|---|---|---|---|---|
| R-003 | Corruption: distance trade factor | The distance calculation keeps ConnectedToCapital = false | openciv3 | City.cs:972/981 | corruption,engine | 2026-11-22 |
`
	if err := os.WriteFile(rulingsPath, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	h := Header(root, "docs/rulings/RULINGS.md", []string{"src/other/file.go"}, ParseAreaMap("{}"))
	if h == "" {
		t.Fatal("expected non-empty header")
	}
	if !contains(h, "No ruling matches this task's files") {
		t.Fatalf("expected no-match note, got %q", h)
	}
}

func TestContradictsRuling_ConnectedToCapital(t *testing.T) {
	rulings := []Ruling{{
		ID:     "R-003",
		Topic:  "Corruption: distance trade factor",
		Ruling: "The distance calculation keeps ConnectedToCapital = false (connectivity does NOT change the distance).",
		Areas:  []string{"corruption", "engine"},
	}}
	repair := Issues{
		Issues: []SchemaIssue{{
			Description: "ConnectedToCapital must be set to true for the distance calculation.",
			File:        "src/rules/corruption/distance.go",
			Line:        972,
		}},
		RepairPrompt: "Set ConnectedToCapital to true so the distance factor changes.",
	}
	if !ContradictsRuling(repair, rulings) {
		t.Fatal("expected repair to contradict R-003")
	}
}

func TestContradictsRuling_NoContradiction(t *testing.T) {
	rulings := []Ruling{{
		ID:     "R-003",
		Topic:  "Corruption: distance trade factor",
		Ruling: "The distance calculation keeps ConnectedToCapital = false.",
		Areas:  []string{"corruption"},
	}}
	repair := Issues{
		Issues: []SchemaIssue{{
			Description: "The distance calculation does not record the capital city.",
			File:        "src/main.go",
			Line:        10,
		}},
	}
	if ContradictsRuling(repair, rulings) {
		t.Fatal("expected no contradiction")
	}
}

func TestContradictIssues_OppositeNotes(t *testing.T) {
	a := Issues{Issues: []SchemaIssue{{Description: "In src/rules/corruption/distance.go the ConnectedToCapital flag must be set to true."}}}
	b := Issues{Issues: []SchemaIssue{{Description: "In src/rules/corruption/distance.go the ConnectedToCapital flag must stay false."}}}
	if !ContradictIssues(a, b) {
		t.Fatal("expected notes to contradict")
	}
}

func TestAreaMap_Parse(t *testing.T) {
	am := ParseAreaMap(`{"src/rules/corruption*": "corruption", "src/engine*": "engine"}`)
	if am.AreaOf("src/rules/corruption/distance.go") != "corruption" {
		t.Fatal("expected corruption area")
	}
	if am.AreaOf("src/engine/foo.go") != "engine" {
		t.Fatal("expected engine area")
	}
	if am.AreaOf("src/other/file.go") != "" {
		t.Fatal("expected no area")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAny(s, sub))
}

func containsAny(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
