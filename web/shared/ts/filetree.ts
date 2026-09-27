// filetree.ts — the shared file tree, and the tree picker built on it.
//
// FileTree renders folders and files into one element and handles what every
// tree needs: name/recent sorting, a filter, folders that stay open or closed
// across refreshes, keyboard navigation, loading folders all at once or on
// demand, and optional selection (pickers). Structural edits are offered as
// UI only: when the module passes rename/remove/move callbacks, rows get
// rename and delete buttons and a drag grip, and the tree calls back with
// the entry (and, for a move, the destination folder). The module keeps its
// own rules and dialogs -- what a move means for its data is its business.
//
// Everything is built with DOM nodes and textContent: file names never
// become markup.
//
// openTreePicker wraps a FileTree in a modal for choosing a file or folder.

import { openModal } from "./modal.js";
import { chevronIcon, editIcon, fileIcon, folderIcon, gripIcon, trashIcon } from "./icons.js";

/** One file or folder. `path` is its unique id (a module's own path format). */
export interface TreeEntry {
  name: string;
  path: string;
  isDir: boolean;
  /** Last modified (any unit, larger = newer); used by the "recent" sort. */
  mtime?: number;
  /** Short trailing text, e.g. a size. */
  meta?: string;
  /** Tooltip; defaults to the path. */
  title?: string;
  /** Shown but not selectable. */
  disabled?: boolean;
  /** A folder that can't be opened here (its contents aren't browsable). */
  leaf?: boolean;
  /** A folder's contents; undefined = not loaded yet (see FileTreeOptions.load). */
  children?: TreeEntry[];
}

export type TreeSort = "name" | "recent";

export interface FileTreeOptions {
  /** Names the tree for assistive tech. */
  label: string;
  /** Loads a folder's contents on demand; `null` asks for the top level. */
  load?: (dir: TreeEntry | null) => Promise<TreeEntry[]>;
  /** Top-level entries, when the whole tree is known up front. */
  entries?: TreeEntry[];
  sort?: TreeSort;
  /** Which files to show (folders show when something inside does). */
  filter?: ((entry: TreeEntry) => boolean) | null;
  /** Folders start open ("all") or closed ("none"). Default: "all" for a
   * tree given `entries`, "none" for one that loads on demand. */
  openFolders?: "all" | "none";
  /** Highlights the entry being worked on (e.g. the open note). */
  activePath?: string | null;
  /** A file was opened: clicked or Enter (in a picker: double-clicked). */
  onOpen?: (entry: TreeEntry) => void;
  /** Picker mode: what can be selected. */
  select?: "file" | "dir";
  onSelect?: (entry: TreeEntry) => void;
  /** No renaming, deleting or moving, whatever callbacks are given. */
  locked?: boolean;
  rename?: (entry: TreeEntry) => void;
  canRename?: (entry: TreeEntry) => boolean;
  remove?: (entry: TreeEntry) => void;
  canRemove?: (entry: TreeEntry) => boolean;
  /** An entry was dragged into `folder` (a folder's path, or rootPath). */
  move?: (entry: TreeEntry, folder: string) => void;
  /** Default: files can be dragged, folders can't. */
  canMove?: (entry: TreeEntry) => boolean;
  /** The folder a drop on empty tree space means. Default "". */
  rootPath?: string;
  /** Shown when there is nothing to list. */
  emptyText?: string;
}

const byName = (a: TreeEntry, b: TreeEntry): number =>
  a.name.localeCompare(b.name, undefined, { sensitivity: "base", numeric: true });

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  node.className = className;
  return node;
}

export class FileTree {
  /** The tree element; the module places it. */
  readonly el: HTMLElement;
  private options: FileTreeOptions;
  private entries: TreeEntry[] = [];
  /** Folders opened or closed by the user, overriding openFolders. */
  private open = new Map<string, boolean>();
  private byPath = new Map<string, TreeEntry>();
  private parentOf = new Map<string, string>();
  private loading = new Set<string>();
  private errors = new Map<string, string>();
  private selectedPath: string | null = null;
  private topLoaded = false;
  /** The last row click, to spot a double-click even if the row was redrawn in between. */
  private lastClick = { path: "", at: 0 };
  /** Open folders met while rendering that still need loading; started after. */
  private queued: TreeEntry[] = [];

