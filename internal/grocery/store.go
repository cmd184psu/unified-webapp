package grocery

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// storeData is the on-disk JSON format.
type storeData struct {
	Title   string    `json:"title,omitempty"`
	Groups  []string  `json:"groups,omitempty"`
	Items   []*Item   `json:"items"`
	Recipes []*Recipe `json:"recipes,omitempty"`
}

// Store is a thread-safe, JSON-backed item/group store.
type Store struct {
	mu       sync.RWMutex
	items    map[string]*Item
	recipes  map[string]*Recipe
	groups   []string
	title    string
	filePath string
	revision int64 // atomically incremented on every save
}

// New creates (or loads) a Store backed by filePath.
func New(filePath string) (*Store, error) {
	s := &Store{items: make(map[string]*Item), recipes: make(map[string]*Recipe), filePath: filePath}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading store: %w", err)
	}
	return s, nil
}

// Title returns the current list title.
func (s *Store) Title() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.title
}

// SetTitle updates the list title and persists it.
func (s *Store) SetTitle(title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.title = title
	return s.save()
}

// Revision returns the current monotonic write counter.
func (s *Store) Revision() int64 {
	return atomic.LoadInt64(&s.revision)
}

// Groups returns the current named group list (excludes virtual NoGroup).
func (s *Store) Groups() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string{}, s.groups...)
}

// SaveGroups persists an updated group list and orphans items from removed groups.
func (s *Store) SaveGroups(groups []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	valid := make(map[string]bool, len(groups))
	for _, g := range groups {
		valid[g] = true
	}
	for _, item := range s.items {
		if item.Group != NoGroup && !valid[item.Group] {
			item.Group = NoGroup
		}
	}
	s.groups = append([]string{}, groups...)
	return s.save()
}

// List returns all items in stable sort order.
func (s *Store) List() []*Item {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedUnsafe()
}

// Add creates a new item and persists it.
func (s *Store) Add(name, group string) (*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	maxOrder := 0
	for _, item := range s.items {
		if item.Group == group && item.Order >= maxOrder {
			maxOrder = item.Order + 1
		}
	}
	item := &Item{
		ID:        s.nextIDUnsafe(),
		Name:      name,
		Group:     group,
		State:     StateNeeded,
		Completed: false,
		Order:     maxOrder,
		CreatedAt: time.Now(),
	}
	s.items[item.ID] = item
	if err := s.save(); err != nil {
		delete(s.items, item.ID)
		return nil, err
	}
	return item, nil
}

// PatchPayload carries optional fields for a partial item update.
type PatchPayload struct {
	State     *ItemState `json:"state,omitempty"`
	Completed *bool      `json:"completed,omitempty"`
}

// Patch applies a partial update to an item by ID.
func (s *Store) Patch(id string, p PatchPayload) (*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("item not found: %s", id)
	}
	if p.State != nil {
		item.State = *p.State
	}
	if p.Completed != nil {
		item.Completed = *p.Completed
	}
	return item, s.save()
}

// Delete removes an item by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return fmt.Errorf("item not found: %s", id)
	}
	// A recipe-owned item may only be removed through DeleteIngredient or by
	// deleting its recipe. Deleting it from the grocery side would silently
	// break the recipe.
	if item.RecipeID != "" {
		return ErrRecipeOwned
	}
	delete(s.items, id)
	return s.save()
}

// MovePayload carries the destination group and desired order of IDs.
type MovePayload struct {
	Group    string   `json:"group"`
	OrderIDs []string `json:"order_ids"`
}

// Move changes an item's group and re-orders the destination group.
func (s *Store) Move(id string, p MovePayload) (*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok {
		return nil, fmt.Errorf("item not found: %s", id)
	}
	item.Group = p.Group
	for i, oid := range p.OrderIDs {
		if it, ok := s.items[oid]; ok {
			it.Order = i
		}
	}
	return item, s.save()
}

