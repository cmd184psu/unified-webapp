package slideshow

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/broker"
	"cmd184psu/unified-webapp/internal/platform/config"
)

var serverStarted = time.Now().UTC().Format("2006-01-02 15:04 UTC")

// CardPositions are the 8 places a control card can be snapped to: the
// edges and corners of the screen (the center is deliberately not one).
var CardPositions = map[string]bool{
	"top-left": true, "top": true, "top-right": true,
	"left": true, "right": true,
	"bottom-left": true, "bottom": true, "bottom-right": true,
}

// CardPoint is where a dragged card sits, as fractions (0..1) of the screen's
// width and height, so the same layout lands in the same relative place on
// every screen showing the slideshow.
type CardPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// CardLayout is one control card's placement. Drag, when set, overrides
// Position; picking a Position clears it.
type CardLayout struct {
	Position  string     `json:"position"`
	Drag      *CardPoint `json:"drag,omitempty"`
	Minimized bool       `json:"minimized"`
}

// ConductorState is the full state broadcast to every SSE client.
type ConductorState struct {
	Subject         string `json:"subject"`
	ImagePath       string `json:"image_path"`
	ImageIndex      int    `json:"image_index"`
	SubjectIndex    int    `json:"subject_index"`
	TotalImages     int    `json:"total_images"`
	TotalSubjects   int    `json:"total_subjects"`
	Mode            string `json:"mode"`
	Playing         bool   `json:"playing"`
	Shuffle         bool   `json:"shuffle"`
	IntervalSeconds int    `json:"interval_seconds"`
	// MaxAgeDays skips images whose file is older than this many days;
	// 0 means no limit. Starts at the config's age_cutoff_days.
	MaxAgeDays int    `json:"max_age_days"`
	Theme      string `json:"theme"`
	// The two translucent control cards (visual: image/subject controls;
	// audio: music controls). Shared by every screen, like the rest of the state.
	VisualCard    CardLayout `json:"visual_card"`
	AudioCard     CardLayout `json:"audio_card"`
	ServerStarted string     `json:"server_started"` // ISO-8601 UTC; set once at startup
	// Music (phase 2)
	MusicEnabled     bool        `json:"music_enabled"`
	MusicCollection  int         `json:"music_collection"`
	MusicCollections []MusicInfo `json:"music_collections,omitempty"`
}

// Conductor is the server-side playlist manager. It owns the tick clock, the
// current ConductorState, and broadcasts state changes via its broker.
// Call Run() once (from Build) to start the background goroutine.
type Conductor struct {
	mu         sync.Mutex
	state      ConductorState
	subjects   []Subject
	playlist   []int              // subject indices in current play order
	playPos    int                // index into playlist (current subject)
	resetCh    chan time.Duration // send new duration to reset the ticker
	store      *Store
	broker     *broker.Broker
	musicStore *MusicStore

	done     chan struct{} // closed by Stop; ends Run
	stopOnce sync.Once
}

// NewConductor creates a Conductor initialised from cfg and the subjects in store.
// It does not start the background goroutine; call Run() for that.
func NewConductor(store *Store, music *MusicStore, b *broker.Broker, cfg config.SlideshowConfig) *Conductor {
	maxAge := store.DefaultMaxAgeDays()
	subjects, _ := store.SubjectsWithin(maxAge)

	interval := cfg.IntervalSeconds
	if interval <= 0 {
		interval = 8
	}
	mode := cfg.DefaultMode
	if mode == "" {
		mode = "kenburns"
	}
	theme := cfg.DefaultTheme
	if theme == "" {
		theme = "dark"
	}

	colls := music.Collections()

	c := &Conductor{
		subjects:   subjects,
		store:      store,
		resetCh:    make(chan time.Duration, 1),
		done:       make(chan struct{}),
		broker:     b,
		musicStore: music,
		state: ConductorState{
			Mode:             mode,
			Playing:          false,
			Shuffle:          cfg.DefaultShuffle,
			IntervalSeconds:  interval,
			MaxAgeDays:       maxAge,
			Theme:            theme,
			VisualCard:       CardLayout{Position: "bottom"},
			AudioCard:        CardLayout{Position: "bottom-right"},
			ServerStarted:    serverStarted,
			TotalSubjects:    len(subjects),
			MusicEnabled:     len(colls) > 0,
			MusicCollections: colls,
		},
	}
	c.rebuildPlaylist()
	c.syncStateFromPosition()
	return c
}

