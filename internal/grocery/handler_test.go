package grocery_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cmd184psu/unified-webapp/internal/grocery"
	"cmd184psu/unified-webapp/internal/platform/broker"
)

// ── test harness ─────────────────────────────────────────────────────────────

type harness struct {
	s      *grocery.Store
	h      *grocery.Handler
	broker *broker.Broker
	mux    *http.ServeMux
}

func newHarness(t *testing.T, groups []string) *harness {
	t.Helper()
	dir := t.TempDir()
	s, err := grocery.New(filepath.Join(dir, "items.json"))
	if err != nil {
		t.Fatalf("grocery.New: %v", err)
	}
	if len(groups) > 0 {
		s.SaveGroups(groups)
	}
	b := broker.NewBroker(0)
	h := grocery.NewHandler(s, groups, false, 1, "Grocery List", b)
	mux := http.NewServeMux()
	h.Register(mux)
	return &harness{s: s, h: h, broker: b, mux: mux}
}

func (hh *harness) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	hh.mux.ServeHTTP(w, req)
	return w
}

func decodeJSON[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decodeJSON: %v (body: %s)", err, w.Body.String())
	}
	return out
}

// notifyC subscribes to the broker, fires f, and asserts a signal arrives within 200ms.
func notifyC(t *testing.T, b *broker.Broker, label string, f func()) {
	t.Helper()
	ch := make(chan struct{}, 1)
	b.Subscribe(ch)
	defer b.Unsubscribe(ch)
	f()
	select {
	case <-ch:
	case <-time.After(200 * time.Millisecond):
		t.Errorf("%s: broker was not notified within 200ms", label)
	}
}

// ── /api/items ────────────────────────────────────────────────────────────────

func TestHandlerGetItems_Empty(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodGet, "/api/items", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var items []grocery.Item
	json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 0 {
		t.Errorf("want empty list, got %d items", len(items))
	}
}

func TestHandlerPostItem_Created(t *testing.T) {
	hh := newHarness(t, []string{"Dairy"})
	w := hh.do(t, http.MethodPost, "/api/items", map[string]string{"name": "Milk", "group": "Dairy"})
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d — %s", w.Code, w.Body.String())
	}
	item := decodeJSON[grocery.Item](t, w)
	if item.Name != "Milk" {
		t.Errorf("got name %q, want Milk", item.Name)
	}
	if item.State != grocery.StateNeeded {
		t.Errorf("got state %q, want needed", item.State)
	}
}

func TestHandlerPostItem_MissingName(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodPost, "/api/items", map[string]string{"name": ""})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

func TestHandlerPatchItem_StateChange(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	createW := hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Carrot", "group": "Produce"})
	item := decodeJSON[grocery.Item](t, createW)

	w := hh.do(t, http.MethodPatch, "/api/items/"+item.ID,
		map[string]string{"state": "check"})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	updated := decodeJSON[grocery.Item](t, w)
	if updated.State != grocery.StateCheck {
		t.Errorf("got %q, want check", updated.State)
	}
}

