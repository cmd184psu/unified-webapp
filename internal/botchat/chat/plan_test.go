package chat

import (
	"errors"
	"strings"
	"testing"
)

func TestCreatePlanAndAddDraftTask(t *testing.T) {
	s := NewStore()
	plan, err := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "ship widget", Planner: "coord"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if plan.Status != PlanActive || plan.Revision != 1 {
		t.Fatalf("new plan status=%q revision=%d, want active/1", plan.Status, plan.Revision)
	}
	task, card, err := s.CreateTask(Task{PlanID: plan.ID, Title: "build", Creator: "coord"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.State != TaskDraft {
		t.Fatalf("plan task state=%q, want draft", task.State)
	}
	if task.RoomID != globalRoomID {
		t.Fatalf("task room=%q, want inherited %q", task.RoomID, globalRoomID)
	}
	if card.Kind != "task_created" {
		t.Fatalf("card kind=%q, want task_created", card.Kind)
	}
	got, _ := s.GetPlan(plan.ID)
	if len(got.TaskIDs) != 1 || got.TaskIDs[0] != task.ID {
		t.Fatalf("plan taskIds=%v, want [%s]", got.TaskIDs, task.ID)
	}
}

func TestCreateTaskInMissingPlanFails(t *testing.T) {
	s := NewStore()
	if _, _, err := s.CreateTask(Task{PlanID: "plan_nope", Title: "x", Creator: "c"}); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("want ErrPlanNotFound, got %v", err)
	}
}

func TestCreateTaskInClosedPlanFails(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	if _, _, err := s.ClosePlan(plan.ID, "coord", false, ""); err != nil {
		t.Fatalf("ClosePlan: %v", err)
	}
	if _, _, err := s.CreateTask(Task{PlanID: plan.ID, Title: "x", Creator: "c"}); !errors.Is(err, ErrPlanClosed) {
		t.Fatalf("want ErrPlanClosed, got %v", err)
	}
}

func TestPromoteThenClaim(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "build", Creator: "coord"})

	// Cannot claim while still a draft.
	if _, _, err := s.TransitionTask(task.ID, "claim", "pum-1", TransitionInput{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("claim of draft: want ErrInvalidTransition, got %v", err)
	}
	ready, _, err := s.TransitionTask(task.ID, "promote", "coord", TransitionInput{})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if ready.State != TaskReady {
		t.Fatalf("state after promote=%q, want ready", ready.State)
	}
	claimed, card, err := s.TransitionTask(task.ID, "claim", "pum-1", TransitionInput{})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.State != TaskAcknowledged {
		t.Fatalf("state after claim=%q, want acknowledged", claimed.State)
	}
	if claimed.ClaimedBy != "pum-1" || claimed.ClaimedAt == nil {
		t.Fatalf("claim did not record claimedBy/claimedAt: %+v", claimed)
	}
	if claimed.Assignee != "pum-1" {
		t.Fatalf("unassigned task should adopt claimant as assignee, got %q", claimed.Assignee)
	}
	if card.Kind != "task_claimed" {
		t.Fatalf("card kind=%q, want task_claimed", card.Kind)
	}
}

func TestPromoteRequiresPlanner(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "build", Creator: "coord"})
	if _, _, err := s.TransitionTask(task.ID, "promote", "someone-else", TransitionInput{}); !errors.Is(err, ErrOverrideForbidden) {
		t.Fatalf("promote by non-planner: want ErrOverrideForbidden, got %v", err)
	}
}

func TestReservedClaimRejectsOtherCallsign(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "build", Assignee: "pum-1", Creator: "coord"})
	_, _, _ = s.TransitionTask(task.ID, "promote", "coord", TransitionInput{})

	if _, _, err := s.TransitionTask(task.ID, "claim", "pum-2", TransitionInput{}); !errors.Is(err, ErrClaimReserved) {
		t.Fatalf("reserved claim by other: want ErrClaimReserved, got %v", err)
	}
	if _, _, err := s.TransitionTask(task.ID, "claim", "pum-1", TransitionInput{}); err != nil {
		t.Fatalf("reserved claim by intended assignee: %v", err)
	}
}

