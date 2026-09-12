# FRD — Recipes Tab for the Grocery Module

**Status:** Draft, ready for planning
**Module:** `internal/grocery/`, `web/grocery/`
**Branch:** `grocerymod`
**Author:** drafted with Claude Code, decisions by Mr. D
**Date:** 2026-09-09

---

## 1. Summary

The grocery module today organizes food **by location in the store** — items belong to
ordered groups (Produce, Meats, mid store, …) and carry a tri-state `state`
(`needed` / `check` / `not_needed`) plus a `completed` checkbox.

This FRD adds a **second main tab, Recipes**, that presents the *same* underlying item
engine organized **by recipe** instead of by store location.

The one behavioral addition: **a recipe can be enabled or disabled.** Enabling a recipe
flips its ingredients to `needed` in the Grocery tab; disabling flips them to
`not_needed`. Everything else — groups, drag/drop, sync, SSE, offline banner, progress
meter — is unchanged.

### 1.1 Goals

- A Recipes tab that lists recipes, each with an enable/disable switch and its ingredients.
- Enabling/disabling a recipe performs a bulk state write across that recipe's items.
- Adding an ingredient is frictionless: no requirement that a matching grocery item exists.
- Recipe ingredients are real grocery items — one shared engine, one data file, one sync path.
- A recipe can never be silently broken from the Grocery side.

### 1.2 Non-goals (explicitly out of scope for v1)

- **Quantities / units.** No "2 lbs beef". Near-duplicate entries are acceptable and expected.
- **Shared ingredients across recipes.** Each recipe owns its own items (see §3.1).
- **Recipe instructions, prep steps, photos, servings, tags, or scheduling/meal-planning by date.**
- **Import/export of recipes**, URL scraping, or recipe search.
- **Drag-reorder of ingredients within a recipe.** Insertion order only.
- Changes to any other module (todo, slideshow, menuserver, obsidianoid).

---

## 2. Terminology

| Term | Meaning |
|---|---|
| **Item** | An existing `grocery.Item` — the row in the Grocery tab. |
| **Recipe** | A named, toggleable collection of ingredients. |
| **Ingredient** | An Item that is *owned by* a Recipe. Not a separate record type. |
| **Owned item** | Same as ingredient: an Item whose `recipe_id` is non-empty. |
| **Free item** | An Item with no `recipe_id` — i.e. every item that exists today. |
| **Unallocated** | The display label for the existing virtual `"No Group"` bucket. |

---

## 3. Locked design decisions

These were decided with the product owner before drafting. Each records the rejected
alternative so planning does not relitigate them.

### 3.1 Ingredients are 1:1 with grocery items, and recipes do NOT share them

An ingredient **is** a grocery item, owned by exactly one recipe. If both *Chili* and
*Taco Night* call for ground beef, the Grocery tab contains **two** rows.

> **Rationale.** There is no quantity concept. Making chili on Monday and tacos on Tuesday
> requires twice the beef, so two rows is the honest representation. This also removes
> reference-counting entirely: toggling a recipe writes only to items it exclusively owns,
> so no toggle can ever contradict another recipe.

*Rejected:* many-to-many ingredient sharing with reference counting; free-text
name-matching against existing items.

### 3.2 Adding an ingredient auto-creates the item in Unallocated

Typing an ingredient name into a recipe **always creates a new grocery item**. It is never
an error that no matching item exists, and the user is never asked to pick a group up front.
The new item lands in the existing virtual `"No Group"` bucket, which renders last in the
Grocery tab. The user can then drag it to Produce/Meats/etc. at their leisure.

The bucket keeps its stored value `"No Group"` (no data migration, no change to
`grocery.NoGroup`) but is **relabeled "Unallocated"** in the UI, since it now holds both
recipe-created items and items orphaned by a deleted group.

*Rejected:* a second distinct virtual bucket; a real (non-virtual) auto-created group.

### 3.3 Display name is derived, not baked in

The item stores `name: "beef"`. The Grocery tab **renders** `beef (Chili)` by appending the
owning recipe's name at display time. The Recipes tab renders just `beef`.

