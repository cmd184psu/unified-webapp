// Copyright (c) 2026 cmd184psu
// SPDX-License-Identifier: MIT

package coord

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func openFeeder(t *testing.T) (*store.Store, *SeatFeeder) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, &SeatFeeder{
		Store:    st,
		QueueDir: filepath.Join(dir, "queues"),
		Seats:    []FeedSeat{{Callsign: "fb-beam", Kind: KindFreebuff}},
	}
}

func enableNative(t *testing.T, st *store.Store) {
	t.Helper()
	if err := st.UpdateSettings(context.Background(), "test", map[string]string{"seat_feed_native": "true"}); err != nil {
		t.Fatal(err)
	}
}

func TestRenderSeatQueue(t *testing.T) {
	ctx := context.Background()
	st, f := openFeeder(t)
	text, err := f.RenderSeatQueue(ctx, "fb-beam")
	if err != nil || text != "" {
		t.Fatalf("native off = %q %v", text, err)
	}
	if _, err := os.Stat(f.queuePath("fb-beam")); !os.IsNotExist(err) {
		t.Fatalf("native off wrote a queue file: %v", err)
	}

	enableNative(t, st)
	empty, err := f.RenderSeatQueue(ctx, "fb-beam")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, "NO CURRENT TASK") || strings.Contains(empty, "## CURRENT TASK:") {
		t.Fatalf("empty queue = %s", empty)
	}
	file, err := os.ReadFile(f.queuePath("fb-beam"))
	if err != nil {
		t.Fatal(err)
	}
	if string(file) != empty {
		t.Fatalf("file differs from render:\n%s", file)
	}

	fed, err := st.CreateSeatTask(ctx, store.SeatTask{Title: "current job", Objective: "do the current job", Acceptance: "gates green", Exclusions: "no prod", EligibleSeats: []string{"fb-beam"}, Priority: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkFed(ctx, fed.ID, "fb-beam", 100); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"next one", "next two"} {
		if _, err := st.CreateSeatTask(ctx, store.SeatTask{Title: title, Objective: "later", EligibleSeats: []string{"fb-beam"}, Priority: 1}); err != nil {
			t.Fatal(err)
		}
	}
	// A ready task for another seat must not appear in this queue.
	if _, err := st.CreateSeatTask(ctx, store.SeatTask{Title: "solar only", EligibleSeats: []string{"solar"}}); err != nil {
		t.Fatal(err)
	}
	got, err := f.RenderSeatQueue(ctx, "fb-beam")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "## CURRENT TASK:") || !strings.Contains(got, "current job") || !strings.Contains(got, "### Objective") || !strings.Contains(got, "do the current job") {
		t.Fatalf("missing current block:\n%s", got)
	}
	up := 0
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "- ") {
			up++
		}
	}
	if up != 2 || !strings.Contains(got, "next one") || !strings.Contains(got, "next two") || strings.Contains(got, "solar only") {
		t.Fatalf("up next = %d\n%s", up, got)
	}
	file, err = os.ReadFile(f.queuePath("fb-beam"))
	if err != nil || string(file) != got {
		t.Fatalf("queue file is not the rendered text: %v\n%s", err, file)
	}
}

func TestFeedSeatsEligibility(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	queues := filepath.Join(dir, "queues")
	var posts []string
	f := &SeatFeeder{
		Store:    st,
		QueueDir: queues,
		Seats: []FeedSeat{
			{Callsign: "fb-beam", Kind: KindFreebuff},
			{Callsign: "nemotron", Kind: KindKilo},
			{Callsign: "solar", Kind: KindFreebuff},
		},
		Post: func(_ context.Context, cs, msg string) error {
			posts = append(posts, cs+": "+msg)
			return nil
		},
	}
	research, err := st.CreateSeatTask(ctx, store.SeatTask{Title: "RE-NOTES: read the binary", Priority: 1, EligibleKinds: []string{"headless"}})
	if err != nil {
		t.Fatal(err)
	}
	general, err := st.CreateSeatTask(ctx, store.SeatTask{Title: "FIX-WIDGET: a small code fix", Priority: 0, EligibleKinds: []string{"freebuff"}})
	if err != nil {
		t.Fatal(err)
	}
	solarTask, err := st.CreateSeatTask(ctx, store.SeatTask{Title: "solar waiting", Priority: 9, EligibleSeats: []string{"solar"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(queues, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queues, "solar.md"), []byte("# Queue\n\n## CURRENT TASK: already working\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := f.FeedSeats(ctx); err != nil {
		t.Fatal(err)
	}
	still, err := st.ListSeatTasks(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range still {
		if task.State != "ready" {
			t.Fatalf("native off changed %s to %s", task.Title, task.State)
		}
	}

	enableNative(t, st)
	if err := f.FeedSeats(ctx); err != nil {
		t.Fatal(err)
	}
	got := map[int64]store.SeatTask{}
	listed, err := st.ListSeatTasks(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range listed {
		got[task.ID] = task
	}
	if got[research.ID].State != "fed" || got[research.ID].FedTo != "nemotron" {
		t.Fatalf("research = %+v", got[research.ID])
	}
	if got[general.ID].State != "fed" || got[general.ID].FedTo != "fb-beam" {
		t.Fatalf("general = %+v", got[general.ID])
	}
	if got[solarTask.ID].State != "ready" || got[solarTask.ID].FedTo != "" {
		t.Fatalf("non-empty queue was fed: %+v", got[solarTask.ID])
	}
	if len(posts) != 2 {
		t.Fatalf("posts = %+v", posts)
	}
	for _, p := range posts {
		if !strings.Contains(p, "new task in your queue file") {
			t.Fatalf("pointer = %s", p)
		}
	}
	headQ, err := os.ReadFile(filepath.Join(queues, "nemotron.md"))
	if err != nil || !strings.Contains(string(headQ), "RE-NOTES") || !strings.Contains(string(headQ), "## CURRENT TASK:") {
		t.Fatalf("headless queue = %s %v", headQ, err)
	}

	posts = nil
	if err := f.FeedSeats(ctx); err != nil {
		t.Fatal(err)
	}
	if len(posts) != 0 {
		t.Fatalf("second feed posted %+v", posts)
	}
	again, err := st.GetSeatTask(ctx, research.ID)
	if err != nil || again.FedTo != "nemotron" {
		t.Fatalf("second feed moved research: %+v %v", again, err)
	}
}
