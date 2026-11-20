package engine

import (
	"context"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/model"
	"cmd184psu/unified-webapp/internal/agentflow/schema"
)

// AF-13 3b: every exhausted task of 11-19/20 failed the same way attempt after attempt. Two
// consecutive attempts with the same fingerprint now stop the loop (stuck_threshold defaults
// to 2, migration 39), and a reviewer who raises the same finding again in other words counts
// as the same failure: the attempt takes the earlier attempt's fingerprint.

// sameFinding reports whether two review issues are the same finding: the same file, lines
// within ten of each other when both name one, and descriptions sharing at least half their
// words.
func sameFinding(a, b schema.Issue) bool {
	if a.File == "" || a.File != b.File {
		return false
	}
	if a.Line > 0 && b.Line > 0 && (a.Line-b.Line > 10 || b.Line-a.Line > 10) {
		return false
	}
	return wordOverlap(a.Description, b.Description) >= 0.5
}

func wordOverlap(a, b string) float64 {
	words := func(s string) map[string]bool {
		m := map[string]bool{}
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
		}) {
			if len(w) > 2 {
				m[w] = true
			}
		}
		return m
	}
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	n := 0
	for w := range wa {
		if wb[w] {
			n++
		}
	}
	small := len(wa)
	if len(wb) < small {
		small = len(wb)
	}
	return float64(n) / float64(small)
}

// repeatedReview returns the previous attempt's fingerprint when it too failed review and its
// reviewer raised a finding this review raises again; "" otherwise.
func (e *Engine) repeatedReview(ctx context.Context, c *chain, review model.Stage) string {
	cur, err := schema.Parse[schema.ReviewOut](review.Parsed)
	if err != nil || len(cur.Issues) == 0 {
		return ""
	}
	atts, err := e.deps.Store.ListAttempts(ctx, c.task.ID)
	if err != nil {
		return ""
	}
	var prev *model.Attempt
	for i := range atts {
		if atts[i].N == c.att.N-1 {
			prev = &atts[i]
		}
	}
	if prev == nil || prev.Outcome != model.OutcomeFailed || !strings.HasPrefix(prev.Fingerprint, "r:") {
		return ""
	}
	stages, err := e.deps.Store.ListAttemptStages(ctx, prev.ID)
	if err != nil {
		return ""
	}
	for i := len(stages) - 1; i >= 0; i-- {
		if stages[i].Role != "reviewer" {
			continue
		}
		old, err := schema.Parse[schema.ReviewOut](stages[i].Parsed)
		if err != nil {
			return ""
		}
		for _, a := range cur.Issues {
			for _, b := range old.Issues {
				if a.Severity != "minor" && sameFinding(a, b) {
					return prev.Fingerprint
				}
			}
		}
		return ""
	}
	return ""
}
