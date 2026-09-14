package taskmaster

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
	"cmd184psu/unified-webapp/internal/taskmaster/coordinator"
	"cmd184psu/unified-webapp/internal/taskmaster/db"
	"cmd184psu/unified-webapp/internal/taskmaster/models"
	"cmd184psu/unified-webapp/internal/taskmaster/worker"

	"net/http"
)

// closer pairs the module's router with a shutdown func so the process owner
// can stop the background worker/GC goroutines Build starts (io.Closer is
// the dispatcher's optional shutdown hook). Plain http.Handler use is
// unaffected. Mirrors the slideshow stoppableHandler pattern.
type closer struct {
	http.Handler
	close func() error
}

func (c *closer) Close() error {
	return c.close()
}

// Build returns a ready-to-use http.Handler for the taskmaster module. The
// handler also implements io.Closer; Close stops the worker/GC goroutines
// Build starts and closes the database.
func Build(cfg config.TaskmasterConfig) (http.Handler, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		return nil, err
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	if err := seedLanes(database, cfg.Lanes); err != nil {
		database.Close()
		return nil, err
	}

	// allow_sudo is DB-authoritative once seeded: the config value seeds the
	// DB on first boot, and the runtime UI toggle (POST /api/capabilities)
	// persists there and wins on every boot thereafter.
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

	// brake_engaged is DB-authoritative once seeded, same pattern as
	// allow_sudo: absent on first boot (seeded false), the runtime
	// /api/brake endpoints persist it thereafter and it wins on every boot
	// so a violent reboot doesn't silently release the hand brake.
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

	// boardBroker is the shared board-events broker (plan D7/B5): the worker
	// publishes task lifecycle events, the coordinator publishes lane/task/
	// brake mutation events, and GET /api/board/events streams them. It has
	// no background goroutines of its own — each subscriber's loop runs
	// inside that request's goroutine and exits when the request context is
	// done — so there is nothing extra to stop on Close().
	boardBroker := broker.NewBroker(0)
	boardBroker.SetMaxSubscribers(cfg.SSEMaxSubscribers)

	registry := worker.NewRegistry()
	stopGC := registry.StartGC(time.Hour)

	workerID, _ := os.Hostname()
	w := worker.New(database, registry, workerID, sudoGate, cancels, brakeGate, procs, boardBroker)
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		w.Start(ctx)
	}()
	log.Printf("taskmaster: worker started (poll 5s, db %s, allow_sudo=%v)", cfg.DBPath, allowSudo)

	c := coordinator.New(database, registry, sudoGate, cancels, brakeGate, procs, cfg.SSEMaxSubscribers, boardBroker)
	r := coordinator.Routes(c)
	r.Handle("/*", static.NewHandler(cfg.StaticDir))

	return &closer{Handler: r, close: func() error {
		cancel() // stops poll loop; kills running commands (CommandContext+WaitDelay)
		<-workerDone
		w.Wait() // join runTask goroutines — bounded, commands are dying
		stopGC() // stop registry GC
		return database.Close()
	}}, nil
}

// seedLanes upserts each configured lane into the DB at startup, ported
// from reference/continuous-task-runner-queue/cmd/ctrq.go:76-89 (originally
// "groups"; renamed to lanes per taskmaster-ui-plan.md D1). The DB is
// authoritative thereafter — this only seeds/updates name/width.
func seedLanes(database *db.DB, lanes []config.TaskmasterLane) error {
	for _, lc := range lanes {
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
