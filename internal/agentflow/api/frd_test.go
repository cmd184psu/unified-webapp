package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// N1: the definition of done is derived from the repository's toolchain, a
// project can override it, and null goes back to the derived one.
func TestN1_DefinitionOfDoneRouteAndOverride(t *testing.T) {
	e := newEnv(t, envOpt{})
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.ok("POST", "/api/projects", map[string]any{"slug": "p", "repo_path": dir}, http.StatusCreated)
	type dod struct {
		Items      []string `json:"items"`
		Source     string   `json:"source"`
		Toolchains []string `json:"toolchains"`
	}
	d := decode[dod](t, e.ok("GET", "/api/projects/p/definition-of-done", nil, 200))
	if d.Source != "detected" || strings.Join(d.Toolchains, ",") != "go" || len(d.Items) != 6 || d.Items[0] != "`go build ./...` succeeds." {
		t.Fatalf("detected = %+v", d)
	}
	pv := decode[ProjectView](t, e.ok("PATCH", "/api/projects/p", `{"definition_of_done":["Docs are updated."]}`, 200))
	if len(pv.DefinitionOfDone) != 1 {
		t.Fatalf("project view = %+v", pv.DefinitionOfDone)
	}
	d = decode[dod](t, e.ok("GET", "/api/projects/p/definition-of-done", nil, 200))
	if d.Source != "project" || len(d.Items) != 1 || d.Items[0] != "Docs are updated." || len(d.Toolchains) != 1 {
		t.Fatalf("override = %+v", d)
	}
	e.field("PATCH", "/api/projects/p", `{"definition_of_done":["ok"," "]}`, "definition_of_done[1]")
	e.field("PATCH", "/api/projects/p", `{"definition_of_done":"nope"}`, "definition_of_done")
	// Other fields leave the override alone; null clears it.
	e.ok("PATCH", "/api/projects/p", `{"cleanup_enabled":false}`, 200)
	if d = decode[dod](t, e.ok("GET", "/api/projects/p/definition-of-done", nil, 200)); d.Source != "project" {
		t.Fatalf("a patch of another field cleared the override: %+v", d)
	}
	e.ok("PATCH", "/api/projects/p", `{"definition_of_done":null}`, 200)
	if d = decode[dod](t, e.ok("GET", "/api/projects/p/definition-of-done", nil, 200)); d.Source != "detected" {
		t.Fatalf("null did not clear the override: %+v", d)
	}
	e.ok("GET", "/api/projects/ghost/definition-of-done", nil, 404)
}

