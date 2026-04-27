package grocery_test

import (
	"os"
	"path/filepath"
	"testing"

	"cmd184psu/unified-webapp/internal/grocery"
)

func newTempStore(t *testing.T) (*grocery.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	s, err := grocery.New(path)
	if err != nil {
		t.Fatalf("grocery.New: %v", err)
	}
	return s, path
}

// ── Add / List ──────────────────────────────────────────────────────────────

func TestAdd_DefaultsToNeeded(t *testing.T) {
	s, _ := newTempStore(t)
	item, err := s.Add("Milk", "Dairy")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if item.State != grocery.StateNeeded {
		t.Errorf("got state %q, want %q", item.State, grocery.StateNeeded)
	}
	if item.Completed {
		t.Error("new item should not be completed")
	}
}

func TestAdd_AppearsInList(t *testing.T) {
	s, _ := newTempStore(t)
	s.Add("Eggs", "Dairy")
	s.Add("Bread", "Bakery")
	if len(s.List()) != 2 {
		t.Fatalf("List: want 2 items, got %d", len(s.List()))
	}
}

// ── Patch ────────────────────────────────────────────────────────────────────

func TestPatch_StateChange(t *testing.T) {
	s, _ := newTempStore(t)
	item, _ := s.Add("Butter", "Dairy")
	st := grocery.StateCheck
	updated, err := s.Patch(item.ID, grocery.PatchPayload{State: &st})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if updated.State != grocery.StateCheck {
		t.Errorf("got %q, want %q", updated.State, grocery.StateCheck)
	}
}

func TestPatch_CompletedToggle(t *testing.T) {
	s, _ := newTempStore(t)
	item, _ := s.Add("Yogurt", "Dairy")
	tr := true
	updated, _ := s.Patch(item.ID, grocery.PatchPayload{Completed: &tr})
	if !updated.Completed {
		t.Error("expected completed=true")
	}
}

func TestPatch_NotFound(t *testing.T) {
	s, _ := newTempStore(t)
	_, err := s.Patch("nonexistent", grocery.PatchPayload{})
	if err == nil {
		t.Error("expected error for unknown id")
	}
}

// ── Delete ───────────────────────────────────────────────────────────────────

func TestDelete_RemovesItem(t *testing.T) {
	s, _ := newTempStore(t)
	item, _ := s.Add("Cheese", "Dairy")
	if err := s.Delete(item.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("list should be empty after delete")
	}
}

func TestDelete_NotFound(t *testing.T) {
	s, _ := newTempStore(t)
	if err := s.Delete("ghost"); err == nil {
		t.Error("expected error for unknown id")
	}
}

// ── Reset ────────────────────────────────────────────────────────────────────

func TestReset_ClearsCompletedAndSetsCheck(t *testing.T) {
	s, _ := newTempStore(t)
	item1, _ := s.Add("Apple", "Produce")
	item2, _ := s.Add("Banana", "Produce")
	tr := true
	ns := grocery.StateNotNeeded
	s.Patch(item1.ID, grocery.PatchPayload{Completed: &tr, State: &ns})
	s.Patch(item2.ID, grocery.PatchPayload{Completed: &tr})

	result, err := s.Reset()
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	for _, it := range result {
		if it.Completed {
			t.Errorf("item %s: completed should be false after reset", it.ID)
		}
		if it.State != grocery.StateCheck {
			t.Errorf("item %s: state should be %q, got %q", it.ID, grocery.StateCheck, it.State)
		}
	}
}

// ── Groups ───────────────────────────────────────────────────────────────────

func TestSaveGroups_OrphansItemsToNoGroup(t *testing.T) {
	s, _ := newTempStore(t)
	s.SaveGroups([]string{"Produce", "Dairy"})
	item, _ := s.Add("Carrot", "Produce")

	if err := s.SaveGroups([]string{"Dairy"}); err != nil {
		t.Fatalf("SaveGroups: %v", err)
	}
	items := s.List()
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	if items[0].ID != item.ID {
		t.Error("wrong item returned")
	}
	if items[0].Group != grocery.NoGroup {
		t.Errorf("item group: got %q, want %q", items[0].Group, grocery.NoGroup)
	}
}

