package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/agentflow/store"
)

func newSeatTaskAPI(t *testing.T) (*store.Store, *httptest.Server) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "agentflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := New(Deps{Store: st})
	mux := http.NewServeMux()
	a.Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return st, srv
}

func seatCall(t *testing.T, srv *httptest.Server, method, path, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, out
}

func TestSeatTaskAPIGaps(t *testing.T) {
	_, srv := newSeatTaskAPI(t)
	post := func(body string, want int) store.SeatTask {
		t.Helper()
		code, out := seatCall(t, srv, "POST", "/api/seat-tasks", body)
		if code != want {
			t.Fatalf("POST %s = %d, want %d: %s", body, code, want, out)
		}
		var got store.SeatTask
		if want == http.StatusCreated {
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
		}
		return got
	}
	readyBeam := post(`{"title":"beam work","objective":"do beam","eligible_seats":["fb-beam"],"priority":1}`, 201)
	post(`{"title":"solar work","objective":"do solar","eligible_seats":["solar"],"priority":0}`, 201)
	post(`{"title":"old work","state":"done","eligible_seats":["deepseek4"],"priority":2}`, 201)
	post(`{"title":"   "}`, 400)
	post(`{"title":""}`, 400)

	code, out := seatCall(t, srv, "GET", "/api/seat-tasks?state=ready", "")
	if code != 200 {
		t.Fatalf("list ready = %d %s", code, out)
	}
	var ready []store.SeatTask
	if err := json.Unmarshal(out, &ready); err != nil {
		t.Fatal(err)
	}
	if len(ready) != 2 {
		t.Fatalf("ready = %+v, want 2", ready)
	}
	code, out = seatCall(t, srv, "GET", "/api/seat-tasks?seat=fb-beam", "")
	if code != 200 {
		t.Fatalf("list seat = %d %s", code, out)
	}
	var beam []store.SeatTask
	if err := json.Unmarshal(out, &beam); err != nil {
		t.Fatal(err)
	}
	if len(beam) != 1 || beam[0].ID != readyBeam.ID || beam[0].Title != "beam work" {
		t.Fatalf("fb-beam = %+v", beam)
	}

	code, out = seatCall(t, srv, "GET", "/api/seat-tasks/"+itoa(readyBeam.ID), "")
	if code != 200 || !strings.Contains(string(out), `"beam work"`) {
		t.Fatalf("get one = %d %s", code, out)
	}
	code, out = seatCall(t, srv, "GET", "/api/seat-tasks/999999", "")
	if code != 404 {
		t.Fatalf("unknown get = %d %s", code, out)
	}

	code, out = seatCall(t, srv, "PATCH", "/api/seat-tasks/"+itoa(readyBeam.ID), `{"objective":"wider","priority":4,"acceptance":"gates green"}`)
	if code != 200 {
		t.Fatalf("patch = %d %s", code, out)
	}
	var patched store.SeatTask
	if err := json.Unmarshal(out, &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Objective != "wider" || patched.Priority != 4 || patched.Acceptance != "gates green" || patched.Title != "beam work" {
		t.Fatalf("patched = %+v", patched)
	}
	code, out = seatCall(t, srv, "PATCH", "/api/seat-tasks/"+itoa(readyBeam.ID), `{"title":""}`)
	if code != 400 {
		t.Fatalf("empty patch title = %d %s", code, out)
	}
	code, out = seatCall(t, srv, "PATCH", "/api/seat-tasks/999999", `{"state":"done"}`)
	if code != 404 {
		t.Fatalf("unknown patch = %d %s", code, out)
	}

	code, out = seatCall(t, srv, "DELETE", "/api/seat-tasks/"+itoa(readyBeam.ID), "")
	if code != 204 {
		t.Fatalf("delete = %d %s", code, out)
	}
	code, out = seatCall(t, srv, "GET", "/api/seat-tasks/"+itoa(readyBeam.ID), "")
	if code != 404 {
		t.Fatalf("get after delete = %d %s", code, out)
	}
	code, out = seatCall(t, srv, "DELETE", "/api/seat-tasks/999999", "")
	if code != 404 {
		t.Fatalf("unknown delete = %d %s", code, out)
	}
}
