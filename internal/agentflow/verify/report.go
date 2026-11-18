// Package verify holds the engine-owned verify stage. It is deterministic Go
// code: it never runs a command taken from a model or a task, and it never
// goes through a shell.
package verify

// Check names reported by the structural stage.
const (
	CheckNoChanges   = "no_changes"
	CheckScope       = "scope"
	CheckContainment = "containment"
	CheckParse       = "parse"
	CheckConflicts   = "conflict_markers"
	CheckNestedRepo  = "nested_repo"
)

// Check is the outcome of one named check. Details lists the offending files
// or reasons; it is empty for a clean pass.
type Check struct {
	Name    string   `json:"name"`
	Pass    bool     `json:"pass"`
	Details []string `json:"details,omitempty"`
}

// Evidence is what a failing test printed, kept apart from the failure's
// signature so it never changes a fingerprint.
type Evidence struct {
	Sig  string `json:"sig"`
	Text string `json:"text"`
}

// VerifyReport is what the reviewer reads. Warnings never fail the report.
// NewFailures and BaselineFailures are filled by the feedback-command half of
// verify (B12); the structural half leaves them empty.
type VerifyReport struct {
	Checks      []Check  `json:"checks"`
	Warnings    []string `json:"warnings"`
	NewFailures []string `json:"new_failures"`
	// Evidence is what each new failing test printed (its assertion message or panic), so a repair can see why it failed.
	Evidence         []Evidence `json:"evidence,omitempty"`
	BaselineFailures []string   `json:"baseline_failures"`
	Pass             bool       `json:"pass"`
}

// Failed returns the failing check with the given name, if any.
func (r VerifyReport) Failed(name string) bool {
	for _, c := range r.Checks {
		if c.Name == name && !c.Pass {
			return true
		}
	}
	return false
}

// Finalize recomputes Pass: every check passes and there are no new failures.
func (r *VerifyReport) Finalize() {
	r.Pass = len(r.Checks) > 0 && len(r.NewFailures) == 0
	for _, c := range r.Checks {
		r.Pass = r.Pass && c.Pass
	}
}
