package agentflow

import (
	"time"
)

// DefaultAgentflowProjectPath is the default project root directory — the
// directory that contains `.agentflow` — when project_path is unset
// (0-length) in the config file.
const DefaultAgentflowProjectPath = "./data/agentflow"

// ─── Profile / Model Configuration ──────────────────────────────────────────

// ProfileConfig defines a named model profile (knife switch).
// Maps to AGENTFLOW_PROFILE env var in the legacy scripts.
type ProfileConfig struct {
	Name         string `json:"name"`          // e.g., "m5", "m4", "windows"
	OllamaHost   string `json:"ollama_host"`   // e.g., "http://192.168.1.239:11434"
	WorkerModel  string `json:"worker_model"`  // e.g., "qwen3.6:35b-a3b-mxfp8"
	PlannerModel string `json:"planner_model"` // e.g., "qwen3.6:35b-a3b-mxfp8"
	ReviewModel  string `json:"review_model"`  // e.g., "mistral-small3.2"
	TimeoutSecs  int    `json:"timeout_secs"`  // default 300
	PromptDir    string `json:"prompt_dir"`    // optional override
}

// ResolvedProfile is a profile with defaults filled in.
type ResolvedProfile struct {
	ProfileConfig
	// Computed fields
	EffectiveTimeout time.Duration
}

// ─── FRD (Functional Requirements Document) ────────────────────────────────

// FRD matches schemas/frd.schema.json
type FRD struct {
	ID                string           `json:"id"`
	Title             string           `json:"title"`
	Status            string           `json:"status"` // draft, approved, in_progress, completed, abandoned
	Created           string           `json:"created,omitempty"`
	ApprovedDate      string           `json:"approved_date,omitempty"`
	Objective         string           `json:"objective"`
	Requirements      []FRDRequirement `json:"requirements"`
	AcceptanceCriteria []FRDAC         `json:"acceptance_criteria"`
	Constraints       []string         `json:"constraints,omitempty"`
	Context           string           `json:"context,omitempty"`
}

type FRDRequirement struct {
	ID          string `json:"id"`                    // R1, R2...
	Description string `json:"description"`
	Priority    string `json:"priority,omitempty"`    // must, should, could
}

type FRDAC struct {
	ID        string `json:"id"`        // AC1, AC2...
	Criterion string `json:"criterion"`
}

// ─── Plan ───────────────────────────────────────────────────────────────────

// Plan matches schemas/plan.schema.json
type Plan struct {
	FRDID          string          `json:"frd_id"`
	Status         string          `json:"status"` // draft, approved, in_progress, completed
	Created        string          `json:"created,omitempty"`
	ApprovedDate   string          `json:"approved_date,omitempty"`
	Approach       string          `json:"approach"`
	Assumptions    []string        `json:"assumptions,omitempty"`
	Risks          []PlanRisk      `json:"risks,omitempty"`
	Phases         []PlanPhase     `json:"phases"`
	EstimatedTasks int             `json:"estimated_tasks,omitempty"`
	ReviewNotes    []PlanReviewNote `json:"review_notes,omitempty"`
}

type PlanRisk struct {
	Risk       string `json:"risk"`
	Mitigation string `json:"mitigation"`
}

type PlanPhase struct {
	ID               string   `json:"id"`                 // P1, P2...
	Name             string   `json:"name"`
	TasksDescription string   `json:"tasks_description"`
	DependsOn        []string `json:"depends_on,omitempty"`
}

type PlanReviewNote struct {
	Description string `json:"description"`
	Severity    string `json:"severity"` // blocker, major, minor
}

// ─── Task Queue ─────────────────────────────────────────────────────────────

// TaskQueue matches schemas/task-queue.schema.json
type TaskQueue struct {
	FRDID       string  `json:"frd_id"`
	PlanStatus  string  `json:"plan_status"` // approved
	CompiledDate string `json:"compiled_date,omitempty"`
	Tasks       []Task  `json:"tasks"`
}

// ─── Task ───────────────────────────────────────────────────────────────────

// Task matches schemas/task.schema.json
type Task struct {
	ID             string   `json:"id"`               // T001, T002...
	Phase          string   `json:"phase"`            // P1, P2...
	Title          string   `json:"title"`
	Status         string   `json:"status"`           // blocked, ready, running, review, passed, failed, repair
	DependsOn      []string `json:"depends_on,omitempty"`
	Prompt         string   `json:"prompt"`
	WorkingDir     string   `json:"working_dir"`
	Verify         string   `json:"verify"`
	Acceptance     []string `json:"acceptance"`
	FilesTouched   []string `json:"files_touched"`
	Attempts       int      `json:"attempts"`
	MaxAttempts    int      `json:"max_attempts"`
	InfraRetries   int      `json:"infra_retries,omitempty"`
	ReviewFollowups int     `json:"review_followups,omitempty"`
	RepairPrompt   string   `json:"repair_prompt,omitempty"`
	LastRun        string   `json:"last_run,omitempty"`
	LastReview     *Review  `json:"last_review,omitempty"`
	ReviewResult   *Review  `json:"review_result,omitempty"`
}

// Review matches schemas/review.schema.json
type Review struct {
	TaskID     string        `json:"task_id"`
	Verdict    string        `json:"verdict"` // pass, repair, fail
	Summary    string        `json:"summary"`
	Issues     []ReviewIssue `json:"issues"`
	RepairPrompt string      `json:"repair_prompt,omitempty"`
	ReviewedAt string        `json:"reviewed_at"`
	Reviewer   string        `json:"reviewer"` // model name
}

type ReviewIssue struct {
	Description string `json:"description"`
	Severity    string `json:"severity"` // blocker, major, minor
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
}

// PlanReview is one plan-reviewer verdict: the exact JSON the plan-reviewer
// prompt specifies, mirrored by the frontend's PlanReviewResult.
type PlanReview struct {
	Verdict      string        `json:"verdict"` // pass, repair, fail
	Summary      string        `json:"summary"`
	Issues       []ReviewIssue `json:"issues"`
	RepairPrompt string        `json:"repair_prompt,omitempty"`
}

// ─── Runtime State (in-memory, not persisted to JSON) ──────────────────────

// TaskExecution represents a live task run with streaming output.
type TaskExecution struct {
	TaskID      string
	Task        *Task
	Status      string          // running, verifying, reviewing, completed
	StartTime   time.Time
	OutputLines []string        // accumulated stdout/stderr
	VerifyOut   string          // verify command output
	Review      *Review         // reviewer result
	Cancel      chan struct{}   // for timeout/cancellation
}

// ─── Prompt Templates ──────────────────────────────────────────────────────

// PromptTemplates holds all prompt templates loaded from disk.
type PromptTemplates struct {
	Planner       string // prompts/planner.md
	Worker        string // prompts/worker.md
	Reviewer      string // prompts/reviewer.md
	PlanReviewer  string // prompts/plan-reviewer.md
	TaskCompiler  string // prompts/task-compiler.md
}

// ─── Lock ──────────────────────────────────────────────────────────────────

// LockFile represents the .agentflow/lock file for single-task enforcement.
type LockFile struct {
	PID      int    `json:"pid"`
	TaskID   string `json:"task_id"`
	Started  string `json:"started"` // RFC3339
	Hostname string `json:"hostname"`
}