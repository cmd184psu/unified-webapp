package seat

import (
	"context"
	"errors"
)

// The external seats module (AF-13, the coordinator in agentflow) watches and nudges the seats
// the owner runs by hand with the same rules as a queue seat: these are those rules.

// ErrHalted is returned by Nudge when the screen shows a halt text: nothing was typed.
var ErrHalted = errHalted

// StillView is the screen as silence detection compares it: without the profile's quiet lines.
func StillView(prof Profile, screen string) string { return stillView(prof, screen) }

// Halted returns the profile's halt text the screen shows, or "".
func Halted(prof Profile, screen string) string { return halted(prof, screen) }

// Nudge types the profile's nudge text into session, then Enter, waits, and presses Enter
// again, unless a halt text shows (ErrHalted). It never starts or kills the session.
func (a *Adapter) Nudge(ctx context.Context, session string, prof Profile) error {
	if a.Mux == nil {
		return errors.New("seat: no terminal multiplexer")
	}
	m, err := a.Mux(ctx)
	if err != nil {
		return err
	}
	return a.nudge(ctx, m, session, prof)
}

// Capture returns the session's screen (the last lines lines).
func (a *Adapter) Capture(ctx context.Context, session string, lines int) (string, error) {
	if a.Mux == nil {
		return "", errors.New("seat: no terminal multiplexer")
	}
	m, err := a.Mux(ctx)
	if err != nil {
		return "", err
	}
	return m.Capture(ctx, session, lines)
}

// Profiles returns the configured profiles by name (built-in ones included).
func (a *Adapter) ProfileNamed(ctx context.Context, name string) (Profile, error) {
	return a.profile(ctx, name)
}
