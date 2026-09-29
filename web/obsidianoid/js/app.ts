import { HamburgerMenu, ThemeManager, FileTree, showToast, confirmDialog, promptDialog } from "@shared";
import type { TreeEntry } from "@shared";

interface VaultInfo { name: string; theme: string; }
interface TreeNode { name: string; path?: string; is_dir?: boolean; mtime?: number; children?: TreeNode[]; }
type SortMode = 'name' | 'recent';
const SORT_KEY = 'obsidianoid-sort';
const LOCK_KEY = 'obsidianoid-tree-locked';
const OVERWRITE_KEY = 'obsidianoid-allow-overwrite';
declare const ThreadsView: { init(): void; activate(): Promise<void>; flush(): Promise<void>; };
/* ─── State ─── */
const state = {
  currentPath: null as string | null,
  isPreviewMode: false,
  isDirty: false,
  treeData: null as TreeNode | null,
  filterText: '',
  /** Paths matching the current search (name or content); null when not searching. */
  searchPaths: null as Set<string> | null,
  sortMode: 'name' as SortMode,
  /** Read-only tree: no moving, renaming or deleting (editing notes still works). */
  treeLocked: false,
  /** The configured thread folder; it can't be renamed (thread mode relies on it). */
  threadsFolder: 'Threads',
  /** Replace a same-named note on move/rename, after an "are you sure?". */
  allowOverwrite: false,
  mode: 'notes',
  activeVault: 0,
  vaults: [] as VaultInfo[],
  autoSave: true,
};
/* ─── Element refs ─── */
const fileTree       = document.getElementById('file-tree')!;
const btnGitSync     = document.getElementById('btn-git-sync') as HTMLButtonElement;
const gitSyncDialog  = document.getElementById('git-sync-dialog') as HTMLDialogElement;
const gitSyncForm    = document.getElementById('git-sync-form')!;
const gitCommitMsg   = document.getElementById('git-commit-msg') as HTMLInputElement;
const btnCancelSync  = document.getElementById('btn-cancel-sync')!;
const editorPane     = document.getElementById('editor-pane') as HTMLTextAreaElement;
const previewPane    = document.getElementById('preview-pane')!;
const emptyState     = document.getElementById('empty-state')!;
const btnToggle      = document.getElementById('btn-toggle-mode') as HTMLButtonElement;
const modeLabel      = document.getElementById('mode-label')!;
const btnSave        = document.getElementById('btn-save') as HTMLButtonElement;
const btnNewNote     = document.getElementById('btn-new-note') as HTMLButtonElement;
const noteTitle      = document.getElementById('note-title')!;
const searchInput    = document.getElementById('search-input') as HTMLInputElement;
const sortSelector   = document.getElementById('sort-selector') as HTMLSelectElement;
const btnNewFolder   = document.getElementById('btn-new-folder') as HTMLButtonElement;
const sidebar        = document.getElementById('sidebar') as HTMLElement;
const resizeHandle   = document.getElementById('resize-handle')!;
const newNoteDialog  = document.getElementById('new-note-dialog') as HTMLDialogElement;
const newNoteForm    = document.getElementById('new-note-form')!;
const newNotePath    = document.getElementById('new-note-path') as HTMLInputElement;
const btnCancelNew   = document.getElementById('btn-cancel-new')!;
const vaultSelector  = document.getElementById('vault-selector') as HTMLSelectElement;
const btnHamburger   = document.getElementById('btn-hamburger')!;
const btnAutoSave    = document.getElementById('btn-autosave')!;

function vaultParam() { return `vault=${state.activeVault}`; }

/* ─── File tree (the shared FileTree) ─── */
// The shared tree draws the rows and handles sorting, folding, keyboard and
// the drag grip; obsidianoid decides what each action means for the vault
// (threads, overwrites, save-before-move), in the functions further down.

/** The server's tree, as the shared tree's entries. */
function toEntries(nodes: TreeNode[] = []): TreeEntry[] {
  return nodes.map(n => ({
    name: n.name,
    path: n.path ?? '',
    isDir: !!n.is_dir,
    mtime: n.mtime,
    children: n.is_dir ? toEntries(n.children) : undefined,
  }));
}

/** Whether a folder holds (or is) the threads folder, which can't be renamed. */
function holdsThreads(folder: string): boolean {
  return folder === state.threadsFolder || state.threadsFolder.startsWith(folder + '/');
}

