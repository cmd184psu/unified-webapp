package utuber

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/taskmaster"
	"cmd184psu/unified-webapp/internal/utuber/history"

	"github.com/stretchr/testify/require"
)

// ── harness ─────────────────────────────────────────────────────────────────

// openEngine opens a real headless (owned-lanes-only) taskmaster engine on a
// fresh DB, closed by t.Cleanup so goleak stays clean. ProgressIntervalMs:50
// keeps board/progress writes prompt under -race (R8).
func openEngine(t *testing.T, dbPath string) *taskmaster.Engine {
	t.Helper()
	e, err := taskmaster.Open(
		config.TaskmasterConfig{DBPath: dbPath, ProgressIntervalMs: 50},
		taskmaster.OpenOptions{OwnedLanesOnly: true},
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func utuberCfg(t *testing.T, root string, workers int) config.UtuberConfig {
	t.Helper()
	staticDir := filepath.Join(root, "web")
	require.NoError(t, os.MkdirAll(staticDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("UTUBER INDEX"), 0o644))
	return config.UtuberConfig{
		StaticDir:   staticDir,
		DownloadDir: filepath.Join(root, "dl"),
		Workers:     workers,
		PythonBin:   "python3.12",
	}
}

// buildTestModule builds a module on a fresh engine with a fake executor and
// width 0, so no submitted job ever runs (R7 test-safety). Most HTTP-shape
// tests use this.
func buildTestModule(t *testing.T) (http.Handler, config.UtuberConfig) {
	t.Helper()
	root := t.TempDir()
	cfg := utuberCfg(t, root, 0)
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, downloadOnlyExec{})
	require.NoError(t, err)
	return h, cfg
}

func req(h http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(method, target, body)
	h.ServeHTTP(rr, r)
	return rr
}

func jobsJSON(t *testing.T, h http.Handler) []map[string]any {
	t.Helper()
	rr := req(h, http.MethodGet, "/jobs.json", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var out []map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &out))
	return out
}

// ── static / build ──────────────────────────────────────────────────────────

func TestBuildServesIndexAndFallsBackOnUnknownPath(t *testing.T) {
	h, _ := buildTestModule(t)

	res := req(h, http.MethodGet, "/", nil)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "UTUBER INDEX") {
		t.Fatalf("GET / = %d %q, want 200 with index body", res.Code, res.Body)
	}
	miss := req(h, http.MethodGet, "/no/such/path", nil)
	if miss.Code != http.StatusOK || !strings.Contains(miss.Body.String(), "UTUBER INDEX") {
		t.Fatalf("GET unknown path = %d %q, want 200 with index body", miss.Code, miss.Body)
	}
}

func TestBuildFailsWhenNilHost(t *testing.T) {
	_, err := Build(config.UtuberConfig{DownloadDir: t.TempDir()}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "utuber: no taskmaster engine")
}

// The 503 path: buildDispatcher turns this error into the scoped
// unavailableHandler, whose body must name the offending path — so the error
// has to carry DownloadDir verbatim.
func TestBuildFailsWhenDownloadDirCannotBeCreated(t *testing.T) {
	root := t.TempDir()
	blocker := filepath.Join(root, "not-a-dir")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	_, err := buildWith(config.UtuberConfig{
		StaticDir:   t.TempDir(),
		DownloadDir: blocker,
		Workers:     0,
		PythonBin:   "python3.12",
	}, e, downloadOnlyExec{})
	require.Error(t, err)
	require.Contains(t, err.Error(), blocker)
}

func TestBuildCreatesDownloadDir(t *testing.T) {
	_, cfg := buildTestModule(t)
	info, err := os.Stat(cfg.DownloadDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("Build did not create DownloadDir %s: %v", cfg.DownloadDir, err)
	}
}

func TestDownloadsFileServer(t *testing.T) {
	h, cfg := buildTestModule(t)
	require.NoError(t, os.WriteFile(filepath.Join(cfg.DownloadDir, "done.m4v"), []byte("VIDEO BYTES"), 0o644))

	res := req(h, http.MethodGet, "/downloads/done.m4v", nil)
	if res.Code != http.StatusOK || res.Body.String() != "VIDEO BYTES" {
		t.Fatalf("GET /downloads/done.m4v = %d %q, want 200 with planted bytes", res.Code, res.Body)
	}
}

