package runner

import (
	"context"
	"testing"
)

type tagRunner struct{ tag string }

func (r tagRunner) Run(context.Context, Request) (Result, error) { return Result{Final: r.tag}, nil }
func (r tagRunner) Version(context.Context) (string, error)      { return r.tag, nil }

func TestMuxSendsEachBackendTypeToItsRunner(t *testing.T) {
	m := &Mux{Default: tagRunner{"oc"}, ByType: map[string]Runner{"grok": tagRunner{"gk"}}}
	for apiType, want := range map[string]string{"grok": "gk", "opencode": "oc", "ollama": "oc", "": "oc"} {
		res, _ := m.Run(context.Background(), Request{Backend: Backend{APIType: apiType}})
		if res.Final != want {
			t.Errorf("%q -> %q, want %q", apiType, res.Final, want)
		}
	}
	if v, _ := m.Version(context.Background()); v != "oc" {
		t.Fatalf("version = %q", v)
	}
}
