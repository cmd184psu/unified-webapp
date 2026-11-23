package watcher

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Clock is the time source for the watcher. The production watcher uses
// RealClock; tests inject a FakeClock to drive cadence promotion without
// real sleeping.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// RealClock is the default Clock implementation.
type RealClock struct{}

func (RealClock) Now() time.Time        { return time.Now() }
func (RealClock) Sleep(d time.Duration) { time.Sleep(d) }

// Tier is the polling cadence tier.
type Tier int

const (
	Tier1Active  Tier = 1 // poll every 1 minute
	Tier2Backoff Tier = 2 // poll every 5 minutes
	Tier3Deep    Tier = 3 // poll every 15 minutes
	TierDormant  Tier = 4 // stopped monitoring
)

func (t Tier) String() string {
	switch t {
	case Tier1Active:
		return "Active 1m"
	case Tier2Backoff:
		return "Backoff 5m"
	case Tier3Deep:
		return "Backoff 15m"
	case TierDormant:
		return "Dormant"
	default:
		return fmt.Sprintf("Tier(%d)", int(t))
	}
}

func (t Tier) Interval() time.Duration {
	switch t {
	case Tier1Active:
		return 1 * time.Minute
	case Tier2Backoff:
		return 5 * time.Minute
	case Tier3Deep:
		return 15 * time.Minute
	default:
		return 0
	}
}

const emptyPollsBeforePromotion = 5

// State is the persistent watcher state, saved to disk so a restart does not
// reset or duplicate the cadence.
type State struct {
	Callsign         string    `json:"callsign"`
	InstanceID       string    `json:"instanceId"`
	AckSeq           int64     `json:"ackSeq"`
	Tier             Tier      `json:"tier"`
	EmptyCount       int       `json:"emptyCount"`
	LastPollAt       time.Time `json:"lastPollAt"`
	LastActionableAt time.Time `json:"lastActionableAt"`
	Status           string    `json:"status"`
	LastEventID      string    `json:"lastEventId"`
	ActiveWake       bool      `json:"activeWake"`
}

// ResumePayload is the subset of GET /resume the watcher checks to decide
// whether to wake the agent.
type ResumePayload struct {
	Actionable            bool                  `json:"actionable"`
	RecommendedNextAction RecommendedNextAction `json:"recommendedNextAction"`
	AckSeq                int64                 `json:"ackSeq"`
	Cursor                int64                 `json:"cursor"`
}

// WakeSchemaVersion is the version of the wake stdin payload contract emitted
// by the reference watcher to its -wake-command/-wake-script. Consumers (e.g.
// the external botchat-helper bridge) should assert on this and reconcile
// field changes across versions. Bump on any breaking change to WakePayload.
const WakeSchemaVersion = "1.0"

// WakePayload is the versioned JSON contract the reference watcher writes to
// the wake command's stdin when actionable work is detected. It carries
// explicit agent identity (Callsign + InstanceID) so the wake target does not
// depend on out-of-band environment variables to know who to resume.
//
// IMPORTANT: RecommendedNextAction.TaskID is a BotChat task id (e.g.
// "TASK-42"), NOT a Zenflow task/chat UUID. The callsign→{zenflowTaskId,
// zenflowChatId} mapping is owned by the external bridge, never by BotChat.
type WakePayload struct {
	SchemaVersion         string                `json:"schemaVersion"`
	EventID               string                `json:"eventId"`
	Callsign              string                `json:"callsign"`
	InstanceID            string                `json:"instanceId"`
	Actionable            bool                  `json:"actionable"`
	AckSeq                int64                 `json:"ackSeq"`
	Cursor                int64                 `json:"cursor"`
	RecommendedNextAction RecommendedNextAction `json:"recommendedNextAction"`
	EmittedAt             string                `json:"emittedAt"`
}

// NewWakePayload builds the versioned wake payload from the watcher's
// configured identity, the dedup eventID, and the resume result. emittedAt is
// injected (rather than read from the clock) so callers control the timestamp
// and tests are deterministic; it is serialized as RFC3339.
func NewWakePayload(eventID, callsign, instanceID string, resume ResumePayload, emittedAt time.Time) WakePayload {
	return WakePayload{
		SchemaVersion:         WakeSchemaVersion,
		EventID:               eventID,
		Callsign:              callsign,
		InstanceID:            instanceID,
		Actionable:            resume.Actionable,
		AckSeq:                resume.AckSeq,
		Cursor:                resume.Cursor,
		RecommendedNextAction: resume.RecommendedNextAction,
		EmittedAt:             emittedAt.UTC().Format(time.RFC3339),
	}
}

