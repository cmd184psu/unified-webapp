package slideshow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/slideshow"
)

// newTestHarness builds a Handler backed by temp imagedir and no music,
// without starting the conductor goroutine.
func newTestHarness(t *testing.T) (*slideshow.Handler, string) {
	t.Helper()
	return newTestHarnessWithMusic(t, "")
}

func newTestHarnessWithMusic(t *testing.T, audioDir string) (*slideshow.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := slideshow.NewStore(dir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	music := slideshow.NewMusicStore(audioDir)
	broker := slideshow.NewSSEBroker()
	cfg := config.SlideshowConfig{
		Prefix:          "slides",
		IntervalSeconds: 8,
		DefaultMode:     "kenburns",
		DefaultTheme:    "dark",
	}
	conductor := slideshow.NewConductor(store, music, broker, cfg)
	return slideshow.NewHandler(store, conductor, broker, music, cfg), dir
}

func serve(t *testing.T, h *slideshow.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Register(mux)
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// ── State endpoint ────────────────────────────────────────────────────────────

func TestGetAPIState_Empty(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "GET", "/api/state", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var state map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if state["total_subjects"].(float64) != 0 {
		t.Errorf("want total_subjects=0, got %v", state["total_subjects"])
	}
	if state["mode"] != "kenburns" {
		t.Errorf("want mode=kenburns, got %v", state["mode"])
	}
	if state["theme"] != "dark" {
		t.Errorf("want theme=dark, got %v", state["theme"])
	}
	if state["controls_position"] != "bottom" {
		t.Errorf("want controls_position=bottom, got %v", state["controls_position"])
	}
	if state["music_enabled"].(bool) {
		t.Error("want music_enabled=false when no audio dir")
	}
}

func TestGetAPIState_WithImages(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo.jpg"), []byte("img"), 0644)

	store, _ := slideshow.NewStore(dir, 0)
	music := slideshow.NewMusicStore("")
	broker := slideshow.NewSSEBroker()
	cfg := config.SlideshowConfig{Prefix: "slides", IntervalSeconds: 8, DefaultMode: "kenburns", DefaultTheme: "dark"}
	conductor := slideshow.NewConductor(store, music, broker, cfg)
	h := slideshow.NewHandler(store, conductor, broker, music, cfg)

	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if state["total_subjects"].(float64) != 1 {
		t.Errorf("want total_subjects=1, got %v", state["total_subjects"])
	}
	if state["subject"] != "beach" {
		t.Errorf("want subject=beach, got %v", state["subject"])
	}
}

func TestGetConfigAlias(t *testing.T) {
	h, _ := newTestHarness(t)
	for _, path := range []string{"/config", "/config/"} {
		w := serve(t, h, "GET", path, "")
		if w.Code != http.StatusOK {
			t.Errorf("%s: status %d", path, w.Code)
		}
		var state map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
			t.Errorf("%s: not JSON: %v", path, err)
		}
		if _, ok := state["mode"]; !ok {
			t.Errorf("%s: missing 'mode' field", path)
		}
	}
}

// ── Control endpoint ──────────────────────────────────────────────────────────

func TestPostAPIControl_Play(t *testing.T) {
	h, _ := newTestHarness(t)
	serve(t, h, "POST", "/api/control", `{"action":"pause"}`)
	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if state["playing"].(bool) {
		t.Error("want playing=false after pause")
	}

	serve(t, h, "POST", "/api/control", `{"action":"play"}`)
	w2 := serve(t, h, "GET", "/api/state", "")
	json.Unmarshal(w2.Body.Bytes(), &state)
	if !state["playing"].(bool) {
		t.Error("want playing=true after play")
	}
}

func TestPostAPIControl_SetMode(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "POST", "/api/control", `{"action":"set-mode","value":"static"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("set-mode: status %d\nbody: %s", w.Code, w.Body.String())
	}
	w2 := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w2.Body.Bytes(), &state)
	if state["mode"] != "static" {
		t.Errorf("want mode=static, got %v", state["mode"])
	}
}

func TestPostAPIControl_SetTheme(t *testing.T) {
	h, _ := newTestHarness(t)
	serve(t, h, "POST", "/api/control", `{"action":"set-theme","value":"light"}`)
	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if state["theme"] != "light" {
		t.Errorf("want theme=light, got %v", state["theme"])
	}
}

func TestPostAPIControl_SetInterval(t *testing.T) {
	h, _ := newTestHarness(t)
	serve(t, h, "POST", "/api/control", `{"action":"set-interval","value":15}`)
	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if state["interval_seconds"].(float64) != 15 {
		t.Errorf("want interval_seconds=15, got %v", state["interval_seconds"])
	}
}

func TestPostAPIControl_SetShuffle(t *testing.T) {
	h, _ := newTestHarness(t)
	serve(t, h, "POST", "/api/control", `{"action":"set-shuffle","value":true}`)
	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if !state["shuffle"].(bool) {
		t.Error("want shuffle=true")
	}
}

func TestPostAPIControl_SetControlsPosition(t *testing.T) {
	h, _ := newTestHarness(t)
	serve(t, h, "POST", "/api/control", `{"action":"set-controls-position","value":"top"}`)
	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if state["controls_position"] != "top" {
		t.Errorf("want controls_position=top, got %v", state["controls_position"])
	}
}

func TestPostAPIControl_BadControlsPosition(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "POST", "/api/control", `{"action":"set-controls-position","value":"left"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid position, got %d", w.Code)
	}
}

func TestPostAPIControl_BadAction(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "POST", "/api/control", `{"action":"unknown-thing"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestPostAPIControl_InvalidJSON(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "POST", "/api/control", "not-json")
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestPostAPIControl_MissingAction(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "POST", "/api/control", `{"value":"something"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── SSE endpoint ──────────────────────────────────────────────────────────────

func TestGetAPIEvents_ContentTypeAndSnapshot(t *testing.T) {
	h, _ := newTestHarness(t)
	mux := http.NewServeMux()
	h.Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	if resp == nil {
		t.Fatal("no response")
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}

	buf := make([]byte, 2048)
	resp.Body.Read(buf) //nolint:errcheck
	body := string(buf)
	if !strings.Contains(body, "event: state") {
		t.Errorf("SSE body missing 'event: state'\ngot: %q", body)
	}
	if !strings.Contains(body, `"mode"`) {
		t.Errorf("SSE snapshot missing mode field\ngot: %q", body)
	}
}

// ── Items endpoint ────────────────────────────────────────────────────────────

func TestHandleSubjects_Empty(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "GET", "/items", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var subjects []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &subjects); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, w.Body.String())
	}
	if len(subjects) != 0 {
		t.Errorf("want empty array, got %d", len(subjects))
	}
}

