# Manual verification — Recipes tab

Everything that could be gated by a command **has** been. This file holds only what is
left: the checks that need a live server, a real browser, and in a few cases two windows
side by side.

**None of these has been run.** They are not simulated, inferred, or claimed anywhere in
`.omc/plans/PROGRESS-recipes-tab.md`. Fourteen numbered FRD §9 lines discharge to this
file and nowhere else.

## What is already green, so you can skip it

| Gate | Result |
|---|---|
| `make test` | exit 0, uncached, `-race`, whole module |
| `node --test web/grocery/app.test.js` | 175 pass / 0 fail / 0 skipped (52 baseline) |
| Allowlist scans | only `internal/grocery/*.go`, `web/grocery/*`, `docs/*.md`, `.omc/plans/*.md` ever touched |

Both AC-9.7's static half (no un-`esc()`-ed name in markup, `CSS.escape` at every
id-derived selector) and AC-8.3's direct half (`setActiveTab` issues no network call)
are automated. Do not re-eyeball those.

## Setup

```sh
BASE=http://<host>:<port>          # your configured server URL
```

Two browsers on the LAN, side by side, for the steps that say so.

---

## 1 — Create and enable · *AC-3, AC-4.1, AC-8.7, AC-9.1, AC-9.3, AC-9.4, AC-8.6*

Create **Chili** using the footer form while the Recipes tab is active. Add *beef*,
*kidney beans*, *cumin*. Enable it.

On **both** browsers' Grocery tabs: three `needed` rows in **Unallocated**, each with a
`(Chili)` suffix and **no trash icon**.

## 2 — Drag and reload · *AC-9.1, §6.2.3*

Drag `beef (Chili)` into **Meats**. Reload. It stays in Meats and keeps its suffix.

Then confirm the display/data split — the screen says Unallocated, the store says
`No Group`:

```sh
curl -s "$BASE/api/items" | grep -c '"No Group"'   # non-zero, while nothing on screen reads "No Group"
```

## 3 — Manual override survives · *AC-4.6, AC-4.7*

Cycle `beef` to `not_needed`. Reload — it sticks. Disable then re-enable Chili — it
returns to `needed`.

## 4 — Recipes do not share ingredients · *AC-4.5*

Create **Tacos**, add *beef*. Disable Chili. `beef (Tacos)` stays `needed`.

## 5 — Rename propagates · *AC-9.2, AC-2.4*

Rename Chili → **Chili Verde**. Every suffix updates on the next render, on both
browsers, with no write to `Item.Name`.

## 6 — PM-3, remote path · *FRD §1.1*

With browser A **mid-typing** in Chili Verde's ingredient input, tick a checkbox in
browser B. A's input keeps its **text, caret position and focus**.

Then the case that broke before review: type a word, **move the caret with the left-arrow
key or Home** without typing anything more, and tick a checkbox in B again. The caret must
stay where you moved it, not jump back to where you last typed.

## 6a — PM-3, local path · *FRD §1.1*

**One browser, no second window.** This is the path an earlier design missed entirely.

1. Add three ingredients to one recipe in a row **without re-tapping the input**. After
   each add the input must be cleared **and still focused**. Do this **twice — once
   submitting with Enter, once with the add button.** They are different code paths and
   only the button path used to pass: on Enter the input is still `document.activeElement`
   when the repaint runs, so the just-added text was re-captured and restored, leaving it
   sitting in the box. Checking one path proves nothing about the other.
2. Start typing a fourth ingredient into that card and, mid-word, click the ✕ on an
   ingredient in a **different** recipe card. The first card's text and caret must
   survive, and focus must **not** jump into it.

## 7 — Reload on the Recipes tab · *AC-8.2, AC-8.3, AC-8.5*

Reload browser A **while it is on the Recipes tab**. All three must hold:

- **Tab chrome matches the restored tab.** In DevTools, not by eye:
  `document.getElementById('tab-recipes').className` and `.getAttribute('aria-selected')`
  — `#tab-recipes` carries `.active` and `aria-selected="true"`, `#tab-grocery` carries
  neither, and the eye / reset / groups buttons are hidden. Restoring the *content*
  under the Grocery *header* is the bug this catches.
