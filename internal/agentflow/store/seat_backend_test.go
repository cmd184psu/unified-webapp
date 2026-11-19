package store

import "testing"

// AF-9 named botchat_seat in code but never in the backends CHECK, so such a backend could
// not be saved; migration 35 allows it and AF-11's freebuff_seat. Neither has a host. The
// seat and notification settings come with it.
func TestSeatBackendsCanBeSaved(t *testing.T) {
	s, _ := openTemp(t)
	for _, b := range []Backend{
		{Name: "deepseek4", APIType: "botchat_seat", Slots: 2, Models: []BackendModel{{Model: "deepseek4"}}},
		{Name: "fb", APIType: "freebuff_seat", Slots: 1, Models: []BackendModel{{Model: "freebuff"}}},
	} {
		if err := s.CreateBackend(ctx, "t", b); err != nil {
			t.Fatalf("%s: create = %v", b.APIType, err)
		}
		got, err := s.GetBackend(ctx, b.Name)
		if err != nil || got.APIType != b.APIType || got.Slots != b.Slots {
			t.Fatalf("%s: got %+v, %v", b.APIType, got, err)
		}
	}
	set, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if set.SeatMux != "tmux" || set.SeatProfiles != "" || !set.NotifyMacOS || set.NotifyCommand != "" || set.NotifyURL != "" {
		t.Fatalf("seat/notify defaults: %+v", set)
	}
	if err := s.UpdateSettings(ctx, "t", map[string]string{"seat_profiles": "not json"}); err == nil {
		t.Fatal("seat_profiles must be a JSON object")
	}
	if err := s.UpdateSettings(ctx, "t", map[string]string{"seat_profiles": `{"freebuff":{"auth_paths":[".config/freebuff"]}}`, "notify_macos": "off"}); err != nil {
		t.Fatal(err)
	}
}
