package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	mk := func(host, origin string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://"+host+"/ws", nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}

	cases := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"absent origin header allowed", "example.com", "", true},
		{"matching host allowed", "example.com", "http://example.com", true},
		{"mismatched host rejected", "example.com", "http://evil.com", false},
		{"matching host with matching port allowed", "example.com:8443", "http://example.com:8443", true},
		{"mismatched port rejected", "example.com:8443", "http://example.com:9000", false},
		{"scheme is ignored, only host[:port] matters", "example.com", "https://example.com", true},
		{"unparsable origin falls back to raw comparison and is rejected", "example.com", "://bad-url", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SameOrigin(mk(tc.host, tc.origin))
			if got != tc.want {
				t.Fatalf("SameOrigin(host=%q origin=%q) = %v, want %v", tc.host, tc.origin, got, tc.want)
			}
		})
	}
}

func TestOriginHost(t *testing.T) {
	cases := []struct {
		origin string
		want   string
	}{
		{"http://example.com", "example.com"},
		{"https://example.com:8443", "example.com:8443"},
		{"not a url with no host", "not a url with no host"},
	}

	for _, tc := range cases {
		got := OriginHost(tc.origin)
		if got != tc.want {
			t.Fatalf("OriginHost(%q) = %q, want %q", tc.origin, got, tc.want)
		}
	}
}
