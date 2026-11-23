package store

import (
	"context"
	"fmt"
	"time"
)

// RecordStageTokens stores the runner-reported token usage of a stage (AT12).
// It is written before the stage's outcome, so the dispatcher, which is kicked
// after the outcome, always sees it.
func (s *Store) RecordStageTokens(ctx context.Context, stage, tokens int64) error {
	if tokens < 0 {
		return Invalid("tokens", "must not be negative")
	}
	res, err := s.db.ExecContext(ctx, `UPDATE stages SET tokens = ? WHERE id = ?`, tokens, stage)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("stage %d: %w", stage, ErrNotFound)
	}
	return nil
}

// EffortUsage totals what an effort has consumed: runner tokens, and the
// seconds its stages have run (stages in parallel add up; a stage still
// running counts up to nowMS). Both cover the effort's own stages (plan,
// review, compile) and those of every attempt of its tasks.
// BackendUsage is what a backend's stages spent in the current UTC day and
// month (AF-20), with the window starts and resets as unix ms. A stage counts
// when it finished (or started, while it is still running) inside the window.
type BackendUsage struct {
	Backend     string
	DayTokens   int64
	MonthTokens int64
	DayStart    int64
	MonthStart  int64
	DayReset    int64
	MonthReset  int64
}

// BackendUsage totals a backend's runner tokens for the current UTC day and
// month. The stages table already records the tokens of every stage.
func (s *Store) BackendUsage(ctx context.Context, backend string, now time.Time) (BackendUsage, error) {
	now = now.UTC()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	u := BackendUsage{Backend: backend, DayStart: day.UnixMilli(), MonthStart: month.UnixMilli(),
		DayReset: day.AddDate(0, 0, 1).UnixMilli(), MonthReset: month.AddDate(0, 1, 0).UnixMilli()}
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN spent >= ? THEN tokens ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN spent >= ? THEN tokens ELSE 0 END), 0)
		FROM (SELECT tokens, COALESCE(NULLIF(finished, 0), started, 0) AS spent
		      FROM stages WHERE backend = ? AND tokens > 0)`,
		u.DayStart, u.MonthStart, backend).Scan(&u.DayTokens, &u.MonthTokens)
	return u, err
}

// AtCap reports whether the usage has reached the backend's daily or monthly
// token cap, the unix ms when the reached window resets, and which cap
// ("daily", "monthly", or "daily and monthly"). A 0 cap means no cap.
func (u BackendUsage) AtCap(dailyTokenCap, monthlyTokenCap int64) (bool, int64, string) {
	var until int64
	var why []string
	if dailyTokenCap > 0 && u.DayTokens >= dailyTokenCap {
		until = max(until, u.DayReset)
		why = append(why, "daily")
	}
	if monthlyTokenCap > 0 && u.MonthTokens >= monthlyTokenCap {
		until = max(until, u.MonthReset)
		why = append(why, "monthly")
	}
	if until == 0 {
		return false, 0, ""
	}
	w := why[0]
	if len(why) == 2 {
		w = "daily and monthly"
	}
	return true, until, w
}

func (s *Store) EffortUsage(ctx context.Context, effort, nowMS int64) (tokens, seconds int64, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(tokens), 0),
		       COALESCE(SUM(CASE WHEN started = 0 THEN 0
		                         WHEN finished > 0 THEN MAX(finished - started, 0)
		                         ELSE MAX(? - started, 0) END), 0) / 1000
		FROM stages
		WHERE effort = ?
		   OR attempt IN (SELECT a.id FROM attempts a
		                    JOIN tasks t ON t.id = a.task
		                    JOIN queues q ON q.id = t.queue
		                   WHERE q.effort = ?)`, nowMS, effort, effort).Scan(&tokens, &seconds)
	return tokens, seconds, err
}
