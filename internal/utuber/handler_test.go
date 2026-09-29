package utuber

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/utuber/history"
)

// ── recording RunContext ────────────────────────────────────────────────────

type progressCall struct {
	pct   int
	label string
}

// fakeRunContext records the ordered Progress calls, SetLabel calls, and
// everything written to Log(), with fixed JobID/ExecID so the download stem is
// deterministic. It implements golane.RunContext.
type fakeRunContext struct {
	jobID  string
	execID int64

	mu       sync.Mutex
	progress []progressCall
	labels   []string
	log      bytes.Buffer
	errLog   bytes.Buffer
}

func (rc *fakeRunContext) JobID() string { return rc.jobID }
func (rc *fakeRunContext) ExecID() int64 { return rc.execID }

func (rc *fakeRunContext) Progress(pct int, label string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.progress = append(rc.progress, progressCall{pct, label})
}

func (rc *fakeRunContext) SetLabel(label string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.labels = append(rc.labels, label)
}

func (rc *fakeRunContext) Log() io.Writer { return (*rcLogWriter)(rc) }

type rcLogWriter fakeRunContext

func (w *rcLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.log.Write(p)
}

func (rc *fakeRunContext) Stderr() io.Writer { return (*rcErrWriter)(rc) }

type rcErrWriter fakeRunContext

func (w *rcErrWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.errLog.Write(p)
}

func (rc *fakeRunContext) errString() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.errLog.String()
}

func (rc *fakeRunContext) lastLabel() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if len(rc.labels) == 0 {
		return ""
	}
	return rc.labels[len(rc.labels)-1]
}

func (rc *fakeRunContext) logString() string {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return rc.log.String()
}

func (rc *fakeRunContext) progressCalls() []progressCall {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]progressCall(nil), rc.progress...)
}

// ── fake executors ──────────────────────────────────────────────────────────

// simulateDownload writes the "<stem>.mp4" that the deterministic
// media.Download returns, so the processor's rename (video) or extract (audio)
// step finds a real file. It fires only on the yt-dlp download call — the one
// carrying -o — and is a no-op for FetchMeta (--print, no -o) and ExtractAudio
// (ffmpeg, no -o).
func simulateDownload(args []string) {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-o" {
			out := strings.Replace(args[i+1], "%(ext)s", "mp4", 1)
			_ = os.WriteFile(out, []byte("video"), 0o644)
			return
		}
	}
}

// downloadOnlyExec creates the deterministic download output on the yt-dlp call
// and does nothing else.
type downloadOnlyExec struct{}

func (downloadOnlyExec) Run(_ context.Context, _ string, args []string, _, _ func(string)) error {
	simulateDownload(args)
	return nil
}

// metaAndDownloadExec emits metaLine on the FetchMeta call (identified by the
// metadata separator in its --print arg) and creates the download output on
// the yt-dlp call.
type metaAndDownloadExec struct{ metaLine string }

func (e metaAndDownloadExec) Run(_ context.Context, _ string, args []string, onStdout, onStderr func(string)) error {
	if hasArg(args, "--print") {
		onStdout(e.metaLine)
		return nil
	}
	simulateDownload(args)
	return nil
}

// errorExec fails every call.
type errorExec struct{ err error }

func (e errorExec) Run(_ context.Context, _ string, _ []string, _, _ func(string)) error {
	return e.err
}

// audioExtractErrorExec downloads fine but fails the ffmpeg extraction.
type audioExtractErrorExec struct{ err error }

func (e audioExtractErrorExec) Run(_ context.Context, name string, args []string, _, _ func(string)) error {
	if name == "ffmpeg" {
		return e.err
	}
	simulateDownload(args)
	return nil
}

// lineEmittingExec emits one raw line on the yt-dlp download call (to exercise
// the raw-line-to-Log() + parsed-progress path) then creates the output file.
type lineEmittingExec struct{ downloadLine string }