> **Rationale.** Renaming a recipe instantly and correctly updates every row; no stale
> suffixes, no string surgery on user data, and search/sort still operate on the clean name.

*Rejected:* concatenating `"beef (Chili)"` into `Item.Name` at creation time.

### 3.4 Toggling is a one-time flip; manual edits always win

- **Enable** → every owned item's `state` becomes `needed`.
- **Disable** → every owned item's `state` becomes `not_needed`.

There is no derived/overlay state and no snapshot/restore. After a toggle, the items are
ordinary items. The user may freely cycle any row's state in the Grocery tab — "I already
have beef in the freezer" → tap to `not_needed` — and that sticks. The recipe re-asserts
`needed` only on the next enable.

*Rejected:* ref-counted derived state; snapshot-and-restore on disable; locking rows while
a recipe is enabled.

### 3.5 Recipe-owned items cannot be deleted from the Grocery tab

Deleting an owned item from the Grocery side is **refused** (button suppressed in the UI,
`409 Conflict` from the API). This is the guard against accidentally breaking a recipe.

Deletion flows only from the recipe side:
- Delete an ingredient from a recipe → its item is deleted.
- Delete a recipe → **all** of its items are deleted (behind a confirmation modal).

*Rejected:* Grocery-side delete cascading up and silently removing the ingredient.

### 3.6 One file, one page

Recipes are persisted in the **existing `grocery.json`** alongside `title`, `groups`, and
`items`. The Recipes tab is a **client-side tab in the existing SPA** — same `app.js`, same
`/api/events` SSE stream, same in-memory state.

> **Rationale.** Recipes are part of the grocery construct, not a neighbouring feature.
> Creating an ingredient writes a Recipe row and an Item row; in one file that is a single
> atomic `save()`. Split across two files it is two writes with a torn-state window.

*Rejected:* separate `recipes.json`; separate `/recipes.html` page.

---

## 4. Data model

### 4.1 Go — `internal/grocery/model.go`

```go
// Item gains one field. Empty string = free item (all existing data).
type Item struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Group     string    `json:"group"`
    State     ItemState `json:"state"`
    Completed bool      `json:"completed"`
    Order     int       `json:"order"`
    CreatedAt time.Time `json:"created_at"`
    RecipeID  string    `json:"recipe_id,omitempty"` // NEW — owning recipe, "" if none
}

// Recipe is a named, toggleable collection of items.
type Recipe struct {
    ID        string    `json:"id"`
    Name      string    `json:"name"`
    Enabled   bool      `json:"enabled"`
    Order     int       `json:"order"`
    CreatedAt time.Time `json:"created_at"`
}
```

**The ingredient list is derived, not stored.** A recipe's ingredients are exactly
`items where item.recipe_id == recipe.id`, ordered by `created_at`. There is no
`ingredient_ids []string` on Recipe.

> **Rationale.** A single source of truth. A stored ID list can diverge from the items map
> (dangling IDs, missing entries) and would need reconciliation on every load, delete, and
> bulk sync. Deriving makes those bugs unrepresentable. The cost is an O(n) filter per
> recipe render, which is irrelevant at this data size.

### 4.2 On-disk — `storeData`

```go
type storeData struct {
    Title   string    `json:"title,omitempty"`
    Groups  []string  `json:"groups,omitempty"`
    Items   []*Item   `json:"items"`
    Recipes []*Recipe `json:"recipes,omitempty"` // NEW
}
```

**Back-compatibility is mandatory.** An existing `grocery.json` with no `recipes` key loads
into an empty recipe list, and items with no `recipe_id` load as free items. The legacy
plain-array format (`load()` already handles a leading `[`) must continue to work.

### 4.3 Config

No changes to `GroceryConfig`. Recipes need no configuration.

---

## 5. API surface

All new routes are registered in `Handler.Register` and call `h.broker.Notify()` after any
mutation, exactly like existing routes.

