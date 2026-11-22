package watcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// FakeClock is a test Clock that advances time deterministically. Sleep
// records the requested duration but does not block; the test advances the
// clock's Now by the sum of slept durations.
type FakeClock struct {
	mu    sync.Mutex
	now   time.Time
	slept time.Duration
}

func NewFakeClock() *FakeClock {
	return &FakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *FakeClock) Sleep(d time.Duration) {
	f.mu.Lock()
	f.slept += d
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *FakeClock) TotalSlept() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.slept
}

// fakeResumeServer returns a test server that responds to GET /resume with
// the given actionable flag and recommended action. If count > 0, it only
// returns actionable for the first count requests, then non-actionable.
func fakeResumeServer(t *testing.T, actionable bool, action, taskID string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		payload := map[string]any{
			"actionable": actionable,
			"ackSeq":     100,
			"recommendedNextAction": map[string]any{
				"action": action,
				"taskId": taskID,
				"reason": "test",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
}

func TestCadencePromotes1to2to3toDormant(t *testing.T) {
	srv := fakeResumeServer(t, false, "idle", "")
	defer srv.Close()
	clock := NewFakeClock()
	var wakes []string
	w, err := New(Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Callsign:   "pum-1",
		InstanceID: "agent-1",
		Wake: func(eventID string, _ ResumePayload) error {
			wakes = append(wakes, eventID)
			return nil
		},
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan struct{})
	w.Run(done)
	// After 5 empty polls at each tier, the watcher should go dormant.
	// Total polls: 5 (Tier1) + 5 (Tier2) + 5 (Tier3) = 15 empty polls.
	st := w.State()
	if st.Tier != TierDormant {
		t.Fatalf("expected TierDormant, got %s (emptyCount=%d)", st.Tier, st.EmptyCount)
	}
	if len(wakes) != 0 {
		t.Fatalf("no wakes should fire on empty polls, got %d", len(wakes))
	}
}

func TestActionableResetsToTier1(t *testing.T) {
	// Server returns actionable=true for the first request, then false.
	var mu sync.Mutex
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		actionable := callCount == 1
		mu.Unlock()
		payload := map[string]any{
			"actionable": actionable,
			"ackSeq":     100,
			"recommendedNextAction": map[string]any{
				"action": "resume",
				"taskId": "TASK-1",
				"reason": "test",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()
	clock := NewFakeClock()
	var wakes []string
	w, err := New(Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Callsign:   "pum-1",
		InstanceID: "agent-1",
		Wake: func(eventID string, _ ResumePayload) error {
			wakes = append(wakes, eventID)
			return nil
		},
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan struct{})
	w.Run(done)
	st := w.State()
	if st.Tier != TierDormant {
		t.Fatalf("expected eventual TierDormant, got %s", st.Tier)
	}
	// Exactly 1 wake should have fired (the first actionable poll).
	if len(wakes) != 1 {
		t.Fatalf("expected exactly 1 wake, got %d", len(wakes))
	}
}

func TestDedupeDoesNotWakeTwiceForSameEvent(t *testing.T) {
	// Server always returns actionable=true with the same eventID.
	srv := fakeResumeServer(t, true, "resume", "TASK-1")
	defer srv.Close()
	clock := NewFakeClock()
	var wakes []string
	w, err := New(Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Callsign:   "pum-1",
		InstanceID: "agent-1",
		Wake: func(eventID string, _ ResumePayload) error {
			wakes = append(wakes, eventID)
			return nil
		},
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Run one iteration manually to simulate the first wake.
	actionable, eventID, payload, err := w.poll()
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if !actionable {
		t.Fatalf("expected actionable")
	}
	w.mu.Lock()
	if w.shouldWake(eventID) {
		w.state.ActiveWake = true
		w.state.LastEventID = eventID
		w.mu.Unlock()
		_ = w.cfg.Wake(eventID, payload)
		w.mu.Lock()
		w.state.ActiveWake = false
		w.state.Tier = Tier1Active
		w.state.EmptyCount = 0
	}
	w.mu.Unlock()
	// Second poll with the same eventID should NOT wake.
	_, eventID2, _, err := w.poll()
	if err != nil {
		t.Fatalf("poll2: %v", err)
	}
	w.mu.Lock()
	shouldWake := w.shouldWake(eventID2)
	w.mu.Unlock()
	if shouldWake {
		t.Fatalf("should not wake again for the same eventID %q", eventID2)
	}
	if len(wakes) != 1 {
		t.Fatalf("expected exactly 1 wake, got %d", len(wakes))
	}
}

func TestDifferentEventIDDoesWake(t *testing.T) {
	srv := fakeResumeServer(t, true, "resume", "TASK-1")
	defer srv.Close()
	clock := NewFakeClock()
	var wakes []string
	w, err := New(Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Callsign:   "pum-1",
		InstanceID: "agent-1",
		Wake: func(eventID string, _ ResumePayload) error {
			wakes = append(wakes, eventID)
			return nil
		},
		Clock: clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// First wake for event "resume:TASK-1:100".
	_, eventID1, payload1, _ := w.poll()
	w.mu.Lock()
	if w.shouldWake(eventID1) {
		w.state.ActiveWake = true
		w.state.LastEventID = eventID1
		w.mu.Unlock()
		_ = w.cfg.Wake(eventID1, payload1)
		w.mu.Lock()
		w.state.ActiveWake = false
	}
	w.mu.Unlock()
	// Simulate a different actionable state (different ackSeq → different eventID).
	// We can't easily change the server response, so test shouldWake directly.
	differentEventID := "resume:TASK-2:200"
	w.mu.Lock()
	shouldWake := w.shouldWake(differentEventID)
	w.mu.Unlock()
	if !shouldWake {
		t.Fatalf("should wake for a different eventID %q", differentEventID)
	}
}

func TestResumeMonitoringRestartFromDormant(t *testing.T) {
	srv := fakeResumeServer(t, false, "idle", "")
	defer srv.Close()
	clock := NewFakeClock()
	w, err := New(Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Callsign:   "pum-1",
		InstanceID: "agent-1",
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Force dormant.
	w.mu.Lock()
	w.state.Tier = TierDormant
	w.state.Status = "dormant"
	w.mu.Unlock()
	w.ResumeMonitoring()
	st := w.State()
	if st.Tier != Tier1Active {
		t.Fatalf("expected Tier1Active after resume, got %s", st.Tier)
	}
	if st.Status != "active" {
		t.Fatalf("expected status 'active', got %q", st.Status)
	}
	if st.EmptyCount != 0 {
		t.Fatalf("expected emptyCount=0, got %d", st.EmptyCount)
	}
}

func TestStatePersistsAcrossRestart(t *testing.T) {
	srv := fakeResumeServer(t, false, "idle", "")
	defer srv.Close()
	clock := NewFakeClock()
	statePath := filepath.Join(t.TempDir(), "watcher.json")
	w1, err := New(Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Callsign:   "pum-1",
		InstanceID: "agent-1",
		StatePath:  statePath,
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Manually set state and save.
	w1.mu.Lock()
	w1.state.Tier = Tier2Backoff
	w1.state.EmptyCount = 3
	w1.state.AckSeq = 42
	w1.mu.Unlock()
	w1.saveState()
	// Create a new watcher with the same state path.
	w2, err := New(Config{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		Callsign:   "pum-1",
		InstanceID: "agent-1",
		StatePath:  statePath,
		Clock:      clock,
	})
	if err != nil {
		t.Fatalf("New w2: %v", err)
	}
	st := w2.State()
	if st.Tier != Tier2Backoff {
		t.Fatalf("expected tier to persist as Tier2Backoff, got %s", st.Tier)
	}
	if st.EmptyCount != 3 {
		t.Fatalf("expected emptyCount to persist as 3, got %d", st.EmptyCount)
	}
	if st.AckSeq != 42 {
		t.Fatalf("expected ackSeq to persist as 42, got %d", st.AckSeq)
	}
}

func TestWakePayloadCarriesIdentity(t *testing.T) {
	resume := ResumePayload{
		Actionable:            true,
		AckSeq:                42,
		Cursor:                42,
		RecommendedNextAction: RecommendedNextAction{Action: "resume", TaskID: "TASK-42", Reason: "test"},
	}
	emitted := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	wp := NewWakePayload("resume:TASK-42:42", "pum-2031", "agent-7", resume, emitted)

	if wp.SchemaVersion != WakeSchemaVersion {
		t.Fatalf("expected schemaVersion %q, got %q", WakeSchemaVersion, wp.SchemaVersion)
	}
	if wp.Callsign != "pum-2031" {
		t.Fatalf("expected callsign pum-2031, got %q", wp.Callsign)
	}
	if wp.InstanceID != "agent-7" {
		t.Fatalf("expected instanceId agent-7, got %q", wp.InstanceID)
	}
	if wp.EmittedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("expected RFC3339 emittedAt, got %q", wp.EmittedAt)
	}
	if wp.Cursor != 42 || wp.AckSeq != 42 {
		t.Fatalf("expected ackSeq/cursor 42, got ackSeq=%d cursor=%d", wp.AckSeq, wp.Cursor)
	}
	// The BotChat task id must survive as recommendedNextAction.taskId.
	if wp.RecommendedNextAction.TaskID != "TASK-42" {
		t.Fatalf("expected BotChat taskId TASK-42, got %q", wp.RecommendedNextAction.TaskID)
	}

	// Round-trip through JSON and assert identity fields are present on the
	// wire (the wake command reads this from stdin).
	raw, err := json.Marshal(wp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"schemaVersion", "eventId", "callsign", "instanceId", "actionable", "ackSeq", "cursor", "recommendedNextAction", "emittedAt"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("wake payload JSON missing required field %q", k)
		}
	}
}

func TestWakePayloadDistinctPerCallsign(t *testing.T) {
	resume := ResumePayload{Actionable: true, AckSeq: 1, Cursor: 1}
	emitted := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a, err := json.Marshal(NewWakePayload("e1", "pum-aaaa", "agent-1", resume, emitted))
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	b, err := json.Marshal(NewWakePayload("e1", "pum-bbbb", "agent-2", resume, emitted))
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	if string(a) == string(b) {
		t.Fatalf("different watcher callsigns must yield distinct payloads")
	}
}

func TestWebhookSignAndVerify(t *testing.T) {
	body := []byte(`{"eventId":"evt_1","actionable":true}`)
	secret := "test-secret"
	sig := SignWebhook(body, secret)
	if !VerifyWebhook(body, secret, sig) {
		t.Fatalf("signature should verify")
	}
	// Wrong secret.
	if VerifyWebhook(body, "wrong-secret", sig) {
		t.Fatalf("signature should NOT verify with wrong secret")
	}
	// Tampered body.
	if VerifyWebhook([]byte(`{"eventId":"evt_2"}`), secret, sig) {
		t.Fatalf("signature should NOT verify with tampered body")
	}
	// Bad header format.
	if VerifyWebhook(body, secret, "md5=abc") {
		t.Fatalf("should reject non-sha256 header")
	}
}