func (e lineEmittingExec) Run(_ context.Context, _ string, args []string, onStdout, onStderr func(string)) error {
	if hasArg(args, "-o") {
		if onStdout != nil && e.downloadLine != "" {
			onStdout(e.downloadLine)
		}
		simulateDownload(args)
	}
	return nil
}

// lineThenFailExec emits one non-error raw line on the yt-dlp download call,
// then fails with err — used to prove Fix 1 leaves the original error
// untouched when nothing logged ever matched "ERROR:".
type lineThenFailExec struct {
	line string
	err  error
}

func (e lineThenFailExec) Run(_ context.Context, _ string, args []string, onStdout, onStderr func(string)) error {
	if hasArg(args, "-o") {
		if onStdout != nil && e.line != "" {
			onStdout(e.line)
		}
		return e.err
	}
	return nil
}

// ageRestrictedExec simulates yt-dlp's real age-restriction failure: it emits
// an informational line on stdout and the final "ERROR:" line on stderr (where
// yt-dlp actually writes it), then returns a generic non-zero-exit error —
// exactly what OSExecutor.Run returns from cmd.Run() on a real failure (D5 Fix
// 1's target case: "exit status 1" must not be the job's recorded error). It
// deliberately puts the ERROR: line on stderr to prove lastErrorLine capture
// works via the onStderrLine closure now that streams are captured separately.
type ageRestrictedExec struct{}

func (ageRestrictedExec) Run(_ context.Context, _ string, args []string, onStdout, onStderr func(string)) error {
	if hasArg(args, "-o") {
		if onStdout != nil {
			onStdout("[youtube] Extracting URL")
		}
		if onStderr != nil {
			onStderr("ERROR: [youtube] abc123: Sign in to confirm your age. This video may be inappropriate for some users. Use --cookies-from-browser or --cookies")
		}
		return errors.New("exit status 1")
	}
	return nil
}

// stdoutStderrExec emits one distinguishable line on each of stdout and stderr
// during the yt-dlp download call, then creates the deterministic output file.
// It proves separate-stream capture end-to-end: the stdout line must reach the
// execution's stdout capture and the stderr line its stderr capture.
type stdoutStderrExec struct{}

func (stdoutStderrExec) Run(_ context.Context, _ string, args []string, onStdout, onStderr func(string)) error {
	if hasArg(args, "-o") {
		if onStdout != nil {
			onStdout("hello-from-stdout")
		}
		if onStderr != nil {
			onStderr("hello-from-stderr")
		}
		simulateDownload(args)
	}
	return nil
}

// cookiesRecordingExec records the args passed on the yt-dlp download call,
// so tests can assert on --cookies presence/absence at the processor.run
// layer (D5 Fix 2).
type cookiesRecordingExec struct{ args []string }

func (e *cookiesRecordingExec) Run(_ context.Context, _ string, args []string, _, _ func(string)) error {
	if hasArg(args, "-o") {
		e.args = append([]string(nil), args...)
		simulateDownload(args)
	}
	return nil
}

// blockingExec blocks the yt-dlp download call until release is closed (or the
// context is canceled), so a test can observe a job in the "running" state.
type blockingExec struct{ release chan struct{} }

