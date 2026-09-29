package slideshow_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/slideshow"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func makeSubject(t *testing.T, root, name string, imageCount int) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for i := range imageCount {
		path := filepath.Join(dir, fmt.Sprintf("img%02d.jpg", i))
		if err := os.WriteFile(path, []byte("img"), 0644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func newConductorFromDir(t *testing.T, imageDir string) *slideshow.Conductor {
	t.Helper()
	store, err := slideshow.NewStore(imageDir, 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return slideshow.NewConductor(
		store,
		slideshow.NewMusicStore(""),
		broker.NewBroker(0),
		config.SlideshowConfig{
			Prefix:          "slides",
			IntervalSeconds: 8,
			DefaultMode:     "kenburns",
			DefaultTheme:    "dark",
		},
	)
}

func stateJSON(t *testing.T, c *slideshow.Conductor) map[string]any {
	t.Helper()
	snap := c.Snapshot()
	var m map[string]any
	if err := json.Unmarshal([]byte(snap), &m); err != nil {
		t.Fatalf("Snapshot not valid JSON: %v\n%s", err, snap)
	}
	return m
}

func intField(t *testing.T, m map[string]any, key string) int {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing field %q in state", key)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("field %q: want float64, got %T (%v)", key, v, v)
	}
	return int(f)
}

func stringField(t *testing.T, m map[string]any, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing field %q in state", key)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("field %q: want string, got %T (%v)", key, v, v)
	}
	return s
}

func boolField(t *testing.T, m map[string]any, key string) bool {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("missing field %q in state", key)
	}
	b, ok := v.(bool)
	if !ok {
		t.Fatalf("field %q: want bool, got %T (%v)", key, v, v)
	}
	return b
}

func control(t *testing.T, c *slideshow.Conductor, action string, value ...any) error {
	t.Helper()
	var raw json.RawMessage
	if len(value) > 0 {
		b, err := json.Marshal(value[0])
		if err != nil {
			t.Fatalf("marshal value for %q: %v", action, err)
		}
		raw = b
	}
	return c.ApplyControl(action, raw)
}

// ── Initial state ─────────────────────────────────────────────────────────────

func TestConductor_InitialState_Defaults(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	s := stateJSON(t, c)

	if stringField(t, s, "mode") != "kenburns" {
		t.Errorf("mode: got %q, want kenburns", s["mode"])
	}
	if stringField(t, s, "theme") != "dark" {
		t.Errorf("theme: got %q, want dark", s["theme"])
	}
	if intField(t, s, "interval_seconds") != 8 {
		t.Errorf("interval_seconds: got %d, want 8", intField(t, s, "interval_seconds"))
	}
	if boolField(t, s, "playing") {
		t.Error("playing: want false (starts paused)")
	}
	if boolField(t, s, "shuffle") {
		t.Error("shuffle: want false")
	}
}

func TestConductor_InitialState_Empty(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	s := stateJSON(t, c)

	if intField(t, s, "total_subjects") != 0 {
		t.Errorf("total_subjects: got %d, want 0", intField(t, s, "total_subjects"))
	}
	if intField(t, s, "total_images") != 0 {
		t.Errorf("total_images: got %d, want 0", intField(t, s, "total_images"))
	}
	if stringField(t, s, "subject") != "" {
		t.Errorf("subject: got %q, want empty", s["subject"])
	}
	if stringField(t, s, "image_path") != "" {
		t.Errorf("image_path: got %q, want empty", s["image_path"])
	}
}

func TestConductor_InitialState_WithSubjects(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 3)
	makeSubject(t, dir, "beta", 2)
	c := newConductorFromDir(t, dir)
	s := stateJSON(t, c)

	if intField(t, s, "total_subjects") != 2 {
		t.Errorf("total_subjects: got %d, want 2", intField(t, s, "total_subjects"))
	}
	if intField(t, s, "total_images") != 3 {
		t.Errorf("total_images (first subject): got %d, want 3", intField(t, s, "total_images"))
	}
	if stringField(t, s, "subject") != "alpha" {
		t.Errorf("subject: got %q, want alpha (sorted first)", s["subject"])
	}
	if intField(t, s, "image_index") != 0 {
		t.Errorf("image_index: got %d, want 0", intField(t, s, "image_index"))
	}
}

// ── Play / pause ──────────────────────────────────────────────────────────────

func TestConductor_StartsNotPlaying(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "a", 2)
	c := newConductorFromDir(t, dir)
	if boolField(t, stateJSON(t, c), "playing") {
		t.Error("conductor should start paused, not playing")
	}
}

