// live.ts — live/pause board-events controller.
//
// Global UI policy (see taskmaster-ui-FRD.md §8a): no manual "Refresh"
// buttons — a live/pause toggle instead, with (a) updates only happening
// when something actually changed (here: server push via SSE) and (b)
// patching only what changed, never a full repaint (the anti-jitter rule).
// Built self-contained so it can be lifted wholesale into a future shared
// UI layer (§8b) — no imports from any taskmaster app code.
//
// The surgical DOM list patcher (patchList/PatchListOptions) that used to
// live here has moved to web/shared/ts/patchlist.ts (plan
// docs/PLAN-utuber-taskmaster-lane.md §4.11), imported via "@shared" —
// utuber needs the same primitive, and this file no longer defines it.
//
// Usage:
//   const live = new LiveController();
//   live.onEvent((ev) => { ... });
//   live.setEnabled(true); // opens the EventSource

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
  // ProgressPct/ProgressLabel carry a "task-progress" event's payload
  // (func-task progress); unset for every other event type.
  progress_pct?: number | null;
  progress_label?: string;
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
