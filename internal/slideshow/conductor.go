package slideshow

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/platform/config"
)

var serverStarted = time.Now().UTC().Format("2006-01-02 15:04 UTC")

// ConductorState is the full state broadcast to every SSE client.
type ConductorState struct {
	Subject          string `json:"subject"`
	ImagePath        string `json:"image_path"`
	ImageIndex       int    `json:"image_index"`
	SubjectIndex     int    `json:"subject_index"`
	TotalImages      int    `json:"total_images"`
	TotalSubjects    int    `json:"total_subjects"`
	Mode             string `json:"mode"`
	Playing          bool   `json:"playing"`
	Shuffle          bool   `json:"shuffle"`
	IntervalSeconds  int    `json:"interval_seconds"`
	Theme            string `json:"theme"`
	ControlsPosition string `json:"controls_position"` // "top" or "bottom"
	ServerStarted    string `json:"server_started"`    // ISO-8601 UTC; set once at startup
	// Music (phase 2)
	MusicEnabled     bool        `json:"music_enabled"`
	MusicCollection  int         `json:"music_collection"`
	MusicCollections []MusicInfo `json:"music_collections,omitempty"`
}

// Conductor is the server-side playlist manager. It owns the tick clock, the
// current ConductorState, and broadcasts state changes via its SSEBroker.
// Call Run() once (from Build) to start the background goroutine.
type Conductor struct {
	mu         sync.Mutex
	state      ConductorState
	subjects   []Subject
	playlist   []int         // subject indices in current play order
	playPos    int           // index into playlist (current subject)
	resetCh    chan time.Duration // send new duration to reset the ticker
	broker     *SSEBroker
	musicStore *MusicStore
}

// NewConductor creates a Conductor initialised from cfg and the subjects in store.
// It does not start the background goroutine; call Run() for that.
func NewConductor(store *Store, music *MusicStore, broker *SSEBroker, cfg config.SlideshowConfig) *Conductor {
	subjects, _ := store.Subjects()

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
		resetCh:    make(chan time.Duration, 1),
		broker:     broker,
		musicStore: music,
		state: ConductorState{
			Mode:             mode,
			Playing:          false,
			Shuffle:          cfg.DefaultShuffle,
			IntervalSeconds:  interval,
			Theme:            theme,
			ControlsPosition: "bottom",
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
// It must be called exactly once; call it from Build().
func (c *Conductor) Run() {
	ticker := time.NewTicker(c.duration())
	defer ticker.Stop()
	for {
		select {
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
	case "set-controls-position":
		var v string
		if err := json.Unmarshal(value, &v); err != nil {
			c.mu.Unlock()
			return fmt.Errorf("set-controls-position: %w", err)
		}
		if v != "top" && v != "bottom" {
			c.mu.Unlock()
			return fmt.Errorf("set-controls-position: must be top or bottom")
		}
		c.state.ControlsPosition = v
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
