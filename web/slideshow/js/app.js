"use strict";
export {};
class PanScan {
  // incremented on every activate(); guards stale load events
  constructor(el) {
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
    this.el = el;
    el.addEventListener("load", () => this.onLoad());
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
  constructor(el) {
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
    this.el = el;
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
const settingsPanel = document.getElementById("settings-panel");
const btnCloseSettings = document.getElementById("btn-close-settings");
const settingsScrim = document.getElementById("settings-scrim");
const selMode = document.getElementById("sel-mode");
const inpInterval = document.getElementById("inp-interval");
const chkShuffle = document.getElementById("chk-shuffle");
const selTheme = document.getElementById("sel-theme");
const selControlsPos = document.getElementById("sel-controls-pos");
const musicControls = document.getElementById("music-controls");
const musicLabel = document.getElementById("music-label");
const btnMusicStop = document.getElementById("btn-music-stop");
const btnMusicPlay = document.getElementById("btn-music-play");
const btnMusicNext = document.getElementById("btn-music-next");
const audioEl = document.getElementById("audio-player");
const debugDisplayEl = document.getElementById("debug-display");
const serverStampEl = document.getElementById("server-stamp");
const chkDebug = document.getElementById("chk-debug");
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
  document.documentElement.dataset["theme"] = state.theme;
  display.dataset["controlsPos"] = state.controls_position || "bottom";
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
  chkShuffle.checked = state.shuffle;
  selTheme.value = state.theme;
  selControlsPos.value = state.controls_position || "bottom";
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
  musicControls.hidden = !hasMusic;
  if (!hasMusic) return;
  const coll = colls[state.music_collection];
  musicLabel.textContent = coll?.name ?? "";
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
function openSettings() {
  settingsPanel.hidden = false;
  settingsScrim.hidden = false;
}
function closeSettings() {
  settingsPanel.hidden = true;
  settingsScrim.hidden = true;
}
btnHamburger.addEventListener("click", () => settingsPanel.hidden ? openSettings() : closeSettings());
btnCloseSettings.addEventListener("click", closeSettings);
settingsScrim.addEventListener("click", closeSettings);
chkDebug.addEventListener("change", () => debugTimer.show(chkDebug.checked));
selMode.addEventListener("change", () => control("set-mode", selMode.value));
selTheme.addEventListener("change", () => control("set-theme", selTheme.value));
chkShuffle.addEventListener("change", () => control("set-shuffle", chkShuffle.checked));
selControlsPos.addEventListener("change", () => control("set-controls-position", selControlsPos.value));
inpInterval.addEventListener("change", () => {
  const v = parseInt(inpInterval.value, 10);
  if (!isNaN(v) && v >= 1) control("set-interval", v);
});
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
    case "Escape":
      closeSettings();
      break;
  }
});
connectSSE();