- The Recipes tab is showing, not Grocery.
- The recipe cards are **populated**.

Then, with the Network panel open, switch tabs back and forth: **zero** XHR and no
EventSource reconnect. (The direct case is already asserted in `app.test.js`; this
catches a refetch reached indirectly through a helper.)

## 8 — Sync off · *AC-10.1, AC-10.2*

Turn Sync off in A. Every Recipes control disables, the hint shows, the footer submit is
inert. Turn it on — immediately live, with no reload.

## 9 — PM-1, resurrection · *AC-6.2*

Assert on **`$GHOST`**, never on the string `"recipe_id"` — by this point several owned
items legitimately exist, so a `"recipe_id"` count is non-zero on correct code.

```sh
# 1. A throwaway recipe nothing else here touches.
GHOST=$(curl -s -X POST -H 'Content-Type: application/json' \
          -d '{"name":"Ghost"}' "$BASE/api/recipes" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
curl -s -X POST -H 'Content-Type: application/json' \
     -d '{"name":"ectoplasm"}' "$BASE/api/recipes/$GHOST/ingredients" > /dev/null
curl -s "$BASE/api/items" | grep -c "$GHOST"        # → 1   PRECONDITION, see below

# 2. Snapshot WHILE the ghost exists — this is the stale client state.
curl -s "$BASE/api/items" > /tmp/snapshot.json

# 3. Delete the recipe.
curl -s -X DELETE "$BASE/api/recipes/$GHOST" > /dev/null

# 4. Replay the stale snapshot — the resurrection attempt.
curl -s -X POST -H 'Content-Type: application/json' \
     --data @/tmp/snapshot.json "$BASE/api/sync" > /dev/null

# 5. THE ASSERTION.
curl -s "$BASE/api/items" | grep -c "$GHOST"        # → 0
```

Step 1's `→ 1` is not decoration: it fails loudly if the id capture returned empty, which
would make step 5's `→ 0` pass vacuously.

## 10 — Delete cascade · *AC-5.5, §6.1.4*

Delete Chili Verde. The modal **names it**, states its **exact item count**, and warns
that items moved into other groups are included. Confirm — its items vanish; Tacos' beef
and every free item survive.

## 11 — Reset · *AC-7.1, AC-7.2*

`POST /api/reset`. All items `check` / uncompleted; **every recipe switch is off**.

## 12 — Title editing still works · *AC-11.2*

Click the title on both tabs, rename, save.

## 12a — A refused recipe name leaves no phantom card

On the Recipes tab, type an existing recipe's name into the footer and submit — try it in a
different case too (`chili` when `Chili` exists). Required: **no new card appears**, and your
text stays in the input, selected, rather than being silently blanked.

This is client-side on purpose. `api()` does not check `res.ok`, so the server's 409 comes back
as a parsed body that the success guard reads as "no reply" — before the fix the phantom card
survived until the next unrelated server write. Also try a name that is only whitespace: nothing
should happen.

## 13 — Output encoding at every render site · *AC-9.7*

Create a recipe named `Chili <b>` and add an ingredient to it. **One site passing is not
a pass** — these are separate `esc()` call sites in separate functions.

Confirm the literal text `Chili <b>` appears, with no bold text and no broken row, in:

1. the **Recipes-tab card title**;
2. the **Grocery-tab suffix** on the owned row;
3. the **delete-confirmation modal**;
4. the ingredient delete button's **`aria-label`** — read it in the DOM inspector.
   Attributes are the encoding site people forget, precisely because a broken one still
   *looks* fine on screen.

Then hover the owned row on the Grocery tab. The tooltip must read **`Belongs to recipe
Chili <b>`** — literally, angle brackets and all, and *not* `Chili &lt;b&gt;`. This one is the
opposite of the four above: `li.title` is a property assignment, not a markup boundary, so
`esc()` there would show the user raw entities. Seeing `&lt;` in the tooltip means someone
"fixed" it by escaping.

