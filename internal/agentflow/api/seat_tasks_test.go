package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	if code != 200 {
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

func TestSeatTaskImportDir(t *testing.T) {
	_, srv := newSeatTaskAPI(t)
	dir := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.json", `{"title":"RE-ONE","objective":"find it","acceptanceCriteria":"notes exist","exclusions":"no C","seats":["fb-beam","solar"],"priority":"first","status":"READY"}`)
	write("b.json", `{"title":"FIX-TWO","objective":"fix it","acceptanceCriteria":"gates","exclusions":"no prod","seats":["deepseek4"],"priority":3,"status":"FED TASK-9"}`)
	write("c.json", `{"title":"RE-ONE","objective":"duplicate","seats":["nemotron"],"status":"PROPOSED"}`)
	write("notes.txt", "not json")

	body := `{"dir":` + strconvQuote(dir) + `}`
	code, out := seatCall(t, srv, "POST", "/api/seat-tasks/import", body)
	if code != 200 || !strings.Contains(string(out), `"imported":2`) {
		t.Fatalf("import = %d %s", code, out)
	}
	code, out = seatCall(t, srv, "GET", "/api/seat-tasks", "")
	if code != 200 {
		t.Fatal(string(out))
	}
	var items []store.SeatTask
	if err := json.Unmarshal(out, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("stored = %+v", items)
	}
	byTitle := map[string]store.SeatTask{}
	for _, it := range items {
		byTitle[it.Title] = it
	}
	one := byTitle["RE-ONE"]
	if one.State != "ready" || one.Priority != 1 || one.Acceptance != "notes exist" || one.Objective != "find it" {
		t.Fatalf("RE-ONE = %+v", one)
	}
	if len(one.EligibleSeats) != 2 || one.EligibleSeats[0] != "fb-beam" || one.EligibleSeats[1] != "solar" {
		t.Fatalf("seats = %+v", one.EligibleSeats)
	}
	two := byTitle["FIX-TWO"]
	if two.State != "done" || two.Priority != 3 || len(two.EligibleSeats) != 1 || two.EligibleSeats[0] != "deepseek4" {
		t.Fatalf("FIX-TWO = %+v", two)
	}
	code, out = seatCall(t, srv, "POST", "/api/seat-tasks/import", body)
	if code != 200 || !strings.Contains(string(out), `"imported":0`) {
		t.Fatalf("second import = %d %s", code, out)
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestSeatTaskAPIFiltersAndDelete(t *testing.T) {
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
	first := post(`{"title":"beam work","eligible_seats":["fb-beam"]}`, 201)
	post(`{"title":"solar work","eligible_seats":["solar"]}`, 201)
	post(`{"title":"old work","state":"done"}`, 201)

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
	if len(beam) != 1 || beam[0].ID != first.ID {
		t.Fatalf("fb-beam = %+v", beam)
	}

	code, out = seatCall(t, srv, "GET", "/api/seat-tasks/"+itoa(first.ID), "")
	if code != 200 || !strings.Contains(string(out), `"beam work"`) {
		t.Fatalf("get one = %d %s", code, out)
	}

	code, out = seatCall(t, srv, "DELETE", "/api/seat-tasks/"+itoa(first.ID), "")
	if code != 200 {
		t.Fatalf("delete = %d %s", code, out)
	}
	code, out = seatCall(t, srv, "GET", "/api/seat-tasks/"+itoa(first.ID), "")
	if code != 404 {
		t.Fatalf("get after delete = %d %s", code, out)
	}
	code, out = seatCall(t, srv, "DELETE", "/api/seat-tasks/"+itoa(first.ID), "")
	if code != 404 {
		t.Fatalf("second delete = %d %s", code, out)
	}

	code, out = seatCall(t, srv, "POST", "/api/seat-tasks", `{"title":""}`)
	if code != 400 {
		t.Fatalf("empty title = %d %s", code, out)
	}
	var eb struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(out, &eb); err != nil || eb.Field != "title" {
		t.Fatalf("empty title field = %+v (%v)", eb, err)
	}

	// PATCH state validation.
	code, out = seatCall(t, srv, "PATCH", "/api/seat-tasks/"+itoa(post(`{"title":"patch me"}`, 201).ID), `{"state":"nope"}`)
	if code != 400 {
		t.Fatalf("bad state = %d %s", code, out)
	}
}
