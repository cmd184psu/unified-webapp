// web/taskmaster/js/api.ts
import { alertDialog } from "/shared/dist/shared.mjs";
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
  // Creates a new pending execution of the finished execution's task (P18:
  // 409 if a func task's latest execution already succeeded, or if the
  // task already has a queued/running execution).
  rerunExecution(id) {
    return apiFetch("/api/executions/" + id + "/rerun", { method: "POST" });
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

// web/taskmaster/js/ui/toggle.ts
function createToggleHandle(opts) {
  const wrapper = document.createElement("label");
  wrapper.className = "ui-toggle";
  const input = document.createElement("input");
  input.type = "checkbox";
  input.setAttribute("role", "switch");
  input.checked = !!opts.checked;
  input.disabled = !!opts.disabled;
  if (opts.label) input.setAttribute("aria-label", opts.label);
  const track = document.createElement("span");
  track.className = "ui-toggle-track";
  wrapper.append(input, track);
  if (opts.label) {
    const labelEl = document.createElement("span");
    labelEl.className = "ui-toggle-label";
    labelEl.textContent = opts.label;
    wrapper.appendChild(labelEl);
  }
  input.addEventListener("change", () => opts.onChange(input.checked));
  return {
    el: wrapper,
    setChecked: (v) => {
      input.checked = v;
    },
    setDisabled: (v) => {
      input.disabled = v;
    },
    getChecked: () => input.checked
  };
}

// web/taskmaster/js/main.ts
import { confirmDialog as confirmDialog2, ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";

// web/taskmaster/js/board.ts
import { openModal as openModal2, confirmDialog, alertDialog as alertDialog3, openOutputModal, QueuePanel } from "/shared/dist/shared.mjs";

// web/taskmaster/js/designer.ts
import { openModal, alertDialog as alertDialog2, createCopyButton } from "/shared/dist/shared.mjs";
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
  const copyBtn = createCopyButton({
    text: () => pre.textContent ?? "",
    label: title,
    className: "btn btn-secondary btn-sm export-panel-copy"
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
  for (const lane of lanes.filter((l) => !l.owner)) {
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
          await alertDialog2("Name, command, and lane are all required.");
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

// web/taskmaster/js/status.ts
import { statusSymbol, effectiveStatus } from "/shared/dist/shared.mjs";
function statusBadgeClass(status) {
  if (status === "success") return "badge-green";
  if (status === "failed") return "badge-red";
  if (status === "canceled") return "badge-yellow";
  if (status === "suspended") return "badge-yellow";
  if (status === "running") return "badge-blue";
  return "badge-muted";
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
var brakeEngaged = false;
var panels = /* @__PURE__ */ new WeakMap();
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
  void api.getBrake().then((b) => {
    brakeEngaged = b.engaged;
    render();
  }).catch(() => {
  });
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
  if (ev.type === "brake" && typeof ev.engaged === "boolean") {
    brakeEngaged = ev.engaged;
  }
  if (ev.type === "task-progress" && ev.execution_id !== void 0) {
    const idx = state.executions.findIndex((e) => e.id === ev.execution_id);
    if (idx !== -1) {
      state.executions[idx] = {
        ...state.executions[idx],
        progress_pct: ev.progress_pct ?? null,
        progress_label: ev.progress_label ?? ""
      };
      render();
    }
    return;
  }
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
function taskByName(name) {
  if (!name) return void 0;
  return state.tasks.find((t) => t.name === name);
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
  patchLanes(boardEl, sorted);
}
function patchLanes(container, lanes) {
  const existingByName = /* @__PURE__ */ new Map();
  for (const child of Array.from(container.children)) {
    const el = child;
    const name = el.getAttribute("data-lane");
    if (name !== null) existingByName.set(name, el);
  }
  const seen = /* @__PURE__ */ new Set();
  let cursor = container.firstChild;
  for (const lane of lanes) {
    seen.add(lane.name);
    let el = existingByName.get(lane.name);
    if (el) {
      updateLaneEl(el, lane);
    } else {
      el = createLaneEl(lane);
    }
    if (cursor !== el) {
      container.insertBefore(el, cursor);
    } else {
      cursor = cursor.nextSibling;
      continue;
    }
    cursor = el.nextSibling;
  }
  for (const [name, el] of existingByName) {
    if (!seen.has(name)) el.remove();
  }
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
  el.appendChild(body);
  const panel = new QueuePanel(body, tmQueueAdapter, {
    recentLimit: RAN_PER_LANE,
    titles: { recent: "Recent runs" },
    emptyText: { running: "nothing running", upnext: "lane is empty", recent: "no history yet" }
  });
  panels.set(el, panel);
  wireDropTarget(panel.list("upnext"), el);
  updateLaneEl(el, lane);
  return el;
}
async function toggleLanePause(btn, lane) {
  if (lane.paused) {
    btn.disabled = true;
    await api.resumeLane(lane.name).catch(() => void 0);
    btn.disabled = false;
    void refreshAll();
    return;
  }
  const laneTaskNames = new Set(tasksByLane(lane.name).map((t) => t.name));
  const running = state.executions.filter(
    (e) => e.status === "running" && !!e.task_name && laneTaskNames.has(e.task_name)
  );
  let cancelToo = false;
  if (running.length > 0) {
    const names = running.map((e) => e.task_name).join(", ");
    cancelToo = await confirmDialog(
      running.length === 1 ? `"${names}" is still running in this lane. Let it finish, or cancel it now?` : `${running.length} tasks are still running in this lane (${names}). Let them finish, or cancel them now?`,
      {
        title: `Stop lane "${lane.name}"`,
        confirmLabel: running.length === 1 ? "Cancel it too" : "Cancel them too",
        cancelLabel: running.length === 1 ? "Let it finish" : "Let them finish"
      }
    );
  }
  btn.disabled = true;
  try {
    await api.pauseLane(lane.name);
    if (cancelToo) {
      await Promise.all(running.map((e) => api.cancelExecution(e.id).catch(() => void 0)));
    }
  } catch {
  } finally {
    btn.disabled = false;
    void refreshAll();
  }
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
    pauseBtn.title = lane.paused ? "Resume lane" : "Stop lane (start nothing new; asks about a running task)";
    pauseBtn.setAttribute("aria-label", pauseBtn.title);
    pauseBtn.onclick = () => void toggleLanePause(pauseBtn, lane);
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
  const ranExecs = laneExecs.filter((e) => e.status === "success" || e.status === "failed" || e.status === "canceled").sort((a, b) => b.id - a.id);
  const runningTaskNames = new Set(runningExecs.map((e) => e.task_name));
  const pendingByTask = /* @__PURE__ */ new Map();
  for (const e of laneExecs) {
    if (e.status === "pending" && e.task_name && !pendingByTask.has(e.task_name)) {
      pendingByTask.set(e.task_name, e);
    }
  }
  const upNextTasks = laneTasks.filter((t) => {
    if (runningTaskNames.has(t.name)) return false;
    if (t.kind && !pendingByTask.has(t.name)) return false;
    return true;
  });
  const items = [
    ...runningExecs.map((exec) => ({ t: "exec", exec, sec: "running" })),
    ...upNextTasks.map((task) => ({ t: "task", task, pending: pendingByTask.get(task.name) ?? null })),
    ...ranExecs.map((exec) => ({ t: "exec", exec, sec: "recent" }))
  ];
  panels.get(el)?.update(items);
}
function itemKey(item) {
  return item.t === "exec" ? "exec:" + item.exec.id : "task:" + item.task.name;
}
function itemSection(item) {
  return item.t === "exec" ? item.sec : "upnext";
}
function itemTitle(item) {
  if (item.t === "task") return item.task.label || item.task.name;
  const task = taskByName(item.exec.task_name);
  return task && (task.label || task.name) || item.exec.task_name || "(unknown task)";
}
function itemStatus(item) {
  if (item.t === "exec") return effectiveStatus(item.exec.status, item.exec.suspended);
  if (item.pending) return "queued";
  if (!item.task.enabled) return "disabled";
  if (item.task.paused) return "paused";
  return "ready";
}
function itemMeta(item) {
  if (item.t === "exec") {
    if (item.exec.duration_ms !== void 0 && item.exec.duration_ms !== null) {
      return (item.exec.duration_ms / 1e3).toFixed(1) + "s";
    }
    if (item.exec.suspended) {
      return "paused";
    }
    if (item.exec.started_at) {
      return fmtElapsed(item.exec.started_at);
    }
    return "";
  }
  const task = item.task;
  if (!task.enabled) return "disabled";
  if (task.paused) return "paused";
  if (item.pending) return "queued";
  if (task.repeat) {
    return brakeEngaged ? "ready" : cooldownLabel(task);
  }
  return "ready";
}
function itemProgress(item) {
  if (item.t !== "exec") return null;
  const pct = item.exec.progress_pct;
  const label = item.exec.progress_label;
  if (pct === void 0 && !label) return null;
  return { pct: pct === void 0 ? null : pct, label: label ?? "" };
}
function itemActions(item) {
  if (item.t === "exec") {
    if (item.sec === "running") {
      const task2 = taskByName(item.exec.task_name);
      if (task2?.kind) return ["cancel"];
      return [item.exec.suspended ? "resume" : "pause", "cancel"];
    }
    const task = taskByName(item.exec.task_name);
    return task?.kind ? ["rerun", "remove"] : ["rerun"];
  }
  if (item.task.kind && item.pending) return ["cancel"];
  return [];
}
async function onBoardAction(action, item, btn) {
  btn.disabled = true;
  try {
    if (item.t === "exec") {
      if (action === "cancel") {
        await api.cancelExecution(item.exec.id);
      } else if (action === "pause") {
        await api.pauseExecution(item.exec.id);
      } else if (action === "resume") {
        await api.resumeExecution(item.exec.id);
      } else if (action === "rerun") {
        await api.rerunExecution(item.exec.id);
      } else if (action === "remove") {
        const name = item.exec.task_name;
        if (name) {
          const ok = await confirmDialog('Remove task "' + name + '"? This deletes its run history too.', {
            title: "Remove task",
            confirmLabel: "Remove"
          });
          if (ok) await api.deleteTask(name);
        }
      }
    } else if (action === "cancel" && item.pending) {
      await api.cancelExecution(item.pending.id);
    }
  } catch {
  } finally {
    btn.disabled = false;
    void refreshAll();
  }
}
function renderBoardBadge(badge, item) {
  if (item.t === "exec") {
    renderStatusBadge(badge, item.exec.status, item.exec.suspended);
    return;
  }
  badge.className = "ui-queue-badge";
  badge.textContent = "";
  badge.title = "";
  badge.removeAttribute("aria-label");
}
function decorateBoardRow(row, item, created) {
  if (item.t === "exec") {
    const kindClass = item.sec === "running" ? "running" : "ran";
    row.classList.add("exec-row", "exec-row-" + kindClass);
    if (item.sec !== "running") return;
    let pidSeam = row.querySelector(".exec-row-pid-seam");
    if (!pidSeam) {
      pidSeam = document.createElement("span");
      pidSeam.className = "exec-row-pid-seam";
      const pidLabel2 = document.createElement("span");
      pidLabel2.className = "exec-row-pid";
      const outputBtn2 = document.createElement("button");
      outputBtn2.type = "button";
      outputBtn2.className = "btn-icon exec-row-output-btn";
      outputBtn2.textContent = "\u2197";
      outputBtn2.title = "View live output";
      outputBtn2.setAttribute("aria-label", "View live output");
      pidSeam.append(pidLabel2, outputBtn2);
      row.appendChild(pidSeam);
    }
    const pidLabel = row.querySelector(".exec-row-pid");
    if (pidLabel) pidLabel.textContent = item.exec.pid !== void 0 ? "pid " + item.exec.pid : "";
    const outputBtn = row.querySelector(".exec-row-output-btn");
    if (outputBtn) outputBtn.onclick = () => openRunningOutput(item.exec);
    return;
  }
  row.classList.add("task-row");
  row.setAttribute("data-task", item.task.name);
  row.classList.toggle("task-row-disabled", !item.task.enabled || item.task.paused);
  if (item.task.kind) return;
  if (created) {
    const capturedName = item.task.name;
    const capturedLane = item.task.lane_name;
    row.draggable = true;
    row.addEventListener("dragstart", (e) => {
      if (!e.dataTransfer) return;
      e.dataTransfer.effectAllowed = "move";
      e.dataTransfer.setData("text/plain", JSON.stringify({ task: capturedName, lane: capturedLane }));
      row.classList.add("dragging");
    });
    row.addEventListener("dragend", () => row.classList.remove("dragging"));
  }
  let upNextBtn = row.querySelector(".task-row-upnext");
  if (!upNextBtn) {
    upNextBtn = document.createElement("button");
    upNextBtn.type = "button";
    upNextBtn.className = "btn btn-secondary btn-sm task-row-upnext";
    upNextBtn.textContent = "Up next";
    row.appendChild(upNextBtn);
  }
  upNextBtn.disabled = !item.task.enabled || item.task.paused;
  const taskName = item.task.name;
  upNextBtn.onclick = () => void api.upNext(taskName).then(() => refreshAll());
}
var tmQueueAdapter = {
  key: itemKey,
  section: itemSection,
  title: itemTitle,
  status: itemStatus,
  meta: itemMeta,
  progress: itemProgress,
  actions: itemActions,
  onAction: onBoardAction,
  renderBadge: renderBoardBadge,
  decorate: decorateBoardRow,
  onTitleClick: (item) => openTaskRoute(item.t === "exec" ? item.exec.task_name ?? "" : item.task.name)
};
function openRunningOutput(exec) {
  const title = (exec.task_name ?? "task") + " \u2014 run #" + exec.id;
  openOutputModal(api.openExecutionOutput(exec.id), title);
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
  const handle = openModal2(content, { title: "New lane" });
  cancelBtn.addEventListener("click", () => handle.close());
  createBtn.addEventListener("click", () => {
    void (async () => {
      const name = nameInput.value.trim();
      const width = parseInt(widthInput.value, 10) || 1;
      if (!name) {
        await alertDialog3("Lane name is required.");
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
import { patchList } from "/shared/dist/shared.mjs";
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
  const sorted = [...rows].sort((a, b) => cardKey(a).localeCompare(cardKey(b)));
  patchList(gridEl, sorted, {
    key: cardKey,
    create: (m) => createCard(m),
    update: (el, m) => updateCard(el, m)
  });
}
function cardKey(m) {
  return m.kind ? "k:" + m.group_name + "/" + m.kind : "n:" + m.task_name;
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
import { patchList as patchList2, openOutputModal as openOutputModal2 } from "/shared/dist/shared.mjs";
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
  if (task.kind) {
    const kindLine = document.createElement("code");
    kindLine.className = "task-detail-command";
    kindLine.textContent = "kind: " + task.kind + (task.label ? " \xB7 " + task.label : "");
    meta.appendChild(kindLine);
    if (task.payload !== void 0 && task.payload !== null) {
      const payloadPre = document.createElement("pre");
      payloadPre.className = "task-detail-payload";
      try {
        payloadPre.textContent = JSON.stringify(task.payload, null, 2);
      } catch {
        payloadPre.textContent = String(task.payload);
      }
      meta.appendChild(payloadPre);
    }
  } else {
    const cmdLine = document.createElement("code");
    cmdLine.className = "task-detail-command";
    cmdLine.textContent = task.command;
    meta.appendChild(cmdLine);
  }
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
    patchList2(historyList, execs, {
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
      openOutputModal2(api.openExecutionOutput(exec.id), task.name + " \u2014 run #" + exec.id);
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
  let canceled = false;
  const goBack = () => {
    window.location.hash = "#board";
  };
  void api.getTask(taskName).then((task) => {
    if (canceled) return;
    unmountLeft = mountBoard(left, live2, caps3, { laneFilter: task.lane_name });
    right.textContent = "";
    unmountRight = mountTaskDetail(right, live2, task, goBack);
  }).catch(() => {
    if (canceled) return;
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
    canceled = true;
    if (unmountLeft) unmountLeft();
    if (unmountRight) unmountRight();
  };
}

// web/taskmaster/js/buildinfo.ts
var FRONTEND_BUILD_TIME = "85a22a34cacd";

// web/taskmaster/js/main.ts
var caps2 = { allow_sudo: false };
var authEnabled = false;
var brake = { engaged: false };
var live = new LiveController();
var LIVE_INTERVALS_SEC = [5, 10, 30, 60];
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
  nav.appendChild(hamburger?.trigger ?? document.createElement("span"));
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
var themes = new ThemeManager({ module: "taskmaster", default: "obsidian" });
themes.apply();
var hamburger = null;
function buildHamburger() {
  hamburger?.destroy();
  const items = [
    { section: "Live updates" },
    {
      id: "live-toggle",
      render: (host) => {
        const row = document.createElement("div");
        row.className = "menu-row";
        const label = document.createElement("span");
        label.textContent = "Live";
        const toggle = createToggleHandle({
          checked: live.isEnabled(),
          onChange: (v) => live.setEnabled(v)
        });
        row.append(label, toggle.el);
        host.append(row);
      }
    },
    {
      id: "fallback-interval",
      render: (host) => {
        const row = document.createElement("div");
        row.className = "menu-row";
        const label = document.createElement("span");
        label.textContent = "Poll interval";
        row.title = "Fallback poll interval, used when live updates are unavailable";
        const select = document.createElement("select");
        LIVE_INTERVALS_SEC.forEach((s) => {
          const opt = document.createElement("option");
          opt.value = String(s * 1e3);
          opt.textContent = s + "s";
          if (s * 1e3 === live.getInterval()) opt.selected = true;
          select.appendChild(opt);
        });
        select.addEventListener("change", () => {
          live.setInterval(parseInt(select.value, 10));
        });
        row.append(label, select);
        host.append(row);
      }
    },
    { separator: true },
    { section: "Server" },
    {
      id: "server-status",
      render: (host) => {
        host.innerHTML = '<div class="menu-row"><span>Status</span><span id="st-status" class="st-value st-muted">\u2026</span></div><div class="menu-row"><span>Backend build</span><span id="st-backend-build" class="st-value st-muted">\u2026</span></div><div class="menu-row"><span>Frontend build</span><span class="st-value">' + FRONTEND_BUILD_TIME + "</span></div>";
      }
    },
    {
      id: "allow-sudo",
      render: (host) => {
        const row = document.createElement("div");
        row.className = "menu-row";
        const label = document.createElement("span");
        label.textContent = "Allow sudo";
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
        row.append(label, sudoToggle.el);
        host.append(row);
      },
      when: () => authEnabled
    }
  ];
  hamburger = new HamburgerMenu({
    title: "taskmaster",
    items,
    themePicker: true,
    themes,
    side: "right",
    onOpen: () => void refreshStatusLine()
  });
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
  const ok = await confirmDialog2(
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
  buildHamburger();
  window.addEventListener("hashchange", route);
  route();
}
document.addEventListener("DOMContentLoaded", () => {
  void bootstrap();
});
