package agentflow

import (
	"context"
	"log"

	"cmd184psu/unified-webapp/internal/agentflow/botchat"
	"cmd184psu/unified-webapp/internal/agentflow/coord"
	"cmd184psu/unified-webapp/internal/agentflow/notify"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// newCoord builds the external seats module (AF-13) from <data dir>/coord.json; nil when the
// file is absent, unreadable, or there is no terminal seat runner (tests).
func newCoord(dataDir string, ss *seats, st *store.Store) *coord.Service {
	cfg, err := coord.Load(dataDir)
	if err != nil {
		log.Printf("agentflow: coordinator module off: %v", err)
		return nil
	}
	if cfg == nil || ss == nil {
		return nil
	}
	client := &botchat.Client{BaseURL: cfg.BotChatURL, KeyFile: cfg.KeyFile}
	c := &coord.Coordinator{Cfg: cfg, Bus: coord.HTTPBus{C: client}, Git: coord.ExecGit{Repos: cfg.Repos, MergeDir: cfg.MergeDir}, Store: st}
	if cfg.ReviewMode == "agentflow" && cfg.AgentflowURL != "" {
		c.Reviews = &coord.HTTPReviewClient{BaseURL: cfg.AgentflowURL}
	}
	svc := &coord.Service{
		Cfg:   cfg,
		Dir:   dataDir,
		Coord: c,
		Watch: &coord.Watcher{Cfg: cfg, Screens: ss.term, Notify: func(ctx context.Context, text string) {
			set, err := st.GetSettings(ctx)
			if err != nil {
				return
			}
			if err := notify.Deliver(ctx, notify.Config{MacOS: set.NotifyMacOS, Command: set.NotifyCommand, URL: set.NotifyURL}, text); err != nil {
				log.Printf("coord: notify: %v", err)
			}
		}},
	}
	log.Printf("agentflow: coordinator module on (enabled=%v, dry_run=%v, %d seat(s))", cfg.Enabled, cfg.DryRun, len(cfg.Seats))
	return svc
}