// TestDefaultCookiesFileNotServedByDownloadsRoute pins a security property of
// the D5 Fix 2 default location: an unconfigured cookies_file must resolve
// outside DownloadDir, so the cookie jar (a real credential, unlike
// settings.json/history.json) is never reachable over the unauthenticated
// /downloads/ static route.
func TestDefaultCookiesFileNotServedByDownloadsRoute(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 0)
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, downloadOnlyExec{})
	require.NoError(t, err)

	got := decodeSettings(t, req(h, http.MethodPost, "/settings.json",
		strings.NewReader(`{"cookies_txt":"# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tCONSENT\tYES+1\n"}`)))
	require.Equal(t, true, got["cookies_configured"])

	res := req(h, http.MethodGet, "/downloads/utuber-cookies.txt", nil)
	require.NotEqual(t, http.StatusOK, res.Code, "the cookie jar must not be reachable via /downloads/")
}

// ── enqueue ─────────────────────────────────────────────────────────────────

func TestJobsEmptyAndEnqueueValidation(t *testing.T) {
	h, _ := buildTestModule(t)

	if got := jobsJSON(t, h); len(got) != 0 {
		t.Errorf("GET /jobs.json = %v, want empty", got)
	}
	noURL := req(h, http.MethodPost, "/enqueue", nil)
	if noURL.Code != http.StatusBadRequest {
		t.Fatalf("POST /enqueue without url = %d, want 400", noURL.Code)
	}
}

func TestEnqueueDuplicateBlockedAndForced(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 0)
	require.NoError(t, os.MkdirAll(cfg.DownloadDir, 0o755))
	hist, err := history.Open(filepath.Join(cfg.DownloadDir, "history.json"))
	require.NoError(t, err)
	require.NoError(t, hist.Record(history.Entry{URL: "http://dup.com", OutputFile: "dup.mp4", ShowName: "Dup Show", Mode: "video"}))

	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, downloadOnlyExec{})
	require.NoError(t, err)

	dup := req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fdup.com", nil)
	require.Equal(t, http.StatusConflict, dup.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(dup.Body.Bytes(), &body))
	require.Equal(t, "duplicate", body["error"])
	require.Equal(t, "dup.mp4", body["output_file"])
	require.Equal(t, "Dup Show", body["show_name"])
	require.Equal(t, "video", body["mode"])
	require.Len(t, jobsJSON(t, h), 0, "the duplicate must not be enqueued")

	forced := req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fdup.com&force=1", nil)
	require.Equal(t, http.StatusNoContent, forced.Code)
	require.Len(t, jobsJSON(t, h), 1, "the forced duplicate must be enqueued")
}

// TestJobsWireFormat pins the /jobs.json contract: exactly 15 snake_case keys
// and status "success" (not "completed", N5) once the job has run.
func TestJobsWireFormat(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 1) // width 1 so the fake job runs
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, downloadOnlyExec{})
	require.NoError(t, err)

	enq := req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fv&show=Cool+Artist&title=Cool+Track&season=2&episode=5&mode=video", nil)
	require.Equal(t, http.StatusNoContent, enq.Code)

	require.Eventually(t, func() bool {
		jobs := jobsJSON(t, h)
		return len(jobs) == 1 && jobs[0]["status"] == "success"
	}, 10*time.Second, 20*time.Millisecond)

	jobs := jobsJSON(t, h)
	want := []string{
		"id", "status", "label", "url", "show_name", "episode_title",
		"season", "episode", "mode", "progress", "output_file", "error",
		"created_at", "started_at", "finished_at",
	}
	var got []string
	for k := range jobs[0] {
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	require.Equal(t, want, got, "wire keys")
	require.Equal(t, "success", jobs[0]["status"])
	require.NotEmpty(t, jobs[0]["output_file"], "a successful job must carry its output file")

	// history.json on disk is updated so a re-enqueue of the same URL dedupes.
	histData, err := os.ReadFile(filepath.Join(cfg.DownloadDir, "history.json"))
	require.NoError(t, err)
	require.Contains(t, string(histData), "http://example.com/v", "history.json must record the completed download")
}

