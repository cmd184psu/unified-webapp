package rulings

import "encoding/json"

// ParseReviewIssues extracts the reviewer's issues list from a parsed reviewer
// final message (json.RawMessage of the ReviewOut), so the engine can pass it
// to ContradictsRuling without importing the schema package here.
func ParseReviewIssues(parsed json.RawMessage) Issues {
	if len(parsed) == 0 {
		return Issues{}
	}
	var raw struct {
		Issues []struct {
			Description string `json:"description"`
		} `json:"issues"`
		RepairPrompt string `json:"repair_prompt"`
	}
	if jsonUnmarshal(parsed, &raw) != nil {
		return Issues{}
	}
	out := Issues{RepairPrompt: raw.RepairPrompt}
	for _, is := range raw.Issues {
		if is.Description != "" {
			out.Issues = append(out.Issues, SchemaIssue{Description: is.Description})
		}
	}
	return out
}
