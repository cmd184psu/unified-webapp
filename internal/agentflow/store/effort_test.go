package store

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

func newProject(t *testing.T, s *Store, slug string) {
	t.Helper()
	if err := s.CreateProject(ctx, "tester", Project{Slug: slug, RepoPath: mkRepo(t), ModelScope: "local"}); err != nil {
		t.Fatal(err)
	}
}

func newEffort(t *testing.T, s *Store, project string) model.Effort {
	t.Helper()
	e, err := s.CreateEffort(ctx, "tester", project, "an effort", false)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func qdef(key string, deps ...string) model.TaskDef {
	return model.TaskDef{Key: key, Phase: "P1", Title: key, Prompt: "do " + key, Acceptance: []string{"ok"}, DependsOn: deps}
}

// queueFixture returns an effort with a saved queue A <- B and an independent C.
func queueFixture(t *testing.T) (*Store, model.Effort, map[string]int64) {
	t.Helper()
	s, _ := openTemp(t)
	newProject(t, s, "proj")
	e := newEffort(t, s, "proj")
	if _, err := s.SaveQueue(ctx, "tester", e.ID, []model.TaskDef{qdef("A"), qdef("B", "A"), qdef("C")}); err != nil {
		t.Fatal(err)
	}
	ts, err := s.ListTasks(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, tk := range ts {
		ids[tk.Key] = tk.ID
	}
	return s, e, ids
}

var testLim = model.Limits{MaxAttempts: 5, StuckThreshold: 3, MaxInfraRetries: 5}

func TestOneActiveEffortPerProject(t *testing.T) { // PJ4
	s, _ := openTemp(t)
	newProject(t, s, "one")
	newProject(t, s, "two")
	e := newEffort(t, s, "one")
	if _, err := s.CreateEffort(ctx, "tester", "one", "second", false); !errors.Is(err, ErrConflict) || HTTPStatus(err) != 409 {
		t.Fatalf("second active effort: %v, want ErrConflict", err)
	}
	newEffort(t, s, "two") // another project is unaffected
	// The partial unique index backs the check up even when the tx check is bypassed.
	if _, err := s.db.Exec(`INSERT INTO efforts (project, title, status, created, updated) VALUES ('one', 'x', 'running', 0, 0)`); err == nil {
		t.Fatal("index allowed a second active effort")
	}
	done := model.EffortComplete
	if _, err := s.UpdateEffort(ctx, "tester", e.ID, EffortPatch{Status: &done}); err != nil {
		t.Fatal(err)
	}
	e2 := newEffort(t, s, "one") // finished efforts free the slot
	got, err := s.ActiveEffort(ctx, "one")
	if err != nil || got.ID != e2.ID {
		t.Fatalf("ActiveEffort = %v, %v", got.ID, err)
	}
	if _, err := s.UpdateEffort(ctx, "tester", e.ID, EffortPatch{Status: &done}); err != nil {
		t.Fatalf("same-status update of finished effort: %v", err)
	}
	draft := model.EffortDraft
	if _, err := s.UpdateEffort(ctx, "tester", e.ID, EffortPatch{Status: &draft}); !errors.Is(err, ErrConflict) {
		t.Fatalf("reviving a finished effort: %v, want ErrConflict", err)
	}
	if _, err := s.CreateEffort(ctx, "tester", "ghost", "x", false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown project: %v", err)
	}
}

func TestEffortValidationAndEvents(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "p")
	if _, err := s.CreateEffort(ctx, "tester", "p", " ", false); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank title: %v", err)
	}
	e := newEffort(t, s, "p")
	bad := "nonsense"
	_, err := s.UpdateEffort(ctx, "tester", e.ID, EffortPatch{Status: &bad})
	wantField(t, err, "status")
	_, err = s.UpdateEffort(ctx, "tester", e.ID, EffortPatch{RunState: &bad})
	wantField(t, err, "run_state")
	yes := true
	run := model.RunRunning
	br := "agentflow/e1"
	u, err := s.UpdateEffort(ctx, "alice", e.ID, EffortPatch{YOLO: &yes, RunState: &run, IntegrationBranch: &br})
	if err != nil || !u.YOLO || u.RunState != run || u.IntegrationBranch != br {
		t.Fatalf("update: %+v %v", u, err)
	}
	evs, _ := s.ListEvents(ctx, "effort", "1", 0)
	if len(evs) != 2 || evs[1].Actor != "alice" || evs[1].Action != "effort.update" {
		t.Fatalf("events: %+v", evs)
	}
	if _, err := s.GetEffort(ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetEffort: %v", err)
	}
}

