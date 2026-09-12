package utuber

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/utuber/history"
	"cmd184psu/unified-webapp/internal/utuber/jobs"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func newHist(t *testing.T) *history.Log {
	t.Helper()
	l, err := history.Open(filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// multipartForm builds a multipart/form-data request body from key/value pairs.
func multipartForm(t *testing.T, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

func enqueueRequest(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()
	body, ct := multipartForm(t, fields)
	r := httptest.NewRequest(http.MethodPost, "/enqueue", body)
	r.Header.Set("Content-Type", ct)
	return r
}

// ── handleEnqueue ─────────────────────────────────────────────────────────────

func TestEnqueueMissingURL(t *testing.T) {
	q := jobs.New(10)
	h := newHist(t)
	rr := httptest.NewRecorder()

	handleEnqueue(q, h)(rr, enqueueRequest(t, map[string]string{"show": "X"}))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "missing url") {
		t.Errorf("body: %q", rr.Body.String())
	}
}

func TestEnqueueSuccess(t *testing.T) {
	q := jobs.New(10)
	h := newHist(t)
	rr := httptest.NewRecorder()

	handleEnqueue(q, h)(rr, enqueueRequest(t, map[string]string{
		"url":     "http://example.com/v",
		"show":    "My Show",
		"title":   "Ep 1",
		"season":  "2",
		"episode": "3",
		"mode":    "video",
	}))

	if rr.Code != http.StatusNoContent {
		t.Errorf("got %d, want 204", rr.Code)
	}
	all := q.All()
	if len(all) != 1 {
		t.Fatalf("queue length: got %d, want 1", len(all))
	}
	j := all[0]
	if j.URL != "http://example.com/v" {
		t.Errorf("URL: %q", j.URL)
	}
	if j.Season != 2 || j.Episode != 3 {
		t.Errorf("Season/Episode: %d/%d", j.Season, j.Episode)
	}
	if j.Mode != "video" {
		t.Errorf("Mode: %q", j.Mode)
	}
}

func TestEnqueueDefaultsToVideo(t *testing.T) {
	q := jobs.New(10)
	h := newHist(t)
	rr := httptest.NewRecorder()

	handleEnqueue(q, h)(rr, enqueueRequest(t, map[string]string{
		"url": "http://example.com/v",
	}))

	if rr.Code != http.StatusNoContent {
		t.Errorf("got %d, want 204", rr.Code)
	}
	if q.All()[0].Mode != "video" {
		t.Errorf("expected default mode 'video'")
	}
}

func TestEnqueueAudioMode(t *testing.T) {
	q := jobs.New(10)
	h := newHist(t)
	rr := httptest.NewRecorder()

	handleEnqueue(q, h)(rr, enqueueRequest(t, map[string]string{
		"url":  "http://example.com/a",
		"mode": "audio",
	}))

	if q.All()[0].Mode != "audio" {
		t.Errorf("expected mode 'audio'")
	}
}

func TestEnqueueDuplicateBlocked(t *testing.T) {
	q := jobs.New(10)
	h := newHist(t)
	h.Record(history.Entry{URL: "http://dup.com", OutputFile: "dup.mp4", ShowName: "Dup Show", Mode: "video"})

	rr := httptest.NewRecorder()
	handleEnqueue(q, h)(rr, enqueueRequest(t, map[string]string{"url": "http://dup.com"}))

	if rr.Code != http.StatusConflict {
		t.Errorf("got %d, want 409", rr.Code)
	}
	var body map[string]string
	json.NewDecoder(rr.Body).Decode(&body)
	if body["error"] != "duplicate" {
		t.Errorf("body: %v", body)
	}
	if body["output_file"] != "dup.mp4" {
		t.Errorf("output_file: %q", body["output_file"])
	}
	if len(q.All()) != 0 {
		t.Error("duplicate should not have been enqueued")
	}
}

func TestEnqueueDuplicateForced(t *testing.T) {
	q := jobs.New(10)
	h := newHist(t)
	h.Record(history.Entry{URL: "http://dup.com", OutputFile: "dup.mp4"})

	rr := httptest.NewRecorder()
	handleEnqueue(q, h)(rr, enqueueRequest(t, map[string]string{
		"url":   "http://dup.com",
		"force": "1",
	}))

	if rr.Code != http.StatusNoContent {
		t.Errorf("got %d, want 204", rr.Code)
	}
	if len(q.All()) != 1 {
		t.Error("forced duplicate should have been enqueued")
	}
}

// ── handleJobs ────────────────────────────────────────────────────────────────

func TestHandleJobsEmpty(t *testing.T) {
	q := jobs.New(10)
	rr := httptest.NewRecorder()
	handleJobs(q)(rr, httptest.NewRequest(http.MethodGet, "/jobs.json", nil))

	if rr.Code != http.StatusOK {
		t.Errorf("got %d, want 200", rr.Code)
	}
	var result []*jobs.Job
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty, got %d jobs", len(result))
	}
}

func TestHandleJobsReturnsAll(t *testing.T) {
	q := jobs.New(10)
	q.Enqueue(&jobs.Job{ID: "1", Status: jobs.Queued})
	q.Enqueue(&jobs.Job{ID: "2", Status: jobs.Queued})

	rr := httptest.NewRecorder()
	handleJobs(q)(rr, httptest.NewRequest(http.MethodGet, "/jobs.json", nil))

	var result []*jobs.Job
	json.NewDecoder(rr.Body).Decode(&result)
	if len(result) != 2 {
		t.Errorf("expected 2, got %d", len(result))
	}
}

// ── processor.Process ─────────────────────────────────────────────────────────

type fakeExec struct {
	name string
	args []string
	err  error
}

func (f *fakeExec) Run(_ context.Context, name string, args []string, _ func(string)) error {
	f.name = name
	f.args = args
	return f.err
}

func TestProcessFillsMetadataWhenBlank(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "raw.mp4")
	os.WriteFile(tmp, []byte("video"), 0644)

	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	p := processor{
		exec: &metaAndDownloadExec{dir: dir, realFile: tmp,
			metaLine: "Cool Artist|||Cool Track"},
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: hist,
	}

	job := &jobs.Job{
		ID: "m1", URL: "http://meta.com",
		ShowName: "", EpisodeTitle: "", // both blank
		Season: 1, Episode: 1, Mode: "video", Status: jobs.Queued,
	}
	q := jobs.New(10)
	q.Enqueue(job)
	snap, _ := q.Get(job.ID)
	if err := p.Process(context.Background(), snap, q); err != nil {
		t.Fatal(err)
	}
	got, ok := q.Get(job.ID)
	if !ok {
		t.Fatalf("job %s vanished from queue", job.ID)
	}
	if got.ShowName != "Cool Artist" {
		t.Errorf("ShowName: got %q", got.ShowName)
	}
	if got.EpisodeTitle != "Cool Track" {
		t.Errorf("EpisodeTitle: got %q", got.EpisodeTitle)
	}
	// The autofill -> filename -> history chain is otherwise a silent failure mode.
	want := "Cool Artist - S01E01 - Cool Track.m4v"
	if got.OutputFile != want {
		t.Errorf("OutputFile: got %q, want %q", got.OutputFile, want)
	}
	if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
		t.Errorf("output file not found: %v", err)
	}
	prior := hist.Lookup("http://meta.com")
	if prior == nil {
		t.Fatal("history not recorded")
	}
	if prior.OutputFile != want {
		t.Errorf("history OutputFile: got %q, want %q", prior.OutputFile, want)
	}
	if prior.ShowName != "Cool Artist" {
		t.Errorf("history ShowName: got %q", prior.ShowName)
	}
}