| Method | Path | Body | Returns |
|---|---|---|---|
| `GET` | `/api/recipes` | — | `[]Recipe` |
| `POST` | `/api/recipes` | `{name}` | `201` + `Recipe` |
| `PATCH` | `/api/recipes/{id}` | `{name?, enabled?}` | `{recipe, items}` |
| `DELETE` | `/api/recipes/{id}` | — | `{items}` (surviving items) |
| `POST` | `/api/recipes/{id}/ingredients` | `{name}` | `201` + `Item` |
| `DELETE` | `/api/recipes/{id}/ingredients/{item_id}` | — | `204` |
| `POST` | `/api/recipes/reorder` | `{ids}` | `[]Recipe` |

Notes:

- `PATCH …/{id}` with `enabled` performs the bulk state write and returns the **full item
  list** so the client can re-render both tabs from one response.
- `DELETE /api/recipes/{id}` deletes the recipe **and every item it owns**, returning the
  surviving item list.
- The `PATCH` and `DELETE` response shapes are **deliberately asymmetric**. `PATCH` returns
  `{recipe, items}` because the recipe still exists and its fields may have changed; `DELETE`
  returns `{items}` only. After a successful `DELETE` the client is responsible for dropping
  that recipe from its local state — it already knows the id it sent.
- `POST …/ingredients` creates the item with `group = NoGroup`, `state` matching the
  recipe's current `enabled` flag (`needed` if enabled, `not_needed` if disabled), and
  `recipe_id` set.
- Errors use the existing `response.WriteError` helper. Unknown recipe → `404`.
  Empty/whitespace name → `400`. Duplicate recipe name → **`409`**.
  > *Corrected 2026-09-10: this line originally said `400` for a duplicate; that was a
  > drafting mistake. A duplicate is a well-formed request colliding with existing state.*

### 5.1 Modified existing endpoints

| Endpoint | Change |
|---|---|
| `DELETE /api/items/{id}` | Returns **`409 Conflict`** with a clear message if the item has a non-empty `recipe_id`. |
| `POST /api/sync` (`BulkSync`) | Must **ignore incoming `recipe_id`** on existing items (server is authoritative) and **reject** new incoming items carrying a `recipe_id`. Prevents an offline client from re-parenting or orphaning ingredients. |
| `POST /api/reset` | Sets all items `completed=false, state=check` **and sets every recipe `enabled=false`** (see assumption A-2). **Response shape is unchanged — a bare `[]Item`.** Recipe changes reach clients via `Notify()` → SSE. |
| `POST /api/config/groups/remove` | Unchanged — owned items orphan to `NoGroup` like any other item. |
| `POST /api/move`, `/api/reorder` | Unchanged — owned items are freely movable between groups. That is the intended workflow. |

---

## 6. Behavior specification

### 6.1 Recipe lifecycle

1. **Create.** Name required, trimmed, must be unique (case-insensitive). New recipes are
   created **disabled** with zero ingredients.
2. **Rename.** Updates the recipe name; all derived display names in the Grocery tab update
   on the next render.
3. **Reorder.** Recipes render in `Order`, editable via per-card up/down controls.
   Drag-reorder of recipe cards is deferred to §11.
4. **Delete.** Confirmation modal naming the recipe and the number of items that will be
   removed. On confirm: recipe and all owned items are deleted.

### 6.2 Ingredient lifecycle

1. **Add.** Free text, trimmed, non-empty. Creates a new item in `NoGroup` with
   `recipe_id` set. Duplicate names *within* a recipe are allowed (no quantity concept, so
   "beef" twice is a legitimate way to say "more beef").
2. **Remove.** Deletes the underlying item outright.
3. **Regroup.** Performed from the Grocery tab by dragging the row into a real group. The
   `recipe_id` is untouched.

### 6.3 Enable / disable

```
enable(R):  for each item where recipe_id == R.id:  state = needed,     completed = false
disable(R): for each item where recipe_id == R.id:  state = not_needed, completed = false
```

- Free items are never touched by a toggle.
- Items owned by *other* recipes are never touched.
- The write is a single store mutation under one lock, followed by one `save()` and one
  `Notify()`.

### 6.4 Manual override

No row is ever locked. In the Grocery tab an owned item's state badge cycles
`needed → check → not_needed → needed` exactly as today, and the checkbox works normally.
A manual change persists until the owning recipe is next toggled.

### 6.5 Offline / sync-disabled behavior

