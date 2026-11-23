package schema

import (
	"fmt"
	"regexp"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

// Issue is a defect reported by a reviewer, critic or architect.
type Issue struct {
	Description string `json:"description"`
	Severity    string `json:"severity"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
}

// WorkerOut is the worker's final message.
type WorkerOut struct {
	Summary      string   `json:"summary"`
	FilesChanged []string `json:"files_changed"`
	Learnings    []string `json:"learnings"`
	Blocked      string   `json:"blocked,omitempty"`
	// NoChangesReason is set when the files already satisfy the task, so the
	// worker changed nothing; it names the evidence.
	NoChangesReason string `json:"no_changes_reason,omitempty"`
	// NeedsFiles names files outside the task's files_touched that the worker could not finish
	// without (AF-13). The worker leaves them alone; the owner may allow them, and the task is
	// retried in the same worktree with them added to its list.
	NeedsFiles []NeededFile `json:"needs_files,omitempty"`
}

// NeededFile is one file a worker asks to be allowed to change, and why.
type NeededFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ReviewOut is the task reviewer's final message.
type ReviewOut struct {
	Verdict      string  `json:"verdict"`
	Summary      string  `json:"summary"`
	Issues       []Issue `json:"issues"`
	RepairPrompt string  `json:"repair_prompt"`
	Blocked      string  `json:"blocked,omitempty"`
}

// PlanOption is one option the planner considered.
type PlanOption struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Pros        []string `json:"pros,omitempty"`
	Cons        []string `json:"cons,omitempty"`
}

// PlanPhase is one ordered phase of a plan.
type PlanPhase struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	TasksDescription string   `json:"tasks_description"`
	DependsOn        []string `json:"depends_on,omitempty"`
	// Toolchains are the host toolchains the phase's files need, carried from
	// the FRD's "Toolchains:" line (AF-6b).
	Toolchains []string `json:"toolchains,omitempty"`
}

// ADR is the architecture decision record of a plan.
type ADR struct {
	Decision     string   `json:"decision"`
	Drivers      []string `json:"drivers"`
	Alternatives []string `json:"alternatives"`
	Why          string   `json:"why"`
	Consequences []string `json:"consequences"`
	Followups    []string `json:"followups"`
}

// PlanOut is the planner's final message. Premortem and TestPlan are the
// optional deliberate-mode additions.
type PlanOut struct {
	Principles []string     `json:"principles"`
	Drivers    []string     `json:"drivers"`
	Options    []PlanOption `json:"options"`
	Phases     []PlanPhase  `json:"phases"`
	ADR        ADR          `json:"adr"`
	Premortem  []string     `json:"premortem,omitempty"`
	TestPlan   []string     `json:"test_plan,omitempty"`
}

// PremortemScenarios is how many failure scenarios a deliberate plan carries (RP5).
const PremortemScenarios = 3

// ValidateDeliberate is Validate plus the deliberate-mode requirement (RP5): a
// pre-mortem of exactly three failure scenarios, none of them blank.
func (p *PlanOut) ValidateDeliberate() error {
	if err := p.Validate(); err != nil {
		return err
	}
	if len(p.Premortem) != PremortemScenarios {
		return bad("premortem", "deliberate mode needs exactly %d failure scenarios (cause, effect, mitigation), got %d", PremortemScenarios, len(p.Premortem))
	}
	for i, s := range p.Premortem {
		if strings.TrimSpace(s) == "" {
			return bad(fmt.Sprintf("premortem[%d]", i), "is blank")
		}
	}
	return nil
}

// PlanReviewOut is the architect's or critic's final message.
type PlanReviewOut struct {
	Verdict string  `json:"verdict"`
	Summary string  `json:"summary,omitempty"`
	Issues  []Issue `json:"issues"`
}

// CompileTask is one task emitted by the compiler. It carries no executable
// content; an unknown field such as verify is rejected by Parse.
type CompileTask struct {
	ID           string   `json:"id"`
	Phase        string   `json:"phase"`
	Title        string   `json:"title"`
	DependsOn    []string `json:"depends_on,omitempty"`
	Prompt       string   `json:"prompt"`
	Acceptance   []string `json:"acceptance"`
	FilesTouched []string `json:"files_touched"`
	// Toolchains names the host toolchains the task needs, for example
	// "dotnet". The engine infers them from files_touched as well and acts on
	// the union (AF-6b).
	Toolchains []string `json:"toolchains,omitempty"`
}

// CompileOut is the compiler's final message.
type CompileOut struct {
	Tasks []CompileTask `json:"tasks"`
}

func bad(field, format string, args ...any) error {
	return &ValidationError{Field: field, Msg: fmt.Sprintf(format, args...)}
}

func oneOf(field, got string, allowed ...string) error {
	for _, a := range allowed {
		if got == a {
			return nil
		}
	}
	return bad(field, "must be one of %v, got %q", allowed, got)
}

func nonEmpty(field, s string) error {
	if s == "" {
		return bad(field, "is required")
	}
	return nil
}

// validateToolchains rejects a toolchain agentflow cannot mount, so a
// misconfigured task fails at compile time with a reason instead of at its
// first verify command.
func validateToolchains(field string, names []string) error {
	seen := map[string]bool{}
	for i, n := range names {
		if !model.ValidToolchain(n) {
			return bad(fmt.Sprintf("%s[%d]", field, i), "unknown toolchain %q (known: %s)", n, strings.Join(model.Toolchains, ", "))
		}
		if seen[n] {
			return bad(field, "lists toolchain %q twice", n)
		}
		seen[n] = true
	}
	return nil
}

func nonEmptyList(field string, l []string) error {
	if len(l) == 0 {
		return bad(field, "needs at least one entry")
	}
	return nil
}

func validateIssues(field string, issues []Issue) error {
	for i, is := range issues {
		f := fmt.Sprintf("%s[%d]", field, i)
		if err := nonEmpty(f+".description", is.Description); err != nil {
			return err
		}
		if err := oneOf(f+".severity", is.Severity, "blocker", "major", "minor"); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks required fields.
func (w *WorkerOut) Validate() error {
	if err := nonEmpty("summary", w.Summary); err != nil {
		return err
	}
	for i, f := range w.NeedsFiles {
		p := strings.TrimSpace(f.Path)
		if p == "" || strings.HasPrefix(p, "/") || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") {
			return bad(fmt.Sprintf("needs_files[%d].path", i), "must be a path relative to the repository root, got %q", f.Path)
		}
		if err := nonEmpty(fmt.Sprintf("needs_files[%d].reason", i), f.Reason); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the verdict enum, required fields and issues. A blocked
// review (Blocked != "") judged nothing, so its verdict must be empty rather
// than an invented one; summary is still required.
func (r *ReviewOut) Validate() error {
	if err := nonEmpty("summary", r.Summary); err != nil {
		return err
	}
	if r.Blocked != "" {
		if r.Verdict != "" {
			return bad("verdict", "must be empty when blocked is set, got %q", r.Verdict)
		}
		return validateIssues("issues", r.Issues)
	}
	if err := oneOf("verdict", r.Verdict, "pass", "repair"); err != nil {
		return err
	}
	if err := validateIssues("issues", r.Issues); err != nil {
		return err
	}
	if r.Verdict == "repair" {
		return nonEmpty("repair_prompt", r.RepairPrompt)
	}
	return nil
}

// Validate requires principles, drivers, two options, phases and a full ADR.
func (p *PlanOut) Validate() error {
	if err := nonEmptyList("principles", p.Principles); err != nil {
		return err
	}
	if err := nonEmptyList("drivers", p.Drivers); err != nil {
		return err
	}
	if len(p.Options) < 2 {
		return bad("options", "needs at least 2 entries, got %d", len(p.Options))
	}
	for i, o := range p.Options {
		if err := nonEmpty(fmt.Sprintf("options[%d].name", i), o.Name); err != nil {
			return err
		}
		if err := nonEmpty(fmt.Sprintf("options[%d].description", i), o.Description); err != nil {
			return err
		}
	}
	if len(p.Phases) == 0 {
		return bad("phases", "needs at least one entry")
	}
	for i, ph := range p.Phases {
		if err := nonEmpty(fmt.Sprintf("phases[%d].id", i), ph.ID); err != nil {
			return err
		}
		if err := nonEmpty(fmt.Sprintf("phases[%d].name", i), ph.Name); err != nil {
			return err
		}
		if err := nonEmpty(fmt.Sprintf("phases[%d].tasks_description", i), ph.TasksDescription); err != nil {
			return err
		}
		if err := validateToolchains(fmt.Sprintf("phases[%d].toolchains", i), ph.Toolchains); err != nil {
			return err
		}
	}
	return p.ADR.validate()
}

func (a *ADR) validate() error {
	if err := nonEmpty("adr.decision", a.Decision); err != nil {
		return err
	}
	if err := nonEmptyList("adr.drivers", a.Drivers); err != nil {
		return err
	}
	if err := nonEmptyList("adr.alternatives", a.Alternatives); err != nil {
		return err
	}
	if err := nonEmpty("adr.why", a.Why); err != nil {
		return err
	}
	if err := nonEmptyList("adr.consequences", a.Consequences); err != nil {
		return err
	}
	return nonEmptyList("adr.followups", a.Followups)
}

// Validate checks the verdict enum and issues.
func (r *PlanReviewOut) Validate() error {
	if err := oneOf("verdict", r.Verdict, "approve", "revise", "reject"); err != nil {
		return err
	}
	return validateIssues("issues", r.Issues)
}

// verifyOnlyTitle matches a task title that only runs, checks or verifies
// tests, builds, lint or vet. The engine does that for every task, and such a
// task changes no files, so verify's no_changes check could never pass it.
//
// The whole title must be a verification phrase ("Verify all tests pass",
// "Run the Go test suite", "Make sure the build succeeds"); a title that adds
// work ("Validate input and add unit tests") is not matched.
var verifyOnlyTitle = regexp.MustCompile(`(?i)^\s*(run|verify|confirm|check|ensure|validate|make sure)\s+(that\s+)?(all\s+|the\s+|every\s+)*(unit\s+|go\s+|existing\s+|integration\s+)*(tests?|builds?|lint|vet)(\s+(suite|passes?|pass|succeeds?|succeed|results?|runs?|clean|green|still))*\s*\.?\s*$`)

// Validate requires at least one fully described task, none of which is
// verification-only.
func (c *CompileOut) Validate() error {
	if len(c.Tasks) == 0 {
		return bad("tasks", "needs at least one entry")
	}
	for i, t := range c.Tasks {
		f := fmt.Sprintf("tasks[%d].", i)
		for _, req := range []struct{ name, val string }{
			{"id", t.ID}, {"phase", t.Phase}, {"title", t.Title}, {"prompt", t.Prompt},
		} {
			if err := nonEmpty(f+req.name, req.val); err != nil {
				return err
			}
		}
		if err := nonEmptyList(f+"acceptance", t.Acceptance); err != nil {
			return err
		}
		if err := nonEmptyList(f+"files_touched", t.FilesTouched); err != nil {
			return err
		}
		if err := validateToolchains(f+"toolchains", t.Toolchains); err != nil {
			return err
		}
		if verifyOnlyTitle.MatchString(t.Title) {
			return bad(f+"title", "task %s %q only verifies tests or the build: verification is automatic for every task, so drop it or fold it into a task that changes files", t.ID, t.Title)
		}
	}
	return nil
}

// Gap is one requirement the finished work does not meet, found by the
// acceptance check (H31).
type Gap struct {
	Requirement string   `json:"requirement"`
	Evidence    string   `json:"evidence"`
	Problem     string   `json:"problem"`
	Fix         string   `json:"fix"`
	Files       []string `json:"files,omitempty"`
}

// AcceptanceOut is the acceptance reviewer's final message.
type AcceptanceOut struct {
	Verdict string `json:"verdict"`
	Summary string `json:"summary,omitempty"`
	Gaps    []Gap  `json:"gaps"`
}

// Validate checks the verdict enum, and that "gaps" names at least one gap and
// each gap says which requirement, where the evidence is, what is wrong and
// what to change.
func (a *AcceptanceOut) Validate() error {
	if err := oneOf("verdict", a.Verdict, "pass", "gaps"); err != nil {
		return err
	}
	if a.Verdict == "gaps" && len(a.Gaps) == 0 {
		return bad("gaps", "needs at least one entry when the verdict is gaps")
	}
	for i, g := range a.Gaps {
		f := fmt.Sprintf("gaps[%d].", i)
		for _, req := range []struct{ name, val string }{
			{"requirement", g.Requirement}, {"evidence", g.Evidence}, {"problem", g.Problem}, {"fix", g.Fix},
		} {
			if err := nonEmpty(f+req.name, req.val); err != nil {
				return err
			}
		}
	}
	return nil
}

// MergerOut is the merger's final message after resolving a conflicted merge
// (AF-18). Notes name any incoming change the merger intentionally dropped and
// why; the engine's no-loss check reads them.
type MergerOut struct {
	Summary string   `json:"summary"`
	Files   []string `json:"files,omitempty"`
	Notes   []string `json:"notes,omitempty"`
	// Blocked is set when both sides cannot hold: the merger stops and says why.
	Blocked string `json:"blocked,omitempty"`
}

func (m *MergerOut) Validate() error {
	if err := nonEmpty("summary", m.Summary); err != nil {
		return err
	}
	if m.Blocked != "" && len(m.Notes) == 0 {
		return bad("notes", "a blocked merger must name what cannot hold")
	}
	return nil
}

// ResolverOut is the resolver's decision on a stuck task (AF-13 3b): one way through, with
// the evidence (file:line) it rests on.
type ResolverOut struct {
	// Decision is clarify, allow_files, override_finding, split or escalate.
	Decision string   `json:"decision"`
	Reason   string   `json:"reason"`
	Evidence []string `json:"evidence"`
	// Clarification amends the task's prompt (clarify), shown to the worker and the reviewer.
	Clarification string `json:"clarification,omitempty"`
	// Files are added to files_touched (allow_files).
	Files []NeededFile `json:"files,omitempty"`
	// Finding is the reviewer finding declared out of scope for this task (override_finding).
	Finding string `json:"finding,omitempty"`
	// Tasks are new tasks taking part of this one (split); they run after it.
	Tasks []CompileTask `json:"tasks,omitempty"`
}

func (r *ResolverOut) Validate() error {
	if err := oneOf("decision", r.Decision, "clarify", "allow_files", "override_finding", "split", "escalate"); err != nil {
		return err
	}
	if err := nonEmpty("reason", r.Reason); err != nil {
		return err
	}
	if r.Decision != "escalate" {
		if err := nonEmptyList("evidence", r.Evidence); err != nil {
			return err
		}
	}
	switch r.Decision {
	case "clarify":
		return nonEmpty("clarification", r.Clarification)
	case "allow_files":
		if len(r.Files) == 0 {
			return bad("files", "allow_files needs at least one file")
		}
		w := WorkerOut{Summary: "x", NeedsFiles: r.Files}
		return w.Validate()
	case "override_finding":
		return nonEmpty("finding", r.Finding)
	case "split":
		if len(r.Tasks) == 0 {
			return bad("tasks", "split needs at least one task")
		}
	}
	return nil
}

// CoordinatorOut is the coordinator's final message for one run (AF-19): what it
// found and the changes it wants made. The engine applies the actions it is
// allowed to make and turns any it may not into an item for the owner; a stage
// itself makes no change.
type CoordinatorOut struct {
	// Summary is what the run saw and did, in one or two sentences.
	Summary string `json:"summary"`
	// Actions are the changes to apply, in the order to apply them.
	Actions []CoordinatorAction `json:"actions,omitempty"`
	// Questions are decisions the coordinator may not make (AF-19 item 6).
	Questions []CoordinatorQuestion `json:"questions,omitempty"`
}

// CoordinatorAction is one change the coordinator asks the engine to make.
type CoordinatorAction struct {
	// Kind is allow_files, retry, budget, note, park, unpark, approve_queue or
	// override for one it may make; approve_frd, merge, promote, delete,
	// set_settings, set_candidates or restart_service is refused.
	Kind string `json:"kind"`
	// Project is the project slug and Task the task key, for a task action.
	Project string `json:"project,omitempty"`
	Task    string `json:"task,omitempty"`
	// Effort is the effort id, for an effort action (budget, park, unpark,
	// approve_queue).
	Effort int64 `json:"effort,omitempty"`
	// Files are the paths to allow (allow_files), repo-relative.
	Files []string `json:"files,omitempty"`
	// Clean removes the task's worktree before the retry (retry).
	Clean bool `json:"clean,omitempty"`
	// Tokens is the new token budget (budget), which may only go up.
	Tokens int64 `json:"tokens,omitempty"`
	// Note is the text appended to the task's prompt (note).
	Note string `json:"note,omitempty"`
	// Reason says why, in the words the event log and the owner read.
	Reason string `json:"reason"`
}

// CoordinatorQuestion is a decision that stays with the owner.
type CoordinatorQuestion struct {
	Question string `json:"question"`
	// Options are the answers it may choose from.
	Options []string `json:"options,omitempty"`
	// Recommendation is the coordinator's own pick, if it has one.
	Recommendation string `json:"recommendation,omitempty"`
}

// coordinatorKinds are the actions the engine applies for the coordinator.
var coordinatorKinds = []string{"allow_files", "retry", "budget", "note", "park", "unpark", "approve_queue", "override"}

// coordinatorRefused are the actions the coordinator may never make: they stay
// with the owner (AF-19 item 4). A run may still name one, and the engine turns
// it into an item for the owner rather than making it.
var coordinatorRefused = []string{"approve_frd", "merge", "promote", "delete", "set_settings", "set_candidates", "restart_service"}

// CoordinatorAllowed reports whether the engine may apply actions of a kind.
func CoordinatorAllowed(kind string) bool {
	for _, k := range coordinatorKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// CoordinatorForbidden reports whether a kind is one only the owner may take.
func CoordinatorForbidden(kind string) bool {
	for _, k := range coordinatorRefused {
		if k == kind {
			return true
		}
	}
	return false
}

func (c *CoordinatorOut) Validate() error {
	if err := nonEmpty("summary", c.Summary); err != nil {
		return err
	}
	if len(c.Actions) > 100 {
		return bad("actions", "at most 100 actions per run")
	}
	for i, a := range c.Actions {
		if a.Kind == "" {
			return bad(fmt.Sprintf("actions[%d].kind", i), "is required")
		}
		if !CoordinatorAllowed(a.Kind) && !CoordinatorForbidden(a.Kind) {
			return bad(fmt.Sprintf("actions[%d].kind", i), "must be one of %v (or a refused kind: %v), got %q", coordinatorKinds, coordinatorRefused, a.Kind)
		}
		if err := nonEmpty(fmt.Sprintf("actions[%d].reason", i), a.Reason); err != nil {
			return err
		}
		switch a.Kind {
		case "allow_files":
			if len(a.Files) == 0 {
				return bad(fmt.Sprintf("actions[%d].files", i), "allow_files needs at least one file")
			}
		case "note":
			if err := nonEmpty(fmt.Sprintf("actions[%d].note", i), a.Note); err != nil {
				return err
			}
		case "budget":
			if a.Effort == 0 || a.Tokens <= 0 {
				return bad(fmt.Sprintf("actions[%d].tokens", i), "budget needs an effort and a token count")
			}
		case "park", "unpark", "approve_queue":
			if a.Effort == 0 {
				return bad(fmt.Sprintf("actions[%d].effort", i), "%s needs an effort", a.Kind)
			}
		}
		if a.Kind == "allow_files" || a.Kind == "retry" || a.Kind == "override" {
			if a.Project == "" || a.Task == "" {
				return bad(fmt.Sprintf("actions[%d]", i), "%s needs a project and a task key", a.Kind)
			}
		}
	}
	for i, q := range c.Questions {
		if err := nonEmpty(fmt.Sprintf("questions[%d].question", i), q.Question); err != nil {
			return err
		}
	}
	return nil
}
