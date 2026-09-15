package timetracker_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/config"
	"cmd184psu/unified-webapp/internal/timetracker"
)

// ── test harness ─────────────────────────────────────────────────────────────

type harness struct {
	s        *timetracker.Store
	dataFile string
	mux      *http.ServeMux
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	dataFile := filepath.Join(dir, "timetracker.json")
	s, err := timetracker.New(dataFile)
	if err != nil {
		t.Fatalf("timetracker.New: %v", err)
	}
	h := timetracker.NewHandler(s)
	mux := http.NewServeMux()
	h.Register(mux)
	return &harness{s: s, dataFile: dataFile, mux: mux}
}

func (hh *harness) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	hh.mux.ServeHTTP(w, req)
	return w
}

// doRaw sends a request with a raw string body and no Content-Type header set
// (used for malformed-JSON tests where encoding a Go value would not produce
// invalid JSON).
func (hh *harness) doRaw(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	hh.mux.ServeHTTP(w, req)
	return w
}

// doMultipart posts a multipart/form-data request. If fieldName is empty, no
// file part is added at all (used for TestImportCSVMissingFileField).
func (hh *harness) doMultipart(t *testing.T, path, fieldName, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if fieldName != "" {
		part, err := mw.CreateFormFile(fieldName, filename)
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	hh.mux.ServeHTTP(w, req)
	return w
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, w.Body.String())
	}
	return out
}

func decodeErr(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	env := decodeJSON[map[string]string](t, w)
	msg, ok := env["error"]
	if !ok {
		t.Fatalf("response missing \"error\" key: %s", w.Body.String())
	}
	return msg
}

func names(customers []timetracker.Customer) []string {
	out := make([]string, len(customers))
	for i, c := range customers {
		out[i] = c.CustomerName
	}
	return out
}

func seedCustomers(t *testing.T, hh *harness, names ...string) {
	t.Helper()
	for _, n := range names {
		if _, err := hh.s.AppendCustomer(timetracker.Customer{CustomerName: n}); err != nil {
			t.Fatalf("seed AppendCustomer(%q): %v", n, err)
		}
	}
}

// ── GET /data ────────────────────────────────────────────────────────────────

func TestGetData(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "zeta", "Alpha", "beta")

	w := hh.do(t, http.MethodGet, "/data", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	raw := decodeJSON[map[string]json.RawMessage](t, w)
	for _, key := range []string{"companyName", "projectName", "author", "version", "customers"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}

	w2 := hh.do(t, http.MethodGet, "/data", nil)
	got := decodeJSON[timetracker.Data](t, w2)
	want := []string{"Alpha", "beta", "zeta"}
	if gotNames := names(got.Customers); strings.Join(gotNames, ",") != strings.Join(want, ",") {
		t.Errorf("customers not sorted case-insensitively: got %v, want %v", gotNames, want)
	}
}

// ── POST /update ─────────────────────────────────────────────────────────────

func TestUpdateAuthor(t *testing.T) {
	hh := newHarness(t)
	w := hh.do(t, http.MethodPost, "/update",
		map[string]any{"index": -1, "field": "anything", "value": "Chris"})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := decodeJSON[timetracker.Data](t, w)
	if got.Author != "Chris" {
		t.Errorf("author = %q, want %q", got.Author, "Chris")
	}
}

func TestUpdateEachField(t *testing.T) {
	cases := []struct {
		field string
		get   func(timetracker.Customer) string
	}{
		{"customerName", func(c timetracker.Customer) string { return c.CustomerName }},
		{"slackChannel", func(c timetracker.Customer) string { return c.SlackChannel }},
		{"insightUrl", func(c timetracker.Customer) string { return c.InsightUrl }},
		{"workLoadType", func(c timetracker.Customer) string { return c.WorkLoadType }},
		{"sfdcUrl", func(c timetracker.Customer) string { return c.SfdcUrl }},
		{"cumulusBucket", func(c timetracker.Customer) string { return c.CumulusBucket }},
		{"jira", func(c timetracker.Customer) string { return c.Jira }},
	}
	for _, c := range cases {
		t.Run(c.field, func(t *testing.T) {
			hh := newHarness(t)
			seedCustomers(t, hh, "Acme")

			w := hh.do(t, http.MethodPost, "/update",
				map[string]any{"index": 0, "field": c.field, "value": "updated-value"})
			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
			}
			got := decodeJSON[timetracker.Data](t, w)
			if len(got.Customers) != 1 {
				t.Fatalf("response is not the full Data (customers len %d, want 1): %+v", len(got.Customers), got)
			}
			if v := c.get(got.Customers[0]); v != "updated-value" {
				t.Errorf("field %s = %q, want %q", c.field, v, "updated-value")
			}
		})
	}
}