func TestDependencyGateBlocksClaim(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	dep, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "first", Creator: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "second", Creator: "coord", DependsOn: []string{dep.ID}})
	_, _, _ = s.TransitionTask(task.ID, "promote", "coord", TransitionInput{})

	if _, _, err := s.TransitionTask(task.ID, "claim", "pum-1", TransitionInput{}); !errors.Is(err, ErrDependenciesPending) {
		t.Fatalf("claim with pending dep: want ErrDependenciesPending, got %v", err)
	}

	// Drive the dependency to accepted, then the claim should succeed.
	_, _, _ = s.TransitionTask(dep.ID, "promote", "coord", TransitionInput{})
	_, _, _ = s.TransitionTask(dep.ID, "claim", "pum-9", TransitionInput{})
	_, _, _ = s.TransitionTask(dep.ID, "start", "pum-9", TransitionInput{})
	_, _, _ = s.TransitionTask(dep.ID, "done", "pum-9", TransitionInput{})
	_, _, _ = s.TransitionTask(dep.ID, "accept", "coord", TransitionInput{})
	if _, _, err := s.TransitionTask(task.ID, "claim", "pum-1", TransitionInput{}); err != nil {
		t.Fatalf("claim after dep accepted: %v", err)
	}
}

func TestConcurrencyGate(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"}) // default max 1
	a, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "a", Creator: "coord"})
	b, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "b", Creator: "coord"})
	_, _, _ = s.TransitionTask(a.ID, "promote", "coord", TransitionInput{})
	_, _, _ = s.TransitionTask(b.ID, "promote", "coord", TransitionInput{})

	if _, _, err := s.TransitionTask(a.ID, "claim", "pum-1", TransitionInput{}); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, _, err := s.TransitionTask(b.ID, "claim", "pum-2", TransitionInput{}); !errors.Is(err, ErrConcurrencyFull) {
		t.Fatalf("second claim over cap: want ErrConcurrencyFull, got %v", err)
	}

	// Freeing the first (accept/close) opens the slot.
	_, _, _ = s.TransitionTask(a.ID, "start", "pum-1", TransitionInput{})
	_, _, _ = s.TransitionTask(a.ID, "done", "pum-1", TransitionInput{})
	_, _, _ = s.TransitionTask(a.ID, "accept", "coord", TransitionInput{})
	if _, _, err := s.TransitionTask(b.ID, "claim", "pum-2", TransitionInput{}); err != nil {
		t.Fatalf("claim after slot freed: %v", err)
	}
}

func TestPlannerOverrideBypassesGates(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	dep, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "first", Creator: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "second", Creator: "coord", DependsOn: []string{dep.ID}})
	_, _, _ = s.TransitionTask(task.ID, "promote", "coord", TransitionInput{})

	// Override without a reason (or by a non-planner) is forbidden.
	if _, _, err := s.TransitionTask(task.ID, "claim", "coord", TransitionInput{Override: true}); !errors.Is(err, ErrOverrideForbidden) {
		t.Fatalf("override without reason: want ErrOverrideForbidden, got %v", err)
	}
	if _, _, err := s.TransitionTask(task.ID, "claim", "pum-1", TransitionInput{Override: true, OverrideReason: "rush"}); !errors.Is(err, ErrOverrideForbidden) {
		t.Fatalf("override by non-planner: want ErrOverrideForbidden, got %v", err)
	}
	// Planner override with a reason bypasses the dependency gate.
	claimed, _, err := s.TransitionTask(task.ID, "claim", "coord", TransitionInput{Override: true, OverrideReason: "rush"})
	if err != nil {
		t.Fatalf("planner override claim: %v", err)
	}
	if claimed.State != TaskAcknowledged {
		t.Fatalf("state after override claim=%q, want acknowledged", claimed.State)
	}
	// The override left an audit card in the transcript.
	msgs, _ := s.GetMessages(globalRoomID)
	found := false
	for _, m := range msgs {
		if m.Kind == "task_override" && m.TaskID == task.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a task_override audit card in the transcript")
	}
}

func TestClaimNextTaskPicksFirstEligible(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord", MaxConcurrentActive: 2})
	a, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "a", Creator: "coord"})
	b, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "b", Creator: "coord"})
	// Only promote b (a stays draft), so ClaimNextTask must skip a and pick b.
	_, _, _ = s.TransitionTask(b.ID, "promote", "coord", TransitionInput{})

	got, _, err := s.ClaimNextTask(plan.ID, "pum-1", nil)
	if err != nil {
		t.Fatalf("ClaimNextTask: %v", err)
	}
	if got.ID != b.ID {
		t.Fatalf("claimed %s, want first eligible %s", got.ID, b.ID)
	}
	// Nothing else eligible now (a is draft) → ErrNoEligibleTask.
	if _, _, err := s.ClaimNextTask(plan.ID, "pum-2", nil); !errors.Is(err, ErrNoEligibleTask) {
		t.Fatalf("second ClaimNextTask: want ErrNoEligibleTask, got %v", err)
	}
	_ = a
}