When the **Sync** toggle is off, the Recipes tab is **read-only**: the enable switches, the
shared footer form (which creates recipes while this tab is active, §7.3), the per-card
ingredient input, and the rename, delete and reorder controls are all disabled, and a hint
reads *"Recipe editing requires sync."* Grocery-tab behavior when offline is unchanged.

> **Rationale.** Recipe mutations create and delete items server-side; queuing them for an
> offline replay is meaningful complexity for a feature used on a home LAN. Deferred, not
> refused — see §11.

---

## 7. UI specification

### 7.1 Tab bar

A two-tab control in the app header: **Grocery** | **Recipes**. Active tab persists in
`localStorage` across reloads. Switching tabs is pure client-side render — no navigation, no
refetch, no SSE reconnect.

Header controls are tab-scoped:

| Control | Grocery | Recipes |
|---|---|---|
| Progress meter | shown (per config) | hidden |
| Hide-not-needed (eye) | shown | hidden |
| Collapse / expand all | shown | shown (collapses recipe cards) |
| Reset | shown | hidden |
| Manage groups | shown | hidden |
| Sync toggle | shown | shown |
| Title (click to edit) | shown | shown |

### 7.2 Recipes tab

A vertical list of recipe cards, styled consistently with `.group-section`:

```
┌──────────────────────────────────────────────┐
│ ⠿  Chili                        [ ON ● ]  🗑  │   ← drag handle, name (click to rename)
│    3 ingredients                          ⌄  │      enable switch, delete
├──────────────────────────────────────────────┤
│    beef                                   ✕  │
│    kidney beans                           ✕  │
│    cumin                                  ✕  │
│    [ Add an ingredient…            ]  ＋      │
└──────────────────────────────────────────────┘
```

- Enable switch reuses the existing `.sync-toggle` track/thumb styling.
- Card collapse/expand mirrors `.group-header` / `.group-body`.
- An enabled recipe's card is visually distinct (accent border or tinted header).
- Empty state when no recipes exist, matching the existing `.empty-state` pattern.

### 7.3 Footer add-form is tab-aware

The existing footer form switches context with the active tab:

- **Grocery tab:** unchanged — group `<select>` + "Add an item…".
- **Recipes tab:** group select hidden, placeholder becomes "Add a recipe…", submit creates
  a recipe.

### 7.4 Grocery tab changes

1. Owned rows render `name (RecipeName)`. The recipe-name portion is styled as a subdued
   suffix, not part of the item name.
2. Owned rows carry a small recipe badge/chip; clicking it switches to the Recipes tab and
   scrolls to that recipe.
3. Owned rows **do not render a delete button**. A `title` on the row explains why
   ("Delete this ingredient from its recipe").
4. `"No Group"` renders with the heading **"Unallocated"**. Its empty-state hint changes from
   "No orphaned items" to "Nothing unallocated". The stored value stays `"No Group"`.

### 7.5 Housekeeping (in scope)

`web/grocery/index.html` currently contains the title-edit modal **six times** — six
identical `id="title-modal"` blocks. It is harmless today (JS binds the first) but it is
invalid HTML and the file is about to be edited heavily. Collapse to a single instance as
part of this work.

---

## 8. Documented assumptions

Flag any of these in planning if the intent was different; each is cheap to flip.

| # | Assumption |
|---|---|
| **A-1** | Toggling a recipe also clears `completed` on its items. A toggle is a fresh planning action, so previously-bought rows come back un-ticked. |
| **A-2** | `POST /api/reset` disables all recipes. Reset means "start a new shopping trip", so recipe selections are cleared with everything else. |
| **A-3** | Recipe names must be unique, case-insensitively. Ingredient names need not be unique, within or across recipes. |
| **A-4** | An existing free item cannot be "adopted" into a recipe in v1. Ingredients are always newly created. |
| **A-5** | Deleting a group leaves owned items alone apart from orphaning them to Unallocated, same as any item. |
| **A-6** | Ingredient order within a recipe is insertion order (`created_at`); no drag-reorder in v1. |
| **A-7** | No cap on recipes or ingredients per recipe. |

---

## 9. Acceptance criteria