func TestConductor_Play_SetsPlaying(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "play"); err != nil {
		t.Fatalf("play: %v", err)
	}
	if !boolField(t, stateJSON(t, c), "playing") {
		t.Error("want playing=true after play")
	}
}

func TestConductor_Pause_ClearsPlaying(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	control(t, c, "play")
	if err := control(t, c, "pause"); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if boolField(t, stateJSON(t, c), "playing") {
		t.Error("want playing=false after pause")
	}
}

func TestConductor_PlayPause_Idempotent(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	// play twice — should stay true
	control(t, c, "play")
	control(t, c, "play")
	if !boolField(t, stateJSON(t, c), "playing") {
		t.Error("double-play: want playing=true")
	}
	// pause twice — should stay false
	control(t, c, "pause")
	control(t, c, "pause")
	if boolField(t, stateJSON(t, c), "playing") {
		t.Error("double-pause: want playing=false")
	}
}

// ── Image navigation ──────────────────────────────────────────────────────────

func TestConductor_Next_AdvancesImage(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "beach", 3)
	c := newConductorFromDir(t, dir)

	before := intField(t, stateJSON(t, c), "image_index")
	if err := control(t, c, "next"); err != nil {
		t.Fatalf("next: %v", err)
	}
	after := intField(t, stateJSON(t, c), "image_index")
	if after != before+1 {
		t.Errorf("image_index: want %d, got %d", before+1, after)
	}
}

func TestConductor_Prev_RetreatsImage(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "beach", 3)
	c := newConductorFromDir(t, dir)

	control(t, c, "next") // move to index 1
	control(t, c, "next") // move to index 2
	if err := control(t, c, "prev"); err != nil {
		t.Fatalf("prev: %v", err)
	}
	if got := intField(t, stateJSON(t, c), "image_index"); got != 1 {
		t.Errorf("image_index after prev: want 1, got %d", got)
	}
}

func TestConductor_Next_WrapsToNextSubject(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 2) // indices 0,1
	makeSubject(t, dir, "beta", 1)
	c := newConductorFromDir(t, dir)

	// alpha has 2 images; advance past the last one
	control(t, c, "next") // alpha[1]
	control(t, c, "next") // → beta[0]

	s := stateJSON(t, c)
	if stringField(t, s, "subject") != "beta" {
		t.Errorf("after wrapping past last image: want subject=beta, got %q", s["subject"])
	}
	if intField(t, s, "image_index") != 0 {
		t.Errorf("after wrapping: want image_index=0, got %d", intField(t, s, "image_index"))
	}
}

func TestConductor_Next_WrapsFromLastSubjectToFirst(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 1)
	makeSubject(t, dir, "beta", 1)
	c := newConductorFromDir(t, dir)

	control(t, c, "next") // alpha[0] → beta[0]
	control(t, c, "next") // beta[0] → alpha[0] (wrap)

	s := stateJSON(t, c)
	if stringField(t, s, "subject") != "alpha" {
		t.Errorf("wrap from last subject: want alpha, got %q", s["subject"])
	}
}

func TestConductor_Prev_WrapsAcrossSubjects(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 2)
	makeSubject(t, dir, "beta", 3) // beta has 3 images (indices 0-2)
	c := newConductorFromDir(t, dir)

	// Start at alpha[0]; prev should go to beta[2]
	if err := control(t, c, "prev"); err != nil {
		t.Fatalf("prev: %v", err)
	}
	s := stateJSON(t, c)
	if stringField(t, s, "subject") != "beta" {
		t.Errorf("prev from first image: want beta, got %q", s["subject"])
	}
	if intField(t, s, "image_index") != 2 {
		t.Errorf("prev from first image: want image_index=2, got %d", intField(t, s, "image_index"))
	}
}

// ── Subject navigation ────────────────────────────────────────────────────────

func TestConductor_NextSubject_JumpsToNextSubject(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 5)
	makeSubject(t, dir, "beta", 2)
	c := newConductorFromDir(t, dir)

	control(t, c, "next") // alpha[1]
	if err := control(t, c, "next-subject"); err != nil {
		t.Fatalf("next-subject: %v", err)
	}
	s := stateJSON(t, c)
	if stringField(t, s, "subject") != "beta" {
		t.Errorf("next-subject: want beta, got %q", s["subject"])
	}
	if intField(t, s, "image_index") != 0 {
		t.Errorf("next-subject resets image_index: want 0, got %d", intField(t, s, "image_index"))
	}
}

