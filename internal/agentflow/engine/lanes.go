package engine

import (
	"context"
	"errors"
	"fmt"

	"cmd184psu/unified-webapp/internal/agentflow/store"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
)

const (
	// LaneOwner owns every agentflow lane.
	LaneOwner = "agentflow"
	// VerifyLane and MergeLane are the engine lanes. Their underscore keeps
	// them from ever colliding with a backend lane, whose names use a hyphen.
	VerifyLane = "agentflow_verify"
	MergeLane  = "agentflow_merge"

	lanePrefix     = "agentflow-"
	pausedByOwner  = "owner:" + LaneOwner
	laneRetentionD = 10
)

// BackendLane is the lane name of a backend (MB3, BK6).
func BackendLane(backend string) string { return lanePrefix + backend }

// laneWant is the state agentflow wants a lane in.
type laneWant struct {
	name   string
	kinds  []golane.Kind
	width  int
	hidden bool
	paused bool
}

// Lane returns the handle of a registered agentflow lane (retired ones too).
func (e *Engine) Lane(name string) (golane.Lane, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	l, ok := e.handles[name]
	return l, ok
}

// SyncLanes registers any lane that has no handle yet and re-asserts the
// desired width, visibility and pause of every agentflow lane (BK2, BK3, RC4).
// The agentflow DB is authoritative for slots. Safe to call at any time.
func (e *Engine) SyncLanes(ctx context.Context) error {
	snap, err := e.deps.Store.Snapshot(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	wants := []laneWant{
		{name: VerifyLane, kinds: e.engineKinds, width: snap.Settings.VerifySlots, paused: e.holding},
		{name: MergeLane, kinds: e.engineKinds, width: 1, paused: e.holding},
	}
	for _, b := range snap.Backends {
		wants = append(wants, backendWant(b, e.holding, e.backendKinds))
	}
	var errs []error
	for _, w := range wants {
		if err := e.apply(w); err != nil {
			errs = append(errs, fmt.Errorf("lane %s: %w", w.name, err))
		}
	}
	return errors.Join(errs...)
}

func backendWant(b store.Backend, holding bool, kinds []golane.Kind) laneWant {
	return laneWant{
		name: BackendLane(b.Name), kinds: kinds, width: b.Slots,
		hidden: b.Retired, paused: b.Retired || holding,
	}
}

// apply registers the lane if needed and moves it toward w. While the brake is
// engaged pause state is left alone; a pause is only lifted when agentflow
// set it (S6).
func (e *Engine) apply(w laneWant) error {
	l, ok := e.handles[w.name]
	if !ok {
		initial := w.width
		if e.deps.TestWidthZero {
			initial = 0
		}
		var err error
		l, err = e.deps.Host.RegisterLane(golane.LaneSpec{
			Name: w.name, Owner: LaneOwner,
			InitialWidth: initial, InitialRetentionDays: laneRetentionD, InitialHidden: w.hidden,
		}, w.kinds...)
		if err != nil {
			return err
		}
		e.handles[w.name] = l
	}
	st, err := l.Settings()
	if err != nil {
		return err
	}
	var patch golane.SettingsPatch
	changed := false
	if !e.deps.TestWidthZero && w.width >= 1 && st.Width != w.width {
		patch.Width, changed = &w.width, true
	}
	if st.Hidden != w.hidden {
		patch.Hidden, changed = &w.hidden, true
	}
	if !st.BrakeEngaged {
		switch {
		case w.paused && !st.Paused:
			patch.Paused, changed = &w.paused, true
		case !w.paused && st.Paused && st.PausedBy == pausedByOwner:
			patch.Paused, changed = &w.paused, true
		}
	}
	if !changed {
		return nil
	}
	_, err = l.UpdateSettings(patch)
	return err
}