  constructor(options: FileTreeOptions) {
    this.options = options;
    this.el = el("div", "ui-tree");
    this.el.setAttribute("role", "tree");
    this.el.setAttribute("aria-label", options.label);
    this.el.addEventListener("click", (e) => this.onClick(e));
    this.el.addEventListener("keydown", (e) => this.onKeydown(e));
    if (options.entries) {
      this.entries = options.entries;
      this.topLoaded = true;
      this.render();
    } else {
      void this.loadTop();
    }
  }

  /** Replaces the top-level entries (open/closed folders are kept). */
  setEntries(entries: TreeEntry[]): void {
    this.entries = entries;
    this.topLoaded = true;
    this.render();
  }

  /** Changes options (sort, filter, activePath, locked, …) and redraws. */
  update(changes: Partial<FileTreeOptions>): void {
    this.options = { ...this.options, ...changes };
    this.render();
  }

  /** The selected entry, in a picker. */
  get selected(): TreeEntry | null {
    return this.selectedPath === null ? null : (this.byPath.get(this.selectedPath) ?? null);
  }

  /** Reloads a folder's contents (or, with no path, the top level). */
  async reload(path?: string): Promise<void> {
    if (path === undefined) {
      await this.loadTop();
      return;
    }
    const entry = this.byPath.get(path);
    if (entry && entry.isDir) {
      entry.children = undefined;
      await this.loadChildren(entry);
    }
  }

  /**
   * Opens the folders down to `path`, loading them as needed, then selects
   * it (in a picker) and scrolls it into view. Paths nest by prefix: a
   * folder "a/b" (or "/a") contains "a/b/c" (or "/a/b").
   */
  async reveal(path: string): Promise<void> {
    if (!this.topLoaded) await this.loadTop();
    let level = this.entries;
    for (;;) {
      const hit = level.find((e) => e.path === path || (e.isDir && contains(e.path, path)));
      if (!hit) break;
      if (hit.path === path) {
        this.choose(hit, false);
        break;
      }
      this.open.set(hit.path, true);
      if (hit.children === undefined) await this.loadChildren(hit);
      level = hit.children ?? [];
    }
    this.render();
    this.rowFor(path)?.scrollIntoView({ block: "nearest" });
  }

  /** Focuses the selected, active or first row. */
  focus(): void {
    const row =
      (this.selectedPath && this.rowFor(this.selectedPath)) ||
      (this.options.activePath && this.rowFor(this.options.activePath)) ||
      this.rows()[0];
    row?.focus();
  }

  // --- loading ---------------------------------------------------------------

  private async loadTop(): Promise<void> {
    if (!this.options.load) return;
    this.loading.add("");
    this.render();
    try {
      this.entries = await this.options.load(null);
      this.errors.delete("");
    } catch (err) {
      this.errors.set("", err instanceof Error ? err.message : String(err));
    } finally {
      this.loading.delete("");
      this.topLoaded = true;
      this.render();
    }
  }

  private async loadChildren(entry: TreeEntry): Promise<void> {
    if (!this.options.load || this.loading.has(entry.path)) return;
    this.loading.add(entry.path);
    this.render();
    try {
      entry.children = await this.options.load(entry);
      this.errors.delete(entry.path);
    } catch (err) {
      this.errors.set(entry.path, err instanceof Error ? err.message : String(err));
    } finally {
      this.loading.delete(entry.path);
      this.render();
    }
  }

  // --- rendering ---------------------------------------------------------------

  private isOpen(entry: TreeEntry): boolean {
    if (entry.leaf) return false;
    // While filtering, folders show open so every match is visible.
    if (this.options.filter) return true;
    const set = this.open.get(entry.path);
    if (set !== undefined) return set;
    const all = this.options.openFolders ?? (this.options.load ? "none" : "all");
    return all === "all";
  }

  private visible(entry: TreeEntry): boolean {
    const filter = this.options.filter;
    if (!filter) return true;
    if (!entry.isDir) return filter(entry);
    // A folder not loaded yet might hold a match; a loaded one shows if any child does.
    return entry.children === undefined || entry.children.some((c) => this.visible(c));
  }

