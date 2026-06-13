package slideshow_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
		slideshow.NewSSEBroker(),
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
	if stringField(t, s, "controls_position") != "bottom" {
		t.Errorf("controls_position: got %q, want bottom", s["controls_position"])
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

// ── Controls position ─────────────────────────────────────────────────────────

func TestConductor_SetControlsPosition_Top(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "set-controls-position", "top"); err != nil {
		t.Fatalf("set-controls-position: %v", err)
	}
	if got := stringField(t, stateJSON(t, c), "controls_position"); got != "top" {
		t.Errorf("controls_position: want top, got %q", got)
	}
}

func TestConductor_SetControlsPosition_Bottom(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	control(t, c, "set-controls-position", "top")
	if err := control(t, c, "set-controls-position", "bottom"); err != nil {
		t.Fatalf("set-controls-position: %v", err)
	}
	if got := stringField(t, stateJSON(t, c), "controls_position"); got != "bottom" {
		t.Errorf("controls_position: want bottom, got %q", got)
	}
}

func TestConductor_SetControlsPosition_InvalidReturnsError(t *testing.T) {
	dir := t.TempDir()
	c := newConductorFromDir(t, dir)
	if err := control(t, c, "set-controls-position", "left"); err == nil {
		t.Error("want error for invalid controls_position, got nil")
	}
	// State must not change on error
	if got := stringField(t, stateJSON(t, c), "controls_position"); got != "bottom" {
		t.Errorf("controls_position unchanged after error: want bottom, got %q", got)
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
		"subject", "image_path", "image_index", "total_images", "total_subjects", "controls_position"} {
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
	broker := slideshow.NewSSEBroker()
	cfg := config.SlideshowConfig{Prefix: "slides", IntervalSeconds: 8, DefaultMode: "kenburns", DefaultTheme: "dark"}
	c := slideshow.NewConductor(store, music, broker, cfg)

	for i, want := range []int{1, 2, 0} { // 3 nexts wrap back to 0
		if err := control(t, c, "music-next"); err != nil {
			t.Fatalf("music-next #%d: %v", i+1, err)
		}
		if got := intField(t, stateJSON(t, c), "music_collection"); got != want {
			t.Errorf("music-next #%d: want collection %d, got %d", i+1, want, got)
		}
	}
}
