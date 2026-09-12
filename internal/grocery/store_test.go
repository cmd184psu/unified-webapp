package grocery_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// ── Recipe persistence ───────────────────────────────────────────────────────

// AC-1.1: an object file written before recipes existed loads with no recipes,
// and Recipes() is a non-nil empty slice rather than nil.
func TestPersistence_BackCompat_NoRecipesKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	legacy := `{"title":"Groceries","groups":["Produce"],"items":[
		{"id":"1","name":"Beef","group":"Produce","state":"needed",
		 "completed":false,"order":0,"created_at":"2024-01-01T00:00:00Z"}]}`
	os.WriteFile(path, []byte(legacy), 0644)

	s, err := grocery.New(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	recipes := s.Recipes()
	if recipes == nil {
		t.Fatal("Recipes() returned nil, want non-nil empty slice")
	}
	if len(recipes) != 0 {
		t.Errorf("want 0 recipes, got %d", len(recipes))
	}
	if items := s.List(); len(items) != 1 || items[0].Name != "Beef" {
		t.Errorf("items lost on back-compat load: %v", items)
	}
}

// AC-1.2: the legacy plain-array format still loads and yields no recipes.
func TestPersistence_LegacyArrayFormat_HasNoRecipes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.json")
	legacy := `[{"id":"1","name":"OldItem","group":"A","state":"needed",
		"completed":false,"order":0,"created_at":"2024-01-01T00:00:00Z"}]`
	os.WriteFile(path, []byte(legacy), 0644)

	s, err := grocery.New(path)
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if items := s.List(); len(items) != 1 || items[0].Name != "OldItem" {
		t.Errorf("legacy load failed: %v", items)
	}
	recipes := s.Recipes()
	if recipes == nil {
		t.Fatal("Recipes() returned nil after legacy load, want non-nil empty slice")
	}
	if len(recipes) != 0 {
		t.Errorf("want 0 recipes after legacy load, got %d", len(recipes))
	}
	if items := s.List(); items[0].RecipeID != "" {
		t.Errorf("legacy item picked up a RecipeID: %q", items[0].RecipeID)
	}
}