Testable, and grouped so they slice cleanly into implementation tasks.

### AC-1 — Persistence & back-compat
1. An existing `grocery.json` with no `recipes` key loads without error; `Recipes()` returns empty.
2. The legacy plain-array item format still loads.
3. Items with no `recipe_id` round-trip with the field **absent** from the JSON (`omitempty`).
4. A recipe survives save→load with `id`, `name`, `enabled`, `order`, `created_at` intact.
5. Every recipe mutation increments `Revision()` exactly once and fires exactly one `Notify()`.

### AC-2 — Recipe CRUD
1. `POST /api/recipes {name:"Chili"}` returns `201` and a recipe with `enabled:false` and no items.
2. Creating a recipe whose name matches an existing one case-insensitively returns `400`.
3. Creating with an empty or whitespace-only name returns `400`.
4. `PATCH /api/recipes/{id} {name:"Chili Verde"}` renames without touching items.
5. `PATCH`/`DELETE` on an unknown id returns `404`.
6. `POST /api/recipes/reorder {ids}` sets `Order` to match the given sequence.

### AC-3 — Ingredient creation
1. `POST /api/recipes/{id}/ingredients {name:"beef"}` returns `201` with `group == "No Group"`, `recipe_id == {id}`, `name == "beef"` (no suffix baked in).
2. Adding to an **enabled** recipe creates the item with `state == "needed"`.
3. Adding to a **disabled** recipe creates the item with `state == "not_needed"`.
4. Two recipes each adding "beef" produce two distinct items with distinct IDs.
5. Adding to an unknown recipe returns `404`.

### AC-4 — Enable / disable semantics
1. Enabling a recipe sets every owned item to `state=="needed"`, `completed==false`.
2. Disabling sets every owned item to `state=="not_needed"`, `completed==false`.
3. Toggling recipe A never modifies any item owned by recipe B.
4. Toggling never modifies a free item.
5. Given beef(Chili) and beef(Tacos) as separate items, disabling Chili leaves beef(Tacos) at `needed`.
6. A manual `PATCH /api/items/{id} {state:"not_needed"}` on an owned item while its recipe is enabled persists; a subsequent `GET` still reports `not_needed`.
7. Re-enabling that recipe restores the item to `needed`.

### AC-5 — Deletion guard
1. `DELETE /api/items/{id}` on an item with a non-empty `recipe_id` returns `409` and the item still exists.
2. `DELETE /api/items/{id}` on a free item still returns `204`.
3. `DELETE /api/recipes/{id}/ingredients/{item_id}` deletes the item and it disappears from `GET /api/items`.
4. Deleting an ingredient whose `recipe_id` does not match the URL's recipe returns `404`.
5. `DELETE /api/recipes/{id}` removes the recipe and every item it owned, and leaves free items and other recipes' items untouched.

### AC-6 — Sync hardening
1. `POST /api/sync` containing an existing owned item with a **mutated** `recipe_id` leaves the stored `recipe_id` unchanged.
2. `POST /api/sync` containing a **new** item that carries a `recipe_id` does not create that item.
3. `POST /api/sync` still merges `state`, `completed`, `group`, `order` for owned items as it does for free items.

### AC-7 — Reset
1. `POST /api/reset` sets every item to `completed=false, state="check"`, including owned items.
2. `POST /api/reset` sets every recipe to `enabled=false` (per A-2).

### AC-8 — Client: tabs & Recipes view
1. The header shows Grocery and Recipes tabs; exactly one is active.
2. The active tab persists across a page reload.
3. Switching tabs does not refetch items and does not reconnect SSE.
4. Grocery-only header controls (eye, reset, groups, progress) are hidden on the Recipes tab.
   On the Grocery tab the progress meter's visibility continues to follow config
   (`progress` enabled **and** at least one item); the Recipes tab never shows it.
5. The Recipes tab lists every recipe with its ingredient count and enable switch.
6. The footer form creates a recipe when the Recipes tab is active and an item when the Grocery tab is active.
7. An SSE `refresh` re-renders whichever tab is active with fresh data.
8. On the Recipes tab, Collapse/Expand collapses and expands all recipe cards.

