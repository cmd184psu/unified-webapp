package chat

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrRoomNotFound        = errors.New("room not found")
	ErrTaskNotFound        = errors.New("task not found")
	ErrInvalidTransition   = errors.New("invalid task transition")
	ErrTaskConflict        = errors.New("assignee already has an active task")
	ErrVersionConflict     = errors.New("task version conflict")
	ErrPlanNotFound        = errors.New("plan not found")
	ErrPlanClosed          = errors.New("plan is closed")
	ErrPlanRevisionStale   = errors.New("plan revision conflict")
	ErrPlanHasActiveWork   = errors.New("plan still has unfinished tasks")
	ErrDependenciesPending = errors.New("task has unmet dependencies")
	ErrConcurrencyFull     = errors.New("plan concurrency limit reached")
	ErrClaimReserved       = errors.New("task is reserved for another callsign")
	ErrOverrideForbidden   = errors.New("override is allowed only by the plan's planner with a reason")
	ErrNoEligibleTask      = errors.New("no eligible task to claim")
	ErrRevisionPending     = errors.New("task specification was revised — acknowledge the new revision before completing")
	ErrNoRevisionPending   = errors.New("no pending specification revision to acknowledge")
	ErrWaitingOnMismatch   = errors.New("you are not the agent this task is waiting on — only the waitingOn callsign may answer")
	ErrAnswerEmpty         = errors.New("answer requires a non-empty \"note\" with the resolution")
)

type Room struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	WaveID         string    `json:"waveId"`
	ModeratorKeyID string    `json:"moderatorKeyId"`
	CreatedAt      time.Time `json:"createdAt"`
	MessageCount   int       `json:"messageCount"`
}

type Message struct {
	ID        string    `json:"id"`
	RoomID    string    `json:"roomId"`
	Callsign  string    `json:"callsign"`
	To        string    `json:"to,omitempty"`
	KeyID     string    `json:"keyId"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	// Seq is a store-wide monotonic sequence number that doubles as an opaque
	// cursor for resumable reads (room history ?after= and the inbox). It is
	// assigned on append: from the SQLite rowid when persistent, or an
	// in-memory counter otherwise.
	Seq int64 `json:"seq"`
	// Kind distinguishes ordinary chat ("" or "chat") from task lifecycle
	// "end-cap" cards such as task_started, task_blocked, task_done. TaskID
	// links a card back to the Task it describes.
	Kind   string `json:"kind,omitempty"`
	TaskID string `json:"taskId,omitempty"`
}

// Callsign is a lightweight, unenforced lease binding a display name to an
// immutable agent instance. The API key is shared authorization, not identity;
// the callsign (and its InstanceID) is identity. Leases exist so operators can
// see who is active and release stale names — they never block posting.
//
// A key may hold multiple leases, one per callsign it has claimed, because the
// shared-key model lets several agents on one machine post under different
// names. The lease is keyed by (keyID, name) so presence is attributed to the
// explicit "from" on each message/heartbeat, not whichever callsign the key
// last registered.
type Callsign struct {
	Name           string    `json:"callsign"`
	InstanceID     string    `json:"instanceId"`
	KeyID          string    `json:"keyId"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
}

// leaseKey is the in-memory map key for a callsign lease: (keyID, name). A
// shared key may hold several leases (one per callsign), so the key cannot be
// keyID alone — otherwise posting as one callsign would refresh another's
// presence, which is exactly the attribution bug this fixes.
func leaseKey(keyID, name string) string { return keyID + "\x00" + name }

type Store struct {
	mu         sync.RWMutex
	rooms      map[string]*Room
	messages   map[string][]Message
	callsigns  map[string]Callsign
	tasks      map[string]*Task
	plans      map[string]*Plan
	ackCursors map[string]int64 // keyed by ackKey(callsign, instanceID)
	taskSeq    int
	planSeq    int
	msgSeq     int64
	db         *sql.DB
	retention  time.Duration

	// TaskIDPrefix is prepended to the task sequence number to form a task ID
	// (e.g. "TASK" → "TASK-42"). ExclusiveTaskPerAssignee, when set, forbids
	// creating a task for an assignee that already has an active (non-terminal)
	// task. Both are configured by the caller after construction.
	TaskIDPrefix             string
	ExclusiveTaskPerAssignee bool

	// CallsignLeaseTTL marks a callsign "stale" once its last activity is older
	// than this. It is purely advisory for operators; posting is never blocked.
	// <= 0 disables staleness (callsigns are never reported stale).
	CallsignLeaseTTL time.Duration

	// AckThreshold flags a task as "ack overdue" once it has sat in the
	// "assigned" state (never acknowledged/started) longer than this. Advisory
	// only, surfaced to operators/coordinators. <= 0 disables the warning.
	AckThreshold time.Duration
}

const globalRoomID = "global"
const maxMessagesPerRoom = 500
const defaultTaskPrefix = "TASK"

func NewStore() *Store {
	s := &Store{
		rooms:        make(map[string]*Room),
		messages:     make(map[string][]Message),
		callsigns:    make(map[string]Callsign),
		tasks:        make(map[string]*Task),
		plans:        make(map[string]*Plan),
		ackCursors:   make(map[string]int64),
		TaskIDPrefix: defaultTaskPrefix,
	}
	s.rooms[globalRoomID] = &Room{
		ID:        globalRoomID,
		Name:      "Global",
		WaveID:    "",
		CreatedAt: time.Now(),
	}
	s.messages[globalRoomID] = []Message{}
	return s
}

func (s *Store) CreateRoom(name, waveID, moderatorKeyID string) (Room, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &Room{
		ID:             newID("room"),
		Name:           name,
		WaveID:         waveID,
		ModeratorKeyID: moderatorKeyID,
		CreatedAt:      time.Now(),
	}
	s.rooms[r.ID] = r
	s.messages[r.ID] = []Message{}
	if s.db != nil {
		s.persistExec(
			`INSERT OR IGNORE INTO rooms(id, name, wave_id, moderator_key_id, created_at) VALUES(?,?,?,?,?)`,
			r.ID, r.Name, r.WaveID, r.ModeratorKeyID, r.CreatedAt.UnixNano(),
		)
	}
	return *r, nil
}

func (s *Store) GetRoom(id string) (Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rooms[id]
	if !ok {
		return Room{}, ErrRoomNotFound
	}
	return *r, nil
}

func (s *Store) DeleteRoom(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == globalRoomID {
		return errors.New("cannot delete global room")
	}
	if _, ok := s.rooms[id]; !ok {
		return ErrRoomNotFound
	}
	delete(s.rooms, id)
	delete(s.messages, id)
	for tid, t := range s.tasks {
		if t.RoomID == id {
			delete(s.tasks, tid)
		}
	}
	if s.db != nil {
		s.persistExec(`DELETE FROM rooms WHERE id = ?`, id)
		s.persistExec(`DELETE FROM messages WHERE room_id = ?`, id)
		s.persistExec(`DELETE FROM tasks WHERE room_id = ?`, id)
	}
	return nil
}

func (s *Store) ListRooms() []Room {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		out = append(out, *r)
	}
	return out
}

func (s *Store) PostMessage(roomID, callsign, keyID, content, to string) (Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Attribute presence to the explicit "from" callsign on this message, not
	// whichever callsign the shared key last registered. A post as BrainStorm
	// on a key registered to HarnessBuilder must bump BrainStorm's lease, not
	// HarnessBuilder's — that is the attribution bug this fixes.
	lk := leaseKey(keyID, callsign)
	if c, ok := s.callsigns[lk]; ok {
		c.LastActivityAt = time.Now()
		s.callsigns[lk] = c
		if s.db != nil {
			s.persistExec(`UPDATE callsigns SET last_activity_at = ? WHERE key_id = ? AND name = ?`, c.LastActivityAt.UnixNano(), keyID, callsign)
		}
	}
	return s.appendMessageLocked(Message{
		RoomID:   roomID,
		Callsign: callsign,
		To:       to,
		KeyID:    keyID,
		Content:  content,
	})
}

