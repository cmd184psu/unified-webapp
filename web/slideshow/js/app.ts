import { ThemeManager, HamburgerMenu } from '/shared/dist/shared.mjs';

export {};

// ── Types ─────────────────────────────────────────────────────────────────────
interface MusicInfo {
  name: string;
  tracks: string[];
}

type CardName = "visual" | "audio";

/** A control card's placement (see internal/slideshow CardLayout). */
interface CardLayout {
  position: string;                 // one of CARD_POSITIONS
  drag?: { x: number; y: number };  // screen fractions; overrides position
  minimized: boolean;
}

interface SlideshowState {
  subject: string;
  image_path: string;
  image_index: number;
  subject_index: number;
  total_images: number;
  total_subjects: number;
  mode: "kenburns" | "panscan" | "static";
  playing: boolean;
  shuffle: boolean;
  interval_seconds: number;
  max_age_days: number;       // skip images older than this; 0 = no limit
  theme: string;
  visual_card: CardLayout;
  audio_card: CardLayout;
  music_enabled: boolean;
  music_collection: number;
  music_collections?: MusicInfo[];
  server_started?: string;
}

// ── Pan & Scan engine ─────────────────────────────────────────────────────────
//
// Design principles:
//
//   1. Time tracking via `elapsed` + `t0`, not back-computed from progress.
//      `elapsed` accumulates across pause/resume cycles.  `t0` is the wall
//      clock when the current run started.  At any moment:
//          totalMs = elapsed + (playing ? now - t0 : 0)
//
//   2. Direction is determined from the image element's own rendered
//      dimensions (offsetWidth/offsetHeight), not window.inner*, so it's
//      correct even when 100dvh ≠ window.innerHeight (iOS Safari toolbar).
//
//   3. State machine: activate → [wait for load] → go ↔ cancel.
//      Only one RAF is ever live at a time; guarded by `this.raf === 0`
//      before calling go().
//
//   4. `loaded()` gates any DOM or animation work that needs naturalWidth.
//      After img.src changes, complete becomes false and naturalWidth = 0,
//      so loaded() returns false until the new image finishes decoding.

class PanScan {
  private readonly el: HTMLImageElement;
  private raf = 0;
  private t0 = 0;            // performance.now() when current run started
  private elapsed = 0;       // ms accumulated before current run
  private duration = 8000;   // ms for full pan (set by activate)
  private dir: "v" | "h" = "v";
  private on = false;
  private playing = false;
  private generation = 0;    // incremented on every activate(); guards stale load events

  constructor(el: HTMLImageElement) {
    this.el = el;
    el.addEventListener("load",    () => this.onLoad());
    window.addEventListener("resize", () => this.onResize());
  }

  // Called when entering panscan mode or when the image changes in panscan mode.
  // Always resets the pan to position 0.
  activate(secs: number): void {
    this.cancel();           // stop any running animation, snapshot elapsed
    this.on         = true;
    this.elapsed    = 0;     // fresh start
    this.duration   = secs * 1000;
    this.generation++;       // invalidate any in-flight load event from the previous image
    this.el.style.objectFit = "cover";
    if (this.loaded()) {
      this.detect();
      this.paint(0);
      if (this.playing) this.go();
    }
    // If image not loaded yet: onLoad() will call detect/paint/go.
  }

  // Called when leaving panscan mode.
  deactivate(): void {
    this.cancel();
    this.on = false;
    this.el.style.removeProperty("object-fit");
    this.el.style.removeProperty("object-position");
  }

  // Called whenever the conductor playing state changes.
  setPlaying(playing: boolean): void {
    if (this.playing === playing) return;   // idempotent
    this.playing = playing;
    if (!this.on) return;
    if (playing && this.loaded() && !this.raf) this.go();
    else if (!playing) this.cancel();
  }

  // ── Private ──────────────────────────────────────────────────────────────────

  private onLoad(): void {
    if (!this.on) return;
    // Capture generation at the moment this load fired.  If activate() has
    // been called again since (navigation to another image), the generation
    // will have incremented and this load event is stale — ignore it.
    const gen = this.generation;
    this.detect();
    this.paint(0);  // always start from position 0 on a fresh image load
    if (this.playing && !this.raf && gen === this.generation) this.go();
  }