func TestSchemaEnumChecksAndNoStatusColumn(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "p")
	e := newEffort(t, s, "p")
	rows, _ := s.db.Query(`PRAGMA table_info(tasks)`)
	for rows.Next() {
		var cid, nn, pk int
		var name, typ string
		var dflt any
		rows.Scan(&cid, &name, &typ, &nn, &dflt, &pk)
		if name == "status" {
			t.Fatal("tasks has a status column")
		}
	}
	rows.Close()
	bad := []string{
		`INSERT INTO efforts (project, title, status, created, updated) VALUES ('p2', 'x', 'bogus', 0, 0)`,
		`INSERT INTO documents (effort, kind, revision, json, created_by, created) VALUES (1, 'bogus', 1, '{}', 'a', 0)`,
		`INSERT INTO queues (effort, revision, hash, status) VALUES (1, 1, 'h', 'bogus')`,
		`INSERT INTO stages (effort, role, state) VALUES (1, 'bogus', 'queued')`,
		`INSERT INTO stages (effort, role, state) VALUES (1, 'worker', 'bogus')`,
		`INSERT INTO stages (role, state) VALUES ('worker', 'queued')`,
		`INSERT INTO stages (attempt, effort, role, state) VALUES (1, 1, 'worker', 'queued')`,
		`INSERT INTO documents (effort, kind, revision, json, created_by, created) VALUES (999, 'frd', 1, '{}', 'a', 0)`,
	}
	for _, q := range bad {
		if _, err := s.db.Exec(q); err == nil {
			t.Errorf("accepted: %s", q)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO stages (effort, role, state) VALUES (?, 'planner', 'queued')`, e.ID); err != nil {
		t.Fatalf("effort stage rejected: %v", err)
	}
}

func TestDocRevisionsImmutable(t *testing.T) { // AC-DBS3 (store)
	s, _ := openTemp(t)
	newProject(t, s, "p")
	e := newEffort(t, s, "p")
	if _, err := s.SaveDoc(ctx, "alice", e.ID, model.DocPlan, []byte(`[1]`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-object plan: %v", err)
	}
	if _, err := s.SaveDoc(ctx, "alice", e.ID, model.DocFRD, []byte(`{"id":"x"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("incomplete frd: %v", err)
	}
	if _, err := s.SaveDoc(ctx, "alice", e.ID, "other", []byte(`{}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad kind: %v", err)
	}
	for _, body := range []string{`{"approach":"v1"}`, `{"approach":"v2"}`} {
		if _, err := s.SaveDoc(ctx, "alice", e.ID, model.DocPlan, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ApprovedRevision(ctx, e.ID, model.DocPlan); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nothing approved yet: %v", err)
	}
	if _, err := s.ApproveDoc(ctx, "bob", e.ID, model.DocPlan, 9); !errors.Is(err, ErrNotFound) {
		t.Fatalf("approve missing revision: %v", err)
	}
	if _, err := s.ApproveDoc(ctx, "bob", e.ID, model.DocPlan, 2); err != nil {
		t.Fatal(err)
	}
	d3, err := s.SaveDoc(ctx, "alice", e.ID, model.DocPlan, []byte(`{"approach":"v3"}`))
	if err != nil || d3.Revision != 3 {
		t.Fatalf("edit after approval: rev %d, %v", d3.Revision, err)
	}
	d2, err := s.GetDoc(ctx, e.ID, model.DocPlan, 2)
	if err != nil || string(d2.JSON) != `{"approach":"v2"}` {
		t.Fatalf("approved revision changed: %s %v", d2.JSON, err)
	}
	ap, err := s.ApprovedRevision(ctx, e.ID, model.DocPlan)
	if err != nil || ap.Revision != 2 || ap.ApprovedBy != "bob" {
		t.Fatalf("approved = %+v, %v", ap, err)
	}
	if latest, _ := s.GetDoc(ctx, e.ID, model.DocPlan, 0); latest.Revision != 3 {
		t.Fatalf("latest = %d", latest.Revision)
	}
	if _, err := s.ApproveDoc(ctx, "bob", e.ID, model.DocReview, 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("approving a review: %v", err)
	}
}

func TestQueueSaveRejectsVerifyAndHashes(t *testing.T) { // AC-V1, QA2
	s, e, _ := queueFixture(t)
	if _, err := model.ParseQueue([]byte(`{"tasks":[{"key":"A","title":"t","prompt":"p","acceptance":["a"],"verify":"x"}]}`)); err == nil {
		t.Fatal("verify accepted")
	}
	if _, err := s.SaveQueue(ctx, "tester", e.ID, []model.TaskDef{qdef("A", "A")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cycle: %v", err)
	}
	q, err := s.GetQueue(ctx, e.ID)
	if err != nil || q.Status != model.QueueDraft || q.Hash != model.QueueHash(q.Tasks) || len(q.Tasks) != 3 {
		t.Fatalf("queue = %+v, %v", q, err)
	}
	if q.Tasks[0].DependsOn == nil || q.Tasks[0].Acceptance == nil || q.Tasks[0].FilesTouched == nil {
		t.Fatalf("a task list is nil and would serialize as JSON null: %+v", q.Tasks[0])
	}
	if _, err := s.ApprovedQueueHash(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("draft queue has an approved hash: %v", err)
	}
	if q.Tasks[1].Key != "B" || len(q.Tasks[1].DependsOn) != 1 || q.Tasks[1].DependsOn[0] != "A" {
		t.Fatalf("deps lost: %+v", q.Tasks[1])
	}
	if _, err := s.ApproveQueue(ctx, "alice", e.ID, "stale-hash"); !errors.Is(err, ErrConflict) {
		t.Fatalf("approve with wrong hash: %v", err)
	}
	ap, err := s.ApproveQueue(ctx, "alice", e.ID, q.Hash)
	if err != nil || ap.Status != model.QueueApproved || ap.ApprovedBy != "alice" {
		t.Fatalf("approve: %+v %v", ap, err)
	}
	if _, err := s.ApproveQueue(ctx, "alice", e.ID, q.Hash); !errors.Is(err, ErrConflict) {
		t.Fatalf("double approve: %v", err)
	}
	h, _ := s.ApprovedQueueHash(ctx, e.ID)
	if h != ap.Hash {
		t.Fatal("approved hash mismatch")
	}
	// Editing a task returns the queue to draft with a new hash.
	edited := qdef("C")
	edited.Prompt = "changed"
	q2, err := s.EditTask(ctx, "alice", e.ID, edited)
	if err != nil || q2.Status != model.QueueDraft || q2.Hash == ap.Hash || q2.Revision != ap.Revision+1 {
		t.Fatalf("edit: %+v %v", q2, err)
	}
	if _, err := s.ApprovedQueueHash(ctx, e.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("edited queue still approved")
	}
	if _, err := s.EditTask(ctx, "alice", e.ID, qdef("Z")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit unknown task: %v", err)
	}
	if _, err := s.EditTask(ctx, "alice", e.ID, qdef("A", "B")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("edit into cycle: %v", err)
	}
	// Recompile replaces the queue until attempts exist.
	if _, err := s.SaveQueue(ctx, "alice", e.ID, []model.TaskDef{qdef("X")}); err != nil {
		t.Fatal(err)
	}
	ts, _ := s.ListTasks(ctx, e.ID)
	if len(ts) != 1 || ts[0].Key != "X" {
		t.Fatalf("tasks after recompile: %+v", ts)
	}
	if _, err := s.StartAttempt(ctx, ts[0].ID, model.AttemptNormal, "", "m"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveQueue(ctx, "alice", e.ID, []model.TaskDef{qdef("Y")}); !errors.Is(err, ErrConflict) {
		t.Fatalf("recompile with attempts: %v", err)
	}
}

func TestAuditEventsOnHumanActions(t *testing.T) {
	s, e, ids := queueFixture(t)
	s.ApproveQueue(ctx, "alice", e.ID, mustHash(t, s, e.ID))
	s.OverrideTask(ctx, "bob", ids["A"], "manual")
	for i := 0; i < testLim.MaxInfraRetries; i++ {
		s.RecordInfraRetry(ctx, ids["C"], 0, "x")
	}
	if err := s.ResetTask(ctx, "carol", ids["C"], true, testLim); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	evs, _ := s.ListEvents(ctx, "", "", 0)
	for _, ev := range evs {
		seen[ev.Action] = ev.Actor
	}
	for action, actor := range map[string]string{"effort.create": "tester", "queue.save": "tester", "queue.approve": "alice", "task.override": "bob", "task.reset": "carol"} {
		if seen[action] != actor {
			t.Errorf("event %s actor = %q, want %q", action, seen[action], actor)
		}
	}
	if err := s.OverrideTask(ctx, "bob", ids["A"], "again"); !errors.Is(err, ErrConflict) {
		t.Fatalf("double override: %v", err)
	}
	if err := s.OverrideTask(ctx, "bob", ids["B"], " "); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank reason: %v", err)
	}
	if err := s.OverrideTask(ctx, "bob", 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
	// An event inside a rolled-back tx does not persist.
	n := countRows(t, s, "events")
	s.WithTx(ctx, func(tx2 *sql.Tx) error {
		AppendEvent(tx2, "x", "y", "z", "1", nil)
		return errors.New("boom")
	})
	if countRows(t, s, "events") != n {
		t.Fatal("event survived rollback")
	}
}

func TestSchemaHasNoGrepViolations(t *testing.T) {
	// B1 guard: store code never branches on error text.
	for _, f := range []string{"efforts.go", "docs.go", "queues.go", "tasks.go", "attempts.go", "stages.go"} {
		b := mustRead(t, f)
		if strings.Contains(b, "err.Error() ==") || strings.Contains(b, "strings.Contains(err.Error()") || strings.Contains(b, "os.IsNotExist") {
			t.Errorf("%s branches on error text", f)
		}
	}
}

func mustRead(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustHash(t *testing.T, s *Store, effort int64) string {
	t.Helper()
	q, err := s.GetQueue(ctx, effort)
	if err != nil {
		t.Fatal(err)
	}
	return q.Hash
}

func TestGetDocRejectsNegativeRevision(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "p")
	e := newEffort(t, s, "p")
	_, err := s.GetDoc(ctx, e.ID, model.DocPlan, -1)
	wantField(t, err, "revision")
}

func TestUpdateEffortStatusIfIsConditional(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "proj")
	e := newEffort(t, s, "proj")
	ok, err := s.UpdateEffortStatusIf(ctx, "t", e.ID, model.EffortQueueDraft, model.EffortRunning)
	if err != nil || ok {
		t.Fatalf("stale expectation: ok=%v err=%v, want false and nil", ok, err)
	}
	if got, _ := s.GetEffort(ctx, e.ID); got.Status != e.Status {
		t.Fatalf("status changed to %s", got.Status)
	}
	ok, err = s.UpdateEffortStatusIf(ctx, "t", e.ID, e.Status, model.EffortQueueDraft)
	if err != nil || !ok {
		t.Fatalf("matching expectation: ok=%v err=%v", ok, err)
	}
	if got, _ := s.GetEffort(ctx, e.ID); got.Status != model.EffortQueueDraft {
		t.Fatalf("status = %s", got.Status)
	}
}