// appendMessageLocked stores m (filling in ID/Timestamp), enforcing the per-room
// cap and write-through persistence. Callers must hold s.mu.
func (s *Store) appendMessageLocked(m Message) (Message, error) {
	r, ok := s.rooms[m.RoomID]
	if !ok {
		return Message{}, ErrRoomNotFound
	}
	m.ID = newID("msg")
	m.Timestamp = time.Now()
	if s.db != nil {
		res, err := s.db.Exec(
			`INSERT INTO messages(id, room_id, callsign, to_callsign, key_id, content, ts, kind, task_id) VALUES(?,?,?,?,?,?,?,?,?)`,
			m.ID, m.RoomID, m.Callsign, m.To, m.KeyID, m.Content, m.Timestamp.UnixNano(), m.Kind, m.TaskID,
		)
		if err != nil {
			log.Printf("chat: persistence error: message insert failed: %v (room=%s id=%s)", err, m.RoomID, m.ID)
		} else if seq, err := res.LastInsertId(); err == nil {
			m.Seq = seq
			if seq > s.msgSeq {
				s.msgSeq = seq
			}
		}

		s.persistExec(
			`DELETE FROM messages WHERE room_id = ? AND id NOT IN (SELECT id FROM messages WHERE room_id = ? ORDER BY seq DESC LIMIT ?)`,
			m.RoomID, m.RoomID, maxMessagesPerRoom,
		)
	} else {
		s.msgSeq++
		m.Seq = s.msgSeq
	}
	msgs := append(s.messages[m.RoomID], m)
	if len(msgs) > maxMessagesPerRoom {
		msgs = msgs[len(msgs)-maxMessagesPerRoom:]
	}
	s.messages[m.RoomID] = msgs
	r.MessageCount++
	return m, nil
}

func (s *Store) GetMessages(roomID string) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.rooms[roomID]; !ok {
		return nil, ErrRoomNotFound
	}
	msgs := s.messages[roomID]
	out := make([]Message, len(msgs))
	copy(out, msgs)
	return out, nil
}

// MessagesAfter returns a room's messages whose Seq is strictly greater than
// after, in ascending Seq order. after <= 0 returns the full room history. This
// backs resumable reads: a client remembers the Seq of the last message it saw
// and passes it back to fetch only what is new.
func (s *Store) MessagesAfter(roomID string, after int64) ([]Message, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.rooms[roomID]; !ok {
		return nil, ErrRoomNotFound
	}
	msgs := s.messages[roomID]
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Seq > after {
			out = append(out, m)
		}
	}
	return out, nil
}

// Inbox returns, across all rooms, the messages addressed to callsign (To ==
// callsign, matched case-insensitively; "everyone" is excluded) with Seq
// strictly greater than after, in ascending Seq order. Task lifecycle cards are
// included automatically because they are routed via To (creator/assignee). The
// returned cursor is the Seq of the last message (or after when empty), which
// the caller passes back on the next poll. limit <= 0 applies a default cap.
func (s *Store) Inbox(callsign string, after int64, limit int) (msgs []Message, cursor int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	cursor = after
	target := strings.ToLower(callsign)
	all := make([]Message, 0)
	for _, roomMsgs := range s.messages {
		for _, m := range roomMsgs {
			if m.Seq <= after {
				continue
			}
			if strings.ToLower(m.To) != target {
				continue
			}
			all = append(all, m)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Seq < all[j].Seq })
	if len(all) > limit {
		all = all[:limit]
	}
	if n := len(all); n > 0 {
		cursor = all[n-1].Seq
	}
	return all, cursor
}

// ackKey is the durable inbox-ack cursor key: (normalized callsign, instanceID).
// The callsign is lowercased so "BrainStorm" and "brainstorm" share a cursor;
// the instanceID scopes the cursor to a specific agent instance. This composite
// key is what BrainStorm's seq 539 design requires: instanceID alone would
// conflate BrainStorm/HarnessBuilder/BOTCHATDEV when they share a key, and a
// bare callsign would conflate two different keys' agents posting under the
// same name. (normalized callsign, agentInstance) gives same-key/same-callsign
// replacement inheritance while isolating callsigns sharing the key, and a new
// key/instance with the same callsign starts fresh.
func ackKey(callsign, instanceID string) string {
	return strings.ToLower(callsign) + "\x00" + instanceID
}

// GetAckCursor returns the highest message seq that (callsign, instanceID) has
// explicitly acknowledged via POST /api/v1/inbox/ack, or 0 if none. Read-only:
// it never advances the cursor. Used by GET /api/v1/resume to scope the inbox
// to unacknowledged directed events without the caller having to remember its
// own cursor.
func (s *Store) GetAckCursor(callsign, instanceID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ackCursors[ackKey(callsign, instanceID)]
}

