/**
 * app.test.js — pure-logic unit tests (no DOM required)
 *
 * Run with:  node --test app.test.js
 * Requires Node.js >= 18 (built-in test runner + assert).
 */
import { describe, it }  from 'node:test';
import assert            from 'node:assert/strict';
import { readFileSync }  from 'node:fs';
import { join }          from 'node:path';

// The SHIPPED app.js source. The mirror-integrity block and AC-9.7's static
// assertions at the end of this file both read it, so they inspect what
// actually ships rather than a mirrored copy. import.meta.dirname keeps this
// independent of the working directory the runner is invoked from.
const APP_SRC = readFileSync(join(import.meta.dirname, 'app.js'), 'utf8');
const CSS_SRC = readFileSync(join(import.meta.dirname, 'style.css'), 'utf8');

// Indentation differs by design: app.js helpers live inside the IIFE at 2-space
// indent; the app.test.js mirrors sit at column 0. Compare on content, not
// layout. This is deliberately NOT whitespace-insensitive — internal spacing,
// line breaks, names and punctuation must still match exactly.
const norm  = s => s.replace(/^[ \t]+/gm, '').trim();
const APP_N = norm(APP_SRC);

// ────────────────────────────────────────────────────────────────
// Inline copies of pure helpers from app.js (no DOM dependency).
// Keep these in sync with app.js.
// ────────────────────────────────────────────────────────────────

const STATES       = ['needed', 'check', 'not_needed'];
const NO_GROUP     = 'No Group';

function nextState(s) {
  return STATES[(STATES.indexOf(s) + 1) % STATES.length];
}

function itemsForGroup(items, group) {
  return [...items]
    .filter(i => i.group === group)
    .sort((a, b) =>
      a.order !== b.order
        ? a.order - b.order
        : new Date(a.created_at) - new Date(b.created_at)
    );
}