func (e blockingExec) Run(ctx context.Context, _ string, args []string, _, _ func(string)) error {
	if hasArg(args, "-o") {
		select {
		case <-e.release:
		case <-ctx.Done():
			return ctx.Err()
		}
		simulateDownload(args)
	}
	return nil
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func newHist(t *testing.T) *history.Log {
	t.Helper()
	l, err := history.Open(filepath.Join(t.TempDir(), "history.json"))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// ── processor.run ───────────────────────────────────────────────────────────

func TestRunFillsMetadataWhenBlank(t *testing.T) {
	dir := t.TempDir()
	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	p := processor{
		exec: metaAndDownloadExec{metaLine: "Cool Artist|||Cool Track"},
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: hist,
	}
	rc := &fakeRunContext{jobID: "m1", execID: 1}

	res, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://meta.com", Season: 1, Episode: 1, Mode: "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := res.(downloadResult)
	want := "Cool Artist - S01E01 - Cool Track.mp4"
	if got.OutputFile != want {
		t.Errorf("OutputFile: got %q, want %q", got.OutputFile, want)
	}
	if got.ShowName != "Cool Artist" || got.EpisodeTitle != "Cool Track" {
		t.Errorf("result metadata: got %q / %q", got.ShowName, got.EpisodeTitle)
	}
	if rc.lastLabel() != "Cool Artist — Cool Track" {
		t.Errorf("SetLabel: got %q", rc.lastLabel())
	}
	if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
		t.Errorf("output file not found: %v", err)
	}
	prior := hist.Lookup("http://meta.com")
	if prior == nil || prior.OutputFile != want || prior.ShowName != "Cool Artist" {
		t.Errorf("history not recorded correctly: %+v", prior)
	}
}

func TestRunKeepsExistingMetadata(t *testing.T) {
	dir := t.TempDir()
	p := processor{exec: downloadOnlyExec{}, cfg: config.UtuberConfig{DownloadDir: dir}, hist: newHist(t)}
	rc := &fakeRunContext{jobID: "m2", execID: 1}

	res, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://meta2.com", ShowName: "My Show", EpisodeTitle: "My Ep",
		Season: 1, Episode: 1, Mode: "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.(downloadResult); got.ShowName != "My Show" || got.EpisodeTitle != "My Ep" {
		t.Errorf("metadata should not be overwritten, got %q / %q", got.ShowName, got.EpisodeTitle)
	}
	if rc.lastLabel() != "My Show — My Ep" {
		t.Errorf("SetLabel: got %q", rc.lastLabel())
	}
}

func TestRunVideoMode(t *testing.T) {
	dir := t.TempDir()
	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	p := processor{exec: downloadOnlyExec{}, cfg: config.UtuberConfig{DownloadDir: dir}, hist: hist}
	rc := &fakeRunContext{jobID: "v1", execID: 7}

	res, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://x.com", ShowName: "Show", EpisodeTitle: "Ep",
		Season: 1, Episode: 2, Mode: "video",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Show - S01E02 - Ep.mp4"
	if got := res.(downloadResult); got.OutputFile != want {
		t.Errorf("OutputFile: got %q, want %q", got.OutputFile, want)
	}
	if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
		t.Errorf("output file not found: %v", err)
	}
	if hist.Lookup("http://x.com") == nil {
		t.Error("history not recorded")
	}
}

func TestRunAudioMode(t *testing.T) {
	dir := t.TempDir()
	hist, _ := history.Open(filepath.Join(dir, "history.json"))
	p := processor{exec: downloadOnlyExec{}, cfg: config.UtuberConfig{DownloadDir: dir}, hist: hist}
	rc := &fakeRunContext{jobID: "a1", execID: 3}

	res, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://y.com", ShowName: "Artist", EpisodeTitle: "Track",
		Season: 1, Episode: 1, Mode: "audio",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "Artist - S01E01 - Track.mp3"
	if got := res.(downloadResult); got.OutputFile != want {
		t.Errorf("OutputFile: got %q, want %q", got.OutputFile, want)
	}
	// The downloaded temp ("<jobID>-<execID>.mp4") is removed after extraction.
	src := filepath.Join(dir, "a1-3.mp4")
	if _, err := os.Stat(src); !errors.Is(err, os.ErrNotExist) {
		t.Error("downloaded temp file should have been removed after audio extraction")
	}
}

func TestRunAudioExtractionError(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("ffmpeg failed")
	p := processor{exec: audioExtractErrorExec{err: boom}, cfg: config.UtuberConfig{DownloadDir: dir}, hist: newHist(t)}
	rc := &fakeRunContext{jobID: "ae1", execID: 1}

	_, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://a.com", ShowName: "S", EpisodeTitle: "E", Mode: "audio",
	})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
	// The temp download is removed even when extraction fails.
	if _, err := os.Stat(filepath.Join(dir, "ae1-1.mp4")); !errors.Is(err, os.ErrNotExist) {
		t.Error("temp file should have been removed after failed extraction")
	}
}

