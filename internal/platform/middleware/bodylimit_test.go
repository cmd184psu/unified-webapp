package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cmd184psu/unified-webapp/internal/platform/response"
)

// echoHandler is a tiny JSON decode handler standing in for a real module
// endpoint: it decodes the body and, on failure, reports through
// response.WriteDecodeError -- exactly the path every module's decode
// branches use, so this test exercises BodyLimit the same way a real request
// would.
func echoHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Msg string `json:"msg"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			response.WriteDecodeError(w, err)
			return
		}
		response.WriteJSON(w, http.StatusOK, body)
	})
}

func TestBodyLimit(t *testing.T) {
	const limit = 32

	// A JSON payload of exactly `limit` bytes and one that overflows it by one
	// byte, both built from the same {"msg":"..."} shape so only size varies.
	atLimit := func() []byte {
		prefix := `{"msg":"`
		suffix := `"}`
		pad := limit - len(prefix) - len(suffix)
		return []byte(prefix + strings.Repeat("a", pad) + suffix)
	}()
	if len(atLimit) != limit {
		t.Fatalf("test setup: atLimit is %d bytes, want %d", len(atLimit), limit)
	}
	overLimit := []byte(strings.Replace(string(atLimit), `"a`, `"aa`, 1))
	if len(overLimit) <= limit {
		t.Fatalf("test setup: overLimit is %d bytes, want > %d", len(overLimit), limit)
	}

	cases := []struct {
		name       string
		body       []byte
		wantStatus int
	}{
		{"at limit decodes and succeeds", atLimit, http.StatusOK},
		{"over limit is rejected as 413", overLimit, http.StatusRequestEntityTooLarge},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "http://example.com/api", bytes.NewReader(c.body))

			BodyLimit(limit, echoHandler()).ServeHTTP(rec, req)

			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, c.wantStatus, rec.Body.String())
			}
		})
	}
}
