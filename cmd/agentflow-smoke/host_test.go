package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// -host is sent as the Host header on every request, so the smoke can reach production
// through 127.0.0.1:8787 under its public name.
func TestHostFlagSetsTheHostHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Host
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	s := &smoke{base: srv.URL, client: &http.Client{Timeout: 5 * time.Second}, host: "agentflow.example.test"}
	if err := s.get("/api/health", nil); err != nil {
		t.Fatal(err)
	}
	if got != "agentflow.example.test" {
		t.Fatalf("Host = %q", got)
	}
	s.host = ""
	if err := s.get("/api/health", nil); err != nil {
		t.Fatal(err)
	}
	if got == "agentflow.example.test" {
		t.Fatal("no -host: the URL's host is sent")
	}
}
