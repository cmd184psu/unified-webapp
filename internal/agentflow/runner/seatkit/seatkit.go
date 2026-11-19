// Package seatkit is what the seat runners share: the report a seat files when it finishes
// a stage, how that report becomes the final message the engine parses for the role, and the
// instructions that tell a seat how to file it. A seat is a person or an interactive agent
// that agentflow hands a stage to and waits for; it costs no tokens.
package seatkit

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Report statuses.
const (
	StatusDone     = "done"
	StatusQuestion = "question"
	StatusBlocked  = "blocked"
)

// Issue is one problem a reviewing seat found.
type Issue struct {
	Description string `json:"description"`
	Severity    string `json:"severity,omitempty"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
}

// Report is what a seat writes when it stops: RESULT.json for a terminal seat.
type Report struct {
	Status   string `json:"status"`
	Summary  string `json:"summary,omitempty"`
	Question string `json:"question,omitempty"`
	Blocked  string `json:"blocked,omitempty"`
	// Worker fields. Branch and Commit are informational: the engine commits the worktree.
	Branch       string   `json:"branch,omitempty"`
	Commit       string   `json:"commit,omitempty"`
	FilesChanged []string `json:"files_changed,omitempty"`
	Learnings    []string `json:"learnings,omitempty"`
	// NoChangesReason says the files already satisfied the task.
	NoChangesReason string `json:"no_changes_reason,omitempty"`
	// Reviewer fields: the verdict JSON the engine already expects.
	Verdict      string  `json:"verdict,omitempty"`
	Issues       []Issue `json:"issues,omitempty"`
	RepairPrompt string  `json:"repair_prompt,omitempty"`
	// Output is the role's whole final object, for roles other than worker and reviewer.
	Output json.RawMessage `json:"output,omitempty"`
}

// ParseReport reads a report. Unknown fields are allowed: a seat may add notes.
func ParseReport(b []byte) (Report, error) {
	var r Report
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("seat report is not JSON: %w", err)
	}
	r.Status = strings.ToLower(strings.TrimSpace(r.Status))
	switch r.Status {
	case StatusDone, StatusBlocked:
	case StatusQuestion:
		if strings.TrimSpace(r.Question) == "" {
			return r, errors.New("seat report: a question needs the question text")
		}
	default:
		return r, fmt.Errorf("seat report: status must be done, question or blocked, got %q", r.Status)
	}
	return r, nil
}

// Final renders a done or blocked report as the role's final message, in exactly the shape the
// engine's strict parsing takes (schema.WorkerOut, schema.ReviewOut, or the role's own object).
func Final(role string, r Report) (string, error) {
	summary := strings.TrimSpace(r.Summary)
	if summary == "" {
		summary = "the seat reported " + r.Status
	}
	var out any
	switch role {
	case "worker", "cleanup":
		w := map[string]any{"summary": summary, "files_changed": nonNil(r.FilesChanged), "learnings": nonNil(r.Learnings)}
		if r.Status == StatusBlocked {
			w["blocked"] = firstNonEmpty(r.Blocked, summary)
		}
		if r.NoChangesReason != "" {
			w["no_changes_reason"] = r.NoChangesReason
		}
		out = w
	case "reviewer":
		issues := make([]map[string]any, 0, len(r.Issues))
		for _, i := range r.Issues {
			sev := strings.ToLower(strings.TrimSpace(i.Severity))
			switch sev {
			case "minor", "major", "blocker":
			case "critical":
				sev = "blocker"
			default:
				sev = "major"
			}
			m := map[string]any{"description": i.Description, "severity": sev}
			if i.File != "" {
				m["file"] = i.File
			}
			if i.Line > 0 {
				m["line"] = i.Line
			}
			issues = append(issues, m)
		}
		rv := map[string]any{"summary": summary, "issues": issues}
		if r.Status == StatusBlocked {
			rv["blocked"] = firstNonEmpty(r.Blocked, summary)
			break
		}
		verdict := strings.ToLower(strings.TrimSpace(r.Verdict))
		switch {
		case verdict == "pass":
		case verdict == "repair" || strings.HasPrefix(verdict, "changes"):
			verdict = "repair"
		default:
			return "", fmt.Errorf("seat review: verdict must be pass or repair, got %q", r.Verdict)
		}
		rv["verdict"] = verdict
		repair := strings.TrimSpace(r.RepairPrompt)
		if verdict == "repair" && repair == "" {
			var b strings.Builder
			b.WriteString("Fix what the reviewing seat found: " + summary)
			for _, i := range r.Issues {
				b.WriteString("\n- " + i.Description)
			}
			repair = b.String()
		}
		rv["repair_prompt"] = repair
		out = rv
	default:
		if len(r.Output) == 0 {
			return "", fmt.Errorf("seat report for a %s stage needs the role's object in \"output\"", role)
		}
		return string(r.Output), nil
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// Instructions is the closing section of a seat's prompt: where to write the report and
// what to put in it for the stage's role.
func Instructions(role, resultPath string) string {
	var b strings.Builder
	b.WriteString("\n\n---\n## How to finish (agentflow seat)\n\n")
	b.WriteString("You are working as an agentflow seat. Do not commit, push or switch branches: leave your changes in the working tree, agentflow commits them and runs its own verify and review.\n\n")
	fmt.Fprintf(&b, "When you stop, write the file `%s` with ONE JSON object, then stop and wait.\n\n", resultPath)
	switch role {
	case "worker", "cleanup":
		b.WriteString("Finished:\n```json\n{\"status\":\"done\",\"summary\":\"what you did\",\"files_changed\":[\"path/one.go\"],\"branch\":\"<branch>\",\"commit\":\"\"}\n```\n")
	case "reviewer":
		b.WriteString("Finished reviewing (verdict is \"pass\" or \"repair\"; severity is minor, major or blocker):\n```json\n{\"status\":\"done\",\"verdict\":\"pass\",\"summary\":\"why\",\"issues\":[{\"description\":\"...\",\"severity\":\"major\",\"file\":\"a.go\",\"line\":12}],\"repair_prompt\":\"what to fix, when repair\"}\n```\n")
	default:
		b.WriteString("Finished: `{\"status\":\"done\",\"output\":{ ...the JSON object the instructions above ask for... }}`\n")
	}
	b.WriteString("\nYou need the owner to decide something before you can go on:\n```json\n{\"status\":\"question\",\"question\":\"the exact question\"}\n```\nThe answer is typed into this session; carry on when it arrives, and write the file again when you stop.\n\n")
	b.WriteString("You cannot do the task at all:\n```json\n{\"status\":\"blocked\",\"blocked\":\"the exact reason\"}\n```\n")
	return b.String()
}
