// queuepanel.ts — the shared queue panel (plan
// docs/PLAN-utuber-taskmaster-lane.md §4.11, decision D10).
//
// A generic "Running / Up next / Recent" list, driven entirely by an
// adapter over an item type T — no module names anywhere in this file. The
// taskmaster lane board (board.ts) is the first consumer; the utuber page
// is the second (Phase 5). Rows are patched via patchList, never rebuilt
// via innerHTML, so scroll position/focus survive a re-render.

import { patchList } from "./patchlist.js";

export type QueueSection = "running" | "upnext" | "recent";
export type QueueActionKind = "cancel" | "pause" | "resume" | "rerun" | "remove";

export interface QueueProgress {
  pct: number | null;
  label: string;
}

export interface QueuePanelAdapter<T> {
  key(item: T): string;
  /** null means "not rendered in this panel". */
  section(item: T): QueueSection | null;
  title(item: T): string;
  /** e.g. 'running' | 'queued' | 'success' ... */
  status(item: T): string;
  /** A progress bar is shown only when this returns non-null (FR-T3). */
  progress?(item: T): QueueProgress | null;
  meta?(item: T): string;
  actions(item: T): QueueActionKind[];
  onAction(action: QueueActionKind, item: T, button: HTMLButtonElement): void | Promise<void>;
  /** Default: the shared symbol badge (ui-queue-badge). */
  renderBadge?(badge: HTMLElement, item: T): void;
  /** Module extras: links, pid, drag handles, etc. */
  decorate?(row: HTMLElement, item: T, created: boolean): void;
  onTitleClick?(item: T): void;
}

export interface QueuePanelOptions {
  /** Defaults: Running / Up next / Recent. */
  titles?: Partial<Record<QueueSection, string>>;
  /** Defaults: 'nothing running' / 'queue is empty' / 'no history yet'. */
  emptyText?: Partial<Record<QueueSection, string>>;
  /** Default Infinity. */
  recentLimit?: number;
  /** Default true (<details>). */
  recentCollapsible?: boolean;
  /** Default false. */
  recentOpen?: boolean;
  /** Default null. */
  header?: { paused: boolean; note?: string; onTogglePause(): void | Promise<void> } | null;
}

const DEFAULT_TITLES: Record<QueueSection, string> = {
  running: "Running",
  upnext: "Up next",
  recent: "Recent",
};

const DEFAULT_EMPTY: Record<QueueSection, string> = {
  running: "nothing running",
  upnext: "queue is empty",
  recent: "no history yet",
};

const SECTIONS: QueueSection[] = ["running", "upnext", "recent"];

// cancel '✖' Cancel · pause '⏸' Pause · resume '▶' Resume · rerun '↻' Re-run · remove '🗑' Remove
export const ACTION_GLYPHS: Record<QueueActionKind, { glyph: string; label: string }> = {
  cancel: { glyph: "✖", label: "Cancel" },
  pause: { glyph: "⏸", label: "Pause" },
  resume: { glyph: "▶", label: "Resume" },
  rerun: { glyph: "↻", label: "Re-run" },
  remove: { glyph: "🗑", label: "Remove" },
};

/** Pure: groups items into their section buckets, in input order, applying recentLimit. */
export function bucketQueue<T>(
  items: T[],
  section: (t: T) => QueueSection | null,
  recentLimit?: number
): Record<QueueSection, T[]> {
  const out: Record<QueueSection, T[]> = { running: [], upnext: [], recent: [] };
  for (const item of items) {
    const sec = section(item);
    if (sec === null) continue;
    out[sec].push(item);
  }
  if (recentLimit !== undefined && Number.isFinite(recentLimit) && recentLimit >= 0) {
    out.recent = out.recent.slice(0, recentLimit);
  }
  return out;
}

/** Pure: resolves a QueueProgress into the primitives the row renderer needs. */
export function progressView(p: QueueProgress | null | undefined): {
  show: boolean;
  pct: number;
  indeterminate: boolean;
  label: string;
} {
  if (!p) return { show: false, pct: 0, indeterminate: false, label: "" };
  if (p.pct === null) return { show: true, pct: 0, indeterminate: true, label: p.label ?? "" };
  const clamped = Math.max(0, Math.min(100, p.pct));
  return { show: true, pct: clamped, indeterminate: false, label: p.label ?? "" };
}