// AckInbox is the explicit mutation that advances the server-side inbox cursor
// for (callsign, instanceID) to max(existing, seq). It is monotonic — a stale
// or lower seq never moves the cursor backwards. Returns the new cursor value.
// This is the "I have processed everything up to seq" signal from the agent;
// GET /api/v1/resume uses it to avoid re-delivering acknowledged events.
func (s *Store) AckInbox(callsign, instanceID string, seq int64) int64 {
	if seq < 0 {
		seq = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := ackKey(callsign, instanceID)
	cur := s.ackCursors[k]
	if seq > cur {
		cur = seq
		s.ackCursors[k] = cur
		if s.db != nil {
			s.persistExec(
				`INSERT OR REPLACE INTO inbox_acks(callsign, instance_id, ack_seq, updated_at) VALUES(?,?,?,?)`,
				strings.ToLower(callsign), instanceID, cur, time.Now().UnixNano(),
			)
		}
	}
	return cur
}

// ResumeSnapshot is the consolidated, read-only resume picture returned by
// GET /api/v1/resume. It folds the §9 resume checklist's steps 1–4 into one
// call: the agent's ack cursor, its unacknowledged directed inbox, its active
// tasks, the tasks needing its attention, the tasks awaiting its review, the
// tasks blocked on it, and its current (active, non-terminal) task. The cursor
// field is the highest seq in the inbox slice — the value to ACK after
// processing. GET /resume does NOT advance the ack cursor; only POST
// /api/v1/inbox/ack does.
//
// Actionable is true when the watcher should wake the agent — at least one of:
// the agent has a current task needing work, an answered blocker to resume,
// a done task awaiting its review/close, an assigned/rework task needing
// acknowledgment, or an active plan with an eligible claimable task.
// RecommendedNextAction is the structured next step the agent should take.
type ResumeSnapshot struct {
	AckSeq                int64                 `json:"ackSeq"`
	Inbox                 []Message             `json:"inbox"`
	Cursor                int64                 `json:"cursor"`
	Tasks                 []Task                `json:"tasks"`
	NeedsAttention        []Task                `json:"needsAttention"`
	AwaitingReview        []Task                `json:"awaitingReview"`
	BlockedOn             []Task                `json:"blockedOn"`
	CurrentTask           *Task                 `json:"currentTask,omitempty"`
	Actionable            bool                  `json:"actionable"`
	RecommendedNextAction RecommendedNextAction `json:"recommendedNextAction"`
}

// RecommendedNextAction is the watcher/agent's structured "what should I do
// next?" derived from the resume snapshot. Action is one of: "resume",
// "answer", "review", "close", "claim", "acknowledge", "idle". TaskID and
// PlanID identify the target when applicable. Reason is a human-readable
// explanation suitable for a log line or an agent prompt.
type RecommendedNextAction struct {
	Action string `json:"action"`
	TaskID string `json:"taskId,omitempty"`
	PlanID string `json:"planId,omitempty"`
	Reason string `json:"reason"`
}

// Resume returns the consolidated resume snapshot for (callsign, instanceID),
// read-only. It takes a single RLock and performs all scans inline (the public
// Inbox/ListTasks/TasksAwaitingReview/TasksBlockedOn helpers each take their own
// RLock and would re-enter the mutex, which Go's sync.RWMutex does not support
// safely). The inbox is scoped to messages addressed to callsign with Seq
// strictly greater than the stored ack cursor; the task slices mirror the
// individual task-listing endpoints. limit <= 0 applies a default cap to the
// inbox slice. The ack cursor is NOT advanced.
func (s *Store) Resume(callsign, instanceID string, limit int) ResumeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 {
		limit = 100
	}
	ack := s.ackCursors[ackKey(callsign, instanceID)]
	target := strings.ToLower(callsign)
	snap := ResumeSnapshot{AckSeq: ack, Cursor: ack}

	// Inbox: directed messages to callsign with Seq > ack, across all rooms,
	// ascending by Seq, capped at limit. Mirrors Store.Inbox but inline.
	all := make([]Message, 0)
	for _, roomMsgs := range s.messages {
		for _, m := range roomMsgs {
			if m.Seq <= ack {
				continue
			}
			if strings.ToLower(m.To) != target {
				continue
			}
			all = append(all, m)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Seq < all[j].Seq })
	if len(all) > limit {
		all = all[:limit]
	}
	if n := len(all); n > 0 {
		snap.Cursor = all[n-1].Seq
	}
	snap.Inbox = all
	if snap.Inbox == nil {
		snap.Inbox = []Message{}
	}

	// Tasks: the agent's active (non-terminal) tasks + the needs_attention set
	// + tasks awaiting the agent's review (done, agent is creator) + tasks
	// blocked on the agent. Each slice is decorated (AckOverdue/DepsPending/
	// Column) and ordered by task number, matching the public helpers. Note
	// TaskDone is NOT terminal (only Closed/Cancelled are): a Done task still
	// needs the creator to accept/rework, so it lands in AwaitingReview.
	snap.Tasks = make([]Task, 0)
	snap.NeedsAttention = make([]Task, 0)
	snap.AwaitingReview = make([]Task, 0)
	snap.BlockedOn = make([]Task, 0)
	unresolvedMine := make(map[string]bool)
	for _, t := range s.tasks {
		// Done + the agent is the creator → awaiting the agent's review.
		if t.State == TaskDone && strings.EqualFold(t.Creator, callsign) {
			snap.AwaitingReview = append(snap.AwaitingReview, s.decorateTaskLocked(t))
		}
		// Fully terminal (closed/cancelled) — nothing more to do.
		if t.State.Terminal() {
			continue
		}
		// Track the agent's own non-terminal tasks for dependency-gated
		// blocked-on resolution downstream.
		if strings.EqualFold(t.Assignee, callsign) || strings.EqualFold(t.ClaimedBy, callsign) {
			unresolvedMine[t.ID] = true
		}
		// The agent's active assigned tasks (and the first one is currentTask).
		if strings.EqualFold(t.Assignee, callsign) {
			snap.Tasks = append(snap.Tasks, s.decorateTaskLocked(t))
			if snap.CurrentTask == nil {
				tc := s.decorateTaskLocked(t)
				snap.CurrentTask = &tc
			}
		}
		// Needs-attention spans the whole board (assigned/blocked/done), not
		// just the agent's own — matches GET /api/v1/tasks?status=needs_attention.
		if t.State.NeedsAttention() {
			snap.NeedsAttention = append(snap.NeedsAttention, s.decorateTaskLocked(t))
		}
		// Blocked-on: a task explicitly waiting on the agent.
		if t.State == TaskBlocked && strings.EqualFold(t.WaitingOn, callsign) {
			snap.BlockedOn = append(snap.BlockedOn, s.decorateTaskLocked(t))
		}
	}
	// Blocked-on also includes downstream tasks depending on an unfinished task
	// owned by callsign (the dependency-gated case), deduplicated.
	seen := make(map[string]bool)
	for _, t := range snap.BlockedOn {
		seen[t.ID] = true
	}
	for _, t := range s.tasks {
		for _, depID := range t.DependsOn {
			if unresolvedMine[depID] && !t.State.Terminal() && !seen[t.ID] {
				seen[t.ID] = true
				snap.BlockedOn = append(snap.BlockedOn, s.decorateTaskLocked(t))
				break
			}
		}
	}
	sort.Slice(snap.Tasks, func(i, j int) bool { return snap.Tasks[i].Num < snap.Tasks[j].Num })
	sort.Slice(snap.NeedsAttention, func(i, j int) bool { return snap.NeedsAttention[i].Num < snap.NeedsAttention[j].Num })
	sort.Slice(snap.AwaitingReview, func(i, j int) bool { return snap.AwaitingReview[i].Num < snap.AwaitingReview[j].Num })
	sort.Slice(snap.BlockedOn, func(i, j int) bool { return snap.BlockedOn[i].Num < snap.BlockedOn[j].Num })
	snap.Actionable, snap.RecommendedNextAction = s.deriveNextActionLocked(callsign, snap)
	return snap
}

// deriveNextActionLocked computes the actionable flag and the structured
// recommendedNextAction from a filled-in resume snapshot. Caller holds s.mu
// (read). Priority order:
//  1. Current task with an answered blocker → resume (the ball is back with
//     the assignee; start to lift the block).
//  2. Current task in assigned/acknowledged/rework → acknowledge/start.
//  3. Current task in working → resume (continue work).
//  4. Current task in blocked (unanswered) → not actionable for this agent
//     (the waitingOn agent owes the answer).
//  5. Done task awaiting this agent's review → review (accept/rework).
//  6. Accepted task awaiting this agent's close → close.
//  7. Blocked task waiting on this agent → answer the blocker.
//  8. No current task → scan active plans for an eligible claimable task →
//     claim.
//  9. Nothing actionable → idle.
func (s *Store) deriveNextActionLocked(callsign string, snap ResumeSnapshot) (bool, RecommendedNextAction) {
	if snap.CurrentTask != nil {
		t := *snap.CurrentTask
		switch t.State {
		case TaskBlocked:
			if t.Answer != "" {
				return true, RecommendedNextAction{
					Action: "resume",
					TaskID: t.ID,
					Reason: fmt.Sprintf("blocker on %s has been answered by %s — start to resume work (the block is not lifted automatically)", t.ID, t.AnsweredBy),
				}
			}
			return false, RecommendedNextAction{
				Action: "idle",
				TaskID: t.ID,
				Reason: fmt.Sprintf("%s is blocked waiting on %s — not actionable for %s", t.ID, t.WaitingOn, callsign),
			}
		case TaskAssigned:
			return true, RecommendedNextAction{
				Action: "acknowledge",
				TaskID: t.ID,
				Reason: fmt.Sprintf("%s is assigned to you — acknowledge then start", t.ID),
			}
		case TaskAcknowledged, TaskRework:
			return true, RecommendedNextAction{
				Action: "start",
				TaskID: t.ID,
				Reason: fmt.Sprintf("%s is in state %s — start work", t.ID, t.State),
			}
		case TaskWorking:
			return true, RecommendedNextAction{
				Action: "resume",
				TaskID: t.ID,
				Reason: fmt.Sprintf("%s is in progress — continue work", t.ID),
			}
		case TaskDone:
			// The assignee's work is complete; the creator owes the review.
			// If the agent IS the creator, recommend review; otherwise fall
			// through (the AwaitingReview branch catches it for the creator,
			// and the assignee is idle on this task).
			if strings.EqualFold(t.Creator, callsign) {
				return true, RecommendedNextAction{
					Action: "review",
					TaskID: t.ID,
					Reason: fmt.Sprintf("%s is done — accept or rework", t.ID),
				}
			}
			// Fall through: the assignee has no action on a done task.
		case TaskAccepted:
			// Same pattern: only the creator owes the close.
			if strings.EqualFold(t.Creator, callsign) {
				return true, RecommendedNextAction{
					Action: "close",
					TaskID: t.ID,
					Reason: fmt.Sprintf("%s is accepted — close it", t.ID),
				}
			}
			// Fall through.
		}
	}
	if len(snap.AwaitingReview) > 0 {
		t := snap.AwaitingReview[0]
		return true, RecommendedNextAction{
			Action: "review",
			TaskID: t.ID,
			Reason: fmt.Sprintf("%s is done awaiting your review — accept or rework", t.ID),
		}
	}
	for _, t := range snap.BlockedOn {
		if t.Answer == "" {
			return true, RecommendedNextAction{
				Action: "answer",
				TaskID: t.ID,
				Reason: fmt.Sprintf("%s is blocked waiting on you — post action=answer with the resolution", t.ID),
			}
		}
	}
	// No current task — scan active plans for an eligible claimable task.
	for _, p := range s.plans {
		if p.Status != PlanActive {
			continue
		}
		if s.planActiveCountLocked(p.ID) >= p.effectiveMaxConcurrent() {
			continue
		}
		for _, tid := range p.TaskIDs {
			t, ok := s.tasks[tid]
			if !ok || t.State != TaskReady {
				continue
			}
			if t.Assignee != "" && !strings.EqualFold(t.Assignee, callsign) {
				continue
			}
			if len(s.depsPendingLocked(t)) > 0 {
				continue
			}
			return true, RecommendedNextAction{
				Action: "claim",
				TaskID: t.ID,
				PlanID: p.ID,
				Reason: fmt.Sprintf("claim %s from plan %s — it is ready, dependencies satisfied, and within plan concurrency", t.ID, p.ID),
			}
		}
	}
	return false, RecommendedNextAction{
		Action: "idle",
		Reason: "no actionable work — idle",
	}
}

