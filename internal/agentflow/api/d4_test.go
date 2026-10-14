package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// richFRD uses every FRD field, including the verbatim ones.
const richFRD = `{
  "id": "FRD-RT", "title": "Round trip", "status": "approved", "created": "2026-10-13", "approved_date": "2026-10-14",
  "objective": "Do the thing.", "context": "Because.", "constraints": ["c1", "c2"],
  "groups": [{"id": "G1", "title": "Group", "summary": "s"}],
  "state_model": {"effort_statuses": ["draft", "complete"], "nested": {"k": [1, 2.5, true, null]}},
  "requirements": [{"id": "R1", "group": "G1", "priority": "must", "findings": [1, 2], "description": "Req one"}, {"id": "R2", "description": "Req two"}],
  "acceptance_criteria": [{"id": "AC1", "requirements": ["R1", "R2"], "criterion": "It works"}],
  "decisions": [{"id": "D1", "decision": "Use Go", "rationale": "Because"}],
  "findings": [{"n": 1, "title": "f", "covered_by": ["R1"]}],
  "out_of_scope": ["x"], "planning_spikes": [{"id": "SP1", "question": "q?"}]
}`

func normalized(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	return v
}

func (e *env) addProject(slug string) {
	e.t.Helper()
	e.ok("POST", "/api/projects", map[string]any{"slug": slug, "repo_path": repo(e.t)}, http.StatusCreated)
}

// DBS6: an FRD exported as JSON imports as a new effort at revision 1 and
// exports to the same JSON.
func TestD4_ExportImportRoundTrip(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.addProject("p1")
	e.addProject("p2")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "src"}, http.StatusCreated))
	e.ok("PUT", "/api/projects/p1/efforts/"+itoa(ef.ID)+"/frd", richFRD, http.StatusOK)

	out := e.ok("GET", "/api/efforts/"+itoa(ef.ID)+"/frd/export?format=json", nil, http.StatusOK)
	if !reflect.DeepEqual(normalized(t, out), normalized(t, []byte(richFRD))) {
		t.Fatalf("export differs from the saved FRD:\n%s", out)
	}
	imp := decode[EffortView](t, e.ok("POST", "/api/projects/p2/efforts/import", string(out), http.StatusCreated))
	if imp.ID == ef.ID || imp.Project != "p2" || imp.Title != "Round trip" || imp.FRD.Latest != 1 || imp.Status != model.EffortDraft {
		t.Fatalf("imported effort = %+v", imp)
	}
	back := e.ok("GET", "/api/efforts/"+itoa(imp.ID)+"/frd/export", nil, http.StatusOK)
	if !reflect.DeepEqual(normalized(t, back), normalized(t, out)) {
		t.Fatalf("round trip changed the FRD:\n%s\n%s", out, back)
	}

	// Revisions: ?revision= picks one, the default is the latest.
	e.ok("PUT", "/api/projects/p1/efforts/"+itoa(ef.ID)+"/frd", strings.Replace(richFRD, "Round trip", "Second", 1), http.StatusOK)
	r1 := e.ok("GET", "/api/efforts/"+itoa(ef.ID)+"/frd/export?revision=1", nil, http.StatusOK)
	r2 := e.ok("GET", "/api/efforts/"+itoa(ef.ID)+"/frd/export", nil, http.StatusOK)
	if !strings.Contains(string(r1), "Round trip") || !strings.Contains(string(r2), "Second") {
		t.Fatalf("revisions mixed up:\n%s\n%s", r1, r2)
	}
	e.ok("GET", "/api/efforts/"+itoa(ef.ID)+"/frd/export?revision=9", nil, http.StatusNotFound)
	e.field("GET", "/api/efforts/"+itoa(ef.ID)+"/frd/export?revision=x", nil, "revision")
	e.field("GET", "/api/efforts/"+itoa(ef.ID)+"/frd/export?format=xml", nil, "format")
	e.ok("GET", "/api/efforts/"+itoa(ef.ID)+"/plan/export", nil, http.StatusNotFound)
	e.ok("GET", "/api/efforts/777/export", nil, http.StatusNotFound)

	// The FRD this remediation is built from imports as it stands.
	doc, err := os.ReadFile("../../../docs/agentflow-remediation/frd.json")
	if err != nil {
		t.Fatal(err)
	}
	e.addProject("p3")
	e.ok("POST", "/api/projects/p3/efforts/import", string(doc), http.StatusCreated)
	e.ok("POST", "/api/projects/p3/efforts/import", string(doc), http.StatusConflict) // PJ4: p3 already has an active effort
	e.ok("POST", "/api/projects/ghost/efforts/import", string(doc), http.StatusNotFound)
}