// Reorder sets the Order field for each item in a group according to the provided IDs.
func (s *Store) Reorder(group string, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range ids {
		if item, ok := s.items[id]; ok && item.Group == group {
			item.Order = i
		}
	}
	return s.save()
}

// BulkSync merges an incoming slice of items (offline edits) into the store.
func (s *Store) BulkSync(incoming []*Item) ([]*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inc := range incoming {
		if existing, ok := s.items[inc.ID]; ok {
			// RecipeID is deliberately not copied: the server is authoritative
			// about ownership, so a stale or edited client payload cannot
			// re-parent an item. State/Completed/Group/Order still merge.
			existing.State = inc.State
			existing.Completed = inc.Completed
			existing.Group = inc.Group
			existing.Order = inc.Order
		} else {
			// A client must never create a recipe-owned item. Accepting one
			// here would resurrect an ingredient the server has already
			// deleted, and it would be undeletable from the grocery side.
			if inc.RecipeID != "" {
				log.Printf("grocery: bulksync rejected new item id=%s recipe_id=%s", inc.ID, inc.RecipeID)
				continue
			}
			s.items[inc.ID] = inc
		}
	}
	if err := s.save(); err != nil {
		return nil, err
	}
	return s.sortedUnsafe(), nil
}

// Reset sets every item to completed=false, state="check" and persists.
func (s *Store) Reset() ([]*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		item.Completed = false
		item.State = StateCheck
	}
	for _, r := range s.recipes {
		r.Enabled = false
	}
	if err := s.save(); err != nil {
		return nil, err
	}
	return s.sortedUnsafe(), nil
}

// ── Recipes ─────────────────────────────────────────────────────────────────
//
// Every method in this section holds exactly one s.mu.Lock(), calls s.save()
// exactly once on the success path, and calls no other locking Store method.
// sync.RWMutex is not reentrant, so delegating to Delete, Patch or List from
// inside one of these deadlocks; and a per-item save() would advance Revision()
// more than once per mutation, which AC-1.5 forbids.

// AddRecipe creates a new, disabled recipe and persists it.
func (s *Store) AddRecipe(name string) (*Recipe, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	for _, r := range s.recipes {
		if strings.EqualFold(r.Name, name) {
			return nil, ErrDuplicateRecipe
		}
	}
	// max(Order)+1, not len(s.recipes): deleting a middle recipe would make
	// len() collide with an existing Order and let the sort tie-break decide
	// display order silently.
	maxOrder := 0
	for _, r := range s.recipes {
		if r.Order >= maxOrder {
			maxOrder = r.Order + 1
		}
	}
	recipe := &Recipe{
		ID:        s.nextIDUnsafe(),
		Name:      name,
		Enabled:   false,
		Order:     maxOrder,
		CreatedAt: time.Now(),
	}
	s.recipes[recipe.ID] = recipe
	if err := s.save(); err != nil {
		delete(s.recipes, recipe.ID)
		return nil, err
	}
	return recipe, nil
}