func TestHandleSubjects_WithImages(t *testing.T) {
	h, dir := newTestHarness(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo.jpg"), []byte("img"), 0644)

	w := serve(t, h, "GET", "/items", "")
	var subjects []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &subjects)
	if len(subjects) != 1 {
		t.Fatalf("want 1 subject, got %d", len(subjects))
	}
	if subjects[0]["subject"] != "beach" {
		t.Errorf("subject name: got %v", subjects[0]["subject"])
	}
}

// ── Image serving ─────────────────────────────────────────────────────────────

func TestHandleImage_ServesFile(t *testing.T) {
	h, dir := newTestHarness(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "photo.jpg"), []byte("JPEG_DATA"), 0644)

	w := serve(t, h, "GET", "/slides/beach/photo.jpg", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "JPEG_DATA") {
		t.Error("response body should contain image data")
	}
}

func TestHandleImage_MissingFile_404(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "GET", "/slides/beach/nonexistent.jpg", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

func TestHandleImage_NonImageExtension_404(t *testing.T) {
	h, dir := newTestHarness(t)
	os.MkdirAll(filepath.Join(dir, "beach"), 0755)
	os.WriteFile(filepath.Join(dir, "beach", "secret.txt"), []byte("secret"), 0644)

	w := serve(t, h, "GET", "/slides/beach/secret.txt", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for non-image extension, got %d", w.Code)
	}
}

func TestHandleImage_DotPrefixSubject_404(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "GET", "/slides/.hidden/photo.jpg", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for dot-prefix subject, got %d", w.Code)
	}
}

