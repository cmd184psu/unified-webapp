// Package botchat is the chat module. It serves the API and the embedded UI
// on whatever hostname host_routing sends here. The process listener belongs
// to puma-unified; this package does not bind a port.
package botchat

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"cmd184psu/unified-webapp/internal/botchat/apikey"
	"cmd184psu/unified-webapp/internal/botchat/auth"
	"cmd184psu/unified-webapp/internal/botchat/chat"
	"cmd184psu/unified-webapp/internal/botchat/config"
	"cmd184psu/unified-webapp/internal/botchat/server"
)

// App is the module handler. Close stops the message sweeper and closes the
// chat store.
type App struct {
	handler http.Handler
	cancel  context.CancelFunc
	done    chan struct{}
	store   interface{ Close() error }
}

// Build opens the chat store, starts the hourly sweeper, and returns the
// HTTP handler. cfg comes from the botchat section of ~/.puma-unified.json.
// The watcher binary is not part of this process.
func Build(cfg config.Config) (http.Handler, error) {
	if err := config.Finalize(&cfg); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return nil, fmt.Errorf("botchat: create data dir %q: %w", cfg.DataDir, err)
	}

	keySvc, err := apikey.NewService(filepath.Join(cfg.DataDir, "apikeys.json"))
	if err != nil {
		return nil, fmt.Errorf("botchat: init api key service: %w", err)
	}

	retention := time.Duration(cfg.Chat.MessageRetentionDays) * 24 * time.Hour
	store, err := chat.OpenStore(filepath.Join(cfg.DataDir, "chat.db"), retention)
	if err != nil {
		return nil, fmt.Errorf("botchat: open chat store: %w", err)
	}
	if cfg.Chat.TaskIDPrefix != "" {
		store.TaskIDPrefix = cfg.Chat.TaskIDPrefix
	}
	store.ExclusiveTaskPerAssignee = cfg.Chat.ExclusiveTaskPerAssignee
	store.CallsignLeaseTTL = time.Duration(cfg.Chat.CallsignLeaseMinutes) * time.Minute
	store.AckThreshold = time.Duration(cfg.Chat.AssignmentAckMinutes) * time.Minute
	hub := chat.NewHub()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runMessageSweeper(ctx, store)
	}()

	srv := server.New(server.Options{
		Auth:          auth.NoAuth{},
		KeySvc:        keySvc,
		Store:         store,
		Hub:           hub,
		WebhookURL:    cfg.Webhook.URL,
		WebhookSecret: cfg.Webhook.Secret,
	})

	slog.Info("botchat module ready",
		"dataDir", cfg.DataDir,
	)

	return &App{
		handler: srv.Handler(),
		cancel:  cancel,
		done:    done,
		store:   store,
	}, nil
}

// ServeHTTP serves the botchat API and UI.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}

// Close stops the sweeper and closes the store.
func (a *App) Close() error {
	a.cancel()
	select {
	case <-a.done:
	case <-time.After(5 * time.Second):
	}
	return a.store.Close()
}

func runMessageSweeper(ctx context.Context, store *chat.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := store.PurgeExpired(); err != nil {
				slog.Error("botchat: purge expired messages", "err", err)
			}
		}
	}
}