// PatchRecipe applies an optional rename and/or an optional enable toggle in a
// single locked mutation, and returns the recipe with the full item list.
//
// It is deliberately one method rather than a rename call composed with a
// toggle call: two calls would mean two saves, two revision bumps and two SSE
// notifications for the one body shape the API explicitly permits.
//
// A patch carrying neither field is not a mutation. It still checks existence
// and still returns {recipe, items}, but it does not save and does not advance
// the revision.
func (s *Store) PatchRecipe(id string, name *string, enabled *bool) (*Recipe, []*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.recipes[id]
	if !ok {
		return nil, nil, ErrRecipeNotFound
	}
	if name == nil && enabled == nil {
		return r, s.sortedUnsafe(), nil
	}

	// Validate everything before writing anything. Renaming first and then
	// discovering a duplicate would leave r.Name, r.Enabled and every owned
	// item's State mutated in memory with no save() and no revision bump — the
	// store would disagree with disk and nothing would signal it.
	var newName string
	if name != nil {
		newName = strings.TrimSpace(*name)
		if newName == "" {
			return nil, nil, ErrInvalidName
		}
		for _, other := range s.recipes {
			if other.ID != id && strings.EqualFold(other.Name, newName) {
				return nil, nil, ErrDuplicateRecipe
			}
		}
	}

	// Snapshot before writing, and restore under the same still-held lock if
	// save() fails. Same rationale as DeleteRecipe, and a toggle is the worse
	// case: a failed patch leaves a rename AND every owned item's State and
	// Completed rewritten in memory with nothing on disk, so the next unrelated
	// successful save() — a title edit, another recipe's toggle — silently
	// commits changes the user was told had failed.
	type prevItem struct {
		state     ItemState
		completed bool
	}
	oldName, oldEnabled := r.Name, r.Enabled
	var touched map[string]prevItem

	if name != nil {
		r.Name = newName
	}
	if enabled != nil {
		state := StateNotNeeded
		if *enabled {
			state = StateNeeded
		}
		touched = make(map[string]prevItem)
		// item.RecipeID == id is the whole guard: items owned by other recipes
		// and free items are untouched by construction.
		for _, item := range s.items {
			if item.RecipeID == id {
				touched[item.ID] = prevItem{item.State, item.Completed}
				item.State = state
				item.Completed = false
			}
		}
		r.Enabled = *enabled
	}
	if err := s.save(); err != nil {
		r.Name, r.Enabled = oldName, oldEnabled
		for itemID, prev := range touched {
			if item, ok := s.items[itemID]; ok {
				item.State, item.Completed = prev.state, prev.completed
			}
		}
		return nil, nil, err
	}
	return r, s.sortedUnsafe(), nil
}

// ReorderRecipes assigns Order by position, ignoring unknown ids.
func (s *Store) ReorderRecipes(ids []string) ([]*Recipe, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range ids {
		if r, ok := s.recipes[id]; ok {
			r.Order = i
		}
	}
	if err := s.save(); err != nil {
		return nil, err
	}
	return s.sortedRecipesUnsafe(), nil
}

// DeleteRecipe removes a recipe and every item it owns, and returns the
// surviving items.
//
// The deletes are inline on purpose. Calling s.Delete here would deadlock —
// sync.RWMutex is not reentrant and the write lock is already held — and saving
// once per removed item would advance Revision() once per ingredient instead of
// once per mutation, which AC-1.5 forbids.
//
// On save failure the snapshot is restored under the same still-held lock:
// unlike a failed toggle, a failed delete is not something the user can simply
// redo, because the curated rows are already gone from memory and the next
// unrelated successful save() would commit their absence.
func (s *Store) DeleteRecipe(id string) ([]*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recipe, ok := s.recipes[id]
	if !ok {
		return nil, ErrRecipeNotFound
	}
	owned := make([]*Item, 0)
	for _, item := range s.items {
		if item.RecipeID == id {
			owned = append(owned, item)
		}
	}
	for _, item := range owned {
		delete(s.items, item.ID)
	}
	delete(s.recipes, id)
	if err := s.save(); err != nil {
		for _, item := range owned {
			s.items[item.ID] = item
		}
		s.recipes[id] = recipe
		return nil, err
	}
	// Logged after the save so a rolled-back delete is not reported as one.
	log.Printf("grocery: recipe delete id=%s name=%q items_deleted=%d", id, recipe.Name, len(owned))
	return s.sortedUnsafe(), nil
}