// While searching, a note shows only if the server's grep matched it (name or
// content); until that answer arrives, the name alone is matched so the tree
// responds as you type. A folder shows if anything inside it does.
function matchesFilter(entry: TreeEntry): boolean {
  if (state.searchPaths) return state.searchPaths.has(entry.path);
  return entry.name.toLowerCase().includes(state.filterText.toLowerCase());
}

const tree = new FileTree({
  label: 'Notes',
  emptyText: 'No notes yet.',
  onOpen: entry => void loadNote(entry.path),
  rename: entry => void (entry.isDir ? renameFolder(entry) : renameNote(entry)),
  canRename: entry => !entry.isDir || !holdsThreads(entry.path),
  // Only empty folders can be deleted.
  remove: entry => void (entry.isDir ? deleteFolder(entry) : deleteNote(entry)),
  canRemove: entry => !entry.isDir || (entry.children ?? []).length === 0,
  move: (entry, folder) => void moveNote(entry, folder),
});
fileTree.replaceChildren(tree.el); // replaces the loading placeholders

/** Redraws the tree from the current sort, search, open note and lock. */
function renderTree() {
  tree.update({
    sort: state.sortMode,
    filter: state.filterText ? matchesFilter : null,
    activePath: state.currentPath,
    locked: state.treeLocked,
  });
}

async function fetchTree() {
  try {
    const res = await fetch(`/api/tree?${vaultParam()}`);
    if (!res.ok) throw new Error('tree fetch failed');
    state.treeData = await res.json() as TreeNode;
    tree.update({ emptyText: 'No notes yet.' });
    tree.setEntries(toEntries(state.treeData.children));
    renderTree();
  } catch (e) {
    tree.update({ emptyText: '⚠ Failed to load vault' });
    tree.setEntries([]);
  }
}

/* ─── Active note highlight sync ─── */
function syncActiveHighlight() {
  tree.update({ activePath: state.currentPath });
}

/* ─── Load note ─── */
async function loadNote(path: string) {
  if (state.isDirty) {
    if (!await confirmDialog('You have unsaved changes. Discard and open new note?')) return;
  }
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(path)}`);
    if (!res.ok) { showToast('Failed to load note', 'error'); return; }
    const text = await res.text();
    state.currentPath = path;
    state.isDirty = false;
    state.isPreviewMode = true;  // open in preview by default
    editorPane.value = text;
    noteTitle.textContent = path;
    btnToggle.disabled = false;
    btnSave.disabled = true;
    await renderPreview(text);
    setEditorMode();
    syncActiveHighlight();
  } catch (e) {
    showToast('Network error loading note', 'error');
  }
}

/* ─── Render preview pane from raw text ─── */
async function renderPreview(text: string) {
  try {
    const res = await fetch('/api/render', {
      method: 'POST',
      headers: { 'Content-Type': 'text/plain' },
      body: text,
    });
    const html = await res.text();
    previewPane.innerHTML = `<div class="md-body">${html}</div>`;
  } catch (e) {
    previewPane.innerHTML = `<div class="md-body"><p style="color:var(--color-danger)">Render failed</p></div>`;
  }
}

/* ─── Reload current note from disk (used by SSE live-update) ─── */
async function reloadCurrentNote() {
  if (!state.currentPath || state.isDirty) return;
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(state.currentPath)}`);
    if (!res.ok) return;
    const text = await res.text();
    editorPane.value = text;
    if (state.isPreviewMode) await renderPreview(text);
  } catch (e) { /* silently ignore network errors during background reload */ }
}

/* ─── Server-Sent Events ─── */
let eventSource: EventSource | null = null;

function reconnectEvents() {
  if (eventSource) { eventSource.close(); eventSource = null; }
  eventSource = new EventSource(`/api/events?${vaultParam()}`);
  eventSource.addEventListener('note-changed', (e) => {
    const data = JSON.parse((e as MessageEvent).data);
    if (data.path === state.currentPath) reloadCurrentNote();
  });
}

/* ─── Editor / Preview toggle ─── */
function show(el: HTMLElement) {
  if (el.id === 'preview-pane') {
    el.style.display = 'block'; // absolutely positioned, not a flex child
  } else if (el.tagName === 'TEXTAREA' || el.id === 'empty-state') {
    el.style.display = 'flex';
  } else {
    el.style.display = 'block';
  }
}
function hide(el: HTMLElement) { el.style.display = 'none'; }

