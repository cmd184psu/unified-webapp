package chat

import "time"

// TaskState is a point in the task lifecycle. The intended flow is:
//
//	assigned → acknowledged → working → (blocked ⇄ working) → done → accepted → closed
//	                                                        ↘ rework → working
//	any non-terminal state → cancelled
//	owned (acknowledged/working/blocked/rework) → release → ready/assigned
//	non-terminal (pre-review) → defer → deferred → reinstate → ready/assigned
//
// Transitions are deliberately lenient (e.g. an agent may "start" straight from
// assigned) to match BotChat's unenforced, cooperative model.
type TaskState string

const (
	// TaskDraft and TaskReady only apply to plan-managed tasks (see Plan). A
	// draft is still being specified by the planner; a ready task is
	// specification-complete and queue-eligible (claimable). Room-only tasks
	// skip these and start at TaskAssigned, preserving the legacy flow.
	TaskDraft        TaskState = "draft"
	TaskReady        TaskState = "ready"
	TaskAssigned     TaskState = "assigned"
	TaskAcknowledged TaskState = "acknowledged"
	TaskWorking      TaskState = "working"
	TaskBlocked      TaskState = "blocked"
	TaskDone         TaskState = "done"
	TaskAccepted     TaskState = "accepted"
	TaskRework       TaskState = "rework"
	// TaskDeferred is valid work intentionally parked off the active queue. It
	// is not terminal (the work is still wanted and can be reinstated) but it
	// holds no concurrency slot and is not claim-eligible.
	TaskDeferred  TaskState = "deferred"
	TaskClosed    TaskState = "closed"
	TaskCancelled TaskState = "cancelled"
)

// Terminal reports whether no further transitions are possible from this state.
func (t TaskState) Terminal() bool {
	return t == TaskClosed || t == TaskCancelled
}

// OccupiesSlot reports whether a task in this state holds a plan's concurrency
// slot. Per the addendum, the slot is held from claim/acknowledgement through
// Working, Blocked, Rework and Done-awaiting-review; it is freed only once the
// planner accepts/closes (or cancels) the task. Draft/Ready hold no slot.
func (t TaskState) OccupiesSlot() bool {
	switch t {
	case TaskAcknowledged, TaskWorking, TaskBlocked, TaskRework, TaskDone:
		return true
	}
	return false
}

// NeedsAttention reports whether a task is waiting on a human/coordinator or an
// assignee to act: freshly assigned, blocked, or done-awaiting-review.
func (t TaskState) NeedsAttention() bool {
	return t == TaskAssigned || t == TaskBlocked || t == TaskDone
}

// Dashboard columns for the operator board. Terminal tasks map to "" (archived,
// not shown on the live board).
const (
	ColumnDraft              = "draft"                   // plan task still being specified
	ColumnReady              = "ready"                   // plan task claimable from the queue
	ColumnActive             = "active"                  // work in progress
	ColumnWaitingForAgent    = "waiting_for_agent"       // ball in the assignee's court
	ColumnBlocked            = "blocked"                 // stuck on a dependency/blocker
	ColumnDoneAwaitingReview = "done_awaiting_review"    // agent finished, coordinator to review
	ColumnWaitingForCoord    = "waiting_for_coordinator" // accepted, coordinator to close out
	ColumnDeferred           = "deferred"                // valid work parked off the active queue
)

// DashboardColumn maps a task's state onto the operator board column it belongs
// in. Terminal states (closed/cancelled) return "" so they drop off the board.
func (t TaskState) DashboardColumn() string {
	switch t {
	case TaskDraft:
		return ColumnDraft
	case TaskReady:
		return ColumnReady
	case TaskWorking:
		return ColumnActive
	case TaskAssigned, TaskAcknowledged, TaskRework:
		return ColumnWaitingForAgent
	case TaskBlocked:
		return ColumnBlocked
	case TaskDone:
		return ColumnDoneAwaitingReview
	case TaskAccepted:
		return ColumnWaitingForCoord
	case TaskDeferred:
		return ColumnDeferred
	}
	return ""
}