func TestRunDownloadError(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("download failed")
	p := processor{exec: errorExec{err: boom}, cfg: config.UtuberConfig{DownloadDir: dir}, hist: newHist(t)}
	rc := &fakeRunContext{jobID: "e1", execID: 1}

	_, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://z.com", ShowName: "S", EpisodeTitle: "E", Mode: "video",
	})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}

// TestRunSurfacesYtdlpErrorReason pins D5 Fix 1: when yt-dlp's output
// contains an "ERROR:" line and the execution ultimately fails, the error
// processor.run returns names yt-dlp's stated reason (age-restriction),
// not the generic exec error ("exit status 1") that a bare cmd.Run failure
// produces.
func TestRunSurfacesYtdlpErrorReason(t *testing.T) {
	dir := t.TempDir()
	p := processor{exec: ageRestrictedExec{}, cfg: config.UtuberConfig{DownloadDir: dir}, hist: newHist(t)}
	rc := &fakeRunContext{jobID: "age1", execID: 1}

	_, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://age-restricted.example/v", ShowName: "S", EpisodeTitle: "E", Mode: "video",
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "Sign in to confirm your age") {
		t.Errorf("job error = %q, want it to contain yt-dlp's stated reason", err.Error())
	}
	if err.Error() == "exit status 1" {
		t.Error("job error is still the generic exec error, not yt-dlp's ERROR: line")
	}
	// yt-dlp writes the ERROR: line to stderr, so it lands in rc.Stderr()
	// (streams are now captured separately). lastErrorLine capture works across
	// either stream, which is why the surfaced error above still names the
	// reason even though it arrived on stderr.
	if !strings.Contains(rc.errString(), "Sign in to confirm your age") {
		t.Errorf("rc.Stderr() is missing the raw ERROR: line: %q", rc.errString())
	}
	// The informational stdout line reached rc.Log() (D8's contract).
	if !strings.Contains(rc.logString(), "[youtube] Extracting URL") {
		t.Errorf("rc.Log() is missing the raw stdout line: %q", rc.logString())
	}
}

// TestRunDownloadErrorWithoutErrorLineKeepsOriginalErr pins the fallback
// half of D5 Fix 1: when nothing matching "ERROR:" was ever logged, the
// original exec error passes through unchanged (errors.Is must still hold),
// exercised already by TestRunDownloadError above; this variant additionally
// confirms a non-ERROR raw line does not get mistaken for a diagnosis.
func TestRunDownloadErrorWithoutErrorLineKeepsOriginalErr(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("network unreachable")
	exec := lineThenFailExec{line: "[download] some progress notice", err: boom}
	p := processor{exec: exec, cfg: config.UtuberConfig{DownloadDir: dir}, hist: newHist(t)}
	rc := &fakeRunContext{jobID: "noerr1", execID: 1}

	_, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://x.com", ShowName: "S", EpisodeTitle: "E", Mode: "video",
	})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want the original exec error %v (no ERROR: line was ever logged)", err, boom)
	}
}

// TestRunProgressStillParsesAlongsideErrorCapture pins that Fix 1's ERROR:
// capture does not interfere with progress-line parsing on the same stream.
func TestRunProgressStillParsesAlongsideErrorCapture(t *testing.T) {
	dir := t.TempDir()
	p := processor{
		exec: lineEmittingExec{downloadLine: "[download]  45.2% of 50MiB"},
		cfg:  config.UtuberConfig{DownloadDir: dir},
		hist: newHist(t),
	}
	rc := &fakeRunContext{jobID: "prog1", execID: 1}

	if _, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://seq.com", ShowName: "S", EpisodeTitle: "E", Mode: "video",
	}); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, c := range rc.progressCalls() {
		if c.pct == 45 {
			found = true
		}
	}
	if !found {
		t.Errorf("progress calls %v do not include the parsed 45%% from the '[download] 45.2%%' line", rc.progressCalls())
	}
}