// AC-1.4: a recipe survives save() → grocery.New with all five fields intact,
// as does the recipe_id linkage on its ingredient.
func TestPersistence_RecipeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	fixture := `{"items":[
		{"id":"10","name":"beef","group":"No Group","state":"needed",
		 "completed":false,"order":0,"created_at":"2024-01-02T00:00:00Z","recipe_id":"7"}],
		"recipes":[
		{"id":"7","name":"Chili","enabled":true,"order":3,
		 "created_at":"2024-01-01T00:00:00Z"}]}`
	os.WriteFile(path, []byte(fixture), 0644)

	s, err := grocery.New(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Any mutation forces a save() through the new storeData shape.
	if err := s.SetTitle("Groceries"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}

	s2, err := grocery.New(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	recipes := s2.Recipes()
	if len(recipes) != 1 {
		t.Fatalf("want 1 recipe after reload, got %d", len(recipes))
	}
	r := recipes[0]
	if r.ID != "7" {
		t.Errorf("ID: got %q, want %q", r.ID, "7")
	}
	if r.Name != "Chili" {
		t.Errorf("Name: got %q, want %q", r.Name, "Chili")
	}
	if !r.Enabled {
		t.Error("Enabled: got false, want true")
	}
	if r.Order != 3 {
		t.Errorf("Order: got %d, want 3", r.Order)
	}
	want := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	if !r.CreatedAt.Equal(want) {
		t.Errorf("CreatedAt: got %v, want %v", r.CreatedAt, want)
	}
	items := s2.List()
	if len(items) != 1 || items[0].RecipeID != "7" {
		t.Errorf("recipe_id linkage lost on round-trip: %v", items)
	}
}

// AC-1.3: a store that holds only free items writes neither a "recipes" key nor
// a "recipe_id" key. This is the whole of AC-1.3 and it needs no real
// grocery.json — omitempty on an empty non-nil []*Recipe omits the key.
func TestPersistence_NoRecipeKeysForFreeItems(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	fixture := `{"title":"Groceries","groups":["Produce"],"items":[]}`
	os.WriteFile(path, []byte(fixture), 0644)

	s, err := grocery.New(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, err := s.Add("beef", "Produce"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(data), `"recipes"`) {
		t.Errorf("on-disk file contains a \"recipes\" key for a free-items-only store:\n%s", data)
	}
	if strings.Contains(string(data), `"recipe_id"`) {
		t.Errorf("on-disk file contains a \"recipe_id\" key for a free item:\n%s", data)
	}
}

// ── Recipes ─────────────────────────────────────────────────────────────────

// newBlockableStore returns a store plus a function that makes every subsequent
// save() fail deterministically.
//
// The mechanism is deliberately not chmod: tests run as root inside most
// container images, and root walks straight through a 0500 directory, so a
// permission-based test would silently pass by never failing the save. Here
// filepath.Dir(filePath) is replaced with a *regular file*, so the os.MkdirAll
// at the top of save() returns ENOTDIR for every user on every platform. That
// also exercises the exact call that moved out of Add and into save().
func newBlockableStore(t *testing.T) (*grocery.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	s, err := grocery.New(filepath.Join(blocker, "grocery.json"))
	if err != nil {
		t.Fatalf("grocery.New: %v", err)
	}
	return s, func() {
		t.Helper()
		if err := os.RemoveAll(blocker); err != nil {
			t.Fatalf("remove blocker dir: %v", err)
		}
		if err := os.WriteFile(blocker, []byte("not a directory"), 0644); err != nil {
			t.Fatalf("write blocker file: %v", err)
		}
	}
}

func TestAddRecipe_DefaultsToDisabled(t *testing.T) {
	s, _ := newTempStore(t)
	r, err := s.AddRecipe("Chili")
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	if r.Enabled {
		t.Error("a new recipe should start disabled")
	}
	if r.Name != "Chili" {
		t.Errorf("Name: got %q, want %q", r.Name, "Chili")
	}
	if len(s.Recipes()) != 1 {
		t.Errorf("want 1 recipe, got %d", len(s.Recipes()))
	}
}

func TestAddRecipe_TrimsAndRejectsEmptyName(t *testing.T) {
	s, _ := newTempStore(t)
	if _, err := s.AddRecipe("   "); !errors.Is(err, grocery.ErrInvalidName) {
		t.Errorf("blank name: got %v, want ErrInvalidName", err)
	}
	r, err := s.AddRecipe("  Chili  ")
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	if r.Name != "Chili" {
		t.Errorf("name not trimmed: %q", r.Name)
	}
}

func TestAddRecipe_DuplicateNameIsCaseInsensitive(t *testing.T) {
	s, _ := newTempStore(t)
	if _, err := s.AddRecipe("Chili"); err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	if _, err := s.AddRecipe("chili"); !errors.Is(err, grocery.ErrDuplicateRecipe) {
		t.Errorf("got %v, want ErrDuplicateRecipe", err)
	}
	if n := len(s.Recipes()); n != 1 {
		t.Errorf("duplicate was created anyway: %d recipes", n)
	}
}

// max(Order)+1, not len(recipes): deleting a middle recipe must not make the
// next recipe collide with an existing Order.
func TestAddRecipe_OrderIsMaxPlusOneNotLen(t *testing.T) {
	s, _ := newTempStore(t)
	s.AddRecipe("A")
	b, _ := s.AddRecipe("B")
	c, _ := s.AddRecipe("C")
	if _, err := s.DeleteRecipe(b.ID); err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	d, err := s.AddRecipe("D")
	if err != nil {
		t.Fatalf("AddRecipe: %v", err)
	}
	if d.Order == c.Order {
		t.Errorf("new recipe collided with existing Order %d", c.Order)
	}
	if d.Order <= c.Order {
		t.Errorf("Order: got %d, want > %d", d.Order, c.Order)
	}
}

func TestPatchRecipe_UnknownID(t *testing.T) {
	s, _ := newTempStore(t)
	name := "Chili"
	if _, _, err := s.PatchRecipe("nope", &name, nil); !errors.Is(err, grocery.ErrRecipeNotFound) {
		t.Errorf("name-only: got %v, want ErrRecipeNotFound", err)
	}
	on := true
	if _, _, err := s.PatchRecipe("nope", nil, &on); !errors.Is(err, grocery.ErrRecipeNotFound) {
		t.Errorf("enabled-only: got %v, want ErrRecipeNotFound", err)
	}
	// The neither-field case still checks existence.
	if _, _, err := s.PatchRecipe("nope", nil, nil); !errors.Is(err, grocery.ErrRecipeNotFound) {
		t.Errorf("neither-field: got %v, want ErrRecipeNotFound", err)
	}
}

func TestPatchRecipe_EmptyNameIsInvalid(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	blank := "   "
	if _, _, err := s.PatchRecipe(r.ID, &blank, nil); !errors.Is(err, grocery.ErrInvalidName) {
		t.Errorf("got %v, want ErrInvalidName", err)
	}
	if s.Recipes()[0].Name != "Chili" {
		t.Errorf("name changed despite the error: %q", s.Recipes()[0].Name)
	}
}

func TestPatchRecipe_RenameToExistingName(t *testing.T) {
	s, _ := newTempStore(t)
	s.AddRecipe("Chili")
	tacos, _ := s.AddRecipe("Tacos")
	name := "chili"
	if _, _, err := s.PatchRecipe(tacos.ID, &name, nil); !errors.Is(err, grocery.ErrDuplicateRecipe) {
		t.Errorf("got %v, want ErrDuplicateRecipe", err)
	}
}

func TestPatchRecipe_RenameToOwnNameDifferentCaseSucceeds(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	name := "CHILI"
	got, _, err := s.PatchRecipe(r.ID, &name, nil)
	if err != nil {
		t.Fatalf("self-rename should be allowed: %v", err)
	}
	if got.Name != "CHILI" {
		t.Errorf("Name: got %q, want %q", got.Name, "CHILI")
	}
}

func TestPatchRecipe_RenameOnlyDoesNotTouchItems(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	on := true
	s.PatchRecipe(r.ID, nil, &on)
	item, _ := s.AddIngredient(r.ID, "beef")
	notNeeded := grocery.StateNotNeeded
	s.Patch(item.ID, grocery.PatchPayload{State: &notNeeded})

	name := "Chili con carne"
	_, items, err := s.PatchRecipe(r.ID, &name, nil)
	if err != nil {
		t.Fatalf("PatchRecipe: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("a rename-only patch must still return the full item list, got %d", len(items))
	}
	if items[0].State != grocery.StateNotNeeded {
		t.Errorf("rename changed an item's state: %v", items[0].State)
	}
}

// Validate before you mutate: a rejected both-fields patch must leave the
// recipe and every owned item exactly as they were.
func TestPatchRecipe_DuplicateNameLeavesNothingMutated(t *testing.T) {
	s, _ := newTempStore(t)
	s.AddRecipe("Chili")
	tacos, _ := s.AddRecipe("Tacos")
	beef, _ := s.AddIngredient(tacos.ID, "beef")

	name, on := "chili", true
	if _, _, err := s.PatchRecipe(tacos.ID, &name, &on); !errors.Is(err, grocery.ErrDuplicateRecipe) {
		t.Fatalf("got %v, want ErrDuplicateRecipe", err)
	}
	for _, r := range s.Recipes() {
		if r.ID == tacos.ID {
			if r.Name != "Tacos" {
				t.Errorf("Name mutated despite the error: %q", r.Name)
			}
			if r.Enabled {
				t.Error("Enabled mutated despite the error")
			}
		}
	}
	for _, it := range s.List() {
		if it.ID == beef.ID && it.State != grocery.StateNotNeeded {
			t.Errorf("owned item state mutated despite the error: %v", it.State)
		}
	}
}

func TestPatchRecipe_EnableFlipsOwnedItemsToNeeded(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	beef, _ := s.AddIngredient(r.ID, "beef")
	beans, _ := s.AddIngredient(r.ID, "beans")
	// Complete one so the toggle has a Completed flag to clear.
	done := true
	s.Patch(beef.ID, grocery.PatchPayload{Completed: &done})

	on := true
	_, items, err := s.PatchRecipe(r.ID, nil, &on)
	if err != nil {
		t.Fatalf("PatchRecipe: %v", err)
	}
	seen := 0
	for _, it := range items {
		if it.ID != beef.ID && it.ID != beans.ID {
			continue
		}
		seen++
		if it.State != grocery.StateNeeded {
			t.Errorf("%s: state %v, want needed", it.Name, it.State)
		}
		if it.Completed {
			t.Errorf("%s: still completed after enable", it.Name)
		}
	}
	if seen != 2 {
		t.Errorf("saw %d owned items, want 2", seen)
	}
}

func TestPatchRecipe_DisableFlipsOwnedItemsToNotNeeded(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	on := true
	s.PatchRecipe(r.ID, nil, &on)
	s.AddIngredient(r.ID, "beef")

	off := false
	_, items, err := s.PatchRecipe(r.ID, nil, &off)
	if err != nil {
		t.Fatalf("PatchRecipe: %v", err)
	}
	for _, it := range items {
		if it.RecipeID == r.ID && it.State != grocery.StateNotNeeded {
			t.Errorf("%s: state %v, want not_needed", it.Name, it.State)
		}
	}
}

func TestPatchRecipe_TogglingOneRecipeLeavesOthersAlone(t *testing.T) {
	s, _ := newTempStore(t)
	a, _ := s.AddRecipe("Chili")
	b, _ := s.AddRecipe("Tacos")
	aBeef, _ := s.AddIngredient(a.ID, "beef")
	bBeef, _ := s.AddIngredient(b.ID, "beef")
	free, _ := s.Add("milk", "Dairy")

	on := true
	if _, _, err := s.PatchRecipe(a.ID, nil, &on); err != nil {
		t.Fatalf("PatchRecipe: %v", err)
	}
	for _, it := range s.List() {
		switch it.ID {
		case aBeef.ID:
			if it.State != grocery.StateNeeded {
				t.Errorf("A's beef: %v, want needed", it.State)
			}
		case bBeef.ID:
			if it.State != grocery.StateNotNeeded {
				t.Errorf("B's beef changed with A's toggle: %v", it.State)
			}
		case free.ID:
			if it.State != grocery.StateNeeded {
				t.Errorf("free item changed with A's toggle: %v", it.State)
			}
		}
	}
}

// AC-4.5: two recipes each own their own "beef"; they are distinct items.
func TestAddIngredient_DuplicateNamesAcrossRecipesAreDistinct(t *testing.T) {
	s, _ := newTempStore(t)
	chili, _ := s.AddRecipe("Chili")
	tacos, _ := s.AddRecipe("Tacos")
	a, err := s.AddIngredient(chili.ID, "beef")
	if err != nil {
		t.Fatalf("AddIngredient: %v", err)
	}
	b, err := s.AddIngredient(tacos.ID, "beef")
	if err != nil {
		t.Fatalf("AddIngredient: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("two recipes' beef share an ID: %s", a.ID)
	}
	if a.RecipeID != chili.ID || b.RecipeID != tacos.ID {
		t.Errorf("ownership wrong: %s→%s, %s→%s", a.Name, a.RecipeID, b.Name, b.RecipeID)
	}
	if a.Group != grocery.NoGroup || b.Group != grocery.NoGroup {
		t.Errorf("ingredients should land in NoGroup, got %q/%q", a.Group, b.Group)
	}
}

// A manual override survives, and re-enabling restores it.
func TestPatchRecipe_ManualOverrideSurvivesUntilReEnable(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	on, off := true, false
	s.PatchRecipe(r.ID, nil, &on)
	beef, _ := s.AddIngredient(r.ID, "beef")

	notNeeded := grocery.StateNotNeeded
	if _, err := s.Patch(beef.ID, grocery.PatchPayload{State: &notNeeded}); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	// A rename must not undo the manual override.
	name := "Chili con carne"
	s.PatchRecipe(r.ID, &name, nil)
	for _, it := range s.List() {
		if it.ID == beef.ID && it.State != grocery.StateNotNeeded {
			t.Fatalf("manual override lost: %v", it.State)
		}
	}
	// Disabling and re-enabling restores needed.
	s.PatchRecipe(r.ID, nil, &off)
	s.PatchRecipe(r.ID, nil, &on)
	for _, it := range s.List() {
		if it.ID == beef.ID && it.State != grocery.StateNeeded {
			t.Errorf("re-enable did not restore needed: %v", it.State)
		}
	}
}

func TestAddIngredient_StateFollowsRecipeEnabled(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	off, err := s.AddIngredient(r.ID, "beans")
	if err != nil {
		t.Fatalf("AddIngredient: %v", err)
	}
	if off.State != grocery.StateNotNeeded {
		t.Errorf("disabled recipe: got %v, want not_needed", off.State)
	}
	on := true
	s.PatchRecipe(r.ID, nil, &on)
	added, err := s.AddIngredient(r.ID, "beef")
	if err != nil {
		t.Fatalf("AddIngredient: %v", err)
	}
	if added.State != grocery.StateNeeded {
		t.Errorf("enabled recipe: got %v, want needed", added.State)
	}
}

func TestAddIngredient_UnknownRecipeAndEmptyName(t *testing.T) {
	s, _ := newTempStore(t)
	if _, err := s.AddIngredient("nope", "beef"); !errors.Is(err, grocery.ErrRecipeNotFound) {
		t.Errorf("unknown recipe: got %v, want ErrRecipeNotFound", err)
	}
	r, _ := s.AddRecipe("Chili")
	if _, err := s.AddIngredient(r.ID, "  "); !errors.Is(err, grocery.ErrInvalidName) {
		t.Errorf("blank name: got %v, want ErrInvalidName", err)
	}
}

func TestDeleteIngredient_MismatchedRecipeIsNotFound(t *testing.T) {
	s, _ := newTempStore(t)
	chili, _ := s.AddRecipe("Chili")
	tacos, _ := s.AddRecipe("Tacos")
	beef, _ := s.AddIngredient(chili.ID, "beef")

	if err := s.DeleteIngredient(tacos.ID, beef.ID); !errors.Is(err, grocery.ErrRecipeNotFound) {
		t.Errorf("mismatch: got %v, want ErrRecipeNotFound", err)
	}
	if err := s.DeleteIngredient(chili.ID, "nope"); !errors.Is(err, grocery.ErrRecipeNotFound) {
		t.Errorf("unknown item: got %v, want ErrRecipeNotFound", err)
	}
	if len(s.List()) != 1 {
		t.Error("the item was removed by a rejected delete")
	}
	if err := s.DeleteIngredient(chili.ID, beef.ID); err != nil {
		t.Fatalf("DeleteIngredient: %v", err)
	}
	if len(s.List()) != 0 {
		t.Error("the item survived its own delete")
	}
}

func TestDeleteRecipe_RemovesOnlyItsOwnItems(t *testing.T) {
	s, _ := newTempStore(t)
	chili, _ := s.AddRecipe("Chili")
	tacos, _ := s.AddRecipe("Tacos")
	s.AddIngredient(chili.ID, "beef")
	s.AddIngredient(chili.ID, "beans")
	tacoBeef, _ := s.AddIngredient(tacos.ID, "beef")
	free, _ := s.Add("milk", "Dairy")

	items, err := s.DeleteRecipe(chili.ID)
	if err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 surviving items, got %d", len(items))
	}
	survivors := map[string]bool{}
	for _, it := range items {
		survivors[it.ID] = true
	}
	if !survivors[tacoBeef.ID] {
		t.Error("another recipe's ingredient was deleted")
	}
	if !survivors[free.ID] {
		t.Error("a free item was deleted")
	}
	if len(s.Recipes()) != 1 {
		t.Errorf("want 1 surviving recipe, got %d", len(s.Recipes()))
	}
}

func TestDeleteRecipe_UnknownID(t *testing.T) {
	s, _ := newTempStore(t)
	if _, err := s.DeleteRecipe("nope"); !errors.Is(err, grocery.ErrRecipeNotFound) {
		t.Errorf("got %v, want ErrRecipeNotFound", err)
	}
}

func TestReorderRecipes_SetsOrderAndIgnoresUnknown(t *testing.T) {
	s, _ := newTempStore(t)
	a, _ := s.AddRecipe("A")
	b, _ := s.AddRecipe("B")
	c, _ := s.AddRecipe("C")

	got, err := s.ReorderRecipes([]string{c.ID, "ghost", b.ID, a.ID})
	if err != nil {
		t.Fatalf("ReorderRecipes: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 recipes, got %d", len(got))
	}
	names := []string{got[0].Name, got[1].Name, got[2].Name}
	if names[0] != "C" || names[2] != "A" {
		t.Errorf("order after reorder: %v", names)
	}
}

// ── Create rollback on save failure ──────────────────────────────────────────

func TestAdd_RollsBackWhenSaveFails(t *testing.T) {
	s, block := newBlockableStore(t)
	if _, err := s.Add("milk", "Dairy"); err != nil {
		t.Fatalf("Add before blocking: %v", err)
	}
	block()
	if _, err := s.Add("beef", "Produce"); err == nil {
		t.Fatal("Add should have failed once save() could not write")
	}
	for _, it := range s.List() {
		if it.Name == "beef" {
			t.Error("the failed Add left a phantom item in memory")
		}
	}
}

func TestAddRecipe_RollsBackWhenSaveFails(t *testing.T) {
	s, block := newBlockableStore(t)
	if _, err := s.AddRecipe("Chili"); err != nil {
		t.Fatalf("AddRecipe before blocking: %v", err)
	}
	block()
	if _, err := s.AddRecipe("Tacos"); err == nil {
		t.Fatal("AddRecipe should have failed once save() could not write")
	}
	for _, r := range s.Recipes() {
		if r.Name == "Tacos" {
			t.Error("the failed AddRecipe left a phantom recipe in memory")
		}
	}
}

func TestAddIngredient_RollsBackWhenSaveFails(t *testing.T) {
	s, block := newBlockableStore(t)
	r, err := s.AddRecipe("Chili")
	if err != nil {
		t.Fatalf("AddRecipe before blocking: %v", err)
	}
	block()
	if _, err := s.AddIngredient(r.ID, "beef"); err == nil {
		t.Fatal("AddIngredient should have failed once save() could not write")
	}
	for _, it := range s.List() {
		if it.Name == "beef" {
			t.Error("the failed AddIngredient left a phantom item in memory")
		}
	}
}

func TestDeleteRecipe_RestoresSnapshotWhenSaveFails(t *testing.T) {
	s, block := newBlockableStore(t)
	r, err := s.AddRecipe("Chili")
	if err != nil {
		t.Fatalf("AddRecipe before blocking: %v", err)
	}
	s.AddIngredient(r.ID, "beef")
	s.AddIngredient(r.ID, "beans")
	block()

	if _, err := s.DeleteRecipe(r.ID); err == nil {
		t.Fatal("DeleteRecipe should have failed once save() could not write")
	}
	if len(s.Recipes()) != 1 {
		t.Errorf("recipe not restored after the failed delete: %d recipes", len(s.Recipes()))
	}
	if len(s.List()) != 2 {
		t.Errorf("ingredients not restored after the failed delete: %d items", len(s.List()))
	}
}

func TestPatchRecipe_RestoresSnapshotWhenSaveFails(t *testing.T) {
	s, block := newBlockableStore(t)
	r, err := s.AddRecipe("Chili")
	if err != nil {
		t.Fatalf("AddRecipe before blocking: %v", err)
	}
	beef, _ := s.AddIngredient(r.ID, "beef")
	// A completed, manually-overridden ingredient: the toggle rewrites BOTH
	// fields, so a partial rollback would be caught here and not by a fresh one.
	st, done := grocery.StateNotNeeded, true
	if _, err := s.Patch(beef.ID, grocery.PatchPayload{State: &st, Completed: &done}); err != nil {
		t.Fatalf("Patch item before blocking: %v", err)
	}
	block()

	name := "Chili Verde"
	enabled := true
	if _, _, err := s.PatchRecipe(r.ID, &name, &enabled); err == nil {
		t.Fatal("PatchRecipe should have failed once save() could not write")
	}

	got := s.Recipes()[0]
	if got.Name != "Chili" {
		t.Errorf("rename survived a failed save: got %q, want %q", got.Name, "Chili")
	}
	if got.Enabled {
		t.Error("enable survived a failed save")
	}
	after := s.List()[0]
	if after.State != grocery.StateNotNeeded {
		t.Errorf("item State survived a failed save: got %q, want %q", after.State, grocery.StateNotNeeded)
	}
	if !after.Completed {
		t.Error("item Completed was cleared by a failed save")
	}
}

func TestDeleteIngredient_RestoresItemWhenSaveFails(t *testing.T) {
	s, block := newBlockableStore(t)
	r, err := s.AddRecipe("Chili")
	if err != nil {
		t.Fatalf("AddRecipe before blocking: %v", err)
	}
	beef, _ := s.AddIngredient(r.ID, "beef")
	block()

	if err := s.DeleteIngredient(r.ID, beef.ID); err == nil {
		t.Fatal("DeleteIngredient should have failed once save() could not write")
	}
	items := s.List()
	if len(items) != 1 || items[0].ID != beef.ID {
		t.Errorf("ingredient not restored after the failed delete: %d items", len(items))
	}
}

// ── Recipe-ownership guards ─────────────────────────────────────────────────

func TestDelete_RefusesRecipeOwnedItem(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	beef, _ := s.AddIngredient(r.ID, "beef")
	free, _ := s.Add("milk", "Dairy")

	if err := s.Delete(beef.ID); !errors.Is(err, grocery.ErrRecipeOwned) {
		t.Errorf("deleting an owned item: got %v, want ErrRecipeOwned", err)
	}
	found := false
	for _, it := range s.List() {
		if it.ID == beef.ID {
			found = true
		}
	}
	if !found {
		t.Error("the owned item was deleted despite the refusal")
	}

	// Free items are unaffected.
	if err := s.Delete(free.ID); err != nil {
		t.Errorf("deleting a free item: %v", err)
	}
}

// PM-1: a stale client payload must not resurrect an ingredient the server has
// already deleted. Without the BulkSync guard the ghost comes back owned, and
// Delete then refuses to remove it — permanently undeletable.
func TestBulkSync_CannotResurrectDeletedIngredient(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	s.AddIngredient(r.ID, "beef")

	// The client's view, captured before the delete.
	stale := s.List()
	if len(stale) != 1 {
		t.Fatalf("setup: want 1 item, got %d", len(stale))
	}

	if _, err := s.DeleteRecipe(r.ID); err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	if len(s.List()) != 0 {
		t.Fatalf("setup: the cascade did not remove the ingredient")
	}

	after, err := s.BulkSync(stale)
	if err != nil {
		t.Fatalf("BulkSync: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("a stale bulk sync resurrected %d deleted ingredient(s): %v", len(after), after)
	}
	if len(s.List()) != 0 {
		t.Errorf("the ghost is in the store: %v", s.List())
	}
}

// AC-6.1 and AC-6.3 in one test: the server owns recipe_id, but the four
// client-owned fields still merge.
func TestBulkSync_IgnoresClientRecipeIDButStillMergesTheRest(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	beef, _ := s.AddIngredient(r.ID, "beef")
	free, _ := s.Add("milk", "Dairy")

	// A client that tried to re-parent one item and orphan another.
	incoming := []*grocery.Item{
		{ID: beef.ID, Name: "beef", Group: "Produce", State: grocery.StateCheck,
			Completed: true, Order: 7, RecipeID: ""},
		{ID: free.ID, Name: "milk", Group: "Frozen", State: grocery.StateNotNeeded,
			Completed: false, Order: 2, RecipeID: r.ID},
	}
	items, err := s.BulkSync(incoming)
	if err != nil {
		t.Fatalf("BulkSync: %v", err)
	}
	for _, it := range items {
		switch it.ID {
		case beef.ID:
			if it.RecipeID != r.ID {
				t.Errorf("client orphaned an owned item: recipe_id %q", it.RecipeID)
			}
			if it.Group != "Produce" || it.State != grocery.StateCheck || !it.Completed || it.Order != 7 {
				t.Errorf("owned item did not merge the client fields: %+v", it)
			}
		case free.ID:
			if it.RecipeID != "" {
				t.Errorf("client adopted a free item into a recipe: recipe_id %q", it.RecipeID)
			}
			if it.Group != "Frozen" || it.State != grocery.StateNotNeeded || it.Order != 2 {
				t.Errorf("free item did not merge the client fields: %+v", it)
			}
		}
	}
}

func TestBulkSync_RejectsNewItemCarryingARecipeID(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")

	incoming := []*grocery.Item{
		{ID: "ghost", Name: "beef", Group: grocery.NoGroup, State: grocery.StateNeeded, RecipeID: r.ID},
		{ID: "legit", Name: "milk", Group: "Dairy", State: grocery.StateNeeded},
	}
	items, err := s.BulkSync(incoming)
	if err != nil {
		t.Fatalf("BulkSync: %v", err)
	}
	if len(items) != 1 || items[0].ID != "legit" {
		t.Errorf("want only the free item to be created, got %v", items)
	}
}

func TestReset_DisablesEveryRecipeAndClearsOwnedItems(t *testing.T) {
	s, _ := newTempStore(t)
	a, _ := s.AddRecipe("Chili")
	b, _ := s.AddRecipe("Tacos")
	on := true
	s.PatchRecipe(a.ID, nil, &on)
	s.PatchRecipe(b.ID, nil, &on)
	beef, _ := s.AddIngredient(a.ID, "beef")
	done := true
	s.Patch(beef.ID, grocery.PatchPayload{Completed: &done})

	items, err := s.Reset()
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}
	for _, it := range items {
		if it.Completed {
			t.Errorf("%s still completed after reset", it.Name)
		}
		if it.State != grocery.StateCheck {
			t.Errorf("%s: state %v after reset, want check", it.Name, it.State)
		}
	}
	for _, r := range s.Recipes() {
		if r.Enabled {
			t.Errorf("recipe %q still enabled after reset", r.Name)
		}
	}
}

// ── Revision deltas ─────────────────────────────────────────────────────────
//
// One mutation is one save is one revision bump. These assert the exact delta,
// not merely that it increased.

func TestRevision_RecipeMutationsAdvanceByExactlyOne(t *testing.T) {
	s, _ := newTempStore(t)

	delta := func(name string, want int64, fn func()) {
		t.Helper()
		before := s.Revision()
		fn()
		if got := s.Revision() - before; got != want {
			t.Errorf("%s: revision delta %d, want %d", name, got, want)
		}
	}

	var r *grocery.Recipe
	delta("AddRecipe", 1, func() { r, _ = s.AddRecipe("Chili") })

	name := "Chili con carne"
	delta("PatchRecipe rename-only", 1, func() { s.PatchRecipe(r.ID, &name, nil) })

	on := true
	delta("PatchRecipe enable-only", 1, func() { s.PatchRecipe(r.ID, nil, &on) })

	delta("AddIngredient", 1, func() { s.AddIngredient(r.ID, "beef") })

	// The case a composed rename+toggle design fails: it would be 2.
	both, off := "Chilli", false
	delta("PatchRecipe with BOTH fields", 1, func() { s.PatchRecipe(r.ID, &both, &off) })

	// A no-op patch is not a mutation.
	delta("PatchRecipe with NEITHER field", 0, func() { s.PatchRecipe(r.ID, nil, nil) })

	delta("ReorderRecipes", 1, func() { s.ReorderRecipes([]string{r.ID}) })

	var doomed *grocery.Item
	delta("AddIngredient again", 1, func() { doomed, _ = s.AddIngredient(r.ID, "beans") })
	delta("DeleteIngredient", 1, func() { s.DeleteIngredient(r.ID, doomed.ID) })
}

// The PM-2a canary: deleting a recipe with three ingredients is ONE mutation.
// A delta of 3 means someone saved per removed item; a hang means someone
// called s.Delete under the already-held write lock.
func TestRevision_DeleteRecipeWithThreeIngredientsIsOneBump(t *testing.T) {
	s, _ := newTempStore(t)
	r, _ := s.AddRecipe("Chili")
	s.AddIngredient(r.ID, "beef")
	s.AddIngredient(r.ID, "beans")
	s.AddIngredient(r.ID, "tomatoes")
	if n := len(s.List()); n != 3 {
		t.Fatalf("setup: want 3 ingredients, got %d", n)
	}

	before := s.Revision()
	if _, err := s.DeleteRecipe(r.ID); err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	if got := s.Revision() - before; got != 1 {
		t.Errorf("DeleteRecipe revision delta %d, want 1", got)
	}
	if n := len(s.List()); n != 0 {
		t.Errorf("want 0 items after the cascade, got %d", n)
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

// TestSave_FileAndDirModes covers the temp-file+rename write path (FR-R4):
// the data directory created by save() must be 0750 and the final data file
// (after the tmp-file rename) must be 0600. Exact equality is not asserted
// because umask can only clear bits from the requested mode, never set them,
// so checking for absent other-permission bits (and absent group-write on the
// dir) is safe regardless of the test runner's umask.
func TestSave_FileAndDirModes(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "sub")
	path := filepath.Join(nested, "items.json")

	s, err := grocery.New(path)
	if err != nil {
		t.Fatalf("grocery.New: %v", err)
	}
	if _, err := s.Add("Milk", "Dairy"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	dirInfo, err := os.Stat(nested)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if dirMode := dirInfo.Mode().Perm(); dirMode&0o007 != 0 || dirMode&0o020 != 0 {
		t.Errorf("data dir mode = %o, want no other bits and no group-write", dirMode)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fileMode := fileInfo.Mode().Perm(); fileMode&0o077 != 0 {
		t.Errorf("data file mode = %o, want no group/other bits (owner-only)", fileMode)
	}

	// The tmp file must not survive the rename, but if it did, it too must
	// never be written with wider-than-owner permissions.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("tmp file should not exist after rename, stat err=%v", err)
	}
}
