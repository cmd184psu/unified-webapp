package taskmaster

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
	"cmd184psu/unified-webapp/internal/taskmaster/coordinator"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"
)

// pruneInterval controls how often the retention pruner sweeps owned lanes.
// Unexported so tests can shorten it via SetPruneIntervalForTest; production
// code always uses the 1-hour default (P10).
var pruneInterval = time.Hour

// SetPruneIntervalForTest overrides pruneInterval for tests. Not for
// production use.
func SetPruneIntervalForTest(d time.Duration) {
	pruneInterval = d
}

var laneNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
var kindNameRe = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9_-]+)+$`)

// OpenOptions configures Open beyond what config.TaskmasterConfig carries.
type OpenOptions struct {
	// OwnedLanesOnly (P16): the worker schedules only lanes with
	// owner != ""; seedLanes is skipped; Handler() returns nil. Used when a
	// module is routed but taskmaster itself is not.
	OwnedLanesOnly bool
}

// Engine is the shared taskmaster engine: one DB, one worker, one HTTP
// handler (unless OwnedLanesOnly), shared by every module that registers a
// lane through RegisterLane.
type Engine struct {
	db       *db.DB
	worker   *worker.Worker
	funcs    *worker.FuncRegistry
	hidden   *worker.HiddenLanes
	progress *worker.ProgressRegistry
	cancels  *worker.CancelRegistry
	brake    *worker.BrakeGate
	procs    *worker.ProcessRegistry
	board    *broker.Broker
	registry *worker.OutputRegistry
	router   http.Handler

	stopGC      func()
	cancelCtx   context.CancelFunc
	workerDone  chan struct{}
	pruneCancel context.CancelFunc
	pruneDone   chan struct{}
	closeOnce   sync.Once
	closeErr    error

	lanesMu sync.Mutex
	lanes   map[string]bool
}

// Open builds the shared engine: everything Build did previously, plus
// module-owned lane/func-task support (§4.5). cfg.ProgressIntervalMs <= 0
// falls back to the 2000ms floor (Q3).
func Open(cfg config.TaskmasterConfig, opts OpenOptions) (*Engine, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, err
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	if !opts.OwnedLanesOnly {
		if err := seedLanes(database, cfg.Lanes); err != nil {
			database.Close()
			return nil, err
		}
	}

	// A fresh process's registries start empty — see build.go's original
	// comment (preserved verbatim in behavior): boot-time reconciliation
	// still runs over every row regardless of OwnedLanesOnly, since a
	// still-genuinely-alive shell process must not be silently abandoned
	// just because this boot happens not to serve the shell UI.
	if skipped, failed, err := worker.ReconcileOrphans(database); err != nil {
		database.Close()
		return nil, err
	} else if skipped > 0 || failed > 0 {
		log.Printf("taskmaster: boot reconciliation — %d execution(s) still genuinely running (left untouched), %d confirmed dead (marked failed)", skipped, failed)
	}

	allowSudo := cfg.AllowSudo
	if v, ok, err := database.GetSetting(db.SettingAllowSudo); err != nil {
		database.Close()
		return nil, err
	} else if ok {
		allowSudo = db.DecodeBoolSetting(v)
	} else if err := database.SetSetting(db.SettingAllowSudo, db.EncodeBoolSetting(cfg.AllowSudo)); err != nil {
		database.Close()
		return nil, err
	}
	sudoGate := worker.NewSudoGate(allowSudo)

	brakeEngaged := false
	if v, ok, err := database.GetSetting(db.SettingBrakeEngaged); err != nil {
		database.Close()
		return nil, err
	} else if ok {
		brakeEngaged = db.DecodeBoolSetting(v)
	} else if err := database.SetSetting(db.SettingBrakeEngaged, db.EncodeBoolSetting(false)); err != nil {
		database.Close()
		return nil, err
	}
	brakeGate := worker.NewBrakeGate(brakeEngaged)
	if brakeEngaged {
		log.Printf("taskmaster: booted with hand brake ENGAGED")
	}

	cancels := worker.NewCancelRegistry()
	procs := worker.NewProcessRegistry()

	hiddenNames, err := database.ListHiddenLanes()
	if err != nil {
		database.Close()
		return nil, err
	}
	hidden := worker.NewHiddenLanes(hiddenNames)

	boardBroker := broker.NewBroker(0)
	boardBroker.SetMaxSubscribers(cfg.SSEMaxSubscribers)

	outputRegistry := worker.NewRegistry()
	stopGC := outputRegistry.StartGC(time.Hour)

	funcs := worker.NewFuncRegistry()
	progress := worker.NewProgressRegistry()

	workerID, _ := os.Hostname()
	w := worker.New(database, outputRegistry, workerID, sudoGate, cancels, brakeGate, procs, boardBroker)

	progressInterval := time.Duration(cfg.ProgressIntervalMs) * time.Millisecond
	if progressInterval <= 0 {
		progressInterval = 2000 * time.Millisecond
	}
	w.EnableFuncTasks(worker.FuncOptions{
		Funcs:            funcs,
		Progress:         progress,
		Hidden:           hidden,
		ProgressInterval: progressInterval,
		OwnedLanesOnly:   opts.OwnedLanesOnly,
	})

	e := &Engine{
		db:       database,
		worker:   w,
		funcs:    funcs,
		hidden:   hidden,
		progress: progress,
		cancels:  cancels,
		brake:    brakeGate,
		procs:    procs,
		board:    boardBroker,
		registry: outputRegistry,
		stopGC:   stopGC,
		lanes:    make(map[string]bool),
	}

	// Retention pruning (P10): once at Open, then on a ticker.
	e.pruneOnce()
	pruneCtx, pruneCancel := context.WithCancel(context.Background())
	pruneDone := make(chan struct{})
	go func() {
		defer close(pruneDone)
		ticker := time.NewTicker(pruneInterval)
		defer ticker.Stop()
		for {
			select {
			case <-pruneCtx.Done():
				return
			case <-ticker.C:
				e.pruneOnce()
			}
		}
	}()
	e.pruneCancel = pruneCancel
	e.pruneDone = pruneDone

	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		w.Start(ctx)
	}()
	log.Printf("taskmaster: worker started (poll 5s, db %s, allow_sudo=%v)", cfg.DBPath, allowSudo)
	e.cancelCtx = cancel
	e.workerDone = workerDone

	if !opts.OwnedLanesOnly {
		c := coordinator.New(database, outputRegistry, sudoGate, cancels, brakeGate, procs, cfg.SSEMaxSubscribers, boardBroker, hidden, progress)
		r := coordinator.Routes(c)
		r.Handle("/*", static.NewHandler(cfg.StaticDir))
		e.router = r
	}

	return e, nil
}

// Handler returns the coordinator + static routes, or nil when the engine
// was opened with OwnedLanesOnly (a headless engine has no HTTP surface of
// its own).
func (e *Engine) Handler() http.Handler { return e.router }

// Close stops the worker and pruner goroutines, joins every runTask
// goroutine, stops the output registry's GC and closes the DB. Safe to call
// more than once (only the first call does anything).
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		e.cancelCtx()
		<-e.workerDone
		e.worker.Wait()
		e.pruneCancel()
		<-e.pruneDone
		e.stopGC()
		e.closeErr = e.db.Close()
	})
	return e.closeErr
}

func (e *Engine) pruneOnce() {
	lanes, err := e.db.ListRetentionLanes()
	if err != nil {
		log.Printf("list retention lanes: %v", err)
		return
	}
	for _, l := range lanes {
		cutoff := time.Now().AddDate(0, 0, -l.Days)
		n, err := e.db.PruneOwnedLane(l.Name, cutoff)
		if err != nil {
			log.Printf("prune lane %q: %v", l.Name, err)
			continue
		}
		if n > 0 {
			log.Printf("taskmaster: pruned %d job(s) from lane %q finished before %s", n, l.Name, cutoff.Format(time.RFC3339))
		}
	}
}

// RegisterLane validates spec/kinds, seeds (or attaches to) the owned lane,
// registers every kind, and returns a golane.Lane handle. A second call for
// the same lane name in this process returns golane.ErrLaneRegistered.
func (e *Engine) RegisterLane(spec golane.LaneSpec, kinds ...golane.Kind) (golane.Lane, error) {
	if err := validateLaneSpec(spec, kinds); err != nil {
		return nil, err
	}

	e.lanesMu.Lock()
	if e.lanes[spec.Name] {
		e.lanesMu.Unlock()
		return nil, golane.ErrLaneRegistered
	}
	e.lanes[spec.Name] = true
	e.lanesMu.Unlock()

	l, err := e.db.EnsureOwnedLane(spec.Name, spec.Owner, spec.InitialWidth, spec.InitialRetentionDays, spec.InitialHidden)
	if err != nil {
		return nil, err
	}
	for _, k := range kinds {
		if err := e.funcs.Register(k); err != nil {
			return nil, err
		}
	}
	e.hidden.Set(spec.Name, l.Hidden)
	log.Printf("taskmaster: lane %q registered by %q (kinds %v, width %d, retention %dd, hidden %v)",
		spec.Name, spec.Owner, kindNames(kinds), l.Width, l.RetentionDays, l.Hidden)

	return &laneHandle{engine: e, name: spec.Name, owner: spec.Owner, kinds: kindSet(kinds)}, nil
}

func validateLaneSpec(spec golane.LaneSpec, kinds []golane.Kind) error {
	if !laneNameRe.MatchString(spec.Name) {
		return fmt.Errorf("%w: invalid lane name %q", golane.ErrInvalidSettings, spec.Name)
	}
	if !laneNameRe.MatchString(spec.Owner) {
		return fmt.Errorf("%w: invalid owner name %q", golane.ErrInvalidSettings, spec.Owner)
	}
	if spec.InitialWidth < 0 || spec.InitialRetentionDays < 0 {
		return fmt.Errorf("%w: width and retention must be >= 0", golane.ErrInvalidSettings)
	}
	if len(kinds) == 0 {
		return fmt.Errorf("%w: at least one kind is required", golane.ErrInvalidSettings)
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		if !kindNameRe.MatchString(k.Name) {
			return fmt.Errorf("%w: invalid kind name %q", golane.ErrInvalidSettings, k.Name)
		}
		if k.Version < 1 {
			return fmt.Errorf("%w: kind %q must declare Version >= 1", golane.ErrInvalidSettings, k.Name)
		}
		if k.Decode == nil || k.Run == nil {
			return fmt.Errorf("%w: kind %q must set Decode and Run", golane.ErrInvalidSettings, k.Name)
		}
		if seen[k.Name] {
			return fmt.Errorf("%w: duplicate kind name %q", golane.ErrInvalidSettings, k.Name)
		}
		seen[k.Name] = true
	}
	return nil
}

func kindSet(kinds []golane.Kind) map[string]golane.Kind {
	m := make(map[string]golane.Kind, len(kinds))
	for _, k := range kinds {
		m[k.Name] = k
	}
	return m
}

func kindNames(kinds []golane.Kind) []string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = k.Name
	}
	return names
}

// seedLanes upserts each configured lane into the DB at startup, ported
// from reference/continuous-task-runner-queue/cmd/ctrq.go:76-89 (originally
// "groups"; renamed to lanes per taskmaster-ui-plan.md D1). The DB is
// authoritative thereafter — this only seeds/updates name/width.
//
// P19: a config lane that collides by name with an already-owned lane is
// skipped (never overwritten), since UpsertLane would otherwise clobber a
// module's width on every boot.
func seedLanes(database *db.DB, lanes []config.TaskmasterLane) error {
	for _, lc := range lanes {
		existing, err := database.GetLane(lc.Name)
		if err != nil {
			return err
		}
		if existing != nil && existing.Owner != "" {
			log.Printf("taskmaster: config lane %q is owned by module %q; ignoring its config entry", lc.Name, existing.Owner)
			continue
		}
		l := &models.Lane{
			Name:  lc.Name,
			Width: lc.Width,
		}
		if err := database.UpsertLane(l); err != nil {
			return err
		}
	}
	return nil
}
