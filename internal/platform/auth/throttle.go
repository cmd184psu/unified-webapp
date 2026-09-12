package auth

import (
	"sync"
	"time"
)

// throttleThreshold is the number of consecutive login failures tolerated
// before a delay is imposed on the *next* attempt.
const throttleThreshold = 5

// throttleBaseDelay is the delay imposed on the attempt immediately
// following the throttleThreshold-th consecutive failure; it doubles on
// each further consecutive failure, capped at throttleMaxDelay.
const throttleBaseDelay = 2 * time.Second

// throttleMaxDelay caps the computed delay regardless of how many
// consecutive failures have accumulated.
const throttleMaxDelay = 60 * time.Second

// throttleMaxShift bounds how far the doubling shift is allowed to grow.
// throttleBaseDelay<<throttleMaxShift already exceeds throttleMaxDelay, so
// this exists only to stop the shift count itself from growing without
// bound across a very long run of failures (a shift count large enough to
// exceed the width of a Duration would otherwise wrap the computation back
// down to zero instead of staying capped).
const throttleMaxShift = 6

// throttle is a single in-memory, GLOBAL gate on POST /api/auth/login,
// covering every auth method including the operator PIN (FR-A7 / N-2). It
// is deliberately not per-IP: the server sits behind a 127.0.0.1 proxy and
// X-Forwarded-For is untrusted, so a per-IP throttle would be trivially
// bypassed.
//
// The throttle tracks consecutive failures and reports how long the next
// attempt must be delayed before the login handler processes it; the
// throttle itself never sleeps -- delay() only computes the wait, and it is
// the caller's job to enforce it.
//
// Delay formula: while failures < throttleThreshold, delay() is zero. Once
// failures >= throttleThreshold, delay = throttleBaseDelay << shift where
// shift = min(failures-throttleThreshold, throttleMaxShift), capped at
// throttleMaxDelay. So the 5th consecutive failure itself completes
// un-delayed, but the very next (6th) attempt sees delay()>=2s; the 7th
// sees >=4s; and so on, doubling until it pins at 60s. Any success() resets
// the failure count (and therefore the delay) to zero.
//
// now is an injected clock (matching session.go's jwt.WithTimeFunc
// philosophy) so tests never sleep. All methods are safe for concurrent
// use.
type throttle struct {
	mu        sync.Mutex
	now       func() time.Time
	failures  int
	notBefore time.Time // zero value means "no delay armed"
}

// newThrottle returns a fresh throttle with no recorded failures, using now
// as its clock source.
func newThrottle(now func() time.Time) *throttle {
	return &throttle{now: now}
}

// delay reports how long the caller must wait before this login attempt
// may be processed. It never blocks or sleeps -- it only computes the
// remaining wait, returning zero once notBefore has passed (or was never
// armed).
func (t *throttle) delay() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.notBefore.IsZero() {
		return 0
	}
	d := t.notBefore.Sub(t.now())
	if d < 0 {
		return 0
	}
	return d
}

// fail records a login failure, incrementing the consecutive-failure count
// and, once threshold is reached, arming notBefore per the doubling
// formula documented on throttle.
func (t *throttle) fail() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures++
	if t.failures < throttleThreshold {
		return
	}
	shift := t.failures - throttleThreshold
	if shift > throttleMaxShift {
		shift = throttleMaxShift
	}
	d := throttleBaseDelay << shift
	if d > throttleMaxDelay {
		d = throttleMaxDelay
	}
	t.notBefore = t.now().Add(d)
}

// success resets the throttle: consecutive failures and any armed delay
// are cleared to zero.
func (t *throttle) success() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures = 0
	t.notBefore = time.Time{}
}
