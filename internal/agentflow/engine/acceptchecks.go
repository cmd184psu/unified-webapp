package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"cmd184psu/unified-webapp/internal/agentflow/schema"
	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/agentflow/verify"
)

// AF-13 item 2 (fs3-peace/9, A1-6): the acceptance check opened a gap task "AC2: gofmt ... go vet
// and go test could not be verified". Workers are told not to run the project's checks, so its
// worker stopped as needs_input. The engine already runs those checks: each merge verifies the
// merged head with the project's feedback commands. The acceptance check is shown that report,
// and a gap that only says a check could not be run is dropped when the report passed.

// mergedHeadReport is the post-merge verify report of the effort's newest merge, or ok false
// when no merge has passed yet.
func (e *Engine) mergedHeadReport(ctx context.Context, effort int64) (verify.VerifyReport, bool, error) {
	st, err := e.deps.Store.LatestMergeStage(ctx, effort, OutcomePass)
	if errors.Is(err, store.ErrNotFound) {
		return verify.VerifyReport{}, false, nil
	}
	if err != nil {
		return verify.VerifyReport{}, false, err
	}
	var m mergeReport
	if json.Unmarshal(st.Parsed, &m) != nil || m.Report == nil {
		return verify.VerifyReport{}, false, nil
	}
	return *m.Report, true, nil
}

// feedbackChecks are the report's feedback-command checks.
func feedbackChecks(rep verify.VerifyReport) []verify.Check {
	var out []verify.Check
	for _, c := range rep.Checks {
		if strings.HasPrefix(c.Name, verify.CheckFeedbackPrefix) {
			out = append(out, c)
		}
	}
	return out
}

// engineChecksSection is the acceptance prompt's account of the engine's own checks.
func engineChecksSection(rep verify.VerifyReport, ok bool) string {
	checks := feedbackChecks(rep)
	if !ok || len(checks) == 0 {
		return "The project has no feedback commands, or no merge has been verified yet: judge every criterion from the code."
	}
	var b strings.Builder
	b.WriteString("After the last merge the engine ran the project's feedback commands on this merged result:\n\n")
	for _, c := range checks {
		res := "passed"
		if !c.Pass {
			res = "failed"
		}
		fmt.Fprintf(&b, "- `%s`: %s\n", strings.TrimPrefix(c.Name, verify.CheckFeedbackPrefix), res)
	}
	if rep.Pass {
		b.WriteString("\nThe merged result has no new failures against the effort's baseline.\n")
	} else {
		b.WriteString("\nThe merged result has new failures: " + strings.Join(rep.NewFailures, "; ") + "\n")
	}
	return b.String()
}

var (
	reUnverified = regexp.MustCompile(`(?i)\b(could ?n[o']t|cannot|can't|unable to|was not|were not|not been|no evidence (that|of)|did not|didn't)\s+(be\s+)?(independently\s+)?(verif|run|ran|execut|confirm|check|observ)`)
	reCheckWords = regexp.MustCompile(`(?i)\b(gofmt|go vet|go test|go build|vet|tests?|build|lint|format(ted|ting)?|dotnet|npm|eslint|tsc|pytest)\b`)
)

// coveredGap reports whether a gap only says that a check the engine runs could not be run or
// verified by the reviewer: with the merged head's report green, that criterion is verified.
func coveredGap(g schema.Gap) bool {
	text := g.Problem + " " + g.Evidence
	return reUnverified.MatchString(text) && reCheckWords.MatchString(text)
}

// dropCoveredGaps removes the gaps the engine's own checks already settle, when they passed.
func dropCoveredGaps(gaps []schema.Gap, rep verify.VerifyReport, ok bool) (kept, dropped []schema.Gap) {
	if !ok || !rep.Pass || len(feedbackChecks(rep)) == 0 {
		return gaps, nil
	}
	for _, g := range gaps {
		if coveredGap(g) {
			dropped = append(dropped, g)
		} else {
			kept = append(kept, g)
		}
	}
	return kept, dropped
}
