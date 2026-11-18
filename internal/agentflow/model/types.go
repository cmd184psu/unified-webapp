// Package model holds agentflow's persistent value types and the pure rules
// over them (queue validation and hashing, derived task status). It has no
// I/O and does not import the store.
package model

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Effort statuses (state_model.effort_statuses). An effort in complete or
// abandoned is finished; every other status is active (PJ4).
const (
	EffortDraft        = "draft"
	EffortPlanning     = "planning"
	EffortPlanReady    = "plan_ready"
	EffortPlanApproved = "plan_approved"
	EffortCompiling    = "compiling"
	EffortQueueDraft   = "queue_draft"
	EffortQueueAppr    = "queue_approved"
	EffortRunning      = "running"
	EffortPaused       = "paused"
	EffortComplete     = "complete"
	EffortAbandoned    = "abandoned"
)

// EffortStatuses lists every effort status in lifecycle order.
var EffortStatuses = []string{
	EffortDraft, EffortPlanning, EffortPlanReady, EffortPlanApproved, EffortCompiling,
	EffortQueueDraft, EffortQueueAppr, EffortRunning, EffortPaused, EffortComplete, EffortAbandoned,
}

// Run states (state_model.run_states). Persisted on an effort for display only.
const (
	RunStopped = "stopped"
	RunRunning = "running"
	RunPausing = "pausing"
	RunPaused  = "paused"
)

// RunStates lists every run state.
var RunStates = []string{RunStopped, RunRunning, RunPausing, RunPaused}

// IsActiveEffortStatus reports whether status counts toward "one active
// effort per project".
func IsActiveEffortStatus(status string) bool {
	return status != EffortComplete && status != EffortAbandoned
}

// Document kinds.
const (
	DocFRD    = "frd"
	DocPlan   = "plan"
	DocReview = "review"
)

// Queue statuses.
const (
	QueueDraft    = "draft"
	QueueApproved = "approved"
)

// Attempt kinds and outcomes. An empty outcome means the attempt is in flight.
const (
	AttemptNormal = "normal"
	AttemptRepair = "repair"

	OutcomePassed          = "passed"           // verified, reviewed and merged
	OutcomeFailed          = "failed"           // failed verify or review; a repair may follow
	OutcomeBlocked         = "blocked"          // worker or reviewer needs a human
	OutcomePolicyViolation = "policy_violation" // the worker broke a policy rule
	OutcomeAbandoned       = "abandoned"        // closed without a verdict (stop, cancel, restart, reset); never counts as an attempt
)

// Stage states and roles.
const (
	StageQueued  = "queued"
	StageRunning = "running"
	StageDone    = "done"
)

// StageRoles lists the roles a stage can have.
var StageRoles = []string{"worker", "reviewer", "planner", "architect", "critic", "compiler", "verify", "merge", "cleanup"}

// Effort is one unit of work on a project: FRD, plan, queue and run.
type Effort struct {
	ID                int64  `json:"id"`
	Project           string `json:"project"`
	Title             string `json:"title"`
	Status            string `json:"status"`
	RunState          string `json:"run_state"`
	IntegrationBranch string `json:"integration_branch"`
	BaseCommit        string `json:"base_commit"`
	YOLO              bool   `json:"yolo"`
	Deliberate        bool   `json:"deliberate"`
	// BudgetTokens and BudgetSeconds cap the effort's runner tokens and stage
	// run time (AT12); 0 means no limit.
	BudgetTokens  int64 `json:"budget_tokens"`
	BudgetSeconds int64 `json:"budget_seconds"`
	Created       int64 `json:"created"`
	Updated       int64 `json:"updated"`
}

// Doc is one immutable revision of an effort's FRD, plan or review.
type Doc struct {
	Effort    int64           `json:"effort"`
	Kind      string          `json:"kind"`
	Revision  int             `json:"revision"`
	JSON      json.RawMessage `json:"json"`
	CreatedBy string          `json:"created_by"`
	Created   int64           `json:"created"`
}

// Approval records that a document revision was approved.
type Approval struct {
	Effort     int64  `json:"effort"`
	Kind       string `json:"kind"`
	Revision   int    `json:"revision"`
	ApprovedBy string `json:"approved_by"`
	At         int64  `json:"at"`
}

// TaskDef is a task's definition. It carries no executable content (V1).
type TaskDef struct {
	Key          string   `json:"key"`
	Phase        string   `json:"phase"`
	Title        string   `json:"title"`
	Prompt       string   `json:"prompt"`
	Acceptance   []string `json:"acceptance"`
	FilesTouched []string `json:"files_touched"`
	DependsOn    []string `json:"depends_on"`
}