// Task is a first-class unit of work that lives inside a room. Its lifecycle is
// mirrored into the room transcript as distinct "card" messages (see Message.Kind).
type Task struct {
	ID                 string    `json:"id"`  // enumerated, e.g. TASK-42
	Num                int       `json:"num"` // monotonic sequence behind the ID
	RoomID             string    `json:"roomId"`
	Title              string    `json:"title"`
	Objective          string    `json:"objective,omitempty"`
	Assignee           string    `json:"assignee,omitempty"`
	Creator            string    `json:"creator,omitempty"`
	AcceptanceCriteria string    `json:"acceptanceCriteria,omitempty"`
	Exclusions         string    `json:"exclusions,omitempty"`
	Attempt            int       `json:"attempt"`
	State              TaskState `json:"state"`
	WaitingOn          string    `json:"waitingOn,omitempty"`
	Blocker            string    `json:"blocker,omitempty"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`

	// CompletedPortions records the work that was finished independently before a
	// block, so a reviewer can see partial progress. Set on "block".
	CompletedPortions string `json:"completedPortions,omitempty"`

	// AckOverdue is a computed (non-persisted) advisory flag: the task has sat
	// in the "assigned" state without being acknowledged/started longer than the
	// configured threshold. Set at read time, so it always reflects "now".
	AckOverdue bool `json:"ackOverdue,omitempty"`

	// DependsOn lists task IDs this task depends on. Advisory (never hard-blocks
	// a transition, matching BotChat's cooperative model) — surfaced so agents
	// and operators can sequence work and avoid collisions.
	DependsOn []string `json:"dependsOn,omitempty"`

	// Capabilities lists the role/skill tags a builder should have to take this
	// task (e.g. "go", "frontend", "deploy"). Capability matching is advisory:
	// eligibility/claim queries can narrow to tasks a builder can handle, but a
	// task with no tags matches anyone, and a builder may always claim by ID.
	Capabilities []string `json:"capabilities,omitempty"`

	// DepsPending is a computed (non-persisted) list of DependsOn entries that
	// are not yet accepted/closed, so a reader can see at a glance what still
	// blocks this task from being safely started.
	DepsPending []string `json:"depsPending,omitempty"`

	// Version is an optimistic-concurrency counter, bumped on every successful
	// transition. A caller may pass expectedVersion to guard against acting on a
	// stale view (e.g. two coordinators racing on the same task).
	Version int `json:"version"`

	// SpecRevision counts how many times the task's specification (title,
	// objective, acceptance criteria, exclusions, dependencies, reservation or
	// capabilities) has been revised. It starts at 1 and is bumped by
	// ReviseTask. AckedSpecRevision records the SpecRevision the current
	// execution owner has acknowledged — set to SpecRevision on claim, on
	// acknowledge, and on the explicit ack_revision action. When a task is
	// revised after it has been claimed, AckedSpecRevision lags SpecRevision
	// until the builder acknowledges the new spec, and the builder may not mark
	// the task done until it does.
	SpecRevision      int `json:"specRevision"`
	AckedSpecRevision int `json:"ackedSpecRevision"`

	// RevisionPending is a computed (non-persisted) advisory flag: the task has
	// been claimed and its spec has been revised since the owner last
	// acknowledged it (AckedSpecRevision < SpecRevision). Set at read time.
	RevisionPending bool `json:"revisionPending,omitempty"`

	// Summary is a durable archival record written when the task is closed:
	// assignment, attempts, final completion packet, acceptance and any
	// unresolved follow-ups. It survives even after the transcript is trimmed.
	Summary string `json:"summary,omitempty"`

	// Column is a computed (non-persisted) operator-board column derived from
	// State (see DashboardColumn). Empty for terminal tasks.
	Column string `json:"column,omitempty"`

	// PlanID, when set, binds this task to a Plan. Plan-managed tasks start in
	// TaskDraft, are promoted to TaskReady, and enforce the stricter queue rules
	// (dependency gating on claim/start, per-plan concurrency). Room-only tasks
	// leave this empty and keep the lenient, advisory legacy behaviour.
	PlanID string `json:"planId,omitempty"`

	// ClaimedBy / ClaimedAt record who pulled the task off the ready queue and
	// when. Assignee remains the intended/reserved assignment (audit); ClaimedBy
	// is the execution owner. For an open (unassigned) ready task the two match
	// after a claim; for a reserved task the claimant must equal Assignee.
	ClaimedBy string     `json:"claimedBy,omitempty"`
	ClaimedAt *time.Time `json:"claimedAt,omitempty"`

	// The following fields form the structured DONE completion contract. They are
	// set on the "done" transition and carried on the completion card so humans
	// and agents get a consistent review packet instead of prose.
	ChangedFiles     []string `json:"changedFiles,omitempty"`
	Tests            string   `json:"tests,omitempty"`
	DeploymentTarget string   `json:"deploymentTarget,omitempty"`
	ServiceHealth    string   `json:"serviceHealth,omitempty"`
	EvidenceIDs      []string `json:"evidenceIds,omitempty"`
	Limitations      string   `json:"limitations,omitempty"`

	// Answer records the most recent answer to a blocker, when the waitingOn
	// agent has posted an explicit task-linked answer (action=answer). The
	// block is NOT lifted automatically — the assignee must act on the answer
	// and transition the task (start/done). AnsweredBy/AnsweredAt/AnswerSeq
	// attribute the answer; AnswerSeq is the seq of the task_answer card, so a
	// replacement agent can tell whether the answer arrived after its last
	// resume. A correction (same blocked task, different note) overwrites
	// these fields and emits a fresh card.
	Answer     string    `json:"answer,omitempty"`
	AnsweredBy string    `json:"answeredBy,omitempty"`
	AnsweredAt time.Time `json:"answeredAt,omitempty"`
	AnswerSeq  int64     `json:"answerSeq,omitempty"`
}

