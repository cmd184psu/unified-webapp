package chat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestGlobalRoomExistsOnInit(t *testing.T) {
	s := NewStore()
	if _, err := s.GetRoom(globalRoomID); err != nil {
		t.Fatalf("global room should exist on init: %v", err)
	}
}

func TestCreateAndDeleteRoom(t *testing.T) {
	s := NewStore()

	room, err := s.CreateRoom("wave-42", "42", "key_mod")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if room.ID == "" {
		t.Fatal("expected a room id")
	}
	if room.ModeratorKeyID != "key_mod" {
		t.Fatalf("moderator key = %q, want key_mod", room.ModeratorKeyID)
	}

	if _, err := s.GetRoom(room.ID); err != nil {
		t.Fatalf("room should be retrievable after creation: %v", err)
	}

	if err := s.DeleteRoom(room.ID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}
	if _, err := s.GetRoom(room.ID); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("room should be gone after deletion, got err=%v", err)
	}
}

func TestGlobalRoomCannotBeDeleted(t *testing.T) {
	s := NewStore()
	if err := s.DeleteRoom(globalRoomID); err == nil {
		t.Fatal("expected error deleting the global room")
	}
	if _, err := s.GetRoom(globalRoomID); err != nil {
		t.Fatalf("global room should still exist: %v", err)
	}
}

