package auth

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a manually-advanced clock for deterministic throttle tests;
// nothing in this file ever calls time.Sleep.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{t: start}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestThrottleFreshHasNoDelay(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	th := newThrottle(clk.now)

	if d := th.delay(); d != 0 {
		t.Errorf("fresh throttle delay = %v, want 0", d)
	}
}

func TestThrottleBelowThresholdNoDelay(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	th := newThrottle(clk.now)

	for i := 0; i < 4; i++ {
		th.fail()
		if d := th.delay(); d != 0 {
			t.Errorf("after %d failure(s), delay = %v, want 0", i+1, d)
		}
	}
}

func TestThrottleFifthFailureArmsDelay(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	th := newThrottle(clk.now)

	for i := 0; i < 5; i++ {
		th.fail()
	}
	if d := th.delay(); d < 2*time.Second {
		t.Errorf("after 5 failures, delay = %v, want >= 2s", d)
	}
}

func TestThrottleDoublingSequenceAndCap(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	th := newThrottle(clk.now)

	// Failures 1-4: below threshold, no delay.
	for i := 0; i < 4; i++ {
		th.fail()
	}

	// Failures 5, 6, 7, 8, 9 map to delays 2s, 4s, 8s, 16s, 32s (doubling
	// from the base 2s per the formula documented on throttle.fail).
	want := []time.Duration{
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
	}
	for i, w := range want {
		th.fail()
		if d := th.delay(); d != w {
			t.Errorf("failure #%d: delay = %v, want exactly %v", i+5, d, w)
		}
	}

	// Failure 10 would compute 64s, which must be capped at exactly 60s.
	th.fail()
	if d := th.delay(); d != 60*time.Second {
		t.Errorf("failure #10: delay = %v, want exactly 60s (capped)", d)
	}

	// Drive many more failures past the cap; it must stay pinned at
	// exactly 60s, not grow (and not wrap back down due to overflow).
	for i := 0; i < 50; i++ {
		th.fail()
	}
	if d := th.delay(); d != 60*time.Second {
		t.Errorf("after many more failures: delay = %v, want exactly 60s (capped)", d)
	}
}

func TestThrottleDelayCountsDownAsClockAdvances(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	th := newThrottle(clk.now)

	for i := 0; i < 5; i++ {
		th.fail()
	}
	if d := th.delay(); d != 2*time.Second {
		t.Fatalf("delay = %v, want exactly 2s", d)
	}

	clk.advance(time.Second)
	if d := th.delay(); d != time.Second {
		t.Errorf("after advancing 1s, delay = %v, want 1s", d)
	}

	clk.advance(2 * time.Second) // now 1s past notBefore
	if d := th.delay(); d != 0 {
		t.Errorf("after the delay has elapsed, delay = %v, want 0", d)
	}
}

func TestThrottleSuccessResetsFailuresAndDelay(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	th := newThrottle(clk.now)

	for i := 0; i < 7; i++ {
		th.fail()
	}
	if d := th.delay(); d == 0 {
		t.Fatalf("delay = 0 after 7 failures, want > 0")
	}

	th.success()
	if d := th.delay(); d != 0 {
		t.Errorf("delay after success() = %v, want 0", d)
	}

	// A single subsequent failure must not immediately re-trigger a delay
	// -- success() must have reset the consecutive-failure count, not just
	// cleared notBefore.
	th.fail()
	if d := th.delay(); d != 0 {
		t.Errorf("delay after 1 failure post-reset = %v, want 0 (count was reset)", d)
	}
}

func TestThrottleConcurrentAccess(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	th := newThrottle(clk.now)

	const goroutines = 50
	const opsPerGoroutine = 200

	var wg sync.WaitGroup
	var successes int64
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				_ = th.delay()
				if (g+i)%7 == 0 {
					th.success()
					atomic.AddInt64(&successes, 1)
				} else {
					th.fail()
				}
			}
		}(g)
	}
	wg.Wait()

	// No assertion on the specific resulting count/delay -- concurrent
	// interleaving makes the exact outcome nondeterministic. The point of
	// this test is that -race stays clean and delay()/fail()/success()
	// never panic or deadlock under concurrent hammering.
	_ = th.delay()
}
