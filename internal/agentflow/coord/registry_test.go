// Copyright (c) 2026 cmd184psu
// SPDX-License-Identifier: MIT

package coord

import (
	"strings"
	"testing"
	"time"
)

func seatOf(cs string, kind SeatKind) RegistrySeat {
	return RegistrySeat{Callsign: cs, Kind: kind, Where: "tmux-" + cs}
}

func opts() MessageOptions {
	return MessageOptions{QueueFile: "/q/deepseek4.md", StartHere: "/q/START-HERE.md"}
}

func TestSuggestNewTask(t *testing.T) {
	text := "# Queue\n\n## CURRENT TASK: TASK-724  TEXTMAP-1 text view\n\n## Up next\n- TASK-725\n"
	got := seatOf("deepseek4", KindFreebuff).Suggest(text, true, opts())
	if got.Status != "TASK-724  TEXTMAP-1 text view" {
		t.Fatalf("status = %q", got.Status)
	}
	for _, want := range []string{
		"Read /q/START-HERE.md first if this is a new session",
		"your queue file /q/deepseek4.md (callsign deepseek4)",
		"(TASK-724)",
		"Then keep taking tasks from your queue by yourself",
	} {
		if !strings.Contains(got.Message, want) {
			t.Errorf("message is missing %q:\n%s", want, got.Message)
		}
	}
}

func TestSuggestReworkNamesTheVerdict(t *testing.T) {
	text := "# Queue\n\n## REWORK TASK-447\n\nfix every finding; the verdict is in ~/work/coordinator/queues/review-TASK-719.md\n\n## CURRENT TASK: TASK-447 TASK-447 rework\n"
	got := seatOf("deepseek4", KindFreebuff).Suggest(text, true, opts())
	if !strings.HasPrefix(got.Message, "Read your queue file /q/deepseek4.md (callsign deepseek4) and do the task in it: a REWORK for TASK-447.") {
		t.Fatalf("message = %q", got.Message)
	}
	if !strings.Contains(got.Message, "The verdict is in /Users/cdelezenski/../review-TASK-719.md") &&
		!strings.Contains(got.Message, "review-TASK-719.md") {
		t.Errorf("rework message does not name the verdict: %s", got.Message)
	}
	if !strings.Contains(got.Message, "post DONE TASK-447 <branch>@<new sha>") {
		t.Errorf("rework message does not ask for DONE: %s", got.Message)
	}
	if !strings.HasPrefix(got.Status, "REWORK TASK-447") {
		t.Errorf("status = %q", got.Status)
	}
}

func TestSuggestEmptyAndMissing(t *testing.T) {
	if got := seatOf("deepseek4", KindFreebuff).Suggest("## No task right now\n", true, opts()); got.Message != "" || got.Status != "queue empty: nothing to send" {
		t.Errorf("empty queue = %+v", got)
	}
	if got := seatOf("deepseek4", KindFreebuff).Suggest("", false, opts()); got.Message != "" || got.Status != "no queue file" {
		t.Errorf("missing queue = %+v", got)
	}
}

func TestSuggestGUI(t *testing.T) {
	text := "## NOTE read this first\n\n## CURRENT TASK: TASK-654  Seats page\n"
	got := seatOf("opus-gui", KindGUI).Suggest(text, true, opts())
	if got.Status != "current: TASK-654 Seats page" {
		t.Errorf("status = %q", got.Status)
	}
	if !strings.Contains(got.Message, "Read /q/deepseek4.md again and do the CURRENT TASK") ||
		!strings.Contains(got.Message, "There is a NOTE at the top") {
		t.Errorf("gui message = %q", got.Message)
	}
}

func TestRegistryUpsertPauseDelete(t *testing.T) {
	r := &Registry{}
	s := seatOf("muse", KindOpencode)
	if err := r.Upsert(s); err != nil {
		t.Fatal(err)
	}
	s.Policy.Enabled = true
	s.Policy.Paused = true
	if err := r.Upsert(s); err != nil {
		t.Fatal(err)
	}
	if len(r.Seats) != 1 || !r.Seats[0].Policy.Enabled || !r.Seats[0].Policy.Paused {
		t.Fatalf("upsert did not replace: %+v", r.Seats)
	}
	if err := r.Delete("muse"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete("muse"); err == nil {
		t.Fatal("deleting twice must fail")
	}
}

func TestRegistryValidate(t *testing.T) {
	bad := []RegistrySeat{
		{Kind: KindFreebuff, Where: "x"},
		{Callsign: "a b", Kind: KindFreebuff, Where: "x"},
		{Callsign: "a", Kind: "bogus", Where: "x"},
		{Callsign: "a", Kind: KindFreebuff},
		{Callsign: "a", Kind: KindFreebuff, Where: "x", Policy: NudgePolicy{Trigger: "always"}},
		{Callsign: "a", Kind: KindFreebuff, Where: "x", Policy: NudgePolicy{QuietHours: "25:00-07:00"}},
	}
	for _, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%+v passed validation", s)
		}
	}
	good := RegistrySeat{Callsign: "a", Kind: KindGUI, Where: "label", Policy: NudgePolicy{Trigger: "both", QuietHours: "22:00-07:00"}}
	if err := good.Validate(); err != nil {
		t.Errorf("gui label seat rejected: %v", err)
	}
}

func TestInQuietHours(t *testing.T) {
	p := NudgePolicy{QuietHours: "22:00-07:00"}
	at := func(h, m int) time.Time { return time.Date(2026, 11, 22, h, m, 0, 0, time.Local) }
	if !p.InQuietHours(at(23, 0)) || !p.InQuietHours(at(3, 0)) {
		t.Error("overnight window should cover 23:00 and 03:00")
	}
	if p.InQuietHours(at(12, 0)) {
		t.Error("noon is outside the window")
	}
}

func TestDefaultRegistryImportsStopgapPolicies(t *testing.T) {
	r := DefaultRegistry()
	byCall := map[string]RegistrySeat{}
	for _, s := range r.Seats {
		byCall[s.Callsign] = s
	}
	if s := byCall["deepseek4"]; s.Policy.Enabled {
		t.Error("freebuff seats start with nudges off")
	}
	if s := byCall["opus-gui"]; s.Policy.Enabled || s.Kind != KindGUI {
		t.Errorf("gui seat = %+v", s)
	}
	if s := byCall["nemotron"]; !s.Policy.Enabled || s.Kind != KindKilo {
		t.Errorf("kilo seat = %+v", s)
	}
}

func TestNudgeLog(t *testing.T) {
	path := t.TempDir() + "/nudges.jsonl"
	now := time.Now()
	if err := AppendNudgeLog(path, NudgeEntry{Seat: "a", At: now, Message: "m1", Typed: true}); err != nil {
		t.Fatal(err)
	}
	if err := AppendNudgeLog(path, NudgeEntry{Seat: "b", At: now, Message: "m2"}); err != nil {
		t.Fatal(err)
	}
	got := ReadNudgeLog(path, 1)
	if len(got) != 1 || got[0].Seat != "b" {
		t.Fatalf("log = %+v", got)
	}
}