func TestRevisePlanReorderAndBumpsRevision(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	a, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "a", Creator: "coord"})
	b, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "b", Creator: "coord"})

	revised, err := s.RevisePlan(plan.ID, PlanRevision{TaskIDs: []string{b.ID, a.ID}})
	if err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	if revised.Revision != 2 {
		t.Fatalf("revision=%d, want 2", revised.Revision)
	}
	if revised.TaskIDs[0] != b.ID || revised.TaskIDs[1] != a.ID {
		t.Fatalf("taskIds=%v, want reordered [%s %s]", revised.TaskIDs, b.ID, a.ID)
	}
	// A reorder that is not a permutation is rejected.
	if _, err := s.RevisePlan(plan.ID, PlanRevision{TaskIDs: []string{a.ID}}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("non-permutation reorder: want ErrInvalidTransition, got %v", err)
	}
}

func TestPlanTasksReturnsInPlanOrder(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	a, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "a", Creator: "coord"})
	b, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "b", Creator: "coord"})
	if _, err := s.RevisePlan(plan.ID, PlanRevision{TaskIDs: []string{b.ID, a.ID}}); err != nil {
		t.Fatalf("RevisePlan: %v", err)
	}
	tasks, err := s.PlanTasks(plan.ID)
	if err != nil {
		t.Fatalf("PlanTasks: %v", err)
	}
	if len(tasks) != 2 || tasks[0].ID != b.ID || tasks[1].ID != a.ID {
		t.Fatalf("PlanTasks order=%v, want [%s %s]", tasks, b.ID, a.ID)
	}
	if _, err := s.PlanTasks("plan_nope"); !errors.Is(err, ErrPlanNotFound) {
		t.Fatalf("PlanTasks(missing): want ErrPlanNotFound, got %v", err)
	}
}

func TestEligibleTasksHonoursGates(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	a, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "a", Creator: "coord"})
	b, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "b", Creator: "coord", DependsOn: []string{a.ID}})
	reserved, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "r", Creator: "coord", Assignee: "pum-owner"})

	// Nothing eligible while all are drafts.
	if el, _ := s.EligibleTasks(plan.ID, "pum-1", nil); len(el) != 0 {
		t.Fatalf("drafts eligible=%v, want none", el)
	}
	// Promote all three.
	for _, id := range []string{a.ID, b.ID, reserved.ID} {
		if _, _, err := s.TransitionTask(id, "promote", "coord", TransitionInput{}); err != nil {
			t.Fatalf("promote %s: %v", id, err)
		}
	}
	// For an open builder: a is eligible (no deps), b is not (dep a pending),
	// reserved is not (reserved for pum-owner).
	el, err := s.EligibleTasks(plan.ID, "pum-1", nil)
	if err != nil {
		t.Fatalf("EligibleTasks: %v", err)
	}
	if len(el) != 1 || el[0].ID != a.ID {
		t.Fatalf("eligible for pum-1=%v, want [%s]", el, a.ID)
	}
	// The reserved owner additionally sees their reserved task (a is still open).
	el, _ = s.EligibleTasks(plan.ID, "pum-owner", nil)
	ids := map[string]bool{}
	for _, tk := range el {
		ids[tk.ID] = true
	}
	if !ids[a.ID] || !ids[reserved.ID] || ids[b.ID] {
		t.Fatalf("eligible for pum-owner=%v, want a+reserved (not b)", el)
	}
	// Claiming a fills the single slot (default cap 1) → nothing else eligible.
	if _, _, err := s.TransitionTask(a.ID, "claim", "pum-1", TransitionInput{}); err != nil {
		t.Fatalf("claim a: %v", err)
	}
	if el, _ := s.EligibleTasks(plan.ID, "pum-owner", nil); len(el) != 0 {
		t.Fatalf("after slot filled eligible=%v, want none", el)
	}
}