func TestUpdateNewCustomer(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Zeta")

	payload := map[string]any{
		"index": 1,
		"field": "newCustomer",
		"value": map[string]string{
			"customerName":   "Alpha",
			"slackChannel":   "#alpha",
			"slackChannelId": "C123",
			"insightUrl":     "http://insight",
			"workLoadType":   "type",
			"sfdcUrl":        "http://sfdc",
			"cumulusBucket":  "bucket",
			"jira":           "JIRA-1",
		},
	}
	w := hh.do(t, http.MethodPost, "/update", payload)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := decodeJSON[timetracker.Data](t, w)
	if len(got.Customers) != 2 {
		t.Fatalf("want 2 customers after append, got %d", len(got.Customers))
	}
	// Mutation responses use insertion order (not Snapshot's sort), so the
	// newly appended "Alpha" must land after "Zeta", not before it.
	if gotNames := names(got.Customers); strings.Join(gotNames, ",") != "Zeta,Alpha" {
		t.Errorf("customers = %v, want insertion order [Zeta Alpha]", gotNames)
	}
	if got.Customers[1].SlackChannelId != "C123" {
		t.Errorf("appended customer missing fields: %+v", got.Customers[1])
	}
}

func TestUpdateUnknownField(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Acme")

	w := hh.do(t, http.MethodPost, "/update",
		map[string]any{"index": 0, "field": "bogus", "value": "x"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if decodeErr(t, w) == "" {
		t.Error("expected non-empty error message")
	}
}

func TestUpdateIndexOutOfRange(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Acme")

	for _, idx := range []int{99, -5} {
		w := hh.do(t, http.MethodPost, "/update",
			map[string]any{"index": idx, "field": "customerName", "value": "x"})
		if w.Code != http.StatusBadRequest {
			t.Errorf("index %d: want 400, got %d", idx, w.Code)
		}
	}

	w := hh.do(t, http.MethodGet, "/data", nil)
	got := decodeJSON[timetracker.Data](t, w)
	if len(got.Customers) != 1 || got.Customers[0].CustomerName != "Acme" {
		t.Errorf("data mutated by out-of-range update: %+v", got.Customers)
	}
}

func TestUpdateMalformedBody(t *testing.T) {
	hh := newHarness(t)
	w := hh.doRaw(t, http.MethodPost, "/update", "{not valid json")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
	if decodeErr(t, w) == "" {
		t.Error("expected non-empty decode-error message")
	}
}

// ── POST /delete ─────────────────────────────────────────────────────────────

func TestDelete(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Acme", "Beta")

	w := hh.do(t, http.MethodPost, "/delete", map[string]any{"index": 0})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := decodeJSON[timetracker.Data](t, w)
	if len(got.Customers) != 1 || got.Customers[0].CustomerName != "Beta" {
		t.Errorf("unexpected customers after delete: %+v", got.Customers)
	}
}

func TestDeleteInvalidIndex(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Acme")

	for _, idx := range []int{-1, 1} {
		w := hh.do(t, http.MethodPost, "/delete", map[string]any{"index": idx})
		if w.Code != http.StatusBadRequest {
			t.Errorf("index %d: want 400, got %d", idx, w.Code)
		}
	}
}

// ── POST /create-customer ────────────────────────────────────────────────────

func TestCreateCustomer(t *testing.T) {
	hh := newHarness(t)
	w := hh.do(t, http.MethodPost, "/create-customer", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := decodeJSON[timetracker.Data](t, w)
	if len(got.Customers) != 1 || got.Customers[0].CustomerName != "New Customer" {
		t.Errorf("unexpected customers after create-customer: %+v", got.Customers)
	}
}

// ── GET /export-csv ──────────────────────────────────────────────────────────

const csvHeader = "CustomerName,SlackChannel,SlackChannelId,InsightUrl,WorkLoadType,SfdcUrl,CumulusBucket,Jira"

func TestExportCSV(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "zeta", "Alpha")

	w := hh.do(t, http.MethodGet, "/export-csv", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/csv" {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != "attachment; filename=customers.csv" {
		t.Errorf("Content-Disposition = %q, want %q", cd, "attachment; filename=customers.csv")
	}

	lines := strings.Split(strings.TrimRight(w.Body.String(), "\r\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != csvHeader {
		t.Fatalf("header row = %q, want %q", lines[0], csvHeader)
	}
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %d lines: %v", len(lines), lines)
	}
	if got := strings.TrimRight(lines[1], "\r"); !strings.HasPrefix(got, "Alpha,") {
		t.Errorf("row 1 = %q, want it to start with sorted-first customer Alpha", got)
	}
	if got := strings.TrimRight(lines[2], "\r"); !strings.HasPrefix(got, "zeta,") {
		t.Errorf("row 2 = %q, want it to start with sorted-second customer zeta", got)
	}
}

// ── POST /import-csv ─────────────────────────────────────────────────────────

func TestImportCSV8Columns(t *testing.T) {
	hh := newHarness(t)
	body := csvHeader + "\n" +
		"Acme,#acme,C1,http://insight,typeA,http://sfdc,bucketA,JIRA-1\n" +
		"Globex,#globex,C2,http://insight2,typeB,http://sfdc2,bucketB,JIRA-2\n"

	w := hh.doMultipart(t, "/import-csv", "file", "customers.csv", []byte(body))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := decodeJSON[timetracker.Data](t, w)
	if len(got.Customers) != 2 {
		t.Fatalf("want 2 customers, got %d", len(got.Customers))
	}
	if got.Customers[0].CustomerName != "Acme" || got.Customers[0].Jira != "JIRA-1" {
		t.Errorf("row 0 mapped incorrectly: %+v", got.Customers[0])
	}
	if got.Customers[1].CustomerName != "Globex" || got.Customers[1].CumulusBucket != "bucketB" {
		t.Errorf("row 1 mapped incorrectly: %+v", got.Customers[1])
	}
}

func TestImportCSV9ColumnLegacy(t *testing.T) {
	hh := newHarness(t)
	legacyHeader := "CustomerName,SlackChannel,SlackChannelId,InsightUrl,WorkLoadType,SfdcUrl,SupportTunnel,CumulusBucket,Jira"
	body := legacyHeader + "\n" +
		"Acme,#acme,C1,http://insight,typeA,http://sfdc,tunnel-value,bucketA,JIRA-1\n"

	w := hh.doMultipart(t, "/import-csv", "file", "legacy.csv", []byte(body))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := decodeJSON[timetracker.Data](t, w)
	if len(got.Customers) != 1 {
		t.Fatalf("want 1 customer, got %d", len(got.Customers))
	}
	c := got.Customers[0]
	if c.CustomerName != "Acme" || c.SlackChannel != "#acme" || c.SlackChannelId != "C1" ||
		c.InsightUrl != "http://insight" || c.WorkLoadType != "typeA" || c.SfdcUrl != "http://sfdc" ||
		c.CumulusBucket != "bucketA" || c.Jira != "JIRA-1" {
		t.Errorf("legacy 9-column row mapped incorrectly (column 6 should be dropped): %+v", c)
	}
}

func TestImportCSVMalformedLeavesDataUntouched(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Preexisting")
	before := decodeJSON[timetracker.Data](t, hh.do(t, http.MethodGet, "/data", nil))

	body := csvHeader + "\n" +
		"Acme,#acme,C1,http://insight,typeA,http://sfdc,bucketA,JIRA-1\n" +
		"TooFewFields,a,b,c,d\n"

	w := hh.doMultipart(t, "/import-csv", "file", "bad.csv", []byte(body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
	}

	after := decodeJSON[timetracker.Data](t, hh.do(t, http.MethodGet, "/data", nil))
	if strings.Join(names(after.Customers), ",") != strings.Join(names(before.Customers), ",") {
		t.Errorf("data mutated by malformed import: before %v, after %v", before.Customers, after.Customers)
	}
}

func TestImportCSVHeaderOnly(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Preexisting")

	w := hh.doMultipart(t, "/import-csv", "file", "header-only.csv", []byte(csvHeader+"\n"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
	}

	after := decodeJSON[timetracker.Data](t, hh.do(t, http.MethodGet, "/data", nil))
	if len(after.Customers) != 1 || after.Customers[0].CustomerName != "Preexisting" {
		t.Errorf("data mutated by header-only import: %+v", after.Customers)
	}
}

func TestImportCSVEmptyUpload(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "Preexisting")

	w := hh.doMultipart(t, "/import-csv", "file", "empty.csv", []byte(""))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
	}

	after := decodeJSON[timetracker.Data](t, hh.do(t, http.MethodGet, "/data", nil))
	if len(after.Customers) != 1 || after.Customers[0].CustomerName != "Preexisting" {
		t.Errorf("data mutated by empty-upload import: %+v", after.Customers)
	}
}

func TestImportCSVRoundTrip(t *testing.T) {
	hh := newHarness(t)
	seedCustomers(t, hh, "zeta", "Alpha")

	exportW := hh.do(t, http.MethodGet, "/export-csv", nil)
	if exportW.Code != http.StatusOK {
		t.Fatalf("export: want 200, got %d", exportW.Code)
	}
	csvBody := exportW.Body.Bytes()

	importW := hh.doMultipart(t, "/import-csv", "file", "roundtrip.csv", csvBody)
	if importW.Code != http.StatusOK {
		t.Fatalf("import: want 200, got %d (%s)", importW.Code, importW.Body.String())
	}

	final := decodeJSON[timetracker.Data](t, hh.do(t, http.MethodGet, "/data", nil))
	want := []string{"Alpha", "zeta"}
	if got := names(final.Customers); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("round-tripped customer set = %v, want %v", got, want)
	}
}

func TestImportCSVMissingFileField(t *testing.T) {
	hh := newHarness(t)
	w := hh.doMultipart(t, "/import-csv", "", "", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d (%s)", w.Code, w.Body.String())
	}
	if decodeErr(t, w) == "" {
		t.Error("expected non-empty error message")
	}
}

// ── method-not-allowed / routing ─────────────────────────────────────────────

func TestMethodNotAllowed(t *testing.T) {
	hh := newHarness(t)
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/update"},
		{http.MethodGet, "/delete"},
		{http.MethodPost, "/export-csv"},
		{http.MethodGet, "/import-csv"},
		{http.MethodPost, "/data"},
	}
	for _, c := range cases {
		w := hh.do(t, c.method, c.path, nil)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: want 405, got %d", c.method, c.path, w.Code)
		}
		if decodeErr(t, w) == "" {
			t.Errorf("%s %s: expected non-empty error envelope", c.method, c.path)
		}
	}
}