  private onResize(): void {
    if (!this.on || !this.loaded()) return;
    this.detect();
    this.paint(this.pos());
  }

  // Start (or restart) the RAF loop from the current elapsed position.
  private go(): void {
    const gen = this.generation;
    this.t0 = performance.now();
    const tick = (now: number): void => {
      // If activate() was called again while this loop was pending, stop.
      if (gen !== this.generation) { this.raf = 0; return; }
      const p = Math.min((this.elapsed + now - this.t0) / this.duration, 1);
      this.paint(p);
      if (p < 1) {
        this.raf = requestAnimationFrame(tick);
      } else {
        this.raf = 0;  // animation complete; hold at end position
      }
    };
    this.raf = requestAnimationFrame(tick);
  }

  // Stop the RAF loop and snapshot elapsed time so we can resume correctly.
  private cancel(): void {
    if (this.raf) {
      this.elapsed = Math.min(this.elapsed + performance.now() - this.t0, this.duration);
      cancelAnimationFrame(this.raf);
      this.raf = 0;
    }
  }

  // Current progress 0..1, usable whether running or paused.
  private pos(): number {
    const ms = this.raf ? this.elapsed + performance.now() - this.t0 : this.elapsed;
    return Math.min(ms / this.duration, 1);
  }

  // Use the element's own rendered size, not window.inner*, so the ratio is
  // correct even when 100dvh ≠ window.innerHeight (iOS Safari with toolbar).
  private detect(): void {
    const iw = this.el.naturalWidth;
    const ih = this.el.naturalHeight;
    const cw = this.el.offsetWidth  || window.innerWidth;
    const ch = this.el.offsetHeight || window.innerHeight;
    // image wider than container → cover overflows horizontally → pan h
    // image taller than container → cover overflows vertically   → pan v
    this.dir = (iw / ih) > (cw / ch) ? "h" : "v";
  }

  private paint(progress: number): void {
    const pct = (cubicEaseInOut(progress) * 100).toFixed(2) + "%";
    this.el.style.objectPosition = this.dir === "v" ? `50% ${pct}` : `${pct} 50%`;
  }

  private loaded(): boolean {
    return this.el.complete && this.el.naturalWidth > 0;
  }

  // Exposed for debug display only.
  getProgress(): number { return this.pos(); }
  getDirection(): "v" | "h" { return this.dir; }
  isActive(): boolean { return this.on; }
}

// Cubic ease-in-out: slow at both ends, fast in the middle.
function cubicEaseInOut(t: number): number {
  return t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2;
}

// ── Debug timer ───────────────────────────────────────────────────────────────
// Tracks time-playing-since-last-image to match the conductor's internal ticker.
// Paused time is excluded so the countdown stays in sync with the server.
class DebugTimer {
  private readonly el: HTMLElement;
  private raf = 0;
  private elapsed = 0;        // ms spent playing since last image
  private runStart = 0;       // performance.now() when current run started
  private running = false;
  private intervalMs = 8000;
  private mode = "";

  constructor(el: HTMLElement) { this.el = el; }

  // Called when a new image arrives.
  resetImage(intervalSecs: number, mode: string): void {
    this.elapsed    = 0;
    this.intervalMs = intervalSecs * 1000;
    this.mode       = mode;
    if (this.running) this.runStart = performance.now();
  }

  // Called whenever mode or interval changes without a new image.
  setMeta(intervalSecs: number, mode: string): void {
    this.intervalMs = intervalSecs * 1000;
    this.mode       = mode;
  }

  setPlaying(playing: boolean): void {
    if (playing && !this.running) {
      this.running  = true;
      this.runStart = performance.now();
      if (!this.raf) this.tick();
    } else if (!playing && this.running) {
      this.elapsed += performance.now() - this.runStart;
      this.running  = false;
    }
  }

  show(visible: boolean): void {
    this.el.hidden = !visible;
    if (visible && !this.raf) this.tick();
    else if (!visible) { cancelAnimationFrame(this.raf); this.raf = 0; }
  }