function setEditorMode() {
  const hasNote = !!state.currentPath;
  const prev = state.isPreviewMode;

  if (!hasNote) {
    show(emptyState as HTMLElement);
    hide(editorPane);
    hide(previewPane as HTMLElement);
  } else if (prev) {
    hide(emptyState as HTMLElement);
    hide(editorPane);
    show(previewPane as HTMLElement);
  } else {
    hide(emptyState as HTMLElement);
    show(editorPane);
    hide(previewPane as HTMLElement);
  }

  modeLabel.textContent = prev ? 'Edit' : 'Preview';
  btnToggle.classList.toggle('preview-active', prev);
  btnToggle.title = prev ? 'Switch to editor' : 'Switch to preview';
}

async function toggleMode() {
  if (!state.currentPath) return;
  state.isPreviewMode = !state.isPreviewMode;
  if (state.isPreviewMode) await renderPreview(editorPane.value);
  setEditorMode();
}

/* ─── Save note ─── */
async function saveNote() {
  if (!state.currentPath) return;
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(state.currentPath)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/plain' },
      body: editorPane.value,
    });
    if (!res.ok) { showToast('Save failed', 'error'); return; }
    state.isDirty = false;
    btnSave.disabled = true;
    showToast('✓ Saved', 'success');
    // A save changes this note's modified time, so "Recent" order moves.
    if (state.sortMode === 'recent') void fetchTree();
  } catch (e) {
    showToast('Network error saving note', 'error');
  }
}

/* ─── Auto-save idle timer ─── */
let autoSaveTimer: ReturnType<typeof setTimeout> | undefined;
function scheduleAutoSave() {
  clearTimeout(autoSaveTimer);
  if (state.autoSave && state.isDirty && state.currentPath) {
    autoSaveTimer = setTimeout(saveNote, 1000);
  }
}

/* ─── Dirty tracking ─── */
editorPane.addEventListener('input', () => {
  if (!state.isDirty) {
    state.isDirty = true;
    btnSave.disabled = false;
  }
  scheduleAutoSave();
});

/* ─── Keyboard shortcuts ─── */
document.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && e.key === 's') {
    e.preventDefault();
    if (!btnSave.disabled) saveNote();
  }
  if ((e.ctrlKey || e.metaKey) && e.key === 'e') {
    e.preventDefault();
    if (!btnToggle.disabled) toggleMode();
  }
});

/* ─── Button wiring ─── */
btnToggle.addEventListener('click', toggleMode);
btnSave.addEventListener('click', saveNote);

btnAutoSave.addEventListener('click', () => {
  state.autoSave = !state.autoSave;
  btnAutoSave.classList.toggle('active', state.autoSave);
  state.autoSave ? scheduleAutoSave() : clearTimeout(autoSaveTimer);
});

btnNewNote.addEventListener('click', () => {
  newNotePath.value = '';
  newNoteDialog.showModal();
  newNotePath.focus();
});
btnCancelNew.addEventListener('click', () => newNoteDialog.close());

newNoteForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  let path = newNotePath.value.trim();
  if (!path) return;
  if (!path.endsWith('.md')) path += '.md';
  newNoteDialog.close();
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(path)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'text/plain' },
      body: `# ${path.replace(/.*\//, '').replace('.md', '')}\n\n`,
    });
    if (!res.ok) { showToast('Failed to create note', 'error'); return; }
    await fetchTree();
    await loadNote(path);
    showToast('✓ Note created', 'success');
  } catch (e) {
    showToast('Network error', 'error');
  }
});

/* ─── Sidebar resize ─── */
let isResizing = false;
resizeHandle.addEventListener('mousedown', () => {
  isResizing = true;
  resizeHandle.classList.add('dragging');
  document.body.style.cursor = 'col-resize';
  document.body.style.userSelect = 'none';
});
document.addEventListener('mousemove', e => {
  if (!isResizing) return;
  const newWidth = Math.min(Math.max(e.clientX, 160), window.innerWidth * 0.5);
  sidebar.style.width = newWidth + 'px';
  document.documentElement.style.setProperty('--sidebar-width', newWidth + 'px');
});
document.addEventListener('mouseup', () => {
  if (!isResizing) return;
  isResizing = false;
  resizeHandle.classList.remove('dragging');
  document.body.style.cursor = '';
  document.body.style.userSelect = '';
});