// RecommendedNextAction mirrors the server's RecommendedNextAction struct.
type RecommendedNextAction struct {
	Action string `json:"action"`
	TaskID string `json:"taskId,omitempty"`
	PlanID string `json:"planId,omitempty"`
	Reason string `json:"reason"`
}

// WakeFunc is the environment-specific wake invocation. The watcher calls it
// only when actionable work is detected and no wake is already active. The
// reference implementation runs a shell command; a Zenflow integration would
// call the Zenflow wake API. eventID is a dedup key — the watcher will not
// wake again for the same eventID while ActiveWake is true.
type WakeFunc func(eventID string, payload ResumePayload) error

// Config holds the watcher's operational configuration.
type Config struct {
	BaseURL    string
	APIKey     string
	Callsign   string
	InstanceID string
	StatePath  string
	Wake       WakeFunc
	Clock      Clock
	HTTPClient *http.Client
}

// Watcher is the non-model polling loop. It polls GET /resume, checks the
// actionable flag, wakes the agent when actionable, and backs off when idle.
// It is single-flight (one wake at a time per callsign) and dedupes on the
// eventID (constructed from the recommended action + taskID + ackSeq, so a
// retry-safe wake for the same actionable state does not fire twice).
type Watcher struct {
	cfg   Config
	mu    sync.Mutex
	state State
}

