package api

import (
	"context"
	"net/http"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// The task edit route accepts risk and risk_reason (AF-26); the value is read
// back, and anything outside normal|risky is a 400 that names risk.
func TestTaskRiskOnEdit(t *testing.T) {
	f := newFlow(t, envOpt{})
	def := model.TaskDef{Key: "T1", Phase: "P1", Title: "t", Prompt: "p", Acceptance: []string{"a"}}
	if _, err := f.st.SaveQueue(context.Background(), "tester", f.effort, []model.TaskDef{def}); err != nil {
		t.Fatal(err)
	}
	q := decode[struct{ Tasks []model.Task }](t, f.ok("PUT", f.base+"/tasks/T1", map[string]any{
		"phase": "P1", "title": "t", "prompt": "p", "acceptance": []string{"a"},
		"risk": "risky", "risk_reason": "touches the migration",
	}, http.StatusOK))
	if len(q.Tasks) != 1 || q.Tasks[0].Risk != model.RiskRisky || q.Tasks[0].RiskReason != "touches the migration" {
		t.Fatalf("tasks after edit = %+v", q.Tasks)
	}
	f.field("PUT", f.base+"/tasks/T1", map[string]any{
		"phase": "P1", "title": "t", "prompt": "p", "acceptance": []string{"a"}, "risk": "spicy",
	}, "risk")
}

// Creating tasks through queue/seed stores the risk too, and refuses a bad one
// with a 400 naming risk.
func TestTaskRiskOnCreate(t *testing.T) {
	f := newFlow(t, envOpt{})
	f.ok("POST", "/api/projects", map[string]any{"slug": "smoke-p2", "repo_path": repo(t)}, http.StatusCreated)
	ef := decode[EffortView](t, f.ok("POST", "/api/projects/smoke-p2/efforts", map[string]any{"title": "seed"}, http.StatusCreated))
	base := "/api/projects/smoke-p2/efforts/" + itoa(ef.ID)
	task := map[string]any{"key": "S1", "phase": "P1", "title": "t", "prompt": "p", "acceptance": []string{"a"}}
	task["risk"], task["risk_reason"] = "risky", "seeded on purpose"
	seeded := decode[struct{ Tasks []model.Task }](t, f.ok("POST", base+"/queue/seed", map[string]any{"tasks": []any{task}}, http.StatusOK))
	if len(seeded.Tasks) != 1 || seeded.Tasks[0].Risk != model.RiskRisky || seeded.Tasks[0].RiskReason != "seeded on purpose" {
		t.Fatalf("seeded tasks = %+v", seeded.Tasks)
	}

	bad := map[string]any{"key": "S1", "phase": "P1", "title": "t", "prompt": "p", "acceptance": []string{"a"}, "risk": "spicy"}
	f.field("POST", base+"/queue/seed", map[string]any{"tasks": []any{bad}}, "risk")
}
