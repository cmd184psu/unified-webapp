package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// P3 migration 10 on a database that already holds data: the new columns take
// their defaults, existing rows survive, and frd_parts works.
func TestMigration10UpgradesAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agentflow.db")
	s1, err := openWith(path, migrations[:9])
	if err != nil {
		t.Fatal(err)
	}
	repo := mkRepo(t)
	for _, q := range []string{
		`INSERT INTO backends (name, host, api_type, hosted, paid, slots, retired, created, updated) VALUES ('b', 'http://localhost:11434', 'ollama', 0, 0, 1, 0, 1, 1)`,
		`INSERT INTO backend_models (backend, model, paid, max_concurrent) VALUES ('b', 'm', 0, 0)`,
		`INSERT INTO projects (slug, repo_path, model_scope, created, updated) VALUES ('p', '` + repo + `', 'local', 1, 1)`,
		`INSERT INTO efforts (project, title, status, created, updated) VALUES ('p', 'e', 'draft', 1, 1)`,
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
	if v := schemaVersion(t, s2.db); v != len(migrations) {
		t.Fatalf("version = %d, want %d", v, len(migrations))
	}
	b, err := s2.GetBackend(ctx, "b")
	if err != nil || len(b.Models) != 1 || b.Models[0].ContextTokens != 0 {
		t.Fatalf("backend after upgrade = %+v (%v)", b, err)
	}
	p, err := s2.GetProject(ctx, "p")
	if err != nil || p.DefinitionOfDone != nil {
		t.Fatalf("project after upgrade = %+v (%v)", p, err)
	}
	if err := s2.AddFRDParts(ctx, "t", 1, []FRDPart{{N: 2, Title: "two", JSON: []byte(`{}`), Tokens: 3}}); err != nil {
		t.Fatalf("frd_parts: %v", err)
	}
	if _, err := s2.db.Exec(`INSERT INTO frd_parts (effort, n, title, json) VALUES (1, 0, 't', '{}')`); err == nil {
		t.Fatal("part number 0 accepted")
	}
}

func TestN4_ContextTokensRoundTripAndValidation(t *testing.T) {
	s, _ := openTemp(t)
	b := goodBackend("gpu")
	b.Models[0].ContextTokens = 32768
	if err := s.CreateBackend(ctx, "t", b); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetBackend(ctx, "gpu")
	if got.Models[0].Model != "free-model" || got.Models[0].ContextTokens != 32768 || got.Models[1].ContextTokens != 0 {
		t.Fatalf("models = %+v", got.Models)
	}
	got.Models[1].ContextTokens = 8192
	if err := s.UpdateBackend(ctx, "t", got); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetBackend(ctx, "gpu"); got.Models[1].ContextTokens != 8192 {
		t.Fatalf("update lost the context: %+v", got.Models)
	}
	got.Models[0].ContextTokens = -5
	wantField(t, s.UpdateBackend(ctx, "t", got), "models")
}