func TestDeleteMissingRoom(t *testing.T) {
	s := NewStore()
	if err := s.DeleteRoom("room_does_not_exist"); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestPostMessageCarriesCallsign(t *testing.T) {
	s := NewStore()
	if _, err := s.RegisterCallsign("nimbus-owl", "key_a"); err != nil {
		t.Fatalf("RegisterCallsign: %v", err)
	}

	msg, err := s.PostMessage(globalRoomID, "nimbus-owl", "key_a", "hello world", "")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if msg.Callsign != "nimbus-owl" {
		t.Fatalf("message callsign = %q, want nimbus-owl", msg.Callsign)
	}
	if msg.To != "" {
		t.Fatalf("undirected message should have empty To, got %q", msg.To)
	}

	msgs, err := s.GetMessages(globalRoomID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Callsign != "nimbus-owl" {
		t.Fatalf("expected stored message from nimbus-owl, got %+v", msgs)
	}
}

func TestDirectedMessageToRegisteredCallsign(t *testing.T) {
	s := NewStore()
	if _, err := s.RegisterCallsign("nimbus-owl", "key_a"); err != nil {
		t.Fatalf("RegisterCallsign a: %v", err)
	}
	if _, err := s.RegisterCallsign("copper-fox", "key_b"); err != nil {
		t.Fatalf("RegisterCallsign b: %v", err)
	}

	msg, err := s.PostMessage(globalRoomID, "nimbus-owl", "key_a", "you up?", "copper-fox")
	if err != nil {
		t.Fatalf("directed PostMessage: %v", err)
	}
	if msg.To != "copper-fox" {
		t.Fatalf("message To = %q, want copper-fox", msg.To)
	}
}

func TestDirectedMessageToArbitraryCallsignSucceeds(t *testing.T) {
	s := NewStore()

	msg, err := s.PostMessage(globalRoomID, "nimbus-owl", "key_a", "hi", "ghost")
	if err != nil {
		t.Fatalf("PostMessage to unregistered callsign should succeed: %v", err)
	}
	if msg.To != "ghost" {
		t.Fatalf("message To = %q, want ghost", msg.To)
	}
}

func TestPostMessageToMissingRoomFails(t *testing.T) {
	s := NewStore()
	if _, err := s.PostMessage("room_nope", "nimbus-owl", "key_a", "hi", ""); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestCreateTaskEmitsAssignedCard(t *testing.T) {
	s := NewStore()
	task, card, err := s.CreateTask(Task{
		RoomID:   globalRoomID,
		Title:    "wire the widget",
		Assignee: "pum-2031",
		Creator:  "coord-5daac805",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.ID != "TASK-1" {
		t.Fatalf("task ID = %q, want TASK-1", task.ID)
	}
	if task.State != TaskAssigned {
		t.Fatalf("state = %q, want assigned", task.State)
	}
	if task.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", task.Attempt)
	}
	if card.Kind != "task_assigned" || card.TaskID != task.ID {
		t.Fatalf("card = %+v, want kind task_assigned for %s", card, task.ID)
	}
	if card.To != "pum-2031" {
		t.Fatalf("assigned card To = %q, want pum-2031", card.To)
	}
	msgs, _ := s.GetMessages(globalRoomID)
	if len(msgs) != 1 || msgs[0].Kind != "task_assigned" {
		t.Fatalf("expected assigned card in transcript, got %+v", msgs)
	}
}

func TestCreateTaskMissingRoomFails(t *testing.T) {
	s := NewStore()
	if _, _, err := s.CreateTask(Task{RoomID: "room_nope", Title: "x"}); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestTaskLifecycleTransitions(t *testing.T) {
	s := NewStore()
	task, _, err := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	steps := []struct {
		action string
		want   TaskState
	}{
		{"acknowledge", TaskAcknowledged},
		{"start", TaskWorking},
		{"block", TaskBlocked},
		{"start", TaskWorking},
		{"done", TaskDone},
		{"rework", TaskRework},
		{"start", TaskWorking},
		{"done", TaskDone},
		{"accept", TaskAccepted},
		{"close", TaskClosed},
	}
	for _, st := range steps {
		updated, _, err := s.TransitionTask(task.ID, st.action, "actor", TransitionInput{})
		if err != nil {
			t.Fatalf("transition %q: %v", st.action, err)
		}
		if updated.State != st.want {
			t.Fatalf("after %q state = %q, want %q", st.action, updated.State, st.want)
		}
	}
	final, _ := s.GetTask(task.ID)
	if final.Attempt != 2 {
		t.Fatalf("attempt = %d, want 2 after one rework", final.Attempt)
	}
}

func TestInvalidTransitionRejected(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Creator: "coord"})
	if _, _, err := s.TransitionTask(task.ID, "accept", "actor", TransitionInput{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("accepting a freshly-assigned task should fail, got %v", err)
	}
}

func TestTransitionMissingTaskFails(t *testing.T) {
	s := NewStore()
	if _, _, err := s.TransitionTask("TASK-999", "start", "actor", TransitionInput{}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("want ErrTaskNotFound, got %v", err)
	}
}

func TestBlockRecordsWaitingOnAndNeedsAttention(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	updated, card, err := s.TransitionTask(task.ID, "block", "pum-1", TransitionInput{Note: "need review", WaitingOn: "coord"})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if updated.WaitingOn != "coord" || updated.Blocker != "need review" {
		t.Fatalf("block did not record waitingOn/blocker: %+v", updated)
	}
	if card.Kind != "task_blocked" {
		t.Fatalf("card kind = %q, want task_blocked", card.Kind)
	}
	na := s.ListTasks(TaskFilter{RoomID: globalRoomID, Status: "needs_attention"})
	if len(na) != 1 || na[0].ID != task.ID {
		t.Fatalf("blocked task should appear in needs_attention, got %+v", na)
	}
}

func TestBlockStructuredCardAndCompletedPortions(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	updated, card, err := s.TransitionTask(task.ID, "block", "pum-1", TransitionInput{
		Note:              "need prod creds",
		WaitingOn:         "coord",
		CompletedPortions: "schema written, unit tests pass",
	})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if updated.CompletedPortions != "schema written, unit tests pass" {
		t.Fatalf("completedPortions not recorded: %+v", updated)
	}
	for _, want := range []string{"BLOCKED", "waiting on coord", "blocker: need prod creds", "completed so far: schema written"} {
		if !strings.Contains(card.Content, want) {
			t.Fatalf("blocked card missing %q, got %q", want, card.Content)
		}
	}
	if strings.Count(card.Content, "need prod creds") != 1 {
		t.Fatalf("blocker note should appear exactly once, got %q", card.Content)
	}
}

func TestDoneStructuredCompletionCard(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	in := TransitionInput{
		ChangedFiles:     []string{"a.go", "b.tsx"},
		Tests:            "go test ./... green",
		DeploymentTarget: "webapps.pumamesh.net:5757",
		ServiceHealth:    "200 OK on /healthz",
		EvidenceIDs:      []string{"msg_1", "TASK-9"},
		Limitations:      "dark mode deferred",
	}
	updated, card, err := s.TransitionTask(task.ID, "done", "pum-1", in)
	if err != nil {
		t.Fatalf("done: %v", err)
	}
	if updated.Tests != in.Tests || len(updated.ChangedFiles) != 2 || len(updated.EvidenceIDs) != 2 {
		t.Fatalf("completion fields not recorded on task: %+v", updated)
	}
	for _, want := range []string{
		"DONE — awaiting review",
		"changed files: a.go, b.tsx",
		"tests: go test ./... green",
		"deployment: webapps.pumamesh.net:5757",
		"health: 200 OK on /healthz",
		"evidence: msg_1, TASK-9",
		"limitations: dark mode deferred",
	} {
		if !strings.Contains(card.Content, want) {
			t.Fatalf("done card missing %q, got %q", want, card.Content)
		}
	}
}

func TestExclusiveTaskPerAssignee(t *testing.T) {
	s := NewStore()
	s.ExclusiveTaskPerAssignee = true
	if _, _, err := s.CreateTask(Task{RoomID: globalRoomID, Title: "a", Assignee: "pum-1", Creator: "coord"}); err != nil {
		t.Fatalf("first task: %v", err)
	}
	if _, _, err := s.CreateTask(Task{RoomID: globalRoomID, Title: "b", Assignee: "pum-1", Creator: "coord"}); !errors.Is(err, ErrTaskConflict) {
		t.Fatalf("second active task for same assignee should conflict, got %v", err)
	}
}

func TestListTasksFilters(t *testing.T) {
	s := NewStore()
	_, _, _ = s.CreateTask(Task{RoomID: globalRoomID, Title: "a", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.CreateTask(Task{RoomID: globalRoomID, Title: "b", Assignee: "pum-2", Creator: "coord"})
	got := s.ListTasks(TaskFilter{Assignee: "pum-2"})
	if len(got) != 1 || got[0].Assignee != "pum-2" {
		t.Fatalf("assignee filter failed: %+v", got)
	}
}

func TestDeleteRoomRemovesTasks(t *testing.T) {
	s := NewStore()
	room, _ := s.CreateRoom("wave", "1", "key_mod")
	task, _, _ := s.CreateTask(Task{RoomID: room.ID, Title: "job", Creator: "coord"})
	if err := s.DeleteRoom(room.ID); err != nil {
		t.Fatalf("DeleteRoom: %v", err)
	}
	if _, err := s.GetTask(task.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("task should be gone after room deletion, got %v", err)
	}
}

func TestMessageSeqMonotonic(t *testing.T) {
	s := NewStore()
	m1, _ := s.PostMessage(globalRoomID, "a", "k", "one", "everyone")
	m2, _ := s.PostMessage(globalRoomID, "a", "k", "two", "everyone")
	if m1.Seq == 0 || m2.Seq <= m1.Seq {
		t.Fatalf("seq should be monotonic and non-zero: m1=%d m2=%d", m1.Seq, m2.Seq)
	}
}

func TestMessagesAfter(t *testing.T) {
	s := NewStore()
	_, _ = s.PostMessage(globalRoomID, "a", "k", "one", "everyone")
	m2, _ := s.PostMessage(globalRoomID, "a", "k", "two", "everyone")
	m3, _ := s.PostMessage(globalRoomID, "a", "k", "three", "everyone")

	got, err := s.MessagesAfter(globalRoomID, m2.Seq)
	if err != nil {
		t.Fatalf("MessagesAfter: %v", err)
	}
	if len(got) != 1 || got[0].ID != m3.ID {
		t.Fatalf("after m2 should return only m3, got %+v", got)
	}

	all, _ := s.MessagesAfter(globalRoomID, 0)
	if len(all) != 3 {
		t.Fatalf("after 0 should return all 3, got %d", len(all))
	}

	if _, err := s.MessagesAfter("room_missing", 0); !errors.Is(err, ErrRoomNotFound) {
		t.Fatalf("want ErrRoomNotFound, got %v", err)
	}
}

func TestInboxScopesToCallsignAndCursor(t *testing.T) {
	s := NewStore()
	_, _ = s.PostMessage(globalRoomID, "sender", "k", "for everyone", "everyone")
	d1, _ := s.PostMessage(globalRoomID, "sender", "k", "hi pum", "pum-1")
	_, _ = s.PostMessage(globalRoomID, "sender", "k", "not you", "pum-2")
	d2, _ := s.PostMessage(globalRoomID, "sender", "k", "again pum", "PUM-1")

	msgs, cursor := s.Inbox("pum-1", 0, 0)
	if len(msgs) != 2 {
		t.Fatalf("want 2 inbox messages (case-insensitive), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].ID != d1.ID || msgs[1].ID != d2.ID {
		t.Fatalf("inbox order wrong: %+v", msgs)
	}
	if cursor != d2.Seq {
		t.Fatalf("cursor = %d, want %d", cursor, d2.Seq)
	}

	rest, cursor2 := s.Inbox("pum-1", cursor, 0)
	if len(rest) != 0 {
		t.Fatalf("no new messages expected after cursor, got %d", len(rest))
	}
	if cursor2 != cursor {
		t.Fatalf("empty poll should preserve cursor: got %d want %d", cursor2, cursor)
	}
}

func TestInboxAcrossRoomsIncludesTaskCards(t *testing.T) {
	s := NewStore()
	room, _ := s.CreateRoom("wave", "1", "key_mod")
	_, _, err := s.CreateTask(Task{RoomID: room.ID, Title: "job", Assignee: "pum-9", Creator: "coord"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	msgs, _ := s.Inbox("pum-9", 0, 0)
	if len(msgs) != 1 || msgs[0].Kind != "task_assigned" {
		t.Fatalf("assignee inbox should include the assigned card, got %+v", msgs)
	}
}

func TestInboxLimit(t *testing.T) {
	s := NewStore()
	for i := 0; i < 5; i++ {
		_, _ = s.PostMessage(globalRoomID, "sender", "k", "msg", "pum-1")
	}
	msgs, cursor := s.Inbox("pum-1", 0, 2)
	if len(msgs) != 2 {
		t.Fatalf("limit=2 should cap at 2, got %d", len(msgs))
	}
	if cursor != msgs[1].Seq {
		t.Fatalf("cursor should be last returned seq")
	}
}

func TestAckInboxAdvancesMax(t *testing.T) {
	s := NewStore()
	// No ack yet → cursor is 0.
	if got := s.GetAckCursor("pum-1", "agent_x"); got != 0 {
		t.Fatalf("fresh cursor should be 0, got %d", got)
	}
	// Ack 5 → cursor 5.
	if got := s.AckInbox("pum-1", "agent_x", 5); got != 5 {
		t.Fatalf("ack 5 should return 5, got %d", got)
	}
	// Ack 3 (stale/lower) → cursor must NOT regress, stays 5.
	if got := s.AckInbox("pum-1", "agent_x", 3); got != 5 {
		t.Fatalf("ack 3 must not regress cursor below 5, got %d", got)
	}
	// Ack 8 (higher) → advances to 8.
	if got := s.AckInbox("pum-1", "agent_x", 8); got != 8 {
		t.Fatalf("ack 8 should advance to 8, got %d", got)
	}
	if got := s.GetAckCursor("pum-1", "agent_x"); got != 8 {
		t.Fatalf("persisted cursor should be 8, got %d", got)
	}
}

func TestResumeIsReadOnly(t *testing.T) {
	s := NewStore()
	_, _ = s.PostMessage(globalRoomID, "sender", "k", "hi pum", "pum-1")
	_, _ = s.PostMessage(globalRoomID, "sender", "k", "again", "pum-1")

	// Resume with no prior ack → should return both messages.
	snap := s.Resume("pum-1", "agent_x", 0)
	if len(snap.Inbox) != 2 {
		t.Fatalf("want 2 inbox messages on first resume, got %d", len(snap.Inbox))
	}
	if snap.AckSeq != 0 {
		t.Fatalf("ackSeq should be 0 before any ack, got %d", snap.AckSeq)
	}
	if snap.Cursor != snap.Inbox[len(snap.Inbox)-1].Seq {
		t.Fatalf("cursor should be last inbox seq, got %d want %d", snap.Cursor, snap.Inbox[len(snap.Inbox)-1].Seq)
	}

	// Crucially: resume must NOT have advanced the ack cursor.
	if got := s.GetAckCursor("pum-1", "agent_x"); got != 0 {
		t.Fatalf("GET /resume must not advance the cursor; got %d, want 0", got)
	}

	// A second resume still returns both (no ack happened in between).
	snap2 := s.Resume("pum-1", "agent_x", 0)
	if len(snap2.Inbox) != 2 {
		t.Fatalf("second resume without ack should still return 2, got %d", len(snap2.Inbox))
	}

	// Now ACK the cursor; the next resume should return only newer messages.
	s.AckInbox("pum-1", "agent_x", snap.Cursor)
	s.PostMessage(globalRoomID, "sender", "k", "third", "pum-1")
	snap3 := s.Resume("pum-1", "agent_x", 0)
	if len(snap3.Inbox) != 1 {
		t.Fatalf("after ack, resume should return only 1 new message, got %d", len(snap3.Inbox))
	}
	if snap3.AckSeq != snap.Cursor {
		t.Fatalf("ackSeq should reflect the ack, got %d want %d", snap3.AckSeq, snap.Cursor)
	}
}

func TestResumeScopedByCompositeKey(t *testing.T) {
	s := NewStore()
	_, _ = s.PostMessage(globalRoomID, "sender", "k", "hi brain", "BrainStorm")

	// Same callsign + different instanceId = separate cursors (different agent
	// instances, even under the same callsign).
	s.AckInbox("BrainStorm", "instance_A", 5)
	if got := s.GetAckCursor("BrainStorm", "instance_B"); got != 0 {
		t.Fatalf("different instanceId should have a fresh cursor, got %d", got)
	}
	// Same callsign (normalized, case-insensitive) + same instanceId shares.
	if got := s.GetAckCursor("brainstorm", "instance_A"); got != 5 {
		t.Fatalf("normalized callsign should share cursor; got %d want 5", got)
	}
	// Different callsign = separate cursor.
	if got := s.GetAckCursor("HarnessBuilder", "instance_A"); got != 0 {
		t.Fatalf("different callsign should have a fresh cursor, got %d", got)
	}

	// Resume is scoped the same way: BrainStorm/instance_A has acked past the
	// message, so its resume inbox is empty; BrainStorm/instance_B has not.
	snapA := s.Resume("BrainStorm", "instance_A", 0)
	if len(snapA.Inbox) != 0 {
		t.Fatalf("acked instance should see empty inbox, got %d", len(snapA.Inbox))
	}
	snapB := s.Resume("BrainStorm", "instance_B", 0)
	if len(snapB.Inbox) != 1 {
		t.Fatalf("unacked instance should see 1 message, got %d", len(snapB.Inbox))
	}
}

func TestResumeConsolidatesTasks(t *testing.T) {
	s := NewStore()
	room, _ := s.CreateRoom("wave", "1", "key_mod")
	// pum-1 is assigned an active task (currentTask).
	t1, _, err := s.CreateTask(Task{RoomID: room.ID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// A task pum-1 created that is now done → awaiting pum-1's review.
	t2, _, err := s.CreateTask(Task{RoomID: room.ID, Title: "review me", Assignee: "other", Creator: "pum-1"})
	if err != nil {
		t.Fatalf("CreateTask t2: %v", err)
	}
	if _, _, err := s.TransitionTask(t2.ID, "start", "other", TransitionInput{}); err != nil {
		t.Fatalf("transition t2 to start: %v", err)
	}
	if _, _, err := s.TransitionTask(t2.ID, "done", "other", TransitionInput{}); err != nil {
		t.Fatalf("transition t2 to done: %v", err)
	}
	// A task blocked on pum-1 (waitingOn=pum-1).
	t3, _, err := s.CreateTask(Task{RoomID: room.ID, Title: "blocked", Assignee: "other2", Creator: "coord"})
	if err != nil {
		t.Fatalf("CreateTask t3: %v", err)
	}
	if _, _, err := s.TransitionTask(t3.ID, "start", "other2", TransitionInput{}); err != nil {
		t.Fatalf("transition t3 to start: %v", err)
	}
	if _, _, err := s.TransitionTask(t3.ID, "block", "other2", TransitionInput{Note: "need pum-1 input", WaitingOn: "pum-1"}); err != nil {
		t.Fatalf("transition t3 to blocked: %v", err)
	}
	// A directed message to pum-1 in the inbox.
	_, _ = s.PostMessage(room.ID, "coord", "k", "hey pum-1", "pum-1")

	snap := s.Resume("pum-1", "agent_x", 0)

	// Inbox should contain the directed message + the task_assigned card for t1.
	if len(snap.Inbox) < 1 {
		t.Fatalf("resume inbox should have directed messages, got %d", len(snap.Inbox))
	}
	foundMsg := false
	for _, m := range snap.Inbox {
		if m.To == "pum-1" && m.Content == "hey pum-1" {
			foundMsg = true
		}
	}
	if !foundMsg {
		t.Fatalf("resume inbox missing the directed message to pum-1: %+v", snap.Inbox)
	}

	// Tasks: t1 is the active assigned task.
	foundT1 := false
	for _, tk := range snap.Tasks {
		if tk.ID == t1.ID {
			foundT1 = true
		}
	}
	if !foundT1 {
		t.Fatalf("resume Tasks missing active task %s: %+v", t1.ID, snap.Tasks)
	}

	// CurrentTask should be t1.
	if snap.CurrentTask == nil || snap.CurrentTask.ID != t1.ID {
		t.Fatalf("currentTask should be %s, got %+v", t1.ID, snap.CurrentTask)
	}

	// AwaitingReview: t2 is done + pum-1 is creator.
	foundT2 := false
	for _, tk := range snap.AwaitingReview {
		if tk.ID == t2.ID {
			foundT2 = true
		}
	}
	if !foundT2 {
		t.Fatalf("awaitingReview missing %s: %+v", t2.ID, snap.AwaitingReview)
	}

	// BlockedOn: t3 is blocked on pum-1.
	foundT3 := false
	for _, tk := range snap.BlockedOn {
		if tk.ID == t3.ID {
			foundT3 = true
		}
	}
	if !foundT3 {
		t.Fatalf("blockedOn missing %s: %+v", t3.ID, snap.BlockedOn)
	}

	// NeedsAttention should include the blocked task (t3) and the done-awaiting one (t2).
	if len(snap.NeedsAttention) == 0 {
		t.Fatalf("needsAttention should not be empty: %+v", snap.NeedsAttention)
	}
}

func TestCallsignRegistrationIsUnenforced(t *testing.T) {
	s := NewStore()
	if _, err := s.RegisterCallsign("shared", "key_a"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// A different key claiming the same callsign is allowed — callsigns are
	// unenforced identity, not reservations.
	if _, err := s.RegisterCallsign("shared", "key_b"); err != nil {
		t.Fatalf("second key registering same callsign should succeed: %v", err)
	}
	if name, ok := s.GetCallsign("key_a", "shared"); !ok || name != "shared" {
		t.Fatalf("key_a callsign = %q (ok=%v), want shared", name, ok)
	}
	if name, ok := s.GetCallsign("key_b", "shared"); !ok || name != "shared" {
		t.Fatalf("key_b callsign = %q (ok=%v), want shared", name, ok)
	}
}

func TestRegisterCallsignAssignsStableInstanceID(t *testing.T) {
	s := NewStore()
	c1, _ := s.RegisterCallsign("nimbus-owl", "key_a")
	if c1.InstanceID == "" {
		t.Fatalf("expected a non-empty instance id")
	}
	if c1.CreatedAt.IsZero() || c1.LastActivityAt.IsZero() {
		t.Fatalf("expected createdAt/lastActivityAt set: %+v", c1)
	}
	// Re-registering the SAME name with the same key keeps the immutable
	// instance id and original createdAt — this is the replacement-agent
	// handoff path (a new agent on the same key + same callsign inherits
	// the prior instance).
	c2, _ := s.RegisterCallsign("nimbus-owl", "key_a")
	if c2.InstanceID != c1.InstanceID {
		t.Fatalf("instance id should be stable per (key, name): %q != %q", c2.InstanceID, c1.InstanceID)
	}
	if !c2.CreatedAt.Equal(c1.CreatedAt) {
		t.Fatalf("createdAt should be preserved across re-register of the same name")
	}
	// A different callsign on the SAME key gets a distinct instance id — it
	// is a different agent sharing the key, not a replacement.
	c3, _ := s.RegisterCallsign("nimbus-hawk", "key_a")
	if c3.InstanceID == c1.InstanceID {
		t.Fatalf("different callsigns on the same key must have distinct instance ids")
	}
	// A different key gets a distinct instance id.
	c4, _ := s.RegisterCallsign("nimbus-owl", "key_b")
	if c4.InstanceID == c1.InstanceID {
		t.Fatalf("different keys must have distinct instance ids")
	}
}

func TestSuggestIfTaken(t *testing.T) {
	s := NewStore()
	_, _ = s.RegisterCallsign("scout", "key_a")
	// Same key re-registering its own name is not "taken".
	if _, taken := s.SuggestIfTaken("scout", "key_a"); taken {
		t.Fatalf("own callsign should not be reported taken")
	}
	sug, taken := s.SuggestIfTaken("scout", "key_b")
	if !taken || sug != "scout-2" {
		t.Fatalf("expected taken with suggestion scout-2, got %q taken=%v", sug, taken)
	}
	_, _ = s.RegisterCallsign("scout-2", "key_c")
	sug, taken = s.SuggestIfTaken("scout", "key_b")
	if !taken || sug != "scout-3" {
		t.Fatalf("expected scout-3 when scout-2 also taken, got %q", sug)
	}
}

func TestReleaseCallsign(t *testing.T) {
	s := NewStore()
	c, _ := s.RegisterCallsign("ghost", "key_a")
	if _, ok := s.ReleaseCallsignByInstance("nope"); ok {
		t.Fatalf("releasing unknown instance should report not found")
	}
	rc, ok := s.ReleaseCallsignByInstance(c.InstanceID)
	if !ok || rc.Name != "ghost" {
		t.Fatalf("release by instance failed: %+v ok=%v", rc, ok)
	}
	if _, ok := s.GetCallsign("key_a", "ghost"); ok {
		t.Fatalf("callsign should be gone after release")
	}
	// Release by name across multiple keys.
	_, _ = s.RegisterCallsign("dup", "key_x")
	_, _ = s.RegisterCallsign("dup", "key_y")
	if n := s.ReleaseCallsignByName("DUP"); n != 2 {
		t.Fatalf("release by name should remove both leases, got %d", n)
	}
}

func TestListCallsignInfoStaleAndCurrentTask(t *testing.T) {
	s := NewStore()
	s.CallsignLeaseTTL = time.Hour
	room, _ := s.CreateRoom("wave", "1", "key_mod")
	_, _ = s.RegisterCallsign("worker", "key_w")
	if _, _, err := s.CreateTask(Task{RoomID: room.ID, Title: "job", Assignee: "worker", Creator: "coord"}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	// Force staleness by backdating last activity.
	c := s.callsigns[leaseKey("key_w", "worker")]
	c.LastActivityAt = time.Now().Add(-2 * time.Hour)
	s.callsigns[leaseKey("key_w", "worker")] = c

	infos := s.ListCallsignInfo()
	var found bool
	for _, in := range infos {
		if in.Callsign == "worker" {
			found = true
			if !in.Stale {
				t.Fatalf("worker should be stale")
			}
			if in.CurrentTask == "" {
				t.Fatalf("worker should have a current task")
			}
		}
	}
	if !found {
		t.Fatalf("worker not in ListCallsignInfo")
	}
}

func TestPostMessageBumpsLastActivity(t *testing.T) {
	s := NewStore()
	c, _ := s.RegisterCallsign("pulse", "key_p")
	before := c.LastActivityAt
	time.Sleep(2 * time.Millisecond)
	if _, err := s.PostMessage(globalRoomID, "pulse", "key_p", "tick", "everyone"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if !s.callsigns[leaseKey("key_p", "pulse")].LastActivityAt.After(before) {
		t.Fatalf("posting should bump last activity")
	}
}

// TestPostMessageAttributesToFromCallsignNotKey is the regression test for the
// presence-attribution bug: a shared key registered to "HarnessBuilder" must
// not have its lease refreshed when another agent posts as "BrainStorm" using
// the same key. Presence follows the explicit "from" callsign, not the key.
func TestPostMessageAttributesToFromCallsignNotKey(t *testing.T) {
	s := NewStore()
	hb, _ := s.RegisterCallsign("HarnessBuilder", "key_shared")
	bs, _ := s.RegisterCallsign("BrainStorm", "key_shared")
	hbBefore := hb.LastActivityAt
	bsBefore := bs.LastActivityAt
	time.Sleep(2 * time.Millisecond)
	// Post as BrainStorm on the key that is ALSO registered to HarnessBuilder.
	if _, err := s.PostMessage(globalRoomID, "BrainStorm", "key_shared", "yo", "everyone"); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	// BrainStorm's lease must advance.
	if !s.callsigns[leaseKey("key_shared", "BrainStorm")].LastActivityAt.After(bsBefore) {
		t.Fatalf("posting as BrainStorm should bump BrainStorm's lease")
	}
	// HarnessBuilder's lease must NOT advance — that was the bug.
	if s.callsigns[leaseKey("key_shared", "HarnessBuilder")].LastActivityAt.After(hbBefore) {
		t.Fatalf("posting as BrainStorm must not bump HarnessBuilder's lease (presence attribution bug)")
	}
}

// TestTouchCallsignAttributesToExplicitCallsign verifies the heartbeat path
// touches only the (key, callsign) lease named in the call, not the key's
// other leases.
func TestTouchCallsignAttributesToExplicitCallsign(t *testing.T) {
	s := NewStore()
	hb, _ := s.RegisterCallsign("HarnessBuilder", "key_shared")
	bs, _ := s.RegisterCallsign("BrainStorm", "key_shared")
	hbBefore := hb.LastActivityAt
	bsBefore := bs.LastActivityAt
	time.Sleep(2 * time.Millisecond)
	s.TouchCallsign("key_shared", "BrainStorm")
	if !s.callsigns[leaseKey("key_shared", "BrainStorm")].LastActivityAt.After(bsBefore) {
		t.Fatalf("TouchCallsign should bump BrainStorm's lease")
	}
	if s.callsigns[leaseKey("key_shared", "HarnessBuilder")].LastActivityAt.After(hbBefore) {
		t.Fatalf("TouchCallsign(BrainStorm) must not bump HarnessBuilder's lease")
	}
}

func TestDerivePresence(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		last  time.Time
		stale bool
		cur   TaskState
		want  string
	}{
		{"never active", time.Time{}, false, "", PresenceDisconnected},
		{"stale wins", now, true, TaskWorking, PresenceStale},
		{"working", now, false, TaskWorking, PresenceWorking},
		{"blocked waits", now, false, TaskBlocked, PresenceWaiting},
		{"done waits", now, false, TaskDone, PresenceWaiting},
		{"idle online", now, false, TaskAssigned, PresenceOnline},
		{"no task online", now, false, "", PresenceOnline},
	}
	for _, tc := range cases {
		if got := derivePresence(tc.last, tc.stale, tc.cur); got != tc.want {
			t.Errorf("%s: derivePresence = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestListCallsignInfoPresenceReflectsTask(t *testing.T) {
	s := NewStore()
	if _, err := s.RegisterCallsign("worker", "key_w"); err != nil {
		t.Fatalf("register: %v", err)
	}
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "worker", Creator: "coord"})
	if _, _, err := s.TransitionTask(task.ID, "start", "worker", TransitionInput{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	find := func() CallsignInfo {
		for _, info := range s.ListCallsignInfo() {
			if info.Callsign == "worker" {
				return info
			}
		}
		t.Fatalf("worker not found")
		return CallsignInfo{}
	}
	if got := find(); got.Presence != PresenceWorking || got.CurrentTask != task.ID {
		t.Fatalf("expected working on %s, got %+v", task.ID, got)
	}
	if _, _, err := s.TransitionTask(task.ID, "block", "worker", TransitionInput{Note: "n", WaitingOn: "coord"}); err != nil {
		t.Fatalf("block: %v", err)
	}
	if got := find(); got.Presence != PresenceWaiting || got.WaitingOn != "coord" {
		t.Fatalf("expected waiting on coord, got %+v", got)
	}
}

func TestCallsignInfoForKey(t *testing.T) {
	s := NewStore()
	if _, ok := s.CallsignInfoForKey("missing", "x"); ok {
		t.Fatalf("expected no info for unknown key")
	}
	if _, err := s.RegisterCallsign("beacon", "key_b"); err != nil {
		t.Fatalf("register: %v", err)
	}
	info, ok := s.CallsignInfoForKey("key_b", "beacon")
	if !ok || info.Callsign != "beacon" || info.KeyID != "key_b" {
		t.Fatalf("CallsignInfoForKey = %+v, %v", info, ok)
	}
}

func TestAckOverdue(t *testing.T) {
	s := NewStore()
	s.AckThreshold = 10 * time.Millisecond
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "worker", Creator: "coord"})
	if got, _ := s.GetTask(task.ID); got.AckOverdue {
		t.Fatalf("fresh assigned task should not be ack-overdue")
	}
	time.Sleep(15 * time.Millisecond)
	if got, _ := s.GetTask(task.ID); !got.AckOverdue {
		t.Fatalf("assigned task past threshold should be ack-overdue")
	}
	if _, _, err := s.TransitionTask(task.ID, "start", "worker", TransitionInput{}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got, _ := s.GetTask(task.ID); got.AckOverdue {
		t.Fatalf("started task should not be ack-overdue")
	}
}

func TestDashboardColumnMapping(t *testing.T) {
	cases := map[TaskState]string{
		TaskWorking:      ColumnActive,
		TaskAssigned:     ColumnWaitingForAgent,
		TaskAcknowledged: ColumnWaitingForAgent,
		TaskRework:       ColumnWaitingForAgent,
		TaskBlocked:      ColumnBlocked,
		TaskDone:         ColumnDoneAwaitingReview,
		TaskAccepted:     ColumnWaitingForCoord,
		TaskClosed:       "",
		TaskCancelled:    "",
	}
	for state, want := range cases {
		if got := state.DashboardColumn(); got != want {
			t.Fatalf("DashboardColumn(%q)=%q, want %q", state, got, want)
		}
	}
}

func TestCreateTaskRecordsDependencies(t *testing.T) {
	s := NewStore()
	dep, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "dep", Assignee: "pum-1", Creator: "coord"})
	task, card, err := s.CreateTask(Task{RoomID: globalRoomID, Title: "main", Assignee: "pum-2", Creator: "coord", DependsOn: []string{dep.ID}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(task.DependsOn) != 1 || task.DependsOn[0] != dep.ID {
		t.Fatalf("dependsOn not recorded: %+v", task.DependsOn)
	}
	if task.Version != 1 {
		t.Fatalf("new task version should be 1, got %d", task.Version)
	}
	if !strings.Contains(card.Content, "depends on: "+dep.ID) {
		t.Fatalf("assigned card missing depends-on line: %q", card.Content)
	}
}

func TestDepsPendingResolvesWhenAccepted(t *testing.T) {
	s := NewStore()
	dep, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "dep", Assignee: "pum-1", Creator: "coord"})
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "main", Assignee: "pum-2", Creator: "coord", DependsOn: []string{dep.ID}})
	if got, _ := s.GetTask(task.ID); len(got.DepsPending) != 1 || got.DepsPending[0] != dep.ID {
		t.Fatalf("dep should be pending, got %+v", got.DepsPending)
	}
	for _, a := range []string{"start", "done", "accept"} {
		if _, _, err := s.TransitionTask(dep.ID, a, "pum-1", TransitionInput{}); err != nil {
			t.Fatalf("%s dep: %v", a, err)
		}
	}
	if got, _ := s.GetTask(task.ID); len(got.DepsPending) != 0 {
		t.Fatalf("dep should be resolved once accepted, got %+v", got.DepsPending)
	}
}

func TestVersionConflict(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	if _, _, err := s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{ExpectedVersion: 99}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale expectedVersion should conflict, got %v", err)
	}
	updated, _, err := s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{ExpectedVersion: task.Version})
	if err != nil {
		t.Fatalf("matching expectedVersion should succeed: %v", err)
	}
	if updated.Version != task.Version+1 {
		t.Fatalf("version should bump on transition: %d -> %d", task.Version, updated.Version)
	}
}

func TestHandoffReassigns(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	updated, card, err := s.TransitionTask(task.ID, "handoff", "coord", TransitionInput{NewAssignee: "pum-9"})
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if updated.Assignee != "pum-9" {
		t.Fatalf("assignee should be pum-9, got %q", updated.Assignee)
	}
	if updated.State != TaskWorking {
		t.Fatalf("handoff should not change lifecycle state, got %q", updated.State)
	}
	if card.To != "pum-9" {
		t.Fatalf("handoff card should be addressed to new assignee, got %q", card.To)
	}
	if !strings.Contains(card.Content, "handed off from pum-1 to pum-9") {
		t.Fatalf("handoff card content unexpected: %q", card.Content)
	}
}

func TestCloseBuildsSummary(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "widget", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	_, _, _ = s.TransitionTask(task.ID, "done", "pum-1", TransitionInput{
		ChangedFiles: []string{"a.go"},
		Tests:        "green",
		Limitations:  "toggle deferred",
	})
	_, _, _ = s.TransitionTask(task.ID, "accept", "coord", TransitionInput{})
	updated, card, err := s.TransitionTask(task.ID, "close", "coord", TransitionInput{})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	for _, want := range []string{"summary: widget", "assignee: pum-1", "changed files: a.go", "follow-ups: toggle deferred"} {
		if !strings.Contains(updated.Summary, want) {
			t.Fatalf("summary missing %q, got %q", want, updated.Summary)
		}
	}
	if !strings.Contains(card.Content, updated.Summary) {
		t.Fatalf("closed card should fold in the summary, got %q", card.Content)
	}
}

// helper: create + start + block a task, returning the blocked task.
func blockedTask(t *testing.T, s *Store, assignee, waitingOn string) Task {
	t.Helper()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: assignee, Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", assignee, TransitionInput{})
	blocked, _, err := s.TransitionTask(task.ID, "block", assignee, TransitionInput{Note: "need creds", WaitingOn: waitingOn})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	return blocked
}

func TestAnswerRequiresBlockedAndWaitingOnMatch(t *testing.T) {
	s := NewStore()
	// answer on a non-blocked task → ErrInvalidTransition
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	if _, _, err := s.TransitionTask(task.ID, "answer", "coord", TransitionInput{Note: "hi"}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("answer on working task should fail with ErrInvalidTransition, got %v", err)
	}
	// answer by the wrong callsign → ErrWaitingOnMismatch
	blocked := blockedTask(t, s, "pum-1", "coord")
	if _, _, err := s.TransitionTask(blocked.ID, "answer", "someone-else", TransitionInput{Note: "hi"}); !errors.Is(err, ErrWaitingOnMismatch) {
		t.Fatalf("answer by non-waitingOn should fail with ErrWaitingOnMismatch, got %v", err)
	}
	// answer with empty note → ErrAnswerEmpty
	if _, _, err := s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "   "}); !errors.Is(err, ErrAnswerEmpty) {
		t.Fatalf("answer with empty note should fail with ErrAnswerEmpty, got %v", err)
	}
}

func TestAnswerRecordsFieldsAndEmitsCardToAssignee(t *testing.T) {
	s := NewStore()
	blocked := blockedTask(t, s, "pum-1", "coord")
	versionBefore := blocked.Version
	updated, card, err := s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "creds are in ~/.pumamesh/db.env"})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if updated.Answer != "creds are in ~/.pumamesh/db.env" {
		t.Fatalf("answer not recorded: %q", updated.Answer)
	}
	if updated.AnsweredBy != "coord" {
		t.Fatalf("answeredBy not recorded: %q", updated.AnsweredBy)
	}
	if updated.AnsweredAt.IsZero() {
		t.Fatalf("answeredAt should be set")
	}
	if updated.AnswerSeq != card.Seq {
		t.Fatalf("answerSeq should match card seq: %d vs %d", updated.AnswerSeq, card.Seq)
	}
	// answer does NOT change state or bump version.
	if updated.State != TaskBlocked {
		t.Fatalf("state should stay blocked, got %q", updated.State)
	}
	if updated.Version != versionBefore {
		t.Fatalf("version should NOT bump on answer: was %d now %d", versionBefore, updated.Version)
	}
	// card is addressed to the assignee, kind=task_answer.
	if card.Kind != "task_answer" {
		t.Fatalf("card kind = %q, want task_answer", card.Kind)
	}
	if card.To != "pum-1" {
		t.Fatalf("card should be addressed to assignee pum-1, got %q", card.To)
	}
	for _, want := range []string{"ANSWER to blocker", "answer: creds are in", "from: coord", "next: @pum-1"} {
		if !strings.Contains(card.Content, want) {
			t.Fatalf("answer card missing %q, got %q", want, card.Content)
		}
	}
}

func TestAnswerIdempotentRetryAndCorrection(t *testing.T) {
	s := NewStore()
	blocked := blockedTask(t, s, "pum-1", "coord")
	first, _, err := s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "use env var X"})
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}
	// retry with the same note → no-op (zero card, no new seq).
	retry, retryCard, err := s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "use env var X"})
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if retryCard.ID != "" {
		t.Fatalf("idempotent retry should return a zero-value card, got %q", retryCard.ID)
	}
	if retry.AnswerSeq != first.AnswerSeq {
		t.Fatalf("answerSeq should not change on idempotent retry: %d vs %d", retry.AnswerSeq, first.AnswerSeq)
	}
	// correction with a different note → new card, new seq, overwritten answer.
	correction, corrCard, err := s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "actually use env var Y"})
	if err != nil {
		t.Fatalf("correction: %v", err)
	}
	if corrCard.ID == "" {
		t.Fatalf("correction should emit a new card")
	}
	if correction.Answer != "actually use env var Y" {
		t.Fatalf("correction should overwrite answer: %q", correction.Answer)
	}
	if correction.AnswerSeq != corrCard.Seq || correction.AnswerSeq == first.AnswerSeq {
		t.Fatalf("correction should have a new answerSeq: %d vs first %d", correction.AnswerSeq, first.AnswerSeq)
	}
}

