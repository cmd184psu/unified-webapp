// web/obsidianoid/js/app.ts
import { HamburgerMenu, ThemeManager, showToast } from "/shared/dist/shared.mjs";
var state = {
  currentPath: null,
  isPreviewMode: false,
  isDirty: false,
  treeData: null,
  filterText: "",
  mode: "notes",
  activeVault: 0,
  vaults: [],
  autoSave: true
};
var fileTree = document.getElementById("file-tree");
var btnGitSync = document.getElementById("btn-git-sync");
var gitSyncDialog = document.getElementById("git-sync-dialog");
var gitSyncForm = document.getElementById("git-sync-form");
var gitCommitMsg = document.getElementById("git-commit-msg");
var btnCancelSync = document.getElementById("btn-cancel-sync");
var editorPane = document.getElementById("editor-pane");
var previewPane = document.getElementById("preview-pane");
var emptyState = document.getElementById("empty-state");
var btnToggle = document.getElementById("btn-toggle-mode");
var modeLabel = document.getElementById("mode-label");
var btnSave = document.getElementById("btn-save");
var btnNewNote = document.getElementById("btn-new-note");
var noteTitle = document.getElementById("note-title");
var searchInput = document.getElementById("search-input");
var sidebar = document.getElementById("sidebar");
var resizeHandle = document.getElementById("resize-handle");
var newNoteDialog = document.getElementById("new-note-dialog");
var newNoteForm = document.getElementById("new-note-form");
var newNotePath = document.getElementById("new-note-path");
var btnCancelNew = document.getElementById("btn-cancel-new");
var vaultSelector = document.getElementById("vault-selector");
var btnHamburger = document.getElementById("btn-hamburger");
var btnAutoSave = document.getElementById("btn-autosave");
function vaultParam() {
  return `vault=${state.activeVault}`;
}
function fileIcon() {
  return `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>`;
}
function chevronIcon() {
  return `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="9 18 15 12 9 6"/></svg>`;
}
function folderIcon() {
  return `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>`;
}
function matchesFilter(node, filter) {
  if (!filter) return true;
  const f = filter.toLowerCase();
  if (!node.is_dir) return node.name.toLowerCase().includes(f);
  return (node.children || []).some((c) => matchesFilter(c, filter));
}
function renderNode(node, depth = 0) {
  if (!matchesFilter(node, state.filterText)) return null;
  if (node.is_dir) {
    const wrapper = document.createElement("div");
    const label = document.createElement("div");
    label.className = "tree-dir-label";
    label.style.paddingLeft = `calc(var(--space-3) + ${depth * 14}px)`;
    label.innerHTML = `${chevronIcon()}${folderIcon()}<span>${node.name}</span>`;
    label.setAttribute("role", "treeitem");
    label.setAttribute("aria-expanded", "true");
    const children = document.createElement("div");
    children.className = "tree-dir-children";
    label.addEventListener("click", () => {
      const collapsed = children.classList.toggle("collapsed");
      label.classList.toggle("collapsed", collapsed);
      label.setAttribute("aria-expanded", String(!collapsed));
    });
    wrapper.appendChild(label);
    (node.children || []).forEach((child) => {
      const el = renderNode(child, depth + 1);
      if (el) children.appendChild(el);
    });
    wrapper.appendChild(children);
    return wrapper;
  } else {
    const item = document.createElement("div");
    item.className = "tree-note" + (node.path === state.currentPath ? " active" : "");
    item.style.paddingLeft = `calc(var(--space-3) + ${depth * 14}px)`;
    item.innerHTML = `${fileIcon()}<span title="${node.path}">${node.name}</span>`;
    item.setAttribute("role", "treeitem");
    item.setAttribute("tabindex", "0");
    item.dataset.path = node.path;
    const open = () => loadNote(node.path);
    item.addEventListener("click", open);
    item.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        open();
      }
    });
    return item;
  }
}
function renderTree() {
  if (!state.treeData) return;
  fileTree.innerHTML = "";
  (state.treeData.children || []).forEach((child) => {
    const el = renderNode(child, 0);
    if (el) fileTree.appendChild(el);
  });
}
async function fetchTree() {
  try {
    const res = await fetch(`/api/tree?${vaultParam()}`);
    if (!res.ok) throw new Error("tree fetch failed");
    state.treeData = await res.json();
    renderTree();
  } catch (e) {
    fileTree.innerHTML = `<div style="padding:var(--space-3);font-size:var(--text-xs);color:var(--color-danger)">\u26A0 Failed to load vault</div>`;
  }
}
function syncActiveHighlight() {
  document.querySelectorAll(".tree-note").forEach((el) => {
    el.classList.toggle("active", el.dataset.path === state.currentPath);
  });
}
async function loadNote(path) {
  if (state.isDirty) {
    if (!confirm("You have unsaved changes. Discard and open new note?")) return;
  }
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(path)}`);
    if (!res.ok) {
      showToast("Failed to load note", "error");
      return;
    }
    const text = await res.text();
    state.currentPath = path;
    state.isDirty = false;
    state.isPreviewMode = true;
    editorPane.value = text;
    noteTitle.textContent = path;
    btnToggle.disabled = false;
    btnSave.disabled = true;
    await renderPreview(text);
    setEditorMode();
    syncActiveHighlight();
  } catch (e) {
    showToast("Network error loading note", "error");
  }
}
async function renderPreview(text) {
  try {
    const res = await fetch("/api/render", {
      method: "POST",
      headers: { "Content-Type": "text/plain" },
      body: text
    });
    const html = await res.text();
    previewPane.innerHTML = `<div class="md-body">${html}</div>`;
  } catch (e) {
    previewPane.innerHTML = `<div class="md-body"><p style="color:var(--color-danger)">Render failed</p></div>`;
  }
}
async function reloadCurrentNote() {
  if (!state.currentPath || state.isDirty) return;
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(state.currentPath)}`);
    if (!res.ok) return;
    const text = await res.text();
    editorPane.value = text;
    if (state.isPreviewMode) await renderPreview(text);
  } catch (e) {
  }
}
var eventSource = null;
function reconnectEvents() {
  if (eventSource) {
    eventSource.close();
    eventSource = null;
  }
  eventSource = new EventSource(`/api/events?${vaultParam()}`);
  eventSource.addEventListener("note-changed", (e) => {
    const data = JSON.parse(e.data);
    if (data.path === state.currentPath) reloadCurrentNote();
  });
}
function show(el) {
  if (el.id === "preview-pane") {
    el.style.display = "block";
  } else if (el.tagName === "TEXTAREA" || el.id === "empty-state") {
    el.style.display = "flex";
  } else {
    el.style.display = "block";
  }
}
function hide(el) {
  el.style.display = "none";
}
function setEditorMode() {
  const hasNote = !!state.currentPath;
  const prev = state.isPreviewMode;
  if (!hasNote) {
    show(emptyState);
    hide(editorPane);
    hide(previewPane);
  } else if (prev) {
    hide(emptyState);
    hide(editorPane);
    show(previewPane);
  } else {
    hide(emptyState);
    show(editorPane);
    hide(previewPane);
  }
  modeLabel.textContent = prev ? "Edit" : "Preview";
  btnToggle.classList.toggle("preview-active", prev);
  btnToggle.title = prev ? "Switch to editor" : "Switch to preview";
}
async function toggleMode() {
  if (!state.currentPath) return;
  state.isPreviewMode = !state.isPreviewMode;
  if (state.isPreviewMode) await renderPreview(editorPane.value);
  setEditorMode();
}
async function saveNote() {
  if (!state.currentPath) return;
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(state.currentPath)}`, {
      method: "PUT",
      headers: { "Content-Type": "text/plain" },
      body: editorPane.value
    });
    if (!res.ok) {
      showToast("Save failed", "error");
      return;
    }
    state.isDirty = false;
    btnSave.disabled = true;
    showToast("\u2713 Saved", "success");
  } catch (e) {
    showToast("Network error saving note", "error");
  }
}
var autoSaveTimer;
function scheduleAutoSave() {
  clearTimeout(autoSaveTimer);
  if (state.autoSave && state.isDirty && state.currentPath) {
    autoSaveTimer = setTimeout(saveNote, 1e3);
  }
}
editorPane.addEventListener("input", () => {
  if (!state.isDirty) {
    state.isDirty = true;
    btnSave.disabled = false;
  }
  scheduleAutoSave();
});
document.addEventListener("keydown", (e) => {
  if ((e.ctrlKey || e.metaKey) && e.key === "s") {
    e.preventDefault();
    if (!btnSave.disabled) saveNote();
  }
  if ((e.ctrlKey || e.metaKey) && e.key === "e") {
    e.preventDefault();
    if (!btnToggle.disabled) toggleMode();
  }
});
btnToggle.addEventListener("click", toggleMode);
btnSave.addEventListener("click", saveNote);
btnAutoSave.addEventListener("click", () => {
  state.autoSave = !state.autoSave;
  btnAutoSave.classList.toggle("active", state.autoSave);
  state.autoSave ? scheduleAutoSave() : clearTimeout(autoSaveTimer);
});
btnNewNote.addEventListener("click", () => {
  newNotePath.value = "";
  newNoteDialog.showModal();
  newNotePath.focus();
});
btnCancelNew.addEventListener("click", () => newNoteDialog.close());
newNoteForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  let path = newNotePath.value.trim();
  if (!path) return;
  if (!path.endsWith(".md")) path += ".md";
  newNoteDialog.close();
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(path)}`, {
      method: "PUT",
      headers: { "Content-Type": "text/plain" },
      body: `# ${path.replace(/.*\//, "").replace(".md", "")}

`
    });
    if (!res.ok) {
      showToast("Failed to create note", "error");
      return;
    }
    await fetchTree();
    await loadNote(path);
    showToast("\u2713 Note created", "success");
  } catch (e2) {
    showToast("Network error", "error");
  }
});
var isResizing = false;
resizeHandle.addEventListener("mousedown", () => {
  isResizing = true;
  resizeHandle.classList.add("dragging");
  document.body.style.cursor = "col-resize";
  document.body.style.userSelect = "none";
});
document.addEventListener("mousemove", (e) => {
  if (!isResizing) return;
  const newWidth = Math.min(Math.max(e.clientX, 160), window.innerWidth * 0.5);
  sidebar.style.width = newWidth + "px";
  document.documentElement.style.setProperty("--sidebar-width", newWidth + "px");
});
document.addEventListener("mouseup", () => {
  if (!isResizing) return;
  isResizing = false;
  resizeHandle.classList.remove("dragging");
  document.body.style.cursor = "";
  document.body.style.userSelect = "";
});
searchInput.addEventListener("input", () => {
  state.filterText = searchInput.value.trim();
  renderTree();
});
async function checkGitAvailable() {
  try {
    const data = await (await fetch(`/api/git/status?${vaultParam()}`)).json();
    btnGitSync.hidden = !data.available;
  } catch (e) {
  }
}
btnGitSync.addEventListener("click", () => {
  const now = /* @__PURE__ */ new Date();
  const ts = now.toISOString().slice(0, 16).replace("T", " ");
  gitCommitMsg.value = `obsidianoid sync ${ts}`;
  gitSyncDialog.showModal();
  gitCommitMsg.select();
});
btnCancelSync.addEventListener("click", () => gitSyncDialog.close());
gitSyncForm.addEventListener("submit", async (e) => {
  e.preventDefault();
  const message = gitCommitMsg.value.trim() || "obsidianoid sync";
  gitSyncDialog.close();
  btnGitSync.disabled = true;
  try {
    const res = await fetch(`/api/git/sync?${vaultParam()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ message })
    });
    const data = await res.json();
    if (data.ok) showToast("\u2713 Synced", "success");
    else {
      showToast("Sync failed \u2014 see console", "error");
      console.error("git sync:", data.output);
    }
  } catch (e2) {
    showToast("Sync failed", "error");
  } finally {
    btnGitSync.disabled = false;
  }
});
function setMode(mode) {
  if (state.mode === "threads" && mode !== "threads") ThreadsView.flush();
  state.mode = mode;
  document.getElementById("app").dataset.mode = mode;
  document.getElementById("btn-mode-notes").classList.toggle("active", mode === "notes");
  document.getElementById("btn-mode-threads").classList.toggle("active", mode === "threads");
  if (mode === "threads") ThreadsView.activate();
}
document.getElementById("btn-mode-notes").addEventListener("click", () => setMode("notes"));
document.getElementById("btn-mode-threads").addEventListener("click", () => setMode("threads"));
var MIGRATED = "ui-theme-migrated:obsidianoid";
if (!localStorage.getItem(MIGRATED)) {
  for (const k of Object.keys(localStorage)) {
    if (k.startsWith("obsidianoid-theme-") && localStorage.getItem(k) === "dark") {
      localStorage.setItem(k, "obsidian");
    }
  }
  localStorage.setItem(MIGRATED, "1");
}
async function fetchConfig() {
  try {
    const d = await (await fetch("/api/config")).json();
    state.autoSave = d.autosave !== false;
    btnAutoSave.classList.toggle("active", state.autoSave);
  } catch (e) {
  }
}
async function fetchVaults() {
  try {
    const res = await fetch("/api/vaults");
    if (!res.ok) return;
    state.vaults = await res.json();
    vaultSelector.innerHTML = "";
    state.vaults.forEach((v, i) => {
      const opt = document.createElement("option");
      opt.value = String(i);
      opt.textContent = v.name;
      vaultSelector.appendChild(opt);
    });
    themes.reresolve();
  } catch (e) {
  }
}
function switchVault(idx) {
  clearTimeout(autoSaveTimer);
  state.activeVault = idx;
  state.currentPath = null;
  state.isDirty = false;
  noteTitle.textContent = "";
  btnToggle.disabled = true;
  btnSave.disabled = true;
  setEditorMode();
  themes.reresolve();
  reconnectEvents();
  checkGitAvailable();
  fetchTree();
}
vaultSelector.addEventListener("change", () => switchVault(parseInt(vaultSelector.value)));
var themes = new ThemeManager({
  module: "obsidianoid",
  default: "obsidian",
  storageKey: () => `obsidianoid-theme-${state.activeVault}`,
  serverDefault: () => state.vaults[state.activeVault]?.theme
});
new HamburgerMenu({
  title: "Settings",
  items: [],
  themePicker: true,
  themes,
  mountTrigger: btnHamburger
});
btnAutoSave.classList.add("active");
ThreadsView.init();
reconnectEvents();
checkGitAvailable();
Promise.all([fetchConfig(), fetchVaults()]).then(fetchTree);
