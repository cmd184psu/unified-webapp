package taskmaster

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/taskmaster/models"

	"github.com/stretchr/testify/require"
)

// TestRegisterLaneFailedKindNeverVisible: while RegisterLane fails at
// EnsureOwnedLane, the worker-visible registry never shows the new kind.
func TestRegisterLaneFailedKindNeverVisible(t *testing.T) {
	cfg := config.TaskmasterConfig{StaticDir: t.TempDir(), DBPath: filepath.Join(t.TempDir(), "tm.db"), ProgressIntervalMs: 50}
	seed, err := db.Open(cfg.DBPath)
	require.NoError(t, err)
	require.NoError(t, seed.UpsertLane(&models.Lane{Name: "cfg-lane", Width: 1}))
	require.NoError(t, seed.Close())

	e, err := Open(cfg, OpenOptions{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, e.Close()) })

	var seen atomic.Bool
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if e.funcs.Has("acme.fresh") {
					seen.Store(true)
				}
			}
		}
	}()

	k := golane.NewKind[struct{}]("acme.fresh", 1, nil, func(context.Context, golane.RunContext, struct{}) (any, error) { return nil, nil })
	for i := 0; i < 300; i++ {
		_, err := e.RegisterLane(golane.LaneSpec{Name: "cfg-lane", Owner: "acme", InitialWidth: 1}, k)
		require.ErrorIs(t, err, db.ErrLaneOwnedByOther)
	}
	close(stop)
	wg.Wait()
	require.False(t, seen.Load(), "kind of a failed RegisterLane must never be visible")
	require.False(t, e.funcs.Has("acme.fresh"))
}
