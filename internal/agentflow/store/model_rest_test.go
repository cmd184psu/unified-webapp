package store

import (
	"testing"
	"time"
)

// A model's rest doubles with each failure in a row up to a cap, a streak that ended
// long ago starts over, and an answered stage clears it.
func TestModelRestEscalatesAndClears(t *testing.T) {
	s, _ := openTemp(t)
	now := time.Unix(1_800_000_000, 0)
	base, max := 5*time.Minute, 20*time.Minute
	want := []time.Duration{5 * time.Minute, 10 * time.Minute, 20 * time.Minute, 20 * time.Minute}
	for i, w := range want {
		r, err := s.RestModel(ctx, "b", "m", "rate limited", base, max, now)
		if err != nil {
			t.Fatal(err)
		}
		if got := time.UnixMilli(r.Until).Sub(now); got != w || r.Streak != i+1 {
			t.Fatalf("failure %d: rest %v streak %d, want %v streak %d", i+1, got, r.Streak, w, i+1)
		}
	}
	if rs, err := s.ModelRests(ctx); err != nil || len(rs) != 1 || rs[0].Streak != 4 {
		t.Fatalf("rests = %+v, %v", rs, err)
	}
	// Hours later the old streak no longer counts.
	later := now.Add(3 * time.Hour)
	if r, _ := s.RestModel(ctx, "b", "m", "stalled", base, max, later); r.Streak != 1 || time.UnixMilli(r.Until).Sub(later) != base {
		t.Fatalf("after a long gap: %+v, want a fresh streak", r)
	}
	if ok, err := s.ClearModelRest(ctx, "b", "m"); err != nil || !ok {
		t.Fatalf("clear = %v, %v", ok, err)
	}
	if ok, _ := s.ClearModelRest(ctx, "b", "m"); ok {
		t.Fatal("a second clear found a rest")
	}
}