func TestCapabilitiesCovered(t *testing.T) {
	cases := []struct {
		name        string
		taskCaps    []string
		builderCaps []string
		want        bool
	}{
		{"no task tags matches anyone", nil, []string{"go"}, true},
		{"no builder tags skips filter", []string{"go"}, nil, true},
		{"builder has all", []string{"go", "deploy"}, []string{"deploy", "go", "frontend"}, true},
		{"builder missing one", []string{"go", "deploy"}, []string{"go"}, false},
		{"case insensitive", []string{"Go"}, []string{"go"}, true},
	}
	for _, tc := range cases {
		if got := capabilitiesCovered(tc.taskCaps, tc.builderCaps); got != tc.want {
			t.Errorf("%s: capabilitiesCovered(%v,%v)=%v, want %v", tc.name, tc.taskCaps, tc.builderCaps, got, tc.want)
		}
	}
}

func TestEligibleTasksFiltersByCapability(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord", MaxConcurrentActive: 5})
	fe, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "fe", Creator: "coord", Capabilities: []string{"frontend"}})
	be, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "be", Creator: "coord", Capabilities: []string{"go"}})
	for _, id := range []string{fe.ID, be.ID} {
		if _, _, err := s.TransitionTask(id, "promote", "coord", TransitionInput{}); err != nil {
			t.Fatalf("promote %s: %v", id, err)
		}
	}
	// A builder declaring only "go" sees the backend task, not the frontend one.
	el, err := s.EligibleTasks(plan.ID, "pum-1", []string{"go"})
	if err != nil {
		t.Fatalf("EligibleTasks: %v", err)
	}
	if len(el) != 1 || el[0].ID != be.ID {
		t.Fatalf("eligible for go builder=%v, want [%s]", el, be.ID)
	}
	// No declared capabilities → filter skipped, both eligible.
	if el, _ := s.EligibleTasks(plan.ID, "pum-1", nil); len(el) != 2 {
		t.Fatalf("eligible without caps=%v, want both", el)
	}
	// ClaimNextTask respects the capability filter (skips fe, claims be).
	got, _, err := s.ClaimNextTask(plan.ID, "pum-1", []string{"go"})
	if err != nil {
		t.Fatalf("ClaimNextTask: %v", err)
	}
	if got.ID != be.ID {
		t.Fatalf("claimed %s, want %s", got.ID, be.ID)
	}
}

func TestTasksAwaitingReview(t *testing.T) {
	s := NewStore()
	// A task created and driven to Done by "coord" awaits coord's review.
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "t", Creator: "coord", Assignee: "pum-1"})
	for _, action := range []string{"acknowledge", "start", "done"} {
		if _, _, err := s.TransitionTask(task.ID, action, "pum-1", TransitionInput{}); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	out := s.TasksAwaitingReview("coord")
	if len(out) != 1 || out[0].ID != task.ID {
		t.Fatalf("awaiting review for coord=%v, want [%s]", out, task.ID)
	}
	// The assignee is not the creator, so nothing awaits their review.
	if out := s.TasksAwaitingReview("pum-1"); len(out) != 0 {
		t.Fatalf("awaiting review for pum-1=%v, want none", out)
	}
	// Empty callsign yields an empty (non-nil) slice.
	if out := s.TasksAwaitingReview(""); out == nil || len(out) != 0 {
		t.Fatalf("awaiting review for empty callsign=%v, want empty", out)
	}
}

func TestTasksBlockedOn(t *testing.T) {
	s := NewStore()
	// (1) An explicitly blocked task waiting on "coord".
	blk, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "blk", Creator: "coord", Assignee: "pum-1"})
	for _, action := range []string{"acknowledge", "start"} {
		if _, _, err := s.TransitionTask(blk.ID, action, "pum-1", TransitionInput{}); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if _, _, err := s.TransitionTask(blk.ID, "block", "pum-1", TransitionInput{WaitingOn: "coord"}); err != nil {
		t.Fatalf("block: %v", err)
	}
	// (2) A downstream task depending on coord's unfinished work.
	up, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "up", Creator: "coord", Assignee: "coord"})
	down, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "down", Creator: "coord", Assignee: "pum-2", DependsOn: []string{up.ID}})

	out := s.TasksBlockedOn("coord")
	ids := map[string]bool{}
	for _, tk := range out {
		ids[tk.ID] = true
	}
	if !ids[blk.ID] || !ids[down.ID] {
		t.Fatalf("blocked on coord=%v, want blk+down", out)
	}
	// Empty callsign yields an empty (non-nil) slice.
	if out := s.TasksBlockedOn(""); out == nil || len(out) != 0 {
		t.Fatalf("blocked on empty callsign=%v, want empty", out)
	}
	_ = up
}

