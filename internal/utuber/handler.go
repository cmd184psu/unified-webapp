package utuber

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster/golane"
	"cmd184psu/unified-webapp/internal/utuber/history"
	"cmd184psu/unified-webapp/internal/utuber/media"
)

type processor struct {
	exec media.Executor
	cfg  config.UtuberConfig
	hist *history.Log
	// settings supplies the configured cookie-jar path (D5 Fix 2). Tests that
	// build a processor literal without it get a nil *settingsStore, whose
	// CookiesPath() safely returns "" (no cookies configured).
	settings *settingsStore
}

// downloadPayload is the "utuber.download" kind payload (version 1). The JSON
// tags are the on-the-wire contract stored per task in the taskmaster DB.
type downloadPayload struct {
	URL          string `json:"url"`
	ShowName     string `json:"show_name"`
	EpisodeTitle string `json:"episode_title"`
	Season       int    `json:"season"`
	Episode      int    `json:"episode"`
	Mode         string `json:"mode"` // "video" or "audio"
}

// validatePayload is the kind's decode-time validator (defence in depth: the
// payload is untrusted until decoded).
func validatePayload(p downloadPayload) error {
	if p.URL == "" || len(p.URL) > 2048 {
		return errors.New("url must be non-empty and at most 2048 bytes")
	}
	if p.Mode != "video" && p.Mode != "audio" {
		return errors.New(`mode must be "video" or "audio"`)
	}
	if p.Season < 0 || p.Season > 999 || p.Episode < 0 || p.Episode > 999 {
		return errors.New("season and episode must be between 0 and 999")
	}
	return nil
}

// downloadResult is the kind's success result, stored per execution.
type downloadResult struct {
	OutputFile   string `json:"output_file"`
	ShowName     string `json:"show_name"`
	EpisodeTitle string `json:"episode_title"`
}

// simulateURLPrefix marks a job submitted by the Queue menu's "Simulate
// download" button (handleEnqueueSimulated) rather than a real URL. run
// branches on it before ever touching yt-dlp, so simulated jobs exercise the
// exact real taskmaster lane / progress / SSE / QueuePanel path with zero
// network calls -- useful for testing the UI without depending on YouTube's
// cooperation (or risking more bot-detection during testing).
const simulateURLPrefix = "test://simulate"

// run is the "utuber.download" kind's Run. It does exactly what the old
// jobs-queue Process did, but reports through the RunContext instead of a
// jobs.Queue: rc.Progress for the progress bar, rc.SetLabel for the title, and
// rc.Log for every raw yt-dlp/ffmpeg line (which taskmaster replays over SSE
// and D5 later mines for the real failure reason).
func (p processor) run(ctx context.Context, rc golane.RunContext, job downloadPayload) (any, error) {
	if strings.HasPrefix(job.URL, simulateURLPrefix) {
		return p.runSimulated(ctx, rc, job)
	}
	show, title := job.ShowName, job.EpisodeTitle
	if show == "" || title == "" {
		rc.Progress(-1, "Fetching metadata")
		m, err := media.FetchMeta(ctx, p.exec, job.URL)
		if err == nil {
			if show == "" {
				show = m.Uploader
			}
			if title == "" {
				title = m.Title
			}
		}
	}
	rc.SetLabel(show + " — " + title)

	rc.Progress(-1, "Downloading")

	// lastErrorLine tracks the most recent non-empty yt-dlp/ffmpeg output line
	// containing "ERROR:" (D5 Fix 1). Every raw line still reaches the task's
	// output unconditionally -- this only additionally remembers the best
	// candidate for the execution's actual failure reason, since a bare exec
	// error like "exit status 1" names nothing an operator can act on.
	//
	// stdout and stderr are now captured separately, so there are two closures:
	// onStdoutLine writes to rc.Log(), onStderrLine to rc.Stderr(). Because
	// yt-dlp can emit its "ERROR:" line on either stream, BOTH scan for it and
	// update the same lastErrorLine, preserving D5's exact error-surfacing
	// behavior regardless of which stream carried the ERROR: line.
	var lastErrorLine string
	trackError := func(line string) {
		if trimmed := strings.TrimSpace(line); trimmed != "" && strings.Contains(trimmed, "ERROR:") {
			lastErrorLine = trimmed
		}
	}
	onStdoutLine := func(line string) {
		fmt.Fprintln(rc.Log(), line)
		trackError(line)
	}
	onStderrLine := func(line string) {
		fmt.Fprintln(rc.Stderr(), line)
		trackError(line)
	}

	// The download filename stem is "<job ID>-<exec ID>": unique per execution
	// (a rerun gets a fresh exec ID, so its temp file never collides with the
	// prior run's leftover). yt-dlp writes "<stem>.mp4" deterministically.
	stem := fmt.Sprintf("%s-%d", rc.JobID(), rc.ExecID())
	src, err := media.Download(
		ctx,
		p.exec,
		job.URL,
		p.cfg.DownloadDir,
		stem,
		p.settings.CookiesPath(),
		func(s string) {
			if pct, perr := strconv.ParseFloat(s, 64); perr == nil {
				rc.Progress(int(pct), "Downloading")
			}
		},
		onStdoutLine,
		onStderrLine,
	)
	if err != nil {
		return nil, ytdlpError(err, lastErrorLine)
	}

	var outputFile string
	if job.Mode == "audio" {
		out := outputName(show, job.Season, job.Episode, title, "mp3")
		outPath := p.cfg.DownloadDir + "/" + out

		rc.Progress(-1, "Converting")
		err = media.ExtractAudio(ctx, p.exec, src, outPath, onStdoutLine)
		_ = os.Remove(src)
		if err != nil {
			return nil, ytdlpError(err, lastErrorLine)
		}
		outputFile = out
	} else {
		// Keep the yt-dlp .mp4 as-is (Apple-compatible H.264/AAC in an mp4
		// container); only give it the Plex-friendly name. No transcode, and
		// no .m4v rename — the extension stays .mp4.
		out := outputName(show, job.Season, job.Episode, title, "mp4")
		if err := os.Rename(src, p.cfg.DownloadDir+"/"+out); err != nil {
			return nil, err
		}
		outputFile = out
	}

	_ = p.hist.Record(history.Entry{
		URL:        job.URL,
		OutputFile: outputFile,
		ShowName:   show,
		Mode:       job.Mode,
	})

	return downloadResult{OutputFile: outputFile, ShowName: show, EpisodeTitle: title}, nil
}

