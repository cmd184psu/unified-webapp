package utuber

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/utuber/history"
	"cmd184psu/unified-webapp/internal/utuber/jobs"
	"cmd184psu/unified-webapp/internal/utuber/media"
)

type processor struct {
	exec media.Executor
	cfg  config.UtuberConfig
	hist *history.Log
}

func (p processor) Process(ctx context.Context, job jobs.Job, q *jobs.Queue) error {
	// Mirror rule: mirror to the local snapshot exactly the fields Process
	// reads after writing — ShowName, EpisodeTitle, OutputFile (read back for
	// naming and hist.Record). Never mirror Progress: nothing reads it back,
	// and mirroring it inside the onLine callbacks would write the snapshot
	// from scanner goroutines.
	if job.ShowName == "" || job.EpisodeTitle == "" {
		q.Update(job.ID, func(j *jobs.Job) { j.Progress = "fetching metadata" })
		m, err := media.FetchMeta(ctx, p.exec, job.URL)
		if err == nil {
			if job.ShowName == "" {
				job.ShowName = m.Uploader
			}
			if job.EpisodeTitle == "" {
				job.EpisodeTitle = m.Title
			}
			q.Update(job.ID, func(j *jobs.Job) {
				j.ShowName = job.ShowName
				j.EpisodeTitle = job.EpisodeTitle
			})
		}
	}

	q.Update(job.ID, func(j *jobs.Job) { j.Progress = "downloading" })

	tmp, err := media.Download(
		ctx,
		p.exec,
		job.URL,
		p.cfg.DownloadDir,
		func(s string) {
			q.Update(job.ID, func(j *jobs.Job) { j.Progress = "download " + s })
		},
	)
	if err != nil {
		return err
	}

	if job.Mode == "audio" {
		out := fmt.Sprintf(
			"%s - S%02dE%02d - %s.mp3",
			safe(job.ShowName),
			job.Season,
			job.Episode,
			safe(job.EpisodeTitle),
		)
		outPath := p.cfg.DownloadDir + "/" + out

		q.Update(job.ID, func(j *jobs.Job) { j.Progress = "converting" })
		err = media.ExtractAudio(ctx, p.exec, tmp, outPath, func(s string) {
			q.Update(job.ID, func(j *jobs.Job) { j.Progress = "convert " + s })
		})
		_ = os.Remove(tmp)
		if err != nil {
			return err
		}
		job.OutputFile = out
	} else {
		out := fmt.Sprintf(
			"%s - S%02dE%02d - %s.m4v",
			safe(job.ShowName),
			job.Season,
			job.Episode,
			safe(job.EpisodeTitle),
		)
		_ = os.Rename(tmp, p.cfg.DownloadDir+"/"+out)
		job.OutputFile = out
	}
	q.Update(job.ID, func(j *jobs.Job) { j.OutputFile = job.OutputFile })

	q.Update(job.ID, func(j *jobs.Job) { j.Progress = "done" })

	_ = p.hist.Record(history.Entry{
		URL:        job.URL,
		OutputFile: job.OutputFile,
		ShowName:   job.ShowName,
		Mode:       job.Mode,
	})

	return nil
}

func handleEnqueue(q *jobs.Queue, hist *history.Log) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(1 << 20)

		url := r.FormValue("url")
		if url == "" {
			http.Error(w, "missing url", http.StatusBadRequest)
			return
		}

		if prior := hist.Lookup(url); prior != nil && r.FormValue("force") != "1" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error":       "duplicate",
				"output_file": prior.OutputFile,
				"show_name":   prior.ShowName,
				"mode":        prior.Mode,
			})
			return
		}

		season, _ := strconv.Atoi(r.FormValue("season"))
		episode, _ := strconv.Atoi(r.FormValue("episode"))

		mode := r.FormValue("mode")
		if mode != "audio" {
			mode = "video"
		}

		job := &jobs.Job{
			ID:           randID(),
			URL:          url,
			ShowName:     r.FormValue("show"),
			EpisodeTitle: r.FormValue("title"),
			Season:       season,
			Episode:      episode,
			Mode:         mode,
			Status:       jobs.Queued,
		}

		q.Enqueue(job)
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleYtdlpUpdate(exec media.Executor, s *settingsStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		f, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		send := func(line string) {
			fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(line, "\n", " "))
			f.Flush()
		}

		send("Starting yt-dlp update...")
		err := exec.Run(r.Context(), s.PythonBin(), []string{"-m", "pip", "install", "-U", "yt-dlp"}, send)
		if err != nil {
			send("ERROR: " + err.Error())
		} else {
			send("__done__")
		}
	}
}

func handleJobs(q *jobs.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(q.All())
	}
}

func randID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func safe(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", " -")
	return s
}