func TestAnswerExpectedVersionCAS(t *testing.T) {
	s := NewStore()
	blocked := blockedTask(t, s, "pum-1", "coord")
	// CAS with the correct version → succeeds.
	if _, _, err := s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "ok", ExpectedVersion: blocked.Version}); err != nil {
		t.Fatalf("answer with correct ExpectedVersion should succeed: %v", err)
	}
	// CAS with a stale version → ErrVersionConflict (version did not bump, so
	// the stale version is now wrong — it's less than current... actually
	// answer does not bump the version, so the same version still matches.
	// The CAS guards against concurrent STATE changes, not answer retries.)
	// Test with a wrong version to confirm CAS still fires.
	if _, _, err := s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "retry", ExpectedVersion: blocked.Version + 99}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("answer with wrong ExpectedVersion should fail with ErrVersionConflict, got %v", err)
	}
}

func TestAnswerPreservedInSummary(t *testing.T) {
	s := NewStore()
	blocked := blockedTask(t, s, "pum-1", "coord")
	_, _, _ = s.TransitionTask(blocked.ID, "answer", "coord", TransitionInput{Note: "creds are in db.env"})
	// assignee resumes (start does NOT clear blocker/answer), then done → accept → close.
	if _, _, err := s.TransitionTask(blocked.ID, "start", "pum-1", TransitionInput{}); err != nil {
		t.Fatalf("start after answer: %v", err)
	}
	resumed, _ := s.GetTask(blocked.ID)
	if resumed.Blocker != "need creds" || resumed.Answer != "creds are in db.env" {
		t.Fatalf("start should NOT clear blocker/answer: blocker=%q answer=%q", resumed.Blocker, resumed.Answer)
	}
	_, _, _ = s.TransitionTask(blocked.ID, "done", "pum-1", TransitionInput{ChangedFiles: []string{"a.go"}})
	_, _, _ = s.TransitionTask(blocked.ID, "accept", "coord", TransitionInput{})
	closed, _, err := s.TransitionTask(blocked.ID, "close", "coord", TransitionInput{})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	for _, want := range []string{"blocker: need creds", "answer: creds are in db.env", "by coord"} {
		if !strings.Contains(closed.Summary, want) {
			t.Fatalf("summary should preserve Q&A, missing %q in %q", want, closed.Summary)
		}
	}
	// An unresolved blocker (no answer) should still be a follow-up in the
	// summary. Go through the full lifecycle (done→accept→close) to get a
	// summary built — cancel does not build one.
	s2 := NewStore()
	t2, _, _ := s2.CreateTask(Task{RoomID: globalRoomID, Title: "stuck", Assignee: "pum-2", Creator: "coord"})
	_, _, _ = s2.TransitionTask(t2.ID, "start", "pum-2", TransitionInput{})
	_, _, _ = s2.TransitionTask(t2.ID, "block", "pum-2", TransitionInput{Note: "no answer coming", WaitingOn: "coord"})
	// The blocker is still unanswered, so resume via start (doesn't clear
	// blocker/answer) then done→accept→close to build the summary.
	_, _, _ = s2.TransitionTask(t2.ID, "start", "pum-2", TransitionInput{})
	_, _, _ = s2.TransitionTask(t2.ID, "done", "pum-2", TransitionInput{Limitations: "toggle deferred"})
	_, _, _ = s2.TransitionTask(t2.ID, "accept", "coord", TransitionInput{})
	closed2, _, err := s2.TransitionTask(t2.ID, "close", "coord", TransitionInput{})
	if err != nil {
		t.Fatalf("close unresolved: %v", err)
	}
	if !strings.Contains(closed2.Summary, "unresolved blocker: no answer coming") {
		t.Fatalf("unresolved blocker should be a follow-up in summary, got %q", closed2.Summary)
	}
	if strings.Contains(closed2.Summary, "answer:") {
		t.Fatalf("unresolved blocker should NOT have an answer line in summary, got %q", closed2.Summary)
	}
}