// RegisterCallsign stores a convenience mapping from an API key + callsign to
// a lease, so an agent can ask "what's my callsign?" later. It is deliberately
// unenforced: the same callsign may be held by many keys, and a key may hold
// several leases (one per callsign it has claimed). Identity is asserted per
// message via the "from" field, not by this registry, so it never fails. The
// instanceID is stable per (keyID, name) — a replacement agent using the same
// key + same callsign inherits the prior instance, while a different callsign
// on the same key gets a distinct instance (it is a different agent).
func (s *Store) RegisterCallsign(name, keyID string) (Callsign, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	lk := leaseKey(keyID, name)
	c, existed := s.callsigns[lk]
	c.Name = name
	c.KeyID = keyID
	if !existed || c.InstanceID == "" {
		c.InstanceID = newID("agent")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.LastActivityAt = now
	s.callsigns[lk] = c
	if s.db != nil {
		s.persistExec(
			`INSERT OR REPLACE INTO callsigns(key_id, name, instance_id, created_at, last_activity_at) VALUES(?,?,?,?,?)`,
			c.KeyID, c.Name, c.InstanceID, c.CreatedAt.UnixNano(), c.LastActivityAt.UnixNano(),
		)
	}
	return c, nil
}

// GetCallsign reports the display name leased under (keyID, name), if any.
func (s *Store) GetCallsign(keyID, name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.callsigns[leaseKey(keyID, name)]
	if !ok {
		return "", false
	}
	return c.Name, true
}

// TouchCallsign bumps the last-activity timestamp on the (keyID, name) lease,
// if one exists. It is best-effort: unregistered (key, callsign) pairs are
// ignored. Posting a message and heartbeating call this with the explicit
// "from" callsign so presence is attributed to the agent that actually acted,
// not whichever callsign the shared key last remembered.
func (s *Store) TouchCallsign(keyID, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lk := leaseKey(keyID, name)
	c, ok := s.callsigns[lk]
	if !ok {
		return
	}
	c.LastActivityAt = time.Now()
	s.callsigns[lk] = c
	if s.db != nil {
		s.persistExec(`UPDATE callsigns SET last_activity_at = ? WHERE key_id = ? AND name = ?`, c.LastActivityAt.UnixNano(), keyID, name)
	}
}

// UnregisterCallsign drops every lease held by keyID. A shared key may hold
// several (one per callsign it claimed); this releases all of them, matching
// the "I am leaving" semantics of the DELETE endpoint.
func (s *Store) UnregisterCallsign(keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for lk, c := range s.callsigns {
		if c.KeyID == keyID {
			delete(s.callsigns, lk)
			found = true
		}
	}
	if !found {
		return errors.New("no callsign registered for this key")
	}
	if s.db != nil {
		s.persistExec(`DELETE FROM callsigns WHERE key_id = ?`, keyID)
	}
	return nil
}

// ReleaseCallsignByInstance removes the lease with the given immutable instance
// ID, regardless of which key holds it. It is an operator control for reclaiming
// stale callsigns. Returns the released lease and whether one was found.
func (s *Store) ReleaseCallsignByInstance(instanceID string) (Callsign, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for keyID, c := range s.callsigns {
		if c.InstanceID == instanceID {
			delete(s.callsigns, keyID)
			if s.db != nil {
				s.persistExec(`DELETE FROM callsigns WHERE key_id = ?`, keyID)
			}
			return c, true
		}
	}
	return Callsign{}, false
}

// ReleaseCallsignByName removes every lease whose display name matches name
// (case-insensitively) and returns how many were released. Operator control.
func (s *Store) ReleaseCallsignByName(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	target := strings.ToLower(name)
	n := 0
	for keyID, c := range s.callsigns {
		if strings.ToLower(c.Name) == target {
			delete(s.callsigns, keyID)
			if s.db != nil {
				s.persistExec(`DELETE FROM callsigns WHERE key_id = ?`, keyID)
			}
			n++
		}
	}
	return n
}

// SuggestIfTaken reports whether name is currently leased by a key other than
// exceptKeyID and, if so, proposes a distinct suffixed alternative. It is purely
// advisory — registration is never blocked; impersonation is allowed by design.
func (s *Store) SuggestIfTaken(name, exceptKeyID string) (suggestion string, taken bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	target := strings.ToLower(name)
	used := make(map[string]bool, len(s.callsigns))
	for _, c := range s.callsigns {
		used[strings.ToLower(c.Name)] = true
		// Compare the struct field, not the map key: the map is now keyed by
		// leaseKey(keyID, name), so the loop key is not the bare keyID.
		if strings.ToLower(c.Name) == target && c.KeyID != exceptKeyID {
			taken = true
		}
	}
	if !taken {
		return "", false
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", name, i)
		if !used[strings.ToLower(cand)] {
			return cand, true
		}
	}
}

// ListCallsigns returns the distinct callsigns currently registered, for
// discovery. Because callsigns are unenforced, the same name may be backed by
// more than one key; duplicates are collapsed here.
func (s *Store) ListCallsigns() []Callsign {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := make(map[string]bool, len(s.callsigns))
	out := make([]Callsign, 0, len(s.callsigns))
	for _, c := range s.callsigns {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	return out
}

// CallsignInfo augments a callsign lease with operator-facing computed fields:
// whether the lease is stale (idle longer than CallsignLeaseTTL) and the ID of
// the agent's current active (non-terminal) task, if any.
type CallsignInfo struct {
	Callsign       string    `json:"callsign"`
	InstanceID     string    `json:"instanceId"`
	KeyID          string    `json:"keyId"`
	CreatedAt      time.Time `json:"createdAt"`
	LastActivityAt time.Time `json:"lastActivityAt"`
	Stale          bool      `json:"stale"`
	CurrentTask    string    `json:"currentTask,omitempty"`
	// Presence is a derived state so operators can tell at a glance whether an
	// agent is disconnected, stale, working, waiting, or merely online. It is
	// computed from last activity and the state of the agent's current task —
	// BotChat does not track live socket connections, so this is advisory.
	Presence string `json:"presence"`
	// WaitingOn echoes the current task's blocker target when the agent is
	// waiting (its task is blocked), so "who/what are we waiting on" is visible
	// without opening the task.
	WaitingOn string `json:"waitingOn,omitempty"`
}

// Presence states surfaced by ListCallsignInfo.
const (
	PresenceDisconnected = "disconnected" // registered but never active
	PresenceStale        = "stale"        // idle longer than the callsign lease TTL
	PresenceWorking      = "working"      // has a task in the working state
	PresenceWaiting      = "waiting"      // has a blocked or done-awaiting-review task
	PresenceOnline       = "online"       // recently active, no active task
)

// ListCallsignInfo returns one entry per lease (not deduplicated) with computed
// staleness, presence, and current-task fields, for the operator console.
func (s *Store) ListCallsignInfo() []CallsignInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	out := make([]CallsignInfo, 0, len(s.callsigns))
	for _, c := range s.callsigns {
		info := CallsignInfo{
			Callsign:       c.Name,
			InstanceID:     c.InstanceID,
			KeyID:          c.KeyID,
			CreatedAt:      c.CreatedAt,
			LastActivityAt: c.LastActivityAt,
		}
		if s.CallsignLeaseTTL > 0 && !c.LastActivityAt.IsZero() {
			info.Stale = now.Sub(c.LastActivityAt) > s.CallsignLeaseTTL
		}
		var curState TaskState
		for _, t := range s.tasks {
			if strings.EqualFold(t.Assignee, c.Name) && !t.State.Terminal() {
				info.CurrentTask = t.ID
				curState = t.State
				if t.State == TaskBlocked {
					info.WaitingOn = t.WaitingOn
				}
				break
			}
		}
		info.Presence = derivePresence(c.LastActivityAt, info.Stale, curState)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Callsign < out[j].Callsign })
	return out
}

