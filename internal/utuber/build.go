package utuber

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/utuber/history"
	"cmd184psu/unified-webapp/internal/utuber/jobs"
	"cmd184psu/unified-webapp/internal/utuber/media"
)

// Build returns a ready-to-use http.Handler for the utuber module.
func Build(cfg config.UtuberConfig) (http.Handler, error) {
	return buildWithExecutor(cfg, media.OSExecutor{})
}

// buildWithExecutor is the test seam: tests inject a fake media.Executor
// here to exercise live workers and the yt-dlp update stream without a real
// yt-dlp binary.
func buildWithExecutor(cfg config.UtuberConfig, exec media.Executor) (http.Handler, error) {
	if err := os.MkdirAll(cfg.DownloadDir, 0o755); err != nil {
		return nil, err
	}
	hist, err := history.Open(filepath.Join(cfg.DownloadDir, "history.json"))
	if err != nil {
		return nil, err
	}
	settings := newSettingsStore(filepath.Join(cfg.DownloadDir, "settings.json"), cfg.PythonBin)

	queue := jobs.New(100)
	proc := processor{exec: exec, cfg: cfg, hist: hist}

	// Workers are deliberately un-stoppable (FR-6): the unified server has no
	// per-module shutdown hook, so in-flight downloads die with the process —
	// the same effective behavior as the standalone's SIGTERM. cfg.Workers and
	// cfg.PythonBin arrive normalized by config.Load and are trusted here.
	//
	// Do NOT "harden" Workers <= 0 to 1 here: Workers: 0 is the deliberate
	// test-safety configuration (the server's buildDispatcher tests bypass
	// Load normalization), and defaulting it in Build would make `make test`
	// perform real yt-dlp network downloads on any host where the binary
	// exists.
	jobs.StartWorkers(context.Background(), queue, proc, cfg.Workers)

	mux := http.NewServeMux()
	mux.HandleFunc("/enqueue", handleEnqueue(queue, hist))
	mux.HandleFunc("/jobs.json", handleJobs(queue))
	mux.HandleFunc("/ytdlp-update", handleYtdlpUpdate(exec, settings))
	mux.HandleFunc("/settings.json", handleSettings(settings))
	mux.Handle(
		"/downloads/",
		http.StripPrefix("/downloads/", http.FileServer(http.Dir(cfg.DownloadDir))),
	)
	mux.Handle("/", &staticHandler{dir: cfg.StaticDir})

	log.Printf("utuber: %d worker(s), downloads %s, static %s", cfg.Workers, cfg.DownloadDir, cfg.StaticDir)
	return mux, nil
}

// staticHandler serves files from dir with an index.html fallback, matching
// the sibling modules' miss-path convention. Declared deviation from the
// reference (which used a bare http.FileServer): unknown paths return
// index.html with 200 instead of 404.
type staticHandler struct {
	dir string
}

func (sh *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := filepath.Join(sh.dir, filepath.Clean("/"+r.URL.Path))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(sh.dir, "index.html"))
		return
	}
	http.ServeFile(w, r, path)
}
