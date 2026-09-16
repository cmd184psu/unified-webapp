package media

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// captureExec records every Run call so tests can assert on args and simulate output.
type captureExec struct {
	name  string
	args  []string
	lines []string
	err   error
}

func (c *captureExec) Run(_ context.Context, name string, args []string, onLine func(string)) error {
	c.name = name
	c.args = args
	for _, l := range c.lines {
		onLine(l)
	}
	return c.err
}

// ── Progress regex ────────────────────────────────────────────────────────────

func TestProgressRegexMatches(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"[download]  45.2% of 100MiB", "45.2"},
		{"[download] 100.0% of 100MiB", "100.0"},
		{"[download]   0.3% of 100MiB", "0.3"},
	}
	for _, tc := range cases {
		m := progressRegex.FindStringSubmatch(tc.line)
		if len(m) < 2 || m[1] != tc.want {
			t.Errorf("line %q: got %v, want %q", tc.line, m, tc.want)
		}
	}
}

func TestProgressRegexNoMatch(t *testing.T) {
	noMatch := []string{
		"[info] downloading",
		"WARNING: something",
		"",
	}
	for _, l := range noMatch {
		if m := progressRegex.FindStringSubmatch(l); len(m) > 1 {
			t.Errorf("line %q should not match, got %v", l, m)
		}
	}
}

// ── Download ──────────────────────────────────────────────────────────────────

func TestDownloadInvokesYtDlp(t *testing.T) {
	dir := t.TempDir()
	exec := &captureExec{}

	got, _ := Download(context.Background(), exec, "http://example.com", dir, "out", func(string) {})

	if exec.name != "yt-dlp" {
		t.Errorf("expected yt-dlp, got %q", exec.name)
	}
	// The output name is deterministic: "<stem>.mp4", not a title-based glob.
	if want := filepath.Join(dir, "out.mp4"); got != want {
		t.Errorf("returned path: got %q, want %q", got, want)
	}
	joined := strings.Join(exec.args, " ")
	if !strings.Contains(joined, "http://example.com") {
		t.Errorf("URL not in args: %s", joined)
	}
	if !strings.Contains(joined, filepath.Join(dir, "out.%(ext)s")) {
		t.Errorf("stem-based output template not in args: %s", joined)
	}
	if strings.Contains(joined, "%(title)s") {
		t.Errorf("title-based output template must be gone: %s", joined)
	}
	if !strings.Contains(joined, "--merge-output-format mp4") {
		t.Errorf("merge-output-format mp4 not in args: %s", joined)
	}
	if !strings.Contains(joined, "--remux-video mp4") {
		t.Errorf("remux-video mp4 (guarantees the .mp4 container) not in args: %s", joined)
	}
	// Apple compatibility: cap at 720p and prefer H.264 (avc1) + AAC (mp4a),
	// which QuickTime / TV.app / Apple TV can play, over the old
	// "bestvideo+bestaudio" default (up to 4K, often VP9/AV1 + Opus).
	if !strings.Contains(joined, "height<=720") {
		t.Errorf("720p cap not in format selector: %s", joined)
	}
	if !strings.Contains(joined, "vcodec^=avc1") {
		t.Errorf("H.264 (avc1) preference not in format selector: %s", joined)
	}
	if strings.Contains(joined, "bestvideo+bestaudio") {
		t.Errorf("old uncapped bestvideo+bestaudio selector still present: %s", joined)
	}
	// Subtitles are retained as soft tracks, not burned in: --embed-subs muxes
	// them as a selectable mov_text stream; no ffmpeg burn-in filter is passed.
	if !strings.Contains(joined, "--embed-subs") {
		t.Errorf("--embed-subs not in args: %s", joined)
	}
	if strings.Contains(joined, "subtitles=") || strings.Contains(joined, "burn") {
		t.Errorf("subtitles must not be burned in: %s", joined)
	}
}

func TestDownloadProgressCallback(t *testing.T) {
	dir := t.TempDir()
	exec := &captureExec{lines: []string{
		"[download]  10.0% of 50MiB",
		"[download]  55.5% of 50MiB",
		"[download] 100.0% of 50MiB",
		"[info] some other line",
	}}

	var got []string
	Download(context.Background(), exec, "http://x.com", dir, "out", func(s string) {
		got = append(got, s)
	})

	want := []string{"10.0", "55.5", "100.0"}
	if len(got) != len(want) {
		t.Fatalf("progress calls: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDownloadReturnsExecError(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("yt-dlp failed")
	exec := &captureExec{err: boom}

	_, err := Download(context.Background(), exec, "http://x.com", dir, "out", func(string) {})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}

// ── ExtractAudio ──────────────────────────────────────────────────────────────

func TestExtractAudioInvokesFfmpeg(t *testing.T) {
	exec := &captureExec{}
	ExtractAudio(context.Background(), exec, "in.mp4", "out.mp3", func(string) {})

	if exec.name != "ffmpeg" {
		t.Errorf("expected ffmpeg, got %q", exec.name)
	}
	joined := strings.Join(exec.args, " ")
	for _, want := range []string{"-vn", "libmp3lame", "in.mp4", "out.mp3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("arg %q not found in: %s", want, joined)
		}
	}
}

func TestExtractAudioReturnsExecError(t *testing.T) {
	boom := errors.New("ffmpeg failed")
	exec := &captureExec{err: boom}

	err := ExtractAudio(context.Background(), exec, "in.mp4", "out.mp3", func(string) {})
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}

// ── FetchMeta ─────────────────────────────────────────────────────────────────

func TestFetchMetaParsesOutput(t *testing.T) {
	exec := &captureExec{lines: []string{"Some Uploader|||Some Title"}}
	m, err := FetchMeta(context.Background(), exec, "http://x.com")
	if err != nil {
		t.Fatal(err)
	}
	if m.Uploader != "Some Uploader" {
		t.Errorf("Uploader: got %q", m.Uploader)
	}
	if m.Title != "Some Title" {
		t.Errorf("Title: got %q", m.Title)
	}
}

func TestFetchMetaNoMatchReturnsEmpty(t *testing.T) {
	exec := &captureExec{lines: []string{"no separator here", "[info] blah"}}
	m, err := FetchMeta(context.Background(), exec, "http://x.com")
	if err != nil {
		t.Fatal(err)
	}
	if m.Uploader != "" || m.Title != "" {
		t.Errorf("expected empty meta, got %+v", m)
	}
}

func TestFetchMetaReturnsExecError(t *testing.T) {
	boom := errors.New("yt-dlp error")
	exec := &captureExec{err: boom}
	_, err := FetchMeta(context.Background(), exec, "http://x.com")
	if !errors.Is(err, boom) {
		t.Errorf("got %v, want %v", err, boom)
	}
}
