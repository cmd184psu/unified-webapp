package utuber

import (
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/platform/static"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/utuber/history"
	"cmd184psu/unified-webapp/internal/utuber/media"
)

// Build returns a ready-to-use http.Handler for the utuber module. host is the
// shared taskmaster engine: utuber registers a func-task lane on it and owns no
// queue state of its own (plan §6 Phase 5 / D8).
func Build(cfg config.UtuberConfig, host golane.Host) (http.Handler, error) {
	return buildWith(cfg, host, media.OSExecutor{})
}

// buildWith is the test seam: tests inject a fake media.Executor and a real
// taskmaster engine so the lane's worker path runs without a real yt-dlp
// binary.
func buildWith(cfg config.UtuberConfig, host golane.Host, exec media.Executor) (http.Handler, error) {
	if host == nil {
		return nil, errors.New("utuber: no taskmaster engine")
	}
	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return nil, err
	}
	hist, err := history.Open(filepath.Join(cfg.DownloadDir, "history.json"))
	if err != nil {
		return nil, err
	}
	// python_bin stays in the on-disk settings.json (P7); the lane's DB owns
	// the queue settings (width, retention, hidden, paused). The cookie jar
	// (D5 Fix 2) defaults to a sibling of download_dir, deliberately NOT
	// inside it: /downloads/ below serves DownloadDir verbatim, and
	// settings.json/history.json living there is fine (neither holds
	// secrets, see docs/guides/utuber.md) but a YouTube session cookie is a
	// real credential -- it must never be reachable over that unauthenticated
	// static route.
	cookiesPath := cfg.CookiesFile
	if cookiesPath == "" {
		// filepath.Clean first: filepath.Dir on a path with a trailing slash
		// returns the directory itself, not its parent, which would place the
		// default cookie jar inside download_dir -- exactly what this default
		// is designed to avoid.
		cookiesPath = filepath.Join(filepath.Dir(filepath.Clean(cfg.DownloadDir)), "utuber-cookies.txt")
	}
	settings := newSettingsStore(filepath.Join(cfg.DownloadDir, "settings.json"), cfg.PythonBin, cookiesPath)

	proc := processor{exec: exec, cfg: cfg, hist: hist, settings: settings}

	// Register the lane on the shared engine. There is no StartWorkers call
	// (R7): InitialWidth seeds the lane width from cfg.Workers, and a width of
	// 0 means the shared taskmaster worker never claims a utuber job — the same
	// test-safety contract the old jobs pool had, so `make test` never triggers
	// a real yt-dlp download. After first creation the DB (☰ menu) is
	// authoritative (P8).
	lane, err := host.RegisterLane(
		golane.LaneSpec{
			Name:                 "utuber",
			Owner:                "utuber",
			InitialWidth:         cfg.Workers,
			InitialRetentionDays: 10,
			InitialHidden:        false,
		},
		golane.NewKind[downloadPayload]("utuber.download", 1, validatePayload, proc.run),
	)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/enqueue", handleEnqueue(lane, hist))
	mux.HandleFunc("/jobs.json", handleJobs(lane))
	mux.HandleFunc("/jobs/cancel", handleJobCancel(lane))
	mux.HandleFunc("/jobs/rerun", handleJobRerun(lane))
	mux.HandleFunc("/jobs/delete", handleJobDelete(lane))
	mux.HandleFunc("/jobs/output", handleJobOutput(lane))
	mux.HandleFunc("/ytdlp-update", handleYtdlpUpdate(exec, settings))
	mux.HandleFunc("/settings.json", handleSettings(settings, lane))
	mux.Handle(
		"/downloads/",
		http.StripPrefix("/downloads/", http.FileServer(http.Dir(cfg.DownloadDir))),
	)
	mux.Handle("/", static.NewHandler(cfg.StaticDir))

	log.Printf("utuber: lane seeded at width %d (0 = idle until raised in the ☰ menu), downloads %s, static %s", cfg.Workers, cfg.DownloadDir, cfg.StaticDir)
	return mux, nil
}
