package api

import (
	"context"
	"net/http"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/platform/auth"
)

// POST /api/projects/{slug}/rulings calls Engine.AddRuling: a pending
// decision-inbox row, attributed to the caller, and no write of the rulings
// file. A task id the store does not know does not create a row.
func TestAddRulingRoute(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.addProject("p")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/p/efforts", map[string]any{"title": "rulings"}, http.StatusCreated))
	ctx := context.Background()
	if _, err := e.st.SaveQueue(ctx, "tester", ef.ID, []model.TaskDef{
		{Key: "T1", Phase: "P1", Title: "t", Prompt: "do it", Acceptance: []string{"ok"}},
	}); err != nil {
		t.Fatal(err)
	}
	tasks, err := e.st.ListTasks(ctx, ef.ID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks: %+v %v", tasks, err)
	}

	body := map[string]any{
		"effort": ef.ID, "task": tasks[0].ID, "ruling_id": "R-API", "proposed_text": "keep the guard",
	}
	code, out := e.call(&auth.Principal{Subject: "coord"}, "POST", "/api/projects/p/rulings", body)
	if code != http.StatusCreated {
		t.Fatalf("add ruling = %d %s", code, out)
	}
	var resp struct {
		Status string `json:"status"`
	}
	decodeJSON(t, out, &resp)
	if resp.Status != "pending" {
		t.Fatalf("response = %s", out)
	}
	pending, err := e.st.ListPendingDecisionInbox(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want 1", pending)
	}
	got := pending[0]
	if got.Effort != ef.ID || got.Task != tasks[0].ID || got.RulingID != "R-API" || got.ProposedBy != "coord" || got.ProposedText != "keep the guard" || got.Status != "pending" {
		t.Fatalf("entry = %+v", got)
	}

	e.field("POST", "/api/projects/p/rulings", `{`, "body")

	code, out = e.call(nil, "POST", "/api/projects/p/rulings", map[string]any{
		"effort": ef.ID, "task": int64(999999), "ruling_id": "R-X", "proposed_text": "missing task",
	})
	if code < 400 {
		t.Fatalf("unknown task = %d %s, want a client or server error", code, out)
	}
	pending, err = e.st.ListPendingDecisionInbox(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("failed add changed the inbox: %+v", pending)
	}
}

func decodeJSON(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := jsonUnmarshal(raw, v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}
