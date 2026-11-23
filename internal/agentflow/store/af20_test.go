package store

import (
	"testing"
	"time"
)

func TestAF20_BackendUsageAndCaps(t *testing.T) {
	s, _ := openTemp(t)
	newProject(t, s, "p")
	ef := newEffort(t, s, "p")
	b := Backend{Name: "metered", Host: "http://127.0.0.1:11434", APIType: "ollama", Slots: 1, Metered: true,
		DailyTokenCap: 1000, MonthlyTokenCap: 5000, Models: []BackendModel{{Model: "m"}}}
	if err := s.CreateBackend(ctx, "tester", b); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	today := now.Add(-time.Hour)
	yesterday := now.AddDate(0, 0, -1)
	for _, x := range []struct {
		at     time.Time
		tokens int64
	}{{today, 400}, {yesterday, 700}} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO stages (effort, role, state, started, finished, tokens, backend, model)
			VALUES (?, 'planner', 'done', ?, ?, ?, 'metered', 'm')`, ef.ID, x.at.UnixMilli(), x.at.UnixMilli(), x.tokens); err != nil {
			t.Fatal(err)
		}
	}
	u, err := s.BackendUsage(ctx, "metered", now)
	if err != nil {
		t.Fatal(err)
	}
	if u.DayTokens != 400 || u.MonthTokens != 1100 {
		t.Fatalf("usage day=%d month=%d, want 400 and 1100", u.DayTokens, u.MonthTokens)
	}
	if at, until, why := u.AtCap(400, 5000); !at || until != u.DayReset || why != "daily" {
		t.Fatalf("daily cap = %v %d %q, want the day reset", at, until, why)
	}
	if at, _, _ := u.AtCap(1000, 5000); at {
		t.Fatal("usage under both caps must not be at cap")
	}
	if at, until, why := u.AtCap(400, 1100); !at || until != u.MonthReset || why != "daily and monthly" {
		t.Fatalf("both caps = %v %d %q, want the month reset", at, until, why)
	}
}

func TestAF20_ReopenKeepsEffortBudget(t *testing.T) {
	s, path := openTemp(t)
	newProject(t, s, "p")
	ef := newEffort(t, s, "p")
	budget := int64(1234)
	if _, err := s.UpdateEffort(ctx, "tester", ef.ID, EffortPatch{BudgetTokens: &budget}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got, err := again.GetEffort(ctx, ef.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.BudgetTokens != budget {
		t.Fatalf("budget after reopen = %d, want %d", got.BudgetTokens, budget)
	}
}