// Run starts the conductor's background tick goroutine.
// It must be called exactly once; call it from Build(). Run returns after
// Stop is called.
func (c *Conductor) Run() {
	ticker := time.NewTicker(c.duration())
	defer ticker.Stop()
	// Re-scan the image folders periodically, so images age out (and new
	// ones appear) during a long-running slideshow.
	rescan := time.NewTicker(rescanInterval)
	defer rescan.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-rescan.C:
			c.mu.Lock()
			c.refreshSubjectsLocked()
			snap := c.snapshotLocked()
			c.mu.Unlock()
			c.broker.Publish(snap)
		case <-ticker.C:
			c.mu.Lock()
			if c.state.Playing && len(c.subjects) > 0 {
				c.advance()
				snap := c.snapshotLocked()
				c.mu.Unlock()
				c.broker.Publish(snap)
			} else {
				c.mu.Unlock()
			}
		case d := <-c.resetCh:
			ticker.Stop()
			// Drain any tick already queued in the channel; without this a
			// stale tick fires on the very next select and advances the image.
			select {
			case <-ticker.C:
			default:
			}
			ticker = time.NewTicker(d)
		}
	}
}

// Stop ends the goroutine Run started. Idempotent; safe to call whether or
// not Run was ever started.
func (c *Conductor) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
}

// Snapshot returns the current state as a JSON string (thread-safe).
func (c *Conductor) Snapshot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked()
}

// ApplyControl mutates conductor state based on an action string and optional
// JSON-encoded value. It broadcasts the updated state to all SSE clients.
func (c *Conductor) ApplyControl(action string, value json.RawMessage) error {
	c.mu.Lock()

	switch action {
	case "play":
		c.state.Playing = true
		return c.resetTickerAndPublish()
	case "pause":
		c.state.Playing = false
	case "next":
		if len(c.subjects) > 0 {
			c.advance()
		}
		return c.resetTickerAndPublish()
	case "prev":
		if len(c.subjects) > 0 {
			c.retreat()
		}
		return c.resetTickerAndPublish()
	case "next-subject":
		if len(c.subjects) > 0 {
			c.playPos = (c.playPos + 1) % len(c.playlist)
			c.state.ImageIndex = 0
			c.syncStateFromPosition()
		}
		return c.resetTickerAndPublish()
	case "prev-subject":
		if len(c.subjects) > 0 {
			c.playPos = (c.playPos - 1 + len(c.playlist)) % len(c.playlist)
			c.state.ImageIndex = 0
			c.syncStateFromPosition()
		}
		return c.resetTickerAndPublish()
	case "set-mode":
		var v string
		if err := json.Unmarshal(value, &v); err != nil {
			c.mu.Unlock()
			return fmt.Errorf("set-mode: %w", err)
		}
		if v != "kenburns" && v != "panscan" && v != "static" {
			c.mu.Unlock()
			return fmt.Errorf("set-mode: unknown mode %q", v)
		}
		c.state.Mode = v
	case "set-interval":
		var v int
		if err := json.Unmarshal(value, &v); err != nil {
			c.mu.Unlock()
			return fmt.Errorf("set-interval: %w", err)
		}
		if v < 1 {
			v = 1
		}
		c.state.IntervalSeconds = v
		d := time.Duration(v) * time.Second
		c.mu.Unlock()
		select {
		case c.resetCh <- d:
		default:
		}
		c.broker.Publish(c.Snapshot())
		return nil
	case "set-shuffle":
		var v bool
		if err := json.Unmarshal(value, &v); err != nil {
			c.mu.Unlock()
			return fmt.Errorf("set-shuffle: %w", err)
		}
		c.state.Shuffle = v
		c.rebuildPlaylist()
	case "set-theme":
		var v string
		if err := json.Unmarshal(value, &v); err != nil {
			c.mu.Unlock()
			return fmt.Errorf("set-theme: %w", err)
		}
		c.state.Theme = v
	case "set-max-age":
		var v int
		if err := json.Unmarshal(value, &v); err != nil {
			c.mu.Unlock()
			return fmt.Errorf("set-max-age: %w", err)
		}
		if v < 0 || v > maxAgeDaysLimit {
			c.mu.Unlock()
			return fmt.Errorf("set-max-age: must be 0 (no limit) to %d days", maxAgeDaysLimit)
		}
		c.state.MaxAgeDays = v
		c.refreshSubjectsLocked()
		return c.resetTickerAndPublish()
	case "set-card-position", "set-card-drag", "set-card-minimized":
		if err := c.applyCardControlLocked(action, value); err != nil {
			c.mu.Unlock()
			return err
		}
	case "music-next":
		if len(c.state.MusicCollections) > 0 {
			c.state.MusicCollection = (c.state.MusicCollection + 1) % len(c.state.MusicCollections)
		}
	default:
		c.mu.Unlock()
		return fmt.Errorf("unknown action %q", action)
	}

	snap := c.snapshotLocked()
	c.mu.Unlock()
	c.broker.Publish(snap)
	return nil
}

