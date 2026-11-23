package store

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestConfirmRejectDecisionInbox(t *testing.T) {
	s, _ := openTemp(t)
	defer s.Close()
	ctx := context.Background()

	if err := migrate(s.db, migrations); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().UnixMilli()
	ns := strconv.FormatInt(now, 10)
	insProj := "INSERT INTO projects (slug, repo_path, model_scope, created, updated) VALUES ('p', 'p.git', 'local', " + ns + ", " + ns + ")"
	insEff := "INSERT INTO efforts (id, project, title, status, created, updated) VALUES (1, 'p', 'e', 'running', " + ns + ", " + ns + ")"
	insQ := "INSERT INTO queues (effort, revision, hash, status) VALUES (1, 1, 'h', 'approved')"
	insT := "INSERT INTO tasks (queue, task_key, title, prompt, acceptance_json, files_touched_json) VALUES (1, 'T1', 't', 'p', '[]', '[]')"
	for _, stmt := range []string{insProj, insEff, insQ, insT} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("insert failed: %s: %v", stmt, err)
		}
	}

	if err := s.InsertDecisionInbox(ctx, 1, 1, "R-NG-001", "coord-fs3", "a proposed new ruling"); err != nil {
		t.Fatalf("InsertDecisionInbox: %v", err)
	}
	if err := s.ConfirmDecisionInbox(ctx, 1); err != nil {
		t.Fatalf("ConfirmDecisionInbox: %v", err)
	}
	if _, err := s.ListPendingDecisionInbox(ctx, 1); err != nil {
		t.Fatalf("ListPendingDecisionInbox after confirm: %v", err)
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, status, proposed_text FROM decision_inbox WHERE id = 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("row 1 gone after confirm")
	}
	var id int64
	var status string
	var pt string
	if err := rows.Scan(&id, &status, &pt); err != nil {
		t.Fatal(err)
	}
	if id != 1 || status != "confirmed" || pt != "a proposed new ruling" {
		t.Fatalf("confirmed row: id=%d status=%s text=%s; want id=1 status=confirmed text=a proposed new ruling", id, status, pt)
	}
	rows.Close()

	// Reject path.
	if err := s.InsertDecisionInbox(ctx, 1, 1, "R-NG-002", "coord-fs3", "another proposed ruling"); err != nil {
		t.Fatalf("InsertDecisionInbox (reject path): %v", err)
	}
	if err := s.RejectDecisionInbox(ctx, 2); err != nil {
		t.Fatalf("RejectDecisionInbox: %v", err)
	}
	if _, err := s.ListPendingDecisionInbox(ctx, 1); err != nil {
		t.Fatalf("ListPendingDecisionInbox after reject: %v", err)
	}

	rows2, err := s.db.QueryContext(ctx, "SELECT id, status, proposed_text FROM decision_inbox WHERE id = 2")
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	if !rows2.Next() {
		t.Fatal("row 2 gone after reject")
	}
	var id2 int64
	var st2 string
	var pt2 string
	if err := rows2.Scan(&id2, &st2, &pt2); err != nil {
		t.Fatal(err)
	}
	if id2 != 2 || st2 != "rejected" || pt2 != "another proposed ruling" {
		t.Fatalf("rejected row: id=%d status=%s text=%s; want id=2 status=rejected text=another proposed ruling", id2, st2, pt2)
	}
	rows2.Close()
}
