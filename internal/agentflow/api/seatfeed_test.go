package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/coord"
	"cmd184psu/unified-webapp/internal/agentflow/store"
)

// The queue the seat reads is the text RenderSeatQueue just wrote.
func TestRenderSeatQueueMatchesGet(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.UpdateSettings(ctx, "test", map[string]string{"seat_feed_native": "true"}); err != nil {
		t.Fatal(err)
	}
	queues := filepath.Join(dir, "queues")
	task, err := st.CreateSeatTask(ctx, store.SeatTask{Title: "shown", Objective: "be shown", EligibleSeats: []string{"fb-beam"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkFed(ctx, task.ID, "fb-beam", 1); err != nil {
		t.Fatal(err)
	}
	f := &coord.SeatFeeder{Store: st, QueueDir: queues, Seats: []coord.FeedSeat{{Callsign: "fb-beam", Kind: coord.KindFreebuff}}}
	text, err := f.RenderSeatQueue(ctx, "fb-beam")
	if err != nil || text == "" {
		t.Fatalf("render = %q %v", text, err)
	}
	a := New(Deps{Store: st, Coord: &coord.Service{Cfg: &coord.Config{QueueDir: queues}}})
	mux := http.NewServeMux()
	a.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	code, out := seatCall(t, srv, "GET", "/api/seats/fb-beam/queue", "")
	if code != 200 {
		t.Fatalf("GET queue = %d %s", code, out)
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	if body.Text != text {
		t.Fatalf("GET text differs from render:\nGET:\n%s\nrender:\n%s", body.Text, text)
	}
}
