// Copyright (c) 2026 cmd184psu
// SPDX-License-Identifier: MIT

package coord

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// RegistryFile holds the owner's seat registry beside coordinator.py's state.json. A missing
// file starts from DefaultRegistry, so the first open of the Seats page shows every hand-run
// seat the stopgap (pastes.py) knew about, with nudges off where the owner had them off.
const RegistryFile = "seats.json"

// NudgeLogFile records every nudge: seat, time, message and whether it was typed.
const NudgeLogFile = "seat-nudges.jsonl"

// SeatKind is how a seat is driven. No kind is banned from being nudged: the policy's Enabled
// decides that, and only the owner sets it.
type SeatKind string

const (
	KindFreebuff SeatKind = "freebuff"
	KindKilo     SeatKind = "kilo"
	KindOpencode SeatKind = "opencode"
	KindGUI      SeatKind = "gui"
	KindOther    SeatKind = "other"
)

// ValidKind reports whether k is a kind the registry accepts.
func ValidKind(k SeatKind) bool {
	switch k {
	case KindFreebuff, KindKilo, KindOpencode, KindGUI, KindOther:
		return true
	}
	return false
}

// NudgePolicy is the owner's rule for one seat's nudges.
type NudgePolicy struct {
	// Enabled turns nudges on. Off by default for freebuff and gui seats: the owner
	// turns a policy on, no kind is hard-coded off.
	Enabled bool `json:"enabled"`
	// Trigger is when a nudge may go out: "idle" (screen stood still), "queued" (the
	// queue has a task the seat has not started) or "both".
	Trigger string `json:"trigger"`
	// IdleMinutes is how long a screen must stand still for the idle trigger.
	IdleMinutes int `json:"idle_minutes"`
	// MinGapMinutes is the smallest gap between two nudges of this seat.
	MinGapMinutes int `json:"min_gap_minutes"`
	// MaxPerHour caps nudges per rolling hour; 0 means the minimum gap alone limits them.
	MaxPerHour int `json:"max_per_hour"`
	// QuietHours is "22:00-07:00" local time, empty for none.
	QuietHours string `json:"quiet_hours,omitempty"`
	// Templates are the messages a nudge may use; the placeholders are listed in
	// FillTemplate. Empty uses the built-in per-kind template.
	Templates []string `json:"templates,omitempty"`
	// Paused silences nudges without losing Enabled, so the card's pause button can resume.
	Paused bool `json:"paused,omitempty"`
}

// RegistrySeat is one hand-driven seat the owner registered.
type RegistrySeat struct {
	Callsign string   `json:"callsign"`
	Kind     SeatKind `json:"kind"`
	// Where is the tmux session name, or a label for a GUI seat.
	Where string `json:"where"`
	// QueueFile overrides the queue path; empty means <queue_dir>/<callsign>.md.
	QueueFile string      `json:"queue_file,omitempty"`
	Policy    NudgePolicy `json:"policy"`
}

// Registry is the seats file.
type Registry struct {
	Seats []RegistrySeat `json:"seats"`
}

// DefaultRegistry is the stopgap's list (pastes.py SEATS) with seatwatch.sh's NUDGE_SEATS and
// coordinator.py's NO_NUDGE read once as import defaults: the freebuff and gui seats start with
// nudges off, the Kilo seat with them on, because that is what the owner ran by hand.
func DefaultRegistry() *Registry {
	return &Registry{Seats: []RegistrySeat{
		{Callsign: "deepseek4", Kind: KindFreebuff, Where: "deepseek4", Policy: NudgePolicy{Trigger: "idle", IdleMinutes: 60, MinGapMinutes: 60, MaxPerHour: 1}},
		{Callsign: "fb-beam", Kind: KindFreebuff, Where: "freebuff-beam", Policy: NudgePolicy{Trigger: "idle", IdleMinutes: 60, MinGapMinutes: 60, MaxPerHour: 1}},
		{Callsign: "solar", Kind: KindFreebuff, Where: "freebuff3", Policy: NudgePolicy{Trigger: "idle", IdleMinutes: 60, MinGapMinutes: 60, MaxPerHour: 1}},
		{Callsign: "nemotron", Kind: KindKilo, Where: "kilo", Policy: NudgePolicy{Enabled: true, Trigger: "idle", IdleMinutes: 30, MinGapMinutes: 30, MaxPerHour: 2}},
		{Callsign: "opus-gui", Kind: KindGUI, Where: "freemilk.ai GUI", Policy: NudgePolicy{Trigger: "queued", IdleMinutes: 0, MinGapMinutes: 60, MaxPerHour: 1}},
	}}
}

