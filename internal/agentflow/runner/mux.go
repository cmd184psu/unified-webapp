package runner

import "context"

// Mux sends each call to the runner for its backend's api type; every other
// type goes to Default. It lets the opencode runner and the Grok runner share
// one engine.
type Mux struct {
	Default Runner
	ByType  map[string]Runner
}

var _ Runner = (*Mux)(nil)

func (m *Mux) pick(apiType string) Runner {
	if r, ok := m.ByType[apiType]; ok {
		return r
	}
	return m.Default
}

// Run dispatches on req.Backend.APIType.
func (m *Mux) Run(ctx context.Context, req Request) (Result, error) {
	return m.pick(req.Backend.APIType).Run(ctx, req)
}

// Version is the default runner's.
func (m *Mux) Version(ctx context.Context) (string, error) { return m.Default.Version(ctx) }

// BuiltinModels is the default runner's list, when it has one.
func (m *Mux) BuiltinModels(ctx context.Context) ([]string, error) {
	if b, ok := m.Default.(interface {
		BuiltinModels(context.Context) ([]string, error)
	}); ok {
		return b.BuiltinModels(ctx)
	}
	return nil, nil
}

// BuiltinModelsFor lists the own models of the runner that serves apiType.
func (m *Mux) BuiltinModelsFor(ctx context.Context, apiType string) ([]string, error) {
	if b, ok := m.pick(apiType).(interface {
		BuiltinModels(context.Context) ([]string, error)
	}); ok {
		return b.BuiltinModels(ctx)
	}
	return nil, nil
}