  private tick = (): void => {
    if (this.el.hidden) { this.raf = 0; return; }
    const total   = this.running
      ? this.elapsed + performance.now() - this.runStart
      : this.elapsed;
    const remaining = Math.max(0, this.intervalMs - total) / 1000;
    const pan       = panScan.isActive() ? panScan.getProgress() : -1;
    const dir       = panScan.isActive() ? (panScan.getDirection() === "v" ? "↕" : "↔") : "";

    let text = `⏱ ${remaining.toFixed(1)}s`;
    if (!this.running) text += " ⏸";
    if (pan >= 0)      text += `  ${dir}pan ${Math.round(pan * 100)}%`;
    this.el.textContent = text;

    this.raf = requestAnimationFrame(this.tick);
  };
}

// ── DOM refs ──────────────────────────────────────────────────────────────────
const display        = document.getElementById("display")          as HTMLDivElement;
const img            = document.getElementById("slide-img")        as HTMLImageElement;
const subjectLabel   = document.getElementById("subject-label")    as HTMLSpanElement;
const imgCounter     = document.getElementById("image-counter")    as HTMLDivElement;
const btnPlayPause   = document.getElementById("btn-play-pause")   as HTMLButtonElement;
const btnPrev        = document.getElementById("btn-prev")         as HTMLButtonElement;
const btnNext        = document.getElementById("btn-next")         as HTMLButtonElement;
const btnPrevSubj    = document.getElementById("btn-prev-subject") as HTMLButtonElement;
const btnNextSubj    = document.getElementById("btn-next-subject") as HTMLButtonElement;
const btnHamburger   = document.getElementById("btn-hamburger")    as HTMLButtonElement;
const cardVisual     = document.getElementById("card-visual")      as HTMLDivElement;
const cardAudio      = document.getElementById("card-audio")       as HTMLDivElement;
const musicLabel     = document.getElementById("music-label")      as HTMLSpanElement;
const btnMusicStop   = document.getElementById("btn-music-stop")   as HTMLButtonElement;
const btnMusicPlay   = document.getElementById("btn-music-play")   as HTMLButtonElement;
const btnMusicNext   = document.getElementById("btn-music-next")   as HTMLButtonElement;
const audioEl        = document.getElementById("audio-player")     as HTMLAudioElement;
const debugDisplayEl = document.getElementById("debug-display")    as HTMLSpanElement;

// ── Settings controls (mounted into the shared ☰ drawer below) ────────────────
function el<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function settingRow(label: string, control: HTMLElement): HTMLLabelElement {
  const row = el("label", "setting-row");
  row.append(label, control);
  return row;
}

function settingToggle(label: string, input: HTMLInputElement): HTMLLabelElement {
  input.type = "checkbox";
  const row = el("label", "ui-toggle");
  row.append(input, el("span", "ui-toggle-track"), label);
  return row;
}

const selMode = el("select");
for (const [value, text] of [["kenburns", "Ken Burns"], ["panscan", "Pan & Scan"], ["static", "Static"]]) {
  const opt = el("option");
  opt.value = value;
  opt.textContent = text;
  selMode.append(opt);
}
const inpInterval = el("input");
inpInterval.type = "number";
inpInterval.min = "1";
inpInterval.max = "300";
const inpMaxAge = el("input");
inpMaxAge.type = "number";
inpMaxAge.min = "0";
inpMaxAge.max = "36500";
inpMaxAge.title = "Skip images whose file is older than this many days. 0 = no limit.";
const ageHint = el("p", "setting-hint");
ageHint.textContent = "Skips images whose file is older than this. 0 = no limit.";
const chkShuffle = el("input");
const chkDebug = el("input");
const serverStampEl = el("div", "server-stamp");

const CARD_POSITIONS = ["top-left", "top", "top-right", "left", "right", "bottom-left", "bottom", "bottom-right"];
const GRID_CELLS = ["top-left", "top", "top-right", "left", "", "right", "bottom-left", "bottom", "bottom-right"];

