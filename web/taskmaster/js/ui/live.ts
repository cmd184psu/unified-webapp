// live.ts — live/pause board-events controller + a surgical DOM list
// patcher.
//
// Global UI policy (see taskmaster-ui-FRD.md §8a): no manual "Refresh"
// buttons — a live/pause toggle instead, with (a) updates only happening
// when something actually changed (here: server push via SSE) and (b)
// patching only what changed, never a full repaint (the anti-jitter rule).
// Built self-contained so it can be lifted wholesale into a future shared
// UI layer (§8b) — no imports from any taskmaster app code.
//
// Usage:
//   const live = new LiveController();
//   live.onEvent((ev) => { ... });
//   live.setEnabled(true); // opens the EventSource
//
//   patchList(container, tasks, {
//     key: (t) => t.id,
//     create: (t) => renderTaskRow(t),
//     update: (el, t) => updateTaskRow(el, t),
//   });

const LIVE_ENABLED_KEY = "tm.live.enabled";
const LIVE_INTERVAL_KEY = "tm.live.interval";
const DEFAULT_INTERVAL_MS = 5000;
const BOARD_EVENTS_URL = "/api/board/events";
const BOARD_EVENT_NAME = "board";

/** Shape of the board-events SSE payload (mirrors worker.BoardEvent). */
export interface BoardEvent {
  type: string;
  lane?: string;
  task?: string;
  execution_id?: number;
  status?: string;
  engaged?: boolean;
}

type BoardEventListener = (ev: BoardEvent) => void;
type TickListener = () => void;
type EnabledListener = (enabled: boolean) => void;

function readStoredEnabled(): boolean {
  try {
    const raw = localStorage.getItem(LIVE_ENABLED_KEY);
    if (raw === null) return true; // live by default
    return raw === "true";
  } catch {
    return true;
  }
}

function readStoredInterval(): number {
  try {
    const raw = localStorage.getItem(LIVE_INTERVAL_KEY);
    const n = raw !== null ? Number(raw) : NaN;
    return Number.isFinite(n) && n > 0 ? n : DEFAULT_INTERVAL_MS;
  } catch {
    return DEFAULT_INTERVAL_MS;
  }
}

/**
 * Owns the board-events EventSource, a persisted live/pause flag, and a
 * persisted poll-interval preference (for a fallback poll cadence the
 * hamburger can set; the actual polling wiring is left to the caller).
 */
export class LiveController {
  private source: EventSource | null = null;
  private enabled: boolean;
  private intervalMs: number;
  private eventListeners = new Set<BoardEventListener>();
  private tickListeners = new Set<TickListener>();
  private enabledListeners = new Set<EnabledListener>();
  private readonly url: string;

  constructor(url: string = BOARD_EVENTS_URL) {
    this.url = url;
    this.enabled = readStoredEnabled();
    this.intervalMs = readStoredInterval();
    if (this.enabled) {
      this.open();
    }
  }

  /** Registers a listener invoked for every parsed board event. */
  onEvent(listener: BoardEventListener): () => void {
    this.eventListeners.add(listener);
    return () => this.eventListeners.delete(listener);
  }

  /** Registers a listener invoked on each fallback-poll tick. */
  onTick(listener: TickListener): () => void {
    this.tickListeners.add(listener);
    return () => this.tickListeners.delete(listener);
  }

  /** Registers a listener invoked whenever live/pause state changes. */
  onEnabledChange(listener: EnabledListener): () => void {
    this.enabledListeners.add(listener);
    return () => this.enabledListeners.delete(listener);
  }

  /** Fires all registered tick listeners. Callers own the actual timer. */
  tick(): void {
    for (const l of this.tickListeners) l();
  }

  isEnabled(): boolean {
    return this.enabled;
  }

  /** Turns the live stream on/off and persists the choice. */
  setEnabled(enabled: boolean): void {
    if (this.enabled === enabled) return;
    this.enabled = enabled;
    try {
      localStorage.setItem(LIVE_ENABLED_KEY, String(enabled));
    } catch {
      // ignore storage errors (private mode, quota, etc.)
    }
    if (enabled) {
      this.open();
    } else {
      this.closeSource();
    }
    for (const l of this.enabledListeners) l(enabled);
  }

  getInterval(): number {
    return this.intervalMs;
  }

  /** Sets the fallback poll interval (ms) and persists it. */
  setInterval(ms: number): void {
    if (!Number.isFinite(ms) || ms <= 0) return;
    this.intervalMs = ms;
    try {
      localStorage.setItem(LIVE_INTERVAL_KEY, String(ms));
    } catch {
      // ignore storage errors
    }
  }

  /** Closes the EventSource and releases all listeners. */
  destroy(): void {
    this.closeSource();
    this.eventListeners.clear();
    this.tickListeners.clear();
    this.enabledListeners.clear();
  }

  private open(): void {
    if (this.source) return;
    if (typeof EventSource === "undefined") return;
    const source = new EventSource(this.url);
    source.addEventListener(BOARD_EVENT_NAME, (e: MessageEvent) => {
      let parsed: BoardEvent | null = null;
      try {
        parsed = JSON.parse(e.data);
      } catch {
        return;
      }
      if (!parsed) return;
      for (const l of this.eventListeners) l(parsed);
    });
    this.source = source;
  }

  private closeSource(): void {
    if (this.source) {
      this.source.close();
      this.source = null;
    }
  }
}

export interface PatchListOptions<T> {
  /** Stable identity for a list item, used to match old/new DOM nodes. */
  key: (item: T) => string | number;
  /** Builds a brand-new DOM node for an item not currently rendered. */
  create: (item: T) => HTMLElement;
  /** Updates an existing DOM node in place to reflect the item's new data. */
  update: (el: HTMLElement, item: T) => void;
}

const KEY_ATTR = "data-tm-key";

/**
 * Surgically reconciles `container`'s children to match `items`, in order:
 * inserts nodes for new keys, removes nodes for keys no longer present, and
 * calls `update` in place (no re-creation) for keys that persist —
 * reordering existing DOM nodes rather than rebuilding them. Never resets
 * innerHTML, so scroll position, focus, and selection inside untouched rows
 * are preserved. This is the anti-jitter primitive backing the live/pause
 * + surgical-refresh UI policy.
 */
export function patchList<T>(
  container: HTMLElement,
  items: T[],
  opts: PatchListOptions<T>
): void {
  const existingByKey = new Map<string, HTMLElement>();
  for (const child of Array.from(container.children)) {
    const el = child as HTMLElement;
    const k = el.getAttribute(KEY_ATTR);
    if (k !== null) existingByKey.set(k, el);
  }

  const seenKeys = new Set<string>();
  let cursor: ChildNode | null = container.firstChild;

  for (const item of items) {
    const key = String(opts.key(item));
    seenKeys.add(key);

    let el = existingByKey.get(key);
    if (el) {
      opts.update(el, item);
    } else {
      el = opts.create(item);
      el.setAttribute(KEY_ATTR, key);
    }

    // Ensure `el` is at the current cursor position without disturbing
    // other untouched nodes.
    if (cursor !== el) {
      container.insertBefore(el, cursor);
    } else {
      cursor = cursor.nextSibling;
      continue;
    }
    cursor = el.nextSibling;
  }

  // Remove any nodes whose keys are no longer present.
  for (const [key, el] of existingByKey) {
    if (!seenKeys.has(key)) {
      el.remove();
    }
  }
}