// TestRunPassesConfiguredCookiesPathToDownload pins D5 Fix 2's wiring: the
// processor threads settings.CookiesPath() through to media.Download's argv.
func TestRunPassesConfiguredCookiesPathToDownload(t *testing.T) {
	dir := t.TempDir()
	cookiesPath := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(cookiesPath, []byte("# Netscape HTTP Cookie File\n.youtube.com\tTRUE\t/\tTRUE\t1999999999\tCONSENT\tYES+1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := newSettingsStore(filepath.Join(dir, "settings.json"), "", cookiesPath)
	exec := &cookiesRecordingExec{}
	p := processor{exec: exec, cfg: config.UtuberConfig{DownloadDir: dir}, hist: newHist(t), settings: settings}
	rc := &fakeRunContext{jobID: "ck1", execID: 1}

	if _, err := p.run(context.Background(), rc, downloadPayload{
		URL: "http://x.com", ShowName: "S", EpisodeTitle: "E", Mode: "video",
	}); err != nil {
		t.Fatal(err)
	}
	if !hasArg(exec.args, "--cookies") {
		t.Errorf("--cookies not passed through to yt-dlp argv: %v", exec.args)
	}
}

// TestRunProgressSequenceAndRawLog pins both the progress ordering and the D8
// design decision: every raw yt-dlp line reaches rc.Log() while the matching
// lines still drive rc.Progress.
func TestRunProgressSequenceAndRawLog(t *testing.T) {
	for _, mode := range []string{"video", "audio"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			p := processor{
				exec: lineEmittingExec{downloadLine: "[download]  42.0% of 50MiB"},
				cfg:  config.UtuberConfig{DownloadDir: dir},
				hist: newHist(t),
			}
			// Blank metadata so the "Fetching metadata" step fires.
			rc := &fakeRunContext{jobID: "seq-" + mode, execID: 1}
			if _, err := p.run(context.Background(), rc, downloadPayload{
				URL: "http://seq.com", Season: 1, Episode: 1, Mode: mode,
			}); err != nil {
				t.Fatal(err)
			}

			want := []progressCall{
				{-1, "Fetching metadata"},
				{-1, "Downloading"},
				{42, "Downloading"},
			}
			if mode == "audio" {
				want = append(want, progressCall{-1, "Converting"})
			}
			assertProgressSubsequence(t, rc.progressCalls(), want)

			// The raw line reached the log (D5 relies on this).
			if !strings.Contains(rc.logString(), "[download]  42.0% of 50MiB") {
				t.Errorf("raw yt-dlp line missing from rc.Log(): %q", rc.logString())
			}
		})
	}
}

func assertProgressSubsequence(t *testing.T, got, want []progressCall) {
	t.Helper()
	i := 0
	for _, g := range got {
		if i < len(want) && g == want[i] {
			i++
		}
	}
	if i != len(want) {
		t.Fatalf("progress calls %v do not contain ordered subsequence %v (matched %d of %d)", got, want, i, len(want))
	}
}

// ── outputName / safe ───────────────────────────────────────────────────────

func TestOutputNameOmitsZeroSeasonEpisode(t *testing.T) {
	cases := []struct {
		season, episode int
		want            string
	}{
		{1, 2, "Show - S01E02 - Title.mp4"},
		{3, 0, "Show - S03 - Title.mp4"},
		{0, 7, "Show - E07 - Title.mp4"},
		{0, 0, "Show - Title.mp4"},
		{-1, -4, "Show - Title.mp4"},
	}
	for _, c := range cases {
		if got := outputName("Show", c.season, c.episode, "Title", "mp4"); got != c.want {
			t.Errorf("S%d E%d: got %q, want %q", c.season, c.episode, got, c.want)
		}
	}
}

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