/** A 3×3 position picker: eight clickable squares, the center disabled. */
function positionPicker(card: CardName, label: string): { root: HTMLElement; sync: (pos: string) => void } {
  const root = el("div", "pos-picker");
  const title = el("span", "pos-picker-label");
  title.textContent = label;
  const grid = el("div", "pos-grid");
  grid.setAttribute("role", "radiogroup");
  grid.setAttribute("aria-label", label);
  const cells: HTMLButtonElement[] = [];
  for (const pos of GRID_CELLS) {
    const cell = el("button", "pos-cell");
    cell.type = "button";
    if (!pos) {
      cell.disabled = true;
      cell.classList.add("pos-center");
      cell.setAttribute("aria-hidden", "true");
    } else {
      cell.dataset.pos = pos;
      cell.title = pos.replace("-", " ");
      cell.setAttribute("role", "radio");
      cell.setAttribute("aria-label", pos.replace("-", " "));
      cell.addEventListener("click", () => {
        const el = card === "visual" ? cardVisual : cardAudio;
        expectLayout(el, (l) => !l.drag && l.position === pos);
        el.dataset.pos = pos;               // snap at once
        el.style.left = "";
        el.style.top = "";
        control("set-card-position", { card, position: pos });
      });
    }
    cells.push(cell);
    grid.append(cell);
  }
  root.append(title, grid);
  return {
    root,
    sync: (pos) => {
      for (const c of cells) {
        const on = c.dataset.pos === pos;
        c.classList.toggle("is-active", on);
        if (c.dataset.pos) c.setAttribute("aria-checked", on ? "true" : "false");
      }
    },
  };
}

const visualPicker = positionPicker("visual", "Image controls position");
const audioPicker = positionPicker("audio", "Audio controls position");

// ── Theme ────────────────────────────────────────────────────────────────────
const themes = new ThemeManager({
  module: 'slideshow',
  default: 'dark',
  onChange: (name: string) => {
    control('set-theme', name);
  },
});
themes.apply();

// ── Settings drawer (the shared ☰ menu, opening beside its top-right trigger) ─
new HamburgerMenu({
  title: "Settings",
  side: "right",
  mountTrigger: btnHamburger,
  themePicker: true,
  themes,
  items: [
    {
      id: "playback",
      render: (host: HTMLElement) => {
        host.append(
          settingRow("Mode", selMode),
          settingRow("Seconds per image", inpInterval),
          settingRow("Image age limit (days)", inpMaxAge),
          ageHint,
          settingToggle("Shuffle subjects", chkShuffle),
        );
      },
    },
    { section: "Controls" },
    {
      id: "positions",
      render: (host: HTMLElement) => {
        const hint = el("p", "setting-hint");
        hint.textContent = "Pick a square to snap a card there; drag a card by its grip to place it anywhere.";
        host.append(visualPicker.root, audioPicker.root, hint);
      },
    },
    { section: "Other" },
    {
      id: "misc",
      render: (host: HTMLElement) => {
        const help = el("div", "shortcuts-help");
        help.innerHTML =
          "<h4>Keyboard shortcuts</h4><dl>" +
          "<dt><kbd>&#8592;</kbd> <kbd>&#8594;</kbd></dt><dd>Prev / next image</dd>" +
          "<dt>Click the image</dt><dd>Next image</dd>" +
          "<dt><kbd>Space</kbd></dt><dd>Play / pause slideshow</dd>" +
          "<dt><kbd>Enter</kbd></dt><dd>Play / stop music</dd>" +
          "<dt><kbd>Esc</kbd></dt><dd>Close this menu</dd></dl>";
        host.append(settingToggle("Debug timer", chkDebug), serverStampEl, help);
      },
    },
  ],
});

// ── Module state ──────────────────────────────────────────────────────────────
const panScan   = new PanScan(img);
const debugTimer = new DebugTimer(debugDisplayEl);
let currentState: SlideshowState | null = null;
let kburnsTick = false;
let prevMusicCollection = -1;
let musicUserPaused = true;   // music never auto-starts; user must press play
let currentTrackIndex = 0;

// ── SSE ───────────────────────────────────────────────────────────────────────
function connectSSE(): void {
  const es = new EventSource("/api/events");
  es.addEventListener("state", (e: MessageEvent) => {
    try { applyState(JSON.parse(e.data) as SlideshowState); } catch { /* ignore */ }
  });
  es.onerror = () => { es.close(); setTimeout(connectSSE, 3000); };
}