/* ─── Search (grep) filter ─── */
// Typing filters by name at once; 250ms after the last keystroke the server
// greps names and contents, and the tree narrows to exactly those notes.
// Clearing the box shows everything again.
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let searchSeq = 0;

async function runSearch() {
  const q = state.filterText;
  const seq = ++searchSeq;
  if (!q) { state.searchPaths = null; renderTree(); return; }
  try {
    const res = await fetch(`/api/search?${vaultParam()}&q=${encodeURIComponent(q)}`);
    if (!res.ok) return;
    const { paths } = await res.json() as { paths: string[] };
    if (seq !== searchSeq) return; // a newer search superseded this one
    state.searchPaths = new Set(paths);
    renderTree();
  } catch (e) { /* keep the name-only filter on network errors */ }
}

searchInput.addEventListener('input', () => {
  state.filterText = searchInput.value.trim();
  state.searchPaths = null;
  renderTree();
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => void runSearch(), 250);
});

/* ─── Sort ─── */
try {
  const saved = localStorage.getItem(SORT_KEY);
  if (saved === 'name' || saved === 'recent') state.sortMode = saved;
} catch (e) { /* storage unavailable: keep the default */ }
sortSelector.value = state.sortMode;
sortSelector.addEventListener('change', () => {
  state.sortMode = sortSelector.value === 'recent' ? 'recent' : 'name';
  try { localStorage.setItem(SORT_KEY, state.sortMode); } catch (e) { /* ignore */ }
  renderTree();
});

/* ─── Moving, renaming, deleting ─── */
// The tree's grip drags a note onto a folder (into it), a note (beside it) or
// empty space (the vault root); order inside a folder comes from the sort.
function folderOf(path: string): string {
  const i = path.lastIndexOf('/');
  return i < 0 ? '' : path.slice(0, i);
}

/** Whether the loaded tree already has a note at path. */
function noteInTree(path: string, node: TreeNode | null = state.treeData): boolean {
  if (!node) return false;
  if (!node.is_dir) return node.path === path;
  return (node.children || []).some(c => noteInTree(path, c));
}

/**
 * POSTs a move/rename. A clash is spotted in the loaded tree before anything
 * is sent, so the expected case never produces a 409: with "Allow overwrites"
 * on it asks first and sends the overwrite straight away; with it off it
 * answers 409 locally for the caller to report. (A 409 can
 * still happen if another tab changed the vault since the tree loaded; the
 * same question is asked then.) Returns null if the user declined.
 */
async function postWithOverwrite(url: string, body: Record<string, unknown>, targetPath: string): Promise<Response | null> {
  const send = (overwrite: boolean) => fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ ...body, overwrite }),
  });
  if (noteInTree(targetPath)) {
    // Known clash: ask (overwrites allowed) or refuse locally (not allowed),
    // without sending a request the server would reject.
    if (!state.allowOverwrite) return new Response(null, { status: 409 });
    return (await confirmOverwrite(targetPath)) ? send(true) : null;
  }
  const res = await send(false);
  if (res.status !== 409 || !state.allowOverwrite) return res;
  return (await confirmOverwrite(targetPath)) ? send(true) : null;
}

function confirmOverwrite(targetPath: string): Promise<boolean> {
  const dirtyTarget = targetPath === state.currentPath && state.isDirty;
  return confirmDialog(
    `"${targetPath}" already exists. Overwrite it? The existing note will be replaced` +
      (dirtyTarget ? ', including your unsaved changes to it' : '') + '.',
    { title: 'Overwrite note', confirmLabel: 'Overwrite' },
  );
}

/** After an overwrite, an open note that was the replaced one shows its new content. */
function afterOverwrite(targetPath: string) {
  if (state.currentPath === targetPath) {
    state.isDirty = false;
    btnSave.disabled = true;
    void reloadCurrentNote();
  }
}

/** Refuses a structural change while the tree is locked; true when allowed. */
function treeUnlocked(): boolean {
  if (state.treeLocked) showToast('The file tree is locked. Unlock it in Settings to change it.', 'notice');
  return !state.treeLocked;
}

