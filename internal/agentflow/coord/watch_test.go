package coord

import (
	"context"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/agentflow/runner/seat"
)

type fakeScreens struct {
	screen map[string]string
	nudged []string
}

func (f *fakeScreens) Capture(_ context.Context, s string, _ int) (string, error) {
	if v, ok := f.screen[s]; ok {
		return v, nil
	}
	return "", context.Canceled
}
func (f *fakeScreens) Nudge(_ context.Context, s string, p seat.Profile) error {
	if seat.Halted(p, f.screen[s]) != "" {
		return seat.ErrHalted
	}
	f.nudged = append(f.nudged, s+":"+p.NudgeText)
	return nil
}
func (f *fakeScreens) ProfileNamed(context.Context, string) (seat.Profile, error) {
	return seat.Profile{}, context.Canceled
}

// seatwatch.sh's rules on AF-11's seat runner: an idle seat at its input box is nudged with
// "read from your queue" once per renotify period; a changing spinner box is not activity; a
// seat showing the take-over screen is never typed into, the owner is told instead.
func TestTheWatcherNudgesIdleSeatsAndNeverTypesIntoAHaltScreen(t *testing.T) {
	now := time.Unix(1000, 0)
	cfg := &Config{Seats: []Seat{
		{Callsign: "deepseek4", Session: "deepseek4", Nudge: true, Ready: []string{"Enter a coding task"}},
		{Callsign: "fb-beam", Session: "freebuff-beam", Nudge: true},
	}}
	sc := &fakeScreens{screen: map[string]string{
		"deepseek4":     "done with TASK-1\n╭ ad 1 ╮\nEnter a coding task",
		"freebuff-beam": "This session is in use elsewhere. Take over to move it here",
	}}
	var notes []string
	w := &Watcher{Cfg: cfg, Screens: sc, Now: func() time.Time { return now }, Notify: func(_ context.Context, s string) { notes = append(notes, s) }}
	if d := w.Pass(context.Background(), false); len(d) != 0 {
		t.Fatalf("first look: %v", d)
	}
	now = now.Add(11 * time.Minute)
	sc.screen["deepseek4"] = "done with TASK-1\n╭ ad 2 (rotated) ╮\nEnter a coding task"
	w.Pass(context.Background(), false)
	if strings.Join(sc.nudged, ",") != "deepseek4:read from your queue" {
		t.Fatalf("nudged %v", sc.nudged)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "fb-beam") || !strings.Contains(notes[0], "in use elsewhere") {
		t.Fatalf("notes %v", notes)
	}
	now = now.Add(5 * time.Minute)
	w.Pass(context.Background(), false)
	if len(sc.nudged) != 1 || len(notes) != 1 {
		t.Fatal("at most one nudge or notice per renotify period")
	}
	if err := w.NudgeNow(context.Background(), "fb-beam"); err != seat.ErrHalted {
		t.Fatalf("a nudge by hand into a halt screen: %v", err)
	}
}
