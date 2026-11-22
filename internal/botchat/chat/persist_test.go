package chat

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")

	s, err := OpenStore(path, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}

	room, err := s.CreateRoom("wave-42", "42", "key_mod")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	if _, err := s.RegisterCallsign("nimbus-owl", "key_a"); err != nil {
		t.Fatalf("RegisterCallsign: %v", err)
	}
	if _, err := s.PostMessage(room.ID, "nimbus-owl", "key_a", "persist me", ""); err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := OpenStore(path, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	if _, err := s2.GetRoom(globalRoomID); err != nil {
		t.Fatalf("global room should survive restart: %v", err)
	}
	if _, err := s2.GetRoom(room.ID); err != nil {
		t.Fatalf("created room should survive restart: %v", err)
	}
	msgs, err := s2.GetMessages(room.ID)
	if err != nil {
		t.Fatalf("GetMessages after restart: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "persist me" {
		t.Fatalf("message should survive restart, got %+v", msgs)
	}
	if msgs[0].Callsign != "nimbus-owl" {
		t.Fatalf("message callsign lost across restart: %q", msgs[0].Callsign)
	}
	if name, ok := s2.GetCallsign("key_a", "nimbus-owl"); !ok || name != "nimbus-owl" {
		t.Fatalf("callsign registration should survive restart, got %q ok=%v", name, ok)
	}
}

func TestGlobalRoomCreatedAtPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")

	s, err := OpenStore(path, 0)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	first, err := s.GetRoom(globalRoomID)
	if err != nil {
		t.Fatalf("GetRoom global: %v", err)
	}
	_ = s.Close()

	s2, err := OpenStore(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	second, err := s2.GetRoom(globalRoomID)
	if err != nil {
		t.Fatalf("GetRoom global after restart: %v", err)
	}
	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatalf("global createdAt reset across restart: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
}

func TestMessageTTLPurge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")

	s, err := OpenStore(path, 24*time.Hour)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.RegisterCallsign("nimbus-owl", "key_a"); err != nil {
		t.Fatalf("RegisterCallsign: %v", err)
	}
	if _, err := s.PostMessage(globalRoomID, "nimbus-owl", "key_a", "recent", ""); err != nil {
		t.Fatalf("PostMessage recent: %v", err)
	}

	old := time.Now().Add(-48 * time.Hour)
	if _, err := s.db.Exec(
		`INSERT INTO messages(id, room_id, callsign, to_callsign, key_id, content, ts) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		"msg_old", globalRoomID, "nimbus-owl", "", "key_a", "ancient", old.UnixNano(),
	); err != nil {
		t.Fatalf("insert old message: %v", err)
	}
	s.mu.Lock()
	s.messages[globalRoomID] = append([]Message{{
		ID: "msg_old", RoomID: globalRoomID, Callsign: "nimbus-owl", KeyID: "key_a",
		Content: "ancient", Timestamp: old,
	}}, s.messages[globalRoomID]...)
	s.mu.Unlock()

	if err := s.PurgeExpired(); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}

	msgs, err := s.GetMessages(globalRoomID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Content != "recent" {
		t.Fatalf("expired message should be purged, remaining=%+v", msgs)
	}

	_ = s.Close()
	s2, err := OpenStore(path, 24*time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	msgs2, err := s2.GetMessages(globalRoomID)
	if err != nil {
		t.Fatalf("GetMessages after restart: %v", err)
	}
	if len(msgs2) != 1 {
		t.Fatalf("purge should be durable, got %d messages after restart", len(msgs2))
	}
}

func TestAckCursorPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")

	s, err := OpenStore(path, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	// Post a message so we have a real seq to ack against.
	msg, err := s.PostMessage(globalRoomID, "sender", "key_a", "hello", "pum-1")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	// Ack past it as (pum-1, agent_x).
	if got := s.AckInbox("pum-1", "agent_x", msg.Seq); got != msg.Seq {
		t.Fatalf("AckInbox should return %d, got %d", msg.Seq, got)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := OpenStore(path, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	// The ack cursor must survive restart.
	if got := s2.GetAckCursor("pum-1", "agent_x"); got != msg.Seq {
		t.Fatalf("ack cursor should survive restart, got %d want %d", got, msg.Seq)
	}
	// Normalized callsign lookup must also work after restart.
	if got := s2.GetAckCursor("PUM-1", "agent_x"); got != msg.Seq {
		t.Fatalf("normalized callsign lookup after restart: got %d want %d", got, msg.Seq)
	}
	// A different instanceId is a fresh cursor.
	if got := s2.GetAckCursor("pum-1", "agent_y"); got != 0 {
		t.Fatalf("different instanceId should be fresh after restart, got %d", got)
	}
	// Resume should reflect the persisted ack: inbox empty for the acked instance.
	snap := s2.Resume("pum-1", "agent_x", 0)
	if len(snap.Inbox) != 0 {
		t.Fatalf("acked instance should have empty inbox after restart, got %d", len(snap.Inbox))
	}
	if snap.AckSeq != msg.Seq {
		t.Fatalf("resume ackSeq after restart: got %d want %d", snap.AckSeq, msg.Seq)
	}
}

func TestAnswerFieldsPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chat.db")
	s, err := OpenStore(path, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "persist-answer", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "start", "pum-1", TransitionInput{})
	_, _, _ = s.TransitionTask(task.ID, "block", "pum-1", TransitionInput{Note: "need creds", WaitingOn: "coord"})
	ans, ansCard, err := s.TransitionTask(task.ID, "answer", "coord", TransitionInput{Note: "creds in db.env"})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := OpenStore(path, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()
	loaded, err := s2.GetTask(task.ID)
	if err != nil {
		t.Fatalf("GetTask after restart: %v", err)
	}
	if loaded.Answer != "creds in db.env" {
		t.Fatalf("answer lost across restart: %q", loaded.Answer)
	}
	if loaded.AnsweredBy != "coord" {
		t.Fatalf("answeredBy lost across restart: %q", loaded.AnsweredBy)
	}
	if loaded.AnswerSeq != ans.AnswerSeq || loaded.AnswerSeq != ansCard.Seq {
		t.Fatalf("answerSeq lost across restart: loaded=%d ans=%d card=%d", loaded.AnswerSeq, ans.AnswerSeq, ansCard.Seq)
	}
	if loaded.Blocker != "need creds" {
		t.Fatalf("blocker lost across restart: %q", loaded.Blocker)
	}
	if loaded.State != TaskBlocked {
		t.Fatalf("state lost across restart: %q", loaded.State)
	}
}