// simulateSteps/simulateStepDelay give a ~30s run: long enough to watch
// several progress polls land smoothly (the UI's 2000ms poll floor), short
// enough not to be annoying to run repeatedly while testing.
const simulateSteps = 20
const simulateStepDelay = 1500 * time.Millisecond

// runSimulated services a "test://simulate..." job (only ever submitted by
// the Queue menu's Simulate button): no network, no yt-dlp, no real video —
// just a synthetic progress ramp through the exact same taskmaster lane /
// SSE / QueuePanel path a real download uses. Ends by writing a tiny real
// (placeholder, not a playable video) file so the success-state Play/
// Download links and history recording are exercised too, not just the
// running state. Honors cancellation exactly like a real job: canceling
// mid-ramp returns ctx.Err(), which the worker records as "canceled".
func (p processor) runSimulated(ctx context.Context, rc golane.RunContext, job downloadPayload) (any, error) {
	show, title := job.ShowName, job.EpisodeTitle
	if show == "" {
		show = "Simulated"
	}
	if title == "" {
		title = "Download Test"
	}
	rc.SetLabel(show + " — " + title)

	for i := 0; i <= simulateSteps; i++ {
		rc.Progress(i*100/simulateSteps, "Downloading (simulated)")
		if i == simulateSteps {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(simulateStepDelay):
		}
	}

	out := outputName(show, job.Season, job.Episode, title, "mp4")
	outPath := p.cfg.DownloadDir + "/" + out
	placeholder := "This is a placeholder file from utuber's Simulate-download testing " +
		"feature -- not a real video. See processor.runSimulated in internal/utuber/handler.go.\n"
	if err := os.WriteFile(outPath, []byte(placeholder), 0o644); err != nil {
		return nil, err
	}

	_ = p.hist.Record(history.Entry{
		URL:        job.URL,
		OutputFile: out,
		ShowName:   show,
		Mode:       job.Mode,
	})

	return downloadResult{OutputFile: out, ShowName: show, EpisodeTitle: title}, nil
}

// ytdlpError returns the execution error the worker records (D5 Fix 1): when
// a captured "ERROR:" line from yt-dlp/ffmpeg's output exists, it replaces
// the generic exec error (typically "exit status 1", which names nothing
// actionable) so the taskmaster execution's error field carries the tool's
// actual stated reason instead. With no captured line, err passes through
// unchanged.
func ytdlpError(err error, lastErrorLine string) error {
	if lastErrorLine == "" {
		return err
	}
	return errors.New(lastErrorLine)
}

func handleEnqueue(lane golane.Lane, hist *history.Log) http.HandlerFunc {
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

		payload := downloadPayload{
			URL:          url,
			ShowName:     r.FormValue("show"),
			EpisodeTitle: r.FormValue("title"),
			Season:       season,
			Episode:      episode,
			Mode:         mode,
		}

		label := payload.ShowName + " — " + payload.EpisodeTitle
		if payload.ShowName == "" && payload.EpisodeTitle == "" {
			label = url
		}

		if _, err := lane.Submit("utuber.download", label, payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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
		err := exec.Run(r.Context(), s.PythonBin(), []string{"-m", "pip", "install", "-U", "yt-dlp"}, send, send)
		if err != nil {
			send("ERROR: " + err.Error())
		} else {
			send("__done__")
		}
	}
}

