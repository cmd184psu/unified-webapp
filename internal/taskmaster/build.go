package taskmaster

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/config"
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
// handler also implements io.Closer; Close stops the worker/GC/pruner
// goroutines Open starts and closes the database. Build is Open with
// OpenOptions{} (every route mounted, every lane in cfg.Lanes seeded), kept
// as the stable entry point every existing caller uses.
func Build(cfg config.TaskmasterConfig) (http.Handler, error) {
	e, err := Open(cfg, OpenOptions{})
	if err != nil {
		return nil, err
	}
	return &closer{Handler: e.Handler(), close: e.Close}, nil
}