// AddIngredient creates an item owned by recipeID, in the virtual NoGroup
// bucket. Duplicate ingredient names are legal, within and across recipes.
func (s *Store) AddIngredient(recipeID, name string) (*Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	recipe, ok := s.recipes[recipeID]
	if !ok {
		return nil, ErrRecipeNotFound
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidName
	}
	state := StateNotNeeded
	if recipe.Enabled {
		state = StateNeeded
	}
	// Same max-order idiom as Add, scoped to NoGroup. It looks
	// iteration-order-dependent under random map ranging but is equivalent to
	// max(Order)+1.
	maxOrder := 0
	for _, item := range s.items {
		if item.Group == NoGroup && item.Order >= maxOrder {
			maxOrder = item.Order + 1
		}
	}
	item := &Item{
		ID:        s.nextIDUnsafe(),
		Name:      name,
		Group:     NoGroup,
		State:     state,
		Completed: false,
		Order:     maxOrder,
		CreatedAt: time.Now(),
		RecipeID:  recipeID,
	}
	s.items[item.ID] = item
	if err := s.save(); err != nil {
		delete(s.items, item.ID)
		return nil, err
	}
	return item, nil
}

// DeleteIngredient removes one item from one recipe.
//
// Do NOT delegate to s.Delete. Besides the reentrancy deadlock, Delete refuses
// recipe-owned items outright — exactly the items this method exists to remove
// — so delegating would fail silently in the shape of a tidy refactor.
//
// A mismatched recipe/item pair is reported as a missing recipe, not as a
// conflict: from the caller's point of view that item is not in that recipe.
func (s *Store) DeleteIngredient(recipeID, itemID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[itemID]
	if !ok || item.RecipeID != recipeID {
		return ErrRecipeNotFound
	}
	delete(s.items, itemID)
	// Restored on save failure for DeleteRecipe's reason: the row is already
	// gone from memory, the user cannot redo a delete they were told failed,
	// and the next unrelated successful save() would commit its absence.
	if err := s.save(); err != nil {
		s.items[itemID] = item
		return err
	}
	return nil
}

// sortedUnsafe must be called with the lock already held.
func (s *Store) sortedUnsafe() []*Item {
	items := make([]*Item, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Group != items[j].Group {
			return items[i].Group < items[j].Group
		}
		if items[i].Order != items[j].Order {
			return items[i].Order < items[j].Order
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

// sortedRecipesUnsafe must be called with the lock already held. Never nil.
func (s *Store) sortedRecipesUnsafe() []*Recipe {
	recipes := make([]*Recipe, 0, len(s.recipes))
	for _, r := range s.recipes {
		recipes = append(recipes, r)
	}
	sort.Slice(recipes, func(i, j int) bool {
		if recipes[i].Order != recipes[j].Order {
			return recipes[i].Order < recipes[j].Order
		}
		return recipes[i].CreatedAt.Before(recipes[j].CreatedAt)
	})
	return recipes
}

// Recipes returns all recipes in stable sort order. Never nil.
func (s *Store) Recipes() []*Recipe {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sortedRecipesUnsafe()
}

// nextIDUnsafe must be called with the write lock held. Items and recipes share
// one ID namespace; this is the only place that fact is enforced.
func (s *Store) nextIDUnsafe() string {
	n := time.Now().UnixNano()
	for {
		id := strconv.FormatInt(n, 10)
		if _, ok := s.items[id]; !ok {
			if _, ok := s.recipes[id]; !ok {
				return id
			}
		}
		n++
	}
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	// Support legacy plain-array format.
	if len(raw) > 0 && raw[0] == '[' {
		var items []*Item
		if err := json.Unmarshal(raw, &items); err != nil {
			return err
		}
		for _, item := range items {
			s.items[item.ID] = item
		}
		return nil
	}
	var sd storeData
	if err := json.Unmarshal(raw, &sd); err != nil {
		return err
	}
	for _, item := range sd.Items {
		s.items[item.ID] = item
	}
	for _, r := range sd.Recipes {
		s.recipes[r.ID] = r
	}
	s.groups = sd.Groups
	s.title = sd.Title
	return nil
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0755); err != nil {
		return err
	}
	sd := storeData{
		Title:   s.title,
		Groups:  s.groups,
		Items:   s.sortedUnsafe(),
		Recipes: s.sortedRecipesUnsafe(),
	}
	data, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.filePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		return err
	}
	atomic.AddInt64(&s.revision, 1)
	return nil
}