## 14 — Tab affordance · *AC-8.1*

Both tab buttons present; exactly one carries `.active` **and** `aria-selected="true"` at
any moment, and clicking the other swaps **both** in the same gesture. Check in the DOM
inspector — a styled-but-not-`aria-selected` tab passes a visual check and fails the
criterion.

## 15 — Header controls and the progress meter · *AC-8.4*

With config `progress:true` and at least one item: on Grocery the eye, reset and groups
buttons **and** the progress bar are visible; switch to Recipes and all four hide; switch
back and all four return.

Then set config `progress:false` (the default) and repeat: the three buttons behave as
above, and the progress bar stays hidden on **both** tabs and after every switch.

**While switching, watch the Grocery/Recipes buttons themselves.** They must stay put, hard
against the title, in every combination — progress on and off, both tabs. They used to drift
toward centre whenever the progress bar was hidden, which under the default `progress:false`
means on both tabs, not just Recipes. Header controls appearing and disappearing is expected;
the tabs moving is not.

## 16 — Empty-state isolation · *R3-18*

From **zero items and zero recipes**: on Grocery, `#empty-state` is visible and
`#recipes-empty-state` is not; switch to Recipes and the two swap; switch back and they
swap again. Then add one recipe and repeat — on Recipes **neither** empty state shows.

## 17 — Unallocated, and only these two sites · *AC-9.4*

With at least one unallocated item, the string **"Unallocated"** — never "No Group" —
appears in exactly **two** places: the **Grocery section heading** and the
**Groups-modal hint**. That is the complete list.

The footer `<select>` is **not** a third site. `rebuildGroupSelect()` populates the
dropdown from real groups only and never offers `No Group` as an add target. **Do not add
the option** to make a check pass — suppressing it is deliberate.

## 18 — The chip reveals its card · *AC-9.6*

On the Recipes tab, collapse **Tacos** with its chevron, with enough recipes above it that
it sits **below the fold** — otherwise the scroll is unobservable and the step proves
nothing.

Switch to Grocery. Click the `(Tacos)` chip on the `beef` row. **All three halves in one
gesture:** the Recipes tab becomes active, the Tacos card **expands**, and it **scrolls
into view**. Then, with that card already expanded and on screen, click the chip again —
the viewport must **not** jump.

The chip and the `(Tacos)` suffix are **the same element**. There is nothing else on the
row to click.

### The missing-card case

**Do not type `revealRecipe('no-such-id')` into the console.** `app.js` is a single IIFE
and assigns nothing to `window`, so every function here is module-private and a console
call is a `ReferenceError` that tests nothing.

Use the DOM instead. In the **Elements** panel, find the `beef` row's chip and change its
`data-recipe-id` to `no-such-id`. Click it. Required result: the app switches to the
Recipes tab and **does nothing else** — no thrown exception, clean console.

This matters because the chip is served by the delegated `gc` listener: a throw on a
`null` card is invisible on screen while silently killing every other arm of that handler
for the rest of the session.

## 19 — Collapse all / Expand all · *AC-8.8*

**Two separate buttons, not one toggle.** With at least three recipes, on the Recipes tab:

1. Press **Collapse all** — every card body collapses.
2. Press **Expand all** — every card body expands.
3. Switch to Grocery, press **Collapse all** there — the *group* sections collapse and the
   recipe cards are untouched.
4. Switch back — the Recipes tab still holds its own collapse state, in both directions.

---

## Layout check (T6)

Load both tabs at phone width and at desktop width. Recipe cards should sit consistently
with `.group-section`.

## Known cosmetic leftover

`.recipe-add`, `.recipe-input`, `.recipe-btn` and `.recipe-icon` in `style.css` are **dead
CSS** from T6: T8's specification routes recipe creation through the shared footer form
instead. Harmless, and removing them is churn in a file a parallel branch may touch, so
they were left in place. Noted rather than silently cleaned.