func TestSaveGroups_ItemsInOtherGroupsUnaffected(t *testing.T) {
	s, _ := newTempStore(t)
	s.SaveGroups([]string{"Produce", "Dairy"})
	s.Add("Milk", "Dairy")
	s.Add("Carrot", "Produce")
	s.SaveGroups([]string{"Dairy"})
	for _, it := range s.List() {
		if it.Name == "Milk" && it.Group != "Dairy" {
			t.Errorf("Milk should stay in Dairy, got %q", it.Group)
		}
	}
}

func TestSaveGroups_NoGroupNameReserved(t *testing.T) {
	s, _ := newTempStore(t)
	item, _ := s.Add("Orphan", grocery.NoGroup)
	s.SaveGroups([]string{"Dairy"})
	found := false
	for _, it := range s.List() {
		if it.ID == item.ID {
			found = true
			if it.Group != grocery.NoGroup {
				t.Errorf("orphan group: got %q, want %q", it.Group, grocery.NoGroup)
			}
		}
	}
	if !found {
		t.Error("orphaned item disappeared from list")
	}
}

// ── Persistence ──────────────────────────────────────────────────────────────

func TestPersistence_RoundTrip(t *testing.T) {
	s, path := newTempStore(t)
	s.SaveGroups([]string{"Frozen"})
	s.Add("Ice Cream", "Frozen")

	s2, err := grocery.New(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	items := s2.List()
	if len(items) != 1 {
		t.Fatalf("want 1 item after reload, got %d", len(items))
	}
	if items[0].Name != "Ice Cream" {
		t.Errorf("got %q, want %q", items[0].Name, "Ice Cream")
	}
	groups := s2.Groups()
	if len(groups) != 1 || groups[0] != "Frozen" {
		t.Errorf("groups after reload: %v", groups)
	}
}

func TestPersistence_LegacyArrayFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	legacy := `[{"id":"1","name":"OldItem","group":"A","state":"needed",
		"completed":false,"order":0,"created_at":"2024-01-01T00:00:00Z"}]`
	os.WriteFile(path, []byte(legacy), 0644)

	s, err := grocery.New(path)
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	items := s.List()
	if len(items) != 1 || items[0].Name != "OldItem" {
		t.Errorf("legacy load failed: %v", items)
	}
}

// ── Reorder / Move ───────────────────────────────────────────────────────────

func TestReorder_SetsOrder(t *testing.T) {
	s, _ := newTempStore(t)
	a, _ := s.Add("A", "G")
	b, _ := s.Add("B", "G")
	c, _ := s.Add("C", "G")

	if err := s.Reorder("G", []string{c.ID, b.ID, a.ID}); err != nil {
		t.Fatalf("Reorder: %v", err)
	}
	orderMap := map[string]int{}
	for _, it := range s.List() {
		orderMap[it.Name] = it.Order
	}
	if !(orderMap["C"] < orderMap["B"] && orderMap["B"] < orderMap["A"]) {
		t.Errorf("unexpected order map: %v", orderMap)
	}
}

func TestMove_ChangesGroup(t *testing.T) {
	s, _ := newTempStore(t)
	item, _ := s.Add("Spinach", "Produce")
	moved, err := s.Move(item.ID, grocery.MovePayload{Group: "Frozen", OrderIDs: []string{item.ID}})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if moved.Group != "Frozen" {
		t.Errorf("got group %q, want Frozen", moved.Group)
	}
}

// ── Revision ─────────────────────────────────────────────────────────────────

func TestRevision_IncrementsOnMutation(t *testing.T) {
	s, _ := newTempStore(t)
	r0 := s.Revision()
	item, _ := s.Add("Milk", "Dairy")
	r1 := s.Revision()
	if r1 <= r0 {
		t.Errorf("revision should increase after Add: %d → %d", r0, r1)
	}
	st := grocery.StateCheck
	s.Patch(item.ID, grocery.PatchPayload{State: &st})
	r2 := s.Revision()
	if r2 <= r1 {
		t.Errorf("revision should increase after Patch: %d → %d", r1, r2)
	}
	s.Delete(item.ID)
	r3 := s.Revision()
	if r3 <= r2 {
		t.Errorf("revision should increase after Delete: %d → %d", r2, r3)
	}
}
