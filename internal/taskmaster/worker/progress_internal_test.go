package worker

import (
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"

	"github.com/stretchr/testify/require"
)

// newProgressTestReporter builds a progressReporter against a real func
// execution row on an in-memory DB, with a fake clock the test drives
// explicitly (R6) so the throttle assertions are deterministic.
func newProgressTestReporter(t *testing.T, interval time.Duration) (*progressReporter, *db.DB, int64, *time.Time) {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	require.NoError(t, d.UpsertLane(&models.Lane{Name: "owned", Width: 1, Owner: "acme-module"}))
	_, execID, err := d.SubmitFuncTask(db.FuncTaskSpec{Name: "owned-progress0001", Lane: "owned", Kind: "acme-module.echo", Payload: []byte(`{}`), PayloadVersion: 1})
	require.NoError(t, err)

	t0 := time.Time{}
	r := newProgressReporter(execID, "owned", "owned-progress0001", d, NewProgressRegistry(), nil, NewHiddenLanes(nil), interval)
	r.now = func() time.Time { return t0 }
	return r, d, execID, &t0
}

func TestProgressReporter_ThrottleAndFreshness(t *testing.T) {
	r, d, execID, t0 := newProgressTestReporter(t, 100*time.Millisecond)

	var hookCount int
	oldHook := progressPersistHook
	progressPersistHook = func(id int64, p models.Progress) { hookCount++ }
	t.Cleanup(func() { progressPersistHook = oldHook })

	for i := 0; i < 50; i++ {
		*t0 = t0.Add(10 * time.Millisecond)
		r.Report(i, "Downloading")

		p, ok := r.reg.Get(execID)
		require.True(t, ok)
		require.NotNil(t, p.Pct)
		require.Equal(t, i, *p.Pct)
	}

	require.Equal(t, 5, hookCount, "expected persists at offsets 10,110,210,310,410ms")

	// The DB holds the last PERSISTED value (offset 410ms => i=40), not the
	// last reported one (i=49, offset 500ms, which never reaches the next
	// 100ms threshold before the loop ends) — the unthrottled registry
	// (asserted per-iteration above) is what carries the freshest value.
	e, err := d.GetExecution(execID)
	require.NoError(t, err)
	require.NotNil(t, e.ProgressPct)
	require.Equal(t, 40, *e.ProgressPct)

	final := r.close()
	require.NotNil(t, final)
	require.Equal(t, 49, *final.Pct)

	r.Report(99, "ignored")
	require.Equal(t, 5, hookCount, "Report after close must be a no-op")
}

func TestProgressReporter_LabelChangePersistsImmediately(t *testing.T) {
	r, _, _, t0 := newProgressTestReporter(t, 10*time.Second)

	var hookCount int
	oldHook := progressPersistHook
	progressPersistHook = func(id int64, p models.Progress) { hookCount++ }
	t.Cleanup(func() { progressPersistHook = oldHook })

	*t0 = t0.Add(time.Millisecond)
	r.Report(10, "Downloading")
	require.Equal(t, 1, hookCount)

	*t0 = t0.Add(time.Millisecond)
	r.Report(-1, "Converting")
	require.Equal(t, 2, hookCount, "a label change must persist immediately even inside the interval")

	*t0 = t0.Add(time.Millisecond)
	r.Report(-1, "Converting")
	require.Equal(t, 2, hookCount, "a repeat of the same label inside the interval must not persist again")
}

func TestProgressReporter_NeverReportedCloseReturnsNil(t *testing.T) {
	r, _, _, _ := newProgressTestReporter(t, time.Second)
	require.Nil(t, r.close())
}
