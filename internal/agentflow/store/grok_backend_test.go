package store

import "testing"

// A Grok CLI backend has no host: it runs on the user's own Grok login.
func TestGrokBackendNeedsNoHost(t *testing.T) {
	s, _ := openTemp(t)
	b := Backend{Name: "grok", APIType: "grok", Hosted: true, Slots: 1,
		Models: []BackendModel{{Model: "grok-4.5"}, {Model: "grok-4.7"}}}
	if err := s.CreateBackend(ctx, "t", b); err != nil {
		t.Fatalf("create = %v", err)
	}
	got, err := s.GetBackend(ctx, "grok")
	if err != nil || got.APIType != "grok" || len(got.Models) != 2 {
		t.Fatalf("got = %+v, %v", got, err)
	}
}