func TestResumeRecommendedNextAction(t *testing.T) {
	s := NewStore()

	// 1. No current task, no eligible plan task → idle, not actionable.
	snap := s.Resume("pum-1", "agent-1", 0)
	if snap.Actionable {
		t.Fatalf("empty store should not be actionable")
	}
	if snap.RecommendedNextAction.Action != "idle" {
		t.Fatalf("expected idle, got %q", snap.RecommendedNextAction.Action)
	}

	// 2. Assigned task → acknowledge.
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "job", Assignee: "pum-1", Creator: "coord"})
	snap = s.Resume("pum-1", "agent-1", 0)
	if !snap.Actionable || snap.RecommendedNextAction.Action != "acknowledge" {
		t.Fatalf("assigned task should recommend acknowledge, got actionable=%v action=%q", snap.Actionable, snap.RecommendedNextAction.Action)
	}
	if snap.RecommendedNextAction.TaskID != task.ID {
		t.Fatalf("expected taskId=%s, got %q", task.ID, snap.RecommendedNextAction.TaskID)
	}

	// 3. Working → resume.
	s.TransitionTask(task.ID, "acknowledge", "pum-1", TransitionInput{})
	s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	snap = s.Resume("pum-1", "agent-1", 0)
	if !snap.Actionable || snap.RecommendedNextAction.Action != "resume" {
		t.Fatalf("working task should recommend resume, got actionable=%v action=%q", snap.Actionable, snap.RecommendedNextAction.Action)
	}

	// 4. Blocked without answer → not actionable (waitingOn owes the answer).
	s.TransitionTask(task.ID, "block", "pum-1", TransitionInput{Note: "need creds", WaitingOn: "coord"})
	snap = s.Resume("pum-1", "agent-1", 0)
	if snap.Actionable {
		t.Fatalf("blocked-without-answer should not be actionable for the assignee, got action=%q", snap.RecommendedNextAction.Action)
	}

	// 5. From the waitingOn agent's perspective: blocked-on → answer.
	snapCoord := s.Resume("coord", "agent-coord", 0)
	if !snapCoord.Actionable || snapCoord.RecommendedNextAction.Action != "answer" {
		t.Fatalf("coord should see answer as next action for the blocked task, got actionable=%v action=%q", snapCoord.Actionable, snapCoord.RecommendedNextAction.Action)
	}

	// 6. Blocked with answer → actionable, resume for assignee; coord is now idle.
	s.TransitionTask(task.ID, "answer", "coord", TransitionInput{Note: "creds in db.env"})
	snap = s.Resume("pum-1", "agent-1", 0)
	if !snap.Actionable || snap.RecommendedNextAction.Action != "resume" {
		t.Fatalf("answered blocker should recommend resume, got actionable=%v action=%q", snap.Actionable, snap.RecommendedNextAction.Action)
	}
	snapCoord = s.Resume("coord", "agent-coord", 0)
	if snapCoord.Actionable {
		t.Fatalf("coord should be idle after answering, got action=%q", snapCoord.RecommendedNextAction.Action)
	}

	// 7. Done → review (creator is coord).
	s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	s.TransitionTask(task.ID, "done", "pum-1", TransitionInput{ChangedFiles: []string{"a.go"}})
	snapCoord = s.Resume("coord", "agent-coord", 0)
	if !snapCoord.Actionable || snapCoord.RecommendedNextAction.Action != "review" {
		t.Fatalf("coord should see review for done task, got actionable=%v action=%q", snapCoord.Actionable, snapCoord.RecommendedNextAction.Action)
	}
	// The assignee should be idle (ball is with creator).
	snapPum := s.Resume("pum-1", "agent-1", 0)
	if snapPum.Actionable {
		t.Fatalf("assignee should be idle after done (ball is with creator), got action=%q", snapPum.RecommendedNextAction.Action)
	}
}

