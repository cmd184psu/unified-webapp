package store

import "context"

// effortStages selects the ids of every stage of an effort: its planning and
// compile stages and those of its tasks' attempts.
const effortStages = `SELECT s.id FROM stages s WHERE s.effort = ?1 OR s.attempt IN (
  SELECT a.id FROM attempts a JOIN tasks t ON a.task = t.id JOIN queues q ON t.queue = q.id WHERE q.effort = ?1)`

// EffortInFlight says whether any stage of the effort is queued or running.
func (s *Store) EffortInFlight(ctx context.Context, effort int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stages WHERE state <> 'done' AND id IN (`+effortStages+`)`, effort).Scan(&n)
	return n > 0, err
}

// EffortLastActivity is the newest time, in milliseconds, that anything
// happened in an effort: an event about it, or a stage or attempt of it that
// started or finished (H30). It is 0 for an effort with no history.
func (s *Store) EffortLastActivity(ctx context.Context, effort int64) (int64, error) {
	var ms int64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(
  COALESCE((SELECT MAX(ts) FROM events WHERE subject_type = 'effort' AND subject_id = CAST(?1 AS TEXT)), 0),
  COALESCE((SELECT MAX(MAX(started), MAX(finished)) FROM stages WHERE id IN (`+effortStages+`)), 0),
  COALESCE((SELECT MAX(MAX(a.started), MAX(a.finished)) FROM attempts a JOIN tasks t ON a.task = t.id JOIN queues q ON t.queue = q.id WHERE q.effort = ?1), 0))`, effort).Scan(&ms)
	return ms, err
}