func TestConductor_PrevSubject_JumpsToPrevSubject(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 2)
	makeSubject(t, dir, "beta", 2)
	c := newConductorFromDir(t, dir)

	control(t, c, "next-subject") // → beta
	if err := control(t, c, "prev-subject"); err != nil {
		t.Fatalf("prev-subject: %v", err)
	}
	s := stateJSON(t, c)
	if stringField(t, s, "subject") != "alpha" {
		t.Errorf("prev-subject: want alpha, got %q", s["subject"])
	}
}

func TestConductor_NextSubject_WrapsAround(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 1)
	makeSubject(t, dir, "beta", 1)
	c := newConductorFromDir(t, dir)

	control(t, c, "next-subject") // → beta
	control(t, c, "next-subject") // → alpha (wrap)

	if got := stringField(t, stateJSON(t, c), "subject"); got != "alpha" {
		t.Errorf("next-subject wrap: want alpha, got %q", got)
	}
}

// ── Mode ──────────────────────────────────────────────────────────────────────

func TestConductor_SetMode_ValidModes(t *testing.T) {
	for _, mode := range []string{"kenburns", "panscan", "static"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			c := newConductorFromDir(t, dir)
			if err := control(t, c, "set-mode", mode); err != nil {
				t.Fatalf("set-mode %q: %v", mode, err)
			}
			if got := stringField(t, stateJSON(t, c), "mode"); got != mode {
				t.Errorf("mode: want %q, got %q", mode, got)
			}
		})
	}
}

func TestConductor_SetMode_InvalidMode(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	err := control(t, c, "set-mode", "slideshow")
	if err == nil {
		t.Error("want error for unknown mode, got nil")
	}
}

// ── Interval ──────────────────────────────────────────────────────────────────

func TestConductor_SetInterval_UpdatesState(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "set-interval", 20); err != nil {
		t.Fatalf("set-interval: %v", err)
	}
	if got := intField(t, stateJSON(t, c), "interval_seconds"); got != 20 {
		t.Errorf("interval_seconds: want 20, got %d", got)
	}
}

func TestConductor_SetInterval_MinimumOne(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "set-interval", 0); err != nil {
		t.Fatalf("set-interval 0: %v", err)
	}
	if got := intField(t, stateJSON(t, c), "interval_seconds"); got < 1 {
		t.Errorf("interval_seconds: want >= 1, got %d", got)
	}
}

// ── Theme ─────────────────────────────────────────────────────────────────────

func TestConductor_SetTheme(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "set-theme", "light"); err != nil {
		t.Fatalf("set-theme: %v", err)
	}
	if got := stringField(t, stateJSON(t, c), "theme"); got != "light" {
		t.Errorf("theme: want light, got %q", got)
	}
}

// ── Control cards ─────────────────────────────────────────────────────────────

// card returns the named card object from a state snapshot.
func card(t *testing.T, m map[string]any, name string) map[string]any {
	t.Helper()
	v, ok := m[name].(map[string]any)
	if !ok {
		t.Fatalf("missing card %q in state", name)
	}
	return v
}

func TestConductor_CardDefaults(t *testing.T) {
	c := newConductorFromDir(t, t.TempDir())
	s := stateJSON(t, c)
	if p := card(t, s, "visual_card")["position"]; p != "bottom" {
		t.Errorf("visual card position: %v, want bottom", p)
	}
	if p := card(t, s, "audio_card")["position"]; p != "bottom-right" {
		t.Errorf("audio card position: %v, want bottom-right", p)
	}
	if card(t, s, "visual_card")["minimized"] != false {
		t.Error("cards start expanded")
	}
}

func TestConductor_SetCardPosition_AllEightAndClearsDrag(t *testing.T) {
	c := newConductorFromDir(t, t.TempDir())
	for pos := range slideshow.CardPositions {
		if err := control(t, c, "set-card-position", map[string]any{"card": "audio", "position": pos}); err != nil {
			t.Fatalf("position %q: %v", pos, err)
		}
		if got := card(t, stateJSON(t, c), "audio_card")["position"]; got != pos {
			t.Errorf("position: got %v, want %q", got, pos)
		}
	}
	// A drag overrides; picking a square snaps back and clears it.
	control(t, c, "set-card-drag", map[string]any{"card": "audio", "x": 0.3, "y": 0.4})
	if card(t, stateJSON(t, c), "audio_card")["drag"] == nil {
		t.Fatal("drag should be recorded")
	}
	control(t, c, "set-card-position", map[string]any{"card": "audio", "position": "top"})
	if d, ok := card(t, stateJSON(t, c), "audio_card")["drag"]; ok && d != nil {
		t.Errorf("picking a position should clear the drag, got %v", d)
	}
}