function esc(str) {
  return String(str)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;')
    .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/**
 * groupsForRender returns the ordered list of groups to render, appending the
 * virtual "No Group" section if any item carries that group and it has items.
 */
function groupsForRender(groups, items) {
  const has = items.some(i => i.group === NO_GROUP);
  return has ? [...groups, NO_GROUP] : [...groups];
}

/**
 * applyReset models doReset(): every item returns to completed=false,
 * state='check', and every recipe switches off (A-2), because a reset that
 * left recipes "on" would show enabled recipes whose ingredients all read
 * Check.
 *
 * NOT a mirror. doReset() mutates module-scope state in place and awaits the
 * server; this is a test-local model of its semantics, so it is deliberately
 * absent from the mirror-integrity block below. Recorded so the tag is not
 * read as a false promise.
 */
function applyReset(items, recipes = []) {
  return {
    items:   items.map(i => ({ ...i, completed: false, state: 'check' })),
    recipes: recipes.map(r => ({ ...r, enabled: false })),
  };
}

/**
 * removeGroup moves items from the deleted group to NO_GROUP and
 * returns updated { groups, items }.
 */
function removeGroup(groups, items, name) {
  const newGroups = groups.filter(g => g !== name);
  const newItems  = items.map(i =>
    i.group === name ? { ...i, group: NO_GROUP } : i
  );
  return { groups: newGroups, items: newItems };
}

/**
 * VISIBILITY_MODES — the three filter levels cycled by the eye button.
 *   0 = SHOW_ALL        : display everything
 *   1 = HIDE_NOT_NEEDED : hide items whose state is 'not_needed'; hide empty groups
 *   2 = HIDE_COMPLETED  : also hide items that are completed; hide empty groups
 */
const VISIBILITY_MODES = [
  'show_all',
  'hide_not_needed',
  'hide_completed',
];

function nextVisibilityMode(current) {
  return (current + 1) % VISIBILITY_MODES.length;
}

/**
 * visibilityFilter returns true if the item should be visible in the given mode.
 *   mode 0 (show_all)       → always true
 *   mode 1 (hide_not_needed)→ false when state === 'not_needed'
 *   mode 2 (hide_completed) → false when state === 'not_needed' OR completed === true
 */
function visibilityFilter(item, mode) {
  if (mode === 0) return true;
  if (item.state === 'not_needed') return false;
  if (mode === 2 && item.completed) return false;
  return true;
}

/**
 * groupIsVisible returns true if the group has at least one visible item
 * (or if mode is 0, always true so empty groups still show).
 */
function groupIsVisible(items, group, mode) {
  if (mode === 0) return true;
  return items.some(i => i.group === group && visibilityFilter(i, mode));
}


// ── Recipes tab helpers (T7) ────────────────────────────────────
// Inline copies per FRD §10's convention. The `mirror integrity` block at the
// end of this file proves against the shipped app.js that they have not
// drifted, so "keep these in sync" is mechanical rather than aspirational.

// mirrors app.js :: recipeNameTaken
function recipeNameTaken(recipes, name, exceptId) {
  const n = String(name).trim().toLowerCase();
  return recipes.some(r => r.id !== exceptId && r.name.trim().toLowerCase() === n);
}

// mirrors app.js :: nextRecipeOrder
function nextRecipeOrder(recipes) {
  return recipes.reduce((max, r) => (r.order >= max ? r.order + 1 : max), 0);
}

// mirrors app.js :: recipeById
function recipeById(recipes, id) {
  return recipes.find(r => r.id === id) || null;
}

// mirrors app.js :: recipesForRender
function recipesForRender(recipes) {
  return [...recipes].sort((a, b) =>
    a.order !== b.order
      ? a.order - b.order
      : new Date(a.created_at) - new Date(b.created_at)
  );
}

// mirrors app.js :: ingredientsForRecipe
function ingredientsForRecipe(items, recipeId) {
  return items
    .filter(i => i.recipe_id === recipeId)
    .sort((a, b) => new Date(a.created_at) - new Date(b.created_at));
}

// mirrors app.js :: isOwned
function isOwned(item) {
  return !!(item && item.recipe_id);
}

// mirrors app.js :: recipeSuffix
function recipeSuffix(item, recipes) {
  if (!isOwned(item)) return '';
  const r = recipeById(recipes, item.recipe_id);
  return r ? ` (${r.name})` : '';
}

// mirrors app.js :: ownedTooltip
function ownedTooltip(item, recipes) {
  if (!isOwned(item)) return '';
  const r = recipeById(recipes, item.recipe_id);
  return r ? `Belongs to recipe ${r.name}` : '';
}

// mirrors app.js :: displayName
function displayName(item, recipes) {
  return item.name + recipeSuffix(item, recipes);
}

// mirrors app.js :: groupLabel
function groupLabel(group) {
  return group === 'No Group' ? 'Unallocated' : group;
}

// mirrors app.js :: groupEmptyHint
function groupEmptyHint(group) {
  return group === 'No Group' ? 'Nothing unallocated' : 'No items';
}

// mirrors app.js :: tabControlVisible
function tabControlVisible(controlId, tab) {
  const groceryOnly = ['#hide-not-needed-btn', '#reset-btn', '#groups-btn'];
  return groceryOnly.includes(controlId) ? tab === 'grocery' : true;
}

// mirrors app.js :: progressBarVisible
function progressBarVisible(showProgress, itemCount, tab) {
  return !!showProgress && itemCount > 0 && tab === 'grocery';
}

// mirrors app.js :: applyRecipeToggle
function applyRecipeToggle(items, recipeId, enabled) {
  return items.map(i =>
    i.recipe_id === recipeId
      ? { ...i, state: enabled ? 'needed' : 'not_needed', completed: false }
      : i
  );
}

// ────────────────────────────────────────────────────────────────
// Tests
// ────────────────────────────────────────────────────────────────

describe('nextState', () => {
  it('cycles needed → check', () => assert.equal(nextState('needed'), 'check'));
  it('cycles check → not_needed', () => assert.equal(nextState('check'), 'not_needed'));
  it('cycles not_needed → needed', () => assert.equal(nextState('not_needed'), 'needed'));
  it('handles unknown state gracefully (returns needed)', () => {
    // indexOf returns -1, (-1+1)%3 = 0 → 'needed'
    assert.equal(nextState('bogus'), 'needed');
  });
});

describe('itemsForGroup', () => {
  const items = [
    { id: '1', group: 'A', order: 2, created_at: '2024-01-01T00:00:00Z' },
    { id: '2', group: 'A', order: 0, created_at: '2024-01-01T00:00:00Z' },
    { id: '3', group: 'B', order: 0, created_at: '2024-01-01T00:00:00Z' },
    { id: '4', group: 'A', order: 1, created_at: '2024-01-01T00:00:00Z' },
  ];

  it('filters to the correct group', () => {
    const result = itemsForGroup(items, 'A');
    assert.equal(result.length, 3);
    result.forEach(i => assert.equal(i.group, 'A'));
  });

  it('sorts by order ascending', () => {
    const result = itemsForGroup(items, 'A');
    assert.deepEqual(result.map(i => i.id), ['2', '4', '1']);
  });

  it('returns empty for unknown group', () => {
    assert.deepEqual(itemsForGroup(items, 'Z'), []);
  });

  it('does not mutate source array', () => {
    const copy = [...items];
    itemsForGroup(items, 'A');
    assert.deepEqual(items, copy);
  });
});

describe('esc', () => {
  it('escapes &', () => assert.equal(esc('a&b'), 'a&amp;b'));
  it('escapes <', () => assert.equal(esc('<tag>'), '&lt;tag&gt;'));
  it('escapes "', () => assert.equal(esc('say "hi"'), 'say &quot;hi&quot;'));
  it('passes plain strings unchanged', () => assert.equal(esc('hello'), 'hello'));
  it('coerces numbers', () => assert.equal(esc(42), '42'));
});

describe('groupsForRender', () => {
  const baseGroups = ['Produce', 'Dairy'];

  it('returns groups unchanged when no NoGroup items', () => {
    const items = [{ group: 'Produce' }, { group: 'Dairy' }];
    assert.deepEqual(groupsForRender(baseGroups, items), baseGroups);
  });

  it('appends No Group when orphaned items exist', () => {
    const items = [{ group: 'Produce' }, { group: NO_GROUP }];
    const result = groupsForRender(baseGroups, items);
    assert.equal(result[result.length - 1], NO_GROUP);
    assert.equal(result.length, baseGroups.length + 1);
  });

  it('does not append No Group if already empty', () => {
    assert.deepEqual(groupsForRender(baseGroups, []), baseGroups);
  });

  it('does not mutate the source groups array', () => {
    const src = ['A', 'B'];
    groupsForRender(src, [{ group: NO_GROUP }]);
    assert.equal(src.length, 2);
  });
});

describe('applyReset', () => {
  it('sets all items to completed=false and state=check', () => {
    const items = [
      { id: '1', completed: true,  state: 'not_needed' },
      { id: '2', completed: false, state: 'needed' },
    ];
    const result = applyReset(items).items;
    result.forEach(i => {
      assert.equal(i.completed, false);
      assert.equal(i.state, 'check');
    });
  });

  it('does not mutate the original array', () => {
    const items = [{ id: '1', completed: true, state: 'needed' }];
    applyReset(items);
    assert.equal(items[0].completed, true);
  });

  it('returns an array of the same length', () => {
    const items = Array.from({ length: 5 }, (_, i) => ({
      id: String(i), completed: true, state: 'needed'
    }));
    assert.equal(applyReset(items).items.length, 5);
  });
});

describe('removeGroup', () => {
  const groups = ['Produce', 'Dairy', 'Frozen'];
  const items  = [
    { id: '1', group: 'Produce' },
    { id: '2', group: 'Dairy' },
    { id: '3', group: 'Produce' },
  ];

  it('removes the named group from the list', () => {
    const { groups: g } = removeGroup(groups, items, 'Produce');
    assert.ok(!g.includes('Produce'));
  });

  it('moves items from deleted group to No Group', () => {
    const { items: i } = removeGroup(groups, items, 'Produce');
    const orphans = i.filter(x => x.id === '1' || x.id === '3');
    orphans.forEach(o => assert.equal(o.group, NO_GROUP));
  });

  it('leaves items in other groups untouched', () => {
    const { items: i } = removeGroup(groups, items, 'Produce');
    const dairy = i.find(x => x.id === '2');
    assert.equal(dairy.group, 'Dairy');
  });

  it('does not mutate the source arrays', () => {
    const origGroups = [...groups];
    const origItems  = items.map(i => ({ ...i }));
    removeGroup(groups, items, 'Dairy');
    assert.deepEqual(groups, origGroups);
    assert.deepEqual(items.map(i => i.group), origItems.map(i => i.group));
  });

  it('handles removing a non-existent group gracefully', () => {
    const { groups: g, items: i } = removeGroup(groups, items, 'Bakery');
    assert.deepEqual(g, groups);
    assert.deepEqual(i.map(x => x.group), items.map(x => x.group));
  });
});

describe('nextVisibilityMode', () => {
  it('advances 0 → 1', () => assert.equal(nextVisibilityMode(0), 1));
  it('advances 1 → 2', () => assert.equal(nextVisibilityMode(1), 2));
  it('wraps 2 → 0',   () => assert.equal(nextVisibilityMode(2), 0));
});

describe('visibilityFilter', () => {
  const needed     = { state: 'needed',     completed: false };
  const check      = { state: 'check',      completed: false };
  const notNeeded  = { state: 'not_needed', completed: false };
  const completedN = { state: 'needed',     completed: true  };
  const completedC = { state: 'check',      completed: true  };

  describe('mode 0 (show_all)', () => {
    it('shows needed items',           () => assert.equal(visibilityFilter(needed,     0), true));
    it('shows check items',            () => assert.equal(visibilityFilter(check,      0), true));
    it('shows not_needed items',       () => assert.equal(visibilityFilter(notNeeded,  0), true));
    it('shows completed needed items', () => assert.equal(visibilityFilter(completedN, 0), true));
  });

  describe('mode 1 (hide_not_needed)', () => {
    it('shows needed items',              () => assert.equal(visibilityFilter(needed,     1), true));
    it('shows check items',               () => assert.equal(visibilityFilter(check,      1), true));
    it('hides not_needed items',          () => assert.equal(visibilityFilter(notNeeded,  1), false));
    it('shows completed items (not mode 2)', () => assert.equal(visibilityFilter(completedN, 1), true));
  });

  describe('mode 2 (hide_completed)', () => {
    it('shows needed items',          () => assert.equal(visibilityFilter(needed,     2), true));
    it('shows check items',           () => assert.equal(visibilityFilter(check,      2), true));
    it('hides not_needed items',      () => assert.equal(visibilityFilter(notNeeded,  2), false));
    it('hides completed needed',      () => assert.equal(visibilityFilter(completedN, 2), false));
    it('hides completed check',       () => assert.equal(visibilityFilter(completedC, 2), false));
  });
});

describe('groupIsVisible', () => {
  const items = [
    { id: '1', group: 'Produce', state: 'needed',     completed: false },
    { id: '2', group: 'Produce', state: 'not_needed', completed: false },
    { id: '3', group: 'Dairy',   state: 'not_needed', completed: false },
    { id: '4', group: 'Frozen',  state: 'needed',     completed: true  },
    { id: '5', group: 'Empty',   state: 'not_needed', completed: false },
  ];

  describe('mode 0 (show_all)', () => {
    it('shows all groups including empty-ish ones', () => {
      ['Produce', 'Dairy', 'Frozen', 'Empty', 'NoItems'].forEach(g =>
        assert.equal(groupIsVisible(items, g, 0), true)
      );
    });
  });

  describe('mode 1 (hide_not_needed)', () => {
    it('shows Produce (has a needed item)',   () => assert.equal(groupIsVisible(items, 'Produce', 1), true));
    it('hides Dairy (only not_needed items)', () => assert.equal(groupIsVisible(items, 'Dairy',   1), false));
    it('shows Frozen (needed+completed)',     () => assert.equal(groupIsVisible(items, 'Frozen',  1), true));
    it('hides Empty (only not_needed)',       () => assert.equal(groupIsVisible(items, 'Empty',   1), false));
    it('hides group with no items at all',    () => assert.equal(groupIsVisible(items, 'NoItems', 1), false));
  });

  describe('mode 2 (hide_completed)', () => {
    it('shows Produce (has a needed non-completed item)', () => assert.equal(groupIsVisible(items, 'Produce', 2), true));
    it('hides Dairy (only not_needed)',                   () => assert.equal(groupIsVisible(items, 'Dairy',   2), false));
    it('hides Frozen (needed but completed)',             () => assert.equal(groupIsVisible(items, 'Frozen',  2), false));
    it('hides Empty (only not_needed)',                   () => assert.equal(groupIsVisible(items, 'Empty',   2), false));
    it('hides group with no items at all',                () => assert.equal(groupIsVisible(items, 'NoItems', 2), false));
  });
});

// ────────────────────────────────────────────────────────────────
// Recipes tab (T7-T9)
// ────────────────────────────────────────────────────────────────

const CHILI = { id: 'r1', name: 'Chili', enabled: false, order: 1, created_at: '2026-01-01T00:00:00Z' };
const TACOS = { id: 'r2', name: 'Tacos', enabled: true,  order: 2, created_at: '2026-01-02T00:00:00Z' };
const RECIPES = [CHILI, TACOS];

const beefChili = { id: 'i1', name: 'beef', group: 'No Group', state: 'needed', completed: false, order: 1, created_at: '2026-02-01T00:00:00Z', recipe_id: 'r1' };
const beefTacos = { id: 'i2', name: 'beef', group: 'No Group', state: 'needed', completed: false, order: 2, created_at: '2026-02-02T00:00:00Z', recipe_id: 'r2' };
const milk      = { id: 'i3', name: 'milk', group: 'Dairy',    state: 'check',  completed: false, order: 1, created_at: '2026-02-03T00:00:00Z' };
const ghost     = { id: 'i4', name: 'salt', group: 'No Group', state: 'check',  completed: false, order: 3, created_at: '2026-02-04T00:00:00Z', recipe_id: 'gone' };

describe('recipeById', () => {
  it('finds a recipe by id', () => assert.equal(recipeById(RECIPES, 'r2'), TACOS));
  it('returns null for an unknown id', () => assert.equal(recipeById(RECIPES, 'nope'), null));
  it('returns null rather than undefined, so callers can test with ===', () =>
    assert.strictEqual(recipeById([], 'r1'), null));
});

describe('recipesForRender', () => {
  it('sorts by order', () => {
    const out = recipesForRender([TACOS, CHILI]);
    assert.deepEqual(out.map(r => r.id), ['r1', 'r2']);
  });

  it('breaks an order tie by created_at, oldest first', () => {
    const a = { id: 'a', order: 1, created_at: '2026-03-02T00:00:00Z' };
    const b = { id: 'b', order: 1, created_at: '2026-03-01T00:00:00Z' };
    assert.deepEqual(recipesForRender([a, b]).map(r => r.id), ['b', 'a']);
  });

  it('does not mutate the source array', () => {
    const src = [TACOS, CHILI];
    recipesForRender(src);
    assert.deepEqual(src.map(r => r.id), ['r2', 'r1']);
  });

  it('returns [] for no recipes', () => assert.deepEqual(recipesForRender([]), []));
});

describe('isOwned', () => {
  it('true for an item with a recipe_id', () => assert.equal(isOwned(beefChili), true));
  it('false for a free item', () => assert.equal(isOwned(milk), false));
  it('false for an empty-string recipe_id (the omitempty wire form)', () =>
    assert.equal(isOwned({ id: 'x', recipe_id: '' }), false));
  it('false for a missing recipe_id key', () => assert.equal(isOwned({ id: 'x' }), false));
  it('false, not a throw, for null/undefined', () => {
    assert.equal(isOwned(null), false);
    assert.equal(isOwned(undefined), false);
  });
});

// AC-9.1's real unit gate: buildRow renders from this function, so this is
// where the derived display name is actually decided.
describe('recipeSuffix', () => {
  it('returns " (Chili)" for an item owned by a known recipe', () =>
    assert.equal(recipeSuffix(beefChili, RECIPES), ' (Chili)'));

  it('returns "" for a free item', () => assert.equal(recipeSuffix(milk, RECIPES), ''));

  it('returns "" for a dangling recipe_id, and does not throw (PM-1)', () => {
    // The transient this guards: a recipe deleted in another window, whose
    // items have not yet been dropped locally. Returning " ()" would render a
    // bare pair of parens on the row.
    assert.doesNotThrow(() => recipeSuffix(ghost, RECIPES));
    assert.equal(recipeSuffix(ghost, RECIPES), '');
  });

  it('returns "" when the recipe list is empty', () =>
    assert.equal(recipeSuffix(beefChili, []), ''));

  it('keeps the leading space, which displayName depends on', () =>
    assert.ok(recipeSuffix(beefChili, RECIPES).startsWith(' ')));

  it('does not mutate the item or the recipe list', () => {
    const item = { ...beefChili };
    const recipes = RECIPES.map(r => ({ ...r }));
    recipeSuffix(item, recipes);
    assert.deepEqual(item, beefChili);
    assert.deepEqual(recipes.map(r => r.name), ['Chili', 'Tacos']);
  });

  it('returns the name verbatim — escaping is the render site\'s job', () => {
    // AC-9.7 is enforced at the markup boundary (esc(suffix.trimStart())), not
    // here; escaping in both places would double-encode.
    assert.equal(recipeSuffix(beefChili, [{ ...CHILI, name: 'Chili <b>' }]), ' (Chili <b>)');
  });
});

describe('ownedTooltip — names the owner, never offers an action', () => {
  it('names the owning recipe', () =>
    assert.equal(ownedTooltip(beefChili, RECIPES), 'Belongs to recipe Chili'));

  it('returns "" for a free item, so free rows get no tooltip at all', () =>
    assert.equal(ownedTooltip(milk, RECIPES), ''));

  it('returns "" for a dangling recipe_id rather than a truncated sentence', () => {
    // Same transient recipeSuffix guards: a recipe deleted in another window
    // whose items have not been dropped locally yet. "Belongs to recipe " with
    // nothing after it is worse than no tooltip, and buildRow's `if (tip)`
    // guard depends on this returning falsy.
    assert.doesNotThrow(() => ownedTooltip(ghost, RECIPES));
    assert.equal(ownedTooltip(ghost, RECIPES), '');
  });

  it('returns "" when the recipe list is empty', () =>
    assert.equal(ownedTooltip(beefChili, []), ''));

  it('says nothing about deleting, removing or trash', () => {
    // The regression this pins is the reported one: the old copy read as a
    // one-click delete offer on a row that has no delete control.
    assert.doesNotMatch(ownedTooltip(beefChili, RECIPES), /delete|remove|trash/i);
  });

  it('passes the name through verbatim — no esc(), because .title is not markup', () => {
    const evil = { id: 'x', name: 'beef', recipe_id: 'r-b' };
    const recs = [{ id: 'r-b', name: 'Chili <b>' }];
    assert.equal(ownedTooltip(evil, recs), 'Belongs to recipe Chili <b>');
  });
});

describe('displayName', () => {
  it('composes "beef (Chili)"', () => assert.equal(displayName(beefChili, RECIPES), 'beef (Chili)'));
  it('leaves a free item as "milk"', () => assert.equal(displayName(milk, RECIPES), 'milk'));
  it('drops the suffix for a dangling recipe_id', () =>
    assert.equal(displayName(ghost, RECIPES), 'salt'));
  it('equals item.name + recipeSuffix(...) by definition', () => {
    [beefChili, beefTacos, milk, ghost].forEach(i =>
      assert.equal(displayName(i, RECIPES), i.name + recipeSuffix(i, RECIPES)));
  });
  it('distinguishes two same-named items owned by different recipes', () => {
    // The user's own case: beef (Chili) and beef (Tacos) are separate items.
    assert.notEqual(displayName(beefChili, RECIPES), displayName(beefTacos, RECIPES));
  });
});

describe('ingredientsForRecipe', () => {
  const items = [ghost, beefTacos, milk, beefChili];

  it('filters to the recipe\'s own items', () =>
    assert.deepEqual(ingredientsForRecipe(items, 'r1').map(i => i.id), ['i1']));

  it('excludes free items and other recipes\' items', () => {
    const out = ingredientsForRecipe(items, 'r2');
    assert.deepEqual(out.map(i => i.id), ['i2']);
  });

  it('sorts by created_at, oldest first', () => {
    const late  = { id: 'z', recipe_id: 'r1', created_at: '2026-05-01T00:00:00Z' };
    const early = { id: 'a', recipe_id: 'r1', created_at: '2026-01-01T00:00:00Z' };
    assert.deepEqual(ingredientsForRecipe([late, early], 'r1').map(i => i.id), ['a', 'z']);
  });

  it('returns [] for an unknown recipe id', () =>
    assert.deepEqual(ingredientsForRecipe(items, 'nope'), []));

  it('does not mutate the source array', () => {
    const src = [ghost, beefTacos, milk, beefChili];
    ingredientsForRecipe(src, 'r1');
    assert.deepEqual(src.map(i => i.id), ['i4', 'i2', 'i3', 'i1']);
  });
});

// AC-9.4 — the relabel is UI-only. NO_GROUP stays 'No Group' everywhere it is
// stored, so drag targets and moveItem keep resolving.
describe('groupLabel', () => {
  it('relabels the virtual bucket as Unallocated', () =>
    assert.equal(groupLabel('No Group'), 'Unallocated'));
  it('leaves a real group name alone', () => assert.equal(groupLabel('Dairy'), 'Dairy'));
  it('is not applied in reverse — "Unallocated" as a real group name passes through', () =>
    assert.equal(groupLabel('Unallocated'), 'Unallocated'));
  it('does not relabel on a case difference', () =>
    assert.equal(groupLabel('no group'), 'no group'));
});

describe('groupEmptyHint', () => {
  it('reads "Nothing unallocated" for the virtual bucket', () =>
    assert.equal(groupEmptyHint('No Group'), 'Nothing unallocated'));
  it('reads "No items" for a real group', () =>
    assert.equal(groupEmptyHint('Dairy'), 'No items'));
});

describe('groupsForRender with owned items present', () => {
  it('appends the virtual bucket when an owned item landed there', () => {
    // An ingredient added from the Recipes tab lands in No Group, which is what
    // makes the bucket appear without the user creating a group first.
    assert.deepEqual(groupsForRender(['Dairy'], [beefChili, milk]), ['Dairy', 'No Group']);
  });

  it('omits the virtual bucket when nothing is unallocated', () =>
    assert.deepEqual(groupsForRender(['Dairy'], [milk]), ['Dairy']));

  it('appends it last, so Unallocated renders at the bottom', () => {
    const out = groupsForRender(['Dairy', 'Produce'], [beefChili]);
    assert.equal(out[out.length - 1], 'No Group');
  });
});

// R-19: this exercises a test-local copy, so it is SUPPORTING EVIDENCE, not a
// gate. #progress-bar is deliberately absent from the table — setActiveTab does
// not route that element through this helper (R3-4); progressBarVisible below
// is the bar's actual gate.
describe('tabControlVisible (supporting evidence, not a gate)', () => {
  it('hides the eye button on Recipes', () =>
    assert.equal(tabControlVisible('#hide-not-needed-btn', 'recipes'), false));
  it('hides reset on Recipes', () =>
    assert.equal(tabControlVisible('#reset-btn', 'recipes'), false));
  it('hides groups on Recipes', () =>
    assert.equal(tabControlVisible('#groups-btn', 'recipes'), false));
  it('shows all three on Grocery', () => {
    ['#hide-not-needed-btn', '#reset-btn', '#groups-btn'].forEach(id =>
      assert.equal(tabControlVisible(id, 'grocery'), true));
  });
  it('leaves any control outside the table visible on both tabs', () => {
    ['#collapse-all-btn', '#expand-all-btn', '#sync-toggle'].forEach(id => {
      assert.equal(tabControlVisible(id, 'grocery'), true);
      assert.equal(tabControlVisible(id, 'recipes'), true);
    });
  });
  it('does not govern #progress-bar', () => {
    // If this ever returns false for Recipes, the helper has silently taken
    // ownership of the bar and progressBarVisible has been bypassed.
    assert.equal(tabControlVisible('#progress-bar', 'recipes'), true);
  });
});

// AC-8.4's gate (R3-4): renderProgressBar calls the shipped helper rather than
// repeating the condition, so this is the same logic that ships.
describe('progressBarVisible', () => {
  it('hidden on both tabs when progress is off — the default, and R-11\'s regression case', () => {
    assert.equal(progressBarVisible(false, 5, 'grocery'), false);
    assert.equal(progressBarVisible(false, 5, 'recipes'), false);
  });
  it('visible on Grocery with progress on and items present', () =>
    assert.equal(progressBarVisible(true, 5, 'grocery'), true));
  it('hidden on Recipes even with progress on and items present (F-8)', () =>
    assert.equal(progressBarVisible(true, 5, 'recipes'), false));
  it('hidden on both tabs when there are no items', () => {
    assert.equal(progressBarVisible(true, 0, 'grocery'), false);
    assert.equal(progressBarVisible(true, 0, 'recipes'), false);
  });
  it('returns a boolean, never a truthy config value', () => {
    // showProgress arrives from config and may be any truthy value; the !! is
    // load-bearing because the caller passes the result straight to setHidden.
    assert.strictEqual(progressBarVisible('yes', 1, 'grocery'), true);
    assert.strictEqual(progressBarVisible(undefined, 1, 'grocery'), false);
  });
  it('treats a negative count as no items', () =>
    assert.equal(progressBarVisible(true, -1, 'grocery'), false));
});

// AC-4 mirrored on the client for the optimistic update. The server
// (store.go, PatchRecipe) is the authority; this must agree with it.
describe('applyRecipeToggle', () => {
  const items = [beefChili, beefTacos, milk];

  it('enabling sets every owned item to needed (AC-4.1)', () => {
    const out = applyRecipeToggle(items, 'r1', true);
    assert.equal(out.find(i => i.id === 'i1').state, 'needed');
  });

  it('disabling sets every owned item to not_needed (AC-4.2)', () => {
    const out = applyRecipeToggle(items, 'r1', false);
    assert.equal(out.find(i => i.id === 'i1').state, 'not_needed');
  });

  it('clears completed in both directions, matching store.go (AC-4.1/4.2)', () => {
    const done = [{ ...beefChili, completed: true }];
    assert.equal(applyRecipeToggle(done, 'r1', true)[0].completed,  false);
    assert.equal(applyRecipeToggle(done, 'r1', false)[0].completed, false);
  });

  it('toggling recipe A never touches an item owned by recipe B (AC-4.3, AC-4.5)', () => {
    // The user's case: disabling Chili leaves beef (Tacos) at needed.
    const out = applyRecipeToggle(items, 'r1', false);
    assert.equal(out.find(i => i.id === 'i2').state, 'needed');
  });

  it('never touches a free item (AC-4.4)', () => {
    const out = applyRecipeToggle(items, 'r1', false);
    assert.equal(out.find(i => i.id === 'i3').state, 'check');
  });

  it('returns untouched items by reference, so only owned rows re-render', () => {
    const out = applyRecipeToggle(items, 'r1', false);
    assert.strictEqual(out.find(i => i.id === 'i3'), milk);
  });

  it('does not mutate the source array or its items', () => {
    const src = [{ ...beefChili, completed: true }];
    applyRecipeToggle(src, 'r1', false);
    assert.equal(src[0].state, 'needed');
    assert.equal(src[0].completed, true);
  });

  it('is a no-op for an unknown recipe id', () => {
    const out = applyRecipeToggle(items, 'nope', true);
    assert.deepEqual(out.map(i => i.state), items.map(i => i.state));
  });

  it('never rewrites item.name (AC-9.2, supporting evidence only)', () => {
    // AC-9.2's real gate is the handler test "rename leaves items untouched",
    // which exercises the real store; this tests a test-local copy.
    const out = applyRecipeToggle(items, 'r1', true);
    assert.deepEqual(out.map(i => i.name), ['beef', 'beef', 'milk']);
  });
});

describe('applyReset with recipes (A-2)', () => {
  it('switches every recipe off', () => {
    const { recipes } = applyReset([], [CHILI, TACOS]);
    assert.deepEqual(recipes.map(r => r.enabled), [false, false]);
  });

  it('leaves an already-disabled recipe disabled', () => {
    const { recipes } = applyReset([], [CHILI]);
    assert.equal(recipes[0].enabled, false);
  });

  it('does not mutate the source recipes', () => {
    const src = [{ ...TACOS }];
    applyReset([], src);
    assert.equal(src[0].enabled, true);
  });

  it('defaults to no recipes, so the existing item-only calls still work', () => {
    const { items, recipes } = applyReset([milk]);
    assert.equal(items.length, 1);
    assert.deepEqual(recipes, []);
  });

  it('leaves no recipe enabled while all its ingredients read Check', () => {
    // The invariant A-2 exists for: an enabled recipe whose items are all
    // Check is a state the UI cannot explain.
    const { items, recipes } = applyReset([beefChili, beefTacos], [CHILI, TACOS]);
    assert.ok(items.every(i => i.state === 'check'));
    assert.ok(recipes.every(r => !r.enabled));
  });
});

// ────────────────────────────────────────────────────────────────
// Mirror integrity + AC-9.7 static half
//
// Everything below inspects the SHIPPED app.js source read into APP_SRC at the
// top of this file. R-19's objection — that a test exercising an inline copy
// cannot prove what ships — does not apply here: there is no copy involved, so
// a regression in app.js fails this suite directly.
// ────────────────────────────────────────────────────────────────

describe('mirror integrity', () => {
  // Why this block exists: the inline-copy convention has already failed four
  // times in this file (removeGroup at :58 vs app.js's own removeGroup is the
  // clearest case). "Keep these in sync" as a discipline has a measured 0%
  // success rate here. The four pre-existing drifts are deliberately NOT
  // retrofitted — that is churn in a file the other branch may touch — but
  // nothing the Recipes tab added may drift silently.
  //
  // The comparison is normalized for LEADING INDENTATION ONLY (see `norm`),
  // because app.js's helpers sit inside the IIFE at 2-space indent while these
  // mirrors sit at column 0. A raw APP_SRC.includes(fn.toString()) is false for
  // every multi-line helper and would fail on a correct implementation.
  const mirrored = [
    recipeById, recipesForRender, ingredientsForRecipe, isOwned, recipeSuffix,
    displayName, ownedTooltip, groupLabel, groupEmptyHint, tabControlVisible,
    progressBarVisible, applyRecipeToggle, recipeNameTaken, nextRecipeOrder,
  ];

  mirrored.forEach(fn => {
    it(`${fn.name} has not drifted from app.js`, () => {
      assert.ok(APP_N.includes(norm(fn.toString())),
        `${fn.name} drifted from app.js — the two copies are no longer identical`);
    });
  });

  it('covers every helper the Recipes tab added', () => {
    // Counting the array three lines up would be a property of this file, not
    // of app.js: it cannot fail unless someone edits the assertion itself. Each
    // mirrored helper carries a `// mirrored in app.test.js ::` tag at its
    // definition, so counting the tags in the SHIPPED source makes a twelfth
    // helper added without a mirror turn this red — the one thing the name
    // promises to catch.
    const tags = (APP_SRC.match(/mirrored in app\.test\.js ::/g) || []).length;
    assert.equal(mirrored.length, tags,
      `app.js tags ${tags} helpers as mirrored, but this file mirrors ${mirrored.length}`);
  });
});

describe('AC-8.3 static half — switching tabs neither refetches nor reconnects', () => {
  // The only prior evidence for AC-8.3 was a DevTools network-panel check in
  // T7's per-task commands. That is a real check, but nothing re-runs it, and
  // AC-8.3 has no numbered section 5.3 step. This gives it a gate that runs on
  // every suite invocation, over the SHIPPED source.
  //
  // It is a floor, not a proof: it proves setActiveTab issues no network call
  // directly. A refetch reached indirectly through a helper would need the
  // DevTools check to catch, which is why step 7 stays in the handback.
  const body = (() => {
    const start = APP_SRC.indexOf('function setActiveTab(tab) {');
    assert.notEqual(start, -1, 'setActiveTab not found — this gate has lost its target');
    // Anchor on the next top-level declaration, not on the first 2-space '}'.
    // The latter happens to land on setActiveTab's own brace today only because
    // every nested block inside it closes at 4-space indent; one 2-space-indented
    // '}' would truncate the body and make every doesNotMatch below pass
    // vacuously over a shorter string.
    const after = APP_SRC.slice(start + 1);
    const rel   = after.search(/\n  (?:function |const |let |\/\/)/);
    return after.slice(0, rel === -1 ? undefined : rel);
  })();

  it('setActiveTab issues no fetch and no api() call (AC-8.3, first half)', () => {
    // The deny-list is setActiveTab's own doc comment, verbatim: every loader
    // it names is listed here. An earlier version covered only three of them,
    // so a refreshAll() dropped into the body — which refetches items, recipes
    // AND config on every switch — sailed through this gate.
    assert.doesNotMatch(body,
      /\bfetch\s*\(|\bapi\s*\(|fetchItems\w*\s*\(|fetchRecipesData\s*\(|loadConfig\w*\s*\(|refreshAll\s*\(/,
      'setActiveTab refetches on a tab switch');
  });

  it('setActiveTab does not touch EventSource (AC-8.3, second half)', () => {
    assert.doesNotMatch(body, /EventSource|\bes\b\s*=|connectSSE\s*\(/,
      'setActiveTab reconnects SSE on a tab switch');
  });

  it('setActiveTab still repaints, or the switch would show stale content', () => {
    assert.match(body, /\n\s*render\(\);/);
  });

  it('no fetchRecipes() call site was reintroduced (T7 R3-1)', () => {
    // fetchRecipesData() is the shipped name; a bare fetchRecipes() was r1's
    // per-switch refetch, removed in T7.
    assert.equal((APP_SRC.match(/[^a-zA-Z]fetchRecipes\(\)/g) || []).length, 0);
  });
});

describe('AC-9.3 static half — the delete control is conditional on ownership', () => {
  // Verified live during T9 against a DOM shim, but that harness is a
  // scratchpad file and does not ship. This keeps the property gated by
  // something that runs on every suite invocation.
  it('buildRow emits .delete-btn only for an unowned item', () => {
    assert.match(APP_SRC, /\$\{owned \? '' : `<button class="delete-btn"/,
      'the delete button is no longer conditional on ownership');
  });

  it('the button was made conditional, not deleted — free rows still get one', () => {
    assert.equal((APP_SRC.match(/class="delete-btn"/g) || []).length, 1);
  });

  it('buildRow titles an owned row with ownedTooltip, guarded on the result', () => {
    // Guarded on `tip`, not on `owned`: an owned item whose recipe is not in
    // the local list yields '', and `if (owned)` would set title=''.
    assert.match(APP_SRC, /const tip = ownedTooltip\(item, recipes\);\s*\n\s*if \(tip\) li\.title = tip;/);
  });

  it('no tooltip anywhere on the row mentions deleting', () => {
    // The generalising gate. Pinning the exact string only detects change, not
    // badness — it would have protected the original copy ("Delete this
    // ingredient from its recipe") indefinitely. The row has no delete control;
    // nothing on it should name one.
    const titles = [...APP_SRC.matchAll(/li\.title = '([^']*)'/g)].map(m => m[1]);
    titles.forEach(t => assert.doesNotMatch(t, /delete|remove|trash/i,
      `an <li> tooltip mentions deleting: ${t}`));
  });

  it('the chip is the merged .recipe-chip.item-recipe-suffix button (AC-9.1, AC-9.6)', () => {
    assert.equal((APP_SRC.match(/class="recipe-chip item-recipe-suffix"/g) || []).length, 1);
  });

  it('the separator space sits OUTSIDE the chip button (R5-4)', () => {
    // A <button> is inline-block; leading collapsible whitespace at the start
    // of an inline formatting context is dropped, so a space inside the button
    // renders as "beef(Chili)".
    assert.match(APP_SRC, /\? ` <button class="recipe-chip item-recipe-suffix"/);
  });
});

describe('AC-9.7 static half — escaping in the shipped source', () => {
  // Lines that interpolate a recipe name RAW, i.e. without esc().
  const rawNameLines = APP_SRC.split('\n')
    .map((line, i) => ({ line, n: i + 1 }))
    .filter(({ line }) => /\$\{\s*(r|recipe|patched\.recipe)\.name\s*\}/.test(line));

  it('no raw recipe name is interpolated into markup', () => {
    // The scope is markup, not every template literal. recipeSuffix() legitimately
    // builds " (Chili)" raw because it returns PLAIN TEXT that buildRow escapes at
    // its single markup boundary; a blanket ban cannot tell that apart from an
    // injection hole, and would fail on correct code. A raw name on a line that
    // also emits a tag is the actual defect this guards.
    const inMarkup = rawNameLines.filter(({ line }) => line.includes('<'));
    assert.deepEqual(inMarkup.map(({ n }) => n), [],
      `recipe name interpolated into markup without esc() at line(s) ${inMarkup.map(x => x.n)}`);
  });

  it('exactly two raw interpolations exist, both plain text, both enumerated', () => {
    // A closed list, not a count. Both are plain-text producers, and both are
    // safe for a DIFFERENT reason, which is why each is pinned to its own
    // pattern rather than waved through by a bumped number:
    //
    //   recipeSuffix  — returns " (Chili)" to buildRow, which escapes it at the
    //                   single markup boundary with esc(suffix.trimStart()).
    //   ownedTooltip  — returns "Belongs to recipe Chili", assigned to the
    //                   li.title PROPERTY. A property assignment is not a markup
    //                   boundary; the DOM stores the string literally, so esc()
    //                   here would render the entities visibly in the tooltip.
    //   syncCreateRecipe — builds "Chili (2)" as the replay retry name. It goes
    //                   through JSON.stringify into a request BODY, never near
    //                   innerHTML; esc() here would send visible entities to
    //                   the server as the recipe's actual name.
    //
    // Adding a fourth raw interpolation must fail this until someone states
    // which of those arguments it relies on.
    const patterns = [
      /return r \? ` \(\$\{r\.name\}\)` : '';/,
      /return r \? `Belongs to recipe \$\{r\.name\}` : '';/,
      /attempt === 1 \? r\.name : `\$\{r\.name\} \(\$\{attempt\}\)`;/,
    ];
    assert.equal(rawNameLines.length, patterns.length,
      `expected exactly ${patterns.length} raw ${'${r.name}'} lines; found ` +
      `${rawNameLines.length}: ${rawNameLines.map(x => x.n)}`);
    patterns.forEach(p => assert.ok(rawNameLines.some(({ line }) => p.test(line)),
      `no raw interpolation matches ${p} — the enumerated list is stale`));
  });

  it('the tooltip is assigned to a property, which is what makes it safe', () => {
    // The safety argument above is only true while the value goes to .title.
    // Interpolate ownedTooltip's result into markup and it becomes a hole.
    assert.match(APP_SRC, /if \(tip\) li\.title = tip;/);
    assert.doesNotMatch(APP_SRC, /\$\{\s*(tip|ownedTooltip\()/,
      'the tooltip value reaches markup — it is unescaped and must not');
  });

  it('the Grocery-tab suffix is escaped at its markup boundary', () => {
    assert.match(APP_SRC, /\$\{esc\(suffix\.trimStart\(\)\)\}/,
      'buildRow no longer escapes the suffix it renders');
  });

  it('the Recipes-tab card title is escaped', () => {
    assert.match(APP_SRC, /const rname\s*=\s*esc\(r\.name\)/,
      'renderRecipesTab no longer binds an escaped recipe name');
  });

  it('revealRecipe wraps its id-derived selector in CSS.escape', () => {
    assert.match(APP_SRC, /CSS\.escape\(\s*recipeId\s*\)/,
      'revealRecipe selector missing CSS.escape');
  });

  it('the focus-restore selector wraps its id in CSS.escape', () => {
    assert.match(APP_SRC, /CSS\.escape\(\s*focusRecipeId\s*\)/,
      'focus-restore selector missing CSS.escape');
  });
});

describe('recipeNameTaken — the client applies the server\'s own two rules', () => {
  const recipes = [
    { id: 'r1', name: 'Chili' },
    { id: 'r2', name: 'Tacos' },
  ];

  it('an unused name is free', () => {
    assert.equal(recipeNameTaken(recipes, 'Curry'), false);
  });

  it('an exact duplicate is taken', () => {
    assert.equal(recipeNameTaken(recipes, 'Chili'), true);
  });

  it('matches case-insensitively, as strings.EqualFold does', () => {
    assert.equal(recipeNameTaken(recipes, 'CHILI'), true);
    assert.equal(recipeNameTaken(recipes, 'cHiLi'), true);
  });

  it('trims before comparing, as strings.TrimSpace does', () => {
    assert.equal(recipeNameTaken(recipes, '  Chili  '), true);
  });

  it('exceptId lets a recipe keep its own name (the rename no-op)', () => {
    assert.equal(recipeNameTaken(recipes, 'Chili', 'r1'), false);
    assert.equal(recipeNameTaken(recipes, 'chili', 'r1'), false);
  });

  it('exceptId does not excuse clashing with a DIFFERENT recipe', () => {
    assert.equal(recipeNameTaken(recipes, 'Tacos', 'r1'), true);
  });

  it('an empty list takes nothing', () => {
    assert.equal(recipeNameTaken([], 'Chili'), false);
  });
});

describe('nextRecipeOrder — matches AddRecipe, not recipes.length', () => {
  it('an empty list starts at 0', () => {
    assert.equal(nextRecipeOrder([]), 0);
  });

  it('a contiguous list yields length, so the common case is unchanged', () => {
    assert.equal(nextRecipeOrder([{ order: 0 }, { order: 1 }, { order: 2 }]), 3);
  });

  it('R-arch: a gap left by a middle delete does NOT collide', () => {
    // This is the whole reason the helper exists. Orders 0 and 2 with the
    // middle recipe deleted: length would return 2 and collide with the tail,
    // leaving the sort tie-break to pick the display position silently.
    const afterMiddleDelete = [{ order: 0 }, { order: 2 }];
    assert.equal(afterMiddleDelete.length, 2);
    assert.equal(nextRecipeOrder(afterMiddleDelete), 3);
  });

  it('is independent of iteration order, as the Go map loop must be', () => {
    const a = [{ order: 0 }, { order: 5 }, { order: 2 }];
    assert.equal(nextRecipeOrder(a), 6);
    assert.equal(nextRecipeOrder([...a].reverse()), 6);
    assert.equal(nextRecipeOrder([{ order: 2 }, { order: 0 }, { order: 5 }]), 6);
  });
});

describe('the create/rename paths actually consult recipeNameTaken', () => {
  // Behavioural coverage above proves the rule; these pin that the rule is
  // WIRED IN. A correct helper nothing calls is the displayName failure mode.
  it('createRecipe refuses blanks and duplicates before pushing', () => {
    assert.match(APP_N, /if \(!name \|\| recipeNameTaken\(recipes, name\)\) return false;/);
  });

  it('renameRecipe excepts the recipe being renamed', () => {
    assert.match(APP_N, /if \(recipeNameTaken\(recipes, name, id\)\) return;/);
  });

  it('the shared footer keeps the text when a recipe name is refused', () => {
    assert.match(APP_N, /if \(recipeNameTaken\(recipes, name\)\) \{ newInput\.select\(\); return; \}/);
  });

  it('createRecipe orders by nextRecipeOrder, never by recipes.length', () => {
    assert.match(APP_N, /order: nextRecipeOrder\(recipes\),/);
    assert.doesNotMatch(APP_N, /order: recipes\.length/);
  });
});

describe('addIngredient leaves the input it was typed in empty', () => {
  // The bug this pins: deleting recipeDrafts[recipeId] is NOT sufficient.
  // renderRecipesTab's step 1 re-reads document.activeElement, and on the Enter
  // path that element is the very input just submitted from — still holding the
  // text. Step 1 re-captures it, step 3 restores it, and the ingredient the user
  // just added is sitting in the box inviting them to add it twice. Clicking the
  // add button never showed it, because there the active element is the button.
  // So the gate has to assert the LIVE ELEMENT is blanked, not just the draft.
  const body = (() => {
    const start = APP_SRC.indexOf('async function addIngredient(recipeId, name) {');
    assert.notEqual(start, -1, 'addIngredient not found — this gate has lost its target');
    const after = APP_SRC.slice(start + 1);
    const rel   = after.search(/\n  (?:async function |function |const |let |\/\/)/);
    return after.slice(0, rel === -1 ? undefined : rel);
  })();

  it('blanks the live input, not only the draft entry', () => {
    assert.match(norm(body), /if \(live\) live\.value = '';/);
  });

  it('does so BEFORE render(), so the wipe cannot re-capture the text', () => {
    const n = norm(body);
    const blank = n.indexOf("live.value = ''");
    const paint = n.indexOf('render()');
    // Both anchors asserted present first: indexOf returns -1 when the blanking
    // line is gone, and -1 < paint is true, so a bare ordering compare would
    // pass vacuously on exactly the code this gate exists to catch.
    assert.notEqual(blank, -1, 'the blanking line is gone entirely');
    assert.notEqual(paint, -1, 'render() call not found — this gate has lost its target');
    assert.ok(blank < paint,
      'the input is blanked after render() — step 1 has already re-captured the draft by then');
  });

  it('still clears the draft and claims focus', () => {
    const n = norm(body);
    assert.match(n, /delete recipeDrafts\[recipeId\];/);
    assert.match(n, /focusRecipeId = recipeId;/);
  });
});

describe('header layout — the tabs do not move when the tab changes', () => {
  // Reported from manual use: the Grocery/Recipes buttons sat beside the title
  // on one tab and drifted toward centre on the other. Cause: .app-header is
  // justify-content: space-between, and the only thing holding the tabs left
  // was #progress-bar { flex: 1 } absorbing the free space. Switching to
  // Recipes hides that bar and three icon buttons, so nothing absorbed it.
  //
  // Source-level, like every other gate here — layout cannot be measured
  // without a real browser, and jsdom does not compute flex anyway.
  // Comments are stripped BEFORE slicing: a rule's own explanatory comment can
  // contain a brace (the .tab-bar comment quotes `#progress-bar { flex: 1 }`),
  // which truncates a naive indexOf('}') slice and makes the gate read prose
  // instead of declarations. Selector spacing is matched with \s*, because
  // this file aligns some selectors with two spaces before the brace.
  const CSS_N = CSS_SRC.replace(/\/\*[\s\S]*?\*\//g, '');
  const block = (sel) => {
    const m = CSS_N.match(new RegExp(sel.replace(/[.\-]/g, '\\$&') + '\\s*\\{([^}]*)\\}'));
    assert.notEqual(m, null, `${sel} rule not found — this gate has lost its target`);
    return m[1];
  };

  it('.tab-bar absorbs the header free space itself', () => {
    assert.match(block('.tab-bar'), /margin-right:\s*auto;/,
      '.tab-bar no longer pins itself left — the tabs will drift when the ' +
      'progress bar is hidden');
  });

  it('the fix does not depend on the progress bar being present', () => {
    // The bug reproduces on BOTH tabs under the default progress:false, so a
    // gate that only holds while #progress-bar exists would miss the real case.
    // .tab-bar must not have gained a flex-grow that only works alongside it.
    assert.doesNotMatch(block('.tab-bar'), /flex-grow|flex:\s*[1-9]/,
      '.tab-bar grows, which competes with #progress-bar instead of yielding');
  });

  it('.header-left and .header-right still refuse to shrink', () => {
    // If either could shrink, the auto margin would steal from them rather
    // than from the gap, and the title or the Sync toggle would compress.
    assert.match(block('.header-left'),  /flex-shrink:\s*0/);
    assert.match(block('.header-right'), /flex-shrink:\s*0/);
  });
});

describe('offline queue — edits made with sync off replay on reconnect', () => {
  // The reported failure: reconnecting after offline edits ran refreshAll()
  // alone, which adopted the server's stale state wholesale and destroyed
  // every local edit. Newest-wins means the queued edits are pushed UP first
  // and only then is the authoritative state re-read.
  it('the sync toggle replays the queue BEFORE refreshing', () => {
    const start = APP_SRC.indexOf("syncTog.addEventListener('change'");
    assert.notEqual(start, -1, 'sync toggle handler not found — this gate has lost its target');
    const body   = APP_SRC.slice(start, APP_SRC.indexOf('});', start));
    const replay = body.indexOf('await replayPendingOps()');
    const fresh  = body.indexOf('await refreshAll()');
    assert.notEqual(replay, -1, 'the reconnect path no longer replays pendingOps');
    assert.notEqual(fresh,  -1, 'refreshAll() call not found — this gate has lost its target');
    assert.ok(replay < fresh,
      'refreshAll() runs before the replay — stale server state overwrites offline edits');
  });

  it('replayPendingOps drains with a while loop, not a one-pass for', () => {
    assert.match(APP_N, /while \(pendingOps\.length\) \{/);
  });

  it('every offline mutation queues instead of silently dropping its sync step', () => {
    // One arm per mutation family; the shape `if (syncEnabled) await sync();
    // else pendingOps.push(sync);` is the wiring this pins. Counting the else
    // branches catches a mutation reverted to the old fire-and-forget-or-drop.
    const queued = (APP_N.match(/else pendingOps\.push\(sync\);/g) || []).length;
    assert.ok(queued >= 12,
      `only ${queued} mutations queue when offline — one has lost its else branch`);
  });

  it('a GET refresh preserves local-only (not yet synced) entities', () => {
    // refreshAll runs on every SSE tick from ANY client; a wholesale
    // `items = data` here is exactly the wipe the offline work removed.
    assert.match(APP_N, /mergeServerItems\(data \|\| \[\]\);/);
    assert.match(APP_N, /mergeServerRecipes\(data \|\| \[\]\);/);
    assert.doesNotMatch(APP_N, /items = data \|\| \[\];/);
    assert.doesNotMatch(APP_N, /recipes = data \|\| \[\];/);
  });

  it('adoptRecipePatch merges items rather than replacing the array', () => {
    assert.doesNotMatch(APP_N, /if \(Array\.isArray\(patched\.items\)\) items = patched\.items;/,
      'adoptRecipePatch replaces items wholesale — pending local creates are destroyed');
    assert.match(APP_N, /if \(Array\.isArray\(patched\.items\)\) mergeServerItems\(patched\.items\);/);
  });

  it('create-then-delete offline cancels out instead of replaying a dead POST', () => {
    assert.match(APP_N, /if \(r\._deleted\) return;/);
    assert.match(APP_N, /if \(it\._deleted\) return;/);
  });

  it('the recipes tab is no longer locked while offline (AC-10.1 retired)', () => {
    // The user asked for offline recipe editing; the old lockout disabled
    // every recipe control and the shared footer submit when sync was off.
    const start = APP_SRC.indexOf('function updateRecipeControlsDisabled() {');
    assert.notEqual(start, -1, 'updateRecipeControlsDisabled not found — this gate has lost its target');
    const after = APP_SRC.slice(start + 1);
    const rel   = after.search(/\n  (?:async function |function |const |let )/);
    const body  = after.slice(0, rel === -1 ? undefined : rel);
    assert.doesNotMatch(body, /!syncEnabled/,
      'recipe controls are gated on sync again — offline recipe edits are locked out');
    assert.match(body, /el\.dataset\.edge === '1'/,
      'the move-button edge state must survive the lockout removal');
  });
});

describe('move-to-group button — a tap alternative to dragging across groups', () => {
  // Reported pain point: dragging an item out of the bottom-most Unallocated
  // group to a named group is fiddly on a small touch screen. Every row gets
  // a .move-btn that opens a plain list of the OTHER groups; picking one
  // calls moveItem with the same destIds shape a cross-group drop already
  // used (append to the destination group's id list), verified live in
  // smoke-move.mjs against a real server. This block pins the static wiring.
  it('buildRow emits a .move-btn on every row, independent of ownership', () => {
    assert.match(APP_N,
      /<button class="move-btn" data-id="\$\{item\.id\}" title="Move to a different group"/);
    assert.equal((APP_SRC.match(/class="move-btn"/g) || []).length, 1,
      'the move button markup should appear once, in buildRow, not duplicated elsewhere');
  });

  it('the move button sits alongside delete, not inside its conditional', () => {
    // Must NOT be nested in the `owned ? '' : ...` branch — an owned
    // (recipe-linked) item still needs to be movable between groups.
    const start = APP_SRC.indexOf('class="move-btn"');
    const ownedTernary = APP_SRC.indexOf('${owned ? \'\' : `<button class="delete-btn"');
    assert.ok(start !== -1 && ownedTernary !== -1 && start < ownedTernary,
      'move-btn must render before, and outside of, the ownership-gated delete button');
  });

  it('openMoveModal excludes the item\'s current group from the destination list', () => {
    assert.match(APP_N, /const dest = \[\.\.\.groups, NO_GROUP\]\.filter\(g => g !== item\.group\);/);
  });

  it('picking a destination appends the item to that group\'s end, then calls moveItem', () => {
    // Same shape as pointerEnd's cross-group drop branch: read the destination
    // group's current ids, push the moved id on the end, hand both to moveItem.
    assert.match(APP_N, /const destIds = itemsForGroup\(targetGroup\)\.map\(i => i\.id\);\s*destIds\.push\(id\);\s*moveItem\(id, targetGroup, destIds\);/);
  });

  it('the delegated list click handler wires .move-btn to openMoveModal', () => {
    assert.match(APP_N, /if \(move\)\s*\{ openMoveModal\(move\.dataset\.id\); return; \}/);
  });

  it('the modal closes on backdrop click and on Cancel, without moving anything', () => {
    assert.match(APP_N,
      /if \(e\.target === moveModal \|\| e\.target\.closest\('#move-modal-cancel'\)\) \{\s*closeMoveModal\(\);\s*return;\s*\}/);
  });

  it('.item-row grid still has exactly 4 tracks — move+delete share the last one', () => {
    // Regression guard for the grid-column landmine: adding a 5th DOM child
    // (move-btn) without also widening grid-template-columns would misalign
    // the actions column on rows where delete-btn is absent (owned items),
    // since grid assigns tracks by DOM order, not by column identity.
    const m = CSS_SRC.match(/\.item-row\s*\{[^}]*grid-template-columns:\s*([^;]+);/);
    assert.ok(m, 'grid-template-columns not found on .item-row');
    assert.equal(m[1].trim().split(/\s+/).length, 4,
      `expected 4 grid tracks, found: ${m[1].trim()}`);
    assert.match(APP_N, /<span class="item-actions">/,
      'move-btn and delete-btn must be wrapped in one flex container occupying the 4th track');
  });
});