const EMPTY_KEY = "__empty__";

export class QueuePanel<T> {
  private readonly host: HTMLElement;
  private readonly adapter: QueuePanelAdapter<T>;
  private readonly opts: QueuePanelOptions;
  private readonly root: HTMLElement;
  private readonly lists: Record<QueueSection, HTMLElement>;
  private headerNoteEl: HTMLElement | null = null;
  private headerToggleBtn: HTMLButtonElement | null = null;

  constructor(host: HTMLElement, adapter: QueuePanelAdapter<T>, opts: QueuePanelOptions = {}) {
    this.host = host;
    this.adapter = adapter;
    this.opts = opts;

    this.root = document.createElement("div");
    this.root.className = "ui-queue";

    if (opts.header) {
      this.root.appendChild(this.buildHeader(opts.header));
    }

    this.lists = { running: document.createElement("div"), upnext: document.createElement("div"), recent: document.createElement("div") };

    for (const sec of SECTIONS) {
      this.root.appendChild(this.buildSection(sec));
    }

    this.host.appendChild(this.root);
  }

  private buildHeader(header: { paused: boolean; note?: string; onTogglePause(): void | Promise<void> }): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "ui-queue-header";

    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "ui-queue-header-toggle";
    btn.addEventListener("click", () => void header.onTogglePause());
    wrap.appendChild(btn);
    this.headerToggleBtn = btn;

    const note = document.createElement("span");
    note.className = "ui-queue-header-note";
    wrap.appendChild(note);
    this.headerNoteEl = note;

