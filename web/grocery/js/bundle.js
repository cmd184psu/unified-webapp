// web/grocery/js/main.ts
import { ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";
var themes = new ThemeManager({ module: "grocery", default: "dark" });
themes.apply();
var hamburger = new HamburgerMenu({
  title: "Grocery",
  items: [],
  themePicker: true,
  themes
});
document.querySelector(".header-right")?.prepend(hamburger.trigger);
var STATES = ["needed", "check", "not_needed"];
var STATE_LABELS = { needed: "Needed", check: "Check", not_needed: "Not Needed" };
var NO_GROUP = "No Group";
var items = [];
var groups = [];
var syncEnabled = true;
var collapsedGroups = {};
var visibilityMode = 0;
var showProgress = false;
var syncIntervalSeconds = 1;
var listTitle = "Grocery List";
var recipes = [];
var activeTab = "grocery";
var pendingOps = [];
var localSeq = 0;
function newLocalId() {
  return "local-" + ++localSeq + "-" + Date.now();
}
var collapsedRecipes = {};
var TAB_KEY = "grocery.activeTab";
var recipeDrafts = {};
var focusRecipeId = null;
var drag = { active: false, id: null, srcGroup: null };
var gc = document.getElementById("groups-container");
var emptyEl = document.getElementById("empty-state");
var addForm = document.getElementById("add-form");
var newInput = document.getElementById("new-item-input");
var groupSel = document.getElementById("group-select");
var syncTog = document.getElementById("sync-toggle");
var banner = document.getElementById("offline-banner");
var resetBtn = document.getElementById("reset-btn");
var groupsBtn = document.getElementById("groups-btn");
var collapseAll = document.getElementById("collapse-all-btn");
var expandAll = document.getElementById("expand-all-btn");
var hideNotNeededBtn = document.getElementById("hide-not-needed-btn");
var tabGrocery = document.getElementById("tab-grocery");
var tabRecipes = document.getElementById("tab-recipes");
var rc = document.getElementById("recipes-container");
var rEmptyEl = document.getElementById("recipes-empty-state");
var resetModal = document.getElementById("reset-modal");
var resetCancel = document.getElementById("reset-cancel");
var resetConfirm = document.getElementById("reset-confirm");
var groupsModal = document.getElementById("groups-modal");
var groupsList = document.getElementById("groups-modal-list");
var groupsForm = document.getElementById("groups-modal-form");
var groupsInput = document.getElementById("groups-modal-input");
var groupsClose = document.getElementById("groups-modal-close");
var titleModal = document.getElementById("title-modal");
var titleInput = document.getElementById("title-modal-input");
var titleError = document.getElementById("title-modal-error");
var titleSaveBtn = document.getElementById("title-modal-save");
var titleCancelBtn = document.getElementById("title-modal-cancel");
var titleModalForm = document.getElementById("title-modal-form");
var editTitleBtn = document.getElementById("edit-title-btn");
function openTitleModal() {
  titleInput.value = listTitle;
  titleInput.classList.remove("input-error");
  titleError.classList.add("hidden");
  titleError.textContent = "";
  titleModal.classList.remove("hidden");
  requestAnimationFrame(() => {
    titleInput.focus();
    titleInput.select();
  });
}
function closeTitleModal() {
  titleModal.classList.add("hidden");
}
async function saveTitleModal() {
  const val = titleInput.value.trim();
  if (!val) {
    titleInput.classList.add("input-error");
    titleError.textContent = "Title cannot be empty.";
    titleError.classList.remove("hidden");
    titleInput.focus();
    return;
  }
  titleSaveBtn.disabled = true;
  try {
    await api("POST", "/api/config/title", { name: val });
    listTitle = val;
    document.title = listTitle;
    editTitleBtn.textContent = listTitle;
    const logo = document.querySelector(".app-logo");
    if (logo) logo.setAttribute("aria-label", listTitle);
    closeTitleModal();
  } catch (err) {
    titleInput.classList.add("input-error");
    titleError.textContent = "Could not save title. Please try again.";
    titleError.classList.remove("hidden");
  } finally {
    titleSaveBtn.disabled = false;
  }
}
editTitleBtn.addEventListener("click", openTitleModal);
titleCancelBtn.addEventListener("click", closeTitleModal);
titleSaveBtn.addEventListener("click", saveTitleModal);
titleModalForm.addEventListener("submit", (e) => {
  e.preventDefault();
  saveTitleModal();
});
titleModal.addEventListener("click", (e) => {
  if (e.target === titleModal) closeTitleModal();
});
titleModal.addEventListener("keydown", (e) => {
  if (e.key === "Escape") closeTitleModal();
});
titleInput.addEventListener("input", () => {
  titleInput.classList.remove("input-error");
  titleError.classList.add("hidden");
});
var VISIBILITY_MODES = ["show_all", "hide_not_needed", "hide_completed"];
function nextVisibilityMode(current) {
  return (current + 1) % VISIBILITY_MODES.length;
}
function visibilityFilter(item, mode) {
  if (mode === 0) return true;
  if (item.state === "not_needed") return false;
  if (mode === 2 && item.completed) return false;
  return true;
}
function groupIsVisible(allItems, group, mode) {
  if (mode === 0) return true;
  return allItems.some((i) => i.group === group && visibilityFilter(i, mode));
}
function nextState(s) {
  return STATES[(STATES.indexOf(s) + 1) % STATES.length];
}
function itemsForGroup(group) {
  return [...items].filter((i) => i.group === group).sort(
    (a, b) => a.order !== b.order ? a.order - b.order : new Date(a.created_at) - new Date(b.created_at)
  );
}
function esc(str) {
  return String(str).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}
function groupsForRender() {
  const hasOrphans = items.some((i) => i.group === NO_GROUP);
  return hasOrphans ? [...groups, NO_GROUP] : [...groups];
}
function recipeNameTaken(recipes2, name, exceptId) {
  const n = String(name).trim().toLowerCase();
  return recipes2.some((r) => r.id !== exceptId && r.name.trim().toLowerCase() === n);
}
function nextRecipeOrder(recipes2) {
  return recipes2.reduce((max, r) => r.order >= max ? r.order + 1 : max, 0);
}
function recipeById(recipes2, id) {
  return recipes2.find((r) => r.id === id) || null;
}
function recipesForRender(recipes2) {
  return [...recipes2].sort(
    (a, b) => a.order !== b.order ? a.order - b.order : new Date(a.created_at) - new Date(b.created_at)
  );
}
function ingredientsForRecipe(items2, recipeId) {
  return items2.filter((i) => i.recipe_id === recipeId).sort((a, b) => new Date(a.created_at) - new Date(b.created_at));
}
function isOwned(item) {
  return !!(item && item.recipe_id);
}
function recipeSuffix(item, recipes2) {
  if (!isOwned(item)) return "";
  const r = recipeById(recipes2, item.recipe_id);
  return r ? ` (${r.name})` : "";
}
function ownedTooltip(item, recipes2) {
  if (!isOwned(item)) return "";
  const r = recipeById(recipes2, item.recipe_id);
  return r ? `Belongs to recipe ${r.name}` : "";
}
function groupLabel(group) {
  return group === "No Group" ? "Unallocated" : group;
}
function groupEmptyHint(group) {
  return group === "No Group" ? "Nothing unallocated" : "No items";
}
function tabControlVisible(controlId, tab) {
  const groceryOnly = ["#hide-not-needed-btn", "#reset-btn", "#groups-btn"];
  return groceryOnly.includes(controlId) ? tab === "grocery" : true;
}
function progressBarVisible(showProgress2, itemCount, tab) {
  return !!showProgress2 && itemCount > 0 && tab === "grocery";
}
function applyRecipeToggle(items2, recipeId, enabled) {
  return items2.map(
    (i) => i.recipe_id === recipeId ? { ...i, state: enabled ? "needed" : "not_needed", completed: false } : i
  );
}
async function api(method, path, body) {
  const opts = { method, headers: { "Content-Type": "application/json" } };
  if (body !== void 0) opts.body = JSON.stringify(body);
  const res = await fetch(path, opts);
  if (res.status === 204) return null;
  return res.json();
}
async function loadConfigData() {
  const cfg = await api("GET", "/api/config").catch(() => null);
  groups = cfg?.groups || [];
  showProgress = cfg?.progress || false;
  syncIntervalSeconds = cfg?.sync_interval_seconds ?? 1;
  if (cfg?.title) {
    listTitle = cfg.title;
    document.title = listTitle;
    const btn = document.getElementById("edit-title-btn");
    if (btn) btn.textContent = listTitle;
    const logo = document.querySelector(".app-logo");
    if (logo) logo.setAttribute("aria-label", listTitle);
  }
}
async function loadConfig() {
  await loadConfigData();
  rebuildGroupSelect();
  renderProgressBar();
}
function mergeServerItems(serverItems) {
  const pendingLocal = items.filter((i) => i.id.startsWith("local-") && !i._deleted);
  items = [...serverItems, ...pendingLocal];
}
function mergeServerRecipes(serverRecipes) {
  const pendingLocal = recipes.filter((r) => r.id.startsWith("local-") && !r._deleted);
  recipes = [...serverRecipes, ...pendingLocal];
}
async function fetchItemsData() {
  const data = await api("GET", "/api/items").catch(() => []);
  mergeServerItems(data || []);
}
async function fetchItems() {
  await fetchItemsData();
  render();
}
async function fetchRecipesData() {
  const data = await api("GET", "/api/recipes").catch(() => []);
  mergeServerRecipes(data || []);
}
async function refreshAll() {
  await Promise.all([fetchItemsData(), fetchRecipesData(), loadConfigData()]);
  rebuildGroupSelect();
  render();
}
var replayingOps = false;
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
function rebuildGroupSelect() {
  const prev = groupSel.value;
  groupSel.innerHTML = "";
  groups.forEach((g) => {
    const opt = document.createElement("option");
    opt.value = g;
    opt.textContent = g.length > 14 ? g.slice(0, 13) + "\u2026" : g;
    opt.title = g;
    groupSel.appendChild(opt);
  });
  if (prev && groups.includes(prev)) groupSel.value = prev;
}
async function addItem(name, group) {
  const tempId = newLocalId();
  const it = {
    id: tempId,
    name,
    group,
    state: "needed",
    completed: false,
    order: items.filter((i) => i.group === group).length,
    created_at: (/* @__PURE__ */ new Date()).toISOString()
  };
  items.push(it);
  render();
  const sync = () => syncAddItem(it);
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function syncAddItem(it) {
  if (it._deleted) return;
  const saved = await api("POST", "/api/items", { name: it.name, group: it.group }).catch(() => null);
  if (saved) {
    Object.assign(it, replayingOps ? { id: saved.id, created_at: saved.created_at } : saved);
    render();
  }
}
async function toggleComplete(id) {
  const item = items.find((i) => i.id === id);
  if (!item) return;
  item.completed = !item.completed;
  render();
  const sync = () => syncPatchItem(item, { completed: item.completed });
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function cycleState(id) {
  const item = items.find((i) => i.id === id);
  if (!item) return;
  item.state = nextState(item.state);
  render();
  const sync = () => syncPatchItem(item, { state: item.state });
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function syncPatchItem(item, fields) {
  if (item.id.startsWith("local-")) return;
  await api("PATCH", `/api/items/${item.id}`, fields).catch(() => null);
}
async function deleteItem(id) {
  const item = items.find((i) => i.id === id);
  if (!item) return;
  items = items.filter((i) => i.id !== id);
  render();
  item._deleted = true;
  if (item.id.startsWith("local-")) return;
  const sync = () => api("DELETE", `/api/items/${item.id}`).catch(() => null);
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function moveItem(id, toGroup, orderIds) {
  const item = items.find((i) => i.id === id);
  if (!item) return;
  item.group = toGroup;
  orderIds.forEach((oid, idx) => {
    const it = items.find((i) => i.id === oid);
    if (it) it.order = idx;
  });
  render();
  const sync = () => api("POST", "/api/move", {
    id: item.id,
    group: item.group,
    order_ids: itemsForGroup(item.group).map((i) => i.id).filter((x) => !x.startsWith("local-"))
  }).catch(() => null);
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function reorderWithinGroup(group, orderIds) {
  orderIds.forEach((oid, idx) => {
    const it = items.find((i) => i.id === oid);
    if (it) it.order = idx;
  });
  render();
  const sync = () => api("POST", "/api/reorder", {
    group,
    ids: itemsForGroup(group).map((i) => i.id).filter((x) => !x.startsWith("local-"))
  }).catch(() => null);
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
function adoptRecipePatch(patched) {
  const r = recipes.find((x) => x.id === patched.recipe.id);
  if (r) Object.assign(r, patched.recipe);
  if (Array.isArray(patched.items)) mergeServerItems(patched.items);
}
async function createRecipe(name) {
  name = String(name).trim();
  if (!name || recipeNameTaken(recipes, name)) return false;
  const r = {
    id: newLocalId(),
    name,
    enabled: false,
    order: nextRecipeOrder(recipes),
    created_at: (/* @__PURE__ */ new Date()).toISOString()
  };
  recipes.push(r);
  render();
  const sync = () => syncCreateRecipe(r);
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
  return true;
}
async function syncCreateRecipe(r) {
  if (r._deleted) return;
  for (let attempt = 1; attempt <= 5; attempt++) {
    const name = attempt === 1 ? r.name : `${r.name} (${attempt})`;
    const saved = await api("POST", "/api/recipes", { name }).catch(() => null);
    if (saved === null) return;
    if (!saved.id) continue;
    const tempId = r.id;
    Object.assign(r, saved, { enabled: r.enabled });
    items.forEach((i) => {
      if (i.recipe_id === tempId) i.recipe_id = r.id;
    });
    render();
    return;
  }
}
async function renameRecipe(id, name) {
  const r = recipeById(recipes, id);
  name = String(name).trim();
  if (!r || !name || name === r.name) return;
  if (recipeNameTaken(recipes, name, id)) return;
  r.name = name;
  render();
  const sync = () => syncPatchRecipe(r, () => ({ name: r.name }));
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function syncPatchRecipe(r, fields) {
  if (r._deleted || r.id.startsWith("local-")) return;
  const patched = await api(
    "PATCH",
    "/api/recipes/" + encodeURIComponent(r.id),
    fields()
  ).catch(() => null);
  if (patched && patched.recipe && !replayingOps) {
    adoptRecipePatch(patched);
    render();
  }
}
async function toggleRecipe(id, enabled) {
  const r = recipeById(recipes, id);
  if (!r) return;
  r.enabled = enabled;
  items = applyRecipeToggle(items, id, enabled);
  render();
  const sync = () => syncPatchRecipe(r, () => ({ enabled: r.enabled }));
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function deleteRecipe(id) {
  const r = recipeById(recipes, id);
  if (!r) return;
  recipes = recipes.filter((x) => x.id !== id);
  items.forEach((i) => {
    if (i.recipe_id === id) i._deleted = true;
  });
  items = items.filter((i) => i.recipe_id !== id);
  delete recipeDrafts[id];
  delete collapsedRecipes[id];
  render();
  r._deleted = true;
  if (r.id.startsWith("local-")) return;
  const sync = async () => {
    const res = await api("DELETE", "/api/recipes/" + encodeURIComponent(r.id)).catch(() => null);
    if (res && Array.isArray(res.items)) {
      mergeServerItems(res.items);
      render();
    }
  };
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function addIngredient(recipeId, name) {
  const r = recipeById(recipes, recipeId);
  if (!r || !name) return;
  const it = {
    id: newLocalId(),
    name,
    group: NO_GROUP,
    state: r.enabled ? "needed" : "not_needed",
    completed: false,
    order: items.filter((i) => i.group === NO_GROUP).length,
    created_at: (/* @__PURE__ */ new Date()).toISOString(),
    recipe_id: recipeId
  };
  items.push(it);
  delete recipeDrafts[recipeId];
  const live = rc.querySelector(
    `.recipe-ingredient-input[data-recipe-id="${CSS.escape(recipeId)}"]`
  );
  if (live) live.value = "";
  focusRecipeId = recipeId;
  render();
  const sync = () => syncAddIngredient(it);
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function syncAddIngredient(it) {
  if (it._deleted) return;
  const saved = await api(
    "POST",
    "/api/recipes/" + encodeURIComponent(it.recipe_id) + "/ingredients",
    { name: it.name }
  ).catch(() => null);
  if (saved && saved.id) {
    Object.assign(it, replayingOps ? { id: saved.id, created_at: saved.created_at, recipe_id: saved.recipe_id } : saved);
    focusRecipeId = it.recipe_id;
    render();
  }
}
async function removeIngredient(recipeId, itemId) {
  const it = items.find((i) => i.id === itemId);
  if (!it) return;
  items = items.filter((i) => i.id !== itemId);
  render();
  it._deleted = true;
  if (it.id.startsWith("local-")) return;
  const sync = async () => {
    const ok = await api(
      "DELETE",
      "/api/recipes/" + encodeURIComponent(it.recipe_id) + "/ingredients/" + encodeURIComponent(it.id)
    ).then(() => true).catch(() => false);
    if (!ok) {
      await fetchItemsData();
      render();
    }
  };
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
async function moveRecipe(id, delta) {
  const ordered = recipesForRender(recipes);
  const idx = ordered.findIndex((r) => r.id === id);
  if (idx === -1) return;
  const target = idx + delta;
  if (target < 0 || target >= ordered.length) return;
  ordered[idx] = ordered.splice(target, 1, ordered[idx])[0];
  ordered.forEach((r, i) => {
    r.order = i;
  });
  recipes = ordered;
  render();
  const sync = async () => {
    const ids = recipesForRender(recipes).map((r) => r.id).filter((x) => !x.startsWith("local-"));
    const saved = await api("POST", "/api/recipes/reorder", { ids }).catch(() => null);
    if (Array.isArray(saved)) {
      mergeServerRecipes(saved);
      render();
    }
  };
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
function setActiveTab(tab) {
  activeTab = tab === "recipes" ? "recipes" : "grocery";
  const onRecipes = activeTab === "recipes";
  try {
    localStorage.setItem(TAB_KEY, activeTab);
  } catch (_) {
  }
  if (tabGrocery) {
    tabGrocery.classList.toggle("active", !onRecipes);
    tabGrocery.setAttribute("aria-selected", String(!onRecipes));
  }
  if (tabRecipes) {
    tabRecipes.classList.toggle("active", onRecipes);
    tabRecipes.setAttribute("aria-selected", String(onRecipes));
  }
  setHidden(hideNotNeededBtn, !tabControlVisible("#hide-not-needed-btn", activeTab));
  setHidden(resetBtn, !tabControlVisible("#reset-btn", activeTab));
  setHidden(groupsBtn, !tabControlVisible("#groups-btn", activeTab));
  setHidden(groupSel, onRecipes);
  newInput.placeholder = onRecipes ? "Add a recipe\u2026" : "Add an item\u2026";
  updateRecipeControlsDisabled();
  render();
}
if (tabGrocery) tabGrocery.addEventListener("click", () => setActiveTab("grocery"));
if (tabRecipes) tabRecipes.addEventListener("click", () => setActiveTab("recipes"));
function revealRecipe(recipeId) {
  delete collapsedRecipes[recipeId];
  setActiveTab("recipes");
  const card = rc.querySelector(
    `.recipe-card[data-recipe-id="${CSS.escape(recipeId)}"]`
  );
  if (card) card.scrollIntoView({ block: "nearest" });
}
function setAllCollapsed(collapsed) {
  if (activeTab === "recipes") {
    recipes.forEach((r) => {
      collapsedRecipes[r.id] = collapsed;
    });
  } else {
    groupsForRender().forEach((g) => {
      collapsedGroups[g] = collapsed;
    });
  }
  render();
}
collapseAll.addEventListener("click", () => setAllCollapsed(true));
expandAll.addEventListener("click", () => setAllCollapsed(false));
var VISIBILITY_TITLES = [
  "Hide \u2018Not Needed\u2019 items",
  // clicked from show_all
  "Also hide completed items",
  // clicked from hide_not_needed
  "Show all items"
  // clicked from hide_completed
];
var VISIBILITY_ARIA = [
  "Show all items",
  "Hide Not Needed items and empty groups",
  "Hide Not Needed, completed items and empty groups"
];
function updateVisibilityBtn() {
  hideNotNeededBtn.classList.toggle("active", visibilityMode !== 0);
  hideNotNeededBtn.setAttribute("aria-label", VISIBILITY_ARIA[visibilityMode]);
  hideNotNeededBtn.title = VISIBILITY_TITLES[visibilityMode];
}
hideNotNeededBtn.addEventListener("click", () => {
  visibilityMode = nextVisibilityMode(visibilityMode);
  updateVisibilityBtn();
  render();
});
updateVisibilityBtn();
function openResetModal() {
  resetModal.classList.remove("hidden");
  resetConfirm.focus();
}
function closeResetModal() {
  resetModal.classList.add("hidden");
}
async function doReset() {
  closeResetModal();
  items.forEach((item) => {
    item.completed = false;
    item.state = "check";
  });
  recipes.forEach((r) => {
    r.enabled = false;
  });
  render();
  const sync = async () => {
    const data = await api("POST", "/api/reset").catch(() => null);
    if (data) {
      mergeServerItems(data);
      render();
    }
  };
  if (syncEnabled) await sync();
  else pendingOps.push(sync);
}
resetBtn.addEventListener("click", openResetModal);
resetCancel.addEventListener("click", closeResetModal);
resetConfirm.addEventListener("click", doReset);
resetModal.addEventListener("click", (e) => {
  if (e.target === resetModal) closeResetModal();
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    closeResetModal();
    closeGroupsModal();
    closeRecipeModal();
  }
});
function renderGroupsList() {
  groupsList.innerHTML = "";
  if (groups.length === 0) {
    const li = document.createElement("li");
    li.className = "groups-modal-empty";
    li.textContent = "No groups yet";
    groupsList.appendChild(li);
    return;
  }
  groups.forEach((g) => {
    const li = document.createElement("li");
    li.className = "groups-modal-item";
    li.dataset.group = g;
    const handle = document.createElement("span");
    handle.className = "groups-modal-handle";
    handle.title = "Drag to reorder";
    handle.setAttribute("aria-label", "Drag to reorder group");
    handle.innerHTML = `<svg viewBox="0 0 14 14" fill="currentColor" width="14" height="14">
      <circle cx="4" cy="3"  r="1.2"/><circle cx="10" cy="3"  r="1.2"/>
      <circle cx="4" cy="7"  r="1.2"/><circle cx="10" cy="7"  r="1.2"/>
      <circle cx="4" cy="11" r="1.2"/><circle cx="10" cy="11" r="1.2"/>
    </svg>`;
    const nameEl = document.createElement("span");
    nameEl.className = "groups-modal-name";
    nameEl.textContent = g;
    const del = document.createElement("button");
    del.className = "groups-modal-delete";
    del.title = `Remove group \u201C${g}\u201D`;
    del.setAttribute("aria-label", `Remove group ${g}`);
    del.innerHTML = `<svg viewBox="0 0 16 16" fill="none" stroke="currentColor"
      stroke-width="2" stroke-linecap="round">
      <path d="M3 3l10 10M13 3L3 13"/></svg>`;
    del.addEventListener("click", () => removeGroup(g));
    li.appendChild(handle);
    li.appendChild(nameEl);
    li.appendChild(del);
    groupsList.appendChild(li);
    attachGroupDrag(li, handle);
  });
}
var gdrag = { active: false, srcGroup: null };
function attachGroupDrag(row, handle) {
  function clearGIndicators() {
    groupsList.querySelectorAll(".gdrag-above,.gdrag-below").forEach((el) => el.classList.remove("gdrag-above", "gdrag-below"));
  }
  function gPointerStart() {
    gdrag.active = true;
    gdrag.srcGroup = row.dataset.group;
    row.classList.add("gdragging");
  }
  function gPointerMove(clientY) {
    if (!gdrag.active) return;
    clearGIndicators();
    const els = [...groupsList.querySelectorAll(".groups-modal-item")];
    for (const el of els) {
      if (el.dataset.group === gdrag.srcGroup) continue;
      const rect = el.getBoundingClientRect();
      if (clientY < rect.top + rect.height / 2) {
        el.classList.add("gdrag-above");
        break;
      } else {
        el.classList.add("gdrag-below");
      }
    }
  }
  function gPointerEnd(clientY) {
    if (!gdrag.active) return;
    gdrag.active = false;
    groupsList.querySelectorAll(".groups-modal-item.gdragging").forEach((el) => el.classList.remove("gdragging"));
    const els = [...groupsList.querySelectorAll(".groups-modal-item")];
    const names = els.map((el) => el.dataset.group);
    const fromIdx = names.indexOf(gdrag.srcGroup);
    let toIdx = names.length;
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
  handle.addEventListener("mousedown", (e) => {
    e.preventDefault();
    gPointerStart();
    const onMove = (e2) => gPointerMove(e2.clientY);
    const onUp = (e2) => {
      gPointerEnd(e2.clientY);
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  });
  handle.addEventListener("touchstart", (e) => {
    gPointerStart();
    const onMove = (e2) => {
      gPointerMove(e2.touches[0].clientY);
      e2.preventDefault();
    };
    const onEnd = (e2) => {
      gPointerEnd(e2.changedTouches[0].clientY);
      handle.removeEventListener("touchmove", onMove);
      handle.removeEventListener("touchend", onEnd);
    };
    handle.addEventListener("touchmove", onMove, { passive: false });
    handle.addEventListener("touchend", onEnd);
  }, { passive: true });
}
function openGroupsModal() {
  renderGroupsList();
  groupsModal.classList.remove("hidden");
  groupsInput.value = "";
  groupsInput.focus();
}
function closeGroupsModal() {
  groupsModal.classList.add("hidden");
}
async function addGroup(name) {
  if (!name || name === NO_GROUP || groups.includes(name)) return;
  groups.push(name);
  rebuildGroupSelect();
  renderGroupsList();
  render();
  if (syncEnabled) {
    const data = await api("POST", "/api/config/groups", { name }).catch(() => null);
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
    const data = await api("POST", "/api/config/groups/reorder", { groups: newOrder }).catch(() => null);
    if (data?.groups) {
      groups = data.groups;
      rebuildGroupSelect();
      renderGroupsList();
      render();
    }
  }
}
async function removeGroup(name) {
  items.forEach((item) => {
    if (item.group === name) item.group = NO_GROUP;
  });
  groups = groups.filter((g) => g !== name);
  rebuildGroupSelect();
  renderGroupsList();
  render();
  if (syncEnabled) {
    const data = await api("POST", "/api/config/groups/remove", { name }).catch(() => null);
    if (data) {
      if (data.groups) groups = data.groups;
      if (data.items) items = data.items;
      rebuildGroupSelect();
      renderGroupsList();
      render();
    }
  }
}
groupsBtn.addEventListener("click", openGroupsModal);
groupsClose.addEventListener("click", closeGroupsModal);
groupsModal.addEventListener("click", (e) => {
  if (e.target === groupsModal) closeGroupsModal();
});
groupsForm.addEventListener("submit", (e) => {
  e.preventDefault();
  const name = groupsInput.value.trim();
  if (!name) return;
  groupsInput.value = "";
  addGroup(name);
  groupsInput.focus();
});
var progressBar = document.getElementById("progress-bar");
var progNeeded = document.getElementById("prog-needed");
var progCheck = document.getElementById("prog-check");
var progNotNeeded = document.getElementById("prog-not-needed");
var progCompleted = document.getElementById("prog-completed");
function renderProgressBar() {
  if (!progressBarVisible(showProgress, items.length, activeTab)) {
    progressBar.classList.add("hidden");
    return;
  }
  progressBar.classList.remove("hidden");
  const total = items.length;
  const nNeeded = items.filter((i) => i.state === "needed" && !i.completed).length;
  const nCheck = items.filter((i) => i.state === "check" && !i.completed).length;
  const nNotNeeded = items.filter((i) => i.state === "not_needed" && !i.completed).length;
  const nCompleted = items.filter((i) => i.completed).length;
  function pct(n) {
    return (n / total * 100).toFixed(1) + "%";
  }
  function tip(label, n) {
    return `${label}: ${n} (${(n / total * 100).toFixed(0)}%)`;
  }
  progNeeded.style.width = pct(nNeeded);
  progCheck.style.width = pct(nCheck);
  progNotNeeded.style.width = pct(nNotNeeded);
  progCompleted.style.width = pct(nCompleted);
  progNeeded.title = tip("Needed", nNeeded);
  progCheck.title = tip("Check", nCheck);
  progNotNeeded.title = tip("Not Needed", nNotNeeded);
  progCompleted.title = tip("Completed", nCompleted);
  function labelText(pctVal) {
    return Math.round(pctVal) + "%";
  }
  progCompleted.dataset.pct = labelText(nCompleted / total * 100);
  progNeeded.dataset.pct = labelText(nNeeded / total * 100);
  progCheck.dataset.pct = labelText(nCheck / total * 100);
  progNotNeeded.dataset.pct = labelText(nNotNeeded / total * 100);
}
document.getElementById("progress-bar").addEventListener("click", (e) => {
  const seg = e.target.closest(".progress-segment");
  if (!seg) return;
  const isOpen = seg.classList.contains("seg-open");
  document.querySelectorAll(".progress-segment").forEach((s) => s.classList.remove("seg-open"));
  if (!isOpen) seg.classList.add("seg-open");
});
document.addEventListener("click", (e) => {
  if (!e.target.closest("#progress-bar")) {
    document.querySelectorAll(".progress-segment.seg-open").forEach((s) => s.classList.remove("seg-open"));
  }
});
function setHidden(el, hidden) {
  if (el) el.classList.toggle("hidden", hidden);
}
function render() {
  setHidden(gc, activeTab !== "grocery");
  setHidden(emptyEl, activeTab !== "grocery" || items.length > 0);
  setHidden(rc, activeTab !== "recipes");
  setHidden(rEmptyEl, activeTab !== "recipes" || recipes.length > 0);
  renderProgressBar();
  if (activeTab === "recipes") renderRecipesTab();
  else renderGroceryTab();
}
function renderRecipesTab() {
  const active = document.activeElement;
  if (active && active.classList && active.classList.contains("recipe-ingredient-input")) {
    focusRecipeId = active.dataset.recipeId;
    recipeDrafts[focusRecipeId] = { value: active.value, caret: active.selectionStart };
  }
  rc.innerHTML = "";
  const ordered = recipesForRender(recipes);
  ordered.forEach((r, idx) => {
    const ing = ingredientsForRecipe(items, r.id);
    const isOpen = !collapsedRecipes[r.id];
    const rid = esc(r.id);
    const rname = esc(r.name);
    const first = idx === 0;
    const last = idx === ordered.length - 1;
    const card = document.createElement("div");
    card.dataset.recipeId = r.id;
    card.className = "recipe-card" + (r.enabled ? " recipe-card--enabled" : "");
    const header = document.createElement("div");
    header.className = "recipe-header" + (isOpen ? " open" : "");
    header.innerHTML = `
      <label class="recipe-switch" title="${r.enabled ? "Disable" : "Enable"} ${rname}">
        <input type="checkbox" class="recipe-switch-input"
               data-recipe-id="${rid}"${r.enabled ? " checked" : ""}
               aria-label="Enable ${rname}">
        <span class="recipe-switch-track"><span class="recipe-switch-thumb"></span></span>
      </label>
      <span class="recipe-title" data-recipe-id="${rid}" title="Rename">${rname}</span>
      <span class="recipe-meta">
        <button type="button" class="recipe-move-btn" data-recipe-id="${rid}"
                data-move="-1"${first ? ' data-edge="1" disabled' : ""}
                aria-label="Move ${rname} up">&#9650;</button>
        <button type="button" class="recipe-move-btn" data-recipe-id="${rid}"
                data-move="1"${last ? ' data-edge="1" disabled' : ""}
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
    const body = document.createElement("div");
    body.className = "recipe-body" + (isOpen ? "" : " collapsed");
    if (ing.length === 0) {
      const hint = document.createElement("div");
      hint.className = "recipe-ingredient-empty";
      hint.textContent = "No ingredients yet";
      body.appendChild(hint);
    } else {
      ing.forEach((item) => {
        const row = document.createElement("div");
        row.className = "recipe-ingredient-row";
        row.innerHTML = `
          <span class="recipe-ingredient-name">${esc(item.name)}</span>
          <button type="button" class="recipe-ingredient-delete"
                  data-recipe-id="${rid}" data-item-id="${esc(item.id)}"
                  aria-label="Remove ${esc(item.name)} from ${rname}">&#10005;</button>`;
        body.appendChild(row);
      });
    }
    const addRow = document.createElement("div");
    addRow.className = "recipe-ingredient-add";
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
  rc.querySelectorAll(".recipe-ingredient-input").forEach((input) => {
    const draft = recipeDrafts[input.dataset.recipeId];
    input.value = draft && draft.value || "";
  });
  if (focusRecipeId) {
    const input = rc.querySelector(
      `.recipe-ingredient-input[data-recipe-id="${CSS.escape(focusRecipeId)}"]`
    );
    if (input) {
      input.focus();
      const draft = recipeDrafts[focusRecipeId];
      if (draft) input.setSelectionRange(draft.caret, draft.caret);
    }
  }
  focusRecipeId = null;
  updateRecipeControlsDisabled();
}
function updateRecipeControlsDisabled() {
  rc.querySelectorAll(".recipe-switch-input, .recipe-ingredient-input, .recipe-ingredient-delete").forEach((el) => {
    el.disabled = false;
  });
  rc.querySelectorAll(".recipe-move-btn").forEach((el) => {
    el.disabled = el.dataset.edge === "1";
  });
  const submit = addForm.querySelector('button[type="submit"]');
  if (submit) submit.disabled = false;
}
function renderGroceryTab() {
  gc.innerHTML = "";
  emptyEl.classList.toggle("hidden", items.length > 0);
  const renderList = groupsForRender();
  renderList.forEach((group) => {
    if (!groupIsVisible(items, group, visibilityMode)) return;
    const allGroupItems = itemsForGroup(group);
    const groupItems = allGroupItems.filter((i) => visibilityFilter(i, visibilityMode));
    const isVirtual = group === NO_GROUP;
    const isOpen = !collapsedGroups[group];
    const section = document.createElement("div");
    section.className = "group-section" + (isVirtual ? " group-section--nogroup" : "");
    section.dataset.group = group;
    const header = document.createElement("div");
    header.className = "group-header" + (isOpen ? " open" : "") + (isVirtual ? " group-header--nogroup" : "");
    header.innerHTML = `
      <span class="group-title">${esc(groupLabel(group))}</span>
      <span class="group-meta">
        <span class="group-count">${visibilityMode !== 0 ? groupItems.length + "/" + allGroupItems.length : groupItems.length}</span>
        <svg class="group-chevron" viewBox="0 0 16 16" fill="none"
             stroke="currentColor" stroke-width="2"
             stroke-linecap="round" stroke-linejoin="round">
          <path d="M4 6l4 4 4-4"/>
        </svg>
      </span>`;
    header.addEventListener("click", () => {
      collapsedGroups[group] = !collapsedGroups[group];
      render();
    });
    const body = document.createElement("div");
    body.className = "group-body" + (isOpen ? "" : " collapsed");
    body.dataset.group = group;
    if (groupItems.length === 0) {
      const hint = document.createElement("div");
      hint.className = "group-empty";
      hint.textContent = groupEmptyHint(group);
      body.appendChild(hint);
    } else {
      const ul = document.createElement("ul");
      ul.className = "item-list";
      ul.dataset.group = group;
      groupItems.forEach((item) => ul.appendChild(buildRow(item)));
      body.appendChild(ul);
    }
    section.appendChild(header);
    section.appendChild(body);
    gc.appendChild(section);
  });
}
function buildRow(item) {
  const li = document.createElement("li");
  li.className = "item-row" + (item.completed ? " completed" : "");
  li.dataset.id = item.id;
  li.dataset.group = item.group;
  const suffix = recipeSuffix(item, recipes);
  const owned = isOwned(item);
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
           ${item.completed ? "checked" : ""}
           aria-label="Mark ${esc(item.name)} complete">
    <span class="item-content">
      <span class="item-name" data-id="${item.id}">${esc(item.name)}${// One element, not two: this button IS the suffix, so the row reads
  // "beef (Chili)" once and the whole of it is the reveal target.
  // The separator space is emitted HERE, outside the tag: a button is
  // inline-block, and leading collapsible white space at the start of an
  // inline formatting context is dropped — inside, it would render
  // "beef(Chili)". recipeSuffix keeps returning the leading space
  // because displayName concatenates on it.
  suffix ? ` <button class="recipe-chip item-recipe-suffix" data-recipe-id="${esc(item.recipe_id)}">${esc(suffix.trimStart())}</button>` : ""}</span>
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
      ${owned ? "" : `<button class="delete-btn" data-id="${item.id}" aria-label="Delete ${esc(item.name)}">
        <svg viewBox="0 0 20 20" fill="none" stroke="currentColor"
             stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
          <path d="M3 6h14M8 6V4h4v2M5 6l1 11h8l1-11"/>
        </svg>
      </button>`}
    </span>`;
  attachDragToHandle(li, li.querySelector(".drag-handle"));
  return li;
}
function attachDragToHandle(row, handle) {
  function clearIndicators() {
    gc.querySelectorAll(".drag-over-above,.drag-over-below").forEach((el) => el.classList.remove("drag-over-above", "drag-over-below"));
    gc.querySelectorAll(".drag-target").forEach((el) => el.classList.remove("drag-target"));
  }
  function pointerStart() {
    drag.active = true;
    drag.id = row.dataset.id;
    drag.srcGroup = row.dataset.group;
    row.classList.add("dragging");
  }
  function pointerMove(clientX, clientY) {
    if (!drag.active) return;
    clearIndicators();
    const el = document.elementFromPoint(clientX, clientY);
    if (!el) return;
    const targetSection = el.closest(".group-section");
    const targetGroup = targetSection?.dataset?.group;
    if (targetGroup && targetGroup !== drag.srcGroup) {
      targetSection.classList.add("drag-target");
      if (collapsedGroups[targetGroup]) {
        collapsedGroups[targetGroup] = false;
        render();
        const reRendered = gc.querySelector(`[data-id="${drag.id}"]`);
        if (reRendered) reRendered.classList.add("dragging");
        const sec = gc.querySelector(
          `.group-section[data-group="${CSS.escape(targetGroup)}"]`
        );
        if (sec) sec.classList.add("drag-target");
      }
      return;
    }
    const targetRow = el.closest(".item-row");
    if (targetRow && targetRow.dataset.id !== drag.id) {
      const rect = targetRow.getBoundingClientRect();
      targetRow.classList.add(
        clientY < rect.top + rect.height / 2 ? "drag-over-above" : "drag-over-below"
      );
    }
  }
  function pointerEnd(clientX, clientY) {
    if (!drag.active) return;
    drag.active = false;
    const el = document.elementFromPoint(clientX, clientY);
    clearIndicators();
    gc.querySelectorAll(".item-row.dragging").forEach((r) => r.classList.remove("dragging"));
    if (!el) {
      drag.id = null;
      return;
    }
    const targetSection = el.closest(".group-section");
    const targetGroup = targetSection?.dataset?.group;
    const targetRow = el.closest(".item-row");
    if (targetGroup && targetGroup !== drag.srcGroup) {
      const destIds = itemsForGroup(targetGroup).map((i) => i.id);
      destIds.push(drag.id);
      moveItem(drag.id, targetGroup, destIds);
    } else if (targetRow && targetRow.dataset.id !== drag.id && targetRow.dataset.group === drag.srcGroup) {
      const ids = itemsForGroup(drag.srcGroup).map((i) => i.id);
      const fromIdx = ids.indexOf(drag.id);
      const rect = targetRow.getBoundingClientRect();
      let toIdx = ids.indexOf(targetRow.dataset.id);
      if (clientY >= rect.top + rect.height / 2) toIdx++;
      ids.splice(fromIdx, 1);
      ids.splice(Math.max(0, fromIdx < toIdx ? toIdx - 1 : toIdx), 0, drag.id);
      reorderWithinGroup(drag.srcGroup, ids);
    } else {
      render();
    }
    drag.id = null;
    drag.srcGroup = null;
  }
  handle.addEventListener("mousedown", (e) => {
    e.preventDefault();
    pointerStart();
    const onMove = (e2) => pointerMove(e2.clientX, e2.clientY);
    const onUp = (e2) => {
      pointerEnd(e2.clientX, e2.clientY);
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
  });
  handle.addEventListener("touchstart", (e) => {
    pointerStart();
    const onMove = (e2) => {
      const t = e2.touches[0];
      pointerMove(t.clientX, t.clientY);
      e2.preventDefault();
    };
    const onEnd = (e2) => {
      const t = e2.changedTouches[0];
      pointerEnd(t.clientX, t.clientY);
      handle.removeEventListener("touchmove", onMove);
      handle.removeEventListener("touchend", onEnd);
    };
    handle.addEventListener("touchmove", onMove, { passive: false });
    handle.addEventListener("touchend", onEnd);
  }, { passive: true });
}
var moveModal = document.createElement("div");
moveModal.className = "modal-overlay hidden";
moveModal.setAttribute("role", "dialog");
moveModal.setAttribute("aria-modal", "true");
document.body.appendChild(moveModal);
var moveModalItemId = null;
function closeMoveModal() {
  moveModal.classList.add("hidden");
  moveModal.innerHTML = "";
  moveModalItemId = null;
}
function openMoveModal(itemId) {
  const item = items.find((i) => i.id === itemId);
  if (!item) return;
  moveModalItemId = itemId;
  const dest = [...groups, NO_GROUP].filter((g) => g !== item.group);
  const rows = dest.map((g) => `
    <li><button type="button" class="move-modal-item" data-group="${esc(g)}">
      ${esc(groupLabel(g))}
    </button></li>`).join("");
  moveModal.innerHTML = `
    <div class="modal">
      <h2 class="modal-title">Move &ldquo;${esc(item.name)}&rdquo;</h2>
      <ul class="move-modal-list">${rows || '<li class="move-modal-empty">No other groups yet</li>'}</ul>
      <div class="modal-actions modal-actions--right">
        <button type="button" class="btn btn-ghost" id="move-modal-cancel">Cancel</button>
      </div>
    </div>`;
  moveModal.classList.remove("hidden");
}
moveModal.addEventListener("click", (e) => {
  if (e.target === moveModal || e.target.closest("#move-modal-cancel")) {
    closeMoveModal();
    return;
  }
  const btn = e.target.closest(".move-modal-item");
  if (!btn) return;
  const id = moveModalItemId;
  const targetGroup = btn.dataset.group;
  closeMoveModal();
  const destIds = itemsForGroup(targetGroup).map((i) => i.id);
  destIds.push(id);
  moveItem(id, targetGroup, destIds);
});
gc.addEventListener("click", (e) => {
  if (drag.id) return;
  const cb = e.target.closest(".item-checkbox");
  const chip = e.target.closest(".recipe-chip");
  const name = e.target.closest(".item-name");
  const badge = e.target.closest(".state-badge");
  const move = e.target.closest(".move-btn");
  const del = e.target.closest(".delete-btn");
  if (cb) {
    toggleComplete(cb.dataset.id);
    return;
  }
  if (chip) {
    revealRecipe(chip.dataset.recipeId);
    return;
  }
  if (name) {
    cycleState(name.dataset.id);
    return;
  }
  if (badge) {
    cycleState(badge.dataset.id);
    return;
  }
  if (move) {
    openMoveModal(move.dataset.id);
    return;
  }
  if (del) {
    deleteItem(del.dataset.id);
    return;
  }
});
var recipeModal = document.createElement("div");
recipeModal.className = "modal-overlay hidden";
recipeModal.setAttribute("role", "dialog");
recipeModal.setAttribute("aria-modal", "true");
document.body.appendChild(recipeModal);
var recipeModalConfirm = null;
function closeRecipeModal() {
  recipeModal.classList.add("hidden");
  recipeModalConfirm = null;
}
function openRecipeModal(html, onConfirm) {
  recipeModal.innerHTML = html;
  recipeModalConfirm = onConfirm;
  recipeModal.classList.remove("hidden");
  const input = recipeModal.querySelector("#recipe-modal-input");
  if (input) {
    input.focus();
    input.select();
    return;
  }
  const ok = recipeModal.querySelector("#recipe-modal-confirm");
  if (ok) ok.focus();
}
function commitRecipeModal() {
  const fn = recipeModalConfirm;
  const input = recipeModal.querySelector("#recipe-modal-input");
  const value = input ? input.value.trim() : "";
  closeRecipeModal();
  if (fn) fn(value);
}
recipeModal.addEventListener("click", (e) => {
  if (e.target === recipeModal || e.target.closest("#recipe-modal-cancel")) {
    closeRecipeModal();
    return;
  }
  if (e.target.closest("#recipe-modal-confirm")) commitRecipeModal();
});
recipeModal.addEventListener("keydown", (e) => {
  if (e.key !== "Enter") return;
  if (!recipeModal.querySelector("#recipe-modal-input")) return;
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
        ingredient${count === 1 ? "" : "s"} from the grocery list &mdash;
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
    </div>`, (name) => renameRecipe(r.id, name));
}
rc.addEventListener("click", (e) => {
  const sw = e.target.closest(".recipe-switch-input");
  if (sw) {
    toggleRecipe(sw.dataset.recipeId, sw.checked);
    return;
  }
  if (e.target.closest(".recipe-switch")) return;
  const del = e.target.closest(".recipe-delete-btn");
  if (del) {
    const r = recipeById(recipes, del.dataset.recipeId);
    if (r) openDeleteRecipeModal(r);
    return;
  }
  const addBtn = e.target.closest(".recipe-ingredient-add-btn");
  if (addBtn) {
    const input = rc.querySelector(
      `.recipe-ingredient-input[data-recipe-id="${CSS.escape(addBtn.dataset.recipeId)}"]`
    );
    const name = input ? input.value.trim() : "";
    if (name) addIngredient(addBtn.dataset.recipeId, name);
    return;
  }
  const mv = e.target.closest(".recipe-move-btn");
  if (mv) {
    moveRecipe(mv.dataset.recipeId, Number(mv.dataset.move));
    return;
  }
  const title = e.target.closest(".recipe-title");
  if (title) {
    const r = recipeById(recipes, title.dataset.recipeId);
    if (r) openRenameRecipeModal(r);
    return;
  }
  const ingDel = e.target.closest(".recipe-ingredient-delete");
  if (ingDel) {
    removeIngredient(ingDel.dataset.recipeId, ingDel.dataset.itemId);
    return;
  }
  const header = e.target.closest(".recipe-header");
  if (header) {
    const card = header.closest(".recipe-card");
    if (!card) return;
    const id = card.dataset.recipeId;
    if (collapsedRecipes[id]) delete collapsedRecipes[id];
    else collapsedRecipes[id] = true;
    render();
  }
});
rc.addEventListener("input", (e) => {
  const el = e.target.closest(".recipe-ingredient-input");
  if (!el) return;
  recipeDrafts[el.dataset.recipeId] = { value: el.value, caret: el.selectionStart };
});
rc.addEventListener("keydown", (e) => {
  if (e.key !== "Enter") return;
  const el = e.target.closest(".recipe-ingredient-input");
  if (!el) return;
  e.preventDefault();
  const name = el.value.trim();
  if (name) addIngredient(el.dataset.recipeId, name);
});
addForm.addEventListener("submit", (e) => {
  e.preventDefault();
  const name = newInput.value.trim();
  if (!name) return;
  if (activeTab === "recipes") {
    if (recipeNameTaken(recipes, name)) {
      newInput.select();
      return;
    }
    newInput.value = "";
    createRecipe(name);
  } else {
    newInput.value = "";
    addItem(name, groupSel.value || groups[0] || NO_GROUP);
  }
  newInput.focus();
});
syncTog.addEventListener("change", async () => {
  syncEnabled = syncTog.checked;
  if (syncEnabled) {
    banner.classList.add("hidden");
    await replayPendingOps();
    await refreshAll();
    connectSSE();
  } else {
    disconnectSSE();
    banner.textContent = "\u26A0 Offline mode \u2014 changes will sync when you reconnect";
    banner.classList.remove("hidden");
  }
  updateRecipeControlsDisabled();
});
var evtSource = null;
function connectSSE() {
  if (!syncEnabled) return;
  if (evtSource) {
    evtSource.close();
    evtSource = null;
  }
  evtSource = new EventSource("/api/events");
  evtSource.addEventListener("message", () => {
    refreshAll();
  });
  evtSource.addEventListener("open", () => {
    banner.classList.add("hidden");
  });
  evtSource.onerror = () => {
    banner.textContent = "\u26A0 Connection lost \u2014 reconnecting\u2026";
    banner.classList.remove("hidden");
  };
}
function disconnectSSE() {
  if (evtSource) {
    evtSource.close();
    evtSource = null;
  }
}
(async () => {
  activeTab = localStorage.getItem(TAB_KEY) === "recipes" ? "recipes" : "grocery";
  await loadConfig();
  await fetchRecipesData();
  await fetchItems();
  setActiveTab(activeTab);
  connectSSE();
})();
