package utuber

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	// yt-dlp writes to a deterministic "<job ID>.mp4" (see media.Download), so
	// src is the exact file just produced — no directory scan to guess which
	// file is the download.
	src, err := media.Download(
		ctx,
		p.exec,
		job.URL,
		p.cfg.DownloadDir,
		job.ID,
		func(s string) {
			q.Update(job.ID, func(j *jobs.Job) { j.Progress = "download " + s })
		},
	)
	if err != nil {
		return err
	}

	if job.Mode == "audio" {
		out := outputName(job.ShowName, job.Season, job.Episode, job.EpisodeTitle, "mp3")
		outPath := p.cfg.DownloadDir + "/" + out

		q.Update(job.ID, func(j *jobs.Job) { j.Progress = "converting" })
		err = media.ExtractAudio(ctx, p.exec, src, outPath, func(s string) {
			q.Update(job.ID, func(j *jobs.Job) { j.Progress = "convert " + s })
		})
		_ = os.Remove(src)
		if err != nil {
			return err
		}
		job.OutputFile = out
	} else {
		// Keep the yt-dlp .mp4 as-is (Apple-compatible H.264/AAC in an mp4
		// container); only give it the Plex-friendly name. No transcode, and
		// no .m4v rename — the extension stays .mp4.
		out := outputName(job.ShowName, job.Season, job.Episode, job.EpisodeTitle, "mp4")
		if err := os.Rename(src, p.cfg.DownloadDir+"/"+out); err != nil {
			return err
		}
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

// handleJobDelete removes a queued or finished job: POST /jobs/delete?id=…
// A running job is refused with 409; its downloaded file is never touched.
func handleJobDelete(q *jobs.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch err := q.Remove(r.URL.Query().Get("id")); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, jobs.ErrRunning):
			http.Error(w, "that job is running and can't be removed", http.StatusConflict)
		default:
			http.Error(w, "job not found", http.StatusNotFound)
		}
	}
}

func handleJobs(q *jobs.Queue) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(q.All())
	}
}

// outputName builds the Plex-style file name. A season or episode of 0 (or
// less) is left out rather than written as "S00"/"E00":
//
//	Show - S01E02 - Title.mp4   both set
//	Show - S01 - Title.mp4      season only
//	Show - E02 - Title.mp4      episode only
//	Show - Title.mp4            neither
func outputName(show string, season, episode int, title, ext string) string {
	var tag string
	if season > 0 {
		tag += fmt.Sprintf("S%02d", season)
	}
	if episode > 0 {
		tag += fmt.Sprintf("E%02d", episode)
	}
	if tag == "" {
		return fmt.Sprintf("%s - %s.%s", safe(show), safe(title), ext)
	}
	return fmt.Sprintf("%s - %s - %s.%s", safe(show), tag, safe(title), ext)
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