// CallsignInfoForKey returns the computed presence entry for the (keyID, name)
// lease, if one is registered. Used by the heartbeat endpoint to echo the
// agent's own presence back to it; the agent supplies its callsign so presence
// is attributed to the right lease on a shared key.
func (s *Store) CallsignInfoForKey(keyID, name string) (CallsignInfo, bool) {
	s.mu.RLock()
	c, ok := s.callsigns[leaseKey(keyID, name)]
	s.mu.RUnlock()
	if !ok {
		return CallsignInfo{}, false
	}
	for _, info := range s.ListCallsignInfo() {
		if info.KeyID == keyID && info.InstanceID == c.InstanceID && info.Callsign == name {
			return info, true
		}
	}
	return CallsignInfo{}, false
}

// derivePresence maps an agent's last activity and current-task state onto one
// of the presence constants. Precedence: never-active → disconnected; idle past
// the lease → stale; otherwise the current task drives working/waiting, falling
// back to online.
func derivePresence(lastActivity time.Time, stale bool, cur TaskState) string {
	if lastActivity.IsZero() {
		return PresenceDisconnected
	}
	if stale {
		return PresenceStale
	}
	switch cur {
	case TaskWorking:
		return PresenceWorking
	case TaskBlocked, TaskDone:
		return PresenceWaiting
	}
	return PresenceOnline
}

// TaskFilter narrows ListTasks. An empty field matches everything. Status may be
// a concrete TaskState or the pseudo-status "needs_attention".
type TaskFilter struct {
	RoomID   string
	Assignee string
	Status   string
}

// CreateTask registers a new task in its room and emits an "assigned" card into
// the transcript. Returns the task and the card message for broadcasting.
func (s *Store) CreateTask(in Task) (Task, Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// A plan-managed task lives inside a plan (which pins the room) and starts
	// its life as a draft rather than an assignment. Resolve the plan first so
	// the room is inherited before the room-existence check.
	var plan *Plan
	if in.PlanID != "" {
		p, ok := s.plans[in.PlanID]
		if !ok {
			return Task{}, Message{}, ErrPlanNotFound
		}
		if p.Status == PlanClosed {
			return Task{}, Message{}, ErrPlanClosed
		}
		plan = p
		in.RoomID = p.RoomID
	}
	if _, ok := s.rooms[in.RoomID]; !ok {
		return Task{}, Message{}, ErrRoomNotFound
	}
	if s.ExclusiveTaskPerAssignee && in.Assignee != "" {
		for _, t := range s.tasks {
			if t.Assignee == in.Assignee && !t.State.Terminal() {
				return Task{}, Message{}, ErrTaskConflict
			}
		}
	}
	s.taskSeq++
	prefix := s.TaskIDPrefix
	if prefix == "" {
		prefix = defaultTaskPrefix
	}
	now := time.Now()
	state := TaskAssigned
	cardKind := "task_assigned"
	if plan != nil {
		state = TaskDraft
		cardKind = "task_created"
	}
	t := &Task{
		Num:                s.taskSeq,
		ID:                 fmt.Sprintf("%s-%d", prefix, s.taskSeq),
		RoomID:             in.RoomID,
		Title:              in.Title,
		Objective:          in.Objective,
		Assignee:           in.Assignee,
		Creator:            in.Creator,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Exclusions:         in.Exclusions,
		DependsOn:          in.DependsOn,
		Capabilities:       in.Capabilities,
		PlanID:             in.PlanID,
		Attempt:            1,
		Version:            1,
		SpecRevision:       1,
		State:              state,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	s.tasks[t.ID] = t
	s.persistTaskLocked(t)
	if plan != nil {
		plan.TaskIDs = append(plan.TaskIDs, t.ID)
		plan.UpdatedAt = now
		s.persistPlanLocked(plan)
	}
	card, _ := s.appendMessageLocked(Message{
		RoomID:   t.RoomID,
		Callsign: t.Creator,
		To:       t.Assignee,
		Content:  taskCardContent(*t, cardKind),
		Kind:     cardKind,
		TaskID:   t.ID,
	})
	return *t, card, nil
}

// TransitionTask applies action to task id, validating the state machine. actor
// is the callsign performing the transition; in carries the optional structured
// payload (note, block details, DONE completion contract). Returns the updated
// task and the emitted card message.
func (s *Store) TransitionTask(id, action, actor string, in TransitionInput) (Task, Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, Message{}, ErrTaskNotFound
	}
	if in.ExpectedVersion > 0 && in.ExpectedVersion != t.Version {
		return Task{}, Message{}, ErrVersionConflict
	}
	next, kind, ok := nextTaskState(t.State, action)
	if !ok {
		return Task{}, Message{}, ErrInvalidTransition
	}
	// answer is a task-linked response to a blocker. It is cooperative: only
	// the agent the task is waitingOn may answer, and the note must carry the
	// resolution. Idempotent — a retry with the same answer text is a no-op
	// (no new card, no version bump). A correction (different text) overwrites
	// and emits a fresh task_answer card. The state stays blocked; the
	// assignee must act on the answer and transition the task.
	if action == "answer" {
		if !strings.EqualFold(t.WaitingOn, actor) {
			return Task{}, Message{}, ErrWaitingOnMismatch
		}
		if strings.TrimSpace(in.Note) == "" {
			return Task{}, Message{}, ErrAnswerEmpty
		}
		if t.AnsweredBy != "" && t.Answer == in.Note {
			return *t, Message{}, nil
		}
	}
	revisionPending := t.AckedSpecRevision > 0 && t.AckedSpecRevision < t.SpecRevision
	// A builder must acknowledge a post-claim spec revision before completing
	// the task, so the DONE it reports is against the current specification.
	if action == "done" && revisionPending {
		return Task{}, Message{}, ErrRevisionPending
	}
	// ack_revision only makes sense when there is a pending revision to accept.
	if action == "ack_revision" && !revisionPending {
		return Task{}, Message{}, ErrNoRevisionPending
	}
	// Plan-managed tasks enforce the stricter queue rules: planner-only promote,
	// reservation on claim, and dependency + concurrency gates on claim/start
	// (with a deliberate planner-only override). Room-only tasks skip all of
	// this and keep the lenient, advisory legacy behaviour.
	overrideApplied := false
	if t.PlanID != "" {
		if err := s.planGateLocked(t, action, actor, in, &overrideApplied); err != nil {
			return Task{}, Message{}, err
		}
	}
	// release/reinstate return a task to the queue. For plan-managed tasks that
	// is the claimable Ready state; room-only tasks have no queue, so they fall
	// back to Assigned (unclaimed, ball in the assignee's court).
	if (action == "release" || action == "reinstate") && next == TaskReady && t.PlanID == "" {
		next = TaskAssigned
	}
	prevAssignee := t.Assignee
	t.State = next
	t.UpdatedAt = time.Now()
	// answer does not move the task through the lifecycle (state stays
	// blocked), so it does not bump the version. ExpectedVersion still guards
	// against concurrent state changes: if the task moved out of blocked, the
	// version bumped and the answer's CAS fails correctly.
	if action != "answer" {
		t.Version++
	}
	switch action {
	case "claim":
		claimedAt := t.UpdatedAt
		t.ClaimedBy = actor
		t.ClaimedAt = &claimedAt
		if t.Assignee == "" {
			t.Assignee = actor
		}
		// Taking execution ownership acknowledges the current specification.
		t.AckedSpecRevision = t.SpecRevision
	case "acknowledge":
		// Picking up an assigned task acknowledges the current specification.
		t.AckedSpecRevision = t.SpecRevision
	case "ack_revision":
		t.AckedSpecRevision = t.SpecRevision
	case "block":
		t.Blocker = in.Note
		t.WaitingOn = in.WaitingOn
		t.CompletedPortions = in.CompletedPortions
	case "answer":
		t.Answer = in.Note
		t.AnsweredBy = actor
		t.AnsweredAt = t.UpdatedAt
		// AnswerSeq is backfilled after the card is appended (needs card.Seq).
	case "start":
		// The block is lifted by the state moving to working — Blocker,
		// WaitingOn and Answer are preserved as the most recent blocker
		// record so the Q&A pair survives into the closed-task summary. A
		// subsequent "block" overwrites them with the new blocker.
	case "rework":
		t.Attempt++
	case "done":
		t.ChangedFiles = in.ChangedFiles
		t.Tests = in.Tests
		t.DeploymentTarget = in.DeploymentTarget
		t.ServiceHealth = in.ServiceHealth
		t.EvidenceIDs = in.EvidenceIDs
		t.Limitations = in.Limitations
	case "handoff":
		t.Assignee = in.NewAssignee
	case "release":
		// Drop execution ownership and the auto-reservation so the task is truly
		// open for the next builder. Work fields (evidence, blocker) are left
		// intact so nothing is lost.
		t.ClaimedBy = ""
		t.ClaimedAt = nil
		t.Assignee = ""
	case "defer":
		// Park the work: release the active owner but keep the reservation and
		// all accumulated work so it can be reinstated exactly where it was.
		t.ClaimedBy = ""
		t.ClaimedAt = nil
	case "close":
		t.Summary = buildTaskSummary(t)
	}
	s.persistTaskLocked(t)
	content := taskCardContent(*t, kind)
	if action == "handoff" && prevAssignee != "" {
		content = fmt.Sprintf("[%s] handed off from %s to %s", t.ID, prevAssignee, t.Assignee)
	}
	// "block" already folds the note into the card as the blocker line, so only
	// append it as a free-form suffix for the other actions. "answer" folds the
	// note into the card as the answer line, so it is skipped here too.
	if in.Note != "" && action != "block" && action != "answer" {
		content += " — " + in.Note
	}
	to := t.Creator
	switch action {
	case "accept", "rework", "close", "cancel", "handoff", "defer", "reinstate", "answer":
		// Address the card at the assignee so it lands in their inbox (for
		// handoff this is the new assignee; for defer/reinstate the reserved
		// builder learns their work was parked or brought back; for answer the
		// assignee learns their blocker has been resolved).
		to = t.Assignee
	}
	card, _ := s.appendMessageLocked(Message{
		RoomID:   t.RoomID,
		Callsign: actor,
		To:       to,
		Content:  content,
		Kind:     kind,
		TaskID:   t.ID,
		KeyID:    in.KeyID,
	})
	// answer needs the card's Seq to complete its attribution record.
	if action == "answer" {
		t.AnswerSeq = card.Seq
		s.persistTaskLocked(t)
	}
	// A privileged planner override leaves its own audit card so the bypass of
	// the dependency/concurrency gates is visible in the transcript.
	if overrideApplied {
		_, _ = s.appendMessageLocked(Message{
			RoomID:   t.RoomID,
			Callsign: actor,
			To:       t.Creator,
			Content:  fmt.Sprintf("[%s] planner override on %s: %s", t.ID, action, in.OverrideReason),
			Kind:     "task_override",
			TaskID:   t.ID,
			KeyID:    in.KeyID,
		})
	}
	return *t, card, nil
}