// cardControl is the value of the three card actions. Card names which card;
// the other fields apply to one action each.
type cardControl struct {
	Card      string   `json:"card"`
	Position  string   `json:"position"`
	X         *float64 `json:"x"`
	Y         *float64 `json:"y"`
	Minimized *bool    `json:"minimized"`
}

// applyCardControlLocked handles set-card-position (snap to one of the 8
// positions, clearing any drag), set-card-drag (free placement as screen
// fractions, clamped to 0..1) and set-card-minimized. Called with mu held;
// on error nothing changes.
func (c *Conductor) applyCardControlLocked(action string, value json.RawMessage) error {
	var v cardControl
	if err := json.Unmarshal(value, &v); err != nil {
		return fmt.Errorf("%s: %w", action, err)
	}
	var card *CardLayout
	switch v.Card {
	case "visual":
		card = &c.state.VisualCard
	case "audio":
		card = &c.state.AudioCard
	default:
		return fmt.Errorf("%s: card must be visual or audio", action)
	}
	switch action {
	case "set-card-position":
		if !CardPositions[v.Position] {
			return fmt.Errorf("set-card-position: unknown position %q", v.Position)
		}
		card.Position = v.Position
		card.Drag = nil
	case "set-card-drag":
		if v.X == nil || v.Y == nil {
			return fmt.Errorf("set-card-drag: x and y are required")
		}
		card.Drag = &CardPoint{X: clamp01(*v.X), Y: clamp01(*v.Y)}
	case "set-card-minimized":
		if v.Minimized == nil {
			return fmt.Errorf("set-card-minimized: minimized is required")
		}
		card.Minimized = *v.Minimized
	}
	return nil
}

func clamp01(f float64) float64 {
	switch {
	case f != f, f < 0: // NaN or negative
		return 0
	case f > 1:
		return 1
	}
	return f
}

// rescanInterval is how often Run re-scans the image folders.
const rescanInterval = time.Hour

// maxAgeDaysLimit bounds the image age setting (about 100 years).
const maxAgeDaysLimit = 36500

// MaxAgeDays returns the current image age limit (0 = none).
func (c *Conductor) MaxAgeDays() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state.MaxAgeDays
}