  private sorted(entries: TreeEntry[]): TreeEntry[] {
    const recent = this.options.sort === "recent";
    return [...entries].sort((a, b) => {
      if (recent) return (b.mtime ?? 0) - (a.mtime ?? 0) || byName(a, b);
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
      return byName(a, b);
    });
  }

  private render(): void {
    const active = document.activeElement as HTMLElement | null;
    const focusedPath = active && this.el.contains(active) ? active.closest<HTMLElement>(".ui-tree-row")?.dataset.path : undefined;
    this.byPath.clear();
    this.parentOf.clear();
    const frag = document.createDocumentFragment();
    if (this.loading.has("")) {
      frag.append(this.noteRow("Loading…", 0));
    } else if (this.errors.has("")) {
      frag.append(this.noteRow(this.errors.get("")!, 0, true));
    } else {
      const shown = this.appendLevel(frag, this.entries, 0, this.options.rootPath ?? "");
      if (!shown && this.topLoaded) frag.append(this.noteRow(this.options.emptyText ?? "Nothing here.", 0));
    }
    this.el.replaceChildren(frag);
    this.el.classList.toggle("is-locked", this.isLocked());
    // Keep one row in the tab order (roving tabindex), and keep focus put.
    const tabRow =
      (focusedPath && this.rowFor(focusedPath)) ||
      (this.selectedPath && this.rowFor(this.selectedPath)) ||
      (this.options.activePath && this.rowFor(this.options.activePath)) ||
      this.rows()[0];
    if (tabRow) tabRow.tabIndex = 0;
    if (focusedPath && tabRow && tabRow.dataset.path === focusedPath) tabRow.focus();
    for (const entry of this.queued.splice(0)) void this.loadChildren(entry);
  }

  /** Appends one level's rows; returns how many were shown. */
  private appendLevel(parent: Node, entries: TreeEntry[], depth: number, parentPath: string): number {
    let shown = 0;
    for (const entry of this.sorted(entries)) {
      this.byPath.set(entry.path, entry);
      this.parentOf.set(entry.path, parentPath);
      if (!this.visible(entry)) continue;
      shown++;
      const row = this.row(entry, depth);
      if (!entry.isDir) {
        parent.appendChild(row);
        continue;
      }
      const wrap = el("div", "ui-tree-branch");
      wrap.append(row);
      if (this.isOpen(entry)) {
        const group = el("div", "ui-tree-group");
        group.setAttribute("role", "group");
        if (this.loading.has(entry.path) || (entry.children === undefined && this.options.load && !this.errors.has(entry.path))) {
          group.append(this.noteRow("Loading…", depth + 1));
          if (!this.loading.has(entry.path)) this.queued.push(entry);
        } else if (this.errors.has(entry.path)) {
          group.append(this.noteRow(this.errors.get(entry.path)!, depth + 1, true));
        } else if (entry.children === undefined) {
          // Nothing to load it with: show as empty.
          if (!this.options.filter) group.append(this.noteRow("Empty", depth + 1));
        } else if (entry.children.length === 0) {
          if (!this.options.filter) group.append(this.noteRow("Empty", depth + 1));
        } else {
          this.appendLevel(group, entry.children, depth + 1, entry.path);
        }
        wrap.append(group);
      }
      parent.appendChild(wrap);
    }
    return shown;
  }