func TestN1_ProjectDefinitionOfDoneOverride(t *testing.T) {
	s, _ := openTemp(t)
	p := Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local", DefinitionOfDone: []string{"Docs updated.", "Changelog entry."}}
	if err := s.CreateProject(ctx, "t", p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProject(ctx, "p")
	if strings.Join(got.DefinitionOfDone, "|") != "Docs updated.|Changelog entry." {
		t.Fatalf("override = %v", got.DefinitionOfDone)
	}
	got.DefinitionOfDone = []string{"ok", "  "}
	wantField(t, s.UpdateProject(ctx, "t", got), "definition_of_done[1]")
	got.DefinitionOfDone = nil
	if err := s.UpdateProject(ctx, "t", got); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProject(ctx, "p"); got.DefinitionOfDone != nil {
		t.Fatalf("nil did not clear the override: %v", got.DefinitionOfDone)
	}
}

// N3: every FRD save fills ids, links, status, date and the FRD id; an FRD
// that needs nothing is stored byte for byte; a bad link is still refused.
func TestN3_SaveDocFillsTheFRD(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	ef, err := s.CreateEffort(ctx, "t", "p", "e", false)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.SaveDoc(ctx, "t", ef.ID, model.DocFRD, []byte(`{"title":"t","objective":"o","requirements":[{"description":"a"},{"description":"b"}],"acceptance_criteria":[{"criterion":"c"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	f, err := model.ParseFRD(d.JSON)
	if err != nil {
		t.Fatal(err)
	}
	if f.ID != "FRD-1" || f.Status != "draft" || len(f.Created) != 10 || f.Requirements[1].ID != "R2" ||
		f.AcceptanceCriteria[0].ID != "AC1" || strings.Join(f.AcceptanceCriteria[0].Requirements, ",") != "R1,R2" {
		t.Fatalf("filled = %s", d.JSON)
	}

	// A FRD saved before the filler existed is not rewritten when it is complete.
	full := `{"id":"X","title":"t","status":"approved","created":"2020-01-01","objective":"o","unknown_future_field":1,` +
		`"requirements":[{"id":"R1","description":"a"}],"acceptance_criteria":[{"id":"AC1","requirements":["R1"],"criterion":"c"}]}`
	d, err = s.SaveDoc(ctx, "t", ef.ID, model.DocFRD, []byte(full))
	if err != nil || string(d.JSON) != full {
		t.Fatalf("complete FRD was rewritten: %s (%v)", d.JSON, err)
	}
	// A link to a requirement that does not exist is dropped, not refused.
	d, err = s.SaveDoc(ctx, "t", ef.ID, model.DocFRD, []byte(`{"title":"t","objective":"o","requirements":[{"description":"a"}],"acceptance_criteria":[{"requirements":["R9"],"criterion":"c"}]}`))
	if err != nil || !strings.Contains(string(d.JSON), `"requirements":["R1"]`) {
		t.Fatalf("dead link: %s (%v)", d.JSON, err)
	}
}

func TestN4_PartsStoreReplaceListGetStart(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	ef, _ := s.CreateEffort(ctx, "t", "p", "e", false)
	parts := []FRDPart{{N: 2, Title: "b", Summary: "sb", JSON: []byte(`{"a":1}`), Tokens: 5}, {N: 3, Title: "c", JSON: []byte(`{}`)}}
	if err := s.AddFRDParts(ctx, "t", ef.ID, parts); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListFRDParts(ctx, ef.ID)
	if err != nil || len(got) != 2 || got[0].Title != "b" || got[0].Summary != "sb" || string(got[0].JSON) != `{"a":1}` || got[0].Tokens != 5 {
		t.Fatalf("list = %+v (%v)", got, err)
	}
	if err := s.MarkFRDPartStarted(ctx, "t", ef.ID, 2, 77); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.GetFRDPart(ctx, ef.ID, 2); p.StartedEffort != 77 {
		t.Fatalf("part = %+v", p)
	}
	if _, err := s.GetFRDPart(ctx, ef.ID, 9); err == nil {
		t.Fatal("missing part found")
	}
	// A second split appends after the stored parts and keeps the started one.
	if err := s.AddFRDParts(ctx, "t", ef.ID, []FRDPart{{N: 2, Title: "d", JSON: []byte(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.ListFRDParts(ctx, ef.ID)
	if len(got) != 3 || got[0].Title != "b" || got[0].StartedEffort != 77 || got[1].Title != "c" || got[2].Title != "d" || got[2].N != 4 || got[2].StartedEffort != 0 {
		t.Fatalf("a re-split must keep the earlier parts and append: %+v", got)
	}
}

// B2: the revision and the parts of a split commit together or not at all.
func TestB2_SplitSaveIsAtomic(t *testing.T) {
	s, _ := openTemp(t)
	if err := s.CreateProject(ctx, "t", Project{Slug: "p", RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
	ef, _ := s.CreateEffort(ctx, "t", "p", "e", false)
	frd := []byte(`{"title":"t","objective":"o","requirements":[{"description":"a"}]}`)
	_, err := s.SaveDocWith(ctx, "t", ef.ID, model.DocFRD, frd, func(tx *sql.Tx, d model.Doc) error {
		return AddFRDPartsTx(ctx, tx, "t", ef.ID, []FRDPart{{Title: "x", JSON: []byte(`{}`)}})
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveDocWith(ctx, "t", ef.ID, model.DocFRD, frd, func(tx *sql.Tx, d model.Doc) error {
		if err := AddFRDPartsTx(ctx, tx, "t", ef.ID, []FRDPart{{Title: "y", JSON: []byte(`{}`)}}); err != nil {
			return err
		}
		return errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected the failure")
	}
	if d, _ := s.GetDoc(ctx, ef.ID, model.DocFRD, 0); d.Revision != 1 {
		t.Fatalf("revision after a failed apply = %d", d.Revision)
	}
	if got, _ := s.ListFRDParts(ctx, ef.ID); len(got) != 1 || got[0].Title != "x" {
		t.Fatalf("parts after a failed apply: %+v", got)
	}
	// SaveDoc failing (invalid FRD) leaves the parts alone too.
	if _, err := s.SaveDocWith(ctx, "t", ef.ID, model.DocFRD, []byte(`{}`), func(tx *sql.Tx, d model.Doc) error {
		return AddFRDPartsTx(ctx, tx, "t", ef.ID, []FRDPart{{Title: "z", JSON: []byte(`{}`)}})
	}); err == nil {
		t.Fatal("invalid FRD saved")
	}
	if got, _ := s.ListFRDParts(ctx, ef.ID); len(got) != 1 {
		t.Fatalf("parts after an invalid save: %+v", got)
	}
}