async function moveNote(node: TreeEntry, folder: string) {
  if (!treeUnlocked()) return;
  const path = node.path!;
  if (path === state.currentPath && state.isDirty) {
    showToast('Save your changes before moving this note.', 'notice');
    return;
  }
  const baseName = path.slice(path.lastIndexOf('/') + 1);
  const targetPath = folder ? `${folder}/${baseName}` : baseName;
  try {
    const res = await postWithOverwrite(`/api/note/move?${vaultParam()}`, { path, folder }, targetPath);
    if (!res) return;
    if (!res.ok) {
      showToast(res.status === 409
        ? 'That folder already has a note with this name. (Turn on "Allow overwrites" in Settings to replace it.)'
        : 'Move failed.', 'error');
      return;
    }
    afterOverwrite(targetPath);
    const { path: newPath, thread_reset: threadReset } = await res.json() as { path: string; thread_reset?: boolean };
    if (state.currentPath === path) {
      state.currentPath = newPath;
      noteTitle.textContent = newPath;
    }
    const where = folder || 'the vault root';
    showToast(threadReset
      ? `Moved ${node.name} to ${where}. It's a regular note now; its thread slot starts over as an empty thread.`
      : `Moved ${node.name} to ${where}`, 'success');
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast('Network error moving note', 'error');
  }
}

/* ─── New folder ─── */
btnNewFolder.addEventListener('click', async () => {
  const name = await promptDialog('Folder name (use / for nested folders, e.g. Projects/2026):', {
    title: 'New folder',
    confirmLabel: 'Create',
  });
  if (name === null || !name.trim()) return;
  try {
    const res = await fetch(`/api/folder?${vaultParam()}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: name.trim() }),
    });
    if (!res.ok) {
      const msg = res.status === 409 ? 'That folder already exists.'
        : res.status === 400 ? 'That folder name isn\'t allowed (names can\'t start with a dot).'
        : 'Could not create the folder.';
      showToast(msg, 'error');
      return;
    }
    showToast(`Created folder ${name.trim()}`, 'success');
    await fetchTree();
  } catch (e) {
    showToast('Network error creating folder', 'error');
  }
});

/* ─── Rename folder ─── */
async function renameFolder(node: TreeEntry) {
  if (!treeUnlocked()) return;
  const path = node.path ?? '';
  if (!path) return;
  const name = await promptDialog('New name for this folder:', {
    title: 'Rename folder',
    defaultValue: node.name,
    confirmLabel: 'Rename',
  });
  if (name === null || !name.trim() || name.trim() === node.name) return;
  try {
    const res = await fetch(`/api/folder/rename?${vaultParam()}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path, name: name.trim() }),
    });
    if (!res.ok) {
      const msg = res.status === 409 ? 'Something here already has that name.'
        : res.status === 403 ? 'The thread folder can\'t be renamed; thread mode depends on it.'
        : res.status === 400 ? 'That name isn\'t allowed (no slashes, and it can\'t start with a dot).'
        : 'Rename failed.';
      showToast(msg, 'error');
      return;
    }
    const { path: newPath } = await res.json() as { path: string };
    // The open note moves with its folder.
    if (state.currentPath && state.currentPath.startsWith(path + '/')) {
      state.currentPath = newPath + state.currentPath.slice(path.length);
      noteTitle.textContent = state.currentPath;
    }
    showToast(`Renamed folder to ${name.trim()}`, 'success');
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast('Network error renaming folder', 'error');
  }
}

/* ─── Delete empty folder ─── */
// The trash icon only appears on folders with nothing in them; the server
// double-checks, and refuses a folder that still holds hidden files.
async function deleteFolder(node: TreeEntry) {
  if (!treeUnlocked()) return;
  const path = node.path ?? '';
  if (!path) return;
  const ok = await confirmDialog(`Delete the empty folder "${path}"?`, { title: 'Delete folder', confirmLabel: 'Delete' });
  if (!ok) return;
  try {
    const res = await fetch(`/api/folder?${vaultParam()}&path=${encodeURIComponent(path)}`, { method: 'DELETE' });
    if (!res.ok) {
      showToast(res.status === 409
        ? 'That folder isn\'t empty (it may contain hidden files).'
        : 'Could not delete the folder.', 'error');
      await fetchTree();
      return;
    }
    showToast(`Deleted folder ${path}`, 'success');
    await fetchTree();
  } catch (e) {
    showToast('Network error deleting folder', 'error');
  }
}

