package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProbeModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"qwen3:32b"},{"name":"mistral:latest"}]}`))
		case "/v1/models":
			w.Write([]byte(`{"data":[{"id":"gpt-x"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	got, err := probeModels(ctx, srv.Client(), srv.URL+"/", "ollama")
	if err != nil || !reflect.DeepEqual(got, []string{"mistral:latest", "qwen3:32b"}) {
		t.Fatalf("ollama: %v %v", got, err)
	}
	got, err = probeModels(ctx, srv.Client(), srv.URL, "openai_compatible")
	if err != nil || !reflect.DeepEqual(got, []string{"gpt-x"}) {
		t.Fatalf("openai: %v %v", got, err)
	}
	for _, c := range [][2]string{{"ftp://x", "ollama"}, {srv.URL, "anthropic"}} {
		if _, err := probeModels(ctx, srv.Client(), c[0], c[1]); err == nil {
			t.Fatalf("%v: want an error", c)
		}
	}
}
