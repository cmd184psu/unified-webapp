package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/engine"
	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// conflictTunePair is a genuinely contradictory note pair about one subject:
// the distance calculation must set ConnectedToCapital to true vs keeps it
// false. It shares a >=3-word phrase with opposite polarity, so the
// clause-polarity heuristic fires.
var conflictTunePair = [2]string{
	"the distance calculation must set ConnectedToCapital to true for all regions in the northern province sector grid",
	"the distance calculation keeps ConnectedToCapital false for all regions in the northern province sector grid",
}

// conflictTuneSetup creates a running effort with the given task keys and
// returns its tasks in queue order.
func conflictTuneSetup(t *testing.T, e *env, slug string, keys []string) ([]model.Task, int64) {
	t.Helper()
	ctx := context.Background()
	e.addProject(slug)
	ef := decode[EffortView](t, e.ok("POST", "/api/projects/"+slug+"/efforts", map[string]any{"title": "x"}, http.StatusCreated))
	var defs []model.TaskDef
	for _, k := range keys {
		defs = append(defs, model.TaskDef{Key: k, Phase: "P", Title: "t", Prompt: "do", Acceptance: []string{"ok"}})
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
	return tasks, ef.ID
}

func conflictTuneAttention(t *testing.T, e *env) []engine.AttentionItem {
	t.Helper()
	start := time.Now()
	req := httptest.NewRequest("GET", "/api/attention", nil)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	el := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/attention = %d: %s", rec.Code, rec.Body.String())
	}
	items := decode[[]engine.AttentionItem](t, rec.Body.Bytes())
	t.Logf("GET /api/attention returned %d items in %s", len(items), el)
	return items
}

func countKind(items []engine.AttentionItem, kind string) int {
	n := 0
	for _, it := range items {
		if it.Kind == kind {
			n++
		}
	}
	return n
}

// Unrelated notes across many tasks must raise nothing: notes of two
// different tasks never conflict, even when every task has several notes.
func TestConflictTuneUnrelatedTasksNoItems(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	var keys []string
	for i := 1; i <= 200; i++ {
		keys = append(keys, fmt.Sprintf("T%03d", i))
	}
	tasks, _ := conflictTuneSetup(t, e, "ct-none", keys)
	for i, task := range tasks {
		notes := []string{
			fmt.Sprintf("calibrated the lobby turnstile counter for floor %d", i),
			fmt.Sprintf("restocked the cafeteria napkin dispensers on level %d", i),
			fmt.Sprintf("polished the rooftop antenna housing on wing %d", i),
		}
		for _, n := range notes {
			if err := e.st.AppendProgress(ctx, task.ID, 0, n); err != nil {
				t.Fatal(err)
			}
		}
	}
	items := conflictTuneAttention(t, e)
	if n := countKind(items, "conflicting_guidance"); n != 0 {
		t.Fatalf("200 unrelated tasks raised %d conflicting_guidance items, want 0", n)
	}
	if n := countKind(items, "conflicting_guidance_summary"); n != 0 {
		t.Fatalf("unexpected %d conflicting-guidance summary items, want 0", n)
	}
}

// Two truly contradictory notes on one task raise exactly one item.
func TestConflictTuneSameTaskContradiction(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	tasks, _ := conflictTuneSetup(t, e, "ct-one", []string{"T1"})
	for _, n := range conflictTunePair {
		if err := e.st.AppendProgress(ctx, tasks[0].ID, 0, n); err != nil {
			t.Fatal(err)
		}
	}
	items := conflictTuneAttention(t, e)
	if n := countKind(items, "conflicting_guidance"); n != 1 {
		t.Fatalf("one contradictory pair raised %d conflicting_guidance items, want 1: %v", n, items)
	}
}

// A note contradicting a ruling (the guidance.conflict event path) raises one
// item. The event is written exactly as the reviewer-repair path writes it.
func TestConflictTuneNoteVsRuling(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	tasks, efID := conflictTuneSetup(t, e, "ct-ruling", []string{"T1"})
	_ = tasks
	note := "reviewer repair dropped: it contradicted an injected ruling (R-3); not a repair"
	if err := e.st.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := store.AppendEvent(tx, "system", "guidance.conflict", "effort",
			strconv.FormatInt(efID, 10), map[string]string{"task": "T1", "note": note})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	items := conflictTuneAttention(t, e)
	if n := countKind(items, "conflicting_guidance"); n != 1 {
		t.Fatalf("note-vs-ruling raised %d conflicting_guidance items, want 1: %v", n, items)
	}
}

// Sixty contradictory tasks are capped: 10 items plus one summary naming the
// 50 hidden.
func TestConflictTuneManyTasksCapped(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	var keys []string
	for i := 1; i <= 60; i++ {
		keys = append(keys, fmt.Sprintf("T%02d", i))
	}
	tasks, _ := conflictTuneSetup(t, e, "ct-many", keys)
	for _, task := range tasks {
		for _, n := range conflictTunePair {
			if err := e.st.AppendProgress(ctx, task.ID, 0, n); err != nil {
				t.Fatal(err)
			}
		}
	}
	items := conflictTuneAttention(t, e)
	if n := countKind(items, "conflicting_guidance"); n != 10 {
		t.Fatalf("60 contradictory tasks raised %d conflicting_guidance items, want 10", n)
	}
	var summaries []engine.AttentionItem
	for _, it := range items {
		if it.Kind == "conflicting_guidance_summary" {
			summaries = append(summaries, it)
		}
	}
	if len(summaries) != 1 {
		t.Fatalf("want exactly 1 conflicting-guidance summary item, got %d: %v", len(summaries), items)
	}
	if !strings.Contains(summaries[0].Message, "50 more conflicting-guidance items hidden") {
		t.Fatalf("summary message = %q, want it to name the 50 hidden items", summaries[0].Message)
	}
}

// Only the newest 10 notes per task are compared: a contradiction buried under
// 10 newer unrelated notes is out of the window and raises nothing.
func TestConflictTuneWindowIgnoresEvictedNotes(t *testing.T) {
	e := newEnv(t, envOpt{})
	ctx := context.Background()
	tasks, _ := conflictTuneSetup(t, e, "ct-window", []string{"T1"})
	// The contradictory pair is the oldest; ten unrelated notes follow it.
	for _, n := range conflictTunePair {
		if err := e.st.AppendProgress(ctx, tasks[0].ID, 0, n); err != nil {
			t.Fatal(err)
		}
	}
	fill := []string{"lobby turnstile", "cafeteria napkins", "rooftop antenna", "parking gate",
		"elevator music", "stairwell paint", "mailroom shelves", "garden sprinklers",
		"kitchen tiles", "window blinds"}
	for _, f := range fill {
		if err := e.st.AppendProgress(ctx, tasks[0].ID, 0, "checked the "+f+" during the morning round"); err != nil {
			t.Fatal(err)
		}
	}
	items := conflictTuneAttention(t, e)
	if n := countKind(items, "conflicting_guidance"); n != 0 {
		t.Fatalf("evicted contradiction raised %d conflicting_guidance items, want 0: %v", n, items)
	}
}