// New creates a Watcher from the config, loading persistent state if it exists.
func New(cfg Config) (*Watcher, error) {
	if cfg.Clock == nil {
		cfg.Clock = RealClock{}
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	w := &Watcher{cfg: cfg}
	w.state = State{
		Callsign:   cfg.Callsign,
		InstanceID: cfg.InstanceID,
		Tier:       Tier1Active,
		Status:     "starting",
	}
	if cfg.StatePath != "" {
		if err := w.loadState(); err != nil {
			return nil, fmt.Errorf("watcher: load state: %w", err)
		}
	}
	return w, nil
}

// State returns a snapshot of the current watcher state (for operator UI).
func (w *Watcher) State() State {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.state
}

// Run starts the polling loop. It blocks until ctx is done or the watcher
// goes dormant (the caller can ResumeMonitoring to restart). Each iteration:
//  1. Poll GET /resume.
//  2. If actionable and not already active for this event → wake + reset to
//     Tier1.
//  3. If not actionable → increment emptyCount, promote tier after 5 empty
//     polls, sleep for the tier's interval.
//  4. Persist state.
func (w *Watcher) Run(done <-chan struct{}) {
	for {
		select {
		case <-done:
			w.mu.Lock()
			w.state.Status = "stopped"
			w.mu.Unlock()
			w.saveState()
			return
		default:
		}

		actionable, eventID, payload, err := w.poll()
		if err != nil {
			// HTTP/server errors are NOT empty polls. Retry with the same
			// tier/counter after a short transport backoff.
			w.cfg.Clock.Sleep(10 * time.Second)
			continue
		}

		w.mu.Lock()
		w.state.LastPollAt = w.cfg.Clock.Now()
		if actionable {
			w.state.LastActionableAt = w.state.LastPollAt
			if w.shouldWake(eventID) {
				w.state.ActiveWake = true
				w.state.LastEventID = eventID
				w.state.Status = "waking"
				w.mu.Unlock()
				// Wake outside the lock so a slow wake command doesn't
				// block the operator UI.
				if w.cfg.Wake != nil {
					_ = w.cfg.Wake(eventID, payload)
				}
				w.mu.Lock()
				w.state.ActiveWake = false
				w.state.Status = "active"
				w.state.Tier = Tier1Active
				w.state.EmptyCount = 0
			}
		} else {
			w.state.EmptyCount++
			if w.state.EmptyCount >= emptyPollsBeforePromotion {
				w.promoteTier()
			}
			if w.state.Tier == TierDormant {
				w.state.Status = "dormant"
				w.mu.Unlock()
				w.saveState()
				return
			}
			w.state.Status = w.state.Tier.String()
		}
		w.mu.Unlock()
		w.saveState()

		interval := w.currentIntervalWithJitter()
		if interval > 0 {
			w.cfg.Clock.Sleep(interval)
		}
	}
}

// ResumeMonitoring restarts a dormant watcher from Tier1.
func (w *Watcher) ResumeMonitoring() {
	w.mu.Lock()
	w.state.Tier = Tier1Active
	w.state.EmptyCount = 0
	w.state.Status = "active"
	w.mu.Unlock()
	w.saveState()
}

// shouldWake returns true if a wake should fire: not already active for the
// same eventID. This is the single-flight + dedupe guard.
func (w *Watcher) shouldWake(eventID string) bool {
	if w.state.ActiveWake {
		return false
	}
	if eventID == w.state.LastEventID && eventID != "" {
		return false
	}
	return true
}

// promoteTier advances the cadence: 1→2→3→dormant.
func (w *Watcher) promoteTier() {
	switch w.state.Tier {
	case Tier1Active:
		w.state.Tier = Tier2Backoff
		w.state.EmptyCount = 0
	case Tier2Backoff:
		w.state.Tier = Tier3Deep
		w.state.EmptyCount = 0
	case Tier3Deep:
		w.state.Tier = TierDormant
		w.state.EmptyCount = 0
	}
}

func (w *Watcher) currentIntervalWithJitter() time.Duration {
	w.mu.Lock()
	tier := w.state.Tier
	w.mu.Unlock()
	base := tier.Interval()
	if base <= 0 {
		return 0
	}
	// ±10% jitter to avoid synchronized workers.
	jitter := time.Duration(float64(base) * 0.1)
	return base - jitter/2 + time.Duration(int64(jitter)*int64(w.cfg.Clock.Now().UnixNano()%2))
}

// poll calls GET /resume and returns (actionable, eventID, payload, error).
// eventID is constructed from the recommended action + taskID + ackSeq so
// the same actionable state does not wake twice.
func (w *Watcher) poll() (bool, string, ResumePayload, error) {
	url := fmt.Sprintf("%s/api/v1/resume?callsign=%s&instanceId=%s&limit=50",
		w.cfg.BaseURL, w.cfg.Callsign, w.cfg.InstanceID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return false, "", ResumePayload{}, err
	}
	req.Header.Set("Authorization", "Bearer "+w.cfg.APIKey)
	resp, err := w.cfg.HTTPClient.Do(req)
	if err != nil {
		return false, "", ResumePayload{}, fmt.Errorf("poll: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, "", ResumePayload{}, fmt.Errorf("poll: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var payload ResumePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, "", ResumePayload{}, fmt.Errorf("poll: decode: %w", err)
	}
	eventID := fmt.Sprintf("%s:%s:%d",
		payload.RecommendedNextAction.Action,
		payload.RecommendedNextAction.TaskID,
		payload.AckSeq)
	if payload.Cursor == 0 {
		payload.Cursor = payload.AckSeq
	}
	w.mu.Lock()
	w.state.AckSeq = payload.AckSeq
	w.mu.Unlock()
	return payload.Actionable, eventID, payload, nil
}

func (w *Watcher) loadState() error {
	data, err := os.ReadFile(w.cfg.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	// Only adopt the saved state if it matches this callsign+instance.
	if s.Callsign == w.cfg.Callsign && s.InstanceID == w.cfg.InstanceID {
		w.state = s
	}
	return nil
}

func (w *Watcher) saveState() {
	if w.cfg.StatePath == "" {
		return
	}
	w.mu.Lock()
	s := w.state
	w.mu.Unlock()
	dir := filepath.Dir(w.cfg.StatePath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o700)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(w.cfg.StatePath, data, 0o600)
}

// WebhookEvent is the signed event payload BotChat POSTs to a configured
// webhook URL on actionable state changes. The receiver (watcher adapter)
// verifies the HMAC-SHA256 signature, dedupes on EventID, and wakes the
// agent with the compact payload — the agent then calls GET /resume.
type WebhookEvent struct {
	EventID               string                `json:"eventId"`
	EventType             string                `json:"eventType"` // task.answered, task.assigned, task.done, task.accepted, task.ready
	Callsign              string                `json:"callsign"`  // the agent who should act
	TaskID                string                `json:"taskId,omitempty"`
	Timestamp             int64                 `json:"timestamp"`
	Actionable            bool                  `json:"actionable"`
	RecommendedNextAction RecommendedNextAction `json:"recommendedNextAction"`
}

// SignWebhook computes the HMAC-SHA256 signature for a webhook event body.
// The signature is sent as the X-BotChat-Signature header:
// "sha256=<hex>". The receiver verifies by recomputing the HMAC over the
// raw body.
func SignWebhook(body []byte, secret string) string {
	return fmt.Sprintf("sha256=%s", hmacHex(body, secret))
}

// VerifyWebhook verifies the X-BotChat-Signature header against the body.
func VerifyWebhook(body []byte, secret, sigHeader string) bool {
	const prefix = "sha256="
	if len(sigHeader) <= len(prefix) || sigHeader[:len(prefix)] != prefix {
		return false
	}
	expected := hmacHex(body, secret)
	return subtleEqual(sigHeader[len(prefix):], expected)
}