func TestConductor_SetCardDrag_ClampsToScreen(t *testing.T) {
	c := newConductorFromDir(t, t.TempDir())
	if err := control(t, c, "set-card-drag", map[string]any{"card": "visual", "x": 1.7, "y": -0.2}); err != nil {
		t.Fatalf("drag: %v", err)
	}
	d := card(t, stateJSON(t, c), "visual_card")["drag"].(map[string]any)
	if d["x"] != 1.0 || d["y"] != 0.0 {
		t.Errorf("drag should clamp to 0..1, got %v", d)
	}
}

func TestConductor_SetCardMinimized(t *testing.T) {
	c := newConductorFromDir(t, t.TempDir())
	if err := control(t, c, "set-card-minimized", map[string]any{"card": "visual", "minimized": true}); err != nil {
		t.Fatalf("minimize: %v", err)
	}
	if card(t, stateJSON(t, c), "visual_card")["minimized"] != true {
		t.Error("visual card should be minimized")
	}
	if card(t, stateJSON(t, c), "audio_card")["minimized"] != false {
		t.Error("minimizing one card must not touch the other")
	}
}

func TestConductor_CardControls_InvalidLeaveStateUnchanged(t *testing.T) {
	c := newConductorFromDir(t, t.TempDir())
	for _, bad := range []struct {
		action string
		value  any
	}{
		{"set-card-position", map[string]any{"card": "visual", "position": "center"}},
		{"set-card-position", map[string]any{"card": "nope", "position": "top"}},
		{"set-card-drag", map[string]any{"card": "visual", "x": 0.5}},
		{"set-card-minimized", map[string]any{"card": "audio"}},
		{"set-card-position", "top"},
	} {
		if err := control(t, c, bad.action, bad.value); err == nil {
			t.Errorf("%s %v: want error", bad.action, bad.value)
		}
	}
	if got := card(t, stateJSON(t, c), "visual_card")["position"]; got != "bottom" {
		t.Errorf("state must not change on error, visual position = %v", got)
	}
}

// ── Shuffle ───────────────────────────────────────────────────────────────────

func TestConductor_SetShuffle_ReflectedInState(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "set-shuffle", true); err != nil {
		t.Fatalf("set-shuffle: %v", err)
	}
	if !boolField(t, stateJSON(t, c), "shuffle") {
		t.Error("shuffle: want true after set-shuffle true")
	}
	control(t, c, "set-shuffle", false)
	if boolField(t, stateJSON(t, c), "shuffle") {
		t.Error("shuffle: want false after set-shuffle false")
	}
}

func TestConductor_SetShuffle_SubjectCountUnchanged(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 2)
	makeSubject(t, dir, "beta", 3)
	makeSubject(t, dir, "gamma", 1)
	c := newConductorFromDir(t, dir)

	control(t, c, "set-shuffle", true)
	s := stateJSON(t, c)
	if intField(t, s, "total_subjects") != 3 {
		t.Errorf("total_subjects after shuffle: want 3, got %d", intField(t, s, "total_subjects"))
	}
}

// ── Unknown action ────────────────────────────────────────────────────────────

func TestConductor_UnknownAction_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "fly-away"); err == nil {
		t.Error("want error for unknown action")
	}
}

// ── Snapshot ──────────────────────────────────────────────────────────────────

func TestConductor_Snapshot_IsValidJSON(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "beach", 2)
	c := newConductorFromDir(t, dir)
	snap := c.Snapshot()
	var m map[string]any
	if err := json.Unmarshal([]byte(snap), &m); err != nil {
		t.Fatalf("Snapshot is not valid JSON: %v\n%s", err, snap)
	}
	for _, key := range []string{"mode", "theme", "playing", "shuffle", "interval_seconds",
		"subject", "image_path", "image_index", "total_images", "total_subjects", "visual_card", "audio_card"} {
		if _, ok := m[key]; !ok {
			t.Errorf("Snapshot missing field %q", key)
		}
	}
}

// ── Music ─────────────────────────────────────────────────────────────────────

func TestConductor_MusicNext_NoOp_WithNoCollections(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir) // no music store
	if err := control(t, c, "music-next"); err != nil {
		t.Fatalf("music-next with no collections: %v", err)
	}
	if got := intField(t, stateJSON(t, c), "music_collection"); got != 0 {
		t.Errorf("music_collection unchanged: want 0, got %d", got)
	}
}

