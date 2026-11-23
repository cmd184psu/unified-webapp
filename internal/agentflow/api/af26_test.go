package api

import (
	"context"
	"net/http"
	"strings"
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

// The auto-risk rules are settings with their defaults (AF-26): on, the
// migrations/auth/config/deploy paths and the risky words, and a save of a
// comma list round-trips.
func TestAutoRiskSettings(t *testing.T) {
	f := newFlow(t, envOpt{})
	s := decode[map[string]any](t, f.ok("GET", "/api/settings", nil, http.StatusOK))
	if s["auto_risk"] != "on" {
		t.Fatalf("auto_risk default = %v, want on", s["auto_risk"])
	}
	paths, _ := s["risky_paths"].(string)
	if !strings.Contains(paths, "internal/agentflow/store/migrate.go") || !strings.Contains(paths, "internal/platform/auth/") {
		t.Fatalf("risky_paths default = %q", paths)
	}
	words, _ := s["risky_words"].(string)
	if !strings.Contains(words, "credential") {
		t.Fatalf("risky_words default = %q", words)
	}

	got := decode[map[string]any](t, f.ok("PUT", "/api/settings", map[string]any{
		"auto_risk": "off", "risky_paths": "docs/", "risky_words": "secret",
	}, http.StatusOK))
	if got["auto_risk"] != "off" || got["risky_paths"] != "docs/" || got["risky_words"] != "secret" {
		t.Fatalf("saved auto-risk settings = %v / %v / %v", got["auto_risk"], got["risky_paths"], got["risky_words"])
	}
}

// A task whose title names a risky subject is stored risky without anyone
// saying so; a docs task stays normal (AF-26).
func TestQueueSeedMarksAutoRisk(t *testing.T) {
	f := newFlow(t, envOpt{})
	f.ok("POST", "/api/projects", map[string]any{"slug": "smoke-p2", "repo_path": repo(t)}, http.StatusCreated)
	ef := decode[EffortView](t, f.ok("POST", "/api/projects/smoke-p2/efforts", map[string]any{"title": "seed"}, http.StatusCreated))
	base := "/api/projects/smoke-p2/efforts/" + itoa(ef.ID)
	risky := map[string]any{"key": "S1", "phase": "P1", "title": "Rotate the credential", "prompt": "p", "acceptance": []string{"a"}}
	docs := map[string]any{"key": "S2", "phase": "P1", "title": "Explain the queue", "prompt": "Write the documentation.", "acceptance": []string{"a"}, "files_touched": []string{"docs/queue.md"}}
	seeded := decode[struct{ Tasks []model.Task }](t, f.ok("POST", base+"/queue/seed", map[string]any{"tasks": []any{risky, docs}}, http.StatusOK))
	if len(seeded.Tasks) != 2 {
		t.Fatalf("seeded tasks = %+v", seeded.Tasks)
	}
	for _, task := range seeded.Tasks {
		switch task.Key {
		case "S1":
			if task.Risk != model.RiskRisky || !strings.Contains(task.RiskReason, "credential") {
				t.Fatalf("a risky title must be stored risky naming the word: %+v", task)
			}
		case "S2":
			if task.Risk != model.RiskNormal || task.RiskReason != "" {
				t.Fatalf("a docs task must stay normal: %+v", task)
			}
		}
	}
}