  private row(entry: TreeEntry, depth: number): HTMLElement {
    const row = el("div", "ui-tree-row");
    row.setAttribute("role", "treeitem");
    row.setAttribute("aria-level", String(depth + 1));
    row.tabIndex = -1;
    row.dataset.path = entry.path;
    row.style.setProperty("--ui-tree-depth", String(depth));
    row.title = entry.title ?? entry.path;
    if (entry.isDir) {
      const open = this.isOpen(entry);
      row.classList.add("is-dir");
      row.classList.toggle("is-open", open);
      if (!entry.leaf) row.setAttribute("aria-expanded", String(open));
    }
    if (entry.path === this.options.activePath) row.classList.add("is-active");
    if (this.options.select) {
      const selected = entry.path === this.selectedPath;
      row.classList.toggle("is-selected", selected);
      row.setAttribute("aria-selected", String(selected));
    }
    if (entry.disabled) {
      row.classList.add("is-disabled");
      row.setAttribute("aria-disabled", "true");
    }

    if (this.canDo("move", entry)) {
      const grip = el("span", "ui-tree-grip");
      grip.title = "Drag to move into a folder";
      grip.append(gripIcon());
      grip.addEventListener("pointerdown", (e) => this.startDrag(e, grip, row, entry));
      row.append(grip);
    }
    const twisty = el("span", "ui-tree-twisty");
    if (entry.isDir && !entry.leaf) twisty.append(chevronIcon());
    const iconEl = el("span", "ui-tree-icon");
    iconEl.append(entry.isDir ? folderIcon() : fileIcon());
    const name = el("span", "ui-tree-name");
    name.textContent = entry.name;
    row.append(twisty, iconEl, name);
    if (entry.meta) {
      const meta = el("span", "ui-tree-meta");
      meta.textContent = entry.meta;
      row.append(meta);
    }
    const tools = el("span", "ui-tree-tools");
    const kind = entry.isDir ? "folder" : "file";
    if (this.canDo("rename", entry)) tools.append(this.tool("rename", editIcon(), `Rename ${kind} ${entry.name}`, `Rename ${kind}`));
    if (this.canDo("remove", entry)) tools.append(this.tool("remove", trashIcon(), `Delete ${kind} ${entry.name}`, `Delete ${kind}`));
    if (tools.childElementCount) row.append(tools);
    return row;
  }

  private tool(action: string, icon: SVGElement, label: string, tip: string): HTMLButtonElement {
    const btn = el("button", `ui-tree-tool ui-tree-${action}`);
    btn.type = "button";
    btn.dataset.action = action;
    btn.title = tip;
    btn.setAttribute("aria-label", label);
    btn.tabIndex = -1;
    btn.append(icon);
    return btn;
  }

  private noteRow(text: string, depth: number, error = false): HTMLElement {
    const note = el("div", error ? "ui-tree-note is-error" : "ui-tree-note");
    note.style.setProperty("--ui-tree-depth", String(depth));
    note.textContent = text;
    return note;
  }

  private isLocked(): boolean {
    return !!this.options.locked;
  }

  private canDo(action: "rename" | "remove" | "move", entry: TreeEntry): boolean {
    if (this.isLocked()) return false;
    const o = this.options;
    if (action === "rename") return !!o.rename && (o.canRename?.(entry) ?? true);
    if (action === "remove") return !!o.remove && (o.canRemove?.(entry) ?? true);
    return !!o.move && (o.canMove?.(entry) ?? !entry.isDir);
  }

  // --- interaction ---------------------------------------------------------------

  private rows(): HTMLElement[] {
    return Array.from(this.el.querySelectorAll<HTMLElement>(".ui-tree-row"));
  }

  private rowFor(path: string): HTMLElement | null {
    return this.el.querySelector<HTMLElement>(`.ui-tree-row[data-path="${CSS.escape(path)}"]`);
  }

  private entryOf(target: EventTarget | null): TreeEntry | null {
    const row = (target as HTMLElement | null)?.closest?.<HTMLElement>(".ui-tree-row");
    return row?.dataset.path !== undefined ? (this.byPath.get(row.dataset.path) ?? null) : null;
  }

  private toggle(entry: TreeEntry, open = !this.isOpen(entry)): void {
    this.open.set(entry.path, open);
    if (open) this.errors.delete(entry.path); // reopening retries a failed load
    this.render();
  }

  /** Selects (picker) an entry; with `open`, also treats it as chosen. */
  private choose(entry: TreeEntry, open: boolean): void {
    if (entry.disabled) return;
    const selectable = this.options.select === (entry.isDir ? "dir" : "file");
    if (selectable) {
      this.selectedPath = entry.path;
      this.options.onSelect?.(entry);
    }
    if (open && (selectable || !this.options.select)) this.options.onOpen?.(entry);
  }

