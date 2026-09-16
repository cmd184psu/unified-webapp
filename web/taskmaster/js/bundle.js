"use strict";
(() => {
  // web/taskmaster/js/ui/modal.ts
  var STYLE_ATTR = "data-tm-ui-modal-styles";
  function ensureStyles() {
    if (document.head.querySelector(`style[${STYLE_ATTR}]`)) return;
    const style = document.createElement("style");
    style.setAttribute(STYLE_ATTR, "");
    style.textContent = `
.tm-modal-overlay {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 10000;
  font-family: var(--font-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
}
.tm-modal-panel {
  background: var(--bg-secondary, #252525);
  color: var(--text-normal, #dcddde);
  border: 1px solid var(--bg-modifier-border, #3a3a3a);
  border-radius: var(--radius, 6px);
  min-width: 20em;
  max-width: min(32em, calc(100vw - 2em));
  max-height: calc(100vh - 2em);
  overflow: auto;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.4);
  padding: 1.25em;
}
.tm-modal-title {
  font-size: 1.05em;
  font-weight: 600;
  margin: 0 0 0.75em 0;
}
.tm-modal-message {
  margin: 0 0 1em 0;
  white-space: pre-wrap;
  line-height: 1.4;
}
.tm-modal-input {
  width: 100%;
  box-sizing: border-box;
  padding: 0.5em;
  margin-bottom: 1em;
  background: var(--bg-primary, #1e1e1e);
  color: var(--text-normal, #dcddde);
  border: 1px solid var(--bg-modifier-border, #3a3a3a);
  border-radius: var(--radius, 6px);
  font-family: inherit;
  font-size: 1em;
}
.tm-modal-input:focus-visible {
  outline: none;
  border-color: var(--interactive-accent, #7f6df2);
}
.tm-modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 0.5em;
}
.tm-modal-btn {
  padding: 0.45em 1em;
  border-radius: var(--radius, 6px);
  border: 1px solid var(--bg-modifier-border, #3a3a3a);
  background: var(--bg-tertiary, #2d2d2d);
  color: var(--text-normal, #dcddde);
  cursor: pointer;
  font-family: inherit;
  font-size: 0.9em;
}
.tm-modal-btn:hover {
  border-color: var(--interactive-accent, #7f6df2);
}
.tm-modal-btn:focus-visible {
  outline: none;
  border-color: var(--interactive-accent, #7f6df2);
  box-shadow: 0 0 0 2px var(--interactive-accent-hover, #9d8fff);
}
.tm-modal-btn-primary {
  background: var(--interactive-accent, #7f6df2);
  border-color: var(--interactive-accent, #7f6df2);
  color: #fff;
}
.tm-modal-btn-primary:hover {
  background: var(--interactive-accent-hover, #9d8fff);
}
`;
    document.head.appendChild(style);
  }
  function openModal(contentEl, opts = {}) {
    ensureStyles();
    const closeOnEscape = opts.closeOnEscape !== false;
    const closeOnOverlayClick = opts.closeOnOverlayClick !== false;
    const previouslyFocused = document.activeElement;
    const overlay = document.createElement("div");
    overlay.className = "tm-modal-overlay";
    const panel = document.createElement("div");
    panel.className = "tm-modal-panel";
    panel.setAttribute("role", "dialog");
    panel.setAttribute("aria-modal", "true");
    panel.tabIndex = -1;
    if (opts.title) {
      const titleEl = document.createElement("h2");
      titleEl.className = "tm-modal-title";
      titleEl.textContent = opts.title;
      panel.appendChild(titleEl);
    }
    panel.appendChild(contentEl);
    overlay.appendChild(panel);
    document.body.appendChild(overlay);
    let closed = false;
    function getFocusable() {
      return Array.from(
        panel.querySelectorAll(
          'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'
        )
      );
    }
    function onKeydown(e) {
      if (e.key === "Escape" && closeOnEscape) {
        e.preventDefault();
        close();
        return;
      }
      if (e.key === "Tab") {
        const focusable2 = getFocusable();
        if (focusable2.length === 0) {
          e.preventDefault();
          panel.focus();
          return;
        }
        const first = focusable2[0];
        const last = focusable2[focusable2.length - 1];
        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    }
    function onOverlayClick(e) {
      if (closeOnOverlayClick && e.target === overlay) {
        close();
      }
    }
    function close() {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKeydown, true);
      overlay.removeEventListener("mousedown", onOverlayClick);
      overlay.remove();
      if (previouslyFocused && typeof previouslyFocused.focus === "function") {
        previouslyFocused.focus();
      }
      opts.onClose?.();
    }
    document.addEventListener("keydown", onKeydown, true);
    overlay.addEventListener("mousedown", onOverlayClick);
    const focusable = getFocusable();
    (focusable[0] ?? panel).focus();
    return { overlay, panel, close };
  }
  function confirmDialog(message, opts = {}) {
    return new Promise((resolve) => {
      let settled = false;
      const content = document.createElement("div");
      const msg = document.createElement("p");
      msg.className = "tm-modal-message";
      msg.textContent = message;
      content.appendChild(msg);
      const actions = document.createElement("div");
      actions.className = "tm-modal-actions";
      const cancelBtn = document.createElement("button");
      cancelBtn.type = "button";
      cancelBtn.className = "tm-modal-btn";
      cancelBtn.textContent = opts.cancelLabel ?? "Cancel";
      const okBtn = document.createElement("button");
      okBtn.type = "button";
      okBtn.className = "tm-modal-btn tm-modal-btn-primary";
      okBtn.textContent = opts.confirmLabel ?? "OK";
      actions.appendChild(cancelBtn);
      actions.appendChild(okBtn);
      content.appendChild(actions);
      const handle = openModal(content, {
        title: opts.title,
        onClose: () => {
          if (!settled) {
            settled = true;
            resolve(false);
          }
        }
      });
      cancelBtn.addEventListener("click", () => {
        settled = true;
        resolve(false);
        handle.close();
      });
      okBtn.addEventListener("click", () => {
        settled = true;
        resolve(true);
        handle.close();
      });
    });
  }
  function alertDialog(message, opts = {}) {
    return new Promise((resolve) => {
      let settled = false;
      const content = document.createElement("div");
      const msg = document.createElement("p");
      msg.className = "tm-modal-message";
      msg.textContent = message;
      content.appendChild(msg);
      const actions = document.createElement("div");
      actions.className = "tm-modal-actions";
      const okBtn = document.createElement("button");
      okBtn.type = "button";
      okBtn.className = "tm-modal-btn tm-modal-btn-primary";
      okBtn.textContent = opts.confirmLabel ?? "OK";
      actions.appendChild(okBtn);
      content.appendChild(actions);
      const handle = openModal(content, {
        title: opts.title,
        onClose: () => {
          if (!settled) {
            settled = true;
            resolve();
          }
        }
      });
      okBtn.addEventListener("click", () => {
        settled = true;
        resolve();
        handle.close();
      });
    });
  }

  // web/taskmaster/js/api.ts
  async function apiFetch(path, options = {}) {
    const headers = { "Content-Type": "application/json" };
    const existingHeaders = options.headers;
    if (existingHeaders) {
      Object.assign(headers, existingHeaders);
    }
    let resp;
    try {
      resp = await fetch(path, { ...options, headers });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      void alertDialog("Network error contacting the server: " + msg);
      throw err;
    }
    if (resp.status === 401) {
      window.location.reload();
      throw new Error("unauthorized");
    }
    if (!resp.ok) {
      let errMsg = resp.statusText;
      try {
        const body = await resp.json();
        if (body.error) errMsg = body.error;
      } catch {
      }
      void alertDialog("Request failed: " + errMsg);
      throw new Error(errMsg);
    }
    if (resp.status === 204) return void 0;
    return resp.json();
  }
  var api = {
    health() {
      return apiFetch("/api/health");
    },
    capabilities() {
      return apiFetch("/api/capabilities");
    },
    setCapabilities(allowSudo) {
      return apiFetch("/api/capabilities", {
        method: "POST",
        body: JSON.stringify({ allow_sudo: allowSudo })
      });
    },
    authMode() {
      return apiFetch("/api/auth/mode");
    },
    logout() {
      return fetch("/api/auth/logout", { method: "POST" });
    },
    // ─── Lanes ────────────────────────────────────────────────────────────
    listLanes() {
      return apiFetch("/api/lanes");
    },
    getLane(name) {
      return apiFetch("/api/lanes/" + encodeURIComponent(name));
    },
    createLane(l) {
      return apiFetch("/api/lanes", { method: "POST", body: JSON.stringify(l) });
    },
    updateLane(name, updates) {
      return apiFetch("/api/lanes/" + encodeURIComponent(name), {
        method: "PUT",
        body: JSON.stringify(updates)
      });
    },
    deleteLane(name) {
      return apiFetch("/api/lanes/" + encodeURIComponent(name), { method: "DELETE" });
    },
    pauseLane(name) {
      return apiFetch("/api/lanes/" + encodeURIComponent(name) + "/pause", {
        method: "POST"
      });
    },
    resumeLane(name) {
      return apiFetch("/api/lanes/" + encodeURIComponent(name) + "/resume", {
        method: "POST"
      });
    },
    setLaneWidth(name, width) {
      return apiFetch("/api/lanes/" + encodeURIComponent(name) + "/width", {
        method: "PUT",
        body: JSON.stringify({ width })
      });
    },
    setLaneOrder(name, order) {
      return apiFetch("/api/lanes/" + encodeURIComponent(name) + "/order", {
        method: "PUT",
        body: JSON.stringify({ order })
      });
    },
    // ─── Tasks ────────────────────────────────────────────────────────────
    listTasks(lane) {
      const q = lane ? "?lane=" + encodeURIComponent(lane) : "";
      return apiFetch("/api/tasks" + q);
    },
    getTask(name) {
      return apiFetch("/api/tasks/" + encodeURIComponent(name));
    },
    addTask(task) {
      return apiFetch("/api/tasks", { method: "POST", body: JSON.stringify(task) });
    },
    updateTask(name, updates) {
      return apiFetch("/api/tasks/" + encodeURIComponent(name), {
        method: "PUT",
        body: JSON.stringify(updates)
      });
    },
    deleteTask(name) {
      return apiFetch("/api/tasks/" + encodeURIComponent(name), { method: "DELETE" });
    },
    pauseTask(name) {
      return apiFetch("/api/tasks/" + encodeURIComponent(name) + "/pause", {
        method: "POST"
      });
    },
    resumeTask(name) {
      return apiFetch("/api/tasks/" + encodeURIComponent(name) + "/resume", {
        method: "POST"
      });
    },
    upNext(name) {
      return apiFetch("/api/tasks/" + encodeURIComponent(name) + "/up-next", {
        method: "POST"
      });
    },
    moveTask(name, laneName) {
      return apiFetch("/api/tasks/" + encodeURIComponent(name) + "/move", {
        method: "POST",
        body: JSON.stringify({ lane_name: laneName })
      });
    },
    // ─── Executions ───────────────────────────────────────────────────────
    listExecutions(taskName, limit = 50) {
      const params = new URLSearchParams({ limit: String(limit) });
      if (taskName) params.set("task", taskName);
      return apiFetch("/api/executions?" + params);
    },
    // If the execution already finished (a genuine race for a fast task —
    // between the board rendering it as running and the click landing), the
    // server answers 200 {"status":"not_running"} rather than an error: that
    // outcome isn't a mistake, so there is nothing here to special-case — a
    // real error status (400 signal failure, 404 no-such-execution) still
    // surfaces through the normal apiFetch error path.
    cancelExecution(id) {
      return apiFetch("/api/executions/" + id + "/cancel", { method: "POST" });
    },
    pauseExecution(id) {
      return apiFetch("/api/executions/" + id + "/pause", { method: "POST" });
    },
    resumeExecution(id) {
      return apiFetch("/api/executions/" + id + "/resume", { method: "POST" });
    },
    /** Opens a live SSE stream of an execution's stdout/stderr. Caller owns close(). */
    openExecutionOutput(id) {
      return new EventSource("/api/executions/" + id + "/output");
    },
    // ─── Metrics ──────────────────────────────────────────────────────────
    getMetrics(lane, task, hours = 24) {
      const params = new URLSearchParams({ hours: String(hours) });
      if (lane) params.set("lane", lane);
      if (task) params.set("task", task);
      return apiFetch("/api/metrics?" + params);
    },
    // ─── Hand brake ───────────────────────────────────────────────────────
    getBrake() {
      return apiFetch("/api/brake");
    },
    engageBrake() {
      return apiFetch("/api/brake", { method: "POST" });
    },
    releaseBrake() {
      return apiFetch("/api/brake", { method: "DELETE" });
    }
  };

  // web/taskmaster/js/ui/live.ts
  var LIVE_ENABLED_KEY = "tm.live.enabled";
  var LIVE_INTERVAL_KEY = "tm.live.interval";
  var DEFAULT_INTERVAL_MS = 5e3;
  var BOARD_EVENTS_URL = "/api/board/events";
  var BOARD_EVENT_NAME = "board";
  function readStoredEnabled() {
    try {
      const raw = localStorage.getItem(LIVE_ENABLED_KEY);
      if (raw === null) return true;
      return raw === "true";
    } catch {
      return true;
    }
  }
  function readStoredInterval() {
    try {
      const raw = localStorage.getItem(LIVE_INTERVAL_KEY);
      const n = raw !== null ? Number(raw) : NaN;
      return Number.isFinite(n) && n > 0 ? n : DEFAULT_INTERVAL_MS;
    } catch {
      return DEFAULT_INTERVAL_MS;
    }
  }
  var LiveController = class {
    constructor(url = BOARD_EVENTS_URL) {
      this.source = null;
      this.eventListeners = /* @__PURE__ */ new Set();
      this.tickListeners = /* @__PURE__ */ new Set();
      this.enabledListeners = /* @__PURE__ */ new Set();
      this.url = url;
      this.enabled = readStoredEnabled();
      this.intervalMs = readStoredInterval();
      if (this.enabled) {
        this.open();
      }
    }
    /** Registers a listener invoked for every parsed board event. */
    onEvent(listener) {
      this.eventListeners.add(listener);
      return () => this.eventListeners.delete(listener);
    }
    /** Registers a listener invoked on each fallback-poll tick. */
    onTick(listener) {
      this.tickListeners.add(listener);
      return () => this.tickListeners.delete(listener);
    }
    /** Registers a listener invoked whenever live/pause state changes. */
    onEnabledChange(listener) {
      this.enabledListeners.add(listener);
      return () => this.enabledListeners.delete(listener);
    }
    /** Fires all registered tick listeners. Callers own the actual timer. */
    tick() {
      for (const l of this.tickListeners) l();
    }
    isEnabled() {
      return this.enabled;
    }
    /** Turns the live stream on/off and persists the choice. */
    setEnabled(enabled) {
      if (this.enabled === enabled) return;
      this.enabled = enabled;
      try {
        localStorage.setItem(LIVE_ENABLED_KEY, String(enabled));
      } catch {
      }
      if (enabled) {
        this.open();
      } else {
        this.closeSource();
      }
      for (const l of this.enabledListeners) l(enabled);
    }
    getInterval() {
      return this.intervalMs;
    }
    /** Sets the fallback poll interval (ms) and persists it. */
    setInterval(ms) {
      if (!Number.isFinite(ms) || ms <= 0) return;
      this.intervalMs = ms;
      try {
        localStorage.setItem(LIVE_INTERVAL_KEY, String(ms));
      } catch {
      }
    }
    /** Closes the EventSource and releases all listeners. */
    destroy() {
      this.closeSource();
      this.eventListeners.clear();
      this.tickListeners.clear();
      this.enabledListeners.clear();
    }
    open() {
      if (this.source) return;
      if (typeof EventSource === "undefined") return;
      const source = new EventSource(this.url);
      source.addEventListener(BOARD_EVENT_NAME, (e) => {
        let parsed = null;
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
    closeSource() {
      if (this.source) {
        this.source.close();
        this.source = null;
      }
    }
  };
  var KEY_ATTR = "data-tm-key";
  function patchList(container, items, opts) {
    const existingByKey = /* @__PURE__ */ new Map();
    for (const child of Array.from(container.children)) {
      const el = child;
      const k = el.getAttribute(KEY_ATTR);
      if (k !== null) existingByKey.set(k, el);
    }
    const seenKeys = /* @__PURE__ */ new Set();
    let cursor = container.firstChild;
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
      if (cursor !== el) {
        container.insertBefore(el, cursor);
      } else {
        cursor = cursor.nextSibling;
        continue;
      }
      cursor = el.nextSibling;
    }
    for (const [key, el] of existingByKey) {
      if (!seenKeys.has(key)) {
        el.remove();
      }
    }
  }

  // web/taskmaster/js/ui/toggle.ts
  var STYLE_ATTR2 = "data-tm-ui-toggle-styles";
  function ensureStyles2() {
    if (document.head.querySelector(`style[${STYLE_ATTR2}]`)) return;
    const style = document.createElement("style");
    style.setAttribute(STYLE_ATTR2, "");
    style.textContent = `
.tm-toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.5em;
  cursor: pointer;
  font-family: var(--font-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif);
  color: var(--text-normal, #dcddde);
  user-select: none;
}
.tm-toggle[data-disabled="true"] {
  cursor: not-allowed;
  opacity: 0.5;
}
.tm-toggle-track {
  position: relative;
  flex: 0 0 auto;
  width: 2.25em;
  height: 1.25em;
  border-radius: 999px;
  background: var(--bg-modifier-border, #3a3a3a);
  transition: background-color 0.15s ease;
  box-sizing: border-box;
  border: 1px solid transparent;
}
.tm-toggle-track:focus-visible {
  outline: none;
  border-color: var(--interactive-accent, #7f6df2);
  box-shadow: 0 0 0 2px var(--interactive-accent-hover, #9d8fff);
}
.tm-toggle[data-checked="true"] .tm-toggle-track {
  background: var(--interactive-accent, #7f6df2);
}
.tm-toggle-thumb {
  position: absolute;
  top: 0.1em;
  left: 0.1em;
  width: 1.05em;
  height: 1.05em;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.15s ease;
}
.tm-toggle[data-checked="true"] .tm-toggle-thumb {
  transform: translateX(1em);
}
.tm-toggle-label {
  font-size: 0.9em;
  line-height: 1;
}
`;
    document.head.appendChild(style);
  }
  function createToggleHandle(opts) {
    ensureStyles2();
    let checked = !!opts.checked;
    let disabled = !!opts.disabled;
    const wrapper = document.createElement("span");
    wrapper.className = "tm-toggle";
    const track = document.createElement("span");
    track.className = "tm-toggle-track";
    track.setAttribute("role", "switch");
    track.tabIndex = disabled ? -1 : 0;
    const thumb = document.createElement("span");
    thumb.className = "tm-toggle-thumb";
    track.appendChild(thumb);
    wrapper.appendChild(track);
    let labelEl = null;
    if (opts.label) {
      labelEl = document.createElement("span");
      labelEl.className = "tm-toggle-label";
      labelEl.textContent = opts.label;
      wrapper.appendChild(labelEl);
    }
    function render3() {
      wrapper.setAttribute("data-checked", String(checked));
      wrapper.setAttribute("data-disabled", String(disabled));
      track.setAttribute("aria-checked", String(checked));
      track.setAttribute("aria-disabled", String(disabled));
      track.tabIndex = disabled ? -1 : 0;
      if (opts.label) {
        track.setAttribute("aria-label", opts.label);
      }
    }
    function toggle() {
      if (disabled) return;
      checked = !checked;
      render3();
      opts.onChange(checked);
    }
    track.addEventListener("click", toggle);
    if (labelEl) {
      labelEl.addEventListener("click", toggle);
    }
    track.addEventListener("keydown", (e) => {
      if (e.key === " " || e.key === "Enter") {
        e.preventDefault();
        toggle();
      }
    });
    render3();
    return {
      el: wrapper,
      setChecked: (v) => {
        checked = v;
        render3();
      },
      setDisabled: (v) => {
        disabled = v;
        render3();
      },
      getChecked: () => checked
    };
  }

  // web/taskmaster/js/designer.ts
  function shQuote(s) {
    if (s === "") return "''";
    if (/^[A-Za-z0-9_\-./:=@%,]+$/.test(s)) return s;
    return "'" + s.replace(/'/g, `'\\''`) + "'";
  }
  function buildCtlExport(f) {
    const origin = window.location.origin;
    const parts = ["taskmasterctl", "-url", shQuote(origin), "-key", '"$API_KEY"', "task", "add"];
    parts.push("-name", shQuote(f.name || "<name>"));
    parts.push("-lane", shQuote(f.lane || "<lane>"));
    parts.push("-command", shQuote(f.command || "<command>"));
    if (f.repeat) parts.push("-repeat", "-cooldown", String(f.cooldown || 60));
    if (f.sudo) parts.push("-sudo");
    if (f.outputFile) parts.push("-output-file", shQuote(f.outputFile));
    return parts.join(" ");
  }
  function buildTaskBody(f) {
    const body = {
      name: f.name || "<name>",
      lane_name: f.lane || "<lane>",
      command: f.command || "<command>",
      enabled: true,
      repeat: f.repeat,
      cooldown_seconds: f.repeat ? f.cooldown || 60 : 0,
      sudo: f.sudo
    };
    if (f.outputFile) body.output_file = f.outputFile;
    return body;
  }
  function buildCurlExport(f) {
    const body = JSON.stringify(buildTaskBody(f), null, 2);
    const origin = window.location.origin;
    return `curl -X POST ${origin}/api/tasks \\
  -H 'Content-Type: application/json' \\
  -H "Authorization: Bearer $API_KEY" \\
  -d '${body.replace(/'/g, `'\\''`)}'`;
  }
  function buildExportPanel(title, render3) {
    const wrap = document.createElement("div");
    wrap.className = "export-panel";
    const header = document.createElement("button");
    header.type = "button";
    header.className = "export-panel-header";
    const caret = document.createElement("span");
    caret.className = "export-panel-caret";
    caret.textContent = "\u25B8";
    const label = document.createElement("span");
    label.textContent = title;
    header.append(caret, label);
    const body = document.createElement("div");
    body.className = "export-panel-body";
    body.hidden = true;
    const pre = document.createElement("pre");
    pre.className = "export-panel-code";
    const copyBtn = document.createElement("button");
    copyBtn.type = "button";
    copyBtn.className = "btn btn-secondary btn-sm export-panel-copy";
    copyBtn.textContent = "Copy";
    copyBtn.addEventListener("click", () => {
      void navigator.clipboard.writeText(pre.textContent ?? "").then(
        () => {
          copyBtn.textContent = "Copied";
          setTimeout(() => copyBtn.textContent = "Copy", 1200);
        },
        () => {
          copyBtn.textContent = "Copy failed";
          setTimeout(() => copyBtn.textContent = "Copy", 1200);
        }
      );
    });
    body.append(pre, copyBtn);
    wrap.append(header, body);
    header.addEventListener("click", () => {
      body.hidden = !body.hidden;
      caret.textContent = body.hidden ? "\u25B8" : "\u25BE";
    });
    function refresh2() {
      pre.textContent = render3();
    }
    refresh2();
    return { el: wrap, refresh: refresh2 };
  }
  async function openTaskDesigner(lanes, preselectLane, caps3) {
    const content = document.createElement("div");
    content.className = "designer-form";
    const nameGroup = document.createElement("div");
    nameGroup.className = "form-group";
    const nameLabel = document.createElement("label");
    nameLabel.textContent = "Task name";
    const nameInput = document.createElement("input");
    nameInput.type = "text";
    nameInput.placeholder = "e.g. nightly-backup";
    nameGroup.append(nameLabel, nameInput);
    const cmdGroup = document.createElement("div");
    cmdGroup.className = "form-group";
    const cmdLabel = document.createElement("label");
    cmdLabel.textContent = "Command";
    const cmdHint = document.createElement("span");
    cmdHint.className = "form-hint";
    cmdHint.textContent = " \u2014 run as a single shell string (sh -c)";
    cmdLabel.appendChild(cmdHint);
    const cmdInput = document.createElement("textarea");
    cmdInput.rows = 3;
    cmdInput.placeholder = "e.g. /usr/local/bin/backup.sh --quiet";
    cmdGroup.append(cmdLabel, cmdInput);
    const laneGroup = document.createElement("div");
    laneGroup.className = "form-group";
    const laneLabel = document.createElement("label");
    laneLabel.textContent = "Lane";
    const laneSelect = document.createElement("select");
    for (const lane of lanes) {
      const opt = document.createElement("option");
      opt.value = lane.name;
      opt.textContent = lane.name;
      if (lane.name === preselectLane) opt.selected = true;
      laneSelect.appendChild(opt);
    }
    laneGroup.append(laneLabel, laneSelect);
    content.append(nameGroup, cmdGroup, laneGroup);
    const repeatRow = document.createElement("div");
    repeatRow.className = "menu-row form-toggle-row";
    const repeatLabel = document.createElement("span");
    repeatLabel.textContent = "Repeat (re-enqueue after cooldown)";
    const repeatToggle = createToggleHandle({
      checked: false,
      onChange: (checked) => {
        cooldownGroup.hidden = !checked;
        refreshExports();
      }
    });
    repeatRow.append(repeatLabel, repeatToggle.el);
    const cooldownGroup = document.createElement("div");
    cooldownGroup.className = "form-group";
    cooldownGroup.hidden = true;
    const cooldownLabel2 = document.createElement("label");
    cooldownLabel2.textContent = "Cooldown seconds (minimum rest between runs)";
    const cooldownInput = document.createElement("input");
    cooldownInput.type = "number";
    cooldownInput.min = "0";
    cooldownInput.value = "60";
    cooldownGroup.append(cooldownLabel2, cooldownInput);
    content.append(repeatRow, cooldownGroup);
    const advToggleBtn = document.createElement("button");
    advToggleBtn.type = "button";
    advToggleBtn.className = "advanced-toggle";
    const advCaret = document.createElement("span");
    advCaret.className = "export-panel-caret";
    advCaret.textContent = "\u25B8";
    advToggleBtn.append(advCaret, document.createTextNode("Advanced"));
    const advBody = document.createElement("div");
    advBody.className = "advanced-body";
    advBody.hidden = true;
    const outputGroup = document.createElement("div");
    outputGroup.className = "form-group";
    const outputLabel = document.createElement("label");
    outputLabel.textContent = "Output file (optional; tees stdout/stderr)";
    const outputInput = document.createElement("input");
    outputInput.type = "text";
    outputInput.placeholder = "e.g. /var/log/taskmaster/{task}-{exec_id}.log";
    outputGroup.append(outputLabel, outputInput);
    advBody.append(outputGroup);
    let sudoToggle = null;
    if (caps3.allow_sudo) {
      const sudoRow = document.createElement("div");
      sudoRow.className = "menu-row form-toggle-row";
      const sudoLabel = document.createElement("span");
      sudoLabel.textContent = "Run with sudo";
      sudoToggle = createToggleHandle({ checked: false, onChange: () => refreshExports() });
      sudoRow.append(sudoLabel, sudoToggle.el);
      advBody.appendChild(sudoRow);
    }
    advToggleBtn.addEventListener("click", () => {
      advBody.hidden = !advBody.hidden;
      advCaret.textContent = advBody.hidden ? "\u25B8" : "\u25BE";
    });
    content.append(advToggleBtn, advBody);
    function currentForm() {
      return {
        name: nameInput.value.trim(),
        command: cmdInput.value.trim(),
        lane: laneSelect.value,
        repeat: repeatToggle.getChecked(),
        cooldown: parseInt(cooldownInput.value, 10) || 0,
        outputFile: outputInput.value.trim(),
        sudo: sudoToggle ? sudoToggle.getChecked() : false
      };
    }
    const ctlPanel = buildExportPanel("taskmasterctl export", () => buildCtlExport(currentForm()));
    const curlPanel = buildExportPanel("curl export", () => buildCurlExport(currentForm()));
    content.append(ctlPanel.el, curlPanel.el);
    function refreshExports() {
      ctlPanel.refresh();
      curlPanel.refresh();
    }
    nameInput.addEventListener("input", refreshExports);
    cmdInput.addEventListener("input", refreshExports);
    laneSelect.addEventListener("change", refreshExports);
    cooldownInput.addEventListener("input", refreshExports);
    outputInput.addEventListener("input", refreshExports);
    const actions = document.createElement("div");
    actions.className = "form-actions";
    const cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "btn btn-secondary";
    cancelBtn.textContent = "Cancel";
    const addBtn = document.createElement("button");
    addBtn.type = "button";
    addBtn.className = "btn btn-primary";
    addBtn.textContent = "Add to lane";
    actions.append(cancelBtn, addBtn);
    content.appendChild(actions);
    return new Promise((resolve) => {
      const handle = openModal(content, { title: "Add task", onClose: () => resolve() });
      cancelBtn.addEventListener("click", () => handle.close());
      addBtn.addEventListener("click", () => {
        void (async () => {
          const f = currentForm();
          if (!f.name || !f.command || !f.lane) {
            await alertDialog("Name, command, and lane are all required.");
            return;
          }
          try {
            await api.addTask(buildTaskBody(f));
            handle.close();
          } catch {
          }
        })();
      });
      nameInput.focus();
    });
  }

  // web/taskmaster/js/outputmodal.ts
  var STYLE_ATTR3 = "data-tm-output-modal-styles";
  function ensureStyles3() {
    if (document.head.querySelector("style[" + STYLE_ATTR3 + "]")) return;
    const style = document.createElement("style");
    style.setAttribute(STYLE_ATTR3, "");
    style.textContent = ".output-modal-panel { max-width: min(90vw, 900px); width: 90vw; }\n.output-modal-box { height: 70vh; max-height: 70vh; overflow: auto; margin: 0; white-space: pre-wrap; word-break: break-word; font-family: var(--font-mono, monospace); font-size: 13px; }\n";
    document.head.appendChild(style);
  }
  function parseOutputLine(raw) {
    try {
      const data = JSON.parse(raw);
      return data.line ?? "";
    } catch {
      return null;
    }
  }
  function appendOutputLine(box, ev) {
    const line = parseOutputLine(ev.data);
    if (line === null) return;
    box.textContent += line + "\n";
    box.scrollTop = box.scrollHeight;
  }
  function showStatusIfEmpty(box, statusText) {
    if (box.textContent === "") box.textContent = "(execution " + statusText + ")";
  }
  function markDoneIfEmpty(box) {
    if (box.textContent === "") box.textContent = "(no output captured for this run)";
  }
  function openOutputModal(execId, title) {
    ensureStyles3();
    const box = document.createElement("pre");
    box.className = "output-modal-box";
    box.textContent = "";
    const source = api.openExecutionOutput(execId);
    wireOutputSource(source, box);
    const handle = openModal(box, { title, onClose: makeCloseSource(source) });
    handle.panel.classList.add("output-modal-panel");
    return handle;
  }
  function wireOutputSource(source, box) {
    source.addEventListener("output", makeAppendHandler(box));
    source.addEventListener("status", makeStatusHandler(box));
    source.addEventListener("done", makeDoneHandler(box, source));
  }
  function makeAppendHandler(box) {
    function onOutput(ev) {
      appendOutputLine(box, ev);
    }
    return onOutput;
  }
  function makeStatusHandler(box) {
    function onStatus(ev) {
      showStatusIfEmpty(box, ev.data);
    }
    return onStatus;
  }
  function makeDoneHandler(box, source) {
    function onDone() {
      markDoneIfEmpty(box);
      source.close();
    }
    return onDone;
  }
  function makeCloseSource(source) {
    function closeSource() {
      source.close();
    }
    return closeSource;
  }

  // web/taskmaster/js/status.ts
  function statusBadgeClass(status) {
    if (status === "success") return "badge-green";
    if (status === "failed") return "badge-red";
    if (status === "canceled") return "badge-yellow";
    if (status === "suspended") return "badge-yellow";
    if (status === "running") return "badge-blue";
    return "badge-muted";
  }
  function statusSymbol(status) {
    if (status === "success") return "\u2713";
    if (status === "failed") return "\u2715";
    if (status === "canceled") return "\u2298";
    if (status === "suspended") return "\u23F8";
    if (status === "running") return "\u25CF";
    if (status === "pending") return "\u2026";
    return "\u2022";
  }
  function effectiveStatus(status, suspended) {
    return status === "running" && suspended ? "suspended" : status;
  }
  function renderStatusBadge(badge, status, suspended) {
    const eff = effectiveStatus(status, suspended);
    badge.className = "badge badge-symbol " + statusBadgeClass(eff);
    badge.textContent = statusSymbol(eff);
    badge.title = eff;
    badge.setAttribute("aria-label", eff);
  }
  function pad2(n) {
    return String(n).padStart(2, "0");
  }
  function fmtDurationSeconds(totalSec) {
    const h = Math.floor(totalSec / 3600);
    const m = Math.floor(totalSec % 3600 / 60);
    const s = totalSec % 60;
    if (h > 0) return h + ":" + pad2(m) + ":" + pad2(s);
    return m + ":" + pad2(s);
  }
  function fmtElapsed(startedAt) {
    const startMs = new Date(startedAt).getTime();
    if (Number.isNaN(startMs)) return "running\u2026";
    const totalSec = Math.max(0, Math.floor((Date.now() - startMs) / 1e3));
    return fmtDurationSeconds(totalSec);
  }
  function fmtMs(ms) {
    if (ms === null || ms === void 0) return "\u2014";
    return (ms / 1e3).toFixed(2) + "s";
  }
  function fmtDate(iso) {
    if (!iso) return "\u2014";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleString();
  }

  // web/taskmaster/js/board.ts
  var RAN_PER_LANE = 5;
  var state = { lanes: [], tasks: [], executions: [] };
  var boardEl = null;
  var unsubscribeEvent = null;
  var countdownTimer;
  var loadSeq = 0;
  var caps = { allow_sudo: false };
  var laneFilter;
  function openTaskRoute(taskName) {
    window.location.hash = "#task/" + encodeURIComponent(taskName);
  }
  function mountBoard(container, live2, capabilities, options = {}) {
    container.textContent = "";
    caps = capabilities;
    laneFilter = options.laneFilter;
    if (!laneFilter) {
      const toolbar = document.createElement("div");
      toolbar.className = "board-toolbar";
      const addLaneBtn = document.createElement("button");
      addLaneBtn.className = "btn btn-secondary";
      addLaneBtn.textContent = "+ new lane";
      addLaneBtn.addEventListener("click", () => void openAddLaneModal());
      toolbar.appendChild(addLaneBtn);
      container.appendChild(toolbar);
    }
    boardEl = document.createElement("div");
    boardEl.className = "board-lanes" + (laneFilter ? " board-lanes-single" : "");
    container.appendChild(boardEl);
    void refreshAll();
    unsubscribeEvent = live2.onEvent((ev) => void handleBoardEvent(ev));
    countdownTimer = window.setInterval(() => render(), 1e3);
    return () => {
      if (unsubscribeEvent) unsubscribeEvent();
      unsubscribeEvent = null;
      if (countdownTimer !== void 0) {
        clearInterval(countdownTimer);
        countdownTimer = void 0;
      }
      boardEl = null;
    };
  }
  async function handleBoardEvent(ev) {
    void ev;
    await refreshAll();
  }
  async function refreshAll() {
    const seq = ++loadSeq;
    try {
      const [lanes, tasks, executions] = await Promise.all([
        api.listLanes(),
        api.listTasks(),
        api.listExecutions(void 0, 300)
      ]);
      if (seq !== loadSeq) return;
      state.lanes = lanes;
      state.tasks = tasks;
      state.executions = executions;
      render();
    } catch {
    }
  }
  function tasksByLane(laneName) {
    return state.tasks.filter((t) => t.lane_name === laneName).sort((a, b) => a.position - b.position);
  }
  function executionsByTask(taskName) {
    return state.executions.filter((e) => e.task_name === taskName);
  }
  function render() {
    if (!boardEl) return;
    const visible = laneFilter ? state.lanes.filter((l) => l.name === laneFilter) : state.lanes;
    if (visible.length === 0) {
      if (!boardEl.querySelector(".empty-state")) {
        boardEl.textContent = "";
        const empty = document.createElement("div");
        empty.className = "empty-state";
        empty.textContent = laneFilter ? "Lane not found." : "No lanes yet. Create one to start running tasks.";
        boardEl.appendChild(empty);
      }
      return;
    }
    boardEl.querySelector(".empty-state")?.remove();
    const sorted = [...visible].sort((a, b) => a.name.localeCompare(b.name));
    patchList(boardEl, sorted, {
      key: (l) => l.name,
      create: (l) => createLaneEl(l),
      update: (el, l) => updateLaneEl(el, l)
    });
  }
  function createLaneEl(lane) {
    const el = document.createElement("section");
    el.className = "lane";
    const header = document.createElement("div");
    header.className = "lane-header";
    const nameEl = document.createElement("span");
    nameEl.className = "lane-name";
    header.appendChild(nameEl);
    const pauseWrap = document.createElement("div");
    pauseWrap.className = "lane-pause";
    const pauseBtn = document.createElement("button");
    pauseBtn.type = "button";
    pauseBtn.className = "btn-icon lane-pause-btn";
    const pauseLabel = document.createElement("span");
    pauseLabel.className = "lane-pause-label";
    pauseLabel.textContent = "Paused";
    pauseWrap.append(pauseBtn, pauseLabel);
    header.appendChild(pauseWrap);
    const widthWrap = document.createElement("div");
    widthWrap.className = "lane-width";
    const widthDown = document.createElement("button");
    widthDown.type = "button";
    widthDown.className = "lane-width-btn";
    widthDown.textContent = "\u2212";
    widthDown.setAttribute("aria-label", "Decrease lane width");
    const widthVal = document.createElement("span");
    widthVal.className = "lane-width-val";
    const widthUp = document.createElement("button");
    widthUp.type = "button";
    widthUp.className = "lane-width-btn";
    widthUp.textContent = "+";
    widthUp.setAttribute("aria-label", "Increase lane width");
    widthWrap.append(widthDown, widthVal, widthUp);
    header.appendChild(widthWrap);
    const deleteBtn = document.createElement("button");
    deleteBtn.type = "button";
    deleteBtn.className = "lane-delete-btn";
    deleteBtn.title = "Delete lane";
    deleteBtn.setAttribute("aria-label", "Delete lane");
    deleteBtn.textContent = "\xD7";
    header.appendChild(deleteBtn);
    el.appendChild(header);
    const addBtn = document.createElement("button");
    addBtn.className = "btn btn-primary btn-sm lane-add-task";
    addBtn.textContent = "+ add task";
    el.appendChild(addBtn);
    const body = document.createElement("div");
    body.className = "lane-body";
    const runningSection = buildSection("running", "Running");
    const upNextSection = buildSection("upnext", "Up next");
    const ranSection = buildRanSection();
    body.append(runningSection.wrap, upNextSection.wrap, ranSection.wrap);
    el.appendChild(body);
    wireDropTarget(upNextSection.list, el);
    updateLaneEl(el, lane);
    return el;
  }
  function buildSection(kind, title) {
    const wrap = document.createElement("div");
    wrap.className = "lane-section lane-section-" + kind;
    const h = document.createElement("div");
    h.className = "lane-section-title";
    h.textContent = title;
    const list = document.createElement("div");
    list.className = "lane-list lane-list-" + kind;
    wrap.append(h, list);
    return { wrap, list };
  }
  function buildRanSection() {
    const wrap = document.createElement("details");
    wrap.className = "lane-section lane-section-ran";
    const summary = document.createElement("summary");
    summary.className = "lane-section-title";
    summary.textContent = "Recent runs";
    const list = document.createElement("div");
    list.className = "lane-list lane-list-ran";
    wrap.append(summary, list);
    return { wrap, list };
  }
  function updateLaneEl(el, lane) {
    el.setAttribute("data-lane", lane.name);
    el.classList.toggle("lane-paused", lane.paused);
    const nameEl = el.querySelector(".lane-name");
    if (nameEl) {
      nameEl.textContent = lane.name;
    }
    const pauseBtn = el.querySelector(".lane-pause-btn");
    if (pauseBtn) {
      pauseBtn.textContent = lane.paused ? "\u25B6" : "\u23F9";
      pauseBtn.title = lane.paused ? "Resume lane" : "Stop lane (finish current run, start nothing new)";
      pauseBtn.setAttribute("aria-label", pauseBtn.title);
      pauseBtn.onclick = () => {
        pauseBtn.disabled = true;
        const req = lane.paused ? api.resumeLane(lane.name) : api.pauseLane(lane.name);
        void req.finally(() => {
          pauseBtn.disabled = false;
          void refreshAll();
        });
      };
    }
    const pauseLabel = el.querySelector(".lane-pause-label");
    if (pauseLabel) pauseLabel.style.display = lane.paused ? "" : "none";
    const widthVal = el.querySelector(".lane-width-val");
    if (widthVal) widthVal.textContent = String(lane.width);
    const widthDown = el.querySelector(".lane-width-btn:first-child");
    const widthUp = el.querySelector(".lane-width-btn:last-of-type");
    if (widthDown) {
      widthDown.disabled = lane.width <= 1;
      widthDown.onclick = () => void changeWidth(lane, lane.width - 1);
    }
    if (widthUp) {
      widthUp.onclick = () => void changeWidth(lane, lane.width + 1);
    }
    const deleteBtn = el.querySelector(".lane-delete-btn");
    if (deleteBtn) {
      deleteBtn.onclick = () => void deleteLane(lane.name);
    }
    const addBtn = el.querySelector(".lane-add-task");
    if (addBtn) {
      addBtn.onclick = () => void openTaskDesigner(state.lanes, lane.name, caps).then(() => refreshAll());
    }
    const laneTasks = tasksByLane(lane.name);
    const laneTaskNames = new Set(laneTasks.map((t) => t.name));
    const laneExecs = state.executions.filter((e) => e.task_name && laneTaskNames.has(e.task_name));
    const runningExecs = laneExecs.filter((e) => e.status === "running").sort((a, b) => b.id - a.id);
    const ranExecs = laneExecs.filter((e) => e.status === "success" || e.status === "failed" || e.status === "canceled").sort((a, b) => b.id - a.id).slice(0, RAN_PER_LANE);
    const runningTaskNames = new Set(runningExecs.map((e) => e.task_name));
    const pendingByTask = /* @__PURE__ */ new Map();
    for (const e of laneExecs) {
      if (e.status === "pending" && e.task_name && !pendingByTask.has(e.task_name)) {
        pendingByTask.set(e.task_name, e);
      }
    }
    const upNextTasks = laneTasks.filter((t) => !runningTaskNames.has(t.name));
    const runningList = el.querySelector(".lane-list-running");
    if (runningList) {
      patchList(runningList, runningExecs, {
        key: (e) => e.id,
        create: (e) => createExecRow(e, "running"),
        update: (row, e) => updateExecRow(row, e, "running")
      });
      toggleEmptyNote(runningList, runningExecs.length === 0, "nothing running");
    }
    const ranList = el.querySelector(".lane-list-ran");
    if (ranList) {
      patchList(ranList, ranExecs, {
        key: (e) => e.id,
        create: (e) => createExecRow(e, "ran"),
        update: (row, e) => updateExecRow(row, e, "ran")
      });
      toggleEmptyNote(ranList, ranExecs.length === 0, "no history yet");
    }
    const upNextList = el.querySelector(".lane-list-upnext");
    if (upNextList) {
      patchList(upNextList, upNextTasks, {
        key: (t) => t.name,
        create: (t) => createTaskRow(t, pendingByTask.get(t.name) ?? null),
        update: (row, t) => updateTaskRow(row, t, pendingByTask.get(t.name) ?? null)
      });
      toggleEmptyNote(upNextList, upNextTasks.length === 0, "lane is empty");
    }
  }
  function toggleEmptyNote(list, empty, text) {
    let note = list.querySelector(".lane-empty-note");
    if (empty) {
      if (!note) {
        note = document.createElement("div");
        note.className = "lane-empty-note";
        note.setAttribute("data-tm-key", "__empty__");
        list.appendChild(note);
      }
      note.textContent = text;
    } else {
      note?.remove();
    }
  }
  function createExecRow(exec, kind) {
    const row = document.createElement("div");
    row.className = "exec-row exec-row-" + kind;
    const name = document.createElement("span");
    name.className = "exec-row-name exec-row-name-clickable";
    name.setAttribute("role", "button");
    name.tabIndex = 0;
    const badge = document.createElement("span");
    badge.className = "badge";
    const meta = document.createElement("span");
    meta.className = "exec-row-meta";
    row.append(name, badge, meta);
    if (kind === "running") {
      const pidSeam = document.createElement("span");
      pidSeam.className = "exec-row-pid-seam";
      const pidLabel = document.createElement("span");
      pidLabel.className = "exec-row-pid";
      const outputBtn = document.createElement("button");
      outputBtn.type = "button";
      outputBtn.className = "btn-icon exec-row-output-btn";
      outputBtn.textContent = "\u2197";
      outputBtn.title = "View live output";
      outputBtn.setAttribute("aria-label", "View live output");
      const pauseBtn = document.createElement("button");
      pauseBtn.type = "button";
      pauseBtn.className = "btn-icon exec-row-pause-btn";
      const cancelBtn = document.createElement("button");
      cancelBtn.type = "button";
      cancelBtn.className = "btn-icon exec-row-cancel-btn";
      cancelBtn.textContent = "\u2716";
      cancelBtn.title = "Cancel execution";
      cancelBtn.setAttribute("aria-label", "Cancel execution");
      pidSeam.append(pidLabel, outputBtn, pauseBtn, cancelBtn);
      row.appendChild(pidSeam);
    }
    updateExecRow(row, exec, kind);
    return row;
  }
  function openRunningOutput(exec) {
    const title = (exec.task_name ?? "task") + " \u2014 run #" + exec.id;
    openOutputModal(exec.id, title);
  }
  function requestProcessToggle(btn, execId, suspended) {
    btn.disabled = true;
    const request = suspended ? api.resumeExecution(execId) : api.pauseExecution(execId);
    finishProcessToggle(request, btn);
  }
  function finishProcessToggle(request, btn) {
    request.then(reenableProcessButton(btn), reenableProcessButton(btn));
  }
  function reenableProcessButton(btn) {
    function reenable() {
      btn.disabled = false;
      void refreshAll();
    }
    return reenable;
  }
  function wireProcessToggle(btn, exec) {
    const suspended = !!exec.suspended;
    btn.textContent = suspended ? "\u25B6" : "\u23F8";
    btn.title = suspended ? "Resume process" : "Pause process";
    btn.setAttribute("aria-label", btn.title);
    btn.onclick = makeProcessToggleHandler(btn, exec.id, suspended);
  }
  function makeProcessToggleHandler(btn, execId, suspended) {
    function handleClick() {
      requestProcessToggle(btn, execId, suspended);
    }
    return handleClick;
  }
  function requestCancel(btn, execId) {
    btn.disabled = true;
    api.cancelExecution(execId).then(reenableCancelButton(btn), reenableCancelButton(btn));
  }
  function reenableCancelButton(btn) {
    function reenable() {
      btn.disabled = false;
      void refreshAll();
    }
    return reenable;
  }
  function wireCancelButton(btn, exec) {
    btn.onclick = makeCancelHandler(btn, exec.id);
  }
  function makeCancelHandler(btn, execId) {
    function handleClick() {
      requestCancel(btn, execId);
    }
    return handleClick;
  }
  function wireOutputButton(btn, exec) {
    btn.onclick = makeOutputHandler(exec);
  }
  function makeOutputHandler(exec) {
    function handleClick() {
      openRunningOutput(exec);
    }
    return handleClick;
  }
  function updateExecRow(row, exec, kind) {
    const name = row.querySelector(".exec-row-name");
    if (name) {
      name.textContent = exec.task_name ?? "(unknown task)";
      const taskName = exec.task_name;
      const openDetail = () => {
        if (!taskName) return;
        openTaskRoute(taskName);
      };
      name.onclick = openDetail;
      name.onkeydown = (e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          openDetail();
        }
      };
    }
    const badge = row.querySelector(".badge");
    if (badge) renderStatusBadge(badge, exec.status, exec.suspended);
    const meta = row.querySelector(".exec-row-meta");
    if (meta) {
      if (exec.duration_ms !== void 0 && exec.duration_ms !== null) {
        meta.textContent = (exec.duration_ms / 1e3).toFixed(1) + "s";
      } else if (exec.suspended) {
        meta.textContent = "paused";
      } else if (exec.started_at) {
        meta.textContent = fmtElapsed(exec.started_at);
      } else {
        meta.textContent = "";
      }
    }
    if (kind === "running") {
      const pidLabel = row.querySelector(".exec-row-pid");
      if (pidLabel) pidLabel.textContent = exec.pid !== void 0 ? "pid " + exec.pid : "";
      const outputBtn = row.querySelector(".exec-row-output-btn");
      if (outputBtn) wireOutputButton(outputBtn, exec);
      const pauseBtn = row.querySelector(".exec-row-pause-btn");
      if (pauseBtn) wireProcessToggle(pauseBtn, exec);
      const cancelBtn = row.querySelector(".exec-row-cancel-btn");
      if (cancelBtn) wireCancelButton(cancelBtn, exec);
    }
  }
  function createTaskRow(task, pending) {
    const row = document.createElement("div");
    row.className = "task-row";
    row.draggable = true;
    const name = document.createElement("span");
    name.className = "task-row-name task-row-name-clickable";
    name.setAttribute("role", "button");
    name.tabIndex = 0;
    const openDetail = () => {
      openTaskRoute(task.name);
    };
    name.addEventListener("click", openDetail);
    name.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        openDetail();
      }
    });
    const status = document.createElement("span");
    status.className = "task-row-status";
    const upNextBtn = document.createElement("button");
    upNextBtn.type = "button";
    upNextBtn.className = "btn btn-secondary btn-sm task-row-upnext";
    upNextBtn.textContent = "Up next";
    row.append(name, status, upNextBtn);
    row.addEventListener("dragstart", (e) => {
      if (!e.dataTransfer) return;
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", JSON.stringify({ task: task.name, lane: task.lane_name }));
      row.classList.add("dragging");
    });
    row.addEventListener("dragend", () => row.classList.remove("dragging"));
    updateTaskRow(row, task, pending);
    return row;
  }
  function updateTaskRow(row, task, pending) {
    row.setAttribute("data-task", task.name);
    row.classList.toggle("task-row-disabled", !task.enabled || task.paused);
    const name = row.querySelector(".task-row-name");
    if (name) name.textContent = task.name;
    const status = row.querySelector(".task-row-status");
    if (status) {
      if (!task.enabled) {
        status.textContent = "disabled";
      } else if (task.paused) {
        status.textContent = "paused";
      } else if (pending) {
        status.textContent = "queued";
      } else if (task.repeat) {
        status.textContent = cooldownLabel(task);
      } else {
        status.textContent = "ready";
      }
    }
    const btn = row.querySelector(".task-row-upnext");
    if (btn) {
      btn.disabled = !task.enabled || task.paused;
      btn.onclick = () => void api.upNext(task.name).then(() => refreshAll());
    }
  }
  function cooldownLabel(task) {
    const execs = executionsByTask(task.name).filter((e) => e.finished_at).sort((a, b) => b.id - a.id);
    const last = execs[0];
    if (!last || !last.finished_at) return "ready";
    const readyAt = new Date(last.finished_at).getTime() + task.cooldown_seconds * 1e3;
    const remainingMs = readyAt - Date.now();
    if (remainingMs <= 0) return "ready";
    const totalSec = Math.ceil(remainingMs / 1e3);
    const m = Math.floor(totalSec / 60);
    const s = totalSec % 60;
    return "again in " + m + ":" + String(s).padStart(2, "0");
  }
  function wireDropTarget(list, laneEl) {
    list.addEventListener("dragover", (e) => {
      e.preventDefault();
      if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
      list.classList.add("drag-over");
      const dragging = list.querySelector(".dragging");
      const after = rowAfter(list, e.clientY);
      if (dragging) {
        if (after == null) {
          list.appendChild(dragging);
        } else if (after !== dragging) {
          list.insertBefore(dragging, after);
        }
      }
    });
    list.addEventListener("dragleave", (e) => {
      if (e.target === list) list.classList.remove("drag-over");
    });
    list.addEventListener("drop", (e) => {
      e.preventDefault();
      list.classList.remove("drag-over");
      const raw = e.dataTransfer?.getData("text/plain");
      if (!raw) return;
      let payload;
      try {
        payload = JSON.parse(raw);
      } catch {
        return;
      }
      const destLane = laneEl.getAttribute("data-lane");
      if (!destLane) return;
      void handleDrop(payload.task, payload.lane, destLane, list);
    });
  }
  function rowAfter(list, y) {
    const rows = Array.from(list.querySelectorAll(".task-row:not(.dragging)"));
    let closest = null;
    for (const row of rows) {
      const box = row.getBoundingClientRect();
      const offset = y - box.top - box.height / 2;
      if (offset < 0 && (closest === null || offset > closest.offset)) {
        closest = { el: row, offset };
      }
    }
    return closest ? closest.el : null;
  }
  async function handleDrop(taskName, sourceLane, destLane, list) {
    const currentOrder = Array.from(list.querySelectorAll(".task-row")).map(
      (r) => r.getAttribute("data-task")
    );
    try {
      if (sourceLane !== destLane) {
        await api.moveTask(taskName, destLane);
      }
      await api.setLaneOrder(destLane, currentOrder);
    } finally {
      await refreshAll();
    }
  }
  async function changeWidth(lane, width) {
    if (width < 1) return;
    try {
      await api.setLaneWidth(lane.name, width);
    } finally {
      await refreshAll();
    }
  }
  async function deleteLane(name) {
    const ok = await confirmDialog('Delete lane "' + name + '"? Tasks in it must be moved or removed first.', {
      title: "Delete lane",
      confirmLabel: "Delete"
    });
    if (!ok) return;
    try {
      await api.deleteLane(name);
    } finally {
      await refreshAll();
    }
  }
  async function openAddLaneModal() {
    const content = document.createElement("div");
    const nameGroup = document.createElement("div");
    nameGroup.className = "form-group";
    const nameLabel = document.createElement("label");
    nameLabel.textContent = "Lane name";
    const nameInput = document.createElement("input");
    nameInput.type = "text";
    nameInput.placeholder = "e.g. backups";
    nameGroup.append(nameLabel, nameInput);
    const widthGroup = document.createElement("div");
    widthGroup.className = "form-group";
    const widthLabel = document.createElement("label");
    widthLabel.textContent = "Width (concurrent slots)";
    const widthInput = document.createElement("input");
    widthInput.type = "number";
    widthInput.min = "1";
    widthInput.value = "1";
    widthGroup.append(widthLabel, widthInput);
    const actions = document.createElement("div");
    actions.className = "form-actions";
    const cancelBtn = document.createElement("button");
    cancelBtn.type = "button";
    cancelBtn.className = "btn btn-secondary";
    cancelBtn.textContent = "Cancel";
    const createBtn = document.createElement("button");
    createBtn.type = "button";
    createBtn.className = "btn btn-primary";
    createBtn.textContent = "Create lane";
    actions.append(cancelBtn, createBtn);
    content.append(nameGroup, widthGroup, actions);
    const handle = openModal(content, { title: "New lane" });
    cancelBtn.addEventListener("click", () => handle.close());
    createBtn.addEventListener("click", () => {
      void (async () => {
        const name = nameInput.value.trim();
        const width = parseInt(widthInput.value, 10) || 1;
        if (!name) {
          await alertDialog("Lane name is required.");
          return;
        }
        try {
          await api.createLane({ name, width });
          handle.close();
          await refreshAll();
        } catch {
        }
      })();
    });
    nameInput.focus();
  }

  // web/taskmaster/js/metrics.ts
  function fmtMs2(ms) {
    if (ms === null || ms === void 0) return "\u2014";
    return (ms / 1e3).toFixed(2) + "s";
  }
  function fmtDate2(iso) {
    if (!iso) return "\u2014";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return iso;
    return d.toLocaleString();
  }
  var gridEl = null;
  var unsubscribe = null;
  var loadSeq2 = 0;
  function mountMetrics(container, live2) {
    container.textContent = "";
    const heading = document.createElement("div");
    heading.className = "metrics-heading";
    heading.textContent = "Fleet-wide metrics (last 24h)";
    container.appendChild(heading);
    gridEl = document.createElement("div");
    gridEl.className = "metrics-grid";
    container.appendChild(gridEl);
    void refresh();
    unsubscribe = live2.onEvent(() => void refresh());
    return () => {
      if (unsubscribe) unsubscribe();
      unsubscribe = null;
      gridEl = null;
    };
  }
  async function refresh() {
    const seq = ++loadSeq2;
    let rows = [];
    try {
      rows = await api.getMetrics();
    } catch {
      return;
    }
    if (seq !== loadSeq2 || !gridEl) return;
    render2(rows);
  }
  function render2(rows) {
    if (!gridEl) return;
    if (rows.length === 0) {
      if (!gridEl.querySelector(".empty-state")) {
        gridEl.textContent = "";
        const empty = document.createElement("div");
        empty.className = "empty-state";
        empty.textContent = "No executions recorded yet.";
        gridEl.appendChild(empty);
      }
      return;
    }
    gridEl.querySelector(".empty-state")?.remove();
    const sorted = [...rows].sort((a, b) => a.task_name.localeCompare(b.task_name));
    patchList(gridEl, sorted, {
      key: (m) => m.task_name,
      create: (m) => createCard(m),
      update: (el, m) => updateCard(el, m)
    });
  }
  function createCard(m) {
    const card = document.createElement("div");
    card.className = "metric-card";
    const title = document.createElement("div");
    title.className = "metric-card-title";
    card.appendChild(title);
    const lane = document.createElement("div");
    lane.className = "metric-card-lane";
    card.appendChild(lane);
    const stats = document.createElement("div");
    stats.className = "metric-card-stats";
    const fields = ["success", "failed", "canceled", "avg", "min", "max", "last run"];
    for (const f of fields) {
      const cell = document.createElement("div");
      cell.className = "metric-card-stat metric-stat-" + f.replace(" ", "-");
      const v = document.createElement("div");
      v.className = "metric-card-stat-val";
      const l = document.createElement("div");
      l.className = "metric-card-stat-label";
      l.textContent = f;
      cell.append(v, l);
      stats.appendChild(cell);
    }
    card.appendChild(stats);
    updateCard(card, m);
    return card;
  }
  function updateCard(card, m) {
    const title = card.querySelector(".metric-card-title");
    if (title) title.textContent = m.task_name;
    const lane = card.querySelector(".metric-card-lane");
    if (lane) lane.textContent = "lane: " + m.group_name;
    const values = {
      success: String(m.success_count),
      failed: String(m.failed_count),
      canceled: String(m.canceled_count),
      avg: fmtMs2(m.avg_duration_ms),
      min: fmtMs2(m.min_duration_ms),
      max: fmtMs2(m.max_duration_ms),
      "last-run": fmtDate2(m.last_execution)
    };
    for (const [key, val] of Object.entries(values)) {
      const cell = card.querySelector(".metric-stat-" + key);
      const v = cell?.querySelector(".metric-card-stat-val");
      if (v) v.textContent = val;
    }
  }

  // web/taskmaster/js/taskdetail.ts
  function mountTaskDetail(container, live2, task, onBack) {
    container.textContent = "";
    container.className = "task-detail";
    const header = document.createElement("div");
    header.className = "task-detail-header";
    const backBtn = document.createElement("button");
    backBtn.type = "button";
    backBtn.className = "btn btn-secondary btn-sm task-detail-back";
    backBtn.textContent = "\u2190 Back to board";
    backBtn.addEventListener("click", onBack);
    const title = document.createElement("h2");
    title.className = "task-detail-title";
    title.textContent = task.name;
    header.append(backBtn, title);
    container.appendChild(header);
    const meta = document.createElement("div");
    meta.className = "task-detail-meta";
    const cmdLine = document.createElement("code");
    cmdLine.className = "task-detail-command";
    cmdLine.textContent = task.command;
    meta.appendChild(cmdLine);
    const laneLine = document.createElement("div");
    laneLine.className = "task-detail-sub";
    laneLine.textContent = "lane: " + task.lane_name + (task.repeat ? " \xB7 repeats, cooldown " + task.cooldown_seconds + "s" : " \xB7 one-shot") + (task.sudo ? " \xB7 sudo" : "");
    meta.appendChild(laneLine);
    container.appendChild(meta);
    const metricsWrap = document.createElement("div");
    metricsWrap.className = "task-detail-metrics";
    metricsWrap.textContent = "Loading metrics\u2026";
    container.appendChild(metricsWrap);
    const historyHeader = document.createElement("div");
    historyHeader.className = "task-detail-section-title";
    historyHeader.textContent = "History";
    const historyList = document.createElement("div");
    historyList.className = "task-detail-history";
    container.append(historyHeader, historyList);
    let destroyed = false;
    function isTerminal(exec) {
      return exec.status === "success" || exec.status === "failed" || exec.status === "canceled";
    }
    function byIdDescending(a, b) {
      return b.id - a.id;
    }
    function keyById(exec) {
      return exec.id;
    }
    async function loadHistory() {
      let execs = [];
      try {
        const all = await api.listExecutions(task.name, 50);
        execs = all.filter(isTerminal);
      } catch {
        return;
      }
      if (destroyed) return;
      execs.sort(byIdDescending);
      if (execs.length === 0) {
        historyList.textContent = "";
        const empty = document.createElement("div");
        empty.className = "lane-empty-note";
        empty.textContent = "no runs yet";
        historyList.appendChild(empty);
        return;
      }
      historyList.querySelector(".lane-empty-note")?.remove();
      patchList(historyList, execs, {
        key: keyById,
        create: createHistoryRow,
        update: updateHistoryRow
      });
    }
    function createHistoryRow(exec) {
      const row = document.createElement("div");
      row.className = "history-row";
      const badge = document.createElement("span");
      badge.className = "badge history-row-status-badge";
      const when = document.createElement("span");
      when.className = "history-row-when";
      const dur = document.createElement("span");
      dur.className = "history-row-dur";
      const viewBtn = document.createElement("button");
      viewBtn.type = "button";
      viewBtn.className = "btn btn-secondary btn-sm";
      viewBtn.textContent = "View output";
      row.append(badge, when, dur, viewBtn);
      updateHistoryRow(row, exec);
      return row;
    }
    function updateHistoryRow(row, exec) {
      const badge = row.querySelector(".history-row-status-badge");
      if (badge) renderStatusBadge(badge, exec.status, exec.suspended);
      const when = row.querySelector(".history-row-when");
      if (when) when.textContent = fmtDate(exec.started_at ?? exec.scheduled_at);
      const dur = row.querySelector(".history-row-dur");
      if (dur) dur.textContent = fmtMs(exec.duration_ms);
      const viewBtn = row.querySelector(".btn-secondary");
      if (viewBtn) viewBtn.onclick = makeViewOutputHandler(exec);
    }
    function makeViewOutputHandler(exec) {
      function handleClick() {
        openOutputModal(exec.id, task.name + " \u2014 run #" + exec.id);
      }
      return handleClick;
    }
    async function loadMetrics() {
      let rows = [];
      try {
        rows = await api.getMetrics(void 0, task.name);
      } catch {
        metricsWrap.textContent = "Metrics unavailable.";
        return;
      }
      if (destroyed) return;
      const m = rows.find((r) => r.task_name === task.name) ?? rows[0];
      if (!m) {
        metricsWrap.textContent = "No runs recorded yet.";
        return;
      }
      if (metricsWrap.textContent !== "" && metricsWrap.children.length === 0) {
        metricsWrap.textContent = "";
      }
      const stats = [
        ["success", String(m.success_count)],
        ["failed", String(m.failed_count)],
        ["canceled", String(m.canceled_count)],
        ["avg", fmtMs(m.avg_duration_ms ?? null)],
        ["min", fmtMs(m.min_duration_ms ?? null)],
        ["max", fmtMs(m.max_duration_ms ?? null)],
        ["last run", fmtDate(m.last_execution ?? null)]
      ];
      let grid = metricsWrap.querySelector(".task-detail-metric-grid");
      if (!grid) {
        metricsWrap.textContent = "";
        grid = document.createElement("div");
        grid.className = "task-detail-metric-grid";
        metricsWrap.appendChild(grid);
      }
      for (const [label, val] of stats) {
        const key = label.replace(/\s+/g, "-");
        let cell = grid.querySelector('[data-metric="' + key + '"]');
        if (!cell) {
          cell = document.createElement("div");
          cell.className = "task-detail-metric";
          cell.setAttribute("data-metric", key);
          const v2 = document.createElement("div");
          v2.className = "task-detail-metric-val";
          const l = document.createElement("div");
          l.className = "task-detail-metric-label";
          l.textContent = label;
          cell.append(v2, l);
          grid.appendChild(cell);
        }
        const v = cell.querySelector(".task-detail-metric-val");
        if (v) v.textContent = val;
      }
    }
    function refreshHistoryAndMetrics() {
      void loadHistory();
      void loadMetrics();
    }
    refreshHistoryAndMetrics();
    const unsubscribe2 = live2.onEvent(refreshHistoryAndMetrics);
    function cleanup() {
      destroyed = true;
      unsubscribe2();
    }
    return cleanup;
  }

  // web/taskmaster/js/taskview.ts
  function mountTaskView(container, live2, caps3, taskName) {
    container.textContent = "";
    const wrap = document.createElement("div");
    wrap.className = "split-view";
    const left = document.createElement("div");
    left.className = "split-left";
    const right = document.createElement("div");
    right.className = "split-right";
    wrap.append(left, right);
    container.appendChild(wrap);
    right.textContent = "Loading task\u2026";
    let unmountLeft = null;
    let unmountRight = null;
    let cancelled = false;
    const goBack = () => {
      window.location.hash = "#board";
    };
    void api.getTask(taskName).then((task) => {
      if (cancelled) return;
      unmountLeft = mountBoard(left, live2, caps3, { laneFilter: task.lane_name });
      right.textContent = "";
      unmountRight = mountTaskDetail(right, live2, task, goBack);
    }).catch(() => {
      if (cancelled) return;
      right.textContent = "";
      const err = document.createElement("div");
      err.className = "empty-state";
      err.textContent = "Task not found.";
      right.appendChild(err);
      const back = document.createElement("button");
      back.type = "button";
      back.className = "btn btn-secondary";
      back.textContent = "\u2190 Back to board";
      back.addEventListener("click", goBack);
      right.appendChild(back);
    });
    return () => {
      cancelled = true;
      if (unmountLeft) unmountLeft();
      if (unmountRight) unmountRight();
    };
  }

  // web/taskmaster/js/buildinfo.ts
  var FRONTEND_BUILD_TIME = "51233a1f8b0e";

  // web/taskmaster/js/main.ts
  var caps2 = { allow_sudo: false };
  var authEnabled = false;
  var brake = { engaged: false };
  var live = new LiveController();
  var LIVE_INTERVALS_SEC = [5, 10, 30, 60];
  var ICON_MENU = '<svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="18" x2="21" y2="18"/></svg>';
  var ICON_LOGOUT = '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>';
  var ICON_BRAKE = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"/><line x1="7" y1="7" x2="17" y2="17"/></svg>';
  var NAV_LINKS = [{ label: "Metrics", hash: "#metrics" }];
  function currentPage() {
    const hash = window.location.hash || "#board";
    return hash.slice(1).split("/")[0] || "board";
  }
  function currentTaskName() {
    const hash = window.location.hash || "";
    const parts = hash.slice(1).split("/");
    if (parts[0] === "task" && parts[1]) {
      try {
        return decodeURIComponent(parts[1]);
      } catch {
        return parts[1];
      }
    }
    return null;
  }
  function closeMenu() {
    const panel = document.getElementById("nav-menu-panel");
    const btn = document.getElementById("nav-menu-btn");
    if (panel) panel.hidden = true;
    if (btn) btn.setAttribute("aria-expanded", "false");
  }
  async function refreshStatusLine() {
    const statusEl = document.getElementById("st-status");
    const backendBuildEl = document.getElementById("st-backend-build");
    if (!statusEl) return;
    statusEl.textContent = "checking\u2026";
    statusEl.className = "st-value st-muted";
    try {
      const h = await api.health();
      statusEl.textContent = h.status === "ok" ? "healthy" : h.status;
      statusEl.className = "st-value st-ok";
      if (backendBuildEl) backendBuildEl.textContent = h.build ?? "dev";
    } catch {
      statusEl.textContent = "unreachable";
      statusEl.className = "st-value st-err";
      if (backendBuildEl) backendBuildEl.textContent = "\u2014";
    }
  }
  function buildNav() {
    const nav = document.getElementById("nav");
    if (!nav) return;
    nav.textContent = "";
    const brand = document.createElement("a");
    brand.className = "nav-brand";
    brand.textContent = "taskmaster";
    brand.href = "#board";
    nav.appendChild(brand);
    const inlineLinks = document.createElement("div");
    inlineLinks.className = "nav-links";
    const page = currentPage();
    NAV_LINKS.forEach(({ label, hash }) => {
      const a = document.createElement("a");
      a.className = "nav-link" + (hash === "#" + page ? " active" : "");
      a.textContent = label;
      a.href = hash;
      inlineLinks.appendChild(a);
    });
    nav.appendChild(inlineLinks);
    const spacer = document.createElement("div");
    spacer.className = "nav-spacer";
    nav.appendChild(spacer);
    nav.appendChild(buildLiveControl());
    nav.appendChild(buildBrakeControl());
    if (authEnabled) {
      const btnLogout = document.createElement("button");
      btnLogout.className = "nav-icon-btn";
      btnLogout.title = "Log out";
      btnLogout.setAttribute("aria-label", "Log out");
      btnLogout.innerHTML = ICON_LOGOUT;
      btnLogout.addEventListener("click", async () => {
        await api.logout();
        window.location.reload();
      });
      nav.appendChild(btnLogout);
    }
    nav.appendChild(buildMenu());
  }
  function liveToggleTitle(enabled) {
    return enabled ? "Live updates on \u2014 click to pause" : "Live updates paused \u2014 click to resume";
  }
  function buildLiveControl() {
    const wrap = document.createElement("div");
    wrap.className = "nav-live";
    const label = document.createElement("span");
    label.className = "nav-live-label";
    label.textContent = "Live";
    const toggle = createToggleHandle({
      checked: live.isEnabled(),
      onChange: (v) => {
        live.setEnabled(v);
        toggle.el.title = liveToggleTitle(v);
      }
    });
    toggle.el.title = liveToggleTitle(live.isEnabled());
    wrap.append(label, toggle.el);
    return wrap;
  }
  function buildMenu() {
    const wrap = document.createElement("div");
    wrap.className = "nav-menu";
    wrap.id = "nav-menu";
    const btn = document.createElement("button");
    btn.id = "nav-menu-btn";
    btn.className = "nav-icon-btn";
    btn.title = "Menu";
    btn.setAttribute("aria-label", "Menu");
    btn.setAttribute("aria-haspopup", "true");
    btn.setAttribute("aria-expanded", "false");
    btn.innerHTML = ICON_MENU;
    const panel = document.createElement("div");
    panel.id = "nav-menu-panel";
    panel.className = "nav-menu-panel";
    panel.hidden = true;
    const navSection = document.createElement("div");
    navSection.className = "menu-section menu-nav";
    const page = currentPage();
    NAV_LINKS.forEach(({ label, hash }) => {
      const a = document.createElement("a");
      a.className = "menu-item" + (hash === "#" + page ? " active" : "");
      a.textContent = label;
      a.href = hash;
      a.addEventListener("click", closeMenu);
      navSection.appendChild(a);
    });
    panel.appendChild(navSection);
    const liveSection = document.createElement("div");
    liveSection.className = "menu-section";
    liveSection.innerHTML = '<div class="menu-heading">Live updates</div>';
    const intervalRow = document.createElement("div");
    intervalRow.className = "menu-row";
    const intervalLabel = document.createElement("span");
    intervalLabel.textContent = "Fallback poll interval";
    const intervalSelect = document.createElement("select");
    LIVE_INTERVALS_SEC.forEach((s) => {
      const opt = document.createElement("option");
      opt.value = String(s * 1e3);
      opt.textContent = s + "s";
      if (s * 1e3 === live.getInterval()) opt.selected = true;
      intervalSelect.appendChild(opt);
    });
    intervalSelect.addEventListener("change", () => {
      live.setInterval(parseInt(intervalSelect.value, 10));
    });
    intervalRow.append(intervalLabel, intervalSelect);
    liveSection.append(intervalRow);
    panel.appendChild(liveSection);
    const stSection = document.createElement("div");
    stSection.className = "menu-section";
    stSection.innerHTML = '<div class="menu-heading">Server</div><div class="menu-row"><span>Status</span><span id="st-status" class="st-value st-muted">\u2026</span></div><div class="menu-row"><span>Backend build</span><span id="st-backend-build" class="st-value st-muted">\u2026</span></div><div class="menu-row"><span>Frontend build</span><span class="st-value">' + FRONTEND_BUILD_TIME + "</span></div>";
    const sudoRow = document.createElement("div");
    sudoRow.className = "menu-row";
    const sudoLabel = document.createElement("span");
    sudoLabel.textContent = "Allow sudo";
    const sudoToggle = createToggleHandle({
      checked: caps2.allow_sudo,
      onChange: (desired) => {
        sudoToggle.setDisabled(true);
        void api.setCapabilities(desired).then((updated) => {
          caps2 = updated;
        }).catch(() => {
          caps2.allow_sudo = !desired;
        }).finally(() => {
          sudoToggle.setChecked(caps2.allow_sudo);
          sudoToggle.setDisabled(false);
        });
      }
    });
    sudoRow.append(sudoLabel, sudoToggle.el);
    stSection.appendChild(sudoRow);
    panel.appendChild(stSection);
    btn.addEventListener("click", (e) => {
      e.stopPropagation();
      const open = panel.hidden;
      panel.hidden = !open;
      btn.setAttribute("aria-expanded", String(open));
      if (open) void refreshStatusLine();
    });
    wrap.append(btn, panel);
    return wrap;
  }
  function buildBrakeControl() {
    const btn = document.createElement("button");
    btn.id = "brake-btn";
    btn.type = "button";
    btn.className = "brake-btn";
    btn.innerHTML = ICON_BRAKE + '<span class="brake-btn-label"></span>';
    btn.addEventListener("click", () => void toggleBrake());
    applyBrakeUI(btn);
    return btn;
  }
  function applyBrakeUI(btn) {
    btn.classList.toggle("brake-engaged", brake.engaged);
    btn.title = brake.engaged ? "Hand brake engaged \u2014 click to release" : "Hand brake \u2014 click to stop everything";
    btn.setAttribute("aria-pressed", String(brake.engaged));
    const label = btn.querySelector(".brake-btn-label");
    if (label) label.textContent = brake.engaged ? "RELEASE BRAKE" : "HAND BRAKE";
  }
  function refreshBrakeUI() {
    const btn = document.getElementById("brake-btn");
    if (btn) applyBrakeUI(btn);
    renderBrakeBanner();
  }
  function renderBrakeBanner() {
    let banner = document.getElementById("brake-banner");
    if (!brake.engaged) {
      banner?.remove();
      return;
    }
    if (!banner) {
      banner = document.createElement("div");
      banner.id = "brake-banner";
      banner.className = "brake-banner";
      banner.textContent = "ALL PAUSED \u2014 hand brake engaged. No tasks will launch until it is released.";
      const nav = document.getElementById("nav");
      nav?.insertAdjacentElement("afterend", banner);
    }
  }
  async function toggleBrake() {
    if (brake.engaged) {
      try {
        brake = await api.releaseBrake();
      } catch {
        return;
      }
      refreshBrakeUI();
      return;
    }
    const ok = await confirmDialog(
      "Engage the hand brake? This pauses every lane and force-kills every running task (sudo children may survive). Everything stays paused until you release it.",
      { title: "Engage hand brake", confirmLabel: "Engage" }
    );
    if (!ok) return;
    try {
      brake = await api.engageBrake();
    } catch {
      return;
    }
    refreshBrakeUI();
  }
  document.addEventListener("click", (e) => {
    const menu = document.getElementById("nav-menu");
    if (menu && !menu.contains(e.target)) closeMenu();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") closeMenu();
  });
  var unmountCurrentPage = null;
  function renderPage() {
    const container = document.getElementById("app");
    if (!container) return;
    if (unmountCurrentPage) {
      unmountCurrentPage();
      unmountCurrentPage = null;
    }
    const page = currentPage();
    switch (page) {
      case "metrics":
        unmountCurrentPage = mountMetrics(container, live);
        break;
      case "task": {
        const taskName = currentTaskName();
        if (taskName) {
          unmountCurrentPage = mountTaskView(container, live, caps2, taskName);
          break;
        }
        unmountCurrentPage = mountBoard(container, live, caps2);
        break;
      }
      case "board":
      default:
        unmountCurrentPage = mountBoard(container, live, caps2);
    }
  }
  function route() {
    buildNav();
    renderBrakeBanner();
    renderPage();
  }
  async function bootstrap() {
    try {
      caps2 = await api.capabilities();
    } catch {
      caps2 = { allow_sudo: false };
    }
    try {
      const mode = await api.authMode();
      authEnabled = Array.isArray(mode.methods) && mode.methods.length > 0;
    } catch {
      authEnabled = false;
    }
    try {
      brake = await api.getBrake();
    } catch {
      brake = { engaged: false };
    }
    live.onEvent((ev) => {
      if (ev.type === "brake" && typeof ev.engaged === "boolean") {
        brake = { engaged: ev.engaged };
        refreshBrakeUI();
      }
    });
    window.addEventListener("hashchange", route);
    route();
  }
  document.addEventListener("DOMContentLoaded", () => {
    void bootstrap();
  });
})();