// DBS6: an invalid import is a 400 naming the field, and creates nothing.
func TestD4_InvalidImportNamesTheField(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.addProject("p1")
	const imp = "/api/projects/p1/efforts/import"
	e.field("POST", imp, `{"id":"X","title":"t","objective":"o","requirements":[{"id":"R1","description":5}]}`, "requirements[0].description")
	e.field("POST", imp, `{"id":"X","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}],"bogus":true}`, "bogus")
	e.field("POST", imp, `{"id":"X","title":"t","objective":"o","requirements":[{"description":""}]}`, "requirements[0]")
	e.field("POST", imp, `{"id":"X","title":"t","requirements":[{"id":"R1","description":"d"}]}`, "objective")
	e.field("POST", imp, `{"id":"X","title":"t","objective":"o","requirements":[]}`, "requirements")
	e.field("POST", imp, `{"id":"X","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"},{"id":"R1","description":"e"}]}`, "requirements")
	e.field("POST", imp, `{"id":"X","title":"t","objective":"o","requirements":[{"id":"R1","description":"d"}],"acceptance_criteria":[{"id":"A","requirements":["R2"],"criterion":"c"}]}`, "acceptance_criteria")
	e.field("POST", imp, `{"id":`, "body")
	e.field("POST", imp, ``, "body")
	e.field("POST", imp, `[1,2]`, "body")
	if list := decode[[]EffortView](t, e.ok("GET", "/api/projects/p1/efforts", nil, 200)); len(list) != 0 {
		t.Fatalf("failed imports left efforts: %+v", list)
	}
	// N3: no id has to be typed; the filler gives the FRD its id, requirement
	// and criterion ids, the links and the defaults.
	ef := decode[EffortView](t, e.ok("POST", imp, `{"title":"t","objective":"o","requirements":[{"description":"d"}],"acceptance_criteria":[{"criterion":"c"}]}`, http.StatusCreated))
	doc := decode[DocView](t, e.ok("GET", "/api/projects/p1/efforts/"+itoa(ef.ID)+"/frd", nil, 200))
	for _, want := range []string{`"id":"FRD-` + itoa(ef.ID) + `"`, `"status":"draft"`, `"id":"R1"`, `"id":"AC1"`, `"requirements":["R1"]`} {
		if !strings.Contains(string(doc.JSON), want) {
			t.Fatalf("filled FRD lacks %s: %s", want, doc.JSON)
		}
	}
}

const evil = `<script>alert(1)</script> <img src=x onerror=alert(1)> & *bold* _it_ [l](http://x) # h`