func strptr(s string) *string { return &s }

func TestRevisePlanOptimisticConflict(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	if plan.Revision != 1 {
		t.Fatalf("new plan revision=%d, want 1", plan.Revision)
	}
	// A stale expectedRevision is rejected.
	if _, err := s.RevisePlan(plan.ID, PlanRevision{Title: strptr("x"), ExpectedRevision: 99}); !errors.Is(err, ErrPlanRevisionStale) {
		t.Fatalf("stale plan revise: want ErrPlanRevisionStale, got %v", err)
	}
	// The current revision succeeds and bumps the counter.
	got, err := s.RevisePlan(plan.ID, PlanRevision{Title: strptr("x"), ExpectedRevision: plan.Revision})
	if err != nil {
		t.Fatalf("revise at current revision: %v", err)
	}
	if got.Revision != 2 || got.Title != "x" {
		t.Fatalf("revised plan revision=%d title=%q, want 2/x", got.Revision, got.Title)
	}
}

func TestClosePlanRejectsUnfinishedWork(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "t", Creator: "coord"})
	// A plan with a non-terminal (draft) task cannot be cleanly closed.
	if _, _, err := s.ClosePlan(plan.ID, "coord", false, ""); !errors.Is(err, ErrPlanHasActiveWork) {
		t.Fatalf("clean close with unfinished work: want ErrPlanHasActiveWork, got %v", err)
	}
	// Force without a reason is still refused.
	if _, _, err := s.ClosePlan(plan.ID, "coord", true, ""); !errors.Is(err, ErrPlanHasActiveWork) {
		t.Fatalf("force close without reason: want ErrPlanHasActiveWork, got %v", err)
	}
	// Force with a reason succeeds and drops an audit card naming the task.
	got, card, err := s.ClosePlan(plan.ID, "coord", true, "abandoning scope")
	if err != nil {
		t.Fatalf("force close: %v", err)
	}
	if got.Status != PlanClosed {
		t.Fatalf("status after force close=%q, want closed", got.Status)
	}
	if card.Kind != "plan_force_closed" || !strings.Contains(card.Content, task.ID) {
		t.Fatalf("audit card kind=%q content=%q, want plan_force_closed naming %s", card.Kind, card.Content, task.ID)
	}
}

func TestClosePlanCleanWhenAllTerminal(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "t", Creator: "coord"})
	if _, _, err := s.TransitionTask(task.ID, "cancel", "coord", TransitionInput{}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	// With every task terminal, a clean close needs no force and emits no card.
	got, card, err := s.ClosePlan(plan.ID, "coord", false, "")
	if err != nil {
		t.Fatalf("clean close: %v", err)
	}
	if got.Status != PlanClosed {
		t.Fatalf("status=%q, want closed", got.Status)
	}
	if card.ID != "" {
		t.Fatalf("clean close should emit no audit card, got %+v", card)
	}
}

func TestReviseTaskAppliesDiffAndBumpsRevision(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "old", Creator: "coord", Assignee: "pum-1"})
	if task.SpecRevision != 1 {
		t.Fatalf("new task specRevision=%d, want 1", task.SpecRevision)
	}
	revised, card, err := s.ReviseTask(task.ID, "coord", TaskRevision{
		Title:        strptr("new"),
		Capabilities: []string{"go"},
		Note:         "sharper scope",
	})
	if err != nil {
		t.Fatalf("ReviseTask: %v", err)
	}
	if revised.Title != "new" || len(revised.Capabilities) != 1 || revised.Capabilities[0] != "go" {
		t.Fatalf("revised task=%+v, want title=new caps=[go]", revised)
	}
	if revised.SpecRevision != 2 || revised.Version != task.Version+1 {
		t.Fatalf("revised specRevision=%d version=%d, want 2/%d", revised.SpecRevision, revised.Version, task.Version+1)
	}
	if card.Kind != "task_revised" || card.To != "pum-1" {
		t.Fatalf("card kind=%q to=%q, want task_revised/pum-1", card.Kind, card.To)
	}
	// A no-op revision (nothing changed) is rejected.
	if _, _, err := s.ReviseTask(task.ID, "coord", TaskRevision{Title: strptr("new")}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("no-op revise: want ErrInvalidTransition, got %v", err)
	}
}