func TestHandlerDeleteItem(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	createW := hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Kale", "group": "Produce"})
	item := decodeJSON[grocery.Item](t, createW)

	w := hh.do(t, http.MethodDelete, "/api/items/"+item.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
	listW := hh.do(t, http.MethodGet, "/api/items", nil)
	var items []grocery.Item
	json.NewDecoder(listW.Body).Decode(&items)
	if len(items) != 0 {
		t.Errorf("want 0 items after delete, got %d", len(items))
	}
}

// ── /api/reset ────────────────────────────────────────────────────────────────

func TestHandlerReset(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Tomato", "group": "Produce"})

	w := hh.do(t, http.MethodPost, "/api/reset", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var items []grocery.Item
	json.NewDecoder(w.Body).Decode(&items)
	for _, it := range items {
		if it.Completed {
			t.Errorf("item %s still completed after reset", it.ID)
		}
		if it.State != grocery.StateCheck {
			t.Errorf("item %s state %q after reset, want check", it.ID, it.State)
		}
	}
}

func TestHandlerReset_WrongMethod(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodGet, "/api/reset", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// ── /api/config/groups ────────────────────────────────────────────────────────

func TestHandlerGroupsAdd(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodPost, "/api/config/groups",
		map[string]string{"name": "Frozen"})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	groups := resp["groups"].([]any)
	if len(groups) != 1 || groups[0] != "Frozen" {
		t.Errorf("unexpected groups: %v", groups)
	}
}

func TestHandlerGroupsAdd_Idempotent(t *testing.T) {
	hh := newHarness(t, []string{"Frozen"})
	w := hh.do(t, http.MethodPost, "/api/config/groups",
		map[string]string{"name": "Frozen"})
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	groups := resp["groups"].([]any)
	if len(groups) != 1 {
		t.Errorf("want 1 group (idempotent), got %d", len(groups))
	}
}

func TestHandlerGroupsAdd_ReservedNoGroup(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodPost, "/api/config/groups",
		map[string]string{"name": grocery.NoGroup})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for reserved name, got %d", w.Code)
	}
}

