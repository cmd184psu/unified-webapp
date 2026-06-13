export {};

// ── Types ─────────────────────────────────────────────────────────────────────
interface MusicInfo {
  name: string;
  tracks: string[];
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
  theme: string;
  controls_position: string;
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
const settingsPanel  = document.getElementById("settings-panel")   as HTMLDivElement;
const btnCloseSettings = document.getElementById("btn-close-settings") as HTMLButtonElement;
const settingsScrim  = document.getElementById("settings-scrim")   as HTMLDivElement;
const selMode        = document.getElementById("sel-mode")         as HTMLSelectElement;
const inpInterval    = document.getElementById("inp-interval")     as HTMLInputElement;
const chkShuffle     = document.getElementById("chk-shuffle")      as HTMLInputElement;
const selTheme       = document.getElementById("sel-theme")        as HTMLSelectElement;
const selControlsPos = document.getElementById("sel-controls-pos") as HTMLSelectElement;
const musicControls  = document.getElementById("music-controls")   as HTMLDivElement;
const musicLabel     = document.getElementById("music-label")      as HTMLSpanElement;
const btnMusicStop   = document.getElementById("btn-music-stop")   as HTMLButtonElement;
const btnMusicPlay   = document.getElementById("btn-music-play")   as HTMLButtonElement;
const btnMusicNext   = document.getElementById("btn-music-next")   as HTMLButtonElement;
const audioEl        = document.getElementById("audio-player")     as HTMLAudioElement;
const debugDisplayEl = document.getElementById("debug-display")    as HTMLSpanElement;
const serverStampEl  = document.getElementById("server-stamp")     as HTMLDivElement;
const chkDebug       = document.getElementById("chk-debug")        as HTMLInputElement;

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

  document.documentElement.dataset["theme"] = state.theme;
  display.dataset["controlsPos"] = state.controls_position || "bottom";

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
  chkShuffle.checked   = state.shuffle;
  selTheme.value       = state.theme;
  selControlsPos.value = state.controls_position || "bottom";

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
  musicControls.hidden = !hasMusic;
  if (!hasMusic) return;

  const coll = colls[state.music_collection];
  musicLabel.textContent = coll?.name ?? "";

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

// ── Settings panel ────────────────────────────────────────────────────────────
function openSettings():  void { settingsPanel.hidden = false; settingsScrim.hidden = false; }
function closeSettings(): void { settingsPanel.hidden = true;  settingsScrim.hidden = true;  }

btnHamburger.addEventListener("click",     () => settingsPanel.hidden ? openSettings() : closeSettings());
btnCloseSettings.addEventListener("click", closeSettings);
settingsScrim.addEventListener("click",    closeSettings);

chkDebug.addEventListener("change", () => debugTimer.show(chkDebug.checked));

selMode.addEventListener("change",        () => control("set-mode",              selMode.value));
selTheme.addEventListener("change",       () => control("set-theme",             selTheme.value));
chkShuffle.addEventListener("change",     () => control("set-shuffle",           chkShuffle.checked));
selControlsPos.addEventListener("change", () => control("set-controls-position", selControlsPos.value));
inpInterval.addEventListener("change", () => {
  const v = parseInt(inpInterval.value, 10);
  if (!isNaN(v) && v >= 1) control("set-interval", v);
});

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
    case "Escape": closeSettings(); break;
  }
});

// ── Boot ──────────────────────────────────────────────────────────────────────
connectSSE();
