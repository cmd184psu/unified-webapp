package engine

import (
	"context"
	"fmt"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// A hosted model has no usage meter to read: the provider answers "rate limit
// exceeded" or goes quiet. So a model that does either is rested, the work goes to
// the next candidate, and the model is tried again when its rest ends. A repeat
// doubles the rest up to a cap; any stage the model answers clears it.
const (
	rateLimitRestBase = 5 * time.Minute
	rateLimitRestMax  = 60 * time.Minute
	stallRestBase     = 3 * time.Minute
	stallRestMax      = 20 * time.Minute
)

// noteRest rests or clears the model of a stage that just ended, by its outcome.
func (e *Engine) noteRest(ctx context.Context, stage int64, outcome string) error {
	st, err := e.deps.Store.GetStage(ctx, stage)
	if err != nil {
		return err
	}
	if st.Backend == "" || st.Model == "" || st.Backend == verifyBackend || st.Backend == mergeBackend {
		return nil
	}
	now := e.deps.Clock()
	switch outcome {
	case OutcomeInfraRateLimit:
		_, err = e.deps.Store.RestModel(ctx, st.Backend, st.Model, "rate limited", rateLimitRestBase, rateLimitRestMax, now)
	case OutcomeInfraTimeout, OutcomeTimeout:
		b, gerr := e.deps.Store.GetBackend(ctx, st.Backend)
		if gerr != nil || b.Host != "" {
			return gerr // a machine of your own that is slow is not being throttled
		}
		_, err = e.deps.Store.RestModel(ctx, st.Backend, st.Model, "stalled", stallRestBase, stallRestMax, now)
	default:
		if !IsInfra(outcome) && outcome != OutcomeDiscarded {
			_, err = e.deps.Store.ClearModelRest(ctx, st.Backend, st.Model)
		}
	}
	if err == nil && e.deps.Kick != nil && (outcome == OutcomeInfraRateLimit || outcome == OutcomeInfraTimeout || outcome == OutcomeTimeout) {
		e.deps.Kick()
	}
	return err
}

// restingUntil is when the candidate's model may take work again, or the zero time.
func restingUntil(v *view, c store.Candidate) (time.Time, string) {
	if r, ok := v.restModel[modelKey(c.Backend, c.Model)]; ok {
		return time.UnixMilli(r.Until), r.Reason
	}
	return time.Time{}, ""
}

func restReason(c store.Candidate, until time.Time, why string) string {
	return fmt.Sprintf("model %s on %s is resting until %s (%s)", c.Model, c.Backend, until.Format("15:04"), why)
}

func restTime(v *view, c store.Candidate) time.Time { t, _ := restingUntil(v, c); return t }
func restWhy(v *view, c store.Candidate) string     { _, w := restingUntil(v, c); return w }
