package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func exportDoc(t *testing.T, e *env) ConfigDoc {
	t.Helper()
	return decode[ConfigDoc](t, e.ok("GET", "/api/config/export", nil, http.StatusOK))
}

func importCfg(e *env, doc ConfigDoc, apply bool, want int) []byte {
	return e.ok("POST", "/api/config/import", map[string]any{"config": doc, "apply": apply}, want)
}

// H13: the configuration travels as a file without any project data or key: a
// dry run changes nothing, applying it recreates the backends with their tiers
// and tool flags and the global candidate lists, an existing project is left
// alone, and importing again updates instead of duplicating.
func TestH13_ConfigExportImportRoundTrip(t *testing.T) {
	srv := fakeOllamaCaps(t, map[string][]string{"m1": {"completion", "tools"}, "m2": {"completion", "tools"}})
	a := newEnv(t, envOpt{})
	b := backendReq("gpu")
	b["host"] = srv.URL
	b["models"] = []map[string]any{{"model": "m1", "tier": 1}, {"model": "m2", "tier": 2, "max_concurrent": 1}}
	b["api_key"] = "sekret-key"
	a.ok("POST", "/api/backends", b, http.StatusCreated)
	a.ok("PUT", "/api/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "gpu", "model": "m2"}, {"backend": "gpu", "model": "m1"}}}, http.StatusOK)
	a.ok("PUT", "/api/candidates/reviewer", map[string]any{"candidates": []map[string]string{{"backend": "gpu", "model": "m1"}}}, http.StatusOK)
	a.addProject("p1")
	a.ok("PUT", "/api/projects/p1/candidates/worker", map[string]any{"candidates": []map[string]string{{"backend": "gpu", "model": "m1"}}}, http.StatusOK)
	a.ok("PUT", "/api/settings", map[string]any{"max_attempts": 7}, http.StatusOK)

	raw := a.ok("GET", "/api/config/export", nil, http.StatusOK)
	if strings.Contains(string(raw), "sekret-key") || strings.Contains(string(raw), "api_key") {
		t.Fatalf("the export carries a key: %s", raw)
	}
	var doc ConfigDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Format != "agentflow-config" || doc.Version != 1 || len(doc.Backends) != 1 || !doc.Backends[0].HasKey {
		t.Fatalf("doc = %+v", doc)
	}
	if got := doc.Candidates["worker"]; len(got) != 2 || got[0].Model != "m2" || got[1].Model != "m1" {
		t.Fatalf("worker list (order matters) = %+v", got)
	}
	if strings.Contains(string(raw), "p1") {
		t.Fatalf("the export names a project: %s", raw)
	}
	if doc.Settings["max_attempts"].(float64) != 7 || doc.Settings["opencode_path"] != nil {
		t.Fatalf("settings = %+v: limits travel, machine paths do not", doc.Settings)
	}

	c := newEnv(t, envOpt{})
	c.addProject("keepme")
	rep := decode[ImportReport](t, importCfg(c, doc, false, http.StatusOK))
	if rep.Applied || len(rep.Created) != 1 || rep.Created[0] != "gpu" || len(rep.Candidates) != 2 || len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "API key") {
		t.Fatalf("dry-run report = %+v", rep)
	}
	if bs := decode[[]BackendView](t, c.ok("GET", "/api/backends", nil, http.StatusOK)); len(bs) != 0 {
		t.Fatalf("a dry run created %d backend(s)", len(bs))
	}
	rep = decode[ImportReport](t, importCfg(c, doc, true, http.StatusOK))
	if !rep.Applied || len(rep.Created) != 1 {
		t.Fatalf("apply report = %+v", rep)
	}
	got := exportDoc(t, c)
	if len(got.Backends) != 1 || got.Backends[0].HasKey {
		t.Fatalf("imported backends = %+v (no key should arrive)", got.Backends)
	}
	tiers := map[string]int{}
	for _, m := range got.Backends[0].Models {
		tiers[m.Model] = m.Tier
		if m.Tools != "yes" {
			t.Fatalf("model %s tools = %q: the importing side must verify with the host", m.Model, m.Tools)
		}
	}
	if tiers["m1"] != 1 || tiers["m2"] != 2 || got.Candidates["worker"][0].Model != "m2" || got.Settings["max_attempts"].(float64) != 7 {
		t.Fatalf("imported config differs: %+v", got)
	}
	if ps := decode[[]ProjectView](t, c.ok("GET", "/api/projects", nil, http.StatusOK)); len(ps) != 1 || ps[0].Slug != "keepme" {
		t.Fatalf("projects after import = %+v", ps)
	}
	rep = decode[ImportReport](t, importCfg(c, doc, true, http.StatusOK))
	if len(rep.Created) != 0 || len(rep.Updated) != 1 {
		t.Fatalf("a second import should update, not create: %+v", rep)
	}
}

// A file that names a model without tool support, a candidate nobody has, an
// unknown role, or is not a configuration file at all is refused, and a refused
// import creates nothing.
func TestH13_ConfigImportRefusals(t *testing.T) {
	srv := fakeOllamaCaps(t, map[string][]string{"good": {"completion", "tools"}, "gemma": {"completion"}})
	e := newEnv(t, envOpt{})
	good := ConfigDoc{Format: "agentflow-config", Version: 1, Candidates: map[string][]configRef{},
		Backends: []configBackend{{Name: "gpu", Host: srv.URL, APIType: "ollama", Slots: 1, Models: []BackendModelView{{Model: "good"}}}}}
	count := func() int { return len(decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, http.StatusOK))) }

	e.field("POST", "/api/config/import", map[string]any{"config": map[string]any{"format": "something-else", "version": 1}, "apply": true}, "config")
	badTools := good
	badTools.Backends = []configBackend{{Name: "gpu", Host: srv.URL, APIType: "ollama", Slots: 1, Models: []BackendModelView{{Model: "gemma"}}}}
	e.field("POST", "/api/config/import", map[string]any{"config": badTools, "apply": true}, "models")
	ghost := good
	ghost.Candidates = map[string][]configRef{"worker": {{Backend: "gpu", Model: "nope"}}}
	e.field("POST", "/api/config/import", map[string]any{"config": ghost, "apply": true}, "config")
	role := good
	role.Candidates = map[string][]configRef{"janitor": {{Backend: "gpu", Model: "good"}}}
	e.field("POST", "/api/config/import", map[string]any{"config": role, "apply": true}, "config")
	if n := count(); n != 0 {
		t.Fatalf("refused imports created %d backend(s)", n)
	}
	retired := good
	retired.Backends = append(slicesClone(good.Backends), configBackend{Name: "old", Host: srv.URL, APIType: "ollama", Slots: 1, Retired: true, Models: []BackendModelView{{Model: "good"}}})
	rep := decode[ImportReport](t, importCfg(e, retired, true, http.StatusOK))
	if len(rep.Created) != 1 || rep.Created[0] != "gpu" || len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "retired") {
		t.Fatalf("a retired backend should be skipped with a note: %+v", rep)
	}
}

func slicesClone[T any](s []T) []T { return append([]T(nil), s...) }