func TestReviseTaskOptimisticConflict(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "t", Creator: "coord"})
	if _, _, err := s.ReviseTask(task.ID, "coord", TaskRevision{Title: strptr("x"), ExpectedVersion: 999}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale revise: want ErrVersionConflict, got %v", err)
	}
	if _, _, err := s.ReviseTask(task.ID, "coord", TaskRevision{Title: strptr("x"), ExpectedVersion: task.Version}); err != nil {
		t.Fatalf("revise at current version: %v", err)
	}
}

func TestReviseTaskPlannerOnly(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "t", Creator: "coord"})
	if _, _, err := s.ReviseTask(task.ID, "pum-1", TaskRevision{Title: strptr("x")}); !errors.Is(err, ErrOverrideForbidden) {
		t.Fatalf("non-planner revise: want ErrOverrideForbidden, got %v", err)
	}
	if _, _, err := s.ReviseTask(task.ID, "coord", TaskRevision{Title: strptr("x")}); err != nil {
		t.Fatalf("planner revise: %v", err)
	}
}

func TestReviseTaskTerminalRejected(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "t", Creator: "coord"})
	if _, _, err := s.TransitionTask(task.ID, "cancel", "coord", TransitionInput{}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, _, err := s.ReviseTask(task.ID, "coord", TaskRevision{Title: strptr("x")}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("revise terminal: want ErrInvalidTransition, got %v", err)
	}
}

func TestRevisionMustBeAckedBeforeDone(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "t", Creator: "coord", Assignee: "pum-1"})
	for _, action := range []string{"acknowledge", "start"} {
		if _, _, err := s.TransitionTask(task.ID, action, "pum-1", TransitionInput{}); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	// Planner revises the spec after the builder is working on it.
	revised, _, err := s.ReviseTask(task.ID, "coord", TaskRevision{Objective: strptr("do more")})
	if err != nil {
		t.Fatalf("ReviseTask: %v", err)
	}
	if !revised.RevisionPending {
		t.Fatalf("revised task should have RevisionPending set")
	}
	// The builder cannot complete until it acknowledges the new revision.
	if _, _, err := s.TransitionTask(task.ID, "done", "pum-1", TransitionInput{}); !errors.Is(err, ErrRevisionPending) {
		t.Fatalf("done with pending revision: want ErrRevisionPending, got %v", err)
	}
	acked, card, err := s.TransitionTask(task.ID, "ack_revision", "pum-1", TransitionInput{})
	if err != nil {
		t.Fatalf("ack_revision: %v", err)
	}
	if acked.State != TaskWorking || acked.RevisionPending {
		t.Fatalf("after ack: state=%q pending=%v, want working/false", acked.State, acked.RevisionPending)
	}
	if card.Kind != "task_revision_acked" {
		t.Fatalf("ack card kind=%q, want task_revision_acked", card.Kind)
	}
	// A second ack with nothing pending is rejected.
	if _, _, err := s.TransitionTask(task.ID, "ack_revision", "pum-1", TransitionInput{}); !errors.Is(err, ErrNoRevisionPending) {
		t.Fatalf("redundant ack: want ErrNoRevisionPending, got %v", err)
	}
	// Now DONE succeeds.
	if _, _, err := s.TransitionTask(task.ID, "done", "pum-1", TransitionInput{}); err != nil {
		t.Fatalf("done after ack: %v", err)
	}
}