// N4: context_tokens round-trips on a backend model and the probe reports the
// windows an Ollama host states, leaving out models that do not answer.
func TestN4_ContextTokensAndProbeContexts(t *testing.T) {
	e := newEnv(t, envOpt{})
	req := backendReq("gpu")
	req["models"] = []map[string]any{{"model": "m1", "context_tokens": 32768}, {"model": "m2"}}
	e.ok("POST", "/api/backends", req, http.StatusCreated)
	bv := decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, 200))
	if bv[0].Models[0].ContextTokens != 32768 || bv[0].Models[1].ContextTokens != 0 {
		t.Fatalf("models = %+v", bv[0].Models)
	}
	e.ok("PATCH", "/api/backends/gpu", `{"models":[{"model":"m1","context_tokens":4096},{"model":"m2"}]}`, 200)
	bv = decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, 200))
	if bv[0].Models[0].ContextTokens != 4096 {
		t.Fatalf("after patch: %+v", bv[0].Models)
	}
	e.field("PATCH", "/api/backends/gpu", `{"models":[{"model":"m1","context_tokens":-1}]}`, "models")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Model string }
		raw, _ := io.ReadAll(r.Body)
		_ = jsonUnmarshal(raw, &body)
		switch {
		case r.URL.Path != "/api/show":
			http.NotFound(w, r)
		case body.Model == "a":
			_, _ = w.Write([]byte(`{"model_info":{"llama.context_length":131072,"llama.embedding_length":4096}}`))
		case body.Model == "b":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			_, _ = w.Write([]byte(`{"model_info":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	got := probeContexts(context.Background(), srv.Client(), srv.URL+"/", "ollama", []string{"a", "b", "c"})
	if len(got) != 1 || got["a"] != 131072 {
		t.Fatalf("contexts = %v", got)
	}
	if got := probeContexts(context.Background(), srv.Client(), srv.URL, "openai_compatible", []string{"a"}); len(got) != 0 {
		t.Fatalf("an openai-compatible host has no context call: %v", got)
	}
}

// N2, N4: the FRD routes. With the lanes at width zero a draft stays queued, so
// a second request is a conflict; parts start new efforts under the one-active
// rule and only once.
func TestN2_FRDDraftSplitAndPartRoutes(t *testing.T) {
	e := newEnv(t, envOpt{widthZero: true})
	e.ok("POST", "/api/projects", map[string]any{"slug": "p", "repo_path": repo(t)}, http.StatusCreated)
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p/efforts", map[string]any{"title": "src"}, http.StatusCreated))
	base := "/api/projects/p/efforts/" + itoa(ef.ID)

	if d := decode[engine.DraftView](t, e.ok("GET", base+"/frd/draft", nil, 200)); d.State != "idle" || d.Error != "" {
		t.Fatalf("idle state = %+v", d)
	}
	e.ok("POST", base+"/frd/draft", `{"description":"x"}`, http.StatusConflict) // no planner model is set
	e.ok("POST", "/api/backends", backendReq("gpu"), http.StatusCreated)
	e.ok("PUT", "/api/candidates/planner", `{"candidates":[{"backend":"gpu","model":"m1"}]}`, 200)
	e.field("POST", base+"/frd/draft", `{"description":"  "}`, "description")
	e.field("POST", base+"/frd/draft", `{"description":"x","bogus":1}`, "bogus")
	e.ok("POST", base+"/frd/split", nil, http.StatusConflict) // nothing to split yet
	e.ok("POST", base+"/frd/draft", `{"description":"x"}`, http.StatusAccepted)
	if d := decode[engine.DraftView](t, e.ok("GET", base+"/frd/draft", nil, 200)); d.State != "running" || d.Started == 0 {
		t.Fatalf("running state = %+v", d)
	}
	e.ok("POST", base+"/frd/draft", `{"description":"y"}`, http.StatusConflict)
	e.ok("POST", base+"/plan/generate", nil, http.StatusConflict)
	sz := decode[map[string]any](t, e.ok("GET", base+"/size", nil, 200))
	for _, k := range []string{"frd_tokens", "plan_tokens", "budget_tokens", "context_tokens_min", "over", "models", "advice"} {
		if _, ok := sz[k]; !ok {
			t.Fatalf("size lacks %s: %v", k, sz)
		}
	}
	e.ok("GET", "/api/projects/p/efforts/9999/size", nil, 404)

	// Parts: stored by a split, started under the one-active-effort rule.
	ctx := context.Background()
	frd := `{"title":"part two","objective":"o","requirements":[{"description":"d"}],"acceptance_criteria":[{"criterion":"c"}]}`
	if err := e.st.ReplaceFRDParts(ctx, "t", ef.ID, []store.FRDPart{{N: 2, Title: "part two", Summary: "o", JSON: []byte(frd), Tokens: 12}}); err != nil {
		t.Fatal(err)
	}
	parts := decode[[]PartView](t, e.ok("GET", base+"/frd/parts", nil, 200))
	if len(parts) != 1 || parts[0].N != 2 || parts[0].Title != "part two" || parts[0].Tokens != 12 || parts[0].StartedEffort != 0 {
		t.Fatalf("parts = %+v", parts)
	}
	e.ok("POST", base+"/frd/parts/2/start", nil, http.StatusConflict) // the project has an active effort
	e.ok("POST", base+"/frd/parts/x/start", nil, http.StatusBadRequest)
	e.ok("POST", base+"/frd/parts/7/start", nil, http.StatusNotFound)
	e.ok("POST", base+"/abandon", `{"confirm":true}`, 200)
	started := decode[EffortView](t, e.ok("POST", base+"/frd/parts/2/start", nil, http.StatusCreated))
	if started.ID == ef.ID || started.Title != "part two" || started.FRD.Latest != 1 {
		t.Fatalf("started effort = %+v", started)
	}
	d, err := e.st.GetDoc(ctx, started.ID, model.DocFRD, 0)
	if err != nil || !strings.Contains(string(d.JSON), `"id":"FRD-`+itoa(started.ID)+`"`) {
		t.Fatalf("started FRD = %s (%v)", d.JSON, err)
	}
	if parts = decode[[]PartView](t, e.ok("GET", base+"/frd/parts", nil, 200)); parts[0].StartedEffort != started.ID {
		t.Fatalf("part not marked started: %+v", parts)
	}
	e.ok("POST", "/api/projects/p/efforts/"+itoa(started.ID)+"/abandon", `{"confirm":true}`, 200)
	e.ok("POST", base+"/frd/parts/2/start", nil, http.StatusConflict) // already started
}