func TestProcessKeepsExistingMetadata(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "raw.mp4")
	os.WriteFile(tmp, []byte("video"), 0644)

	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	p := processor{
		exec: &newestAfterExec{dir: dir, realFile: tmp},
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: hist,
	}

	job := &jobs.Job{
		ID: "m2", URL: "http://meta2.com",
		ShowName: "My Show", EpisodeTitle: "My Ep", // already set
		Season: 1, Episode: 1, Mode: "video", Status: jobs.Queued,
	}
	q := jobs.New(10)
	q.Enqueue(job)
	snap, _ := q.Get(job.ID)
	if err := p.Process(context.Background(), snap, q); err != nil {
		t.Fatal(err)
	}
	got, ok := q.Get(job.ID)
	if !ok {
		t.Fatalf("job %s vanished from queue", job.ID)
	}
	if got.ShowName != "My Show" {
		t.Errorf("ShowName should not be overwritten, got %q", got.ShowName)
	}
}

func TestProcessVideoMode(t *testing.T) {
	dir := t.TempDir()
	// create a fake downloaded file that the processor will rename
	tmp := filepath.Join(dir, "raw.mp4")
	os.WriteFile(tmp, []byte("video"), 0644)

	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	exec := &fakeExec{}
	p := processor{
		exec: exec,
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: hist,
	}

	// Make Download return our fake file by having the executor write nothing
	// but newestFile will find tmp since we created it above.
	job := &jobs.Job{
		ID: "v1", URL: "http://x.com", ShowName: "Show", EpisodeTitle: "Ep",
		Season: 1, Episode: 2, Mode: "video", Status: jobs.Queued,
	}

	// Wrap exec so Download "succeeds" and returns our tmp file path.
	wrappedExec := &newestAfterExec{dir: dir, realFile: tmp}
	p.exec = wrappedExec

	q := jobs.New(10)
	q.Enqueue(job)
	snap, _ := q.Get(job.ID)
	err := p.Process(context.Background(), snap, q)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := q.Get(job.ID)
	if !ok {
		t.Fatalf("job %s vanished from queue", job.ID)
	}
	want := "Show - S01E02 - Ep.m4v"
	if got.OutputFile != want {
		t.Errorf("OutputFile: got %q, want %q", got.OutputFile, want)
	}
	if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
		t.Errorf("output file not found: %v", err)
	}
	if hist.Lookup("http://x.com") == nil {
		t.Error("history not recorded")
	}
	if got.Progress != "done" {
		t.Errorf("Progress: got %q, want done", got.Progress)
	}
}