/* ─── Delete ─── */
async function deleteNote(node: TreeEntry) {
  if (!treeUnlocked()) return;
  const path = node.path!;
  const isOpen = path === state.currentPath;
  const ok = await confirmDialog(
    `Delete "${node.name}"? This removes ${path} from the vault` +
      (isOpen && state.isDirty ? ', including your unsaved changes' : '') + '.',
    { title: 'Delete note', confirmLabel: 'Delete' },
  );
  if (!ok) return;
  try {
    const res = await fetch(`/api/note?${vaultParam()}&path=${encodeURIComponent(path)}`, { method: 'DELETE' });
    if (!res.ok) { showToast('Delete failed', 'error'); return; }
    const { thread_reset: threadReset } = await res.json() as { thread_reset?: boolean };
    if (state.currentPath === path) {
      // The open note is gone: return to the empty state.
      clearTimeout(autoSaveTimer);
      state.currentPath = null;
      state.isDirty = false;
      noteTitle.textContent = '';
      editorPane.value = '';
      btnToggle.disabled = true;
      btnSave.disabled = true;
      setEditorMode();
    }
    showToast(threadReset
      ? `Deleted ${node.name}. Its thread slot starts over as an empty thread.`
      : `Deleted ${node.name}`, 'success');
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast('Network error deleting note', 'error');
  }
}

/* ─── Rename ─── */
async function renameNote(node: TreeEntry) {
  if (!treeUnlocked()) return;
  const path = node.path!;
  if (path === state.currentPath && state.isDirty) {
    showToast('Save your changes before renaming this note.', 'notice');
    return;
  }
  const name = await promptDialog('New name for this note:', {
    title: 'Rename note',
    defaultValue: node.name,
    confirmLabel: 'Rename',
  });
  if (name === null || name.trim() === '' || name.trim() === node.name) return;
  const dir = folderOf(path);
  const newFile = name.trim().replace(/\.md$/, '') + '.md';
  const targetPath = dir ? `${dir}/${newFile}` : newFile;
  try {
    const res = await postWithOverwrite(`/api/note/rename?${vaultParam()}`, { path, name: name.trim() }, targetPath);
    if (!res) return;
    if (!res.ok) {
      const msg = res.status === 409 ? 'A note with that name already exists. (Turn on "Allow overwrites" in Settings to replace it.)'
        : res.status === 400 ? 'That name isn\'t allowed (no slashes, and it can\'t start with a dot).'
        : 'Rename failed.';
      showToast(msg, 'error');
      return;
    }
    if (targetPath !== path) afterOverwrite(targetPath);
    const { path: newPath, thread_reset: threadReset } = await res.json() as { path: string; thread_reset?: boolean };
    if (state.currentPath === path) {
      state.currentPath = newPath;
      noteTitle.textContent = newPath;
    }
    showToast(threadReset
      ? `Renamed to ${name.trim()}. It's a regular note now; its thread slot starts over as an empty thread.`
      : `Renamed to ${name.trim()}`, 'success');
    await fetchTree();
    if (state.filterText) void runSearch();
  } catch (e) {
    showToast('Network error renaming note', 'error');
  }
}

/* ─── Git sync ─── */
async function checkGitAvailable() {
  try {
    const data = await (await fetch(`/api/git/status?${vaultParam()}`)).json() as { available: boolean };
    btnGitSync.hidden = !data.available;
  } catch (e) {}
}

btnGitSync.addEventListener('click', () => {
  const now = new Date();
  const ts = now.toISOString().slice(0, 16).replace('T', ' ');
  gitCommitMsg.value = `obsidianoid sync ${ts}`;
  gitSyncDialog.showModal();
  gitCommitMsg.select();
});

btnCancelSync.addEventListener('click', () => gitSyncDialog.close());

