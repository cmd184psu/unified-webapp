package timetracker_test

import (
	"errors"
	"net/http"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/timetracker"
)

func newTempReportStore(t *testing.T) *timetracker.ReportStore {
	t.Helper()
	r, err := timetracker.NewReportStore(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatalf("NewReportStore: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// ── ReportStore ──────────────────────────────────────────────────────────────

func TestReportSaveGetRoundtrip(t *testing.T) {
	r := newTempReportStore(t)
	in := timetracker.Report{
		CustomerName: "Acme",
		Date:         "2026-09-15",
		Body:         "did some work",
		TimeBlocks:   []timetracker.TimeBlock{{Hour: 9, Quarter: 0}, {Hour: 9, Quarter: 1}},
	}
	if err := r.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, exists, err := r.Get("Acme", "2026-09-15")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !exists {
		t.Fatal("exists = false after Save")
	}
	if got.Body != in.Body {
		t.Errorf("Body = %q, want %q", got.Body, in.Body)
	}
	if len(got.TimeBlocks) != 2 || got.TimeBlocks[1] != (timetracker.TimeBlock{Hour: 9, Quarter: 1}) {
		t.Errorf("TimeBlocks = %v, want %v", got.TimeBlocks, in.TimeBlocks)
	}

	// Overwrite (auto-save) replaces, not duplicates.
	in.Body = "revised"
	in.TimeBlocks = nil
	in.TimeBlocks = []timetracker.TimeBlock{{Hour: 10, Quarter: 3}}
	if err := r.Save(in); err != nil {
		t.Fatalf("Save (overwrite): %v", err)
	}
	got, _, err = r.Get("Acme", "2026-09-15")
	if err != nil {
		t.Fatalf("Get (overwrite): %v", err)
	}
	if got.Body != "revised" || len(got.TimeBlocks) != 1 {
		t.Errorf("overwrite failed: %+v", got)
	}
}

func TestReportGetMissing(t *testing.T) {
	r := newTempReportStore(t)
	got, exists, err := r.Get("Acme", "2026-09-15")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if exists {
		t.Error("exists = true for a report never saved")
	}
	if got.CustomerName != "Acme" || got.Date != "2026-09-15" || got.Body != "" || len(got.TimeBlocks) != 0 {
		t.Errorf("missing report should return a blank keyed report, got %+v", got)
	}
}

func TestReportEmptySaveDeletes(t *testing.T) {
	r := newTempReportStore(t)
	rep := timetracker.Report{CustomerName: "Acme", Date: "2026-09-15", Body: "text"}
	if err := r.Save(rep); err != nil {
		t.Fatalf("Save: %v", err)
	}
	rep.Body = ""
	rep.TimeBlocks = nil
	if err := r.Save(rep); err != nil {
		t.Fatalf("Save (empty): %v", err)
	}
	if _, exists, _ := r.Get("Acme", "2026-09-15"); exists {
		t.Error("empty save should delete the stored report")
	}
}

func TestReportValidation(t *testing.T) {
	r := newTempReportStore(t)
	for _, rep := range []timetracker.Report{
		{CustomerName: "", Date: "2026-09-15"},
		{CustomerName: "Acme", Date: "yesterday"},
		{CustomerName: "Acme", Date: ""},
	} {
		if err := r.Save(rep); !errors.Is(err, timetracker.ErrInvalidReport) {
			t.Errorf("Save(%+v): got %v, want ErrInvalidReport", rep, err)
		}
	}
}

func TestReportNeighbors(t *testing.T) {
	r := newTempReportStore(t)
	for _, d := range []string{"2026-09-01", "2026-09-10", "2026-09-20"} {
		if err := r.Save(timetracker.Report{CustomerName: "Acme", Date: d, Body: "x"}); err != nil {
			t.Fatalf("Save(%s): %v", d, err)
		}
	}
	// A different customer's reports must not bleed into navigation.
	if err := r.Save(timetracker.Report{CustomerName: "Other", Date: "2026-09-15", Body: "x"}); err != nil {
		t.Fatalf("Save(other): %v", err)
	}

	cases := []struct{ date, wantPrev, wantNext string }{
		{"2026-09-10", "2026-09-01", "2026-09-20"},
		{"2026-09-01", "", "2026-09-10"},
		{"2026-09-20", "2026-09-10", ""},
		{"2026-09-15", "2026-09-10", "2026-09-20"}, // between stored dates
	}
	for _, tc := range cases {
		prev, next, err := r.Neighbors("Acme", tc.date)
		if err != nil {
			t.Fatalf("Neighbors(%s): %v", tc.date, err)
		}
		if prev != tc.wantPrev || next != tc.wantNext {
			t.Errorf("Neighbors(%s) = (%q, %q), want (%q, %q)", tc.date, prev, next, tc.wantPrev, tc.wantNext)
		}
	}
}

func TestReportRenameCustomer(t *testing.T) {
	r := newTempReportStore(t)
	if err := r.Save(timetracker.Report{CustomerName: "Old", Date: "2026-09-15", Body: "history"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := r.RenameCustomer("Old", "New"); err != nil {
		t.Fatalf("RenameCustomer: %v", err)
	}
	if _, exists, _ := r.Get("Old", "2026-09-15"); exists {
		t.Error("report still stored under the old name")
	}
	got, exists, err := r.Get("New", "2026-09-15")
	if err != nil || !exists || got.Body != "history" {
		t.Errorf("report not migrated: exists=%v err=%v got=%+v", exists, err, got)
	}
}

// ── /report endpoints ────────────────────────────────────────────────────────

type reportResp struct {
	timetracker.Report
	Exists   bool   `json:"exists"`
	PrevDate string `json:"prevDate"`
	NextDate string `json:"nextDate"`
}

func TestReportEndpointRoundtrip(t *testing.T) {
	hh := newHarness(t)

	w := hh.do(t, http.MethodGet, "/report?customer=Acme&date=2026-09-15", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET missing: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeJSON[reportResp](t, w); got.Exists {
		t.Errorf("exists = true before any save: %+v", got)
	}

	save := map[string]any{
		"customerName": "Acme",
		"date":         "2026-09-15",
		"body":         "hello",
		"timeBlocks":   []map[string]int{{"hour": 8, "quarter": 2}},
	}
	w = hh.do(t, http.MethodPost, "/report", save)
	if w.Code != http.StatusOK {
		t.Fatalf("POST: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	if got := decodeJSON[reportResp](t, w); !got.Exists || got.Body != "hello" {
		t.Errorf("save response wrong: %+v", got)
	}

	w = hh.do(t, http.MethodGet, "/report?customer=Acme&date=2026-09-15", nil)
	got := decodeJSON[reportResp](t, w)
	if !got.Exists || got.Body != "hello" || len(got.TimeBlocks) != 1 || got.TimeBlocks[0].Hour != 8 {
		t.Errorf("GET after save: %+v", got)
	}
}

func TestReportEndpointNeighbors(t *testing.T) {
	hh := newHarness(t)
	for _, d := range []string{"2026-09-01", "2026-09-10"} {
		hh.do(t, http.MethodPost, "/report",
			map[string]any{"customerName": "Acme", "date": d, "body": "x"})
	}

	w := hh.do(t, http.MethodGet, "/report?customer=Acme&date=2026-09-10", nil)
	got := decodeJSON[reportResp](t, w)
	if got.PrevDate != "2026-09-01" || got.NextDate != "" {
		t.Errorf("neighbors = (%q, %q), want (2026-09-01, \"\")", got.PrevDate, got.NextDate)
	}
}

func TestReportEndpointRejectsBadKey(t *testing.T) {
	hh := newHarness(t)
	for _, path := range []string{
		"/report?customer=&date=2026-09-15",
		"/report?customer=Acme&date=nope",
		"/report?customer=Acme",
	} {
		if w := hh.do(t, http.MethodGet, path, nil); w.Code != http.StatusBadRequest {
			t.Errorf("GET %s: want 400, got %d", path, w.Code)
		}
	}
	w := hh.do(t, http.MethodPost, "/report",
		map[string]any{"customerName": "", "date": "2026-09-15", "body": "x"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("POST blank customer: want 400, got %d", w.Code)
	}
}

func TestRenameMovesReports(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Acme")
	hh.do(t, http.MethodPost, "/report",
		map[string]any{"customerName": "Acme", "date": "2026-09-15", "body": "history"})

	w := hh.do(t, http.MethodPost, "/update",
		map[string]any{"index": 0, "field": "customerName", "value": "AcmeCorp"})
	if w.Code != http.StatusOK {
		t.Fatalf("rename: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = hh.do(t, http.MethodGet, "/report?customer=AcmeCorp&date=2026-09-15", nil)
	got := decodeJSON[reportResp](t, w)
	if !got.Exists || got.Body != "history" {
		t.Errorf("report did not follow the rename: %+v", got)
	}
	w = hh.do(t, http.MethodGet, "/report?customer=Acme&date=2026-09-15", nil)
	if got := decodeJSON[reportResp](t, w); got.Exists {
		t.Error("report still reachable under the old name")
	}
}
