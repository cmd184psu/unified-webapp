(() => {
  'use strict';

  const STATES       = ['needed', 'check', 'not_needed'];
  const STATE_LABELS = { needed: 'Needed', check: 'Check', not_needed: 'Not Needed' };
  const NO_GROUP     = 'No Group';

  let items               = [];
  let groups              = [];   // real groups only; NO_GROUP is virtual
  let syncEnabled         = true;
  let collapsedGroups     = {};
  // 0=show_all  1=hide_not_needed  2=hide_completed
  let visibilityMode      = 0;
  let showProgress        = false;
  let syncIntervalSeconds = 1;    // configurable via ~/.grocery.json
  let listTitle           = 'Grocery List'; // configurable via ~/.grocery.json

  let recipes          = [];
  let activeTab        = 'grocery';
  // Deferred closures for mutations made while offline (syncEnabled === false).
  // Each closure reads its target object's CURRENT fields at replay time, not
  // at queue time, so a later edit to something still queued is what actually
  // reaches the server — "newest wins" falls out of that for free, with no
  // separate merge step. Drained in order on reconnect, before refreshAll().
  let pendingOps = [];
  // Temp ids need a sequence, not just a clock: two creates in the same
  // millisecond (routine when adding several things while offline) would
  // otherwise mint the SAME id, and every find-by-id after that hits whichever
  // twin comes first.
  let localSeq = 0;
  function newLocalId() { return 'local-' + (++localSeq) + '-' + Date.now(); }
  // Card collapse state, deliberately NOT persisted — collapsedGroups is not
  // persisted either, and persisting one but not the other is new behaviour
  // behind no acceptance criterion.
  let collapsedRecipes = {};
  const TAB_KEY        = 'grocery.activeTab';

  // PM-3: a mutation on another card, or an SSE tick, rebuilds the whole
  // container, so a half-typed ingredient has to live outside the DOM.
  let recipeDrafts  = {};    // recipeId -> { value, caret }
  let focusRecipeId = null;  // card whose input should hold focus after the next pass

  const drag = { active: false, id: null, srcGroup: null };

  const gc           = document.getElementById('groups-container');
  const emptyEl      = document.getElementById('empty-state');
  const addForm      = document.getElementById('add-form');
  const newInput     = document.getElementById('new-item-input');
  const groupSel     = document.getElementById('group-select');
  const syncTog      = document.getElementById('sync-toggle');
  const banner       = document.getElementById('offline-banner');
  const resetBtn     = document.getElementById('reset-btn');
  const groupsBtn    = document.getElementById('groups-btn');
  const collapseAll      = document.getElementById('collapse-all-btn');
  const expandAll        = document.getElementById('expand-all-btn');
  const hideNotNeededBtn = document.getElementById('hide-not-needed-btn');

  const tabGrocery   = document.getElementById('tab-grocery');
  const tabRecipes   = document.getElementById('tab-recipes');
  const rc           = document.getElementById('recipes-container');
  const rEmptyEl     = document.getElementById('recipes-empty-state');

  // ── Reset modal
  const resetModal   = document.getElementById('reset-modal');
  const resetCancel  = document.getElementById('reset-cancel');
  const resetConfirm = document.getElementById('reset-confirm');

  // ── Groups modal
  const groupsModal  = document.getElementById('groups-modal');
  const groupsList   = document.getElementById('groups-modal-list');
  const groupsForm   = document.getElementById('groups-modal-form');
  const groupsInput  = document.getElementById('groups-modal-input');
  const groupsClose  = document.getElementById('groups-modal-close');

  // ── Title modal
  const titleModal     = document.getElementById('title-modal');
  const titleInput     = document.getElementById('title-modal-input');
  const titleError     = document.getElementById('title-modal-error');
  const titleSaveBtn   = document.getElementById('title-modal-save');
  const titleCancelBtn = document.getElementById('title-modal-cancel');
  const titleModalForm = document.getElementById('title-modal-form');
  const editTitleBtn   = document.getElementById('edit-title-btn');

  function openTitleModal() {
    titleInput.value = listTitle;
    titleInput.classList.remove('input-error');
    titleError.classList.add('hidden');
    titleError.textContent = '';
    titleModal.classList.remove('hidden');
    requestAnimationFrame(() => { titleInput.focus(); titleInput.select(); });
  }

  function closeTitleModal() {
    titleModal.classList.add('hidden');
  }

  async function saveTitleModal() {
    const val = titleInput.value.trim();
    if (!val) {
      titleInput.classList.add('input-error');
      titleError.textContent = 'Title cannot be empty.';
      titleError.classList.remove('hidden');
      titleInput.focus();
      return;
    }
    titleSaveBtn.disabled = true;
    try {
      await api('POST', '/api/config/title', { name: val });
      listTitle = val;
      document.title = listTitle;
      editTitleBtn.textContent = listTitle;
      const logo = document.querySelector('.app-logo');
      if (logo) logo.setAttribute('aria-label', listTitle);
      closeTitleModal();
    } catch (err) {
      titleInput.classList.add('input-error');
      titleError.textContent = 'Could not save title. Please try again.';
      titleError.classList.remove('hidden');
    } finally {
      titleSaveBtn.disabled = false;
    }
  }

  editTitleBtn.addEventListener('click', openTitleModal);
  titleCancelBtn.addEventListener('click', closeTitleModal);
  titleSaveBtn.addEventListener('click', saveTitleModal);
  titleModalForm.addEventListener('submit', e => { e.preventDefault(); saveTitleModal(); });
  titleModal.addEventListener('click', e => { if (e.target === titleModal) closeTitleModal(); });
  titleModal.addEventListener('keydown', e => { if (e.key === 'Escape') closeTitleModal(); });
  titleInput.addEventListener('input', () => {
    titleInput.classList.remove('input-error');
    titleError.classList.add('hidden');
  });

  // ────────────────────────────────────────────────────────────────
  // Pure helpers  (mirrored in app.test.js — keep in sync)
  // ────────────────────────────────────────────────────────────────

  const VISIBILITY_MODES = ['show_all', 'hide_not_needed', 'hide_completed'];

  function nextVisibilityMode(current) {
    return (current + 1) % VISIBILITY_MODES.length;
  }

  /**
   * visibilityFilter — true if item should be visible in the given mode.
   *   0 (show_all)        → always true
   *   1 (hide_not_needed) → hide state==='not_needed'
   *   2 (hide_completed)  → also hide completed===true
   */
  function visibilityFilter(item, mode) {
    if (mode === 0) return true;
    if (item.state === 'not_needed') return false;
    if (mode === 2 && item.completed) return false;
    return true;
  }

  /**
   * groupIsVisible — true when the group has at least one visible item.
   * Mode 0 always returns true (show empty groups too).
   */
  function groupIsVisible(allItems, group, mode) {
    if (mode === 0) return true;
    return allItems.some(i => i.group === group && visibilityFilter(i, mode));
  }

  function nextState(s) {
    return STATES[(STATES.indexOf(s) + 1) % STATES.length];
  }

  function itemsForGroup(group) {
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
   * groupsForRender returns the ordered render list:
   * real groups first, then the virtual NO_GROUP appended only if
   * at least one item carries that group.
   */
  function groupsForRender() {
    const hasOrphans = items.some(i => i.group === NO_GROUP);
    return hasOrphans ? [...groups, NO_GROUP] : [...groups];
  }

  // Every helper below takes all its inputs as parameters and reads no module
  // state, so the app.test.js mirror is an exact copy rather than a paraphrase.

  // The server trims, rejects blanks and rejects case-insensitive duplicates in
  // AddRecipe/PatchRecipe. api() does not check res.ok, so a 400 or 409 comes
  // back as a parsed error envelope that every `saved && saved.id` guard reads
  // as "no reply" — the optimistic card is kept and the user is never told the
  // server refused it. Applying the same two rules here is what keeps a refused
  // name from painting a phantom card that survives until the next unrelated
  // server write.
  // mirrored in app.test.js :: recipeNameTaken
  function recipeNameTaken(recipes, name, exceptId) {
    const n = String(name).trim().toLowerCase();
    return recipes.some(r => r.id !== exceptId && r.name.trim().toLowerCase() === n);
  }

  // max(order)+1, matching AddRecipe. recipes.length is wrong for the reason
  // store.go states: after a middle recipe is deleted, len() collides with an
  // existing Order and lets the sort tie-break pick the display position.
  // mirrored in app.test.js :: nextRecipeOrder
  function nextRecipeOrder(recipes) {
    return recipes.reduce((max, r) => (r.order >= max ? r.order + 1 : max), 0);
  }

  // mirrored in app.test.js :: recipeById
  function recipeById(recipes, id) {
    return recipes.find(r => r.id === id) || null;
  }

  // mirrored in app.test.js :: recipesForRender
  function recipesForRender(recipes) {
    return [...recipes].sort((a, b) =>
      a.order !== b.order
        ? a.order - b.order
        : new Date(a.created_at) - new Date(b.created_at)
    );
  }

  // mirrored in app.test.js :: ingredientsForRecipe
  function ingredientsForRecipe(items, recipeId) {
    return items
      .filter(i => i.recipe_id === recipeId)
      .sort((a, b) => new Date(a.created_at) - new Date(b.created_at));
  }

  // mirrored in app.test.js :: isOwned
  function isOwned(item) {
    return !!(item && item.recipe_id);
  }

  /**
   * recipeSuffix — " (Chili)" for an item owned by a KNOWN recipe, "" otherwise.
   * A dangling recipe_id (owner already deleted, list not yet refetched) yields
   * "" rather than " ()", which is the transient render this guards against.
   */
  // mirrored in app.test.js :: recipeSuffix
  function recipeSuffix(item, recipes) {
    if (!isOwned(item)) return '';
    const r = recipeById(recipes, item.recipe_id);
    return r ? ` (${r.name})` : '';
  }

  /**
   * ownedTooltip — "Belongs to recipe Chili" for an item owned by a KNOWN
   * recipe, "" otherwise. Same dangling-recipe_id guard as recipeSuffix, and
   * for the same reason: "Belongs to recipe " with nothing after it is worse
   * than no tooltip.
   *
   * It says nothing about deleting, deliberately. The row carries no delete
   * control, and this title sits on the whole <li>, so an imperative here reads
   * as an offer to act on a row that cannot act. Its actual job is to
   * disambiguate the "(Chili)" suffix, which is not self-explanatory the first
   * time you meet it.
   *
   * The name is assigned through the li.title PROPERTY, never interpolated into
   * markup, so it needs no esc() — the DOM stores the string literally. Every
   * other name render site in this file escapes; this is the one exception, and
   * it is an exception because it is not a markup site.
   */
  // mirrored in app.test.js :: ownedTooltip
  function ownedTooltip(item, recipes) {
    if (!isOwned(item)) return '';
    const r = recipeById(recipes, item.recipe_id);
    return r ? `Belongs to recipe ${r.name}` : '';
  }

  // mirrored in app.test.js :: displayName
  function displayName(item, recipes) {
    return item.name + recipeSuffix(item, recipes);
  }

  // 'No Group' is NO_GROUP inlined: the mirror convention forbids module reads.
  // mirrored in app.test.js :: groupLabel
  function groupLabel(group) {
    return group === 'No Group' ? 'Unallocated' : group;
  }

  // mirrored in app.test.js :: groupEmptyHint
  function groupEmptyHint(group) {
    return group === 'No Group' ? 'Nothing unallocated' : 'No items';
  }

  /**
   * tabControlVisible — AC-8.4's truth table for the three grocery-only header
   * controls. #progress-bar is deliberately NOT in this table: renderProgressBar
   * is its single owner, via progressBarVisible.
   */
  // mirrored in app.test.js :: tabControlVisible
  function tabControlVisible(controlId, tab) {
    const groceryOnly = ['#hide-not-needed-btn', '#reset-btn', '#groups-btn'];
    return groceryOnly.includes(controlId) ? tab === 'grocery' : true;
  }

  // The shipped progress-bar guard, extracted so the test exercises the same
  // expression the code runs rather than a second description of it.
  // mirrored in app.test.js :: progressBarVisible
  function progressBarVisible(showProgress, itemCount, tab) {
    return !!showProgress && itemCount > 0 && tab === 'grocery';
  }

  // mirrored in app.test.js :: applyRecipeToggle
  function applyRecipeToggle(items, recipeId, enabled) {
    return items.map(i =>
      i.recipe_id === recipeId
        ? { ...i, state: enabled ? 'needed' : 'not_needed', completed: false }
        : i
    );
  }

  // ────────────────────────────────────────────────────────────────
  // API
  // ────────────────────────────────────────────────────────────────
  async function api(method, path, body) {
    const opts = { method, headers: { 'Content-Type': 'application/json' } };
    if (body !== undefined) opts.body = JSON.stringify(body);
    const res = await fetch(path, opts);
    if (res.status === 204) return null;
    return res.json();
  }

  // loadConfigData is the fetch and the field assignments only. loadConfig keeps
  // its exact current behaviour so no existing call site changes.
  async function loadConfigData() {
    const cfg = await api('GET', '/api/config').catch(() => null);
    groups              = cfg?.groups                || [];
    showProgress        = cfg?.progress              || false;
    syncIntervalSeconds = cfg?.sync_interval_seconds ?? 1;
    if (cfg?.title) {
      listTitle = cfg.title;
      document.title = listTitle;
      const btn = document.getElementById('edit-title-btn');
      if (btn) btn.textContent = listTitle;
      const logo = document.querySelector('.app-logo');
      if (logo) logo.setAttribute('aria-label', listTitle);
    }
  }

  async function loadConfig() {
    await loadConfigData();
    rebuildGroupSelect();
    renderProgressBar();
  }

  // A local-only entity (id still 'local-'-prefixed, never reached the
  // server) must survive a GET refresh. refreshAll() runs on every SSE tick —
  // any client's mutation, not just this one's — so an unlucky tick landing
  // between an optimistic create and its resolution used to wipe the
  // optimistic row outright. That is the same mechanism that, over a longer
  // gap, made a reconnect after offline edits look like the server "blowing
  // away" the recipe: refreshAll() replaced local state wholesale. Anything
  // already server-known is still fully replaced, since the server is
  // authoritative for it; only not-yet-synced local creates are preserved.
  function mergeServerItems(serverItems) {
    const pendingLocal = items.filter(i => i.id.startsWith('local-') && !i._deleted);
    items = [...serverItems, ...pendingLocal];
  }

  function mergeServerRecipes(serverRecipes) {
    const pendingLocal = recipes.filter(r => r.id.startsWith('local-') && !r._deleted);
    recipes = [...serverRecipes, ...pendingLocal];
  }

  async function fetchItemsData() {
    const data = await api('GET', '/api/items').catch(() => []);
    mergeServerItems(data || []);
  }

  async function fetchItems() {
    await fetchItemsData();
    render();
  }

  // Deliberately has no render-and-fetch wrapper twin: every call site uses
  // this data-only form or refreshAll, so a wrapper would have no callers.
  async function fetchRecipesData() {
    const data = await api('GET', '/api/recipes').catch(() => []);
    mergeServerRecipes(data || []);
  }

  /**
   * refreshAll re-reads everything in one pass and renders once. It calls
   * loadConfigData + rebuildGroupSelect rather than loadConfig, because
   * loadConfig would run renderProgressBar a second time — once on its way out
   * and once during the render below. Letting the dispatcher own the bar keeps
   * the single-owner rule true on the SSE path too.
   */
  async function refreshAll() {
    await Promise.all([fetchItemsData(), fetchRecipesData(), loadConfigData()]);
    rebuildGroupSelect();
    render();
  }

  // Drains pendingOps in the order edits were made while offline — a while
  // loop, not a for, so an op that defensively re-queues itself is still
  // caught in this same reconnect pass. Every op is written to never throw
  // (failures are swallowed the same way every other sync call in this file
  // already does), so the .catch here is a backstop, not the primary guard.
  // True only while the queue drains. Confirm-adoption is suppressed under it:
  // a mid-drain server reply reflects only the ops replayed SO FAR, so adopting
  // it would clobber a newer local edit a later op is about to read (a rename's
  // reply carries enabled:false and would erase a still-queued toggle). The
  // refreshAll() the reconnect handler runs after the drain is the one
  // reconciliation point, once every op has reached the server.
  let replayingOps = false;

  async function replayPendingOps() {
    replayingOps = true;
    try {
      while (pendingOps.length) {
        const op = pendingOps.shift();
        await op().catch(() => null);
      }
    } finally {
      replayingOps = false;
    }
  }

  async function syncToServer() {
    const data = await api('POST', '/api/sync', items).catch(() => null);
    if (data) { items = data; render(); }
  }

  // ────────────────────────────────────────────────────────────────
  // Group select (footer)
  // ────────────────────────────────────────────────────────────────
  function rebuildGroupSelect() {
    const prev = groupSel.value;
    groupSel.innerHTML = '';
    // Never offer NO_GROUP as an add target
    groups.forEach(g => {
      const opt = document.createElement('option');
      opt.value = g;
      opt.textContent = g.length > 14 ? g.slice(0, 13) + '\u2026' : g;
      opt.title = g;
      groupSel.appendChild(opt);
    });
    if (prev && groups.includes(prev)) groupSel.value = prev;
  }

  // ────────────────────────────────────────────────────────────────
  // Mutations
  // ────────────────────────────────────────────────────────────────
  async function addItem(name, group) {
    const tempId = newLocalId();
    const it = {
      id: tempId, name, group,
      state: 'needed', completed: false,
      order: items.filter(i => i.group === group).length,
      created_at: new Date().toISOString()
    };
    items.push(it);
    render();
    const sync = () => syncAddItem(it);
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function syncAddItem(it) {
    if (it._deleted) return; // deleted offline before it ever reached the server
    const saved = await api('POST', '/api/items', { name: it.name, group: it.group }).catch(() => null);
    // In-place, not array-replacement: keeps object identity so any later
    // queued closure that still references `it` sees the resolved real id.
    // Mid-drain, only the identity fields are adopted — the server row's
    // state/completed reflect none of the still-queued patches, and a full
    // assign here would feed a later patch the stale values.
    if (saved) {
      Object.assign(it, replayingOps ? { id: saved.id, created_at: saved.created_at } : saved);
      render();
    }
  }

  async function toggleComplete(id) {
    const item = items.find(i => i.id === id);
    if (!item) return;
    item.completed = !item.completed;
    render();
    const sync = () => syncPatchItem(item, { completed: item.completed });
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function cycleState(id) {
    const item = items.find(i => i.id === id);
    if (!item) return;
    item.state = nextState(item.state);
    render();
    const sync = () => syncPatchItem(item, { state: item.state });
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  // fields is read at CALL time (it's the deferred arrow's own expression),
  // so a queued toggleComplete followed by a queued cycleState replays with
  // whatever the item's current completed/state actually are, not whatever
  // they were when each was queued — the second call's PATCH is a redundant
  // but harmless resend of the same already-current values.
  async function syncPatchItem(item, fields) {
    if (item.id.startsWith('local-')) return; // its own create-replay sends current fields already
    await api('PATCH', `/api/items/${item.id}`, fields).catch(() => null);
  }

  async function deleteItem(id) {
    const item = items.find(i => i.id === id);
    if (!item) return;
    items = items.filter(i => i.id !== id);
    render();
    item._deleted = true; // cancels a still-pending create for this same item
    if (item.id.startsWith('local-')) return; // never reached the server
    const sync = () => api('DELETE', `/api/items/${item.id}`).catch(() => null);
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function moveItem(id, toGroup, orderIds) {
    const item = items.find(i => i.id === id);
    if (!item) return;
    item.group = toGroup;
    orderIds.forEach((oid, idx) => {
      const it = items.find(i => i.id === oid);
      if (it) it.order = idx;
    });
    render();
    // Ids re-derived at replay time: the queued snapshot could hold 'local-'
    // ids that a queued create resolves earlier in the same drain, and the
    // current sort already reflects every later reorder anyway.
    const sync = () => api('POST', '/api/move', {
      id: item.id, group: item.group,
      order_ids: itemsForGroup(item.group).map(i => i.id).filter(x => !x.startsWith('local-'))
    }).catch(() => null);
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function reorderWithinGroup(group, orderIds) {
    orderIds.forEach((oid, idx) => {
      const it = items.find(i => i.id === oid);
      if (it) it.order = idx;
    });
    render();
    const sync = () => api('POST', '/api/reorder', {
      group, ids: itemsForGroup(group).map(i => i.id).filter(x => !x.startsWith('local-'))
    }).catch(() => null);
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  // ────────────────────────────────────────────────────────────────
  // Recipe mutations
  //
  // Each one is optimistic-then-confirm in the style of addItem: mutate local
  // state, paint, and only then talk to the server. Every adopt is guarded on
  // the response actually having the shape its endpoint promises, because api()
  // does not check res.ok — a 400 body would otherwise be adopted as the
  // object. The guard leaves the optimistic value standing until the next
  // refresh, which is strictly better than painting an error object.
  // ────────────────────────────────────────────────────────────────

  // PATCH /api/recipes/:id answers {recipe, items} for a rename as well as a
  // toggle, so both callers adopt through here. Object.assign, not slot
  // replacement: pending offline closures hold references to the existing
  // recipe object, and replacing the slot would strand them on a stale copy.
  function adoptRecipePatch(patched) {
    const r = recipes.find(x => x.id === patched.recipe.id);
    if (r) Object.assign(r, patched.recipe);
    if (Array.isArray(patched.items)) mergeServerItems(patched.items);
  }

  // Returns false when the name is one the server would refuse, so the caller
  // can leave the user's text in the input instead of silently eating it.
  async function createRecipe(name) {
    name = String(name).trim();
    if (!name || recipeNameTaken(recipes, name)) return false;
    const r = {
      id: newLocalId(), name, enabled: false,
      order: nextRecipeOrder(recipes),
      created_at: new Date().toISOString()
    };
    recipes.push(r);
    render();
    const sync = () => syncCreateRecipe(r);
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
    return true;
  }

  async function syncCreateRecipe(r) {
    if (r._deleted) return; // created and deleted within the same offline stretch
    // Attempt 1 is the name as typed. A null reply is a network failure and
    // ends the attempts (matching every other swallowed sync error here); a
    // parsed body WITHOUT an id is the server's error envelope — for a create
    // that means the name clashed with a recipe some other client added while
    // this one was offline. Newest-wins says the offline recipe still lands,
    // so it lands under a disambiguated name rather than being dropped.
    for (let attempt = 1; attempt <= 5; attempt++) {
      const name = attempt === 1 ? r.name : `${r.name} (${attempt})`;
      const saved = await api('POST', '/api/recipes', { name }).catch(() => null);
      if (saved === null) return;
      if (!saved.id) continue;
      const tempId = r.id;
      // In place, so queued closures still holding this object see the real id.
      Object.assign(r, saved, { enabled: r.enabled });
      // recipe_id is a plain string on each item, not an object reference, so
      // the temp→real hop has to be patched by hand for offline ingredients.
      items.forEach(i => { if (i.recipe_id === tempId) i.recipe_id = r.id; });
      render();
      return;
    }
  }

  async function renameRecipe(id, name) {
    const r = recipeById(recipes, id);
    name = String(name).trim();
    if (!r || !name || name === r.name) return;
    // exceptId is the recipe being renamed: re-saving a card under its own
    // name differing only in case is a no-op the server accepts, not a clash.
    if (recipeNameTaken(recipes, name, id)) return;
    r.name = name;
    render();
    const sync = () => syncPatchRecipe(r, () => ({ name: r.name }));
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  // fields is a thunk so the payload is built from the recipe's CURRENT state
  // at replay time — the newest offline edit is what the server receives, even
  // when several queued patches target the same recipe.
  async function syncPatchRecipe(r, fields) {
    if (r._deleted || r.id.startsWith('local-')) return; // create-replay already carries current state
    const patched = await api('PATCH', '/api/recipes/' + encodeURIComponent(r.id),
                              fields()).catch(() => null);
    if (patched && patched.recipe && !replayingOps) { adoptRecipePatch(patched); render(); }
  }

  async function toggleRecipe(id, enabled) {
    const r = recipeById(recipes, id);
    if (!r) return;
    r.enabled = enabled;
    // AC-9.5: the grocery side flips in the same paint, with no manual reload.
    items = applyRecipeToggle(items, id, enabled);
    render();
    const sync = () => syncPatchRecipe(r, () => ({ enabled: r.enabled }));
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function deleteRecipe(id) {
    const r = recipeById(recipes, id);
    if (!r) return;
    recipes = recipes.filter(x => x.id !== id);
    // Flag before filtering: queued offline creates for these ingredients hold
    // references to the objects and must see the flag, or they would replay a
    // POST onto a recipe that no longer exists.
    items.forEach(i => { if (i.recipe_id === id) i._deleted = true; });
    items = items.filter(i => i.recipe_id !== id);
    // Both maps are keyed by recipe id; dropping the entries keeps them from
    // growing across a long session, as collapsedGroups never does.
    delete recipeDrafts[id];
    delete collapsedRecipes[id];
    render();
    r._deleted = true; // cancels a still-pending create and any queued patches
    if (r.id.startsWith('local-')) return; // never reached the server
    const sync = async () => {
      // D-1: the response is {items} only — there is no recipe key to adopt,
      // which is why the recipe was removed client-side above.
      const res = await api('DELETE', '/api/recipes/' + encodeURIComponent(r.id))
                    .catch(() => null);
      if (res && Array.isArray(res.items)) { mergeServerItems(res.items); render(); }
    };
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function addIngredient(recipeId, name) {
    const r = recipeById(recipes, recipeId);
    if (!r || !name) return;
    const it = {
      id: newLocalId(), name, group: NO_GROUP,
      state: r.enabled ? 'needed' : 'not_needed',
      completed: false,
      order: items.filter(i => i.group === NO_GROUP).length,
      created_at: new Date().toISOString(),
      recipe_id: recipeId
    };
    items.push(it);
    // Clear the draft and claim focus BEFORE painting, so the input the user
    // was typing in comes back empty and focused, ready for the next one.
    // Blanking the live element is not redundant with deleting the draft: on
    // the Enter path that element is still document.activeElement, so
    // renderRecipesTab's step-1 re-read captures whatever is sitting in it and
    // resurrects the draft this line just deleted. Emptying it first makes
    // that re-read capture ''. The button path never hit this, because there
    // the active element is the button.
    delete recipeDrafts[recipeId];
    const live = rc.querySelector(
      `.recipe-ingredient-input[data-recipe-id="${CSS.escape(recipeId)}"]`);
    if (live) live.value = '';
    focusRecipeId = recipeId;
    render();
    const sync = () => syncAddIngredient(it);
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function syncAddIngredient(it) {
    if (it._deleted) return; // removed (or its recipe deleted) before syncing
    // it.recipe_id is read here, at call time: an offline-created recipe's
    // create-replay has already rewritten it from the temp id to the real one.
    const saved = await api('POST',
      '/api/recipes/' + encodeURIComponent(it.recipe_id) + '/ingredients',
      { name: it.name }).catch(() => null);
    if (saved && saved.id) {
      // Identity-only under replay, for the same reason as syncAddItem.
      Object.assign(it, replayingOps
        ? { id: saved.id, created_at: saved.created_at, recipe_id: saved.recipe_id }
        : saved);
      // The confirm pass wipes the container too, so the claim is re-made.
      focusRecipeId = it.recipe_id;
      render();
    }
  }

  async function removeIngredient(recipeId, itemId) {
    const it = items.find(i => i.id === itemId);
    if (!it) return;
    items = items.filter(i => i.id !== itemId);
    render();
    it._deleted = true; // cancels a still-pending create for this ingredient
    if (it.id.startsWith('local-')) return; // never reached the server
    const sync = async () => {
      // 204 carries no body, so there is nothing to adopt; the only meaningful
      // confirm is repairing an optimistic removal the server refused.
      const ok = await api('DELETE',
        '/api/recipes/' + encodeURIComponent(it.recipe_id) +
        '/ingredients/' + encodeURIComponent(it.id))
        .then(() => true).catch(() => false);
      if (!ok) { await fetchItemsData(); render(); }
    };
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  async function moveRecipe(id, delta) {
    const ordered = recipesForRender(recipes);
    const idx     = ordered.findIndex(r => r.id === id);
    if (idx === -1) return;
    const target = idx + delta;
    if (target < 0 || target >= ordered.length) return;
    ordered[idx] = ordered.splice(target, 1, ordered[idx])[0];
    ordered.forEach((r, i) => { r.order = i; });
    recipes = ordered;
    render();
    const sync = async () => {
      // Ids re-derived at replay time, filtered of any still-local recipe the
      // server has never heard of; the current sort reflects every later move.
      const ids = recipesForRender(recipes)
        .map(r => r.id).filter(x => !x.startsWith('local-'));
      const saved = await api('POST', '/api/recipes/reorder', { ids }).catch(() => null);
      if (Array.isArray(saved)) { mergeServerRecipes(saved); render(); }
    };
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  // ────────────────────────────────────────────────────────────────
  // Tabs
  // ────────────────────────────────────────────────────────────────

  /**
   * setActiveTab renders from state already in memory. It must never fetch:
   * no fetchItems, fetchItemsData, fetchRecipesData, loadConfig, loadConfigData,
   * refreshAll, connectSSE or disconnectSSE. Switching tabs issues zero
   * requests, and that is the criterion.
   */
  function setActiveTab(tab) {
    activeTab = tab === 'recipes' ? 'recipes' : 'grocery';
    const onRecipes = activeTab === 'recipes';

    try {
      localStorage.setItem(TAB_KEY, activeTab);
    } catch (_) {
      // Storage can be unavailable (private mode, disabled cookies). The tab
      // still switches; only the restore-on-reload is lost.
    }

    if (tabGrocery) {
      tabGrocery.classList.toggle('active', !onRecipes);
      tabGrocery.setAttribute('aria-selected', String(!onRecipes));
    }
    if (tabRecipes) {
      tabRecipes.classList.toggle('active', onRecipes);
      tabRecipes.setAttribute('aria-selected', String(onRecipes));
    }

    // #progress-bar is deliberately absent from this list. Hiding or showing it
    // here would override the !showProgress early return in renderProgressBar,
    // which is what keeps it hidden when config sets progress:false — the
    // default. renderProgressBar is its only owner.
    setHidden(hideNotNeededBtn, !tabControlVisible('#hide-not-needed-btn', activeTab));
    setHidden(resetBtn,         !tabControlVisible('#reset-btn',           activeTab));
    setHidden(groupsBtn,        !tabControlVisible('#groups-btn',          activeTab));

    // The footer form is tab-aware: it creates recipes on the Recipes tab.
    setHidden(groupSel, onRecipes);
    newInput.placeholder = onRecipes ? 'Add a recipe\u2026' : 'Add an item\u2026';

    updateRecipeControlsDisabled();
    render();
  }

  if (tabGrocery) tabGrocery.addEventListener('click', () => setActiveTab('grocery'));
  if (tabRecipes) tabRecipes.addEventListener('click', () => setActiveTab('recipes'));

  /**
   * revealRecipe implements AC-9.6: clicking an owned row's chip switches to the
   * Recipes tab, expands that card and scrolls it into view, in one gesture.
   * T9 owns the chip; the state it needs (collapsedRecipes) lives here.
   *
   * The order of the three steps is the specification, not a preference.
   */
  function revealRecipe(recipeId) {
    // 1. Expand FIRST. renderRecipesTab reads this map while building each card,
    //    so clearing it afterwards would land the user on a collapsed card.
    //    delete, not = false: absent-means-expanded matches collapsedGroups.
    delete collapsedRecipes[recipeId];

    // 2. setActiveTab persists the tab, re-chromes the header and paints. It
    //    must not be followed by a second paint here — that would move T7's
    //    census, and there is nothing left to draw.
    setActiveTab('recipes');

    // 3. Only now does the card exist in the DOM: until step 2 returns, the
    //    container still holds the previous pass. Painting is synchronous
    //    (plain innerHTML writes, no await), so no rAF or setTimeout is needed.
    //    The guard is mandatory — this runs inside a delegated handler, where a
    //    throw is invisible on screen and kills the rest of the handler.
    const card = rc.querySelector(
      `.recipe-card[data-recipe-id="${CSS.escape(recipeId)}"]`);
    if (card) card.scrollIntoView({ block: 'nearest' });
  }

  // ────────────────────────────────────────────────────────────────
  // Collapse / Expand all
  // ────────────────────────────────────────────────────────────────
  function setAllCollapsed(collapsed) {
    if (activeTab === 'recipes') {
      // Iterate the module-scope array, not the DOM: a DOM walk would miss any
      // card a filter had not rendered, and state is never re-derived from it.
      recipes.forEach(r => { collapsedRecipes[r.id] = collapsed; });
    } else {
      groupsForRender().forEach(g => { collapsedGroups[g] = collapsed; });
    }
    render();
  }

  collapseAll.addEventListener('click', () => setAllCollapsed(true));
  expandAll.addEventListener('click',   () => setAllCollapsed(false));

  // ────────────────────────────────────────────────────────────────
  // Visibility mode cycle  (eye button)
  // ────────────────────────────────────────────────────────────────
  const VISIBILITY_TITLES = [
    'Hide \u2018Not Needed\u2019 items',           // clicked from show_all
    'Also hide completed items',                    // clicked from hide_not_needed
    'Show all items',                               // clicked from hide_completed
  ];
  const VISIBILITY_ARIA = [
    'Show all items',
    'Hide Not Needed items and empty groups',
    'Hide Not Needed, completed items and empty groups',
  ];

  function updateVisibilityBtn() {
    hideNotNeededBtn.classList.toggle('active', visibilityMode !== 0);
    // aria-label describes the *current* state
    hideNotNeededBtn.setAttribute('aria-label', VISIBILITY_ARIA[visibilityMode]);
    // title describes what clicking will do next
    hideNotNeededBtn.title = VISIBILITY_TITLES[visibilityMode];
  }

  hideNotNeededBtn.addEventListener('click', () => {
    visibilityMode = nextVisibilityMode(visibilityMode);
    updateVisibilityBtn();
    render();
  });

  updateVisibilityBtn(); // set initial state

  // ────────────────────────────────────────────────────────────────
  // Reset modal
  // ────────────────────────────────────────────────────────────────
  function openResetModal()  { resetModal.classList.remove('hidden'); resetConfirm.focus(); }
  function closeResetModal() { resetModal.classList.add('hidden'); }

  async function doReset() {
    closeResetModal();
    items.forEach(item => { item.completed = false; item.state = 'check'; });
    // A-2: reset returns every item to Check, so no recipe is still "on".
    recipes.forEach(r => { r.enabled = false; });
    render();
    const sync = async () => {
      const data = await api('POST', '/api/reset').catch(() => null);
      if (data) { mergeServerItems(data); render(); }
    };
    if (syncEnabled) await sync();
    else pendingOps.push(sync);
  }

  resetBtn.addEventListener('click',     openResetModal);
  resetCancel.addEventListener('click',  closeResetModal);
  resetConfirm.addEventListener('click', doReset);
  resetModal.addEventListener('click', e => { if (e.target === resetModal) closeResetModal(); });

  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') { closeResetModal(); closeGroupsModal(); closeRecipeModal(); }
  });

  // ────────────────────────────────────────────────────────────────
  // Groups modal
  // ────────────────────────────────────────────────────────────────
  function renderGroupsList() {
    groupsList.innerHTML = '';
    if (groups.length === 0) {
      const li = document.createElement('li');
      li.className   = 'groups-modal-empty';
      li.textContent = 'No groups yet';
      groupsList.appendChild(li);
      return;
    }
    groups.forEach(g => {
      const li  = document.createElement('li');
      li.className    = 'groups-modal-item';
      li.dataset.group = g;

      // Drag handle
      const handle = document.createElement('span');
      handle.className = 'groups-modal-handle';
      handle.title     = 'Drag to reorder';
      handle.setAttribute('aria-label', 'Drag to reorder group');
      handle.innerHTML = `<svg viewBox="0 0 14 14" fill="currentColor" width="14" height="14">
        <circle cx="4" cy="3"  r="1.2"/><circle cx="10" cy="3"  r="1.2"/>
        <circle cx="4" cy="7"  r="1.2"/><circle cx="10" cy="7"  r="1.2"/>
        <circle cx="4" cy="11" r="1.2"/><circle cx="10" cy="11" r="1.2"/>
      </svg>`;

      const nameEl = document.createElement('span');
      nameEl.className   = 'groups-modal-name';
      nameEl.textContent = g;

      const del = document.createElement('button');
      del.className = 'groups-modal-delete';
      del.title     = `Remove group \u201c${g}\u201d`;
      del.setAttribute('aria-label', `Remove group ${g}`);
      del.innerHTML = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor"
        stroke-width="2" stroke-linecap="round">
        <path d="M3 3l10 10M13 3L3 13"/></svg>`;
      del.addEventListener('click', () => removeGroup(g));

      li.appendChild(handle);
      li.appendChild(nameEl);
      li.appendChild(del);
      groupsList.appendChild(li);

      attachGroupDrag(li, handle);
    });
  }

  // ────────────────────────────────────────────────────────────────
  // Group-list drag (mouse + touch, handle-only)
  // ────────────────────────────────────────────────────────────────
  const gdrag = { active: false, srcGroup: null };

  function attachGroupDrag(row, handle) {
    function clearGIndicators() {
      groupsList.querySelectorAll('.gdrag-above,.gdrag-below')
        .forEach(el => el.classList.remove('gdrag-above', 'gdrag-below'));
    }

    function gPointerStart() {
      gdrag.active   = true;
      gdrag.srcGroup = row.dataset.group;
      row.classList.add('gdragging');
    }

    function gPointerMove(clientY) {
      if (!gdrag.active) return;
      clearGIndicators();
      const els = [...groupsList.querySelectorAll('.groups-modal-item')];
      for (const el of els) {
        if (el.dataset.group === gdrag.srcGroup) continue;
        const rect = el.getBoundingClientRect();
        if (clientY < rect.top + rect.height / 2) {
          el.classList.add('gdrag-above');
          break;
        } else {
          el.classList.add('gdrag-below');
        }
      }
    }

    function gPointerEnd(clientY) {
      if (!gdrag.active) return;
      gdrag.active = false;
      groupsList.querySelectorAll('.groups-modal-item.gdragging')
        .forEach(el => el.classList.remove('gdragging'));

      // Find insertion point
      const els      = [...groupsList.querySelectorAll('.groups-modal-item')];
      const names    = els.map(el => el.dataset.group);
      const fromIdx  = names.indexOf(gdrag.srcGroup);
      let   toIdx    = names.length; // default: end

      for (let i = 0; i < els.length; i++) {
        if (els[i].dataset.group === gdrag.srcGroup) continue;
        const rect = els[i].getBoundingClientRect();
        if (clientY < rect.top + rect.height / 2) {
          toIdx = i;
          break;
        }
      }

      const dragged = gdrag.srcGroup;
      clearGIndicators();
      gdrag.srcGroup = null;

      if (fromIdx === toIdx || fromIdx === -1) return;
      const newOrder = [...names];
      newOrder.splice(fromIdx, 1);
      const insertAt = fromIdx < toIdx ? toIdx - 1 : toIdx;
      newOrder.splice(insertAt, 0, dragged);
      reorderGroups(newOrder);
    }

    // Mouse
    handle.addEventListener('mousedown', e => {
      e.preventDefault();
      gPointerStart();
      const onMove = e => gPointerMove(e.clientY);
      const onUp   = e => {
        gPointerEnd(e.clientY);
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup',   onUp);
      };
      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup',   onUp);
    });

    // Touch
    handle.addEventListener('touchstart', e => {
      gPointerStart();
      const onMove = e => { gPointerMove(e.touches[0].clientY); e.preventDefault(); };
      const onEnd  = e => {
        gPointerEnd(e.changedTouches[0].clientY);
        handle.removeEventListener('touchmove', onMove);
        handle.removeEventListener('touchend',  onEnd);
      };
      handle.addEventListener('touchmove', onMove, { passive: false });
      handle.addEventListener('touchend',  onEnd);
    }, { passive: true });
  }

  function openGroupsModal() {
    renderGroupsList();
    groupsModal.classList.remove('hidden');
    groupsInput.value = '';
    groupsInput.focus();
  }

  function closeGroupsModal() { groupsModal.classList.add('hidden'); }

  async function addGroup(name) {
    if (!name || name === NO_GROUP || groups.includes(name)) return;
    // Optimistic
    groups.push(name);
    rebuildGroupSelect();
    renderGroupsList();
    render();
    if (syncEnabled) {
      const data = await api('POST', '/api/config/groups', { name }).catch(() => null);
      if (data?.groups) {
        groups = data.groups;
        rebuildGroupSelect();
        renderGroupsList();
        render();
      }
    }
  }

  async function reorderGroups(newOrder) {
    groups = newOrder;
    rebuildGroupSelect();
    renderGroupsList();
    render();
    if (syncEnabled) {
      const data = await api('POST', '/api/config/groups/reorder', { groups: newOrder }).catch(() => null);
      if (data?.groups) {
        groups = data.groups;
        rebuildGroupSelect();
        renderGroupsList();
        render();
      }
    }
  }

  async function removeGroup(name) {
    // Optimistic local: move items in deleted group to NO_GROUP
    items.forEach(item => { if (item.group === name) item.group = NO_GROUP; });
    groups = groups.filter(g => g !== name);
    rebuildGroupSelect();
    renderGroupsList();
    render();
    if (syncEnabled) {
      // POST /api/config/groups/remove returns { groups, items }
      const data = await api('POST', '/api/config/groups/remove', { name }).catch(() => null);
      if (data) {
        if (data.groups) groups = data.groups;
        if (data.items)  items  = data.items;
        rebuildGroupSelect();
        renderGroupsList();
        render();
      }
    }
  }

  groupsBtn.addEventListener('click', openGroupsModal);
  groupsClose.addEventListener('click', closeGroupsModal);
  groupsModal.addEventListener('click', e => { if (e.target === groupsModal) closeGroupsModal(); });

  groupsForm.addEventListener('submit', e => {
    e.preventDefault();
    const name = groupsInput.value.trim();
    if (!name) return;
    groupsInput.value = '';
    addGroup(name);
    groupsInput.focus();
  });

  // ────────────────────────────────────────────────────────────────
  // Progress bar
  // ────────────────────────────────────────────────────────────────
  const progressBar    = document.getElementById('progress-bar');
  const progNeeded     = document.getElementById('prog-needed');
  const progCheck      = document.getElementById('prog-check');
  const progNotNeeded  = document.getElementById('prog-not-needed');
  const progCompleted  = document.getElementById('prog-completed');

  function renderProgressBar() {
    if (!progressBarVisible(showProgress, items.length, activeTab)) {
      progressBar.classList.add('hidden');
      return;
    }
    progressBar.classList.remove('hidden');

    const total      = items.length;
    const nNeeded    = items.filter(i => i.state === 'needed'     && !i.completed).length;
    const nCheck     = items.filter(i => i.state === 'check'      && !i.completed).length;
    const nNotNeeded = items.filter(i => i.state === 'not_needed' && !i.completed).length;
    const nCompleted = items.filter(i => i.completed).length;

    function pct(n) { return (n / total * 100).toFixed(1) + '%'; }
    function tip(label, n) { return `${label}: ${n} (${(n/total*100).toFixed(0)}%)`; }

    progNeeded.style.width    = pct(nNeeded);
    progCheck.style.width     = pct(nCheck);
    progNotNeeded.style.width = pct(nNotNeeded);
    progCompleted.style.width = pct(nCompleted);

    progNeeded.title    = tip('Needed',      nNeeded);
    progCheck.title     = tip('Check',       nCheck);
    progNotNeeded.title = tip('Not Needed',  nNotNeeded);
    progCompleted.title = tip('Completed',   nCompleted);

    // Update inline label text (shown on tap)
    function labelText(pctVal) { return Math.round(pctVal) + '%'; }
    progCompleted.dataset.pct  = labelText(nCompleted  / total * 100);
    progNeeded.dataset.pct     = labelText(nNeeded     / total * 100);
    progCheck.dataset.pct      = labelText(nCheck      / total * 100);
    progNotNeeded.dataset.pct  = labelText(nNotNeeded  / total * 100);
  }

  // Tap a segment to reveal/hide its percentage label
  document.getElementById('progress-bar').addEventListener('click', e => {
    const seg = e.target.closest('.progress-segment');
    if (!seg) return;
    // Toggle this one; close all others
    const isOpen = seg.classList.contains('seg-open');
    document.querySelectorAll('.progress-segment').forEach(s => s.classList.remove('seg-open'));
    if (!isOpen) seg.classList.add('seg-open');
  });

  // Clicking outside the bar closes any open label
  document.addEventListener('click', e => {
    if (!e.target.closest('#progress-bar')) {
      document.querySelectorAll('.progress-segment.seg-open')
        .forEach(s => s.classList.remove('seg-open'));
    }
  });

  // ────────────────────────────────────────────────────────────────
  // Render
  // ────────────────────────────────────────────────────────────────
  // NOT mirrored in app.test.js: touches the DOM, so it is not a pure helper.
  function setHidden(el, hidden) { if (el) el.classList.toggle('hidden', hidden); }

  /**
   * render is the tab dispatcher. It owns the visibility of all four containers
   * BEFORE dispatching, because renderGroceryTab does not run on the Recipes tab
   * and would otherwise leave #empty-state visible across a switch.
   */
  function render() {
    setHidden(gc,       activeTab !== 'grocery');
    setHidden(emptyEl,  activeTab !== 'grocery' || items.length > 0);
    setHidden(rc,       activeTab !== 'recipes');
    setHidden(rEmptyEl, activeTab !== 'recipes' || recipes.length > 0);
    renderProgressBar();                 // sole owner of #progress-bar
    if (activeTab === 'recipes') renderRecipesTab();
    else                         renderGroceryTab();
  }

  /**
   * renderRecipesTab fills #recipes-container and nothing else. The dispatcher
   * above already owns every container's visibility, so hiding or showing
   * anything here would give one element two writers.
   */
  function renderRecipesTab() {
    // Step 1 (PM-3): one activeElement read, BEFORE the wipe. Reading it after
    // would report the button that triggered this pass rather than the input
    // the user was typing in on some other card.
    const active = document.activeElement;
    if (active && active.classList &&
        active.classList.contains('recipe-ingredient-input')) {
      focusRecipeId = active.dataset.recipeId;
      // Re-read value and caret from the live element rather than trusting what
      // the `input` listener last captured. Arrow keys, Home/End and a click
      // move the caret without firing `input`, so a user who repositions the
      // caret and then receives an SSE tick would have it yanked back to
      // wherever they last TYPED. This is the only read that is always current.
      recipeDrafts[focusRecipeId] = { value: active.value, caret: active.selectionStart };
    }

    // Step 2: rebuild, with the ingredient inputs empty in the markup.
    rc.innerHTML = '';

    const ordered = recipesForRender(recipes);
    ordered.forEach((r, idx) => {
      const ing    = ingredientsForRecipe(items, r.id);
      const isOpen = !collapsedRecipes[r.id];
      const rid    = esc(r.id);
      const rname  = esc(r.name);
      const first  = idx === 0;
      const last   = idx === ordered.length - 1;

      const card = document.createElement('div');
      // data-recipe-id belongs on the card itself, not only on the inner
      // controls: it is what revealRecipe queries on (AC-9.7).
      card.dataset.recipeId = r.id;
      card.className = 'recipe-card' + (r.enabled ? ' recipe-card--enabled' : '');

      const header = document.createElement('div');
      header.className = 'recipe-header' + (isOpen ? ' open' : '');
      header.innerHTML = `
        <label class="recipe-switch" title="${r.enabled ? 'Disable' : 'Enable'} ${rname}">
          <input type="checkbox" class="recipe-switch-input"
                 data-recipe-id="${rid}"${r.enabled ? ' checked' : ''}
                 aria-label="Enable ${rname}">
          <span class="recipe-switch-track"><span class="recipe-switch-thumb"></span></span>
        </label>
        <span class="recipe-title" data-recipe-id="${rid}" title="Rename">${rname}</span>
        <span class="recipe-meta">
          <button type="button" class="recipe-move-btn" data-recipe-id="${rid}"
                  data-move="-1"${first ? ' data-edge="1" disabled' : ''}
                  aria-label="Move ${rname} up">&#9650;</button>
          <button type="button" class="recipe-move-btn" data-recipe-id="${rid}"
                  data-move="1"${last ? ' data-edge="1" disabled' : ''}
                  aria-label="Move ${rname} down">&#9660;</button>
          <span class="recipe-count">${ing.length}</span>
          <button type="button" class="recipe-ingredient-delete recipe-delete-btn"
                  data-recipe-id="${rid}"
                  aria-label="Delete ${rname}">&#10005;</button>
          <svg class="recipe-chevron" viewBox="0 0 16 16" fill="none"
               stroke="currentColor" stroke-width="2"
               stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 6l4 4 4-4"/>
          </svg>
        </span>`;

      const body = document.createElement('div');
      body.className = 'recipe-body' + (isOpen ? '' : ' collapsed');

      if (ing.length === 0) {
        const hint = document.createElement('div');
        hint.className   = 'recipe-ingredient-empty';
        hint.textContent = 'No ingredients yet';
        body.appendChild(hint);
      } else {
        ing.forEach(item => {
          const row = document.createElement('div');
          row.className = 'recipe-ingredient-row';
          row.innerHTML = `
            <span class="recipe-ingredient-name">${esc(item.name)}</span>
            <button type="button" class="recipe-ingredient-delete"
                    data-recipe-id="${rid}" data-item-id="${esc(item.id)}"
                    aria-label="Remove ${esc(item.name)} from ${rname}">&#10005;</button>`;
          body.appendChild(row);
        });
      }

      const addRow = document.createElement('div');
      addRow.className = 'recipe-ingredient-add';
      addRow.innerHTML = `
        <input type="text" class="recipe-ingredient-input" data-recipe-id="${rid}"
               placeholder="Add an ingredient&hellip;" autocomplete="off"
               autocorrect="off" spellcheck="false" maxlength="120">
        <button type="button" class="recipe-move-btn recipe-ingredient-add-btn"
                data-recipe-id="${rid}"
                aria-label="Add an ingredient to ${rname}">&#43;</button>`;
      body.appendChild(addRow);

      card.appendChild(header);
      card.appendChild(body);
      rc.appendChild(card);
    });

    // Step 3: drafts go back as a PROPERTY. They are raw user text that never
    // reaches innerHTML, so no escaping is involved on this path at all.
    rc.querySelectorAll('.recipe-ingredient-input').forEach(input => {
      const draft = recipeDrafts[input.dataset.recipeId];
      input.value = (draft && draft.value) || '';
    });

    // Step 4: restore focus. The null-out is the last statement of the step and
    // sits outside every branch above it (R4-15) — a card that has gone away
    // must not leave a stale id behind to yank focus on some later pass.
    if (focusRecipeId) {
      const input = rc.querySelector(
        `.recipe-ingredient-input[data-recipe-id="${CSS.escape(focusRecipeId)}"]`);
      if (input) {
        input.focus();
        const draft = recipeDrafts[focusRecipeId];
        // No draft entry means the user focused without ever typing. Letting the
        // browser place the caret beats setSelectionRange(undefined, undefined),
        // which coerces to (0, 0) and jumps the caret to the start.
        if (draft) input.setSelectionRange(draft.caret, draft.caret);
      }
    }
    focusRecipeId = null;

    updateRecipeControlsDisabled();
  }

  // AC-10.1's lockout is gone by request: offline recipe edits now queue in
  // pendingOps and replay on reconnect, exactly as grocery-item edits always
  // could. What remains is the edge-state repair for the move buttons and
  // making sure nothing left over from the lockout era stays disabled.
  function updateRecipeControlsDisabled() {
    rc.querySelectorAll('.recipe-switch-input, .recipe-ingredient-input, ' +
                        '.recipe-ingredient-delete')
      .forEach(el => { el.disabled = false; });

    // Move buttons carry their own boundary state in data-edge: the first
    // card's up arrow and the last card's down arrow stay disabled whatever
    // sync is doing. The add button shares this class and carries no data-edge.
    rc.querySelectorAll('.recipe-move-btn').forEach(el => {
      el.disabled = el.dataset.edge === '1';
    });

    const submit = addForm.querySelector('button[type="submit"]');
    if (submit) submit.disabled = false;
  }

  function renderGroceryTab() {
    gc.innerHTML = '';
    emptyEl.classList.toggle('hidden', items.length > 0);

    const renderList = groupsForRender();

    renderList.forEach(group => {
      if (!groupIsVisible(items, group, visibilityMode)) return;

      const allGroupItems = itemsForGroup(group);
      const groupItems    = allGroupItems.filter(i => visibilityFilter(i, visibilityMode));
      const isVirtual  = group === NO_GROUP;
      const isOpen     = !collapsedGroups[group];

      // Skip empty real groups during drag so they can still act as drop targets.
      // Always show NO_GROUP section (it only appears when it has items).
      const section = document.createElement('div');
      section.className     = 'group-section' + (isVirtual ? ' group-section--nogroup' : '');
      section.dataset.group = group;

      const header = document.createElement('div');
      header.className = 'group-header' + (isOpen ? ' open' : '') + (isVirtual ? ' group-header--nogroup' : '');
      header.innerHTML = `
        <span class="group-title">${esc(groupLabel(group))}</span>
        <span class="group-meta">
          <span class="group-count">${visibilityMode !== 0 ? groupItems.length + '/' + allGroupItems.length : groupItems.length}</span>
          <svg class="group-chevron" viewBox="0 0 16 16" fill="none"
               stroke="currentColor" stroke-width="2"
               stroke-linecap="round" stroke-linejoin="round">
            <path d="M4 6l4 4 4-4"/>
          </svg>
        </span>`;
      header.addEventListener('click', () => {
        collapsedGroups[group] = !collapsedGroups[group];
        render();
      });

      const body = document.createElement('div');
      body.className     = 'group-body' + (isOpen ? '' : ' collapsed');
      body.dataset.group = group;

      if (groupItems.length === 0) {
        const hint = document.createElement('div');
        hint.className   = 'group-empty';
        hint.textContent = groupEmptyHint(group);
        body.appendChild(hint);
      } else {
        const ul = document.createElement('ul');
        ul.className     = 'item-list';
        ul.dataset.group = group;
        groupItems.forEach(item => ul.appendChild(buildRow(item)));
        body.appendChild(ul);
      }

      section.appendChild(header);
      section.appendChild(body);
      gc.appendChild(section);
    });
  }

  // ────────────────────────────────────────────────────────────────
  // Build item row
  // ────────────────────────────────────────────────────────────────
  function buildRow(item) {
    const li = document.createElement('li');
    li.className     = 'item-row' + (item.completed ? ' completed' : '');
    li.dataset.id    = item.id;
    li.dataset.group = item.group;

    // recipeSuffix is the single shipped implementation of the derived display
    // name; displayName is defined in terms of it. buildRow must never
    // re-concatenate the two halves, or the unit test stops testing what ships.
    const suffix = recipeSuffix(item, recipes);
    const owned  = isOwned(item);

    // Names the owning recipe rather than describing the missing trash icon.
    // AC-9.3 still holds — the control is removed, not left to fail — but the
    // explanation the criterion asked for was doing more harm than good as copy:
    // an imperative on a whole-row title read as a one-click delete on a row
    // that has no delete control. Empty for a dangling recipe_id, so the
    // guard is `if (tip)`, not `if (owned)`.
    const tip = ownedTooltip(item, recipes);
    if (tip) li.title = tip;

    li.innerHTML = `
      <span class="drag-handle" title="Drag to reorder or move group">
        <svg viewBox="0 0 14 14" fill="currentColor">
          <circle cx="4" cy="3"  r="1.2"/><circle cx="10" cy="3"  r="1.2"/>
          <circle cx="4" cy="7"  r="1.2"/><circle cx="10" cy="7"  r="1.2"/>
          <circle cx="4" cy="11" r="1.2"/><circle cx="10" cy="11" r="1.2"/>
        </svg>
      </span>
      <input type="checkbox" class="item-checkbox" data-id="${item.id}"
             ${item.completed ? 'checked' : ''}
             aria-label="Mark ${esc(item.name)} complete">
      <span class="item-content">
        <span class="item-name" data-id="${item.id}">${esc(item.name)}${
          // One element, not two: this button IS the suffix, so the row reads
          // "beef (Chili)" once and the whole of it is the reveal target.
          // The separator space is emitted HERE, outside the tag: a button is
          // inline-block, and leading collapsible white space at the start of an
          // inline formatting context is dropped — inside, it would render
          // "beef(Chili)". recipeSuffix keeps returning the leading space
          // because displayName concatenates on it.
          suffix
            ? ` <button class="recipe-chip item-recipe-suffix" data-recipe-id="${esc(item.recipe_id)}">${esc(suffix.trimStart())}</button>`
            : ''
        }</span>
        <span class="state-badge" data-state="${item.state}" data-id="${item.id}">
          ${STATE_LABELS[item.state]}
        </span>
      </span>
      <span class="item-actions">
        <button class="move-btn" data-id="${item.id}" title="Move to a different group"
                aria-label="Move ${esc(item.name)} to a different group">
          <svg viewBox="0 0 20 20" fill="none" stroke="currentColor"
               stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 10h11M9 5l5 5-5 5"/>
          </svg>
        </button>
        ${owned ? '' : `<button class="delete-btn" data-id="${item.id}" aria-label="Delete ${esc(item.name)}">
          <svg viewBox="0 0 20 20" fill="none" stroke="currentColor"
               stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 6h14M8 6V4h4v2M5 6l1 11h8l1-11"/>
          </svg>
        </button>`}
      </span>`;

    attachDragToHandle(li, li.querySelector('.drag-handle'));
    return li;
  }

  // ────────────────────────────────────────────────────────────────
  // Drag  (handle-only, mouse + touch)
  // ────────────────────────────────────────────────────────────────
  function attachDragToHandle(row, handle) {
    function clearIndicators() {
      gc.querySelectorAll('.drag-over-above,.drag-over-below')
        .forEach(el => el.classList.remove('drag-over-above', 'drag-over-below'));
      gc.querySelectorAll('.drag-target')
        .forEach(el => el.classList.remove('drag-target'));
    }

    function pointerStart() {
      drag.active   = true;
      drag.id       = row.dataset.id;
      drag.srcGroup = row.dataset.group;
      row.classList.add('dragging');
    }

    function pointerMove(clientX, clientY) {
      if (!drag.active) return;
      clearIndicators();
      const el = document.elementFromPoint(clientX, clientY);
      if (!el) return;

      const targetSection = el.closest('.group-section');
      const targetGroup   = targetSection?.dataset?.group;

      if (targetGroup && targetGroup !== drag.srcGroup) {
        targetSection.classList.add('drag-target');
        if (collapsedGroups[targetGroup]) {
          collapsedGroups[targetGroup] = false;
          render();
          const reRendered = gc.querySelector(`[data-id="${drag.id}"]`);
          if (reRendered) reRendered.classList.add('dragging');
          const sec = gc.querySelector(
            `.group-section[data-group="${CSS.escape(targetGroup)}"]`
          );
          if (sec) sec.classList.add('drag-target');
        }
        return;
      }

      const targetRow = el.closest('.item-row');
      if (targetRow && targetRow.dataset.id !== drag.id) {
        const rect = targetRow.getBoundingClientRect();
        targetRow.classList.add(
          clientY < rect.top + rect.height / 2 ? 'drag-over-above' : 'drag-over-below'
        );
      }
    }

    function pointerEnd(clientX, clientY) {
      if (!drag.active) return;
      drag.active = false;
      const el = document.elementFromPoint(clientX, clientY);
      clearIndicators();
      gc.querySelectorAll('.item-row.dragging').forEach(r => r.classList.remove('dragging'));

      if (!el) { drag.id = null; return; }

      const targetSection = el.closest('.group-section');
      const targetGroup   = targetSection?.dataset?.group;
      const targetRow     = el.closest('.item-row');

      if (targetGroup && targetGroup !== drag.srcGroup) {
        const destIds = itemsForGroup(targetGroup).map(i => i.id);
        destIds.push(drag.id);
        moveItem(drag.id, targetGroup, destIds);
      } else if (
        targetRow &&
        targetRow.dataset.id !== drag.id &&
        targetRow.dataset.group === drag.srcGroup
      ) {
        const ids     = itemsForGroup(drag.srcGroup).map(i => i.id);
        const fromIdx = ids.indexOf(drag.id);
        const rect    = targetRow.getBoundingClientRect();
        let   toIdx   = ids.indexOf(targetRow.dataset.id);
        if (clientY >= rect.top + rect.height / 2) toIdx++;
        ids.splice(fromIdx, 1);
        ids.splice(Math.max(0, fromIdx < toIdx ? toIdx - 1 : toIdx), 0, drag.id);
        reorderWithinGroup(drag.srcGroup, ids);
      } else {
        render();
      }
      drag.id = null; drag.srcGroup = null;
    }

    // Mouse
    handle.addEventListener('mousedown', e => {
      e.preventDefault();
      pointerStart();
      const onMove = e => pointerMove(e.clientX, e.clientY);
      const onUp   = e => {
        pointerEnd(e.clientX, e.clientY);
        document.removeEventListener('mousemove', onMove);
        document.removeEventListener('mouseup',   onUp);
      };
      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup',   onUp);
    });

    // Touch
    handle.addEventListener('touchstart', e => {
      pointerStart();
      const onMove = e => {
        const t = e.touches[0];
        pointerMove(t.clientX, t.clientY);
        e.preventDefault();
      };
      const onEnd = e => {
        const t = e.changedTouches[0];
        pointerEnd(t.clientX, t.clientY);
        handle.removeEventListener('touchmove', onMove);
        handle.removeEventListener('touchend',  onEnd);
      };
      handle.addEventListener('touchmove', onMove, { passive: false });
      handle.addEventListener('touchend',  onEnd);
    }, { passive: true });
  }

  // ────────────────────────────────────────────────────────────────
  // Move-to-group modal — a tap-driven alternative to the drag handle.
  // Dragging an item across a long list (e.g. up out of the bottom-most
  // Unallocated group) is fiddly on a touch screen, so every row also gets
  // a button that opens a plain list of the other groups; tapping one calls
  // moveItem exactly as a cross-group drop would, appending to that group's
  // end (same destIds derivation as pointerEnd's cross-group branch above).
  // ────────────────────────────────────────────────────────────────
  const moveModal = document.createElement('div');
  moveModal.className = 'modal-overlay hidden';
  moveModal.setAttribute('role', 'dialog');
  moveModal.setAttribute('aria-modal', 'true');
  document.body.appendChild(moveModal);

  let moveModalItemId = null;

  function closeMoveModal() {
    moveModal.classList.add('hidden');
    moveModal.innerHTML = '';
    moveModalItemId = null;
  }

  function openMoveModal(itemId) {
    const item = items.find(i => i.id === itemId);
    if (!item) return;
    moveModalItemId = itemId;
    const dest = [...groups, NO_GROUP].filter(g => g !== item.group);
    const rows = dest.map(g => `
      <li><button type="button" class="move-modal-item" data-group="${esc(g)}">
        ${esc(groupLabel(g))}
      </button></li>`).join('');
    moveModal.innerHTML = `
      <div class="modal">
        <h2 class="modal-title">Move &ldquo;${esc(item.name)}&rdquo;</h2>
        <ul class="move-modal-list">${rows ||
          '<li class="move-modal-empty">No other groups yet</li>'}</ul>
        <div class="modal-actions modal-actions--right">
          <button type="button" class="btn btn-ghost" id="move-modal-cancel">Cancel</button>
        </div>
      </div>`;
    moveModal.classList.remove('hidden');
  }

  moveModal.addEventListener('click', e => {
    if (e.target === moveModal || e.target.closest('#move-modal-cancel')) {
      closeMoveModal();
      return;
    }
    const btn = e.target.closest('.move-modal-item');
    if (!btn) return;
    const id         = moveModalItemId;
    const targetGroup = btn.dataset.group;
    closeMoveModal();
    const destIds = itemsForGroup(targetGroup).map(i => i.id);
    destIds.push(id);
    moveItem(id, targetGroup, destIds);
  });

  // ────────────────────────────────────────────────────────────────
  // Event delegation  (list)
  // ────────────────────────────────────────────────────────────────
  gc.addEventListener('click', e => {
    if (drag.id) return;
    const cb    = e.target.closest('.item-checkbox');
    const chip  = e.target.closest('.recipe-chip');
    const name  = e.target.closest('.item-name');
    const badge = e.target.closest('.state-badge');
    const move  = e.target.closest('.move-btn');
    const del   = e.target.closest('.delete-btn');
    if (cb)    { toggleComplete(cb.dataset.id);  return; }
    // Mandatory ordering: the chip is a DESCENDANT of .item-name, so the arm
    // below would match from it and cycle the row's state instead of revealing
    // the recipe. T8 owns the switch, the expand and the scroll together.
    if (chip)  { revealRecipe(chip.dataset.recipeId); return; }
    if (name)  { cycleState(name.dataset.id);    return; }
    if (badge) { cycleState(badge.dataset.id);   return; }
    if (move)  { openMoveModal(move.dataset.id); return; }
    if (del)   { deleteItem(del.dataset.id);     return; }
  });

  // ────────────────────────────────────────────────────────────────
  // Recipe modal
  //
  // Built here rather than in index.html because T8 owns app.js alone, and the
  // markup is the reset modal's, which style.css already covers. One overlay
  // serves both the delete confirmation and the rename prompt; the confirm
  // callback is what differs. No native alert/confirm: they block the page.
  // ────────────────────────────────────────────────────────────────
  const recipeModal = document.createElement('div');
  recipeModal.className = 'modal-overlay hidden';
  recipeModal.setAttribute('role', 'dialog');
  recipeModal.setAttribute('aria-modal', 'true');
  document.body.appendChild(recipeModal);

  let recipeModalConfirm = null;

  function closeRecipeModal() {
    recipeModal.classList.add('hidden');
    recipeModalConfirm = null;
  }

  function openRecipeModal(html, onConfirm) {
    recipeModal.innerHTML  = html;
    recipeModalConfirm     = onConfirm;
    recipeModal.classList.remove('hidden');
    const input = recipeModal.querySelector('#recipe-modal-input');
    if (input) { input.focus(); input.select(); return; }
    const ok = recipeModal.querySelector('#recipe-modal-confirm');
    if (ok) ok.focus();
  }

  // Close first, then call: the callback paints, and a modal still on screen
  // during that pass would sit over its own result.
  function commitRecipeModal() {
    const fn    = recipeModalConfirm;
    const input = recipeModal.querySelector('#recipe-modal-input');
    const value = input ? input.value.trim() : '';
    closeRecipeModal();
    if (fn) fn(value);
  }

  recipeModal.addEventListener('click', e => {
    if (e.target === recipeModal || e.target.closest('#recipe-modal-cancel')) {
      closeRecipeModal();
      return;
    }
    if (e.target.closest('#recipe-modal-confirm')) commitRecipeModal();
  });

  recipeModal.addEventListener('keydown', e => {
    if (e.key !== 'Enter') return;
    if (!recipeModal.querySelector('#recipe-modal-input')) return;
    e.preventDefault();
    commitRecipeModal();
  });

  function openDeleteRecipeModal(r) {
    const count = ingredientsForRecipe(items, r.id).length;
    openRecipeModal(`
      <div class="modal">
        <div class="modal-icon modal-icon--warning">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor"
               stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0
                     1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
            <line x1="12" y1="9"  x2="12"    y2="13"/>
            <line x1="12" y1="17" x2="12.01" y2="17"/>
          </svg>
        </div>
        <h2 class="modal-title">Delete &ldquo;${esc(r.name)}&rdquo;?</h2>
        <p class="modal-body">
          This also removes its <strong>${count}</strong>
          ingredient${count === 1 ? '' : 's'} from the grocery list &mdash;
          <strong>including any you have moved into other groups</strong>.
        </p>
        <div class="modal-actions">
          <button id="recipe-modal-cancel"  class="btn btn-ghost">No, cancel</button>
          <button id="recipe-modal-confirm" class="btn btn-danger">Yes, delete</button>
        </div>
      </div>`, () => deleteRecipe(r.id));
  }

  function openRenameRecipeModal(r) {
    openRecipeModal(`
      <div class="modal">
        <h2 class="modal-title">Rename recipe</h2>
        <div class="title-modal-form">
          <label class="title-modal-label" for="recipe-modal-input">Name</label>
          <input id="recipe-modal-input" type="text" class="title-modal-input"
                 maxlength="120" autocomplete="off" autocorrect="off"
                 spellcheck="false" value="${esc(r.name)}">
        </div>
        <div class="modal-actions">
          <button id="recipe-modal-cancel"  class="btn btn-ghost">Cancel</button>
          <button id="recipe-modal-confirm" class="btn btn-primary">Save</button>
        </div>
      </div>`, name => renameRecipe(r.id, name));
  }

  // ────────────────────────────────────────────────────────────────
  // Event delegation  (recipes)
  //
  // Three listeners, all bound EXACTLY ONCE here at module scope. Never inside
  // a render function: emptying #recipes-container destroys its children, but
  // these are attached to the container itself, which survives — so a listener
  // registered per pass would accumulate, and one click would fire N times.
  // Delegation is what makes binding once safe, since every arm matches with
  // closest() against markup that did not exist at bind time.
  // ────────────────────────────────────────────────────────────────
  rc.addEventListener('click', e => {
    const sw = e.target.closest('.recipe-switch-input');
    if (sw) { toggleRecipe(sw.dataset.recipeId, sw.checked); return; }
    // A click on the label's track arrives here first, while checked still
    // holds the OLD value; the browser then synthesises a click on the input
    // itself, which the arm above handles with the new one. Absorbing the
    // label's event keeps the collapse arm at the bottom from firing too.
    if (e.target.closest('.recipe-switch')) return;

    const del = e.target.closest('.recipe-delete-btn');
    if (del) {
      const r = recipeById(recipes, del.dataset.recipeId);
      if (r) openDeleteRecipeModal(r);
      return;
    }

    // Matched before .recipe-move-btn, whose styling it borrows.
    const addBtn = e.target.closest('.recipe-ingredient-add-btn');
    if (addBtn) {
      const input = rc.querySelector(
        `.recipe-ingredient-input[data-recipe-id="${CSS.escape(addBtn.dataset.recipeId)}"]`);
      const name = input ? input.value.trim() : '';
      if (name) addIngredient(addBtn.dataset.recipeId, name);
      return;
    }

    const mv = e.target.closest('.recipe-move-btn');
    if (mv) { moveRecipe(mv.dataset.recipeId, Number(mv.dataset.move)); return; }

    const title = e.target.closest('.recipe-title');
    if (title) {
      const r = recipeById(recipes, title.dataset.recipeId);
      if (r) openRenameRecipeModal(r);
      return;
    }

    const ingDel = e.target.closest('.recipe-ingredient-delete');
    if (ingDel) {
      removeIngredient(ingDel.dataset.recipeId, ingDel.dataset.itemId);
      return;
    }

    // Last arm: anything else on the header collapses or expands the card,
    // through the same map-then-paint path the group headers already use.
    const header = e.target.closest('.recipe-header');
    if (header) {
      const card = header.closest('.recipe-card');
      if (!card) return;
      const id = card.dataset.recipeId;
      if (collapsedRecipes[id]) delete collapsedRecipes[id];
      else                      collapsedRecipes[id] = true;
      render();
    }
  });

  rc.addEventListener('input', e => {
    const el = e.target.closest('.recipe-ingredient-input');
    if (!el) return;
    // Captured as the user types, not scraped at wipe time: by then the
    // triggering element may be a button on a different card.
    recipeDrafts[el.dataset.recipeId] = { value: el.value, caret: el.selectionStart };
  });

  rc.addEventListener('keydown', e => {
    if (e.key !== 'Enter') return;
    const el = e.target.closest('.recipe-ingredient-input');
    if (!el) return;
    // The inputs are deliberately not wrapped in a form — a nested form inside
    // the page's existing add flow is not something this file does anywhere,
    // and this is what lets a phone add several ingredients without dismissing
    // the keyboard to reach a button.
    e.preventDefault();
    const name = el.value.trim();
    if (name) addIngredient(el.dataset.recipeId, name);
  });

  addForm.addEventListener('submit', e => {
    e.preventDefault();
    const name = newInput.value.trim();
    if (!name) return;
    // AC-8.6: the footer is shared. setActiveTab has already hidden the group
    // select and swapped the placeholder; this is the other half.
    if (activeTab === 'recipes') {
      // Clear only on acceptance. A duplicate recipe name leaves the text in
      // place, which is the feedback: blanking the input on a refusal would
      // look identical to success and lose what the user typed.
      if (recipeNameTaken(recipes, name)) { newInput.select(); return; }
      newInput.value = '';
      createRecipe(name);
    } else {
      newInput.value = '';
      addItem(name, groupSel.value || groups[0] || NO_GROUP);
    }
    newInput.focus();
  });

  syncTog.addEventListener('change', async () => {
    syncEnabled = syncTog.checked;
    if (syncEnabled) {
      banner.classList.add('hidden');
      // Replay FIRST, refresh SECOND. Offline edits happened later in the
      // timeline than anything the server holds, so they are pushed up before
      // the authoritative re-read \u2014 refreshing first would adopt the stale
      // server state over them, which is the data-loss this ordering fixes.
      await replayPendingOps();
      await refreshAll();
      connectSSE();
    } else {
      disconnectSSE();
      banner.textContent =
        '\u26a0 Offline mode \u2014 changes will sync when you reconnect';
      banner.classList.remove('hidden');
    }
    updateRecipeControlsDisabled();
  });

  // ────────────────────────────────────────────────────────────────
  // SSE live sync
  // Connects to /api/events; re-fetches items whenever the server
  // broadcasts a "refresh" event (i.e. after any mutation).
  // Auto-reconnects on drop; shows a banner while disconnected.
  // ────────────────────────────────────────────────────────────────
  let evtSource = null;

  function connectSSE() {
    if (!syncEnabled) return;
    if (evtSource) { evtSource.close(); evtSource = null; }

    evtSource = new EventSource('/api/events');

    evtSource.addEventListener('message', () => {
      // Server sent a refresh signal — re-read items, recipes AND config (the
      // title may have changed) in one pass, then render once. Three separate
      // fetches would each render, opening a window where an item still
      // references a recipe already dropped from the recipes array.
      refreshAll();
    });

    evtSource.addEventListener('open', () => {
      banner.classList.add('hidden');
    });

    evtSource.onerror = () => {
      // EventSource will auto-reconnect; show banner in the meantime.
      banner.textContent = '\u26a0 Connection lost \u2014 reconnecting\u2026';
      banner.classList.remove('hidden');
    };
  }

  function disconnectSSE() {
    if (evtSource) { evtSource.close(); evtSource = null; }
  }

  // ────────────────────────────────────────────────────────────────
  // Init
  // ────────────────────────────────────────────────────────────────
  (async () => {
    // Seeded FIRST: loadConfig calls renderProgressBar, which now reads
    // activeTab through progressBarVisible.
    activeTab = localStorage.getItem(TAB_KEY) === 'recipes' ? 'recipes' : 'grocery';
    await loadConfig();
    await fetchRecipesData();
    await fetchItems();
    // Applies the chrome, not just the data. Everything that reconciles the
    // header with activeTab lives inside setActiveTab, so without this a reload
    // onto the Recipes tab shows the recipe view under a Grocery-tab header.
    setActiveTab(activeTab);
    connectSSE();
  })();

})();
