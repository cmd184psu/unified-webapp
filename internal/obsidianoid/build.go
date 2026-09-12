package obsidianoid

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
)

// watcherHandler pairs the module's mux with the vault watchers Build
// started so the process owner can shut them down (io.Closer is the
// dispatcher's optional shutdown hook). Plain http.Handler use is unaffected.
type watcherHandler struct {
	http.Handler
	watchers []io.Closer
}

func (h watcherHandler) Close() error {
	for _, w := range h.watchers {
		_ = w.Close()
	}
	return nil
}

// Build returns a ready-to-use http.Handler for the obsidianoid module. The
// handler also implements io.Closer; Close stops the per-vault fsnotify
// watcher goroutines Build starts.
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
	var watchers []io.Closer
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
		watchers = append(watchers, closer)
	}

	h := NewHandler(cfg, state, brokers)
	mux := http.NewServeMux()
	h.Register(mux)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	return watcherHandler{Handler: mux, watchers: watchers}, nil
}