// TestWidthZeroNeverRuns pins R7: a lane seeded at width 0 never claims a
// submitted job.
func TestWidthZeroNeverRuns(t *testing.T) {
	h, _ := buildTestModule(t) // width 0
	enq := req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fv", nil)
	require.Equal(t, http.StatusNoContent, enq.Code)

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		jobs := jobsJSON(t, h)
		require.Len(t, jobs, 1)
		require.Equal(t, "queued", jobs[0]["status"], "a width-0 lane must never run a job")
		time.Sleep(20 * time.Millisecond)
	}
}

// ── cancel / rerun / delete matrix ──────────────────────────────────────────

func TestJobActionStatusMatrix(t *testing.T) {
	h, _ := buildTestModule(t)

	// 405 on GET for every action route (no Allow header, mirroring style).
	for _, path := range []string{"/jobs/cancel", "/jobs/rerun", "/jobs/delete"} {
		if code := req(h, http.MethodGet, path+"?id=x", nil).Code; code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s = %d, want 405", path, code)
		}
	}
	// 404 for an unknown id.
	require.Equal(t, http.StatusNotFound, req(h, http.MethodPost, "/jobs/cancel?id=nope", nil).Code)
	require.Equal(t, http.StatusNotFound, req(h, http.MethodPost, "/jobs/rerun?id=nope", nil).Code)
	require.Equal(t, http.StatusNotFound, req(h, http.MethodPost, "/jobs/delete?id=nope", nil).Code)

	// Cancel a queued job -> 200; then delete it -> 204.
	require.Equal(t, http.StatusNoContent, req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fq", nil).Code)
	id := jobsJSON(t, h)[0]["id"].(string)

	cancel := req(h, http.MethodPost, "/jobs/cancel?id="+id, nil)
	require.Equal(t, http.StatusOK, cancel.Code)
	var co map[string]string
	require.NoError(t, json.Unmarshal(cancel.Body.Bytes(), &co))
	require.Contains(t, []string{"canceled", "canceling"}, co["status"])

	require.Equal(t, http.StatusNoContent, req(h, http.MethodPost, "/jobs/delete?id="+id, nil).Code)
}

// TestCancelAndRerunOnSucceededJob covers the finished-job branches: cancel of
// a finished job -> 409 not_running; rerun of a succeeded job -> 409 (N5/P18).
func TestCancelAndRerunOnSucceededJob(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 1)
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, downloadOnlyExec{})
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent, req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fs&show=S&title=E&mode=video", nil).Code)
	require.Eventually(t, func() bool {
		jobs := jobsJSON(t, h)
		return len(jobs) == 1 && jobs[0]["status"] == "success"
	}, 10*time.Second, 20*time.Millisecond)

	id := jobsJSON(t, h)[0]["id"].(string)

	cancel := req(h, http.MethodPost, "/jobs/cancel?id="+id, nil)
	require.Equal(t, http.StatusConflict, cancel.Code)
	require.Contains(t, cancel.Body.String(), "already finished")

	rerun := req(h, http.MethodPost, "/jobs/rerun?id="+id, nil)
	require.Equal(t, http.StatusConflict, rerun.Code)
	require.Contains(t, rerun.Body.String(), "already succeeded")
}

// TestDeleteRunningJobRefused: a running job can't be removed (409). We hold a
// job running via a blocking fake executor.
func TestDeleteRunningJobRefused(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 1)
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h, err := buildWith(cfg, e, blockingExec{release: release})
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent, req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fr&show=S&title=E&mode=video", nil).Code)
	require.Eventually(t, func() bool {
		jobs := jobsJSON(t, h)
		return len(jobs) == 1 && jobs[0]["status"] == "running"
	}, 10*time.Second, 20*time.Millisecond)

	id := jobsJSON(t, h)[0]["id"].(string)
	del := req(h, http.MethodPost, "/jobs/delete?id="+id, nil)
	require.Equal(t, http.StatusConflict, del.Code)
	require.Contains(t, del.Body.String(), "running")
}

// ── output streaming (SSE) ──────────────────────────────────────────────────