// ReviseTask applies a planner's adjustments to a task's specification (title,
// objective, acceptance criteria, exclusions, reservation, dependencies or
// capabilities) without moving it through the lifecycle. actor is the callsign
// performing the revision. For a plan-managed task only the plan's planner may
// revise (mirroring promote/override); room-only tasks stay lenient. A terminal
// task cannot be revised. When rev.ExpectedVersion > 0 the revision is an
// optimistic compare-and-swap against the task's Version. Each applied revision
// bumps Version and SpecRevision and emits a task_revised audit card carrying a
// human-readable diff, addressed to the current execution owner so a
// mid-flight builder is notified that the spec changed under it.
func (s *Store) ReviseTask(id, actor string, rev TaskRevision) (Task, Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, Message{}, ErrTaskNotFound
	}
	if rev.ExpectedVersion > 0 && rev.ExpectedVersion != t.Version {
		return Task{}, Message{}, ErrVersionConflict
	}
	if t.State.Terminal() {
		return Task{}, Message{}, ErrInvalidTransition
	}
	if t.PlanID != "" {
		if plan := s.plans[t.PlanID]; plan != nil && plan.Planner != "" && actor != plan.Planner {
			return Task{}, Message{}, ErrOverrideForbidden
		}
	}
	var changes []string
	if rev.Title != nil && *rev.Title != t.Title {
		changes = append(changes, fmt.Sprintf("title: %q → %q", t.Title, *rev.Title))
		t.Title = *rev.Title
	}
	if rev.Objective != nil && *rev.Objective != t.Objective {
		changes = append(changes, "objective updated")
		t.Objective = *rev.Objective
	}
	if rev.Assignee != nil && *rev.Assignee != t.Assignee {
		changes = append(changes, fmt.Sprintf("reserved for: %q → %q", t.Assignee, *rev.Assignee))
		t.Assignee = *rev.Assignee
	}
	if rev.AcceptanceCriteria != nil && *rev.AcceptanceCriteria != t.AcceptanceCriteria {
		changes = append(changes, "acceptance criteria updated")
		t.AcceptanceCriteria = *rev.AcceptanceCriteria
	}
	if rev.Exclusions != nil && *rev.Exclusions != t.Exclusions {
		changes = append(changes, "exclusions updated")
		t.Exclusions = *rev.Exclusions
	}
	if rev.DependsOn != nil && !sameOrder(rev.DependsOn, t.DependsOn) {
		changes = append(changes, fmt.Sprintf("depends on: [%s] → [%s]", strings.Join(t.DependsOn, ", "), strings.Join(rev.DependsOn, ", ")))
		t.DependsOn = append([]string{}, rev.DependsOn...)
	}
	if rev.Capabilities != nil && !sameOrder(rev.Capabilities, t.Capabilities) {
		changes = append(changes, fmt.Sprintf("capabilities: [%s] → [%s]", strings.Join(t.Capabilities, ", "), strings.Join(rev.Capabilities, ", ")))
		t.Capabilities = append([]string{}, rev.Capabilities...)
	}
	if len(changes) == 0 {
		return Task{}, Message{}, ErrInvalidTransition
	}
	t.Version++
	t.SpecRevision++
	t.UpdatedAt = time.Now()
	s.persistTaskLocked(t)
	content := fmt.Sprintf("[%s] revised to r%d by %s\n%s", t.ID, t.SpecRevision, actor, strings.Join(changes, "\n"))
	if rev.Note != "" {
		content += "\nnote: " + rev.Note
	}
	if t.AckedSpecRevision > 0 && t.AckedSpecRevision < t.SpecRevision {
		content += fmt.Sprintf("\n%s must ack_revision before marking this task done", t.ClaimedBy)
	}
	// Address the card at the current execution owner (claimant, else assignee)
	// so a mid-flight builder is notified; fall back to the creator otherwise.
	to := t.ClaimedBy
	if to == "" {
		to = t.Assignee
	}
	if to == "" {
		to = t.Creator
	}
	card, _ := s.appendMessageLocked(Message{
		RoomID:   t.RoomID,
		Callsign: actor,
		To:       to,
		Content:  content,
		Kind:     "task_revised",
		TaskID:   t.ID,
		KeyID:    rev.KeyID,
	})
	return s.decorateTaskLocked(t), card, nil
}

// GetTask returns a task by ID.
func (s *Store) GetTask(id string) (Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return s.decorateTaskLocked(t), nil
}