  /** The row's main action: a folder opens/closes (and selects, in a folder
   * picker); a file opens, or is selected in a picker. */
  private activate(entry: TreeEntry): void {
    if (entry.isDir) {
      if (this.options.select === "dir") {
        this.choose(entry, false);
        // Selecting a folder also opens it, unless it can't be opened.
        if (entry.leaf) this.markSelection();
        else this.toggle(entry, true);
      } else {
        this.toggle(entry);
      }
      return;
    }
    if (this.options.select) {
      this.choose(entry, false);
      this.markSelection();
    } else {
      this.choose(entry, true);
    }
  }

  private onClick(e: MouseEvent): void {
    const target = e.target as HTMLElement;
    if (target.closest(".ui-tree-grip")) return;
    const entry = this.entryOf(target);
    if (!entry) return;
    const tool = target.closest<HTMLElement>(".ui-tree-tool");
    if (tool) {
      e.stopPropagation();
      if (tool.dataset.action === "rename") this.options.rename?.(entry);
      if (tool.dataset.action === "remove") this.options.remove?.(entry);
      return;
    }
    // The chevron only opens/closes, even in a folder picker.
    if (entry.isDir && !entry.leaf && target.closest(".ui-tree-twisty")) {
      this.toggle(entry);
      return;
    }
    // In a picker, a second click on the same entry (a double-click) chooses
    // it. Timed here, not with dblclick, because the first click may redraw
    // the row, and the browser then never pairs the two clicks.
    const now = Date.now();
    const again = this.lastClick.path === entry.path && now - this.lastClick.at < 450;
    this.lastClick = again ? { path: "", at: 0 } : { path: entry.path, at: now };
    if (again && this.options.select === (entry.isDir ? "dir" : "file")) {
      this.choose(entry, true);
      return;
    }
    this.activate(entry);
  }

  /** Moves the selection mark without redrawing the rows. */
  private markSelection(): void {
    for (const row of this.rows()) {
      const selected = row.dataset.path === this.selectedPath;
      row.classList.toggle("is-selected", selected);
      row.setAttribute("aria-selected", String(selected));
    }
  }

  private onKeydown(e: KeyboardEvent): void {
    const entry = this.entryOf(e.target);
    if (!entry) return;
    const rows = this.rows();
    const at = rows.indexOf(e.target as HTMLElement);
    const focusRow = (row: HTMLElement | undefined): void => {
      if (!row) return;
      for (const r of rows) r.tabIndex = -1;
      row.tabIndex = 0;
      row.focus();
    };
    switch (e.key) {
      case "ArrowDown":
        focusRow(rows[at + 1]);
        break;
      case "ArrowUp":
        focusRow(rows[at - 1]);
        break;
      case "Home":
        focusRow(rows[0]);
        break;
      case "End":
        focusRow(rows[rows.length - 1]);
        break;
      case "ArrowRight":
        if (!entry.isDir || entry.leaf) return;
        if (!this.isOpen(entry)) this.toggle(entry, true);
        else focusRow(rows[at + 1]);
        break;
      case "ArrowLeft":
        if (entry.isDir && !entry.leaf && this.isOpen(entry)) {
          this.toggle(entry, false);
        } else {
          const parent = this.parentOf.get(entry.path);
          if (parent !== undefined) focusRow(this.rowFor(parent) ?? undefined);
        }
        break;
      case "Enter":
        if (this.options.select && this.options.select === (entry.isDir ? "dir" : "file")) this.choose(entry, true);
        else this.activate(entry);
        break;
      case " ":
        this.activate(entry);
        break;
      default:
        return;
    }
    e.preventDefault();
  }

  // --- drag to move (grocery/todo-style grip, pointer-driven) -------------------

  private dropFolderAt(x: number, y: number, dragged: TreeEntry): { folder: string; mark: HTMLElement } | null {
    const hit = document.elementFromPoint(x, y) as HTMLElement | null;
    if (!hit || !this.el.contains(hit)) return null;
    const root = this.options.rootPath ?? "";
    const target = this.entryOf(hit);
    let folder: string;
    if (!target) folder = root;
    else if (target.isDir) folder = target.path;
    else folder = this.parentOf.get(target.path) ?? root;
    // Not where it already is, and never into itself.
    if (folder === this.parentOf.get(dragged.path)) return null;
    if (dragged.isDir && (folder === dragged.path || contains(dragged.path, folder))) return null;
    const mark = folder === root ? this.el : (this.rowFor(folder) ?? this.el);
    return { folder, mark };
  }