// ── Apply server state to DOM ─────────────────────────────────────────────────
function applyState(state: SlideshowState): void {
  const prev = currentState;
  currentState = state;

  themes.set(state.theme);
  layoutCard(cardVisual, state.visual_card);
  layoutCard(cardAudio, state.audio_card);
  visualPicker.sync(state.visual_card?.drag ? "" : state.visual_card?.position ?? "");
  audioPicker.sync(state.audio_card?.drag ? "" : state.audio_card?.position ?? "");

  const imageChanged    = !prev || prev.image_path       !== state.image_path;
  const modeChanged     = !prev || prev.mode             !== state.mode;
  const intervalChanged = !prev || prev.interval_seconds !== state.interval_seconds;

  if (imageChanged) {
    // Setting img.src causes complete → false and naturalWidth → 0 immediately,
    // so PanScan.loaded() will return false until the new image is decoded.
    img.src = state.image_path ? `/slides/${state.image_path}` : "";
    applyModeClass(state.mode, state.interval_seconds);
    debugTimer.resetImage(state.interval_seconds, state.mode);
  } else if (modeChanged || intervalChanged) {
    applyModeClass(state.mode, state.interval_seconds);
    debugTimer.setMeta(state.interval_seconds, state.mode);
  }

  // Update pan & scan engine playing state every tick (handles play/pause).
  panScan.setPlaying(state.playing);
  debugTimer.setPlaying(state.playing);

  // CSS animation pause (kenburns uses animation-play-state via this class).
  display.classList.toggle("paused", !state.playing);

  subjectLabel.textContent = state.subject
    ? `${state.subject} (${state.subject_index + 1}/${state.total_subjects})`
    : "";
  imgCounter.textContent = state.total_images > 0
    ? `${state.image_index + 1} / ${state.total_images}`
    : "";
  btnPlayPause.textContent = state.playing ? "⏸" : "▶";
  btnPlayPause.title       = state.playing ? "Pause" : "Play";

  selMode.value        = state.mode;
  inpInterval.value    = String(state.interval_seconds);
  if (document.activeElement !== inpMaxAge) inpMaxAge.value = String(state.max_age_days ?? 0);
  chkShuffle.checked   = state.shuffle;

  applyMusicState(state);

  if (state.server_started && serverStampEl.textContent !== state.server_started) {
    serverStampEl.textContent = `started ${state.server_started}`;
  }
}

function applyModeClass(mode: SlideshowState["mode"], intervalSecs: number): void {
  img.classList.remove("mode-static", "mode-panscan", "mode-kenburns-a", "mode-kenburns-b");
  img.style.setProperty("--interval", `${intervalSecs}s`);

  if (mode === "panscan") {
    img.classList.add("mode-panscan");
    panScan.activate(intervalSecs);
  } else {
    panScan.deactivate();
    if (mode === "static") {
      img.classList.add("mode-static");
    } else {
      // Alternate class names to force CSS animation restart on each image.
      kburnsTick = !kburnsTick;
      img.classList.add(kburnsTick ? "mode-kenburns-a" : "mode-kenburns-b");
    }
  }
}

// ── Music ─────────────────────────────────────────────────────────────────────
audioEl.addEventListener("ended", () => {
  if (musicUserPaused) return;   // user stopped music; don't advance
  const state = currentState;
  if (!state?.music_collections?.length) return;
  const coll = state.music_collections[state.music_collection];
  if (!coll) return;
  currentTrackIndex = (currentTrackIndex + 1) % coll.tracks.length;
  loadAndPlay(coll);
});

function applyMusicState(state: SlideshowState): void {
  const colls = state.music_collections ?? [];
  const hasMusic = state.music_enabled && colls.length > 0;
  cardAudio.hidden = !hasMusic;
  if (!hasMusic) return;

  const coll = colls[state.music_collection];
  musicLabel.textContent = coll?.name || "Audio";

  if (state.music_collection !== prevMusicCollection) {
    prevMusicCollection = state.music_collection;
    currentTrackIndex = 0;
    if (coll) {
      setTrack(coll);                               // always prime the src
      if (!musicUserPaused) audioEl.play().catch(() => {}); // resume only if user was playing
    }
  }
}