func TestProcessAudioMode(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "raw.mp4")
	os.WriteFile(tmp, []byte("video"), 0644)

	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	p := processor{
		exec: &newestAfterExec{dir: dir, realFile: tmp},
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: hist,
	}

	job := &jobs.Job{
		ID: "a1", URL: "http://y.com", ShowName: "Artist", EpisodeTitle: "Track",
		Season: 1, Episode: 1, Mode: "audio", Status: jobs.Queued,
	}

	q := jobs.New(10)
	q.Enqueue(job)
	snap, _ := q.Get(job.ID)
	err := p.Process(context.Background(), snap, q)
	if err != nil {
		t.Fatal(err)
	}

	got, ok := q.Get(job.ID)
	if !ok {
		t.Fatalf("job %s vanished from queue", job.ID)
	}
	want := "Artist - S01E01 - Track.mp3"
	if got.OutputFile != want {
		t.Errorf("OutputFile: got %q, want %q", got.OutputFile, want)
	}
	// tmp should be removed
	if _, err := os.Stat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Error("temp file should have been removed after audio extraction")
	}
	if got.Progress != "done" {
		t.Errorf("Progress: got %q, want done", got.Progress)
	}
}

func TestProcessAudioExtractionError(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, "raw.mp4")
	os.WriteFile(tmp, []byte("video"), 0644)

	boom := errors.New("ffmpeg failed")
	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	p := processor{
		exec: &newestThenErrorExec{dir: dir, realFile: tmp, extractErr: boom},
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: hist,
	}
	job := &jobs.Job{ID: "ae1", URL: "http://a.com", Mode: "audio", Status: jobs.Queued}

	q := jobs.New(10)
	q.Enqueue(job)
	snap, _ := q.Get(job.ID)
	if err := p.Process(context.Background(), snap, q); !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
	got, ok := q.Get(job.ID)
	if !ok {
		t.Fatalf("job %s vanished from queue", job.ID)
	}
	// newestThenErrorExec's second call is consumed by Download (its two
	// executor calls are FetchMeta then yt-dlp), so the failure happens while
	// Progress is still "downloading" — proving the write-through reached the
	// queue.
	if got.Progress != "downloading" {
		t.Errorf("Progress: got %q, want downloading", got.Progress)
	}
}

func TestProcessDownloadError(t *testing.T) {
	dir := t.TempDir()
	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	boom := errors.New("download failed")

	p := processor{
		exec: &errorExec{err: boom},
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: hist,
	}
	job := &jobs.Job{ID: "e1", URL: "http://z.com", Mode: "video", Status: jobs.Queued}

	q := jobs.New(10)
	q.Enqueue(job)
	snap, _ := q.Get(job.ID)
	if err := p.Process(context.Background(), snap, q); !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
	got, ok := q.Get(job.ID)
	if !ok {
		t.Fatalf("job %s vanished from queue", job.ID)
	}
	if got.Progress != "downloading" {
		t.Errorf("Progress: got %q, want downloading", got.Progress)
	}
}

// ── progress sequence ─────────────────────────────────────────────────────────

