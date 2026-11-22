package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// TestAF15_ParkBacklogAndBundleRoundTrip exercises the HTTP surface: park a
// prepared effort, list it, return it to queue_approved, then export and import
// the effort as a bundle. A bundle whose content changed after export loses its
// approvals and lands in draft.
func TestAF15_ParkBacklogAndBundleRoundTrip(t *testing.T) {
	f := newFlow(t, envOpt{})
	f.planAndCompile()
	f.ok("POST", f.base+"/frd/approve", map[string]any{"revision": 1}, http.StatusOK)
	for _, slug := range []string{"p2", "p3"} {
		f.ok("POST", "/api/projects", map[string]any{"slug": slug, "repo_path": repo(t)}, http.StatusCreated)
	}
	q := decode[struct{ Hash string }](t, f.ok("GET", f.base+"/queue", nil, http.StatusOK))
	f.ok("POST", f.base+"/queue/approve", map[string]any{"hash": q.Hash}, http.StatusOK)

	ev := decode[EffortView](t, f.ok("POST", f.base+"/park", map[string]any{"priority": 7, "start_condition": "branch main contains HEAD"}, http.StatusOK))
	if ev.Status != model.EffortReady || ev.Priority != 7 || ev.StartCondition == "" {
		t.Fatalf("parked = %+v", ev)
	}
	list := decode[[]EffortView](t, f.ok("GET", "/api/backlog", nil, http.StatusOK))
	if len(list) != 1 || list[0].ID != f.effort {
		t.Fatalf("backlog = %+v", list)
	}
	ev = decode[EffortView](t, f.ok("POST", f.base+"/unpark", nil, http.StatusOK))
	if ev.Status != model.EffortQueueAppr {
		t.Fatalf("unparked status = %s", ev.Status)
	}
	f.ok("POST", f.base+"/park", map[string]any{"priority": 7}, http.StatusOK)

	var ex EffortExport
	if err := json.Unmarshal(f.ok("GET", "/api/efforts/"+itoa(f.effort)+"/export", nil, http.StatusOK), &ex); err != nil {
		t.Fatal(err)
	}
	if ex.Format != bundleFormat || ex.ContentHash == "" || !ex.Approvals.FRD || !ex.Approvals.Plan || !ex.Approvals.Queue {
		t.Fatalf("bundle = %+v", ex)
	}
	imported := decode[EffortView](t, f.ok("POST", "/api/import?project=p2", ex, http.StatusCreated))
	if imported.Status != model.EffortReady || imported.Project != "p2" || imported.Priority != 7 {
		t.Fatalf("imported = %+v", imported)
	}
	tampered := ex
	tampered.FRD = &DocView{JSON: []byte(strings.Replace(string(ex.FRD.JSON), "FRD-T", "FRD-TAMPER", 1))}
	imported = decode[EffortView](t, f.ok("POST", "/api/import?project=p3", tampered, http.StatusCreated))
	if imported.Status == model.EffortReady {
		t.Fatalf("a tampered bundle kept its approvals: %+v", imported)
	}
}
