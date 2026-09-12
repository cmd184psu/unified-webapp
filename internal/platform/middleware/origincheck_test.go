package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// flaggingReader records whether Read was ever called on it, so tests can
// assert that a rejected request's body is never drained.
type flaggingReader struct {
	data []byte
	pos  int
	read bool
}

func (f *flaggingReader) Read(p []byte) (int, error) {
	f.read = true
	if f.pos >= len(f.data) {
		return 0, errors.New("EOF")
	}
	n := copy(p, f.data[f.pos:])
	f.pos += n
	return n, nil
}

func newHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestOriginCheck_TableOverModes(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		method     string
		origin     string
		wantStatus int
		wantCalled bool
		wantLogged bool
	}{
		// enforce
		{"enforce: GET foreign origin passes", ModeEnforce, http.MethodGet, "http://evil.com", http.StatusOK, true, false},
		{"enforce: POST absent origin passes", ModeEnforce, http.MethodPost, "", http.StatusOK, true, false},
		{"enforce: POST foreign origin rejected", ModeEnforce, http.MethodPost, "http://evil.com", http.StatusForbidden, false, true},
		{"enforce: POST same origin passes", ModeEnforce, http.MethodPost, "http://example.com", http.StatusOK, true, false},

		// log
		{"log: GET foreign origin passes", ModeLog, http.MethodGet, "http://evil.com", http.StatusOK, true, false},
		{"log: POST absent origin passes", ModeLog, http.MethodPost, "", http.StatusOK, true, false},
		{"log: POST foreign origin served and logged", ModeLog, http.MethodPost, "http://evil.com", http.StatusOK, true, true},

		// off
		{"off: GET foreign origin passes", ModeOff, http.MethodGet, "http://evil.com", http.StatusOK, true, false},
		{"off: POST absent origin passes", ModeOff, http.MethodPost, "", http.StatusOK, true, false},
		{"off: POST foreign origin passthrough, no logging", ModeOff, http.MethodPost, "http://evil.com", http.StatusOK, true, false},

		// empty mode normalizes to enforce
		{"empty mode behaves like enforce: POST foreign origin rejected", "", http.MethodPost, "http://evil.com", http.StatusForbidden, false, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logBuf bytes.Buffer
			orig := log.Writer()
			log.SetOutput(&logBuf)
			defer log.SetOutput(orig)

			var called bool
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "http://example.com/api/thing", nil)
			req.Host = "example.com"
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			OriginCheck(tc.mode, newHandler(&called)).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if called != tc.wantCalled {
				t.Fatalf("inner handler called = %v, want %v", called, tc.wantCalled)
			}
			logged := logBuf.Len() > 0
			if logged != tc.wantLogged {
				t.Fatalf("logged = %v (log: %q), want %v", logged, logBuf.String(), tc.wantLogged)
			}
			if tc.wantLogged {
				if !strings.Contains(logBuf.String(), tc.origin) {
					t.Errorf("log line missing origin %q: %q", tc.origin, logBuf.String())
				}
				if !strings.Contains(logBuf.String(), "example.com") {
					t.Errorf("log line missing host %q: %q", "example.com", logBuf.String())
				}
			}
			if rec.Code == http.StatusForbidden {
				var body map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("403 body is not the standard JSON error envelope: %v (%s)", err, rec.Body.String())
				}
				if _, ok := body["error"]; !ok {
					t.Fatalf("403 body missing %q field: %s", "error", rec.Body.String())
				}
				if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
					t.Errorf("Content-Type = %q, want application/json", ct)
				}
			}
		})
	}
}

// TestOriginCheck_EnforceDoesNotDrainBody asserts that a rejected request's
// body is never read by the middleware -- rejection must happen before any
// body consumption.
func TestOriginCheck_EnforceDoesNotDrainBody(t *testing.T) {
	var called bool
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://example.com/api/thing", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://evil.com")

	fr := &flaggingReader{data: []byte(`{"some":"payload"}`)}
	req.Body = flaggingReadCloser{fr}

	OriginCheck(ModeEnforce, newHandler(&called)).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if called {
		t.Fatalf("inner handler should not be called on rejection")
	}
	if fr.read {
		t.Fatalf("request body was read before rejection")
	}
}

// flaggingReadCloser adapts a *flaggingReader (which has no Close method) into
// an io.ReadCloser for assignment to http.Request.Body.
type flaggingReadCloser struct {
	*flaggingReader
}

func (f flaggingReadCloser) Close() error { return nil }
