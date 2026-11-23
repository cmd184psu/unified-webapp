package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// attentionBulkNote is a long single-clause progress note. Twenty tasks of
// fifteen of these share hundred-word runs, which used to send the
// notes-vs-notes scan quadratic over every entry ever written with a cubic
// phrase match per pair, stalling GET /api/attention.
func attentionBulkNote(bay, unit int) string {
	return fmt.Sprintf("progress note for calibration review of the widget assembly line: the pressure gauge reading settled within tolerance "+
		"and the conveyor belt alignment passed visual inspection and the torque wrench setting matched the specification sheet "+
		"and the pressure gauge reading settled again within tolerance and the conveyor belt alignment passed a second visual "+
		"inspection and the torque wrench setting matched the specification sheet for bay %d unit %d with the shift supervisor "+
		"countersignature and the quality checklist archived in the logbook cabinet", bay, unit)
}

// TestAttentionLongHistoryAnswersQuickly is a regression test: an effort with
// a long progress history across many tasks must still get an attention
// answer within 5 s, and a genuine contradiction among the newest notes must
// still be reported.
func TestAttentionLongHistoryAnswersQuickly(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	e.addProject("attn")
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/attn/efforts", map[string]any{"title": "x"}, http.StatusCreated))
	var defs []model.TaskDef
	for i := 1; i <= 20; i++ {
		defs = append(defs, model.TaskDef{Key: fmt.Sprintf("T%d", i), Phase: "P", Title: "t", Prompt: "do", Acceptance: []string{"ok"}})
	}
	q, err := e.st.SaveQueue(ctx, "t", ef.ID, defs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ApproveQueue(ctx, "t", ef.ID, q.Hash); err != nil {
		t.Fatal(err)
	}
	running := model.EffortRunning
	if _, err := e.st.UpdateEffort(ctx, "t", ef.ID, store.EffortPatch{Status: &running}); err != nil {
		t.Fatal(err)
	}
	tasks, err := e.st.ListTasks(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	for ti, task := range tasks {
		for n := 0; n < 15; n++ {
			if err := e.st.AppendProgress(ctx, task.ID, 0, attentionBulkNote(ti, n)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A genuine contradiction among the newest notes of the first task.
	if err := e.st.AppendProgress(ctx, tasks[0].ID, 0, "the distance calculation must set ConnectedToCapital to true for all regions in the northern province sector grid"); err != nil {
		t.Fatal(err)
	}
	if err := e.st.AppendProgress(ctx, tasks[0].ID, 0, "the distance calculation keeps ConnectedToCapital false for all regions in the northern province sector grid"); err != nil {
		t.Fatal(err)
	}

	type result struct {
		code int
		body []byte
	}
	done := make(chan result, 1)
	go func() {
		req := httptest.NewRequest("GET", "/api/attention", nil)
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, req)
		done <- result{rec.Code, rec.Body.Bytes()}
	}()
	var res result
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("GET /api/attention did not answer within 5 s on a long progress history")
	}
	if res.code != http.StatusOK {
		t.Fatalf("GET /api/attention = %d: %s", res.code, res.body)
	}
	var items []engine.AttentionItem
	if err := jsonUnmarshal(res.body, &items); err != nil {
		t.Fatalf("attention body is not JSON: %v: %s", err, res.body)
	}
	for _, it := range items {
		if it.Kind == "conflicting_guidance" && strings.Contains(it.Message, "T1") {
			return
		}
	}
	t.Fatalf("no conflicting_guidance item for the contradictory T1 notes: %v", items)
}
