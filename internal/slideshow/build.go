package slideshow

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// stoppableHandler pairs the module's mux with the conductor's Stop so the
// process owner can end the background tick goroutine (io.Closer is the
// dispatcher's optional shutdown hook). Plain http.Handler use is unaffected.
type stoppableHandler struct {
	http.Handler
	stop func()
}

func (h stoppableHandler) Close() error {
	h.stop()
	return nil
}

// Build returns a ready-to-use http.Handler for the slideshow module. The
// handler also implements io.Closer; Close stops the conductor goroutine
// Build starts.
func Build(cfg config.SlideshowConfig) (http.Handler, error) {
	store, err := NewStore(cfg.ImageDir, cfg.AgeCutoffDays)
	if err != nil {
		return nil, err
	}
	music := NewMusicStore(cfg.Music.AudioDir)
	b := broker.NewBroker(0)
	b.SetMaxSubscribers(cfg.SSEMaxSubscribers)
	conductor := NewConductor(store, music, b, cfg)
	h := NewHandler(store, conductor, b, music, cfg)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	go conductor.Run()

	return stoppableHandler{Handler: mux, stop: conductor.Stop}, nil
}
