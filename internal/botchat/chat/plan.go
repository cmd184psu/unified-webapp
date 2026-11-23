package chat

import (
	"fmt"
	"strings"
	"time"
)

// PlanStatus is the lifecycle state of a Plan.
type PlanStatus string

const (
	// PlanDraft is a plan still being assembled by its planner.
	PlanDraft PlanStatus = "draft"
	// PlanActive is a plan whose tasks are being worked.
	PlanActive PlanStatus = "active"
	// PlanClosed is a finished (or abandoned) plan; no new tasks may be added.
	PlanClosed PlanStatus = "closed"
)

// Plan is a first-class, authoritative work queue that lives inside a room. It
// is the planner's ordered view of what should be done. Tasks bound to a plan
// (Task.PlanID) follow the stricter Draft → Ready → claim lifecycle with
// dependency and concurrency gating; room-only tasks are unaffected.
type Plan struct {
	ID        string     `json:"id"`
	RoomID    string     `json:"roomId"`
	Title     string     `json:"title"`
	Objective string     `json:"objective,omitempty"`
	Planner   string     `json:"planner,omitempty"`
	Revision  int        `json:"revision"`
	Status    PlanStatus `json:"status"`
	// TaskIDs is the planner's intended execution order. Builders claim the
	// first eligible ready task in this order (see ClaimNextTask).
	TaskIDs []string `json:"taskIds"`
	// MaxConcurrentActive caps how many of the plan's tasks may occupy a slot
	// (be actively worked) at once. <= 0 means the default of 1 (strictly
	// sequential); a higher value allows controlled parallelism.
	MaxConcurrentActive int        `json:"maxConcurrentActive"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
	ClosedAt            *time.Time `json:"closedAt,omitempty"`
}

// effectiveMaxConcurrent returns the configured concurrency cap, defaulting to
// 1 (strictly sequential) when unset.
func (p *Plan) effectiveMaxConcurrent() int {
	if p.MaxConcurrentActive <= 0 {
		return 1
	}
	return p.MaxConcurrentActive
}

// CreatePlan registers a new plan (in PlanActive) inside an existing room.
func (s *Store) CreatePlan(in Plan) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rooms[in.RoomID]; !ok {
		return Plan{}, ErrRoomNotFound
	}
	s.planSeq++
	now := time.Now()
	p := &Plan{
		ID:                  newID("plan"),
		RoomID:              in.RoomID,
		Title:               in.Title,
		Objective:           in.Objective,
		Planner:             in.Planner,
		Revision:            1,
		Status:              PlanActive,
		TaskIDs:             []string{},
		MaxConcurrentActive: in.MaxConcurrentActive,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	s.plans[p.ID] = p
	s.persistPlanLocked(p)
	return *p, nil
}

// GetPlan returns a plan by ID.
func (s *Store) GetPlan(id string) (Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.plans[id]
	if !ok {
		return Plan{}, ErrPlanNotFound
	}
	return *p, nil
}

// ListPlans returns every plan, optionally filtered by room, ordered by
// creation sequence.
func (s *Store) ListPlans(roomID string) []Plan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Plan, 0, len(s.plans))
	for _, p := range s.plans {
		if roomID != "" && p.RoomID != roomID {
			continue
		}
		out = append(out, *p)
	}
	sortByCreatedAt(out)
	return out
}

// PlanRevision carries the fields a planner may adjust on RevisePlan. A nil
// pointer field is left unchanged; a non-nil field is applied. Every applied
// revision bumps the plan's Revision counter. ExpectedRevision, when > 0, makes
// the revision an optimistic compare-and-swap against the plan's Revision.
type PlanRevision struct {
	Title               *string
	Objective           *string
	MaxConcurrentActive *int
	TaskIDs             []string // when non-nil, replaces the ordered task list
	ExpectedRevision    int
}

// RevisePlan applies a planner's adjustments and bumps the revision. Reordering
// via TaskIDs must be a permutation of the plan's existing tasks (no adds or
// drops — tasks are added through CreateTask and removed by cancelling them).
// When rev.ExpectedRevision > 0 the revision is an optimistic compare-and-swap:
// it fails with ErrPlanRevisionStale unless it matches the plan's current
// Revision, so two planners racing on the same plan cannot silently clobber
// each other.
func (s *Store) RevisePlan(id string, rev PlanRevision) (Plan, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return Plan{}, ErrPlanNotFound
	}
	if p.Status == PlanClosed {
		return Plan{}, ErrPlanClosed
	}
	if rev.ExpectedRevision > 0 && rev.ExpectedRevision != p.Revision {
		return Plan{}, ErrPlanRevisionStale
	}
	if rev.Title != nil {
		p.Title = *rev.Title
	}
	if rev.Objective != nil {
		p.Objective = *rev.Objective
	}
	if rev.MaxConcurrentActive != nil {
		p.MaxConcurrentActive = *rev.MaxConcurrentActive
	}
	if rev.TaskIDs != nil {
		if !sameMembers(rev.TaskIDs, p.TaskIDs) {
			return Plan{}, fmt.Errorf("%w: reorder must be a permutation of the plan's tasks", ErrInvalidTransition)
		}
		p.TaskIDs = append([]string{}, rev.TaskIDs...)
	}
	p.Revision++
	p.UpdatedAt = time.Now()
	s.persistPlanLocked(p)
	return *p, nil
}

// ClosePlan marks a plan closed. Its tasks are left as-is (their transcripts and
// summaries survive); no further tasks may be added. By default a plan with
// unfinished (non-terminal) tasks cannot be closed — the planner should resolve
// or cancel the outstanding work first — and ErrPlanHasActiveWork is returned.
// Passing force with a non-empty reason overrides the guard and drops a
// plan_force_closed audit card into the room naming the abandoned tasks, so the
// deliberate abandonment is visible in the transcript. actor is the callsign
// closing the plan (recorded on the audit card). Closing an already-closed plan
// is a no-op.
func (s *Store) ClosePlan(id, actor string, force bool, reason string) (Plan, Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.plans[id]
	if !ok {
		return Plan{}, Message{}, ErrPlanNotFound
	}
	if p.Status == PlanClosed {
		return *p, Message{}, nil
	}
	unfinished := s.planUnfinishedTasksLocked(p)
	if len(unfinished) > 0 {
		if !force || reason == "" {
			return Plan{}, Message{}, ErrPlanHasActiveWork
		}
	}
	now := time.Now()
	p.Status = PlanClosed
	p.ClosedAt = &now
	p.UpdatedAt = now
	s.persistPlanLocked(p)
	var card Message
	if len(unfinished) > 0 {
		card, _ = s.appendMessageLocked(Message{
			RoomID:   p.RoomID,
			Callsign: actor,
			To:       "everyone",
			Content:  fmt.Sprintf("plan %q force-closed by %s with %d unfinished task(s) [%s]: %s", p.Title, actor, len(unfinished), strings.Join(unfinished, ", "), reason),
			Kind:     "plan_force_closed",
		})
	}
	return *p, card, nil
}

// planUnfinishedTasksLocked returns the IDs of the plan's tasks that are not yet
// in a terminal state (accepted counts as unfinished until closed, matching the
// slot model where the coordinator still owes a close-out). Caller holds s.mu.
func (s *Store) planUnfinishedTasksLocked(p *Plan) []string {
	out := make([]string, 0)
	for _, tid := range p.TaskIDs {
		t, ok := s.tasks[tid]
		if !ok {
			continue
		}
		if !t.State.Terminal() {
			out = append(out, tid)
		}
	}
	return out
}

// ClaimNextTask atomically claims the first eligible ready task in the plan's
// order on behalf of actor and returns the claimed task plus its card. A task
// is eligible when it is Ready, its dependencies are satisfied, it is not
// reserved for a different callsign, and the plan has a free concurrency slot.
// When caps is non-empty, tasks whose required capabilities the builder does
// not cover are skipped (a task with no capability tags matches any builder).
// Returns ErrNoEligibleTask when nothing can be claimed.
func (s *Store) ClaimNextTask(planID, actor string, caps []string) (Task, Message, error) {
	s.mu.RLock()
	p, ok := s.plans[planID]
	if !ok {
		s.mu.RUnlock()
		return Task{}, Message{}, ErrPlanNotFound
	}
	if p.Status == PlanClosed {
		s.mu.RUnlock()
		return Task{}, Message{}, ErrPlanClosed
	}
	if s.planActiveCountLocked(p.ID) >= p.effectiveMaxConcurrent() {
		s.mu.RUnlock()
		return Task{}, Message{}, ErrConcurrencyFull
	}
	var target string
	for _, tid := range p.TaskIDs {
		t, ok := s.tasks[tid]
		if !ok || t.State != TaskReady {
			continue
		}
		if t.Assignee != "" && t.Assignee != actor {
			continue
		}
		if !capabilitiesCovered(t.Capabilities, caps) {
			continue
		}
		if len(s.depsPendingLocked(t)) > 0 {
			continue
		}
		target = tid
		break
	}
	s.mu.RUnlock()
	if target == "" {
		return Task{}, Message{}, ErrNoEligibleTask
	}
	// Claim through the normal transition so all bookkeeping and the card
	// emission stay in one place. The re-check under the write lock keeps the
	// claim atomic even if another builder raced us to the same task.
	return s.TransitionTask(target, "claim", actor, TransitionInput{})
}

// PlanTasks returns the plan's tasks in the planner's intended order (the plan's
// TaskIDs), decorated with the computed read-time fields. Any task ID that no
// longer resolves (e.g. hard-deleted) is skipped.
func (s *Store) PlanTasks(planID string) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.plans[planID]
	if !ok {
		return nil, ErrPlanNotFound
	}
	out := make([]Task, 0, len(p.TaskIDs))
	for _, tid := range p.TaskIDs {
		if t, ok := s.tasks[tid]; ok {
			out = append(out, s.decorateTaskLocked(t))
		}
	}
	return out, nil
}

// EligibleTasks returns the plan's tasks that actor could claim right now, in
// plan order. A task is eligible when it is Ready, its dependencies are
// satisfied, and it is not reserved for a different callsign. When the plan is
// already at its concurrency cap nothing is eligible, so an empty list is
// returned. actor may be empty to list tasks eligible for any open builder
// (reserved tasks are then excluded). When caps is non-empty, tasks whose
// required capabilities the builder does not cover are excluded (a task with no
// capability tags matches any builder).
func (s *Store) EligibleTasks(planID, actor string, caps []string) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.plans[planID]
	if !ok {
		return nil, ErrPlanNotFound
	}
	out := make([]Task, 0, len(p.TaskIDs))
	if p.Status == PlanClosed || s.planActiveCountLocked(p.ID) >= p.effectiveMaxConcurrent() {
		return out, nil
	}
	for _, tid := range p.TaskIDs {
		t, ok := s.tasks[tid]
		if !ok || t.State != TaskReady {
			continue
		}
		if t.Assignee != "" && actor != "" && t.Assignee != actor {
			continue
		}
		if t.Assignee != "" && actor == "" {
			continue
		}
		if !capabilitiesCovered(t.Capabilities, caps) {
			continue
		}
		if len(s.depsPendingLocked(t)) > 0 {
			continue
		}
		out = append(out, s.decorateTaskLocked(t))
	}
	return out, nil
}

// capabilitiesCovered reports whether a builder with the given capabilities can
// take a task requiring taskCaps. A task with no required capabilities matches
// any builder. When the builder declares no capabilities (builderCaps empty),
// capability filtering is skipped entirely so behaviour matches the pre-tag
// model. Matching is case-insensitive.
func capabilitiesCovered(taskCaps, builderCaps []string) bool {
	if len(taskCaps) == 0 || len(builderCaps) == 0 {
		return true
	}
	have := make(map[string]bool, len(builderCaps))
	for _, c := range builderCaps {
		have[strings.ToLower(strings.TrimSpace(c))] = true
	}
	for _, need := range taskCaps {
		if !have[strings.ToLower(strings.TrimSpace(need))] {
			return false
		}
	}
	return true
}

// planGateLocked enforces the plan-managed queue rules for a transition that has
// already been validated by nextTaskState. It runs only for tasks bound to a
// plan. Caller holds s.mu (write). overrideApplied is set true when a valid
// planner override bypassed the gates (so the caller can emit an audit card).
func (s *Store) planGateLocked(t *Task, action, actor string, in TransitionInput, overrideApplied *bool) error {
	plan := s.plans[t.PlanID]
	switch action {
	case "promote", "defer", "reinstate":
		// Only the plan's planner may declare a task queue-eligible, park it, or
		// bring it back. When no planner was recorded, fall back to the
		// cooperative model (anyone).
		if plan != nil && plan.Planner != "" && actor != plan.Planner {
			return ErrOverrideForbidden
		}
	case "release":
		// A claimed task may be released by its current execution owner or by
		// the plan's planner (e.g. reclaiming a stale claim). Anyone else is
		// refused.
		if plan != nil && plan.Planner != "" && actor != plan.Planner && actor != t.ClaimedBy {
			return ErrOverrideForbidden
		}
	case "claim":
		// A reserved task may only be claimed by its intended assignee (or via a
		// planner override).
		if t.Assignee != "" && t.Assignee != actor && !in.Override {
			return ErrClaimReserved
		}
		if in.Override {
			if err := s.checkOverrideLocked(plan, actor, in); err != nil {
				return err
			}
			*overrideApplied = true
			return nil
		}
		if len(s.depsPendingLocked(t)) > 0 {
			return ErrDependenciesPending
		}
		if plan != nil && s.planActiveCountLocked(plan.ID) >= plan.effectiveMaxConcurrent() {
			return ErrConcurrencyFull
		}
	case "start":
		if in.Override {
			if err := s.checkOverrideLocked(plan, actor, in); err != nil {
				return err
			}
			*overrideApplied = true
			return nil
		}
		if len(s.depsPendingLocked(t)) > 0 {
			return ErrDependenciesPending
		}
	}
	return nil
}

// checkOverrideLocked validates that an override request is legitimate: the
// actor must be the plan's recorded planner and must supply a reason.
func (s *Store) checkOverrideLocked(plan *Plan, actor string, in TransitionInput) error {
	if plan == nil || plan.Planner == "" || actor != plan.Planner || in.OverrideReason == "" {
		return ErrOverrideForbidden
	}
	return nil
}

// planActiveCountLocked counts how many of a plan's tasks currently occupy a
// concurrency slot (are actively being worked). Caller holds s.mu.
func (s *Store) planActiveCountLocked(planID string) int {
	n := 0
	for _, t := range s.tasks {
		if t.PlanID == planID && t.State.OccupiesSlot() {
			n++
		}
	}
	return n
}

// sameMembers reports whether a and b contain exactly the same elements (as a
// multiset), used to validate that a reorder neither adds nor drops tasks.
func sameMembers(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
		if counts[v] < 0 {
			return false
		}
	}
	return true
}

// sameOrder reports whether a and b are identical sequences (same elements in
// the same order), used to detect a no-op revision of an ordered string field.
func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sortByCreatedAt orders plans oldest-first by creation time (stable enough for
// the small plan counts BotChat holds).
func sortByCreatedAt(plans []Plan) {
	for i := 1; i < len(plans); i++ {
		for j := i; j > 0 && plans[j].CreatedAt.Before(plans[j-1].CreatedAt); j-- {
			plans[j], plans[j-1] = plans[j-1], plans[j]
		}
	}
}
