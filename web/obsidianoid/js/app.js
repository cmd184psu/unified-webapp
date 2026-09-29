// web/obsidianoid/js/app.ts
import { HamburgerMenu, ThemeManager, FileTree, showToast, confirmDialog, promptDialog } from "/shared/dist/shared.mjs";
var SORT_KEY = "obsidianoid-sort";
var LOCK_KEY = "obsidianoid-tree-locked";
var OVERWRITE_KEY = "obsidianoid-allow-overwrite";
var state = {
  currentPath: null,
  isPreviewMode: false,
  isDirty: false,
  treeData: null,
  filterText: "",
  /** Paths matching the current search (name or content); null when not searching. */
  searchPaths: null,
  sortMode: "name",
  /** Read-only tree: no moving, renaming or deleting (editing notes still works). */
  treeLocked: false,
  /** The configured thread folder; it can't be renamed (thread mode relies on it). */
  threadsFolder: "Threads",
  /** Replace a same-named note on move/rename, after an "are you sure?". */
  allowOverwrite: false,
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
var sortSelector = document.getElementById("sort-selector");
var btnNewFolder = document.getElementById("btn-new-folder");
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
function toEntries(nodes = []) {
  return nodes.map((n) => ({
    name: n.name,
    path: n.path ?? "",
    isDir: !!n.is_dir,
    mtime: n.mtime,
    children: n.is_dir ? toEntries(n.children) : void 0
  }));
}
function holdsThreads(folder) {
  return folder === state.threadsFolder || state.threadsFolder.startsWith(folder + "/");
}
function matchesFilter(entry) {
  if (state.searchPaths) return state.searchPaths.has(entry.path);
  return entry.name.toLowerCase().includes(state.filterText.toLowerCase());
}
var tree = new FileTree({
  label: "Notes",
  emptyText: "No notes yet.",
  onOpen: (entry) => void loadNote(entry.path),
  rename: (entry) => void (entry.isDir ? renameFolder(entry) : renameNote(entry)),
  canRename: (entry) => !entry.isDir || !holdsThreads(entry.path),
  // Only empty folders can be deleted.
  remove: (entry) => void (entry.isDir ? deleteFolder(entry) : deleteNote(entry)),
  canRemove: (entry) => !entry.isDir || (entry.children ?? []).length === 0,
  move: (entry, folder) => void moveNote(entry, folder)
});
fileTree.replaceChildren(tree.el);
function renderTree() {
  tree.update({
    sort: state.sortMode,
    filter: state.filterText ? matchesFilter : null,
    activePath: state.currentPath,
    locked: state.treeLocked
  });
}
async function fetchTree() {
  try {
    const res = await fetch(`/api/tree?${vaultParam()}`);
    if (!res.ok) throw new Error("tree fetch failed");
    state.treeData = await res.json();
    tree.update({ emptyText: "No notes yet." });
    tree.setEntries(toEntries(state.treeData.children));
    renderTree();
  } catch (e) {
    tree.update({ emptyText: "\u26A0 Failed to load vault" });
    tree.setEntries([]);
  }
}
function syncActiveHighlight() {
  tree.update({ activePath: state.currentPath });
}
async function loadNote(path) {
  if (state.isDirty) {
    if (!await confirmDialog("You have unsaved changes. Discard and open new note?")) return;
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
    if (state.sortMode === "recent") void fetchTree();
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
var searchTimer;
var searchSeq = 0;
async function runSearch() {
  const q = state.filterText;
  const seq = ++searchSeq;
  if (!q) {
    state.searchPaths = null;
    renderTree();
    return;
  }
  try {
    const res = await fetch(`/api/search?${vaultParam()}&q=${encodeURIComponent(q)}`);
    if (!res.ok) return;
    const { paths } = await res.json();
    if (seq !== searchSeq) return;
    state.searchPaths = new Set(paths);
    renderTree();
  } catch (e) {
  }
}
searchInput.addEventListener("input", () => {
  state.filterText = searchInput.value.trim();
  state.searchPaths = null;
  renderTree();
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => void runSearch(), 250);
});
try {
  const saved = localStorage.getItem(SORT_KEY);
  if (saved === "name" || saved === "recent") state.sortMode = saved;
} catch (e) {
}
sortSelector.value = state.sortMode;
sortSelector.addEventListener("change", () => {
  state.sortMode = sortSelector.value === "recent" ? "recent" : "name";
  try {
    localStorage.setItem(SORT_KEY, state.sortMode);
  } catch (e) {
  }
  renderTree();
});
function folderOf(path) {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}
function noteInTree(path, node = state.treeData) {
  if (!node) return false;
  if (!node.is_dir) return node.path === path;
  return (node.children || []).some((c) => noteInTree(path, c));
}
async function postWithOverwrite(url, body, targetPath) {
  const send = (overwrite) => fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ...body, overwrite })
  });
  if (noteInTree(targetPath)) {
    if (!state.allowOverwrite) return new Response(null, { status: 409 });
    return await confirmOverwrite(targetPath) ? send(true) : null;
  }
  const res = await send(false);
  if (res.status !== 409 || !state.allowOverwrite) return res;
  return await confirmOverwrite(targetPath) ? send(true) : null;
}
function confirmOverwrite(targetPath) {
  const dirtyTarget = targetPath === state.currentPath && state.isDirty;
  return confirmDialog(
    `"${targetPath}" already exists. Overwrite it? The existing note will be replaced` + (dirtyTarget ? ", including your unsaved changes to it" : "") + ".",
    { title: "Overwrite note", confirmLabel: "Overwrite" }
  );
}
function afterOverwrite(targetPath) {
  if (state.currentPath === targetPath) {
    state.isDirty = false;
    btnSave.disabled = true;
    void reloadCurrentNote();
  }
}
function treeUnlocked() {
  if (state.treeLocked) showToast("The file tree is locked. Unlock it in Settings to change it.", "notice");
  return !state.treeLocked;
}
async function moveNote(node, folder) {
  if (!treeUnlocked()) return;
  const path = node.path;
  if (path === state.currentPath && state.isDirty) {
    showToast("Save your changes before moving this note.", "notice");
    return;
  }
  const baseName = path.slice(path.lastIndexOf("/") + 1);
  const targetPath = folder ? `${folder}/${baseName}` : baseName;
  try {
    const res = await postWithOverwrite(`/api/note/move?${vaultParam()}`, { path, folder }, targetPath);
    if (!res) return;
    if (!res.ok) {
      showToast(res.status === 409 ? 'That folder already has a note with this name. (Turn on "Allow overwrites" in Settings to replace it.)' : "Move failed.", "error");
      return;
    }
    afterOverwrite(targetPath);
    const { path: newPath, thread_reset: threadReset } = await res.json();
    if (state.currentPath === path) {
      state.currentPath = newPath;
      noteTitle.textContent = newPath;
    }
    const where = folder || "the vault root";
    showToast(threadReset ? `Moved ${node.name} to ${where}. It's a regular note now; its thread slot starts over as an empty thread.` : `Moved ${node.name} to ${where}`, "success");
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast("Network error moving note", "error");
  }
}
btnNewFolder.addEventListener("click", async () => {
  const name = await promptDialog("Folder name (use / for nested folders, e.g. Projects/2026):", {
    title: "New folder",
    confirmLabel: "Create"
  });
  if (name === null || !name.trim()) return;
  try {
    const res = await fetch(`/api/folder?${vaultParam()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path: name.trim() })
    });
    if (!res.ok) {
      const msg = res.status === 409 ? "That folder already exists." : res.status === 400 ? "That folder name isn't allowed (names can't start with a dot)." : "Could not create the folder.";
      showToast(msg, "error");
      return;
    }
    showToast(`Created folder ${name.trim()}`, "success");
    await fetchTree();
  } catch (e) {
    showToast("Network error creating folder", "error");
  }
});
async function renameFolder(node) {
  if (!treeUnlocked()) return;
  const path = node.path ?? "";
  if (!path) return;
  const name = await promptDialog("New name for this folder:", {
    title: "Rename folder",
    defaultValue: node.name,
    confirmLabel: "Rename"
  });
  if (name === null || !name.trim() || name.trim() === node.name) return;
  try {
    const res = await fetch(`/api/folder/rename?${vaultParam()}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, name: name.trim() })
    });
    if (!res.ok) {
      const msg = res.status === 409 ? "Something here already has that name." : res.status === 403 ? "The thread folder can't be renamed; thread mode depends on it." : res.status === 400 ? "That name isn't allowed (no slashes, and it can't start with a dot)." : "Rename failed.";
      showToast(msg, "error");
      return;
    }
    const { path: newPath } = await res.json();
    if (state.currentPath && state.currentPath.startsWith(path + "/")) {
      state.currentPath = newPath + state.currentPath.slice(path.length);
      noteTitle.textContent = state.currentPath;
    }
    showToast(`Renamed folder to ${name.trim()}`, "success");
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast("Network error renaming folder", "error");
  }
}
async function deleteFolder(node) {
  if (!treeUnlocked()) return;
  const path = node.path ?? "";
  if (!path) return;
  const ok = await confirmDialog(`Delete the empty folder "${path}"?`, { title: "Delete folder", confirmLabel: "Delete" });
  if (!ok) return;
  try {
    const res = await fetch(`/api/folder?${vaultParam()}&path=${encodeURIComponent(path)}`, { method: "DELETE" });
    if (!res.ok) {
      showToast(res.status === 409 ? "That folder isn't empty (it may contain hidden files)." : "Could not delete the folder.", "error");
      await fetchTree();
      return;
    }
    showToast(`Deleted folder ${path}`, "success");
    await fetchTree();
  } catch (e) {
    showToast("Network error deleting folder", "error");
  }
}
async function deleteNote(node) {
  if (!treeUnlocked()) return;
  const path = node.path;
  const isOpen = path === state.currentPath;
  const ok = await confirmDialog(
    `Delete "${node.name}"? This removes ${path} from the vault` + (isOpen && state.isDirty ? ", including your unsaved changes" : "") + ".",
    { title: "Delete note", confirmLabel: "Delete" }
  );
  if (!ok) return;
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(path)}`, { method: "DELETE" });
    if (!res.ok) {
      showToast("Delete failed", "error");
      return;
    }
    const { thread_reset: threadReset } = await res.json();
    if (state.currentPath === path) {
      clearTimeout(autoSaveTimer);
      state.currentPath = null;
      state.isDirty = false;
      noteTitle.textContent = "";
      editorPane.value = "";
      btnToggle.disabled = true;
      btnSave.disabled = true;
      setEditorMode();
    }
    showToast(threadReset ? `Deleted ${node.name}. Its thread slot starts over as an empty thread.` : `Deleted ${node.name}`, "success");
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast("Network error deleting note", "error");
  }
}
async function renameNote(node) {
  if (!treeUnlocked()) return;
  const path = node.path;
  if (path === state.currentPath && state.isDirty) {
    showToast("Save your changes before renaming this note.", "notice");
    return;
  }
  const name = await promptDialog("New name for this note:", {
    title: "Rename note",
    defaultValue: node.name,
    confirmLabel: "Rename"
  });
  if (name === null || name.trim() === "" || name.trim() === node.name) return;
  const dir = folderOf(path);
  const newFile = name.trim().replace(/\.md$/, "") + ".md";
  const targetPath = dir ? `${dir}/${newFile}` : newFile;
  try {
    const res = await postWithOverwrite(`/api/note/rename?${vaultParam()}`, { path, name: name.trim() }, targetPath);
    if (!res) return;
    if (!res.ok) {
      const msg = res.status === 409 ? 'A note with that name already exists. (Turn on "Allow overwrites" in Settings to replace it.)' : res.status === 400 ? "That name isn't allowed (no slashes, and it can't start with a dot)." : "Rename failed.";
      showToast(msg, "error");
      return;
    }
    if (targetPath !== path) afterOverwrite(targetPath);
    const { path: newPath, thread_reset: threadReset } = await res.json();
    if (state.currentPath === path) {
      state.currentPath = newPath;
      noteTitle.textContent = newPath;
    }
    showToast(threadReset ? `Renamed to ${name.trim()}. It's a regular note now; its thread slot starts over as an empty thread.` : `Renamed to ${name.trim()}`, "success");
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast("Network error renaming note", "error");
  }
}
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
  if (state.mode === "threads" && mode !== "threads") {
    void ThreadsView.flush().catch(() => void 0).finally(() => {
      void fetchTree().then(() => {
        if (state.filterText) void runSearch();
      });
    });
  }
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
    if (typeof d.threads_folder === "string") state.threadsFolder = d.threads_folder;
    if (d.thread_count && d.max_threads) fillThreadCount(d.thread_count, d.max_threads);
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
  state.searchPaths = null;
  fetchTree().then(() => {
    if (state.filterText) void runSearch();
  });
}
vaultSelector.addEventListener("change", () => switchVault(parseInt(vaultSelector.value)));
var themes = new ThemeManager({
  module: "obsidianoid",
  default: "obsidian",
  storageKey: () => `obsidianoid-theme-${state.activeVault}`,
  serverDefault: () => state.vaults[state.activeVault]?.theme
});
try {
  state.treeLocked = localStorage.getItem(LOCK_KEY) === "1";
} catch (e) {
}
try {
  state.allowOverwrite = localStorage.getItem(OVERWRITE_KEY) === "1";
} catch (e) {
}
function settingToggle(text, hint, checked, onChange) {
  const label = document.createElement("label");
  label.className = "ui-toggle";
  label.title = hint;
  const input = document.createElement("input");
  input.type = "checkbox";
  input.checked = checked;
  const track = document.createElement("span");
  track.className = "ui-toggle-track";
  label.append(input, track, text);
  input.addEventListener("change", () => onChange(input.checked));
  return label;
}
var threadCountSelect = document.createElement("select");
threadCountSelect.className = "settings-select";
threadCountSelect.setAttribute("aria-label", "Number of threads");
function fillThreadCount(count, max) {
  threadCountSelect.innerHTML = "";
  for (let n = 1; n <= max; n++) {
    const opt = document.createElement("option");
    opt.value = String(n);
    opt.textContent = String(n);
    threadCountSelect.append(opt);
  }
  threadCountSelect.value = String(count);
}
threadCountSelect.addEventListener("change", async () => {
  const count = parseInt(threadCountSelect.value, 10);
  try {
    if (state.mode === "threads") await ThreadsView.flush();
    const res = await fetch("/api/threads/count", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ count })
    });
    if (!res.ok) {
      showToast("Could not change the number of threads", "error");
      return;
    }
    showToast(`Threads: ${count}`, "success");
    if (state.mode === "threads") void ThreadsView.activate();
  } catch (e) {
    showToast("Network error changing the number of threads", "error");
  }
});
new HamburgerMenu({
  title: "Settings",
  items: [
    { section: "File tree" },
    {
      id: "tree-lock",
      render: (host) => {
        host.append(settingToggle(
          "Lock file tree",
          "Prevent moving, renaming and deleting in the file tree. Notes can still be edited.",
          state.treeLocked,
          (on) => {
            state.treeLocked = on;
            try {
              localStorage.setItem(LOCK_KEY, on ? "1" : "0");
            } catch (e) {
            }
            document.getElementById("sidebar").classList.toggle("tree-locked", on);
            renderTree();
            showToast(on ? "File tree locked" : "File tree unlocked", "notice");
          }
        ));
      }
    },
    {
      id: "allow-overwrite",
      render: (host) => {
        host.append(settingToggle(
          "Allow overwrites",
          "When moving or renaming a note onto an existing note, ask and then replace it instead of refusing.",
          state.allowOverwrite,
          (on) => {
            state.allowOverwrite = on;
            try {
              localStorage.setItem(OVERWRITE_KEY, on ? "1" : "0");
            } catch (e) {
            }
          }
        ));
      }
    },
    { section: "Threads" },
    {
      id: "thread-count",
      render: (host) => {
        const row = document.createElement("label");
        row.className = "settings-row";
        row.append("Number of threads", threadCountSelect);
        host.append(row);
      }
    }
  ],
  themePicker: true,
  themes,
  side: "right",
  mountTrigger: btnHamburger
});
document.getElementById("sidebar").classList.toggle("tree-locked", state.treeLocked);
btnAutoSave.classList.add("active");
ThreadsView.init();
reconnectEvents();
checkGitAvailable();
Promise.all([fetchConfig(), fetchVaults()]).then(fetchTree);