// progressJSON is the /jobs.json progress object: {"pct":int|null,"label":str}.
type progressJSON struct {
	Pct   *int   `json:"pct"`
	Label string `json:"label"`
}

// jobJSON is the /jobs.json wire format (15 snake_case keys). It is the
// browser-facing contract; the QueuePanel front end decodes exactly these.
type jobJSON struct {
	ID           string        `json:"id"`
	Status       string        `json:"status"` // queued|running|success|failed|canceled
	Label        string        `json:"label"`
	URL          string        `json:"url"`
	ShowName     string        `json:"show_name"`
	EpisodeTitle string        `json:"episode_title"`
	Season       int           `json:"season"`
	Episode      int           `json:"episode"`
	Mode         string        `json:"mode"`
	Progress     *progressJSON `json:"progress"`
	OutputFile   string        `json:"output_file"`
	Error        string        `json:"error"`
	CreatedAt    time.Time     `json:"created_at"`
	StartedAt    *time.Time    `json:"started_at"`
	FinishedAt   *time.Time    `json:"finished_at"`
}

func toJobJSON(j golane.Job) jobJSON {
	var p downloadPayload
	_ = json.Unmarshal(j.Payload, &p)

	jj := jobJSON{
		ID:           j.ID,
		Status:       j.Status,
		Label:        j.Label,
		URL:          p.URL,
		ShowName:     p.ShowName,
		EpisodeTitle: p.EpisodeTitle,
		Season:       p.Season,
		Episode:      p.Episode,
		Mode:         p.Mode,
		Error:        j.Error,
		CreatedAt:    j.CreatedAt,
		StartedAt:    j.StartedAt,
		FinishedAt:   j.FinishedAt,
	}
	if j.Progress != nil {
		jj.Progress = &progressJSON{Pct: j.Progress.Pct, Label: j.Progress.Label}
	}
	// Result fields win when a successful execution recorded them.
	if len(j.Result) > 0 {
		var r downloadResult
		if json.Unmarshal(j.Result, &r) == nil {
			jj.OutputFile = r.OutputFile
			if r.ShowName != "" {
				jj.ShowName = r.ShowName
			}
			if r.EpisodeTitle != "" {
				jj.EpisodeTitle = r.EpisodeTitle
			}
		}
	}
	return jj
}

func handleJobs(lane golane.Lane) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		jobs, err := lane.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := make([]jobJSON, 0, len(jobs))
		for _, j := range jobs {
			out = append(out, toJobJSON(j))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

// handleJobCancel cancels a queued or running download: POST /jobs/cancel?id=…
func handleJobCancel(lane golane.Lane) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		outcome, err := lane.Cancel(r.URL.Query().Get("id"))
		switch {
		case errors.Is(err, golane.ErrNotFound):
			http.Error(w, "job not found", http.StatusNotFound)
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		case outcome == golane.CancelOutcome("not_running"):
			http.Error(w, "that download already finished", http.StatusConflict)
		default: // "canceled" | "canceling"
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": string(outcome)})
		}
	}
}

// handleJobRerun re-queues a finished download: POST /jobs/rerun?id=…
func handleJobRerun(lane golane.Lane) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, err := lane.Rerun(r.URL.Query().Get("id"))
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, golane.ErrNotFound):
			http.Error(w, "job not found", http.StatusNotFound)
		case errors.Is(err, golane.ErrBusy):
			http.Error(w, "that download is queued or running", http.StatusConflict)
		case errors.Is(err, golane.ErrSucceeded):
			http.Error(w, "that download already succeeded", http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// handleJobOutput streams a job's latest execution's stdout+stderr as SSE:
// GET /jobs/output?id=… . It mirrors the taskmaster coordinator's per-
// execution output stream, but reachable even when utuber runs headless (P16,
// taskmaster's own routes not mounted), since lane.StreamOutput calls the same
// shared worker streaming code. lane.StreamOutput writes the SSE stream (and
// any inline error, e.g. the 503 subscriber-limit) directly to w on success;
// only a non-nil returned error (job not found) needs an HTTP status mapped
// here, and it is returned before anything is written to w.
func handleJobOutput(lane golane.Lane) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := lane.StreamOutput(w, r, r.URL.Query().Get("id"))
		switch {
		case err == nil:
			// StreamOutput already wrote the response (SSE stream or inline error).
		case errors.Is(err, golane.ErrNotFound):
			http.Error(w, "job not found", http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// handleJobDelete removes a queued or finished job: POST /jobs/delete?id=…
// A running job is refused with 409; its downloaded file is never touched.
func handleJobDelete(lane golane.Lane) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch err := lane.Remove(r.URL.Query().Get("id")); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, golane.ErrRunning):
			http.Error(w, "that job is running and can't be removed", http.StatusConflict)
		default:
			http.Error(w, "job not found", http.StatusNotFound)
		}
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

func safe(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", " -")
	return s
}