// claimPlanTask promotes a fresh draft plan task and claims it for builder,
// returning the claimed task. Fails the test on any transition error.
func claimPlanTask(t *testing.T, s *Store, planID, builder string) Task {
	t.Helper()
	task, _, err := s.CreateTask(Task{PlanID: planID, Title: "t", Creator: "coord"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, _, err := s.TransitionTask(task.ID, "promote", "coord", TransitionInput{}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	claimed, _, err := s.TransitionTask(task.ID, "claim", builder, TransitionInput{})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return claimed
}

func TestReleaseReturnsTaskToQueue(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	claimed := claimPlanTask(t, s, plan.ID, "pum-1")
	if claimed.State != TaskAcknowledged || claimed.ClaimedBy != "pum-1" || claimed.Assignee != "pum-1" {
		t.Fatalf("after claim state=%q claimedBy=%q assignee=%q", claimed.State, claimed.ClaimedBy, claimed.Assignee)
	}
	released, card, err := s.TransitionTask(claimed.ID, "release", "pum-1", TransitionInput{})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.State != TaskReady {
		t.Fatalf("released state=%q, want ready", released.State)
	}
	if released.ClaimedBy != "" || released.Assignee != "" {
		t.Fatalf("release should clear ownership: claimedBy=%q assignee=%q", released.ClaimedBy, released.Assignee)
	}
	if card.Kind != "task_released" {
		t.Fatalf("card kind=%q, want task_released", card.Kind)
	}
	// The freed task is claimable again by a different builder.
	if _, _, err := s.TransitionTask(released.ID, "claim", "pum-2", TransitionInput{}); err != nil {
		t.Fatalf("reclaim after release: %v", err)
	}
}

func TestReleaseOnlyByOwnerOrPlanner(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	claimed := claimPlanTask(t, s, plan.ID, "pum-1")
	// A bystander (not the owner, not the planner) cannot release it.
	if _, _, err := s.TransitionTask(claimed.ID, "release", "pum-2", TransitionInput{}); !errors.Is(err, ErrOverrideForbidden) {
		t.Fatalf("bystander release: want ErrOverrideForbidden, got %v", err)
	}
	// The planner may release someone else's claim (e.g. a stale claim).
	if _, _, err := s.TransitionTask(claimed.ID, "release", "coord", TransitionInput{}); err != nil {
		t.Fatalf("planner release: %v", err)
	}
}

func TestDeferAndReinstate(t *testing.T) {
	s := NewStore()
	plan, _ := s.CreatePlan(Plan{RoomID: globalRoomID, Title: "p", Planner: "coord"})
	task, _, _ := s.CreateTask(Task{PlanID: plan.ID, Title: "t", Creator: "coord", Assignee: "pum-1"})
	if _, _, err := s.TransitionTask(task.ID, "promote", "coord", TransitionInput{}); err != nil {
		t.Fatalf("promote: %v", err)
	}
	// A non-planner cannot defer a plan task.
	if _, _, err := s.TransitionTask(task.ID, "defer", "pum-1", TransitionInput{}); !errors.Is(err, ErrOverrideForbidden) {
		t.Fatalf("non-planner defer: want ErrOverrideForbidden, got %v", err)
	}
	deferred, card, err := s.TransitionTask(task.ID, "defer", "coord", TransitionInput{})
	if err != nil {
		t.Fatalf("defer: %v", err)
	}
	if deferred.State != TaskDeferred || card.Kind != "task_deferred" {
		t.Fatalf("defer state=%q card=%q, want deferred/task_deferred", deferred.State, card.Kind)
	}
	// Deferring preserves the reservation so it can be reinstated as it was.
	if deferred.Assignee != "pum-1" {
		t.Fatalf("defer should preserve reservation, assignee=%q", deferred.Assignee)
	}
	if deferred.State.Terminal() {
		t.Fatalf("deferred must not be terminal")
	}
	// A deferred task is not eligible to claim and blocks a clean close.
	elig, _ := s.EligibleTasks(plan.ID, "", nil)
	if len(elig) != 0 {
		t.Fatalf("deferred task should not be eligible, got %d", len(elig))
	}
	if _, _, err := s.ClosePlan(plan.ID, "coord", false, ""); !errors.Is(err, ErrPlanHasActiveWork) {
		t.Fatalf("close with deferred work: want ErrPlanHasActiveWork, got %v", err)
	}
	// Reinstate brings it back into the claimable queue.
	reinstated, card, err := s.TransitionTask(task.ID, "reinstate", "coord", TransitionInput{})
	if err != nil {
		t.Fatalf("reinstate: %v", err)
	}
	if reinstated.State != TaskReady || card.Kind != "task_reinstated" {
		t.Fatalf("reinstate state=%q card=%q, want ready/task_reinstated", reinstated.State, card.Kind)
	}
}

func TestReleaseRoomOnlyTaskGoesToAssigned(t *testing.T) {
	s := NewStore()
	task, _, _ := s.CreateTask(Task{RoomID: globalRoomID, Title: "t", Creator: "coord", Assignee: "pum-1"})
	if _, _, err := s.TransitionTask(task.ID, "acknowledge", "pum-1", TransitionInput{}); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	// A room-only task has no queue, so release falls back to assigned.
	released, _, err := s.TransitionTask(task.ID, "release", "pum-1", TransitionInput{})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released.State != TaskAssigned {
		t.Fatalf("room-only release state=%q, want assigned", released.State)
	}
}
