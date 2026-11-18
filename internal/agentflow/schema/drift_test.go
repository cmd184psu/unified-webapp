package schema

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/prompts"
)

var enumPlaceholder = regexp.MustCompile(`"([a-z_]+(?:\|[a-z_]+)+)"`)

// TestPromptContractsMatchGoTypes decodes the output-contract JSON example of
// each prompt into its Go type, so a prompt field that drifts from the schema
// fails here. Enum placeholders are replaced by their first value.
func TestPromptContractsMatchGoTypes(t *testing.T) {
	cases := []struct {
		role string
		dst  func() any
	}{
		{"worker", func() any { return new(WorkerOut) }},
		{"reviewer", func() any { return new(ReviewOut) }},
		{"planner", func() any { return new(PlanOut) }},
		{"architect", func() any { return new(PlanReviewOut) }},
		{"critic", func() any { return new(PlanReviewOut) }},
		{"compiler", func() any { return new(CompileOut) }},
		{"cleanup", func() any { return new(WorkerOut) }},
		{"drafter", func() any { return new(FRDDraft) }},
		{"acceptance", func() any { return new(AcceptanceOut) }},
	}
	if len(cases) != len(prompts.Roles) {
		t.Fatalf("%d cases, %d roles", len(cases), len(prompts.Roles))
	}
	for _, c := range cases {
		t.Run(c.role, func(t *testing.T) {
			text, err := prompts.Load(c.role)
			if err != nil {
				t.Fatal(err)
			}
			_, out, ok := strings.Cut(text, "## Output")
			if !ok {
				t.Fatal("prompt has no Output section")
			}
			out = enumPlaceholder.ReplaceAllStringFunc(out, func(m string) string {
				first, _, _ := strings.Cut(strings.Trim(m, `"`), "|")
				return `"` + first + `"`
			})
			raw, err := ExtractFinal(out)
			if err != nil {
				t.Fatalf("no example object in Output section: %v", err)
			}
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(c.dst()); err != nil {
				t.Fatalf("example does not decode into Go type: %v\n%s", err, raw)
			}
		})
	}
}
