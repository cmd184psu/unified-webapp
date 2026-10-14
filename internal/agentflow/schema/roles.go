package schema

import (
	"fmt"
	"regexp"
	"strings"
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
	return nonEmpty("summary", w.Summary)
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
var verifyOnlyTitle = regexp.MustCompile(`(?i)^\s*(run|verify|confirm|check|ensure|validate)\b.*\b(tests?|builds?|lint|vet)\b`)

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
		if verifyOnlyTitle.MatchString(t.Title) {
			return bad(f+"title", "task %s %q only verifies tests or the build: verification is automatic for every task, so drop it or fold it into a task that changes files", t.ID, t.Title)
		}
	}
	return nil
}
