package api

import "testing"

func TestBackendKiloAndCLIPath(t *testing.T) {
	e := newEnv(t, envOpt{})
	body := map[string]any{
		"name": "kilo", "host": "", "api_type": "kilo", "hosted": true,
		"slots": 1, "cli_path": "/opt/homebrew/bin/kilo",
		"models": []map[string]any{{"model": "kilo/liquid/lfm-3-1.7b:free"}},
	}
	out := decode[backendResult](t, e.ok("POST", "/api/backends", body, 201))
	if out.Backend.APIType != "kilo" || out.Backend.CLIPath != "/opt/homebrew/bin/kilo" {
		t.Fatalf("backend = %+v", out.Backend)
	}
	e.ok("PATCH", "/api/backends/kilo", map[string]any{"cli_path": ""}, 200)
	list := decode[[]BackendView](t, e.ok("GET", "/api/backends", nil, 200))
	if len(list) != 1 || list[0].CLIPath != "" {
		t.Fatalf("backends = %+v", list)
	}
}