// refreshSubjectsLocked re-scans the image folders under the current age
// limit and rebuilds the playlist, staying on the current subject and image
// when they're still in rotation (otherwise starting that subject, or the
// playlist, from the top). Called with mu held.
func (c *Conductor) refreshSubjectsLocked() {
	subjects, err := c.store.SubjectsWithin(c.state.MaxAgeDays)
	if err != nil {
		return // keep what we have; a transient read error shouldn't blank the show
	}
	curSubject, curImage := c.state.Subject, c.state.ImagePath
	c.subjects = subjects
	c.rebuildPlaylist() // resets to the start of the (possibly reshuffled) playlist
	for pos, idx := range c.playlist {
		if c.subjects[idx].Subject != curSubject {
			continue
		}
		c.playPos = pos
		c.state.ImageIndex = 0
		for i, e := range c.subjects[idx].Entries {
			if e == curImage {
				c.state.ImageIndex = i
				break
			}
		}
		break
	}
	c.syncStateFromPosition()
}

// ── internal helpers (all called with mu held unless noted) ──────────────────

func (c *Conductor) duration() time.Duration {
	c.mu.Lock()
	d := time.Duration(c.state.IntervalSeconds) * time.Second
	c.mu.Unlock()
	return d
}

func (c *Conductor) advance() {
	if len(c.subjects) == 0 || len(c.playlist) == 0 {
		return
	}
	subj := c.subjects[c.playlist[c.playPos]]
	c.state.ImageIndex++
	if c.state.ImageIndex >= len(subj.Entries) {
		c.state.ImageIndex = 0
		c.playPos++
		if c.playPos >= len(c.playlist) {
			c.playPos = 0
			if c.state.Shuffle {
				rand.Shuffle(len(c.playlist), func(i, j int) {
					c.playlist[i], c.playlist[j] = c.playlist[j], c.playlist[i]
				})
			}
		}
	}
	c.syncStateFromPosition()
}

func (c *Conductor) retreat() {
	if len(c.subjects) == 0 || len(c.playlist) == 0 {
		return
	}
	c.state.ImageIndex--
	if c.state.ImageIndex < 0 {
		c.playPos = (c.playPos - 1 + len(c.playlist)) % len(c.playlist)
		subj := c.subjects[c.playlist[c.playPos]]
		c.state.ImageIndex = len(subj.Entries) - 1
	}
	c.syncStateFromPosition()
}

func (c *Conductor) rebuildPlaylist() {
	n := len(c.subjects)
	playlist := make([]int, n)
	for i := range playlist {
		playlist[i] = i
	}
	if c.state.Shuffle {
		rand.Shuffle(n, func(i, j int) { playlist[i], playlist[j] = playlist[j], playlist[i] })
	}
	c.playlist = playlist
	c.playPos = 0
	c.state.ImageIndex = 0
	c.syncStateFromPosition()
}

func (c *Conductor) syncStateFromPosition() {
	c.state.TotalSubjects = len(c.subjects)
	if len(c.subjects) == 0 || len(c.playlist) == 0 {
		c.state.Subject = ""
		c.state.ImagePath = ""
		c.state.TotalImages = 0
		c.state.SubjectIndex = 0
		c.state.ImageIndex = 0
		return
	}
	idx := c.playlist[c.playPos]
	subj := c.subjects[idx]
	c.state.Subject = subj.Subject
	c.state.SubjectIndex = c.playPos
	c.state.TotalImages = len(subj.Entries)
	if c.state.ImageIndex >= len(subj.Entries) {
		c.state.ImageIndex = 0
	}
	c.state.ImagePath = subj.Entries[c.state.ImageIndex]
}

func (c *Conductor) snapshotLocked() string {
	b, _ := json.Marshal(c.state)
	return string(b)
}

// resetTickerAndPublish snapshots state, releases the lock, resets the tick
// interval to a fresh full interval, then broadcasts.  Call while holding mu.
func (c *Conductor) resetTickerAndPublish() error {
	snap := c.snapshotLocked()
	d := time.Duration(c.state.IntervalSeconds) * time.Second
	c.mu.Unlock()
	select {
	case c.resetCh <- d:
	default:
	}
	c.broker.Publish(snap)
	return nil
}