// LoadRegistry reads path. A missing file gives DefaultRegistry; a malformed one is an error,
// so a hand-edited file is reported instead of silently replaced.
func LoadRegistry(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultRegistry(), nil
	}
	if err != nil {
		return nil, err
	}
	var r Registry
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("coord: %s: %w", RegistryFile, err)
	}
	return &r, nil
}

// Save writes the registry atomically.
func (r *Registry) Save(path string) error {
	b, err := json.MarshalIndent(r, "", " ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".seats-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// seat returns the registered seat.
func (r *Registry) seat(callsign string) (RegistrySeat, bool) {
	for _, s := range r.Seats {
		if s.Callsign == callsign {
			return s, true
		}
	}
	return RegistrySeat{}, false
}

// Validate checks a seat before it is stored.
func (s RegistrySeat) Validate() error {
	if strings.TrimSpace(s.Callsign) == "" {
		return errors.New("coord: a seat needs a callsign")
	}
	if strings.ContainsAny(s.Callsign, " /\\\t\n") {
		return fmt.Errorf("coord: callsign %q has whitespace or a slash", s.Callsign)
	}
	if !ValidKind(s.Kind) {
		return fmt.Errorf("coord: seat %s: unknown kind %q", s.Callsign, s.Kind)
	}
	if s.Kind != KindGUI && strings.TrimSpace(s.Where) == "" {
		return fmt.Errorf("coord: seat %s needs a session or a label in where", s.Callsign)
	}
	switch s.Policy.Trigger {
	case "", "idle", "queued", "both":
	default:
		return fmt.Errorf("coord: seat %s: trigger %q is not idle, queued or both", s.Callsign, s.Policy.Trigger)
	}
	if s.Policy.QuietHours != "" {
		if _, _, err := ParseQuietHours(s.Policy.QuietHours); err != nil {
			return fmt.Errorf("coord: seat %s: %w", s.Callsign, err)
		}
	}
	return nil
}

// Upsert adds or replaces a seat.
func (r *Registry) Upsert(s RegistrySeat) error {
	if err := s.Validate(); err != nil {
		return err
	}
	for i := range r.Seats {
		if r.Seats[i].Callsign == s.Callsign {
			r.Seats[i] = s
			return nil
		}
	}
	r.Seats = append(r.Seats, s)
	return nil
}

// Delete removes a seat; an unknown callsign is an error so a stale page cannot delete air.
func (r *Registry) Delete(callsign string) error {
	for i := range r.Seats {
		if r.Seats[i].Callsign == callsign {
			r.Seats = append(r.Seats[:i], r.Seats[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("coord: no seat %q", callsign)
}

// ParseQuietHours reads "22:00-07:00" into minutes since midnight; the window may wrap.
func ParseQuietHours(s string) (start, end int, err error) {
	parts := strings.Split(strings.TrimSpace(s), "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("quiet_hours %q is not HH:MM-HH:MM", s)
	}
	if start, err = parseClock(parts[0]); err != nil {
		return 0, 0, err
	}
	if end, err = parseClock(parts[1]); err != nil {
		return 0, 0, err
	}
	return start, end, nil
}

func parseClock(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, fmt.Errorf("quiet_hours %q is not HH:MM-HH:MM", s)
	}
	return h*60 + m, nil
}

// InQuietHours reports whether t is inside the seat's quiet window.
func (p NudgePolicy) InQuietHours(t time.Time) bool {
	if p.QuietHours == "" {
		return false
	}
	start, end, err := ParseQuietHours(p.QuietHours)
	if err != nil {
		return false
	}
	now := t.Hour()*60 + t.Minute()
	if start == end {
		return false
	}
	if start < end {
		return now >= start && now < end
	}
	return now >= start || now < end
}

// MessageOptions are the paths the templates and the stopgap's rules name.
type MessageOptions struct {
	QueueFile string
	StartHere string
	KeepGoing string
}

// Suggestion is the paste card's content: a one-line status and the exact message.
type Suggestion struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// keepGoing is the sentence pastes.py ends every new-task message with.
const keepGoing = "Then keep taking tasks from your queue by yourself, as ~/work/coordinator/queues/START-HERE.md describes (post DONE on BotChat when each task is finished)."

var (
	taskRe    = regexp.MustCompile(`## CURRENT TASK: (TASK-\d+)\s+(.*)`)
	reworkRe  = regexp.MustCompile(`## REWORK`)
	verdictRe = regexp.MustCompile(`(/[^\x60\s]*review-TASK-\d+\.md)`)
)

// Suggest applies the stopgap's message rules to one seat's queue text, and reports whether
// the queue file existed. It is the single source of the paste card's text, so the API's
// suggested_message and the tests read the same rules.
func (s RegistrySeat) Suggest(queueText string, exists bool, opts MessageOptions) Suggestion {
	if !exists {
		return Suggestion{Status: "no queue file"}
	}
	if s.Kind == KindGUI {
		cur := "(see file)"
		if m := taskRe.FindStringSubmatch(queueText); m != nil {
			cur = strings.TrimSpace(m[1] + " " + m[2])
		}
		note := ""
		if strings.Contains(queueText, "## NOTE") {
			note = " There is a NOTE at the top about what to do first."
		}
		msg := fmt.Sprintf("Read %s again and do the CURRENT TASK in it.%s Write files only; when a job is done, check the queue file for the next one.", opts.QueueFile, note)
		return Suggestion{Status: "current: " + cur, Message: msg}
	}
	if strings.Contains(queueText, "## No task right now") {
		return Suggestion{Status: "queue empty: nothing to send"}
	}
	m := taskRe.FindStringSubmatch(queueText)
	tid, title := "?", ""
	if m != nil {
		tid, title = m[1], strings.TrimSpace(m[2])
	}
	keep := opts.KeepGoing
	if keep == "" {
		keep = keepGoing
	}
	verdict := ""
	if vm := verdictRe.FindStringSubmatch(queueText); vm != nil {
		verdict = vm[1]
	}
	if reworkRe.MatchString(queueText) && verdict != "" {
		msg := fmt.Sprintf("Read your queue file %s (callsign %s) and do the task in it: a REWORK for %s. The verdict is in %s; fix every numbered finding on the same branch, push, then post DONE %s <branch>@<new sha>. %s",
			opts.QueueFile, s.Callsign, tid, verdict, tid, keep)
		return Suggestion{Status: "REWORK " + tid + "  " + title, Message: msg}
	}
	start := opts.StartHere
	if start == "" {
		start = "~/work/coordinator/queues/START-HERE.md"
	}
	msg := fmt.Sprintf("Read %s first if this is a new session, then your queue file %s (callsign %s), and do the task in it (%s). %s",
		start, opts.QueueFile, s.Callsign, tid, keep)
	return Suggestion{Status: tid + "  " + title, Message: msg}
}

// NudgeEntry is one logged nudge.
type NudgeEntry struct {
	Seat    string    `json:"seat"`
	At      time.Time `json:"at"`
	Message string    `json:"message"`
	Typed   bool      `json:"typed"`
}

// AppendNudgeLog appends one JSON line to the log.
func AppendNudgeLog(path string, e NudgeEntry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// ReadNudgeLog reads the log's last limit entries, oldest first. A malformed line is skipped
// so one bad write does not hide the rest.
func ReadNudgeLog(path string, limit int) []NudgeEntry {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []NudgeEntry
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e NudgeEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}