    this.applyHeaderState(header.paused, header.note);
    return wrap;
  }

  private applyHeaderState(paused: boolean, note?: string): void {
    if (this.headerToggleBtn) {
      this.headerToggleBtn.textContent = paused ? "▶" : "⏸";
      this.headerToggleBtn.title = paused ? "Resume queue" : "Pause queue";
      this.headerToggleBtn.setAttribute("aria-label", this.headerToggleBtn.title);
      this.headerToggleBtn.disabled = !!note;
    }
    if (this.headerNoteEl) {
      this.headerNoteEl.textContent = note ?? "";
      this.headerNoteEl.hidden = !note;
    }
  }

  /** Updates the header pause state and optional note (e.g. "Paused by taskmaster hand brake"). */
  setPaused(paused: boolean, note?: string): void {
    this.applyHeaderState(paused, note);
  }

  private buildSection(sec: QueueSection): HTMLElement {
    const title = this.opts.titles?.[sec] ?? DEFAULT_TITLES[sec];
    const collapsible = sec === "recent" && (this.opts.recentCollapsible ?? true);

    const wrap = document.createElement(collapsible ? "details" : "div");
    wrap.className = "ui-queue-section ui-queue-section-" + sec;
    if (collapsible) (wrap as HTMLDetailsElement).open = this.opts.recentOpen ?? false;

    const heading = document.createElement(collapsible ? "summary" : "div");
    heading.className = "ui-queue-title";
    heading.textContent = title;
    wrap.appendChild(heading);

    const list = this.lists[sec];
    list.className = "ui-queue-list ui-queue-list-" + sec;
    wrap.appendChild(list);

    return wrap;
  }

  /** Within a section, input order is preserved. */
  update(items: T[]): void {
    const buckets = bucketQueue(items, (t) => this.adapter.section(t), this.opts.recentLimit);
    for (const sec of SECTIONS) {
      this.renderSection(sec, buckets[sec]);
    }
  }

  private renderSection(sec: QueueSection, items: T[]): void {
    const list = this.lists[sec];
    patchList(list, items, {
      key: (item) => this.adapter.key(item),
      create: (item) => this.createRow(item),
      update: (row, item) => this.updateRow(row, item),
    });
    this.toggleEmptyNote(list, items.length === 0, this.opts.emptyText?.[sec] ?? DEFAULT_EMPTY[sec]);
  }

  private toggleEmptyNote(list: HTMLElement, empty: boolean, text: string): void {
    let note = list.querySelector<HTMLElement>(".ui-queue-empty-note");
    if (empty) {
      if (!note) {
        note = document.createElement("div");
        note.className = "ui-queue-empty-note";
        note.setAttribute("data-ui-key", EMPTY_KEY);
        list.appendChild(note);
      }
      note.textContent = text;
    } else {
      note?.remove();
    }
  }

  private createRow(item: T): HTMLElement {
    const row = document.createElement("div");
    row.className = "ui-queue-row";

    const name = document.createElement("span");
    name.className = "ui-queue-name";
    row.appendChild(name);

    const badge = document.createElement("span");
    badge.className = "ui-queue-badge";
    row.appendChild(badge);

    const meta = document.createElement("span");
    meta.className = "ui-queue-meta";
    row.appendChild(meta);

    const progressWrap = document.createElement("div");
    progressWrap.className = "ui-queue-progress";
    const progressBar = document.createElement("div");
    progressBar.className = "ui-queue-progress-bar";
    const progressLabel = document.createElement("span");
    progressLabel.className = "ui-queue-progress-label";
    progressWrap.append(progressBar, progressLabel);
    row.appendChild(progressWrap);

    const actions = document.createElement("div");
    actions.className = "ui-queue-actions";
    row.appendChild(actions);

    this.updateRow(row, item, true);
    return row;
  }

  private updateRow(row: HTMLElement, item: T, created = false): void {
    const nameEl = row.querySelector<HTMLElement>(".ui-queue-name");
    if (nameEl) {
      nameEl.textContent = this.adapter.title(item);
      if (this.adapter.onTitleClick) {
        nameEl.classList.add("ui-queue-name-clickable");
        nameEl.setAttribute("role", "button");
        nameEl.tabIndex = 0;
        const onClick = (): void => this.adapter.onTitleClick?.(item);
        nameEl.onclick = onClick;
        nameEl.onkeydown = (e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onClick();
          }
        };
      }
    }

    const badgeEl = row.querySelector<HTMLElement>(".ui-queue-badge");
    if (badgeEl) {
      if (this.adapter.renderBadge) {
        this.adapter.renderBadge(badgeEl, item);
      } else {
        const status = this.adapter.status(item);
        badgeEl.className = "ui-queue-badge ui-queue-badge-" + status;
        badgeEl.textContent = status;
        badgeEl.title = status;
        badgeEl.setAttribute("aria-label", status);
      }
    }

    const metaEl = row.querySelector<HTMLElement>(".ui-queue-meta");
    if (metaEl) metaEl.textContent = this.adapter.meta ? this.adapter.meta(item) : "";

    const progressWrap = row.querySelector<HTMLElement>(".ui-queue-progress");
    const progressBar = row.querySelector<HTMLElement>(".ui-queue-progress-bar");
    const progressLabelEl = row.querySelector<HTMLElement>(".ui-queue-progress-label");
    const pv = progressView(this.adapter.progress ? this.adapter.progress(item) : null);
    if (progressWrap) progressWrap.hidden = !pv.show;
    if (progressBar) {
      progressBar.classList.toggle("ui-queue-progress-indeterminate", pv.indeterminate);
      progressBar.style.width = pv.indeterminate ? "" : pv.pct + "%";
      progressBar.setAttribute("role", "progressbar");
      progressBar.setAttribute("aria-valuemin", "0");
      progressBar.setAttribute("aria-valuemax", "100");
      if (pv.indeterminate) {
        progressBar.removeAttribute("aria-valuenow");
      } else {
        progressBar.setAttribute("aria-valuenow", String(pv.pct));
      }
      const ariaLabel = pv.label || (pv.indeterminate ? "in progress" : pv.pct + "%");
      progressBar.setAttribute("aria-label", ariaLabel);
    }
    if (progressLabelEl) progressLabelEl.textContent = pv.label;

    const actionsEl = row.querySelector<HTMLElement>(".ui-queue-actions");
    if (actionsEl) {
      actionsEl.textContent = "";
      for (const kind of this.adapter.actions(item)) {
        const glyph = ACTION_GLYPHS[kind];
        const btn = document.createElement("button");
        btn.type = "button";
        btn.className = "ui-queue-action ui-queue-action-" + kind;
        btn.textContent = glyph.glyph;
        btn.title = glyph.label;
        btn.setAttribute("aria-label", glyph.label);
        btn.addEventListener("click", () => void this.adapter.onAction(kind, item, btn));
        actionsEl.appendChild(btn);
      }
    }

    if (this.adapter.decorate) this.adapter.decorate(row, item, created);
  }

  list(section: QueueSection): HTMLElement {
    return this.lists[section];
  }

  destroy(): void {
    this.root.remove();
  }
}