// Queue is an effort's task queue. Hash covers the task definitions.
type Queue struct {
	Effort     int64     `json:"effort"`
	Revision   int       `json:"revision"`
	Hash       string    `json:"hash"`
	Status     string    `json:"status"`
	ApprovedBy string    `json:"approved_by,omitempty"`
	Tasks      []TaskDef `json:"tasks"`
}

// Task is a stored task: its definition plus the mutable waiting reason. Its
// status is never stored; see DeriveTaskStatus.
type Task struct {
	ID    int64 `json:"id"`
	Queue int64 `json:"queue"`
	TaskDef
	WaitingReason string `json:"waiting_reason"`
}

// Attempt is one worker/verify/review chain on a task.
type Attempt struct {
	ID            int64  `json:"id"`
	Task          int64  `json:"task"`
	N             int    `json:"n"`
	Kind          string `json:"kind"`
	Worktree      string `json:"worktree"`
	Outcome       string `json:"outcome"`
	Fingerprint   string `json:"fingerprint"`
	WorkerModel   string `json:"worker_model"`
	ReviewerModel string `json:"reviewer_model"`
	// ReviewIndependence is how independent the reviewer was of the worker: different_family, same_family or same_model; empty when no reviewer ran.
	ReviewIndependence string `json:"review_independence,omitempty"`
	Started            int64  `json:"started"`
	Finished           int64  `json:"finished"`
}

// Stage is one runner or engine step. It belongs to an attempt, or to an
// effort (plan, review and compile stages), never both.
type Stage struct {
	ID        int64           `json:"id"`
	Attempt   int64           `json:"attempt"` // 0 when the stage belongs to an effort
	Effort    int64           `json:"effort"`  // 0 when the stage belongs to an attempt
	Role      string          `json:"role"`
	Backend   string          `json:"backend"`
	Model     string          `json:"model"`
	LaneJobID string          `json:"lane_job_id"`
	QueueHash string          `json:"queue_hash"`
	State     string          `json:"state"`
	Outcome   string          `json:"outcome"`
	RawOutput string          `json:"raw_output"`
	Parsed    json.RawMessage `json:"parsed"`
	PromptRef string          `json:"prompt_path"`
	Started   int64           `json:"started"`
	Finished  int64           `json:"finished"`
	// Tokens is the runner-reported token usage of the stage (AT12); 0 for
	// engine stages and for runners that report none.
	Tokens int64 `json:"tokens"`
}

// VerifyReport is the engine's deterministic verification of an attempt.
type VerifyReport struct {
	Pass             bool     `json:"pass"`
	Checks           []Check  `json:"checks"`
	Warnings         []string `json:"warnings,omitempty"`
	NewFailures      []string `json:"new_failures,omitempty"`
	BaselineFailures []string `json:"baseline_failures,omitempty"`
}