// DBS6: markdown carries no raw HTML from the input, for the FRD, the plan and
// the whole effort.
func TestD4_MarkdownEscapesInput(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.addProject("p1")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": evil}, http.StatusCreated))
	frd := map[string]any{
		"id": "F<b>", "title": evil, "objective": evil + "\n- not a list\n1. nor this\n<div>x</div>", "context": evil,
		"constraints":         []string{evil},
		"requirements":        []map[string]any{{"id": "R1", "description": evil, "priority": "<i>must</i>"}},
		"acceptance_criteria": []map[string]any{{"id": "AC1", "requirements": []string{"R1"}, "criterion": evil}},
		"state_model":         map[string]any{evil: []any{evil, map[string]any{"k": evil}}},
		"decisions":           []map[string]any{{"id": "D1", "decision": evil, "rationale": evil}},
	}
	base := "/api/projects/p1/efforts/" + itoa(ef.ID)
	e.ok("PUT", base+"/frd", frd, http.StatusOK)
	e.ok("PUT", base+"/plan", map[string]any{"principles": []string{evil}, "phases": []map[string]any{{"id": "P1", "name": evil}}, evil: evil}, http.StatusOK)

	for _, path := range []string{"/frd/export", "/plan/export", "/export"} {
		body := e.ok("GET", "/api/efforts/"+itoa(ef.ID)+path+"?format=md", nil, http.StatusOK)
		s := string(body)
		if strings.ContainsAny(s, "<>") {
			t.Errorf("%s: markdown contains raw angle brackets:\n%s", path, s)
		}
		if !strings.Contains(s, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("%s: script text missing or unescaped:\n%s", path, s)
		}
		if strings.Contains(s, "*bold*") || strings.Contains(s, "[l](") {
			t.Errorf("%s: markdown syntax from the input survived:\n%s", path, s)
		}
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/efforts/"+itoa(ef.ID)+"/export?format=md", nil))
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.Contains(rec.Header().Get("Content-Disposition"), "effort-"+itoa(ef.ID)+".md") {
		t.Errorf("headers = %v", rec.Header())
	}
}

// The effort export carries the FRD, the plan, the queue and the derived
// task statuses, as JSON and as markdown.
func TestD4_EffortExportHasQueueAndStatuses(t *testing.T) {
	f := newFlow(t, envOpt{})
	f.planAndCompile()
	var ex EffortExport
	if err := json.Unmarshal(f.ok("GET", "/api/efforts/"+itoa(f.effort)+"/export", nil, http.StatusOK), &ex); err != nil {
		t.Fatal(err)
	}
	if ex.Effort.ID != f.effort || ex.FRD == nil || ex.Plan == nil || !ex.Plan.Approved || ex.Queue == nil || len(ex.Queue.Tasks) != 2 ||
		ex.Statuses["T001"] != model.StatusReady || ex.Statuses["T002"] != model.StatusBlocked {
		t.Fatalf("export = %+v", ex)
	}
	md := string(f.ok("GET", "/api/efforts/"+itoa(f.effort)+"/export?format=md", nil, http.StatusOK))
	for _, want := range []string{"# Effort", "T001: first \\[ready\\]", "create a.txt", "## Plan (revision"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q:\n%s", want, md)
		}
	}
}

// AT12 and RP5: an effort is created with budgets, patched later, and its view
// carries usage against them; a bad budget names its field.
func TestAT12_EffortBudgetAndDeliberateRoundTrip(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.addProject("p1")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p1/efforts", map[string]any{"title": "x", "budget_tokens": 1000}, http.StatusCreated))
	if ef.BudgetTokens != 1000 || ef.BudgetSeconds != 0 || ef.Budget == nil || ef.Budget.State != "ok" || ef.Budget.TokenLimit != 1000 {
		t.Fatalf("created = %+v / %+v", ef, ef.Budget)
	}
	base := "/api/projects/p1/efforts/" + itoa(ef.ID)
	ef = decode[EffortView](t, e.ok("PATCH", base, map[string]any{"budget_seconds": 3600, "deliberate": true}, http.StatusOK))
	if ef.BudgetTokens != 1000 || ef.BudgetSeconds != 3600 || !ef.Deliberate {
		t.Fatalf("patched = %+v", ef)
	}
	e.field("PATCH", base, map[string]any{"budget_tokens": -1}, "budget_tokens")
	ef = decode[EffortView](t, e.ok("PATCH", base, map[string]any{"budget_tokens": 0, "budget_seconds": 0}, http.StatusOK))
	if ef.Budget != nil || !ef.Deliberate {
		t.Fatalf("limits removed = %+v", ef)
	}
	e.ok("PATCH", "/api/projects/other/efforts/"+itoa(ef.ID), map[string]any{}, http.StatusNotFound)
}
