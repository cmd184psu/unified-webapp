package todo

import (
	"net/http"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Build returns a ready-to-use http.Handler for the todo module.
func Build(cfg config.TodoConfig) (http.Handler, error) {
	store, err := NewStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	mbr := broker.NewMultiRoomBroker(cfg.SyncIntervalSeconds * 1000)
	mbr.SetMaxSubscribers(cfg.SSEMaxSubscribers)
	h := NewHandler(store, mbr, cfg)

	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