// Check is one verify check and its result.
type Check struct {
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// ReviewResult is a task reviewer's verdict.
type ReviewResult struct {
	Verdict      string  `json:"verdict"` // pass, repair, fail
	Summary      string  `json:"summary"`
	Issues       []Issue `json:"issues"`
	RepairPrompt string  `json:"repair_prompt,omitempty"`
}

// Issue is a defect reported by a reviewer.
type Issue struct {
	Description string `json:"description"`
	Severity    string `json:"severity"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
}

// FRD is the FRD document stored as a "frd" revision. StateModel, Findings
// and PlanningSpikes are carried verbatim.
type FRD struct {
	ID                 string           `json:"id"`
	Title              string           `json:"title"`
	Status             string           `json:"status,omitempty"`
	Created            string           `json:"created,omitempty"`
	ApprovedDate       string           `json:"approved_date,omitempty"`
	Objective          string           `json:"objective"`
	Context            string           `json:"context,omitempty"`
	Constraints        []string         `json:"constraints,omitempty"`
	Groups             []FRDGroup       `json:"groups,omitempty"`
	StateModel         json.RawMessage  `json:"state_model,omitempty"`
	Requirements       []FRDRequirement `json:"requirements"`
	AcceptanceCriteria []FRDAC          `json:"acceptance_criteria"`
	Decisions          []FRDDecision    `json:"decisions,omitempty"`
	Findings           json.RawMessage  `json:"findings,omitempty"`
	OutOfScope         []string         `json:"out_of_scope,omitempty"`
	PlanningSpikes     json.RawMessage  `json:"planning_spikes,omitempty"`
}

// FRDGroup groups related requirements.
type FRDGroup struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
}

// FRDRequirement is one requirement.
type FRDRequirement struct {
	ID          string          `json:"id"`
	Group       string          `json:"group,omitempty"`
	Priority    string          `json:"priority,omitempty"` // must, should, could, wont
	Findings    json.RawMessage `json:"findings,omitempty"`
	Description string          `json:"description"`
}

// FRDAC is one acceptance criterion.
type FRDAC struct {
	ID           string   `json:"id"`
	Requirements []string `json:"requirements,omitempty"`
	Criterion    string   `json:"criterion"`
}

// FRDDecision is a recorded design decision.
type FRDDecision struct {
	ID        string `json:"id"`
	Decision  string `json:"decision"`
	Rationale string `json:"rationale,omitempty"`
}

// ParseFRD decodes and validates an FRD document. Ids are optional (FillFRD
// assigns them on save), but an id that is given must be unique, and a link
// from a criterion to a requirement must name a requirement of the document.
func ParseFRD(raw []byte) (FRD, error) {
	var f FRD
	if err := json.Unmarshal(raw, &f); err != nil {
		return FRD{}, fmt.Errorf("frd: %w", err)
	}
	switch {
	case strings.TrimSpace(f.Title) == "":
		return FRD{}, fmt.Errorf("frd: title is required")
	case strings.TrimSpace(f.Objective) == "":
		return FRD{}, fmt.Errorf("frd: objective is required")
	case len(f.Requirements) == 0:
		return FRD{}, fmt.Errorf("frd: at least one requirement is required")
	}
	reqs := map[string]bool{}
	for i, r := range f.Requirements {
		if strings.TrimSpace(r.Description) == "" {
			return FRD{}, fmt.Errorf("frd: requirements[%d] needs a description", i)
		}
		if r.ID != "" && reqs[r.ID] {
			return FRD{}, fmt.Errorf("frd: duplicate requirement %q", r.ID)
		}
		reqs[r.ID] = true
	}
	for i, ac := range f.AcceptanceCriteria {
		if strings.TrimSpace(ac.Criterion) == "" {
			return FRD{}, fmt.Errorf("frd: acceptance_criteria[%d] needs a criterion", i)
		}
		for _, r := range ac.Requirements {
			if !reqs[r] {
				return FRD{}, fmt.Errorf("frd: %s references unknown requirement %q", ac.ID, r)
			}
		}
	}
	return f, nil
}

// FillFRD completes what code can decide, so nobody types it: the id defaults
// to FRD-<effort>, requirements and criteria without an id get R<n> and AC<n>
// with n above every number already used in the document (an id is never
// reused, so a criterion linking a deleted requirement cannot silently point
// at a new one), a link to a requirement that does not exist is dropped, a
// criterion with no links covers every requirement, and status and created
// date default to draft and today. It never changes a value that is already
// set otherwise, so a filled FRD fills to itself.
func FillFRD(f *FRD, effort int64, today time.Time) {
	if strings.TrimSpace(f.ID) == "" {
		f.ID = fmt.Sprintf("FRD-%d", effort)
	}
	if f.Status == "" {
		f.Status = "draft"
	}
	if f.Created == "" {
		f.Created = today.Format("2006-01-02")
	}
	used := map[string]bool{}
	n := 0
	for _, r := range f.Requirements {
		used[r.ID] = true
		n = max(n, idNumber("R", r.ID))
	}
	next := func(prefix string, n *int) string {
		for {
			*n++
			if id := fmt.Sprintf("%s%d", prefix, *n); !used[id] {
				used[id] = true
				return id
			}
		}
	}
	for i := range f.Requirements {
		if f.Requirements[i].ID == "" {
			f.Requirements[i].ID = next("R", &n)
		}
	}
	all := make([]string, len(f.Requirements))
	exists := map[string]bool{}
	for i, r := range f.Requirements {
		all[i] = r.ID
		exists[r.ID] = true
	}
	used = map[string]bool{}
	n = 0
	for _, ac := range f.AcceptanceCriteria {
		used[ac.ID] = true
		n = max(n, idNumber("AC", ac.ID))
	}
	for i := range f.AcceptanceCriteria {
		ac := &f.AcceptanceCriteria[i]
		if ac.ID == "" {
			ac.ID = next("AC", &n)
		}
		if len(ac.Requirements) > 0 {
			kept := ac.Requirements[:0:0]
			for _, r := range ac.Requirements {
				if exists[r] {
					kept = append(kept, r)
				}
			}
			ac.Requirements = kept
		}
		if len(ac.Requirements) == 0 {
			ac.Requirements = slices.Clone(all)
		}
	}
}

// idNumber is n for an id of the form prefix+n, else 0.
func idNumber(prefix, id string) int {
	rest, ok := strings.CutPrefix(id, prefix)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// ParseFilledFRD is ParseFRD after FillFRD, the order every save uses: ids the
// author left out are assigned before links are checked.
func ParseFilledFRD(raw []byte) (FRD, error) {
	var f FRD
	if err := json.Unmarshal(raw, &f); err != nil {
		return FRD{}, fmt.Errorf("frd: %w", err)
	}
	FillFRD(&f, 0, time.Time{})
	filled, err := json.Marshal(f)
	if err != nil {
		return FRD{}, err
	}
	return ParseFRD(filled)
}
