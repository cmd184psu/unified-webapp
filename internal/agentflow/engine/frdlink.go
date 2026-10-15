package engine

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
)

var reFRDRef = regexp.MustCompile(`\b(R\d+|AC\d+)\b`)

// frdSection is the FRD text behind a task's criteria, word for word (H1). A
// criterion such as "R3: ..." is a paraphrase by the compiler; the section
// puts the requirement the person approved next to it, with its exact paths,
// ports and messages, so a worker or reviewer does not rely on the paraphrase.
// It is empty when the task names no requirement or the effort has no FRD.
func (e *Engine) frdSection(ctx context.Context, task model.Task) string {
	effort, err := e.deps.Store.QueueEffort(ctx, task.Queue)
	if err != nil {
		return ""
	}
	d, err := e.deps.Store.GetDoc(ctx, effort, model.DocFRD, 0)
	if err != nil {
		return ""
	}
	var f model.FRD
	if json.Unmarshal(d.JSON, &f) != nil {
		return ""
	}
	want := map[string]bool{}
	for _, a := range task.Acceptance {
		for _, id := range reFRDRef.FindAllString(a, -1) {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return ""
	}
	var b strings.Builder
	for _, r := range f.Requirements {
		linked := false
		for _, ac := range f.AcceptanceCriteria {
			if want[ac.ID] {
				for _, rid := range ac.Requirements {
					linked = linked || rid == r.ID
				}
			}
		}
		if want[r.ID] || linked {
			b.WriteString("- " + r.ID + ": " + r.Description + "\n")
		}
	}
	for _, ac := range f.AcceptanceCriteria {
		if want[ac.ID] {
			b.WriteString("- " + ac.ID + ": " + ac.Criterion + "\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n## The approved requirements behind these criteria\n\nThis is the person's wording. Where it names an exact path, port, status code or message, the work must use exactly that.\n" + b.String()
}