// TestJobOutputStreamsSeparateStreams pins Feature 2 end-to-end: a func job's
// stdout and stderr are captured separately (Feature 1) and GET
// /jobs/output?id=… replays both over the shared taskmaster streaming path
// (Feature 2), each SSE "output" event tagged with the stream it came from.
// This exercises the whole chain — processor.run's onStdout/onStderr closures,
// runContext.Log()/Stderr(), the OutputRegistry captures, and
// worker.StreamExecutionOutput via lane.StreamOutput — with no coordinator
// route mounted (utuber runs headless).
func TestJobOutputStreamsSeparateStreams(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 1) // width 1 so the fake job runs
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, stdoutStderrExec{})
	require.NoError(t, err)

	require.Equal(t, http.StatusNoContent,
		req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fout&show=S&title=E&mode=video", nil).Code)
	require.Eventually(t, func() bool {
		jobs := jobsJSON(t, h)
		return len(jobs) == 1 && jobs[0]["status"] == "success"
	}, 10*time.Second, 20*time.Millisecond)

	id := jobsJSON(t, h)[0]["id"].(string)

	// Unknown id -> 404 (job not found).
	require.Equal(t, http.StatusNotFound, req(h, http.MethodGet, "/jobs/output?id=does-not-exist", nil).Code)

	// The finished execution's captured lines are still in the registry's
	// replay window, so the stream replays both and then closes.
	rr := req(h, http.MethodGet, "/jobs/output?id="+id, nil)
	require.Equal(t, http.StatusOK, rr.Code)

	streams := parseSSEOutput(t, rr.Body.String())
	require.Equal(t, "hello-from-stdout", streams["stdout"], "stdout line must be captured and tagged stdout")
	require.Equal(t, "hello-from-stderr", streams["stderr"], "stderr line must be captured and tagged stderr")
	require.Contains(t, rr.Body.String(), "event: done", "the stream must close with a done event once both streams are finished")
}