// TransitionInput carries the optional structured payload for a task transition.
// Note applies to every action; the remaining fields are only meaningful for
// specific actions ("block" uses WaitingOn/CompletedPortions; "done" uses the
// completion-contract fields). Zero values are ignored, so ordinary transitions
// need only Note (or nothing at all).
type TransitionInput struct {
	Note              string
	WaitingOn         string
	CompletedPortions string
	ChangedFiles      []string
	Tests             string
	DeploymentTarget  string
	ServiceHealth     string
	EvidenceIDs       []string
	Limitations       string

	// NewAssignee is required for the "handoff" action — the callsign the task
	// is being reassigned to (takeover is just a self-directed handoff).
	NewAssignee string

	// ExpectedVersion, when > 0, makes the transition an optimistic
	// compare-and-swap: it fails with ErrVersionConflict unless it matches the
	// task's current Version. 0 skips the check (fire-and-forget).
	ExpectedVersion int

	// Override, when true on a "claim" or "start" of a plan-managed task, bypasses
	// the dependency and concurrency gates. It is a deliberately privileged
	// action: it is honoured only when the actor is the plan's planner and a
	// non-empty OverrideReason is supplied, and it emits an audit card.
	Override       bool
	OverrideReason string

	// KeyID is the ID of the shared API key that authorized this transition. It
	// is stamped onto the emitted audit card so a human can trace which machine
	// key performed a claim or state change, even though the acting callsign is
	// unenforced (the key authorizes; it does not authenticate identity).
	KeyID string
}

// TaskRevision carries a planner's adjustments to a task's specification. Every
// field is a pointer (or nil slice) so an omitted field is left unchanged while
// an explicitly supplied one is applied — including clearing a field by sending
// an empty value. Note is a free-form reason recorded on the audit card;
// ExpectedVersion, when > 0, makes the revision an optimistic compare-and-swap.
type TaskRevision struct {
	Title              *string
	Objective          *string
	Assignee           *string
	AcceptanceCriteria *string
	Exclusions         *string
	DependsOn          []string
	Capabilities       []string
	Note               string
	ExpectedVersion    int

	// KeyID is the ID of the shared API key that authorized this revision,
	// stamped onto the task_revised audit card for traceability (see the note
	// on TransitionInput.KeyID).
	KeyID string
}

