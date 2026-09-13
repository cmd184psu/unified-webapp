package taskmaster

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

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
	if err := seedGroups(database, cfg.Groups); err != nil {
		database.Close()
		return nil, err
	}

	registry := worker.NewRegistry()
	stopGC := registry.StartGC(time.Hour)

	workerID, _ := os.Hostname()
	w := worker.New(database, registry, workerID, cfg.AllowSudo)
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		w.Start(ctx)
	}()
	log.Printf("taskmaster: worker started (poll 5s, db %s, allow_sudo=%v)", cfg.DBPath, cfg.AllowSudo)

	c := coordinator.New(database, registry, cfg.AllowSudo, cfg.SSEMaxSubscribers)
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

// seedGroups upserts each configured group into the DB at startup, ported
// from reference/continuous-task-runner-queue/cmd/ctrq.go:76-89. The DB is
// authoritative thereafter — this only seeds/updates name/pool_limit/
// allowed_types.
func seedGroups(database *db.DB, groups []config.TaskmasterGroup) error {
	for _, gc := range groups {
		g := &models.Group{
			Name:         gc.Name,
			PoolLimit:    gc.PoolLimit,
			AllowedTypes: gc.AllowedTypes,
		}
		if g.AllowedTypes == nil {
			g.AllowedTypes = []string{}
		}
		if err := database.UpsertGroup(g); err != nil {
			return err
		}
	}
	return nil
}
