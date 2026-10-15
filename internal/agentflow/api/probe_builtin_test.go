package api

import (
	"context"
	"testing"
)

func TestProbeBuiltinListsOpencodeModelsWithoutAHost(t *testing.T) {
	e := newEnv(t, envOpt{})
	e.api.deps.BuiltinModels = func(context.Context) ([]string, error) { return []string{"big-pickle", "cat-jam-free"}, nil }
	out := decode[struct {
		Models []string `json:"models"`
	}](t, e.ok("POST", "/api/backends/probe", map[string]any{"api_type": "opencode"}, 200))
	if len(out.Models) != 2 || out.Models[0] != "big-pickle" {
		t.Fatalf("models = %v", out.Models)
	}
	b := map[string]any{"name": "free", "host": "", "api_type": "opencode", "hosted": true, "slots": 1,
		"models": []map[string]any{{"model": "big-pickle"}}}
	e.ok("POST", "/api/backends", b, 201)
}
