package obsidianoid

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// Build returns a ready-to-use http.Handler for the obsidianoid module.
func Build(cfg config.ObsidianoidConfig) (http.Handler, error) {
	if len(cfg.Vaults) == 0 {
		return nil, fmt.Errorf("obsidianoid: no vaults configured")
	}

	// Apply safe defaults for zero-value fields.
	for i := range cfg.Vaults {
		if cfg.Vaults[i].Theme == "" {
			cfg.Vaults[i].Theme = "dark"
		}
	}
	if cfg.ThreadCount == 0 {
		cfg.ThreadCount = 4
	}
	if cfg.ThreadsFolder == "" {
		cfg.ThreadsFolder = "Threads"
	}

	state, err := NewStateStore(cfg.DataDir, cfg.ThreadCount)
	if err != nil {
		return nil, fmt.Errorf("obsidianoid: state store: %w", err)
	}

	brokers := make([]*broker.Broker, len(cfg.Vaults))
	for i, v := range cfg.Vaults {
		b := broker.NewBroker(0)
		b.SetMaxSubscribers(cfg.SSEMaxSubscribers)
		brokers[i] = b

		if _, err := os.Stat(v.Path); err != nil {
			log.Printf("obsidianoid: vault %q not found, skipping watcher: %v", v.Path, err)
			continue
		}
		closer, err := startVaultWatcher(v.Path, b)
		if err != nil {
			log.Printf("obsidianoid: watcher for vault %q failed: %v", v.Path, err)
			continue
		}
		_ = closer // process-lifetime watcher; never closed in production
	}

	h := NewHandler(cfg, state, brokers)
	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return mux, nil
}
