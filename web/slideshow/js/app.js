"use strict";
import { ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";
export {};
class PanScan {
  // incremented on every activate(); guards stale load events
  constructor(el2) {
    this.raf = 0;
    this.t0 = 0;
    // performance.now() when current run started
    this.elapsed = 0;
    // ms accumulated before current run
    this.duration = 8e3;
    // ms for full pan (set by activate)
    this.dir = "v";
    this.on = false;
    this.playing = false;
    this.generation = 0;
    this.el = el2;
    el2.addEventListener("load", () => this.onLoad());
    window.addEventListener("resize", () => this.onResize());
  }
  // Called when entering panscan mode or when the image changes in panscan mode.
  // Always resets the pan to position 0.
  activate(secs) {
    this.cancel();
    this.on = true;
    this.elapsed = 0;
    this.duration = secs * 1e3;
    this.generation++;
    this.el.style.objectFit = "cover";
    if (this.loaded()) {
      this.detect();
      this.paint(0);
      if (this.playing) this.go();
    }
  }
  // Called when leaving panscan mode.
  deactivate() {
    this.cancel();
    this.on = false;
    this.el.style.removeProperty("object-fit");
    this.el.style.removeProperty("object-position");
  }
  // Called whenever the conductor playing state changes.
  setPlaying(playing) {
    if (this.playing === playing) return;
    this.playing = playing;
    if (!this.on) return;
    if (playing && this.loaded() && !this.raf) this.go();
    else if (!playing) this.cancel();
  }
  // ── Private ──────────────────────────────────────────────────────────────────
  onLoad() {
    if (!this.on) return;
    const gen = this.generation;
    this.detect();
    this.paint(0);
    if (this.playing && !this.raf && gen === this.generation) this.go();
  }
  onResize() {
    if (!this.on || !this.loaded()) return;
    this.detect();
    this.paint(this.pos());
  }
  // Start (or restart) the RAF loop from the current elapsed position.
  go() {
    const gen = this.generation;
    this.t0 = performance.now();
    const tick = (now) => {
      if (gen !== this.generation) {
        this.raf = 0;
        return;
      }
      const p = Math.min((this.elapsed + now - this.t0) / this.duration, 1);
      this.paint(p);
      if (p < 1) {
        this.raf = requestAnimationFrame(tick);
      } else {
        this.raf = 0;
      }
    };
    this.raf = requestAnimationFrame(tick);
  }
  // Stop the RAF loop and snapshot elapsed time so we can resume correctly.
  cancel() {
    if (this.raf) {
      this.elapsed = Math.min(this.elapsed + performance.now() - this.t0, this.duration);
      cancelAnimationFrame(this.raf);
      this.raf = 0;
    }
  }
  // Current progress 0..1, usable whether running or paused.
  pos() {
    const ms = this.raf ? this.elapsed + performance.now() - this.t0 : this.elapsed;
    return Math.min(ms / this.duration, 1);
  }
  // Use the element's own rendered size, not window.inner*, so the ratio is
  // correct even when 100dvh ≠ window.innerHeight (iOS Safari with toolbar).
  detect() {
    const iw = this.el.naturalWidth;
    const ih = this.el.naturalHeight;
    const cw = this.el.offsetWidth || window.innerWidth;
    const ch = this.el.offsetHeight || window.innerHeight;
    this.dir = iw / ih > cw / ch ? "h" : "v";
  }
  paint(progress) {
    const pct = (cubicEaseInOut(progress) * 100).toFixed(2) + "%";
    this.el.style.objectPosition = this.dir === "v" ? `50% ${pct}` : `${pct} 50%`;
  }
  loaded() {
    return this.el.complete && this.el.naturalWidth > 0;
  }
  // Exposed for debug display only.
  getProgress() {
    return this.pos();
  }
  getDirection() {
    return this.dir;
  }
  isActive() {
    return this.on;
  }
}
function cubicEaseInOut(t) {
  return t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2;
}
class DebugTimer {
  constructor(el2) {
    this.raf = 0;
    this.elapsed = 0;
    // ms spent playing since last image
    this.runStart = 0;
    // performance.now() when current run started
    this.running = false;
    this.intervalMs = 8e3;
    this.mode = "";
    this.tick = () => {
      if (this.el.hidden) {
        this.raf = 0;
        return;
      }
      const total = this.running ? this.elapsed + performance.now() - this.runStart : this.elapsed;
      const remaining = Math.max(0, this.intervalMs - total) / 1e3;
      const pan = panScan.isActive() ? panScan.getProgress() : -1;
      const dir = panScan.isActive() ? panScan.getDirection() === "v" ? "\u2195" : "\u2194" : "";
      let text = `\u23F1 ${remaining.toFixed(1)}s`;
      if (!this.running) text += " \u23F8";
      if (pan >= 0) text += `  ${dir}pan ${Math.round(pan * 100)}%`;
      this.el.textContent = text;
      this.raf = requestAnimationFrame(this.tick);
    };
    this.el = el2;
  }
  // Called when a new image arrives.
  resetImage(intervalSecs, mode) {
    this.elapsed = 0;
    this.intervalMs = intervalSecs * 1e3;
    this.mode = mode;
    if (this.running) this.runStart = performance.now();
  }
  // Called whenever mode or interval changes without a new image.
  setMeta(intervalSecs, mode) {
    this.intervalMs = intervalSecs * 1e3;
    this.mode = mode;
  }
  setPlaying(playing) {
    if (playing && !this.running) {
      this.running = true;
      this.runStart = performance.now();
      if (!this.raf) this.tick();
    } else if (!playing && this.running) {
      this.elapsed += performance.now() - this.runStart;
      this.running = false;
    }
  }
  show(visible) {
    this.el.hidden = !visible;
    if (visible && !this.raf) this.tick();
    else if (!visible) {
      cancelAnimationFrame(this.raf);
      this.raf = 0;
    }
  }
}
const display = document.getElementById("display");
const img = document.getElementById("slide-img");
const subjectLabel = document.getElementById("subject-label");
const imgCounter = document.getElementById("image-counter");
const btnPlayPause = document.getElementById("btn-play-pause");
const btnPrev = document.getElementById("btn-prev");
const btnNext = document.getElementById("btn-next");
const btnPrevSubj = document.getElementById("btn-prev-subject");
const btnNextSubj = document.getElementById("btn-next-subject");
const btnHamburger = document.getElementById("btn-hamburger");
const cardVisual = document.getElementById("card-visual");
const cardAudio = document.getElementById("card-audio");
const musicLabel = document.getElementById("music-label");
const btnMusicStop = document.getElementById("btn-music-stop");
const btnMusicPlay = document.getElementById("btn-music-play");
const btnMusicNext = document.getElementById("btn-music-next");
const audioEl = document.getElementById("audio-player");
const debugDisplayEl = document.getElementById("debug-display");
function el(tag, className) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}
function settingRow(label, control2) {
  const row = el("label", "setting-row");
  row.append(label, control2);
  return row;
}
function settingToggle(label, input) {
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
function positionPicker(card, label) {
  const root = el("div", "pos-picker");
  const title = el("span", "pos-picker-label");
  title.textContent = label;
  const grid = el("div", "pos-grid");
  grid.setAttribute("role", "radiogroup");
  grid.setAttribute("aria-label", label);
  const cells = [];
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
        const el2 = card === "visual" ? cardVisual : cardAudio;
        expectLayout(el2, (l) => !l.drag && l.position === pos);
        el2.dataset.pos = pos;
        el2.style.left = "";
        el2.style.top = "";
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
    }
  };
}
const visualPicker = positionPicker("visual", "Image controls position");
const audioPicker = positionPicker("audio", "Audio controls position");
const themes = new ThemeManager({
  module: "slideshow",
  default: "dark",
  onChange: (name) => {
    control("set-theme", name);
  }
});
themes.apply();
new HamburgerMenu({
  title: "Settings",
  side: "right",
  mountTrigger: btnHamburger,
  themePicker: true,
  themes,
  items: [
    {
      id: "playback",
      render: (host) => {
        host.append(
          settingRow("Mode", selMode),
          settingRow("Seconds per image", inpInterval),
          settingRow("Image age limit (days)", inpMaxAge),
          ageHint,
          settingToggle("Shuffle subjects", chkShuffle)
        );
      }
    },
    { section: "Controls" },
    {
      id: "positions",
      render: (host) => {
        const hint = el("p", "setting-hint");
        hint.textContent = "Pick a square to snap a card there; drag a card by its grip to place it anywhere.";
        host.append(visualPicker.root, audioPicker.root, hint);
      }
    },
    { section: "Other" },
    {
      id: "misc",
      render: (host) => {
        const help = el("div", "shortcuts-help");
        help.innerHTML = "<h4>Keyboard shortcuts</h4><dl><dt><kbd>&#8592;</kbd> <kbd>&#8594;</kbd></dt><dd>Prev / next image</dd><dt>Click the image</dt><dd>Next image</dd><dt><kbd>Space</kbd></dt><dd>Play / pause slideshow</dd><dt><kbd>Enter</kbd></dt><dd>Play / stop music</dd><dt><kbd>Esc</kbd></dt><dd>Close this menu</dd></dl>";
        host.append(settingToggle("Debug timer", chkDebug), serverStampEl, help);
      }
    }
  ]
});
const panScan = new PanScan(img);
const debugTimer = new DebugTimer(debugDisplayEl);
let currentState = null;
let kburnsTick = false;
let prevMusicCollection = -1;
let musicUserPaused = true;
let currentTrackIndex = 0;
function connectSSE() {
  const es = new EventSource("/api/events");
  es.addEventListener("state", (e) => {
    try {
      applyState(JSON.parse(e.data));
    } catch {
    }
  });
  es.onerror = () => {
    es.close();
    setTimeout(connectSSE, 3e3);
  };
}
function applyState(state) {
  const prev = currentState;
  currentState = state;
  if (state.theme) themes.adopt(state.theme);
  layoutCard(cardVisual, state.visual_card);
  layoutCard(cardAudio, state.audio_card);
  visualPicker.sync(state.visual_card?.drag ? "" : state.visual_card?.position ?? "");
  audioPicker.sync(state.audio_card?.drag ? "" : state.audio_card?.position ?? "");
  const imageChanged = !prev || prev.image_path !== state.image_path;
  const modeChanged = !prev || prev.mode !== state.mode;
  const intervalChanged = !prev || prev.interval_seconds !== state.interval_seconds;
  if (imageChanged) {
    img.src = state.image_path ? `/slides/${state.image_path}` : "";
    applyModeClass(state.mode, state.interval_seconds);
    debugTimer.resetImage(state.interval_seconds, state.mode);
  } else if (modeChanged || intervalChanged) {
    applyModeClass(state.mode, state.interval_seconds);
    debugTimer.setMeta(state.interval_seconds, state.mode);
  }
  panScan.setPlaying(state.playing);
  debugTimer.setPlaying(state.playing);
  display.classList.toggle("paused", !state.playing);
  subjectLabel.textContent = state.subject ? `${state.subject} (${state.subject_index + 1}/${state.total_subjects})` : "";
  imgCounter.textContent = state.total_images > 0 ? `${state.image_index + 1} / ${state.total_images}` : "";
  btnPlayPause.textContent = state.playing ? "\u23F8" : "\u25B6";
  btnPlayPause.title = state.playing ? "Pause" : "Play";
  selMode.value = state.mode;
  inpInterval.value = String(state.interval_seconds);
  if (document.activeElement !== inpMaxAge) inpMaxAge.value = String(state.max_age_days ?? 0);
  chkShuffle.checked = state.shuffle;
  applyMusicState(state);
  if (state.server_started && serverStampEl.textContent !== state.server_started) {
    serverStampEl.textContent = `started ${state.server_started}`;
  }
}
function applyModeClass(mode, intervalSecs) {
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
      kburnsTick = !kburnsTick;
      img.classList.add(kburnsTick ? "mode-kenburns-a" : "mode-kenburns-b");
    }
  }
}
audioEl.addEventListener("ended", () => {
  if (musicUserPaused) return;
  const state = currentState;
  if (!state?.music_collections?.length) return;
  const coll = state.music_collections[state.music_collection];
  if (!coll) return;
  currentTrackIndex = (currentTrackIndex + 1) % coll.tracks.length;
  loadAndPlay(coll);
});
function applyMusicState(state) {
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
      setTrack(coll);
      if (!musicUserPaused) audioEl.play().catch(() => {
      });
    }
  }
}
function setTrack(coll) {
  if (!coll.tracks.length) return;
  if (currentTrackIndex >= coll.tracks.length) currentTrackIndex = 0;
  audioEl.src = `/audio/${coll.tracks[currentTrackIndex]}`;
}
function loadAndPlay(coll) {
  setTrack(coll);
  audioEl.play().catch(() => {
  });
}
btnMusicStop.addEventListener("click", () => {
  audioEl.pause();
  audioEl.currentTime = 0;
  musicUserPaused = true;
});
btnMusicPlay.addEventListener("click", () => {
  musicUserPaused = false;
  audioEl.play().catch(() => {
  });
});
btnMusicNext.addEventListener("click", () => control("music-next"));
function control(action, value) {
  const body = { action };
  if (value !== void 0) body["value"] = value;
  fetch("/api/control", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  }).catch(() => {
  });
}
btnPlayPause.addEventListener("click", () => control(currentState?.playing ? "pause" : "play"));
btnPrev.addEventListener("click", () => control("prev"));
btnNext.addEventListener("click", () => control("next"));
btnPrevSubj.addEventListener("click", () => control("prev-subject"));
btnNextSubj.addEventListener("click", () => control("next-subject"));
chkDebug.addEventListener("change", () => debugTimer.show(chkDebug.checked));
selMode.addEventListener("change", () => control("set-mode", selMode.value));
chkShuffle.addEventListener("change", () => control("set-shuffle", chkShuffle.checked));
inpMaxAge.addEventListener("change", () => {
  const v = parseInt(inpMaxAge.value, 10);
  if (!isNaN(v) && v >= 0) control("set-max-age", v);
});
inpInterval.addEventListener("change", () => {
  const v = parseInt(inpInterval.value, 10);
  if (!isNaN(v) && v >= 1) control("set-interval", v);
});
const cardLayouts = /* @__PURE__ */ new Map();
let draggingCard = null;
const pendingLayout = /* @__PURE__ */ new Map();
function expectLayout(card, matches) {
  pendingLayout.set(card, { matches, until: Date.now() + 3e3 });
}
const near = (a, b) => Math.abs(a - b) < 1e-6;
function clamp(v, lo, hi) {
  return Math.min(Math.max(v, lo), Math.max(lo, hi));
}
function placeFree(card, x, y) {
  const W = display.clientWidth;
  const H = display.clientHeight;
  const w = card.offsetWidth;
  const h = card.offsetHeight;
  card.style.left = `${clamp(x * W - w / 2, 0, W - w)}px`;
  card.style.top = `${clamp(y * H - h / 2, 0, H - h)}px`;
}
function layoutCard(card, layout) {
  if (!layout || card === draggingCard) return;
  const pending = pendingLayout.get(card);
  if (pending) {
    if (!pending.matches(layout) && Date.now() < pending.until) return;
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
window.addEventListener("resize", () => {
  for (const [card, layout] of cardLayouts) {
    if (layout.drag) placeFree(card, layout.drag.x, layout.drag.y);
  }
});
function setupCard(card) {
  const name = card.dataset.card;
  const grip = card.querySelector(".card-grip");
  grip.addEventListener("pointerdown", (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    grip.setPointerCapture(e.pointerId);
    draggingCard = card;
    card.classList.add("dragging");
    const dRect = display.getBoundingClientRect();
    const cRect = card.getBoundingClientRect();
    const offX = e.clientX - cRect.left;
    const offY = e.clientY - cRect.top;
    card.style.left = `${cRect.left - dRect.left}px`;
    card.style.top = `${cRect.top - dRect.top}px`;
    card.dataset.pos = "free";
    const onMove = (ev) => {
      const W = display.clientWidth;
      const H = display.clientHeight;
      card.style.left = `${clamp(ev.clientX - dRect.left - offX, 0, W - card.offsetWidth)}px`;
      card.style.top = `${clamp(ev.clientY - dRect.top - offY, 0, H - card.offsetHeight)}px`;
    };
    const onEnd = () => {
      grip.removeEventListener("pointermove", onMove);
      grip.removeEventListener("pointerup", onEnd);
      grip.removeEventListener("pointercancel", onEnd);
      card.classList.remove("dragging");
      draggingCard = null;
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
  const setMinimized = (minimized) => {
    card.classList.toggle("minimized", minimized);
    const prev = cardLayouts.get(card);
    if (prev) {
      cardLayouts.set(card, { ...prev, minimized });
      if (prev.drag) placeFree(card, prev.drag.x, prev.drag.y);
    }
    expectLayout(card, (l) => l.minimized === minimized);
    control("set-card-minimized", { card: name, minimized });
  };
  card.querySelector(".card-min").addEventListener("click", () => setMinimized(true));
  card.querySelector(".card-icon").addEventListener("click", () => setMinimized(!card.classList.contains("minimized")));
}
setupCard(cardVisual);
setupCard(cardAudio);
img.addEventListener("click", () => control("next"));
document.addEventListener("keydown", (e) => {
  if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return;
  switch (e.key) {
    case "ArrowRight":
      control("next");
      break;
    case "ArrowLeft":
      control("prev");
      break;
    case " ":
      control(currentState?.playing ? "pause" : "play");
      e.preventDefault();
      break;
    case "Enter":
      if (currentState?.music_enabled) {
        if (audioEl.paused) {
          audioEl.play().catch(() => {
          });
          musicUserPaused = false;
        } else {
          audioEl.pause();
          musicUserPaused = true;
        }
        e.preventDefault();
      }
      break;
  }
});
connectSSE();