// TaskTransitions maps an action name to the card kind it produces, for docs and
// validation. The authoritative logic lives in nextTaskState.
var TaskActions = []string{"promote", "claim", "acknowledge", "start", "block", "answer", "done", "accept", "rework", "close", "cancel", "handoff", "ack_revision", "release", "defer", "reinstate"}

// nextTaskState validates action against the current state and returns the
// resulting state plus the card kind to emit. ok is false for illegal actions.
func nextTaskState(cur TaskState, action string) (next TaskState, cardKind string, ok bool) {
	switch action {
	case "promote":
		// Planner marks a drafted plan task specification-complete and
		// queue-eligible.
		if cur == TaskDraft {
			return TaskReady, "task_ready", true
		}
	case "claim":
		// Builder pulls a ready plan task off the queue and takes execution
		// ownership, entering the active lifecycle.
		if cur == TaskReady {
			return TaskAcknowledged, "task_claimed", true
		}
	case "acknowledge":
		if cur == TaskAssigned {
			return TaskAcknowledged, "task_acknowledged", true
		}
	case "start":
		if cur == TaskAssigned || cur == TaskAcknowledged || cur == TaskBlocked || cur == TaskRework {
			return TaskWorking, "task_started", true
		}
	case "block":
		if cur == TaskWorking {
			return TaskBlocked, "task_blocked", true
		}
	case "answer":
		// The waitingOn agent answers a blocker. The state does NOT change —
		// the assignee must act on the answer and transition the task
		// (start/done). nextTaskState only validates the task is blocked; the
		// store applies the cooperative guardrail (waitingOn == actor) and the
		// non-empty answer requirement.
		if cur == TaskBlocked {
			return TaskBlocked, "task_answer", true
		}
	case "done":
		if cur == TaskWorking {
			return TaskDone, "task_done", true
		}
	case "accept":
		if cur == TaskDone {
			return TaskAccepted, "task_accepted", true
		}
	case "rework":
		if cur == TaskDone {
			return TaskRework, "task_rework", true
		}
	case "close":
		if cur == TaskAccepted || cur == TaskCancelled {
			return TaskClosed, "task_closed", true
		}
	case "cancel":
		if !cur.Terminal() {
			return TaskCancelled, "task_cancelled", true
		}
	case "handoff":
		// Reassign an in-flight task without changing its lifecycle position.
		if !cur.Terminal() {
			return cur, "task_handoff", true
		}
	case "release":
		// The execution owner (or planner) gives up a claimed task, returning
		// it to the queue for another builder. Distinct from cancel — the work
		// is still wanted. Valid only while the task is owned and pre-review.
		switch cur {
		case TaskAcknowledged, TaskWorking, TaskBlocked, TaskRework:
			return TaskReady, "task_released", true
		}
	case "defer":
		// The planner parks valid work off the active queue without abandoning
		// it. Allowed from any non-terminal, pre-review state; a task awaiting or
		// past review should be accepted/reworked/cancelled instead.
		switch cur {
		case TaskDraft, TaskReady, TaskAssigned, TaskAcknowledged, TaskWorking, TaskBlocked, TaskRework:
			return TaskDeferred, "task_deferred", true
		}
	case "reinstate":
		// The planner brings a deferred task back into the queue.
		if cur == TaskDeferred {
			return TaskReady, "task_reinstated", true
		}
	case "ack_revision":
		// The execution owner acknowledges a revised specification without
		// changing the task's lifecycle position. Only meaningful once the task
		// has been picked up (acknowledged onward, excluding terminal states).
		switch cur {
		case TaskAcknowledged, TaskWorking, TaskBlocked, TaskRework, TaskDone:
			return cur, "task_revision_acked", true
		}
	}
	return "", "", false
}