func TestNoTunnelMappingEndpoint(t *testing.T) {
	dir := t.TempDir()
	staticDir := t.TempDir()
	marker := "<html><body>timetracker-test-index</body></html>"
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(marker), 0644); err != nil {
		t.Fatalf("write index.html fixture: %v", err)
	}

	handler, err := timetracker.Build(config.TimetrackerConfig{
		StaticDir: staticDir,
		DataFile:  filepath.Join(dir, "timetracker.json"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/tunnel-mapping", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (static fallback), got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), marker) {
		t.Errorf("expected static index fallback body, got %q", w.Body.String())
	}
	var probe map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &probe); err == nil {
		t.Errorf("response parsed as a JSON tunnel mapping: %v", probe)
	}
}

func TestStaticFallback(t *testing.T) {
	dir := t.TempDir()
	staticDir := t.TempDir()
	marker := "<html><body>timetracker-test-index</body></html>"
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte(marker), 0644); err != nil {
		t.Fatalf("write index.html fixture: %v", err)
	}

	handler, err := timetracker.Build(config.TimetrackerConfig{
		StaticDir: staticDir,
		DataFile:  filepath.Join(dir, "timetracker.json"),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	for _, path := range []string{"/", "/nonexistent-page"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s: want 200, got %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), marker) {
			t.Errorf("GET %s: expected static index fixture body, got %q", path, w.Body.String())
		}
	}
}