// decorateTaskLocked returns a copy of t with the computed (non-persisted)
// read-time fields populated: ack-overdue advisory, pending dependencies, and
// the operator-board column. Caller holds s.mu (read or write).
func (s *Store) decorateTaskLocked(t *Task) Task {
	out := *t
	out.AckOverdue = s.ackOverdueLocked(t)
	out.DepsPending = s.depsPendingLocked(t)
	out.Column = t.State.DashboardColumn()
	out.RevisionPending = t.AckedSpecRevision > 0 && t.AckedSpecRevision < t.SpecRevision
	return out
}

// depsPendingLocked returns the subset of t.DependsOn that are not yet resolved
// (a dependency is resolved once it is accepted or closed). Unknown dependency
// IDs are treated as pending. Caller holds s.mu.
func (s *Store) depsPendingLocked(t *Task) []string {
	if len(t.DependsOn) == 0 {
		return nil
	}
	var pending []string
	for _, id := range t.DependsOn {
		dep, ok := s.tasks[id]
		if !ok || (dep.State != TaskAccepted && dep.State != TaskClosed) {
			pending = append(pending, id)
		}
	}
	return pending
}

// ackOverdueLocked reports whether a task has been assigned but not
// acknowledged/started for longer than AckThreshold. Caller holds s.mu.
func (s *Store) ackOverdueLocked(t *Task) bool {
	if s.AckThreshold <= 0 || t.State != TaskAssigned {
		return false
	}
	return time.Since(t.UpdatedAt) > s.AckThreshold
}