// Load the current track into the audio element without playing.
// Called whenever the collection changes so the ▶ button works immediately.
function setTrack(coll: MusicInfo): void {
  if (!coll.tracks.length) return;
  if (currentTrackIndex >= coll.tracks.length) currentTrackIndex = 0;
  audioEl.src = `/audio/${coll.tracks[currentTrackIndex]}`;
}

// Load and play — only called when the user has explicitly started playback.
function loadAndPlay(coll: MusicInfo): void {
  setTrack(coll);
  audioEl.play().catch(() => {});
}

btnMusicStop.addEventListener("click", () => {
  audioEl.pause();
  audioEl.currentTime = 0;
  musicUserPaused = true;
});
btnMusicPlay.addEventListener("click",  () => { musicUserPaused = false; audioEl.play().catch(() => {}); });
btnMusicNext.addEventListener("click",  () => control("music-next"));

// ── Control API ───────────────────────────────────────────────────────────────
function control(action: string, value?: unknown): void {
  const body: Record<string, unknown> = { action };
  if (value !== undefined) body["value"] = value;
  fetch("/api/control", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).catch(() => {});
}

// ── Button listeners ──────────────────────────────────────────────────────────
btnPlayPause.addEventListener("click", () => control(currentState?.playing ? "pause" : "play"));
btnPrev.addEventListener("click",      () => control("prev"));
btnNext.addEventListener("click",      () => control("next"));
btnPrevSubj.addEventListener("click",  () => control("prev-subject"));
btnNextSubj.addEventListener("click",  () => control("next-subject"));

// ── Settings ──────────────────────────────────────────────────────────────────
chkDebug.addEventListener("change", () => debugTimer.show(chkDebug.checked));

selMode.addEventListener("change",    () => control("set-mode",    selMode.value));
chkShuffle.addEventListener("change", () => control("set-shuffle", chkShuffle.checked));
inpMaxAge.addEventListener("change", () => {
  const v = parseInt(inpMaxAge.value, 10);
  if (!isNaN(v) && v >= 0) control("set-max-age", v);
});
inpInterval.addEventListener("change", () => {
  const v = parseInt(inpInterval.value, 10);
  if (!isNaN(v) && v >= 1) control("set-interval", v);
});

// ── Control cards: placement, drag, minimize ─────────────────────────────────
// A card is either snapped (data-pos = one of the 8 positions; CSS places it)
// or dragged (data-pos="free"; placed here from screen fractions so the same
// layout lands in the same relative spot on every screen).
const cardLayouts = new Map<HTMLElement, CardLayout>();
let draggingCard: HTMLElement | null = null;

// After a local change (drop, minimize, snap), state updates the server sent
// before it saved that change would snap the card back ("hiccup"). So each
// local change records what the server's layout should now look like, and
// that card ignores layouts that don't match yet, for up to 3 seconds (in
// case the save failed, the server's view then wins).
const pendingLayout = new Map<HTMLElement, { matches: (l: CardLayout) => boolean; until: number }>();

function expectLayout(card: HTMLElement, matches: (l: CardLayout) => boolean): void {
  pendingLayout.set(card, { matches, until: Date.now() + 3000 });
}

const near = (a: number, b: number): boolean => Math.abs(a - b) < 1e-6;

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(Math.max(v, lo), Math.max(lo, hi));
}

function placeFree(card: HTMLElement, x: number, y: number): void {
  const W = display.clientWidth;
  const H = display.clientHeight;
  const w = card.offsetWidth;
  const h = card.offsetHeight;
  card.style.left = `${clamp(x * W - w / 2, 0, W - w)}px`;
  card.style.top = `${clamp(y * H - h / 2, 0, H - h)}px`;
}

function layoutCard(card: HTMLElement, layout: CardLayout | undefined): void {
  if (!layout || card === draggingCard) return;  // never yank a card mid-drag
  const pending = pendingLayout.get(card);
  if (pending) {
    if (!pending.matches(layout) && Date.now() < pending.until) return; // stale
    pendingLayout.delete(card);
  }
  cardLayouts.set(card, layout);
  card.classList.toggle("minimized", layout.minimized);
  if (layout.drag) {
    card.dataset.pos = "free";
    placeFree(card, layout.drag.x, layout.drag.y);
  } else {
    card.dataset.pos = CARD_POSITIONS.includes(layout.position) ? layout.position : "bottom";
    card.style.left = "";
    card.style.top = "";
  }
}

