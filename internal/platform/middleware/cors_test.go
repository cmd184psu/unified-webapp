package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func innerHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})
}

func TestWrap_AbsentOrigin_NoACAO(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = "example.com"

	Wrap(innerHandler("ok")).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if got := rec.Header().Get("Vary"); got != "" {
		t.Fatalf("Vary = %q, want empty", got)
	}
}

func TestWrap_SameOrigin_ReflectsACAO(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")

	Wrap(innerHandler("ok")).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "http://example.com")
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want %q", got, "Origin")
	}
}

func TestWrap_ForeignOrigin_NoACAO(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://evil.com")

	Wrap(innerHandler("ok")).ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want %q", got, "Origin")
	}
}

func TestWrap_OptionsPreflight_SameOriginShortCircuits(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "http://example.com/api", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://example.com")

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	Wrap(inner).ServeHTTP(rec, req)

	if called {
		t.Fatalf("inner handler should not be called for same-origin OPTIONS preflight")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want %q", got, "http://example.com")
	}
}

func TestWrap_OptionsPreflight_ForeignOriginFallsThrough(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "http://example.com/api", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "http://evil.com")

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	Wrap(inner).ServeHTTP(rec, req)

	if !called {
		t.Fatalf("inner handler should be called for foreign-origin OPTIONS request")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty", got)
	}
}

func TestWrap_SetsXContentTypeOptions_OnSuccess(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = "example.com"

	Wrap(innerHandler("ok")).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestWrap_SetsXContentTypeOptions_OnErrorPath(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = "example.com"

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	Wrap(inner).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want %q", got, "nosniff")
	}
}

func TestWrap_PlainGET_ReachesInnerHandlerUnchanged(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.com/api", nil)
	req.Host = "example.com"

	Wrap(innerHandler("hello world")).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != "hello world" {
		t.Fatalf("body = %q, want %q", string(body), "hello world")
	}
}