// parseSSEOutput collects the last "line" seen per stream from an SSE body's
// `event: output` frames.
func parseSSEOutput(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, raw := range strings.Split(body, "\n") {
		if !strings.HasPrefix(raw, "data: {") {
			continue
		}
		var payload struct {
			Stream string `json:"stream"`
			Line   string `json:"line"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(raw, "data: ")), &payload); err != nil {
			continue
		}
		if payload.Stream != "" {
			out[payload.Stream] = payload.Line
		}
	}
	return out
}

// ── settings ────────────────────────────────────────────────────────────────

func decodeSettings(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var m map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &m))
	return m
}

func TestSettingsRoundTripAndPartialPost(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 2)
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, downloadOnlyExec{})
	require.NoError(t, err)

	// GET: exactly 9 keys with the seeded values (D8's 7 plus D5's
	// cookies_configured/cookies_updated_at).
	got := decodeSettings(t, req(h, http.MethodGet, "/settings.json", nil))
	wantKeys := []string{
		"python_bin", "age_out_days", "concurrent_downloads", "show_in_taskmaster",
		"queue_paused", "queue_paused_by", "brake_engaged",
		"cookies_configured", "cookies_updated_at",
	}
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.Strings(wantKeys)
	require.Equal(t, wantKeys, keys)
	require.Equal(t, "python3.12", got["python_bin"])
	require.Equal(t, float64(10), got["age_out_days"])
	require.Equal(t, float64(2), got["concurrent_downloads"])
	require.Equal(t, true, got["show_in_taskmaster"])
	require.Equal(t, false, got["queue_paused"])

	// Partial POST of age_out_days leaves python_bin untouched (N6).
	got = decodeSettings(t, req(h, http.MethodPost, "/settings.json", strings.NewReader(`{"age_out_days":30}`)))
	require.Equal(t, float64(30), got["age_out_days"])
	require.Equal(t, "python3.12", got["python_bin"], "python_bin must survive a POST that omits it")

	// Set python_bin; age_out_days unchanged.
	got = decodeSettings(t, req(h, http.MethodPost, "/settings.json", strings.NewReader(`{"python_bin":"python3"}`)))
	require.Equal(t, "python3", got["python_bin"])
	require.Equal(t, float64(30), got["age_out_days"])

	// Empty python_bin clears the override back to the config default (N6).
	got = decodeSettings(t, req(h, http.MethodPost, "/settings.json", strings.NewReader(`{"python_bin":""}`)))
	require.Equal(t, "python3.12", got["python_bin"])
	require.Equal(t, float64(30), got["age_out_days"])

	// Invalid value: 400, nothing applied.
	bad := req(h, http.MethodPost, "/settings.json", strings.NewReader(`{"age_out_days":0}`))
	require.Equal(t, http.StatusBadRequest, bad.Code)
	require.Contains(t, bad.Body.String(), "age_out_days")
	got = decodeSettings(t, req(h, http.MethodGet, "/settings.json", nil))
	require.Equal(t, float64(30), got["age_out_days"], "a rejected POST must not change anything")
}

// ── restart / persistence ───────────────────────────────────────────────────

// TestWidthSeedsOnceThenDBAuthoritative pins P8/N7: cfg.Workers seeds the lane
// width only at first creation; after that the DB (☰ menu) wins, and a later
// boot with a different seed does not overwrite it.
func TestWidthSeedsOnceThenDBAuthoritative(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "taskmaster.db")

	cfg := utuberCfg(t, root, 2)
	e1 := openEngine(t, dbPath)
	h1, err := buildWith(cfg, e1, downloadOnlyExec{})
	require.NoError(t, err)
	require.Equal(t, float64(2), decodeSettings(t, req(h1, http.MethodGet, "/settings.json", nil))["concurrent_downloads"])

	// Raise via the menu, then shut down.
	require.Equal(t, float64(4), decodeSettings(t, req(h1, http.MethodPost, "/settings.json", strings.NewReader(`{"concurrent_downloads":4}`)))["concurrent_downloads"])
	require.NoError(t, e1.Close())

	// Reopen with a DIFFERENT seed (7). The DB value (4) must win.
	cfg2 := utuberCfg(t, root, 7)
	cfg2.DownloadDir = cfg.DownloadDir
	e2 := openEngine(t, dbPath)
	h2, err := buildWith(cfg2, e2, downloadOnlyExec{})
	require.NoError(t, err)
	require.Equal(t, float64(4), decodeSettings(t, req(h2, http.MethodGet, "/settings.json", nil))["concurrent_downloads"],
		"the config seed must not overwrite the DB-authoritative width on a later boot")
}

// TestQueuedJobSurvivesRestart pins N8: a queued job persists across an engine
// Close + reopen.
func TestQueuedJobSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "taskmaster.db")
	cfg := utuberCfg(t, root, 0) // width 0: it stays queued

	e1 := openEngine(t, dbPath)
	h1, err := buildWith(cfg, e1, downloadOnlyExec{})
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, req(h1, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fpersist", nil).Code)
	require.Len(t, jobsJSON(t, h1), 1)
	require.NoError(t, e1.Close())

	e2 := openEngine(t, dbPath)
	h2, err := buildWith(cfg, e2, downloadOnlyExec{})
	require.NoError(t, err)
	jobs := jobsJSON(t, h2)
	require.Len(t, jobs, 1, "the queued job must survive a restart")
	require.Equal(t, "queued", jobs[0]["status"])
	require.Equal(t, "http://example.com/persist", jobs[0]["url"])
}

// TestHiddenLaneStillRunsJob pins that hiding the lane from the taskmaster
// board does not stop its jobs from running (D9-adjacent; utuber-side).
func TestHiddenLaneStillRunsJob(t *testing.T) {
	root := t.TempDir()
	cfg := utuberCfg(t, root, 1)
	e := openEngine(t, filepath.Join(root, "taskmaster.db"))
	h, err := buildWith(cfg, e, downloadOnlyExec{})
	require.NoError(t, err)

	// Hide the lane.
	got := decodeSettings(t, req(h, http.MethodPost, "/settings.json", strings.NewReader(`{"show_in_taskmaster":false}`)))
	require.Equal(t, false, got["show_in_taskmaster"])

	require.Equal(t, http.StatusNoContent, req(h, http.MethodPost, "/enqueue?url=http%3A%2F%2Fexample.com%2Fhidden&show=S&title=E&mode=video", nil).Code)
	require.Eventually(t, func() bool {
		jobs := jobsJSON(t, h)
		return len(jobs) == 1 && jobs[0]["status"] == "success"
	}, 10*time.Second, 20*time.Millisecond, "a hidden lane must still run its job")
}