func TestConductor_MusicNext_WrapsAround(t *testing.T) {
	imageDir := t.TempDir()
	audioDir := t.TempDir()
	for _, coll := range []string{"Jazz", "Rock", "Classical"} {
		os.MkdirAll(filepath.Join(audioDir, coll), 0755)
		os.WriteFile(filepath.Join(audioDir, coll, "a.mp3"), []byte("audio"), 0644)
	}

	store, _ := slideshow.NewStore(imageDir, 0)
	music := slideshow.NewMusicStore(audioDir)
	b := broker.NewBroker(0)
	cfg := config.SlideshowConfig{Prefix: "slides", IntervalSeconds: 8, DefaultMode: "kenburns", DefaultTheme: "dark"}
	c := slideshow.NewConductor(store, music, b, cfg)

	for i, want := range []int{1, 2, 0} { // 3 nexts wrap back to 0
		if err := control(t, c, "music-next"); err != nil {
			t.Fatalf("music-next #%d: %v", i+1, err)
		}
		if got := intField(t, stateJSON(t, c), "music_collection"); got != want {
			t.Errorf("music-next #%d: want collection %d, got %d", i+1, want, got)
		}
	}
}

// ── Image age limit ───────────────────────────────────────────────────────────

func ageImage(t *testing.T, root, subject, name string, daysAgo int) {
	t.Helper()
	p := filepath.Join(root, subject, name)
	when := time.Now().AddDate(0, 0, -daysAgo)
	if err := os.Chtimes(p, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestConductor_SetMaxAge_SkipsOldImagesAndZeroRestores(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "fresh", 2)
	makeSubject(t, dir, "stale", 2)
	ageImage(t, dir, "stale", "img00.jpg", 40)
	ageImage(t, dir, "stale", "img01.jpg", 40)
	ageImage(t, dir, "fresh", "img01.jpg", 40)
	c := newConductorFromDir(t, dir)

	if got := intField(t, stateJSON(t, c), "max_age_days"); got != 0 {
		t.Fatalf("default max_age_days = %d, want 0 (config default)", got)
	}
	if n := intField(t, stateJSON(t, c), "total_subjects"); n != 2 {
		t.Fatalf("no limit: %d subjects, want 2", n)
	}

	if err := control(t, c, "set-max-age", 30); err != nil {
		t.Fatalf("set-max-age: %v", err)
	}
	s := stateJSON(t, c)
	if n := intField(t, s, "total_subjects"); n != 1 {
		t.Errorf("30-day limit: %d subjects, want 1 (stale has only old images)", n)
	}
	if stringField(t, s, "subject") != "fresh" || intField(t, s, "total_images") != 1 {
		t.Errorf("30-day limit: want fresh with 1 image, got %v / %v", s["subject"], s["total_images"])
	}

	if err := control(t, c, "set-max-age", 0); err != nil {
		t.Fatalf("set-max-age 0: %v", err)
	}
	if n := intField(t, stateJSON(t, c), "total_subjects"); n != 2 {
		t.Errorf("back to no limit: %d subjects, want 2", n)
	}
}

func TestConductor_SetMaxAge_KeepsCurrentSubjectWhenAllowed(t *testing.T) {
	dir := t.TempDir()
	makeSubject(t, dir, "alpha", 3)
	makeSubject(t, dir, "beta", 3)
	c := newConductorFromDir(t, dir)
	control(t, c, "next") // move to image 2 of the first subject
	before := stateJSON(t, c)

	if err := control(t, c, "set-max-age", 7); err != nil { // everything is new
		t.Fatalf("set-max-age: %v", err)
	}
	after := stateJSON(t, c)
	if stringField(t, after, "subject") != stringField(t, before, "subject") ||
		stringField(t, after, "image_path") != stringField(t, before, "image_path") {
		t.Errorf("an age change that removes nothing should keep the place: before %v/%v, after %v/%v",
			before["subject"], before["image_path"], after["subject"], after["image_path"])
	}
}

func TestConductor_SetMaxAge_RejectsNegative(t *testing.T) {
	c := newConductorFromDir(t, t.TempDir())
	if err := control(t, c, "set-max-age", -1); err == nil {
		t.Error("want an error for a negative age")
	}
	if got := intField(t, stateJSON(t, c), "max_age_days"); got != 0 {
		t.Errorf("state must not change on error, got %d", got)
	}
}