### AC-9 — Client: Grocery view changes
1. An owned item renders as `name (RecipeName)`.
2. Renaming a recipe updates that suffix everywhere on the next render, with no write to `Item.Name`.
3. Owned rows render no delete button; free rows still do.
4. Every user-visible occurrence of the `"No Group"` string reads "Unallocated" — the
   Grocery section heading and the Groups-modal hint.
   The stored value remains `"No Group"` (renaming it is deferred to §11).
   *(The footer group `<select>` was listed here in error and is struck as of the plan's r4
   revision, R4-1: `rebuildGroupSelect()` populates the dropdown from real groups only and
   never offers `"No Group"` as an add target, so there is no occurrence there to relabel.
   Adding one would be a feature change, not a rename.)*
5. Toggling a recipe's switch updates the Grocery tab's states without a manual reload.
6. Clicking an owned row's recipe chip switches to the Recipes tab, expands that recipe's
   card if it is collapsed, and scrolls it into view.
7. Recipe names are output-encoded everywhere they render: a recipe named `Chili <b>` appears
   as literal text in both the Recipes-tab card and the Grocery-tab suffix. No recipe name is
   interpolated into markup without `esc()`, and any selector built from a recipe id uses
   `CSS.escape`.

### AC-10 — Client: offline
1. With Sync off, all Recipes-tab mutation controls are disabled and the hint is shown.
2. With Sync on, they re-enable immediately.

### AC-11 — Housekeeping
1. `web/grocery/index.html` contains exactly one element with `id="title-modal"`.
2. Title editing still works.

---

## 10. Test plan

| Layer | Location | Coverage |
|---|---|---|
| Go store | `internal/grocery/store_test.go` | AC-1, AC-3 (creation rules), AC-4 (toggle semantics incl. isolation between recipes), AC-5 (cascade + guard), AC-6 (BulkSync hardening), AC-7 |
| Go handler | `internal/grocery/handler_test.go` | AC-2, status codes, `409` guard, payload shapes, `Notify` fan-out |
| Config | `internal/platform/config/config_test.go` | unchanged — assert no regression |
| JS pure logic | `web/grocery/app.test.js` | display-name derivation, ingredient filtering by `recipe_id`, tab-scoped control visibility, `groupsForRender` with the Unallocated relabel. Follow the existing convention: helpers are mirrored inline in the test file and kept in sync with `app.js`. |
| Manual | — | Full loop on two browsers at once to confirm SSE cross-client refresh on recipe toggle. |

`make test` (`go test -race ./...`) must pass. JS tests run with `node --test web/grocery/app.test.js`.

---

## 11. Deferred / future work

Recorded so planning does not accidentally scope them in:

- Quantities and units.
- Adopting an existing free item into a recipe (A-4).
- Offline recipe editing with replay-on-reconnect (§6.5).
- Meal planning by date; "what am I cooking this week".
- Recipe instructions, servings, photos, import/export.
- Drag-reorder of ingredients within a recipe (A-6).
- Drag-reorder of recipe cards on the Recipes tab (§6.1.3 ships up/down controls instead).
- Renaming the `"No Group"` stored value to `"Unallocated"` with a data migration.

---

## 12. Risks

| Risk | Mitigation |
|---|---|
| `BulkSync` is the weakest link — offline clients POST whole items and could corrupt `recipe_id`. | Server-authoritative `recipe_id` (AC-6), covered by explicit tests. |
| Near-duplicate rows (`beef (Chili)`, `beef (Tacos)`) may look like a bug to a user mid-shop. | Recipe suffix + badge on every owned row makes provenance obvious at a glance. |
| `app.js` is a 938-line IIFE with no module boundaries; adding a second view risks entangling render paths. | Introduce a single `activeTab` state variable and two top-level render functions sharing the existing helpers. Resist a rewrite — this FRD does not authorize one. |
| Deleting a recipe destroys items the user may have spent time grouping. | Confirmation modal states the item count explicitly. |
| Six duplicate `title-modal` blocks indicate prior copy-paste drift in `index.html`. | Cleaned up as in-scope housekeeping (AC-11) before the file grows further. |