gitSyncForm.addEventListener('submit', async (e) => {
  e.preventDefault();
  const message = gitCommitMsg.value.trim() || 'obsidianoid sync';
  gitSyncDialog.close();
  btnGitSync.disabled = true;
  try {
    const res = await fetch(`/api/git/sync?${vaultParam()}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message }),
    });
    const data = await res.json() as { ok: boolean; output: string };
    if (data.ok) showToast('✓ Synced', 'success');
    else { showToast('Sync failed — see console', 'error'); console.error('git sync:', data.output); }
  } catch (e) {
    showToast('Sync failed', 'error');
  } finally {
    btnGitSync.disabled = false;
  }
});

/* ─── Mode switching ─── */
function setMode(mode: string) {
  if (state.mode === 'threads' && mode !== 'threads') {
    // Leaving Threads view: once any pending thread edit is saved, reload the
    // tree so thread files written or recreated there show up in Notes view.
    void ThreadsView.flush().catch(() => undefined).finally(() => {
      void fetchTree().then(() => { if (state.filterText) void runSearch(); });
    });
  }
  state.mode = mode;
  document.getElementById('app')!.dataset.mode = mode;
  document.getElementById('btn-mode-notes')!.classList.toggle('active', mode === 'notes');
  document.getElementById('btn-mode-threads')!.classList.toggle('active', mode === 'threads');
  if (mode === 'threads') ThreadsView.activate();
}

document.getElementById('btn-mode-notes')!.addEventListener('click', () => setMode('notes'));
document.getElementById('btn-mode-threads')!.addEventListener('click', () => setMode('threads'));

/* ─── Theme ─── */
// One-time storage migration for the palette rename: this module's own
// near-black palette used to be stored under the name the shared matrix now
// gives to GitHub-dark, so a browser carrying an explicit choice would
// silently resolve to a different (and much bluer) ground.
//
// The keys are discovered from STORAGE, not from the vault roster: the roster
// arrives over the network at the bottom of this file, so it is empty for the
// whole of module evaluation, and deferring the rewrite until after it would
// mean rewriting storage the resolver had already read. Three properties come
// out of the prefix scan and would not come out of a roster loop: it reaches
// keys for vaults no longer in the config, Object.keys is snapshotted before
// the writes, and the test is on the KEY prefix, so no other module's stored
// theme is reachable by construction. The equality on the value is exact, so
// forest/ocean/ember/rose and an already-migrated value are left alone.
const MIGRATED = 'ui-theme-migrated:obsidianoid';
if (!localStorage.getItem(MIGRATED)) {
  for (const k of Object.keys(localStorage)) {
    if (k.startsWith('obsidianoid-theme-') && localStorage.getItem(k) === 'dark') {
      localStorage.setItem(k, 'obsidian');
    }
  }
  localStorage.setItem(MIGRATED, '1');
}

async function fetchConfig() {
  try {
    const d = await (await fetch('/api/config')).json() as {
      autosave: boolean; threads_folder?: string; thread_count?: number; max_threads?: number;
    };
    state.autoSave = d.autosave !== false;
    if (typeof d.threads_folder === 'string') state.threadsFolder = d.threads_folder;
    if (d.thread_count && d.max_threads) fillThreadCount(d.thread_count, d.max_threads);
    btnAutoSave.classList.toggle('active', state.autoSave);
  } catch (e) { /* keep default */ }
}

async function fetchVaults() {
  try {
    const res = await fetch('/api/vaults');
    if (!res.ok) return;
    state.vaults = await res.json() as VaultInfo[];
    vaultSelector.innerHTML = '';
    state.vaults.forEach((v, i) => {
      const opt = document.createElement('option');
      opt.value = String(i);
      opt.textContent = v.name;
      vaultSelector.appendChild(opt);
    });
    // The first moment the roster exists is the first moment serverDefault()
    // can answer, so this is the initial per-vault resolution. It deliberately
    // does not write storage: persisting the server's answer here would make a
    // configured default indistinguishable from a user's own choice for ever
    // after. Safe on an empty roster — serverDefault() returns undefined and
    // resolution falls through.
    themes.reresolve();
  } catch (e) { /* continue with vault 0 */ }
}

function switchVault(idx: number) {
  clearTimeout(autoSaveTimer);
  state.activeVault = idx;
  state.currentPath = null;
  state.isDirty = false;
  noteTitle.textContent = '';
  btnToggle.disabled = true;
  btnSave.disabled = true;
  setEditorMode();
  // The storage key closes over state.activeVault, which has just changed, so
  // the closure must be re-run rather than merely relied upon.
  themes.reresolve();
  reconnectEvents();
  checkGitAvailable();
  state.searchPaths = null;
  fetchTree().then(() => { if (state.filterText) void runSearch(); });
}

vaultSelector.addEventListener('change', () => switchVault(parseInt(vaultSelector.value)));

/* ─── Init ─── */
const themes = new ThemeManager({
  module: 'obsidianoid',
  default: 'obsidian',
  storageKey: () => `obsidianoid-theme-${state.activeVault}`,
  serverDefault: () => state.vaults[state.activeVault]?.theme,
});
// The trigger is the topbar button adopted in place (keeps its glyph, gains
// a11y wiring). It is a direct child of #topbar, outside #topbar-actions,
// so it stays visible when thread mode hides #topbar-actions (D-5).
// Tree lock: a per-browser setting. Locked, the tree can be browsed, searched
// and sorted, and notes still edited, but nothing can be moved, renamed or deleted.
try { state.treeLocked = localStorage.getItem(LOCK_KEY) === '1'; } catch (e) { /* keep unlocked */ }
try { state.allowOverwrite = localStorage.getItem(OVERWRITE_KEY) === '1'; } catch (e) { /* keep off */ }

/** A shared-toggle row for the Settings drawer. */
function settingToggle(text: string, hint: string, checked: boolean, onChange: (on: boolean) => void): HTMLLabelElement {
  const label = document.createElement('label');
  label.className = 'ui-toggle';
  label.title = hint;
  const input = document.createElement('input');
  input.type = 'checkbox';
  input.checked = checked;
  const track = document.createElement('span');
  track.className = 'ui-toggle-track';
  label.append(input, track, text);
  input.addEventListener('change', () => onChange(input.checked));
  return label;
}

// Thread count: filled in once /api/config answers (the drawer is built first).
const threadCountSelect = document.createElement('select');
threadCountSelect.className = 'settings-select';
threadCountSelect.setAttribute('aria-label', 'Number of threads');
function fillThreadCount(count: number, max: number) {
  threadCountSelect.innerHTML = '';
  for (let n = 1; n <= max; n++) {
    const opt = document.createElement('option');
    opt.value = String(n);
    opt.textContent = String(n);
    threadCountSelect.append(opt);
  }
  threadCountSelect.value = String(count);
}
threadCountSelect.addEventListener('change', async () => {
  const count = parseInt(threadCountSelect.value, 10);
  try {
    if (state.mode === 'threads') await ThreadsView.flush();
    const res = await fetch('/api/threads/count', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ count }),
    });
    if (!res.ok) { showToast('Could not change the number of threads', 'error'); return; }
    showToast(`Threads: ${count}`, 'success');
    if (state.mode === 'threads') void ThreadsView.activate();
  } catch (e) {
    showToast('Network error changing the number of threads', 'error');
  }
});

new HamburgerMenu({
  title: 'Settings',
  items: [
    { section: 'File tree' },
    {
      id: 'tree-lock',
      render: (host: HTMLElement) => {
        host.append(settingToggle(
          'Lock file tree',
          'Prevent moving, renaming and deleting in the file tree. Notes can still be edited.',
          state.treeLocked,
          on => {
            state.treeLocked = on;
            try { localStorage.setItem(LOCK_KEY, on ? '1' : '0'); } catch (e) { /* ignore */ }
            document.getElementById('sidebar')!.classList.toggle('tree-locked', on);
            renderTree();
            showToast(on ? 'File tree locked' : 'File tree unlocked', 'notice');
          },
        ));
      },
    },
    {
      id: 'allow-overwrite',
      render: (host: HTMLElement) => {
        host.append(settingToggle(
          'Allow overwrites',
          'When moving or renaming a note onto an existing note, ask and then replace it instead of refusing.',
          state.allowOverwrite,
          on => {
            state.allowOverwrite = on;
            try { localStorage.setItem(OVERWRITE_KEY, on ? '1' : '0'); } catch (e) { /* ignore */ }
          },
        ));
      },
    },
    { section: 'Threads' },
    {
      id: 'thread-count',
      render: (host: HTMLElement) => {
        const row = document.createElement('label');
        row.className = 'settings-row';
        row.append('Number of threads', threadCountSelect);
        host.append(row);
      },
    },
  ],
  themePicker: true,
  themes,
  side: 'right',
  mountTrigger: btnHamburger,
});
document.getElementById('sidebar')!.classList.toggle('tree-locked', state.treeLocked);
btnAutoSave.classList.add('active');
ThreadsView.init();
reconnectEvents();
checkGitAvailable();
Promise.all([fetchConfig(), fetchVaults()]).then(fetchTree);
