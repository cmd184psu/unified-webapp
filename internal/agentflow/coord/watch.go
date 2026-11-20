package coord

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner/seat"
)

// Screens is what the watcher needs from AF-11's seat runner: the screen, the profile rules
// and the nudge (seat.Adapter implements it).
type Screens interface {
	Capture(ctx context.Context, session string, lines int) (string, error)
	Nudge(ctx context.Context, session string, prof seat.Profile) error
	ProfileNamed(ctx context.Context, name string) (seat.Profile, error)
}

// SeatState is what the watcher knows about one hand-run seat.
type SeatState struct {
	Callsign string    `json:"callsign"`
	Session  string    `json:"session"`
	Running  bool      `json:"running"`
	Since    time.Time `json:"since"` // the screen has looked the same since
	Halt     string    `json:"halt,omitempty"`
	Dead     bool      `json:"dead,omitempty"`
	Ready    bool      `json:"ready"`
	// LastNote is when the seat was last nudged or the owner told; LastAction says which.
	LastNote   time.Time `json:"last_note,omitempty"`
	LastAction string    `json:"last_action,omitempty"`
	hash       string
}

// Watcher replaces seatwatch.sh: every minute it compares each seat's screen (without its
// profile's quiet lines) and, when it has stood still for IdleAfter at the input box, types the
// profile's nudge text (Enter, wait, Enter), never while a halt text shows. A dead session, a
// halt screen or a seat that may not be nudged notifies the owner instead.
type Watcher struct {
	Cfg     *Config
	Screens Screens
	Notify  func(ctx context.Context, text string)
	Now     func() time.Time

	mu    sync.Mutex
	seats map[string]*SeatState
}

const defaultDeadText = `(?i)session.*(ended|expired|dead)|free session|disconnected`

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// Seats is the watcher's view of every configured seat.
func (w *Watcher) Seats() []SeatState {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []SeatState
	for _, s := range w.Cfg.Seats {
		if st, ok := w.seats[s.Callsign]; ok {
			out = append(out, *st)
		} else {
			out = append(out, SeatState{Callsign: s.Callsign, Session: s.Session})
		}
	}
	return out
}

func (w *Watcher) profile(ctx context.Context, s Seat) seat.Profile {
	if s.Profile != "" {
		if p, err := w.Screens.ProfileNamed(ctx, s.Profile); err == nil {
			return p
		}
	}
	return DefaultSeatProfile()
}

// DefaultSeatProfile is seatwatch.sh's rules as a seat profile: the freebuff halt texts plus
// seatwatch's login and take-over screens, the box-drawn lines (ads, the input box) ignored,
// and the owner-approved nudge text.
func DefaultSeatProfile() seat.Profile {
	p := seat.Freebuff()
	p.HaltTexts = append(p.HaltTexts, "take over", "Take over", "press enter to login", "log in to", "Log in to", "sign in", "Sign in")
	p.QuietLines = append(p.QuietLines, `[╭╮╰╯│]`, `^\s*$`)
	p.NudgeText = "read from your queue"
	return p
}

// Pass looks at every seat once. dry decides without typing or notifying.
func (w *Watcher) Pass(ctx context.Context, dry bool) []Decision {
	dry = dry || w.Cfg.DryRun
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seats == nil {
		w.seats = map[string]*SeatState{}
	}
	idle, renotify := w.Cfg.IdleAfter.or(10*time.Minute), w.Cfg.RenotifyAfter.or(30*time.Minute)
	dead := regexp.MustCompile(defaultDeadText)
	if w.Cfg.DeadText != "" {
		if re, err := regexp.Compile(w.Cfg.DeadText); err == nil {
			dead = re
		}
	}
	now := w.now()
	var out []Decision
	for _, s := range w.Cfg.Seats {
		if s.Session == "" {
			continue
		}
		st := w.seats[s.Callsign]
		if st == nil {
			st = &SeatState{Callsign: s.Callsign, Session: s.Session, Since: now}
			w.seats[s.Callsign] = st
		}
		screen, err := w.Screens.Capture(ctx, s.Session, 200)
		st.Running = err == nil
		if err != nil {
			continue // no session: the owner has not started it; nothing to watch
		}
		prof := w.profile(ctx, s)
		body := seat.StillView(prof, screen)
		sum := sha256.Sum256([]byte(body))
		if h := hex.EncodeToString(sum[:8]); h != st.hash {
			st.hash, st.Since, st.LastNote, st.LastAction = h, now, time.Time{}, ""
		}
		st.Halt = seat.Halted(prof, lastLines(screen, 12))
		st.Dead = dead.MatchString(lastLines(body, 15))
		st.Ready = len(s.Ready) == 0 || containsAny(screen, s.Ready)
		reason := ""
		switch {
		case st.Dead:
			reason = "session ended"
		case now.Sub(st.Since) >= idle:
			reason = "idle " + now.Sub(st.Since).Round(time.Minute).String()
		}
		if reason == "" || (!st.LastNote.IsZero() && now.Sub(st.LastNote) < renotify) {
			continue
		}
		busy := strings.Contains(screen, "working...")
		if s.Nudge && !st.Dead && st.Halt == "" && !busy && st.Ready {
			out = append(out, Decision{Kind: "nudge", Seat: s.Callsign, Detail: reason})
			if !dry {
				if err := w.Screens.Nudge(ctx, s.Session, prof); err != nil {
					if errors.Is(err, seat.ErrHalted) {
						st.Halt = "halt text"
					}
					out = append(out, Decision{Kind: "alert", Seat: s.Callsign, Detail: "nudge failed: " + err.Error()})
				}
			}
			st.LastNote, st.LastAction = now, "nudged"
			continue
		}
		why := reason
		if st.Halt != "" {
			why += "; the screen shows " + st.Halt
		}
		out = append(out, Decision{Kind: "notify", Seat: s.Callsign, Detail: why})
		if !dry && w.Notify != nil {
			w.Notify(ctx, s.Callsign+" needs a kick ("+why+")")
		}
		st.LastNote, st.LastAction = now, "notified"
	}
	return out
}

// NudgeNow nudges one seat at once (the API's button), unless a halt text shows.
func (w *Watcher) NudgeNow(ctx context.Context, callsign string) error {
	s, ok := w.Cfg.seat(callsign)
	if !ok || s.Session == "" {
		return ErrUnknownSeat
	}
	if err := w.Screens.Nudge(ctx, s.Session, w.profile(ctx, s)); err != nil {
		return err
	}
	w.mu.Lock()
	if st := w.seats[callsign]; st != nil {
		st.LastNote, st.LastAction = w.now(), "nudged by hand"
	}
	w.mu.Unlock()
	return nil
}

// ErrUnknownSeat: no configured seat has that callsign (or it has no session).
var ErrUnknownSeat = errors.New("coord: no such seat")

func lastLines(s string, n int) string {
	var kept []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, l)
		}
	}
	if len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return strings.Join(kept, "\n")
}

func containsAny(s string, subs []string) bool {
	for _, x := range subs {
		if x != "" && strings.Contains(s, x) {
			return true
		}
	}
	return false
}