// samplingExec records the job's queue-side Progress at Run entry and again
// after each onLine call returns. The post-onLine sample is the only one that
// can observe "download 42.0": onLine -> Download's regex-gated callback ->
// the Process closure's q.Update all complete inside the onLine call.
type samplingExec struct {
	q    *jobs.Queue
	id   string
	line string // must match media/downloader.go's progress regex
	mu   sync.Mutex
	rec  []string
}

func (e *samplingExec) sample() {
	snap, _ := e.q.Get(e.id)
	e.mu.Lock()
	e.rec = append(e.rec, snap.Progress)
	e.mu.Unlock()
}

func (e *samplingExec) recorded() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.rec...)
}

func (e *samplingExec) Run(_ context.Context, _ string, _ []string, onLine func(string)) error {
	e.sample() // at Run entry
	if onLine != nil && e.line != "" {
		onLine(e.line)
		e.sample() // AFTER onLine returns
	}
	return nil
}

func TestProcessProgressSequence(t *testing.T) {
	for _, mode := range []string{"video", "audio"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			tmp := filepath.Join(dir, "raw.mp4")
			os.WriteFile(tmp, []byte("video"), 0644)

			q := jobs.New(10)
			// ShowName and EpisodeTitle must both be blank: the metadata
			// branch (and its "fetching metadata" progress write) is skipped
			// once both fields are populated.
			job := &jobs.Job{
				ID: "seq-" + mode, URL: "http://seq.com",
				Season: 1, Episode: 1, Mode: mode, Status: jobs.Queued,
			}
			q.Enqueue(job)

			exec := &samplingExec{q: q, id: job.ID, line: "[download]  42.0%"}
			p := processor{
				exec: exec,
				cfg:  config.UtuberConfig{DownloadDir: dir},
				hist: newHist(t),
			}

			snap, _ := q.Get(job.ID)
			if err := p.Process(context.Background(), snap, q); err != nil {
				t.Fatal(err)
			}
			got, ok := q.Get(job.ID)
			if !ok {
				t.Fatalf("job %s vanished from queue", job.ID)
			}
			rec := append(exec.recorded(), got.Progress)

			want := []string{"fetching metadata", "downloading", "download 42.0", "done"}
			if mode == "audio" {
				// The third Run's post-onLine sample records the unfiltered
				// "convert [download]  42.0%" (ExtractAudio has no regex
				// gate); the subsequence assertion tolerates it.
				want = []string{"fetching metadata", "downloading", "download 42.0", "converting", "done"}
			}
			assertOrderedSubsequence(t, rec, want)
		})
	}
}

func assertOrderedSubsequence(t *testing.T, got, want []string) {
	t.Helper()
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Fatalf("progress recording %q does not contain ordered subsequence %q (matched %d of %d)",
			got, want, i, len(want))
	}
}

// ── safe ─────────────────────────────────────────────────────────────────────

func TestSafe(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"  spaces  ", "spaces"},
		{"a/b/c", "a-b-c"},
		{"a: b", "a - b"},
	}
	for _, tc := range cases {
		if got := safe(tc.in); got != tc.want {
			t.Errorf("safe(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── test executor helpers ─────────────────────────────────────────────────────

// newestAfterExec does nothing on Run, leaving realFile in place so newestFile picks it up.
type newestAfterExec struct {
	dir      string
	realFile string
	callNum  int
}

func (e *newestAfterExec) Run(_ context.Context, _ string, _ []string, _ func(string)) error {
	e.callNum++
	// On the second call (ffmpeg for audio), create the expected output file
	// so the processor doesn't fail on a missing file.
	return nil
}

type errorExec struct{ err error }

func (e *errorExec) Run(_ context.Context, _ string, _ []string, _ func(string)) error {
	return e.err
}

// metaAndDownloadExec returns metaLine on the first call (FetchMeta) and
// succeeds silently on the second call (Download), leaving realFile in place.
type metaAndDownloadExec struct {
	dir      string
	realFile string
	metaLine string
	callNum  int
}

func (e *metaAndDownloadExec) Run(_ context.Context, _ string, _ []string, onLine func(string)) error {
	e.callNum++
	if e.callNum == 1 {
		onLine(e.metaLine)
	}
	return nil
}

// newestThenErrorExec succeeds on the first call (yt-dlp download) and returns
// extractErr on the second call (ffmpeg audio extraction).
type newestThenErrorExec struct {
	dir        string
	realFile   string
	extractErr error
	callNum    int
}

func (e *newestThenErrorExec) Run(_ context.Context, _ string, _ []string, _ func(string)) error {
	e.callNum++
	if e.callNum == 2 {
		return e.extractErr
	}
	return nil
}
