package slideshow

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Build returns a ready-to-use http.Handler for the slideshow module.
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

	return mux, nil
}
