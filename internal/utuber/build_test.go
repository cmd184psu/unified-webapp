package utuber

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

// buildTestModule builds the module with a planted index.html and a download
// dir under t.TempDir(). Workers: 0 is the test-safety contract from
// build.go: no worker goroutines, so nothing ever invokes the real
// media.OSExecutor that Build wires in.
func buildTestModule(t *testing.T) (http.Handler, config.UtuberConfig) {
	t.Helper()
	root := t.TempDir()
	staticDir := filepath.Join(root, "web")
	if err := os.MkdirAll(staticDir, 0o755); err != nil {
		t.Fatalf("mkdir static: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("UTUBER INDEX"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	cfg := config.UtuberConfig{
		StaticDir:   staticDir,
		DownloadDir: filepath.Join(root, "dl"),
		Workers:     0,
		PythonBin:   "python3.12",
	}
	h, err := Build(cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return h, cfg
}

func TestBuildServesIndexAndFallsBackOnUnknownPath(t *testing.T) {
	h, _ := buildTestModule(t)

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/", nil))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "UTUBER INDEX") {
		t.Fatalf("GET / = %d %q, want 200 with index body", res.Code, res.Body)
	}

	// Declared deviation (P4 category 4): the reference's bare http.FileServer
	// returned 404 here; the module falls back to index.html with 200,
	// matching the sibling modules' equivalent miss-path fallback.
	miss := httptest.NewRecorder()
	h.ServeHTTP(miss, httptest.NewRequest(http.MethodGet, "/no/such/path", nil))
	if miss.Code != http.StatusOK || !strings.Contains(miss.Body.String(), "UTUBER INDEX") {
		t.Fatalf("GET unknown path = %d %q, want 200 with index body", miss.Code, miss.Body)
	}
}

func TestBuildJobsEmptyAndEnqueueValidation(t *testing.T) {
	h, _ := buildTestModule(t)

	jobs := httptest.NewRecorder()
	h.ServeHTTP(jobs, httptest.NewRequest(http.MethodGet, "/jobs.json", nil))
	if jobs.Code != http.StatusOK {
		t.Fatalf("GET /jobs.json = %d, want 200", jobs.Code)
	}
	// json.NewEncoder.Encode appends \n, hence the TrimSpace.
	if got := strings.TrimSpace(jobs.Body.String()); got != "[]" {
		t.Errorf("GET /jobs.json = %q, want []", got)
	}

	noURL := httptest.NewRecorder()
	h.ServeHTTP(noURL, httptest.NewRequest(http.MethodPost, "/enqueue", nil))
	if noURL.Code != http.StatusBadRequest {
		t.Fatalf("POST /enqueue without url = %d, want 400", noURL.Code)
	}
}

// FR-4's wire-format guarantee lives at the HTTP layer: whatever refactors
// happen inside jobs, a browser polling /jobs.json must keep seeing exactly
// these 11 keys. Complements the package-level TestJobJSONKeysUnchanged.
func TestJobsWireFormatElevenKeys(t *testing.T) {
	h, _ := buildTestModule(t)

	enq := httptest.NewRecorder()
	h.ServeHTTP(enq, httptest.NewRequest(http.MethodPost,
		"/enqueue?url=http%3A%2F%2Fexample.com%2Fv&show=Cool+Artist&title=Cool+Track&season=2&episode=5&mode=audio", nil))
	if enq.Code != http.StatusNoContent {
		t.Fatalf("POST /enqueue = %d, want 204 (body %s)", enq.Code, enq.Body)
	}

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/jobs.json", nil))
	var decoded []map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode /jobs.json: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("jobs = %d, want 1", len(decoded))
	}
	want := []string{
		"ID", "URL", "ShowName", "EpisodeTitle", "Season", "Episode",
		"Mode", "Status", "Progress", "OutputFile", "Error",
	}
	var got []string
	for k := range decoded[0] {
		got = append(got, k)
	}
	sort.Strings(want)
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("wire keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("wire keys = %v, want %v", got, want)
		}
	}
}

func TestDownloadsFileServer(t *testing.T) {
	h, cfg := buildTestModule(t)
	if err := os.WriteFile(filepath.Join(cfg.DownloadDir, "done.m4v"), []byte("VIDEO BYTES"), 0o644); err != nil {
		t.Fatalf("plant download: %v", err)
	}

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/downloads/done.m4v", nil))
	if res.Code != http.StatusOK || res.Body.String() != "VIDEO BYTES" {
		t.Fatalf("GET /downloads/done.m4v = %d %q, want 200 with planted bytes", res.Code, res.Body)
	}
}

func TestBuildCreatesDownloadDir(t *testing.T) {
	_, cfg := buildTestModule(t)
	info, err := os.Stat(cfg.DownloadDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("Build did not create DownloadDir %s: %v", cfg.DownloadDir, err)
	}
}

// The 503 path: buildDispatcher turns this error into the scoped
// unavailableHandler, whose body must name the offending path — so the error
// has to carry DownloadDir verbatim. DownloadDir is the regular file ITSELF
// (not a path under it): MkdirAll's first Stat then fails with !IsDir() and
// the PathError names DownloadDir, not its parent.
func TestBuildFailsWhenDownloadDirCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	_, err := Build(config.UtuberConfig{
		StaticDir:   t.TempDir(),
		DownloadDir: blocker,
		Workers:     0,
		PythonBin:   "python3.12",
	})
	if err == nil {
		t.Fatal("Build succeeded with a file as DownloadDir, want error")
	}
	if !strings.Contains(err.Error(), blocker) {
		t.Errorf("Build error %q does not name the configured path %q", err, blocker)
	}
}

// Pins the test-safety contract the build.go comment documents: Workers: 0
// starts zero workers. If someone "hardens" Workers <= 0 to 1 in Build, this
// fails before `make test` starts downloading the internet (yt-dlp IS on
// this host's PATH and Build wires the real media.OSExecutor).
func TestWorkersZeroStartsNoWorkers(t *testing.T) {
	h, _ := buildTestModule(t)

	enq := httptest.NewRecorder()
	h.ServeHTTP(enq, httptest.NewRequest(http.MethodPost,
		"/enqueue?url=http%3A%2F%2Fexample.com%2Fv", nil))
	if enq.Code != http.StatusNoContent {
		t.Fatalf("POST /enqueue = %d, want 204", enq.Code)
	}

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		res := httptest.NewRecorder()
		h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/jobs.json", nil))
		var decoded []struct {
			Status string
		}
		if err := json.Unmarshal(res.Body.Bytes(), &decoded); err != nil {
			t.Fatalf("decode /jobs.json: %v", err)
		}
		if len(decoded) != 1 {
			t.Fatalf("jobs = %d, want 1", len(decoded))
		}
		if decoded[0].Status != "queued" {
			t.Fatalf("job status = %q — a worker picked it up with Workers: 0", decoded[0].Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