func TestHandlerGroupsRemove_OrphansItems(t *testing.T) {
	hh := newHarness(t, []string{"Produce", "Dairy"})
	hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Spinach", "group": "Produce"})

	w := hh.do(t, http.MethodPost, "/api/config/groups/remove",
		map[string]string{"name": "Produce"})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — %s", w.Code, w.Body.String())
	}
	var resp struct {
		Groups []string       `json:"groups"`
		Items  []grocery.Item `json:"items"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Groups) != 1 || resp.Groups[0] != "Dairy" {
		t.Errorf("unexpected groups after remove: %v", resp.Groups)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("want 1 item in response, got %d", len(resp.Items))
	}
	if resp.Items[0].Group != grocery.NoGroup {
		t.Errorf("item group: got %q, want %q", resp.Items[0].Group, grocery.NoGroup)
	}
}

func TestHandlerGroupsRemove_WrongMethod(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodDelete, "/api/config/groups/remove", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// ── /api/config ──────────────────────────────────────────────────────────────

func TestHandlerGetConfig(t *testing.T) {
	hh := newHarness(t, []string{"Bakery"})
	w := hh.do(t, http.MethodGet, "/api/config", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	groups, ok := resp["groups"].([]any)
	if !ok || len(groups) != 1 || groups[0] != "Bakery" {
		t.Errorf("unexpected config: %v", resp)
	}
}

func TestHandlerGetConfig_SyncInterval(t *testing.T) {
	dir := t.TempDir()
	s, err := grocery.New(filepath.Join(dir, "items.json"))
	if err != nil {
		t.Fatalf("grocery.New: %v", err)
	}
	const wantInterval = 7
	b := broker.NewBroker(0)
	h := grocery.NewHandler(s, nil, false, wantInterval, "Grocery List", b)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got, ok := resp["sync_interval_seconds"]
	if !ok {
		t.Fatal("response missing sync_interval_seconds")
	}
	if int(got.(float64)) != wantInterval {
		t.Errorf("sync_interval_seconds: got %v, want %d", got, wantInterval)
	}
}

func TestHandlerGetConfig_TitleDefault(t *testing.T) {
	dir := t.TempDir()
	s, _ := grocery.New(filepath.Join(dir, "items.json"))
	b := broker.NewBroker(0)
	h := grocery.NewHandler(s, nil, false, 1, "My Market Run", b)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["title"] != "My Market Run" {
		t.Errorf("title: got %v, want %q", resp["title"], "My Market Run")
	}
}

func TestHandlerConfigTitle_SetAndRead(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodPost, "/api/config/title",
		map[string]string{"name": "Corner Store Run"})
	if w.Code != http.StatusOK {
		t.Fatalf("POST title: want 200, got %d — %s", w.Code, w.Body.String())
	}
	var post map[string]string
	json.NewDecoder(w.Body).Decode(&post)
	if post["title"] != "Corner Store Run" {
		t.Errorf("POST response title: got %q, want %q", post["title"], "Corner Store Run")
	}

	cfgW := hh.do(t, http.MethodGet, "/api/config", nil)
	var cfgResp map[string]any
	json.NewDecoder(cfgW.Body).Decode(&cfgResp)
	if cfgResp["title"] != "Corner Store Run" {
		t.Errorf("GET /api/config title after update: got %v, want %q", cfgResp["title"], "Corner Store Run")
	}
}

func TestHandlerConfigTitle_EmptyNameRejected(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodPost, "/api/config/title",
		map[string]string{"name": ""})
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for empty title, got %d", w.Code)
	}
}

func TestHandlerConfigTitle_WrongMethod(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodGet, "/api/config/title", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// ── /api/revision ─────────────────────────────────────────────────────────────

func TestHandlerRevision_InitialIsZero(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodGet, "/api/revision", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp map[string]int64
	json.NewDecoder(w.Body).Decode(&resp)
	if _, ok := resp["revision"]; !ok {
		t.Fatal("response missing revision key")
	}
}

func TestHandlerRevision_IncrementsAfterMutation(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	revNow := func() int64 {
		w := hh.do(t, http.MethodGet, "/api/revision", nil)
		var resp map[string]int64
		json.NewDecoder(w.Body).Decode(&resp)
		return resp["revision"]
	}
	r0 := revNow()
	hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Lettuce", "group": "Produce"})
	r1 := revNow()
	if r1 <= r0 {
		t.Errorf("revision should increase after POST /api/items: before=%d after=%d", r0, r1)
	}
}

func TestHandlerRevision_WrongMethod(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodPost, "/api/revision", nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

// ── broker notification ───────────────────────────────────────────────────────

func TestBroker_NotifiedOnConfigTitle(t *testing.T) {
	hh := newHarness(t, nil)
	notifyC(t, hh.broker, "POST /api/config/title", func() {
		hh.do(t, http.MethodPost, "/api/config/title",
			map[string]string{"name": "Farmers Market"})
	})
}

func TestBroker_NotifiedOnPostItem(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	notifyC(t, hh.broker, "POST /api/items", func() {
		hh.do(t, http.MethodPost, "/api/items",
			map[string]string{"name": "Tomato", "group": "Produce"})
	})
}

func TestBroker_NotifiedOnPatchItem(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	createW := hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Kale", "group": "Produce"})
	item := decodeJSON[grocery.Item](t, createW)
	notifyC(t, hh.broker, "PATCH /api/items/:id", func() {
		hh.do(t, http.MethodPatch, "/api/items/"+item.ID,
			map[string]string{"state": "check"})
	})
}

func TestBroker_NotifiedOnDeleteItem(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	createW := hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Spinach", "group": "Produce"})
	item := decodeJSON[grocery.Item](t, createW)
	notifyC(t, hh.broker, "DELETE /api/items/:id", func() {
		hh.do(t, http.MethodDelete, "/api/items/"+item.ID, nil)
	})
}

func TestBroker_NotifiedOnReset(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Apple", "group": "Produce"})
	notifyC(t, hh.broker, "POST /api/reset", func() {
		hh.do(t, http.MethodPost, "/api/reset", nil)
	})
}

func TestBroker_NotifiedOnGroupsAdd(t *testing.T) {
	hh := newHarness(t, nil)
	notifyC(t, hh.broker, "POST /api/config/groups", func() {
		hh.do(t, http.MethodPost, "/api/config/groups",
			map[string]string{"name": "Bakery"})
	})
}

func TestBroker_NotifiedOnGroupsRemove(t *testing.T) {
	hh := newHarness(t, []string{"Deli"})
	notifyC(t, hh.broker, "POST /api/config/groups/remove", func() {
		hh.do(t, http.MethodPost, "/api/config/groups/remove",
			map[string]string{"name": "Deli"})
	})
}

func TestBroker_NotifiedOnGroupsReorder(t *testing.T) {
	hh := newHarness(t, []string{"A", "B"})
	notifyC(t, hh.broker, "POST /api/config/groups/reorder", func() {
		hh.do(t, http.MethodPost, "/api/config/groups/reorder",
			map[string]any{"groups": []string{"B", "A"}})
	})
}

func TestBroker_NotifiedOnSync(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	notifyC(t, hh.broker, "POST /api/sync", func() {
		hh.do(t, http.MethodPost, "/api/sync", []map[string]any{})
	})
}

func TestBroker_NotifiedOnReorder(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	a, _ := hh.s.Add("A", "Produce")
	b, _ := hh.s.Add("B", "Produce")
	notifyC(t, hh.broker, "POST /api/reorder", func() {
		hh.do(t, http.MethodPost, "/api/reorder",
			map[string]any{"group": "Produce", "ids": []string{b.ID, a.ID}})
	})
}

func TestBroker_NotifiedOnMove(t *testing.T) {
	hh := newHarness(t, []string{"Produce", "Frozen"})
	createW := hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Peas", "group": "Produce"})
	item := decodeJSON[grocery.Item](t, createW)
	notifyC(t, hh.broker, "POST /api/move", func() {
		hh.do(t, http.MethodPost, "/api/move",
			map[string]any{"id": item.ID, "group": "Frozen", "order_ids": []string{item.ID}})
	})
}

// ── /api/events (SSE) ────────────────────────────────────────────────────────

func TestSSE_RetryDirectiveSent(t *testing.T) {
	dir := t.TempDir()
	s, _ := grocery.New(filepath.Join(dir, "items.json"))
	const retryMs = 5000
	b := broker.NewBroker(retryMs)
	h := grocery.NewHandler(s, nil, false, 5, "Grocery List", b)
	mux := http.NewServeMux()
	h.Register(mux)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		mux.ServeHTTP(rw, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	body := rw.Body.String()
	want := fmt.Sprintf("retry: %d", retryMs)
	if !strings.Contains(body, want) {
		t.Errorf("SSE stream missing %q; got: %q", want, body)
	}
}

func TestSSE_ConnectedCommentSentOnOpen(t *testing.T) {
	hh := newHarness(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		hh.mux.ServeHTTP(rw, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(rw.Body.String(), ": connected") {
		t.Errorf("SSE stream missing ': connected' preamble; got: %q", rw.Body.String())
	}
}

func TestSSE_DataEventDelivered(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		hh.mux.ServeHTTP(rw, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	hh.do(t, http.MethodPost, "/api/items",
		map[string]string{"name": "Broccoli", "group": "Produce"})
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(rw.Body.String(), "refresh") {
		t.Errorf("SSE stream missing 'refresh' event after mutation; got: %q", rw.Body.String())
	}
}

// ── /api/recipes ──────────────────────────────────────────────────────────────

// notifyCountC subscribes with a deep buffer, fires f, and asserts the exact
// number of Notify signals. Notify() is a non-blocking send per subscriber and
// the handlers call it synchronously before returning, so once f() has returned
// len(ch) is settled and does not need a timeout to observe.
//
// The existing notifyC answers "at least one"; the recipe routes need "exactly
// one" (a patch must not double-broadcast) and "exactly zero" (a patch that
// wrote nothing must not broadcast at all).
func notifyCountC(t *testing.T, b *broker.Broker, label string, want int, f func()) {
	t.Helper()
	ch := make(chan struct{}, 8)
	b.Subscribe(ch)
	defer b.Unsubscribe(ch)
	f()
	if got := len(ch); got != want {
		t.Errorf("%s: got %d Notify signals, want %d", label, got, want)
	}
}

// patchResp is the PATCH /api/recipes/:id envelope: the recipe plus the whole
// item list, because enabling a recipe rewrites its ingredients' states.
type patchResp struct {
	Recipe grocery.Recipe `json:"recipe"`
	Items  []grocery.Item `json:"items"`
}

func mkRecipe(t *testing.T, hh *harness, name string) grocery.Recipe {
	t.Helper()
	w := hh.do(t, http.MethodPost, "/api/recipes", map[string]string{"name": name})
	if w.Code != http.StatusCreated {
		t.Fatalf("create recipe %q: want 201, got %d (%s)", name, w.Code, w.Body.String())
	}
	return decodeJSON[grocery.Recipe](t, w)
}

func mkIngredient(t *testing.T, hh *harness, recipeID, name string) grocery.Item {
	t.Helper()
	w := hh.do(t, http.MethodPost, "/api/recipes/"+recipeID+"/ingredients",
		map[string]string{"name": name})
	if w.Code != http.StatusCreated {
		t.Fatalf("add ingredient %q: want 201, got %d (%s)", name, w.Code, w.Body.String())
	}
	return decodeJSON[grocery.Item](t, w)
}

func TestHandlerRecipes_GetEmptyIsArrayNotNull(t *testing.T) {
	hh := newHarness(t, nil)
	w := hh.do(t, http.MethodGet, "/api/recipes", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	// A nil slice marshals to "null", which the client's .map() would choke on.
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("empty recipe list serialized as %q, want %q", got, "[]")
	}
}

func TestHandlerRecipes_PostCreatesDisabledRecipe(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "  Chili  ")
	if r.Name != "Chili" {
		t.Errorf("name %q, want %q (handler must trim)", r.Name, "Chili")
	}
	if r.Enabled {
		t.Error("new recipe is enabled; recipes must start disabled")
	}
	if r.ID == "" {
		t.Error("new recipe has no id")
	}

	list := decodeJSON[[]grocery.Recipe](t, hh.do(t, http.MethodGet, "/api/recipes", nil))
	if len(list) != 1 || list[0].ID != r.ID {
		t.Errorf("GET /api/recipes did not return the created recipe: %+v", list)
	}
}

// A malformed name is 400; a well-formed name that collides is 409. The two
// sentinels exist to be told apart, so the test asserts them apart.
func TestHandlerRecipes_PostRejectsBlankAndDuplicateNames(t *testing.T) {
	hh := newHarness(t, nil)
	mkRecipe(t, hh, "Chili")

	cases := []struct {
		label string
		body  any
		want  int
	}{
		{"empty name", map[string]string{"name": ""}, http.StatusBadRequest},
		{"whitespace name", map[string]string{"name": "   "}, http.StatusBadRequest},
		{"duplicate name", map[string]string{"name": "Chili"}, http.StatusConflict},
		{"case-insensitive duplicate", map[string]string{"name": "cHiLi"}, http.StatusConflict},
	}
	for _, c := range cases {
		w := hh.do(t, http.MethodPost, "/api/recipes", c.body)
		if w.Code != c.want {
			t.Errorf("%s: want %d, got %d (%s)", c.label, c.want, w.Code, w.Body.String())
		}
	}
}

func TestHandlerRecipes_WrongMethodIs405(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	item := mkIngredient(t, hh, r.ID, "Beef")

	cases := []struct{ method, path string }{
		{http.MethodPut, "/api/recipes"},
		{http.MethodDelete, "/api/recipes"},
		{http.MethodGet, "/api/recipes/reorder"},
		{http.MethodGet, "/api/recipes/" + r.ID},
		{http.MethodPost, "/api/recipes/" + r.ID},
		{http.MethodGet, "/api/recipes/" + r.ID + "/ingredients"},
		{http.MethodGet, "/api/recipes/" + r.ID + "/ingredients/" + item.ID},
	}
	for _, c := range cases {
		w := hh.do(t, c.method, c.path, nil)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: want 405, got %d", c.method, c.path, w.Code)
		}
	}
}

func TestHandlerRecipes_UnknownShapeIs404(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")

	cases := []struct{ method, path string }{
		// A bare trailing slash trims to an empty id; the store answers "recipe
		// not found", and the handler is deliberately not second-guessing it
		// with a 400.
		{http.MethodDelete, "/api/recipes/"},
		{http.MethodPatch, "/api/recipes/"},
		{http.MethodPost, "/api/recipes/" + r.ID + "/steps"},
		{http.MethodDelete, "/api/recipes/" + r.ID + "/ingredients/x/y"},
	}
	for _, c := range cases {
		w := hh.do(t, c.method, c.path, map[string]string{"name": "x"})
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404, got %d", c.method, c.path, w.Code)
		}
	}
}

func TestHandlerRecipes_PatchRenameReturnsRecipeAndItems(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	mkIngredient(t, hh, r.ID, "Beef")

	w := hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID, map[string]any{"name": "Chili Verde"})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	resp := decodeJSON[patchResp](t, w)
	if resp.Recipe.Name != "Chili Verde" {
		t.Errorf("recipe name %q, want %q", resp.Recipe.Name, "Chili Verde")
	}
	// A rename does not touch item state, but the items array still ships so
	// the client has one response shape to handle for every patch.
	if len(resp.Items) != 1 {
		t.Fatalf("rename response carried %d items, want 1", len(resp.Items))
	}
	if resp.Items[0].State != grocery.StateNotNeeded {
		t.Errorf("rename changed item state to %q; renames must not touch state", resp.Items[0].State)
	}
}

func TestHandlerRecipes_PatchEnabledFlipsIngredientStates(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	beef := mkIngredient(t, hh, r.ID, "Beef")
	if beef.State != grocery.StateNotNeeded {
		t.Fatalf("ingredient of a disabled recipe starts as %q, want %q", beef.State, grocery.StateNotNeeded)
	}

	on := decodeJSON[patchResp](t, hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID,
		map[string]any{"enabled": true}))
	if !on.Recipe.Enabled {
		t.Error("recipe not enabled after PATCH enabled:true")
	}
	if on.Items[0].State != grocery.StateNeeded {
		t.Errorf("after enable, item state %q, want %q", on.Items[0].State, grocery.StateNeeded)
	}

	off := decodeJSON[patchResp](t, hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID,
		map[string]any{"enabled": false}))
	if off.Items[0].State != grocery.StateNotNeeded {
		t.Errorf("after disable, item state %q, want %q", off.Items[0].State, grocery.StateNotNeeded)
	}
}

func TestHandlerRecipes_PatchBadBodyIs400(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")

	// Both fields are pointers, so a decode failure is indistinguishable from a
	// no-op patch unless the handler checks the error. Without that check this
	// would answer 200 and tell the client a write it never made succeeded.
	w := hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID, map[string]any{"enabled": "yes"})
	if w.Code != http.StatusBadRequest {
		t.Errorf("PATCH with a non-boolean enabled: want 400, got %d (%s)", w.Code, w.Body.String())
	}

	after := decodeJSON[[]grocery.Recipe](t, hh.do(t, http.MethodGet, "/api/recipes", nil))
	if after[0].Enabled {
		t.Error("rejected patch still enabled the recipe")
	}
}

func TestHandlerRecipes_PatchUnknownIs404AndDuplicateIs409(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	mkRecipe(t, hh, "Tacos")

	w := hh.do(t, http.MethodPatch, "/api/recipes/nope", map[string]any{"name": "X"})
	if w.Code != http.StatusNotFound {
		t.Errorf("patch unknown recipe: want 404, got %d", w.Code)
	}

	// Renaming INTO an existing name is the true conflict case: the request is
	// valid, the target name is taken.
	w = hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID, map[string]any{"name": "Tacos"})
	if w.Code != http.StatusConflict {
		t.Errorf("patch to a duplicate name: want 409, got %d", w.Code)
	}

	// Renaming a recipe to its own current name is not a duplicate.
	w = hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID, map[string]any{"name": "Chili"})
	if w.Code != http.StatusOK {
		t.Errorf("self-rename: want 200, got %d (%s)", w.Code, w.Body.String())
	}
}

func TestHandlerRecipes_PatchWithNeitherFieldIs200AndSilent(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	mkIngredient(t, hh, r.ID, "Beef")

	var w *httptest.ResponseRecorder
	notifyCountC(t, hh.broker, "PATCH /api/recipes/:id with neither field", 0, func() {
		w = hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID, map[string]any{})
	})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	// The response shape must not degrade just because nothing changed.
	body := decodeJSON[map[string]json.RawMessage](t, w)
	if _, ok := body["recipe"]; !ok {
		t.Error(`no-op patch response is missing the "recipe" key`)
	}
	if _, ok := body["items"]; !ok {
		t.Error(`no-op patch response is missing the "items" key`)
	}
}

func TestHandlerRecipes_DeleteReturnsItemsWithoutRecipeKey(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	hh.do(t, http.MethodPost, "/api/items", map[string]string{"name": "Milk", "group": "Produce"})
	r := mkRecipe(t, hh, "Chili")
	mkIngredient(t, hh, r.ID, "Beef")
	mkIngredient(t, hh, r.ID, "Beans")

	w := hh.do(t, http.MethodDelete, "/api/recipes/"+r.ID, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	body := decodeJSON[map[string]json.RawMessage](t, w)
	// The recipe is gone, so there is nothing to put under a "recipe" key;
	// shipping one would invite the client to render a tombstone.
	if _, ok := body["recipe"]; ok {
		t.Error(`delete response carries a "recipe" key; it should carry only "items"`)
	}
	var items []grocery.Item
	if err := json.Unmarshal(body["items"], &items); err != nil {
		t.Fatalf("items: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Milk" {
		t.Errorf("after deleting the recipe the list should hold only the free item; got %+v", items)
	}

	if w := hh.do(t, http.MethodDelete, "/api/recipes/"+r.ID, nil); w.Code != http.StatusNotFound {
		t.Errorf("deleting an already-deleted recipe: want 404, got %d", w.Code)
	}
}

func TestHandlerRecipes_PostIngredientLandsUnallocatedAndOwned(t *testing.T) {
	hh := newHarness(t, []string{"Produce"})
	r := mkRecipe(t, hh, "Chili")

	item := mkIngredient(t, hh, r.ID, "  Beef  ")
	if item.Name != "Beef" {
		t.Errorf("ingredient name %q, want %q", item.Name, "Beef")
	}
	if item.Group != grocery.NoGroup {
		t.Errorf("ingredient group %q, want %q", item.Group, grocery.NoGroup)
	}
	if item.RecipeID != r.ID {
		t.Errorf("ingredient recipe_id %q, want %q", item.RecipeID, r.ID)
	}

	for _, c := range []struct {
		label string
		path  string
		body  any
		want  int
	}{
		{"blank name", "/api/recipes/" + r.ID + "/ingredients", map[string]string{"name": "  "}, http.StatusBadRequest},
		{"unknown recipe", "/api/recipes/nope/ingredients", map[string]string{"name": "Beef"}, http.StatusNotFound},
	} {
		if w := hh.do(t, http.MethodPost, c.path, c.body); w.Code != c.want {
			t.Errorf("%s: want %d, got %d (%s)", c.label, c.want, w.Code, w.Body.String())
		}
	}
}

func TestHandlerRecipes_DeleteIngredientIs204WithNoBody(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	other := mkRecipe(t, hh, "Tacos")
	beef := mkIngredient(t, hh, r.ID, "Beef")

	// An ingredient may only be deleted through the recipe that owns it.
	w := hh.do(t, http.MethodDelete, "/api/recipes/"+other.ID+"/ingredients/"+beef.ID, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("delete via the wrong recipe: want 404, got %d", w.Code)
	}

	w = hh.do(t, http.MethodDelete, "/api/recipes/"+r.ID+"/ingredients/"+beef.ID, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d (%s)", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Errorf("204 carried a body: %q", w.Body.String())
	}

	items := decodeJSON[[]grocery.Item](t, hh.do(t, http.MethodGet, "/api/items", nil))
	if len(items) != 0 {
		t.Errorf("ingredient survived deletion: %+v", items)
	}
}

func TestHandlerRecipes_ReorderRewritesOrder(t *testing.T) {
	hh := newHarness(t, nil)
	a := mkRecipe(t, hh, "Chili")
	b := mkRecipe(t, hh, "Tacos")
	c := mkRecipe(t, hh, "Soup")

	w := hh.do(t, http.MethodPost, "/api/recipes/reorder",
		map[string]any{"ids": []string{c.ID, a.ID, b.ID}})
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", w.Code, w.Body.String())
	}
	got := decodeJSON[[]grocery.Recipe](t, w)
	want := []string{"Soup", "Chili", "Tacos"}
	for i, name := range want {
		if i >= len(got) || got[i].Name != name {
			t.Fatalf("order after reorder: got %+v, want %v", got, want)
		}
	}

	if w := hh.do(t, http.MethodPost, "/api/recipes/reorder", map[string]any{"ids": "nope"}); w.Code != http.StatusBadRequest {
		t.Errorf("reorder with a malformed body: want 400, got %d", w.Code)
	}
}

func TestHandlerItems_DeleteRecipeOwnedItemIs409(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	beef := mkIngredient(t, hh, r.ID, "Beef")

	w := hh.do(t, http.MethodDelete, "/api/items/"+beef.ID, nil)
	// 409, not 404: the item is right there, the request is refused because
	// deleting it from the grocery side would silently break the recipe.
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "recipe") {
		t.Errorf("409 message does not mention the recipe: %s", w.Body.String())
	}

	items := decodeJSON[[]grocery.Item](t, hh.do(t, http.MethodGet, "/api/items", nil))
	if len(items) != 1 {
		t.Errorf("refused delete still removed the item: %+v", items)
	}
}

func TestBroker_RecipeRoutesNotifyExactlyOnce(t *testing.T) {
	hh := newHarness(t, nil)
	r := mkRecipe(t, hh, "Chili")
	other := mkRecipe(t, hh, "Tacos")
	beef := mkIngredient(t, hh, r.ID, "Beef")

	notifyCountC(t, hh.broker, "POST /api/recipes", 1, func() {
		hh.do(t, http.MethodPost, "/api/recipes", map[string]string{"name": "Soup"})
	})
	notifyCountC(t, hh.broker, "PATCH /api/recipes/:id (both fields)", 1, func() {
		hh.do(t, http.MethodPatch, "/api/recipes/"+r.ID,
			map[string]any{"name": "Chili Verde", "enabled": true})
	})
	notifyCountC(t, hh.broker, "POST /api/recipes/:id/ingredients", 1, func() {
		hh.do(t, http.MethodPost, "/api/recipes/"+r.ID+"/ingredients",
			map[string]string{"name": "Beans"})
	})
	notifyCountC(t, hh.broker, "DELETE /api/recipes/:id/ingredients/:item_id", 1, func() {
		hh.do(t, http.MethodDelete, "/api/recipes/"+r.ID+"/ingredients/"+beef.ID, nil)
	})
	notifyCountC(t, hh.broker, "POST /api/recipes/reorder", 1, func() {
		hh.do(t, http.MethodPost, "/api/recipes/reorder",
			map[string]any{"ids": []string{other.ID, r.ID}})
	})
	notifyCountC(t, hh.broker, "DELETE /api/recipes/:id", 1, func() {
		hh.do(t, http.MethodDelete, "/api/recipes/"+r.ID, nil)
	})

	// Rejected requests must be silent: no write happened, so no client should
	// be told to refetch.
	notifyCountC(t, hh.broker, "POST /api/recipes (duplicate name)", 0, func() {
		hh.do(t, http.MethodPost, "/api/recipes", map[string]string{"name": "Tacos"})
	})
	notifyCountC(t, hh.broker, "DELETE /api/recipes/:id (unknown)", 0, func() {
		hh.do(t, http.MethodDelete, "/api/recipes/nope", nil)
	})
}