  private startDrag(e: PointerEvent, grip: HTMLElement, row: HTMLElement, entry: TreeEntry): void {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    grip.setPointerCapture(e.pointerId);
    row.classList.add("is-dragging");
    let target: { folder: string; mark: HTMLElement } | null = null;
    const clear = (): void => {
      this.el.classList.remove("is-drop-target");
      this.el.querySelectorAll(".is-drop-target").forEach((n) => n.classList.remove("is-drop-target"));
    };
    const onMove = (ev: PointerEvent): void => {
      clear();
      target = this.dropFolderAt(ev.clientX, ev.clientY, entry);
      target?.mark.classList.add("is-drop-target");
    };
    const onEnd = (): void => {
      grip.removeEventListener("pointermove", onMove);
      grip.removeEventListener("pointerup", onEnd);
      grip.removeEventListener("pointercancel", onEnd);
      row.classList.remove("is-dragging");
      clear();
      if (target) this.options.move?.(entry, target.folder);
    };
    grip.addEventListener("pointermove", onMove);
    grip.addEventListener("pointerup", onEnd);
    grip.addEventListener("pointercancel", onEnd);
  }
}

/** Whether folder `dir` contains `path` (by path prefix; "/" contains all). */
function contains(dir: string, path: string): boolean {
  if (dir === "" || dir === "/") return path !== dir;
  return path.startsWith(dir.endsWith("/") ? dir : dir + "/");
}

// --- the picker ------------------------------------------------------------------

export interface TreePickerOptions {
  title: string;
  /** What is being chosen. */
  select: "file" | "dir";
  /** Loads folders on demand (`null` = the top level)… */
  load?: (dir: TreeEntry | null) => Promise<TreeEntry[]>;
  /** …or the whole tree up front. */
  entries?: TreeEntry[];
  /** Opened to and preselected. */
  startPath?: string;
  /** A line of guidance above the tree. */
  note?: string;
  /** The confirm button's label. Default "Choose". */
  confirmLabel?: string;
  emptyText?: string;
}

/**
 * A modal for choosing a file or folder from a tree. Resolves with the chosen
 * entry, or null if canceled. Double-click or Enter on an entry chooses it.
 */
export function openTreePicker(options: TreePickerOptions): Promise<TreeEntry | null> {
  return new Promise((resolve) => {
    let settled = false;
    const settle = (value: TreeEntry | null): void => {
      if (settled) return;
      settled = true;
      resolve(value);
    };

    const content = el("div", "ui-tree-picker");
    if (options.note) {
      const note = el("p", "ui-modal-message");
      note.textContent = options.note;
      content.append(note);
    }
    const pathBar = el("div", "ui-tree-picker-path");
    pathBar.textContent = "Nothing selected";
    const actions = el("div", "ui-modal-actions");
    const cancel = el("button", "ui-modal-btn");
    cancel.type = "button";
    cancel.textContent = "Cancel";
    const ok = el("button", "ui-modal-btn ui-modal-btn-primary");
    ok.type = "button";
    ok.textContent = options.confirmLabel ?? "Choose";
    ok.disabled = true;
    actions.append(cancel, ok);

    let modal: { close: () => void } | null = null;
    const finish = (entry: TreeEntry | null): void => {
      settle(entry);
      modal?.close();
    };

    const tree = new FileTree({
      label: options.title,
      select: options.select,
      load: options.load,
      entries: options.entries,
      emptyText: options.emptyText,
      onSelect: (entry) => {
        pathBar.textContent = entry.path;
        ok.disabled = false;
      },
      onOpen: (entry) => finish(entry),
    });
    tree.el.classList.add("ui-tree-picker-tree");
    content.append(pathBar, tree.el, actions);

    modal = openModal(content, { title: options.title, onClose: () => settle(null) });
    cancel.addEventListener("click", () => finish(null));
    ok.addEventListener("click", () => finish(tree.selected));
    if (options.startPath !== undefined) {
      void tree.reveal(options.startPath).then(() => tree.focus());
    }
  });
}