// A dragged card keeps its relative spot when the window changes size.
window.addEventListener("resize", () => {
  for (const [card, layout] of cardLayouts) {
    if (layout.drag) placeFree(card, layout.drag.x, layout.drag.y);
  }
});

function setupCard(card: HTMLElement): void {
  const name = card.dataset.card as CardName;
  const grip = card.querySelector<HTMLElement>(".card-grip")!;

  grip.addEventListener("pointerdown", (e: PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    grip.setPointerCapture(e.pointerId);
    draggingCard = card;
    card.classList.add("dragging");
    const dRect = display.getBoundingClientRect();
    const cRect = card.getBoundingClientRect();
    const offX = e.clientX - cRect.left;
    const offY = e.clientY - cRect.top;
    // Pin the card where it is before leaving its snapped position, so it
    // doesn't jump between pointer-down and the first move.
    card.style.left = `${cRect.left - dRect.left}px`;
    card.style.top = `${cRect.top - dRect.top}px`;
    card.dataset.pos = "free";

    const onMove = (ev: PointerEvent): void => {
      const W = display.clientWidth;
      const H = display.clientHeight;
      card.style.left = `${clamp(ev.clientX - dRect.left - offX, 0, W - card.offsetWidth)}px`;
      card.style.top = `${clamp(ev.clientY - dRect.top - offY, 0, H - card.offsetHeight)}px`;
    };
    const onEnd = (): void => {
      grip.removeEventListener("pointermove", onMove);
      grip.removeEventListener("pointerup", onEnd);
      grip.removeEventListener("pointercancel", onEnd);
      card.classList.remove("dragging");
      draggingCard = null;
      // Store the card's center as screen fractions.
      const x = (card.offsetLeft + card.offsetWidth / 2) / display.clientWidth;
      const y = (card.offsetTop + card.offsetHeight / 2) / display.clientHeight;
      const prev = cardLayouts.get(card);
      if (prev) cardLayouts.set(card, { ...prev, drag: { x, y } });
      expectLayout(card, (l) => !!l.drag && near(l.drag.x, x) && near(l.drag.y, y));
      control("set-card-drag", { card: name, x, y });
    };
    grip.addEventListener("pointermove", onMove);
    grip.addEventListener("pointerup", onEnd);
    grip.addEventListener("pointercancel", onEnd);
  });

  const setMinimized = (minimized: boolean): void => {
    card.classList.toggle("minimized", minimized);   // respond at once
    const prev = cardLayouts.get(card);
    if (prev) {
      cardLayouts.set(card, { ...prev, minimized });
      if (prev.drag) placeFree(card, prev.drag.x, prev.drag.y);
    }
    expectLayout(card, (l) => l.minimized === minimized);
    control("set-card-minimized", { card: name, minimized });
  };
  // Minimized, a card is just its grip (still draggable) and icon; the icon
  // expands it again. Expanded, the icon or the – button minimizes it.
  card.querySelector<HTMLButtonElement>(".card-min")!.addEventListener("click", () => setMinimized(true));
  card.querySelector<HTMLButtonElement>(".card-icon")!.addEventListener("click", () =>
    setMinimized(!card.classList.contains("minimized")));
}

setupCard(cardVisual);
setupCard(cardAudio);

// Clicking the image skips straight to the next one, interrupting the timer
// (the cards and top bar sit above it, so their clicks never reach it).
img.addEventListener("click", () => control("next"));

// ── Keyboard shortcuts ────────────────────────────────────────────────────────
document.addEventListener("keydown", (e: KeyboardEvent) => {
  if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return;
  switch (e.key) {
    case "ArrowRight": control("next"); break;
    case "ArrowLeft":  control("prev"); break;
    case " ":
      control(currentState?.playing ? "pause" : "play");
      e.preventDefault();
      break;
    case "Enter":
      if (currentState?.music_enabled) {
        if (audioEl.paused) { audioEl.play().catch(() => {}); musicUserPaused = false; }
        else                { audioEl.pause(); musicUserPaused = true; }
        e.preventDefault();
      }
      break;
  }
});

// ── Boot ──────────────────────────────────────────────────────────────────────
connectSSE();