// ListTasks returns tasks matching f, ordered by creation sequence.
func (s *Store) ListTasks(f TaskFilter) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if f.RoomID != "" && t.RoomID != f.RoomID {
			continue
		}
		if f.Assignee != "" && t.Assignee != f.Assignee {
			continue
		}
		if f.Status == "needs_attention" {
			if !t.State.NeedsAttention() {
				continue
			}
		} else if f.Status != "" && string(t.State) != f.Status {
			continue
		}
		out = append(out, s.decorateTaskLocked(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

// TasksAwaitingReview returns the tasks that are done and awaiting review by
// callsign — i.e. in the Done state with callsign as their creator (the planner
// who must accept or request rework). Ordered by task sequence. This answers the
// agent-facing "tasks awaiting my review" query without scanning room history.
func (s *Store) TasksAwaitingReview(callsign string) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0)
	if callsign == "" {
		return out
	}
	for _, t := range s.tasks {
		if t.State == TaskDone && t.Creator == callsign {
			out = append(out, s.decorateTaskLocked(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

// TasksBlockedOn returns the tasks whose progress is waiting on callsign to act.
// Two kinds qualify: (1) a Blocked task that explicitly names callsign in its
// WaitingOn field, and (2) any non-terminal task that depends on an unfinished
// task owned (assigned to or claimed by) callsign — the downstream task is
// dependency-gated until callsign completes its work. Deduplicated and ordered
// by task sequence. This answers the agent-facing "tasks blocked on me" query.
func (s *Store) TasksBlockedOn(callsign string) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, 0)
	if callsign == "" {
		return out
	}
	// Task IDs owned by callsign that are not yet resolved (accepted/closed);
	// downstream tasks depending on these are effectively blocked on callsign.
	unresolvedMine := make(map[string]bool)
	for id, t := range s.tasks {
		if t.State == TaskAccepted || t.State == TaskClosed || t.State == TaskCancelled {
			continue
		}
		if t.Assignee == callsign || t.ClaimedBy == callsign {
			unresolvedMine[id] = true
		}
	}
	seen := make(map[string]bool)
	add := func(t *Task) {
		if seen[t.ID] {
			return
		}
		seen[t.ID] = true
		out = append(out, s.decorateTaskLocked(t))
	}
	for _, t := range s.tasks {
		if t.State == TaskBlocked && t.WaitingOn == callsign {
			add(t)
			continue
		}
		if t.State.Terminal() {
			continue
		}
		for _, dep := range t.DependsOn {
			if unresolvedMine[dep] {
				add(t)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Num < out[j].Num })
	return out
}

// persistExec runs a write against the persistent store and logs any error
// instead of silently discarding it. The in-memory state has already been
// mutated by the caller, so the service keeps working even when persistence
// fails — but a silent swallow hid a real schema bug for a full session (the
// VALUES placeholder count mismatch in persistTaskLocked), so critical task
// and plan mutations now surface their persistence errors. Caller holds s.mu.
func (s *Store) persistExec(query string, args ...any) {
	if s.db == nil {
		return
	}
	if _, err := s.db.Exec(query, args...); err != nil {
		log.Printf("chat: persistence error: %v (query: %s)", err, truncateQuery(query))
	}
}

// truncateQuery shortens a SQL statement for log lines so a long INSERT does
// not spam the log. The prefix is enough to identify which write failed.
func truncateQuery(q string) string {
	const max = 120
	if len(q) <= max {
		return q
	}
	return q[:max] + "…"
}

func (s *Store) persistTaskLocked(t *Task) {
	if s.db == nil {
		return
	}
	var claimedAt int64
	if t.ClaimedAt != nil {
		claimedAt = t.ClaimedAt.UnixNano()
	}
	s.persistExec(
		`INSERT INTO tasks(num, id, room_id, title, objective, assignee, creator, acceptance, exclusions, attempt, state, waiting_on, blocker, completed_portions, changed_files, tests, deployment_target, service_health, evidence_ids, limitations, depends_on, capabilities, answer, answered_by, answered_at, answer_seq, version, spec_revision, acked_spec_revision, summary, plan_id, claimed_by, claimed_at, created_at, updated_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   title=excluded.title, objective=excluded.objective, assignee=excluded.assignee,
		   creator=excluded.creator, acceptance=excluded.acceptance, exclusions=excluded.exclusions,
		   attempt=excluded.attempt, state=excluded.state, waiting_on=excluded.waiting_on,
		   blocker=excluded.blocker, completed_portions=excluded.completed_portions,
		   changed_files=excluded.changed_files, tests=excluded.tests,
		   deployment_target=excluded.deployment_target, service_health=excluded.service_health,
		   evidence_ids=excluded.evidence_ids, limitations=excluded.limitations,
		   depends_on=excluded.depends_on, capabilities=excluded.capabilities,
		   answer=excluded.answer, answered_by=excluded.answered_by,
		   answered_at=excluded.answered_at, answer_seq=excluded.answer_seq,
		   version=excluded.version, spec_revision=excluded.spec_revision,
		   acked_spec_revision=excluded.acked_spec_revision, summary=excluded.summary,
		   plan_id=excluded.plan_id, claimed_by=excluded.claimed_by, claimed_at=excluded.claimed_at,
		   updated_at=excluded.updated_at`,
		t.Num, t.ID, t.RoomID, t.Title, t.Objective, t.Assignee, t.Creator, t.AcceptanceCriteria,
		t.Exclusions, t.Attempt, string(t.State), t.WaitingOn, t.Blocker, t.CompletedPortions,
		encodeStringSlice(t.ChangedFiles), t.Tests, t.DeploymentTarget, t.ServiceHealth,
		encodeStringSlice(t.EvidenceIDs), t.Limitations,
		encodeStringSlice(t.DependsOn), encodeStringSlice(t.Capabilities),
		t.Answer, t.AnsweredBy, t.AnsweredAt.UnixNano(), t.AnswerSeq,
		t.Version, t.SpecRevision, t.AckedSpecRevision, t.Summary,
		t.PlanID, t.ClaimedBy, claimedAt,
		t.CreatedAt.UnixNano(), t.UpdatedAt.UnixNano(),
	)
}

// persistPlanLocked upserts a plan row. No-op for an in-memory store. Caller
// holds s.mu (write).
func (s *Store) persistPlanLocked(p *Plan) {
	if s.db == nil {
		return
	}
	var closedAt int64
	if p.ClosedAt != nil {
		closedAt = p.ClosedAt.UnixNano()
	}
	s.persistExec(
		`INSERT INTO plans(id, room_id, title, objective, planner, revision, status, task_ids, max_concurrent_active, created_at, updated_at, closed_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO UPDATE SET
		   title=excluded.title, objective=excluded.objective, planner=excluded.planner,
		   revision=excluded.revision, status=excluded.status, task_ids=excluded.task_ids,
		   max_concurrent_active=excluded.max_concurrent_active,
		   updated_at=excluded.updated_at, closed_at=excluded.closed_at`,
		p.ID, p.RoomID, p.Title, p.Objective, p.Planner, p.Revision, string(p.Status),
		encodeStringSlice(p.TaskIDs), p.MaxConcurrentActive,
		p.CreatedAt.UnixNano(), p.UpdatedAt.UnixNano(), closedAt,
	)
}

// encodeStringSlice serializes a string slice to a compact JSON array for
// storage in a TEXT column. Empty/nil slices become "" so legacy rows and empty
// values are indistinguishable and cheap to store.
func encodeStringSlice(v []string) string {
	if len(v) == 0 {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// decodeStringSlice is the inverse of encodeStringSlice. Empty strings and
// malformed JSON decode to nil.
func decodeStringSlice(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	return out
}

func taskCardContent(t Task, kind string) string {
	assignee := t.Assignee
	if assignee == "" {
		assignee = "(unassigned)"
	}
	switch kind {
	case "task_assigned":
		head := fmt.Sprintf("[%s] assigned to %s: %s", t.ID, assignee, t.Title)
		if len(t.DependsOn) > 0 {
			head += "\ndepends on: " + strings.Join(t.DependsOn, ", ")
		}
		if len(t.Capabilities) > 0 {
			head += "\ncapabilities: " + strings.Join(t.Capabilities, ", ")
		}
		return head
	case "task_created":
		head := fmt.Sprintf("[%s] drafted in plan %s: %s", t.ID, t.PlanID, t.Title)
		if t.Assignee != "" {
			head += "\nreserved for: " + t.Assignee
		}
		if len(t.DependsOn) > 0 {
			head += "\ndepends on: " + strings.Join(t.DependsOn, ", ")
		}
		if len(t.Capabilities) > 0 {
			head += "\ncapabilities: " + strings.Join(t.Capabilities, ", ")
		}
		return head
	case "task_ready":
		head := fmt.Sprintf("[%s] READY — %s", t.ID, t.Title)
		if t.Assignee != "" {
			head += " (reserved for " + t.Assignee + ")"
		}
		return head
	case "task_claimed":
		return fmt.Sprintf("[%s] claimed by %s", t.ID, t.ClaimedBy)
	case "task_override":
		return fmt.Sprintf("[%s] planner override by %s", t.ID, assignee)
	case "task_handoff":
		return fmt.Sprintf("[%s] handed off to %s", t.ID, assignee)
	case "task_acknowledged":
		return fmt.Sprintf("[%s] acknowledged by %s", t.ID, assignee)
	case "task_started":
		return fmt.Sprintf("[%s] work started (attempt %d)", t.ID, t.Attempt)
	case "task_blocked":
		head := fmt.Sprintf("[%s] BLOCKED", t.ID)
		if t.WaitingOn != "" {
			head += ", waiting on " + t.WaitingOn
		}
		var lines []string
		if t.Blocker != "" {
			lines = append(lines, "blocker: "+t.Blocker)
		}
		if t.CompletedPortions != "" {
			lines = append(lines, "completed so far: "+t.CompletedPortions)
		}
		if len(lines) == 0 {
			return head
		}
		return head + "\n" + strings.Join(lines, "\n")
	case "task_answer":
		head := fmt.Sprintf("[%s] ANSWER to blocker", t.ID)
		var lines []string
		if t.Blocker != "" {
			lines = append(lines, "blocker: "+t.Blocker)
		}
		if t.Answer != "" {
			lines = append(lines, "answer: "+t.Answer)
		}
		if t.AnsweredBy != "" {
			lines = append(lines, "from: "+t.AnsweredBy)
		}
		assignee := t.Assignee
		if assignee == "" {
			assignee = "(unassigned)"
		}
		lines = append(lines, "next: @"+assignee+" resume/start — the blocker has been answered (the block is not lifted automatically)")
		return head + "\n" + strings.Join(lines, "\n")
	case "task_done":
		head := fmt.Sprintf("[%s] DONE — awaiting review", t.ID)
		var lines []string
		if len(t.ChangedFiles) > 0 {
			lines = append(lines, "changed files: "+strings.Join(t.ChangedFiles, ", "))
		}
		if t.Tests != "" {
			lines = append(lines, "tests: "+t.Tests)
		}
		if t.DeploymentTarget != "" {
			lines = append(lines, "deployment: "+t.DeploymentTarget)
		}
		if t.ServiceHealth != "" {
			lines = append(lines, "health: "+t.ServiceHealth)
		}
		if len(t.EvidenceIDs) > 0 {
			lines = append(lines, "evidence: "+strings.Join(t.EvidenceIDs, ", "))
		}
		if t.Limitations != "" {
			lines = append(lines, "limitations: "+t.Limitations)
		}
		if len(lines) == 0 {
			return head
		}
		return head + "\n" + strings.Join(lines, "\n")
	case "task_accepted":
		return fmt.Sprintf("[%s] ACCEPTED", t.ID)
	case "task_rework":
		return fmt.Sprintf("[%s] REWORK requested (attempt %d)", t.ID, t.Attempt)
	case "task_closed":
		head := fmt.Sprintf("[%s] closed", t.ID)
		if t.Summary != "" {
			return head + "\n" + t.Summary
		}
		return head
	case "task_cancelled":
		return fmt.Sprintf("[%s] cancelled", t.ID)
	case "task_released":
		return fmt.Sprintf("[%s] released — back in the queue", t.ID)
	case "task_deferred":
		head := fmt.Sprintf("[%s] DEFERRED — parked off the active queue", t.ID)
		if t.Assignee != "" {
			head += " (reserved for " + t.Assignee + ")"
		}
		return head
	case "task_reinstated":
		return fmt.Sprintf("[%s] reinstated — back in the queue", t.ID)
	case "task_revision_acked":
		return fmt.Sprintf("[%s] revision %d acknowledged by %s", t.ID, t.SpecRevision, t.ClaimedBy)
	}
	return t.ID
}

// buildTaskSummary produces the durable archival record retained when a task is
// closed: assignment, attempts, the final completion packet, acceptance state
// and any unresolved follow-ups (limitations / an outstanding blocker). Kept as
// a single pre-formatted block so it survives transcript trimming.
func buildTaskSummary(t *Task) string {
	lines := []string{
		fmt.Sprintf("summary: %s", t.Title),
	}
	if t.Assignee != "" {
		lines = append(lines, fmt.Sprintf("assignee: %s", t.Assignee))
	}
	if t.Creator != "" {
		lines = append(lines, fmt.Sprintf("creator: %s", t.Creator))
	}
	lines = append(lines, fmt.Sprintf("attempts: %d", t.Attempt))
	lines = append(lines, fmt.Sprintf("final state: %s", t.State))
	if len(t.ChangedFiles) > 0 {
		lines = append(lines, "changed files: "+strings.Join(t.ChangedFiles, ", "))
	}
	if t.Tests != "" {
		lines = append(lines, "tests: "+t.Tests)
	}
	if t.DeploymentTarget != "" {
		lines = append(lines, "deployment: "+t.DeploymentTarget)
	}
	if len(t.EvidenceIDs) > 0 {
		lines = append(lines, "evidence: "+strings.Join(t.EvidenceIDs, ", "))
	}
	var followups []string
	if t.Limitations != "" {
		followups = append(followups, t.Limitations)
	}
	// A blocker that was answered is part of the task's story — preserve the
	// question and answer in the summary body. An unanswered blocker remains
	// an unresolved follow-up.
	if t.Blocker != "" {
		if t.Answer != "" {
			lines = append(lines, "blocker: "+t.Blocker)
			by := t.AnsweredBy
			if by == "" {
				by = "(unknown)"
			}
			lines = append(lines, fmt.Sprintf("answer: %s (by %s)", t.Answer, by))
		} else {
			followups = append(followups, "unresolved blocker: "+t.Blocker)
		}
	}
	if len(followups) > 0 {
		lines = append(lines, "follow-ups: "+strings.Join(followups, "; "))
	}
	return strings.Join(lines, "\n")
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(b))
}