func TestResumeRecommendedNextActionClaimFromPlan(t *testing.T) {
	s := NewStore()
	// Create a plan, then a draft task in it, then promote to ready.
	plan, err := s.CreatePlan(Plan{
		RoomID: globalRoomID, Title: "plan-1", Objective: "test",
		Planner: "coord", MaxConcurrentActive: 2,
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	task, _, err := s.CreateTask(Task{Title: "task-A", Assignee: "pum-1", Creator: "coord", PlanID: plan.ID})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, _, err := s.TransitionTask(task.ID, "promote", "coord", TransitionInput{}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	// pum-1 has no current task → should see claim as next action.
	snap := s.Resume("pum-1", "agent-1", 0)
	if !snap.Actionable || snap.RecommendedNextAction.Action != "claim" {
		t.Fatalf("eligible plan task should recommend claim, got actionable=%v action=%q", snap.Actionable, snap.RecommendedNextAction.Action)
	}
	if snap.RecommendedNextAction.PlanID != plan.ID {
		t.Fatalf("expected planId=%s, got %q", plan.ID, snap.RecommendedNextAction.PlanID)
	}
	// After claiming, the task becomes the current task → start.
	s.ClaimNextTask(plan.ID, "pum-1", nil)
	snap = s.Resume("pum-1", "agent-1", 0)
	if !snap.Actionable {
		t.Fatalf("after claim, should be actionable (start), got action=%q", snap.RecommendedNextAction.Action)
	}
	if snap.RecommendedNextAction.Action != "start" && snap.RecommendedNextAction.Action != "acknowledge" {
		t.Fatalf("after claim, should recommend start or acknowledge, got %q", snap.RecommendedNextAction.Action)
	}
}