// ── Audio serving ─────────────────────────────────────────────────────────────

func TestHandleAudio_ServesFile(t *testing.T) {
	audioDir := t.TempDir()
	os.MkdirAll(filepath.Join(audioDir, "Jazz"), 0755)
	os.WriteFile(filepath.Join(audioDir, "Jazz", "track1.mp3"), []byte("MP3_DATA"), 0644)

	h, _ := newTestHarnessWithMusic(t, audioDir)
	w := serve(t, h, "GET", "/audio/Jazz/track1.mp3", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200\nbody: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "MP3_DATA") {
		t.Error("response body should contain audio data")
	}
}

func TestHandleAudio_NonAudioExtension_404(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "GET", "/audio/Jazz/secret.txt", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for non-audio extension, got %d", w.Code)
	}
}

func TestHandleAudio_DotPrefix_404(t *testing.T) {
	h, _ := newTestHarness(t)
	w := serve(t, h, "GET", "/audio/.hidden/track.mp3", "")
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404 for dot-prefix, got %d", w.Code)
	}
}

// ── Music control ─────────────────────────────────────────────────────────────

func TestMusicNextControl_NoCollections(t *testing.T) {
	h, _ := newTestHarness(t) // no audio dir → no collections
	// music-next with no collections should be a no-op (no error)
	w := serve(t, h, "POST", "/api/control", `{"action":"music-next"}`)
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	ws := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(ws.Body.Bytes(), &state)
	if state["music_collection"].(float64) != 0 {
		t.Errorf("want music_collection=0 (unchanged), got %v", state["music_collection"])
	}
}

func TestMusicNextControl_WrapsAround(t *testing.T) {
	audioDir := t.TempDir()
	for _, coll := range []string{"Jazz", "Rock"} {
		os.MkdirAll(filepath.Join(audioDir, coll), 0755)
		os.WriteFile(filepath.Join(audioDir, coll, "a.mp3"), []byte("audio"), 0644)
	}

	h, _ := newTestHarnessWithMusic(t, audioDir)

	// Advance once → collection 1
	serve(t, h, "POST", "/api/control", `{"action":"music-next"}`)
	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if state["music_collection"].(float64) != 1 {
		t.Errorf("after 1 next: want music_collection=1, got %v", state["music_collection"])
	}

	// Advance again → wraps to 0
	serve(t, h, "POST", "/api/control", `{"action":"music-next"}`)
	w2 := serve(t, h, "GET", "/api/state", "")
	json.Unmarshal(w2.Body.Bytes(), &state)
	if state["music_collection"].(float64) != 0 {
		t.Errorf("after 2 next (wrap): want music_collection=0, got %v", state["music_collection"])
	}
}

func TestMusicEnabledInState(t *testing.T) {
	audioDir := t.TempDir()
	os.MkdirAll(filepath.Join(audioDir, "Jazz"), 0755)
	os.WriteFile(filepath.Join(audioDir, "Jazz", "a.mp3"), []byte("audio"), 0644)

	h, _ := newTestHarnessWithMusic(t, audioDir)
	w := serve(t, h, "GET", "/api/state", "")
	var state map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &state)
	if !state["music_enabled"].(bool) {
		t.Error("want music_enabled=true when audio dir has collections")
	}
	colls, _ := state["music_collections"].([]interface{})
	if len(colls) != 1 {
		t.Errorf("want 1 collection in state, got %d", len(colls))
	}
}
