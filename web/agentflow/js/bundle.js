// web/agentflow/js/main.ts
import { ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";

// web/agentflow/js/api.ts
import { alertDialog } from "/shared/dist/shared.mjs";
var ApiError = class extends Error {
  constructor(status, message, field) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.field = field;
  }
};
function fail(err, quiet) {
  if (!quiet) void alertDialog(err.message);
  throw err;
}
async function apiFetch(path2, opts = {}) {
  if (!path2.startsWith("/api/")) throw new Error(`apiFetch: ${path2} is not an /api path`);
  const init = { method: opts.method ?? "GET" };
  if (opts.body !== void 0) {
    init.headers = { "Content-Type": "application/json" };
    init.body = typeof opts.body === "string" ? opts.body : JSON.stringify(opts.body);
  }
  let resp;
  try {
    resp = await fetch(path2, init);
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return fail(new ApiError(0, "Network error contacting the server: " + msg), opts.quiet);
  }
  if (resp.status === 401) {
    window.location.reload();
    throw new ApiError(401, "unauthorized");
  }
  if (!resp.ok) {
    let message = resp.statusText || `HTTP ${resp.status}`;
    let field;
    try {
      const body = await resp.json();
      if (body.error) message = body.error;
      field = body.field;
    } catch {
    }
    return fail(new ApiError(resp.status, message, field), opts.quiet);
  }
  if (resp.status === 204) return void 0;
  return await resp.json();
}
var enc = encodeURIComponent;
var proj = (slug) => `/api/projects/${enc(slug)}`;
var eff = (slug, id) => `${proj(slug)}/efforts/${id}`;
var get = (path2, o) => apiFetch(path2, o);
var send = (method, path2, body, o) => apiFetch(path2, { ...o, method, body });
var api = {
  // Health and opencode
  health: (o) => get("/api/health", o),
  attention: (o) => get("/api/attention", o),
  coordStatus: (o) => get("/api/coord", o),
  reviews: (project = "", o) => get(`/api/reviews${project ? `?project=${enc(project)}` : ""}`, o),
  review: (id, o) => get(`/api/reviews/${id}`, o),
  coordRun: (dry) => send("POST", "/api/coord/run", { dry }),
  externalSeats: (o) => get("/api/seats/external", o),
  seatQueue: (callsign) => get(`/api/seats/${encodeURIComponent(callsign)}/queue`),
  nudgeSeat: (callsign) => send("POST", `/api/seats/${encodeURIComponent(callsign)}/nudge`, {}),
  seats: (o) => get("/api/seats", o),
  answerSeat: (stage, text) => send("POST", `/api/seats/${stage}/answer`, { text }),
  cancelSeat: (stage) => send("POST", `/api/seats/${stage}/cancel`),
  opencodeVersion: (refresh = false) => get(`/api/opencode/version${refresh ? "?refresh=1" : ""}`),
  restartStatus: (o) => get("/api/service/restart", o),
  restartService: (cancelRunning) => send("POST", "/api/service/restart", { cancel_running: cancelRunning }),
  cancelRestart: () => send("DELETE", "/api/service/restart"),
  serviceHold: (o) => get("/api/service/hold", o),
  holdService: (reason) => send("POST", "/api/service/hold", { reason }),
  liftHold: () => send("DELETE", "/api/service/hold"),
  fsDirs: (path2 = "") => get(`/api/fs/dirs${path2 ? `?path=${encodeURIComponent(path2)}` : ""}`, { quiet: true }),
  // Projects
  projects: () => get("/api/projects"),
  project: (slug) => get(proj(slug)),
  createProject: (p, o) => send("POST", "/api/projects", p, o),
  gitInit: (repoPath) => send("POST", "/api/projects/git-init", { repo_path: repoPath, confirm: true }),
  patchProject: (slug, p, o) => send("PATCH", proj(slug), p, o),
  deleteProject: (slug) => send("DELETE", proj(slug)),
  dropAllProjects: () => send("POST", "/api/projects/_all_", { confirm: true }),
  definitionOfDone: (slug, o) => get(`${proj(slug)}/definition-of-done`, o),
  limits: (slug) => get(`${proj(slug)}/limits`),
  putLimits: (slug, limits, o) => send("PUT", `${proj(slug)}/limits`, { limits }, o),
  // Backends, candidates, settings
  backends: () => get("/api/backends"),
  createBackend: (b, o) => send("POST", "/api/backends", b, o),
  probeBackend: (host, apiType, key = "", backend = "", cliPath = "") => send("POST", "/api/backends/probe", { host, api_type: apiType, api_key: key, backend, cli_path: cliPath }, { quiet: true }),
  patchBackend: (name, b, o) => send("PATCH", `/api/backends/${enc(name)}`, b, o),
  retireBackend: (name, cancelRunning) => send("POST", `/api/backends/${enc(name)}/retire`, { cancel_running: cancelRunning }),
  extendWindows: (name, apply = true) => send("POST", `/api/backends/${enc(name)}/extend-windows`, { apply }),
  enableBackend: (name) => send("POST", `/api/backends/${enc(name)}/enable`),
  deleteBackend: (name) => send("DELETE", `/api/backends/${enc(name)}`),
  rests: () => get("/api/rests", { quiet: true }),
  wakeModel: (backend, model) => send("DELETE", `/api/rests?backend=${enc(backend)}&model=${enc(model)}`),
  holds: () => get("/api/holds", { quiet: true }),
  setHold: (backend, model, hold) => send("PUT", `/api/holds?backend=${enc(backend)}&model=${enc(model)}&hold=${hold}`),
  checkModels: (name) => send("POST", `/api/backends/${enc(name)}/check`),
  checkStatus: (name) => get(`/api/backends/${enc(name)}/check`, { quiet: true }),
  checkReviewers: (name, body = {}) => send("POST", `/api/backends/${enc(name)}/evals`, body),
  evalStatus: (name) => get(`/api/backends/${enc(name)}/evals`, { quiet: true }),
  candidates: () => get("/api/candidates"),
  putCandidates: (role, candidates, o) => send("PUT", `/api/candidates/${enc(role)}`, { candidates }, o),
  putProjectCandidates: (slug, role, candidates, o) => send("PUT", `${proj(slug)}/candidates/${enc(role)}`, { candidates }, o),
  clearProjectCandidates: (slug, role) => send("DELETE", `${proj(slug)}/candidates/${enc(role)}`),
  settings: () => get("/api/settings"),
  putSettings: (s, o) => send("PUT", "/api/settings", s, o),
  modelStats: (project = "") => get(`/api/stats/models${project ? `?project=${enc(project)}` : ""}`),
  strikeHistory: (project = "", limit = 50) => get(`/api/strikes?limit=${limit}${project ? `&project=${enc(project)}` : ""}`),
  installs: (backend) => get(`/api/backends/${enc(backend)}/installs`),
  startInstall: (backend, model, tier) => send("POST", `/api/backends/${enc(backend)}/install`, { model, tier }),
  cancelInstall: (id) => send("POST", `/api/installs/${id}/cancel`),
  importConfig: (config, apply) => send("POST", "/api/config/import", { config, apply }),
  clearStrikes: (backend, model, role) => send("POST", "/api/strikes/clear", { backend, model, role }),
  // Efforts
  efforts: (slug) => get(`${proj(slug)}/efforts`),
  effort: (slug, id) => get(eff(slug, id)),
  createEffort: (slug, title, deliberate, o, budget) => send("POST", `${proj(slug)}/efforts`, { title, deliberate, ...budget }, o),
  patchEffort: (slug, id, patch, o) => send("PATCH", eff(slug, id), patch, o),
  importFRD: (slug, frdJSON, o) => send("POST", `${proj(slug)}/efforts/import`, frdJSON, o),
  abandonEffort: (slug, id) => send("POST", `${eff(slug, id)}/abandon`),
  setYOLO: (slug, id, on, confirm) => send("POST", `${eff(slug, id)}/yolo`, { on, confirm }),
  yoloInvariants: () => get("/api/yolo/invariants"),
  // FRD and plan documents
  frd: (slug, id, o) => get(`${eff(slug, id)}/frd`, o),
  putFRD: (slug, id, frd, o) => send("PUT", `${eff(slug, id)}/frd`, frd, o),
  frdRevisions: (slug, id) => get(`${eff(slug, id)}/frd/revisions`),
  frdRevision: (slug, id, n) => get(`${eff(slug, id)}/frd/revisions/${n}`),
  approveFRD: (slug, id, revision) => send("POST", `${eff(slug, id)}/frd/approve`, revision === void 0 ? {} : { revision }),
  draftFRD: (slug, id, description, polish, o) => send("POST", `${eff(slug, id)}/frd/draft`, { description, polish }, o),
  draftState: (slug, id, o) => get(`${eff(slug, id)}/frd/draft`, o),
  frdSize: (slug, id, o) => get(`${eff(slug, id)}/size`, o),
  splitFRD: (slug, id, o) => send("POST", `${eff(slug, id)}/frd/split`, {}, o),
  frdParts: (slug, id, o) => get(`${eff(slug, id)}/frd/parts`, o),
  startPart: (slug, id, n, o) => send("POST", `${eff(slug, id)}/frd/parts/${n}/start`, void 0, o),
  generatePlan: (slug, id) => send("POST", `${eff(slug, id)}/plan/generate`),
  plan: (slug, id, o) => get(`${eff(slug, id)}/plan`, o),
  putPlan: (slug, id, plan, o) => send("PUT", `${eff(slug, id)}/plan`, plan, o),
  planRounds: (slug, id) => get(`${eff(slug, id)}/plan/rounds`),
  planRevisions: (slug, id) => get(`${eff(slug, id)}/plan/revisions`),
  planRevision: (slug, id, n) => get(`${eff(slug, id)}/plan/revisions/${n}`),
  approvePlan: (slug, id) => send("POST", `${eff(slug, id)}/plan/approve`),
  // Queue and tasks
  compile: (slug, id) => send("POST", `${eff(slug, id)}/compile`),
  queue: (slug, id, o) => get(`${eff(slug, id)}/queue`, o),
  /** gitCommands lists the commands to merge and push a complete effort's branch; agentflow runs none of them (GIT4). */
  gitCommands: (slug, id, o) => get(`${eff(slug, id)}/git-commands`, o),
  approveQueue: (slug, id, hash, o) => send("POST", `${eff(slug, id)}/queue/approve`, { hash }, o),
  tasks: (slug, id) => get(`${eff(slug, id)}/tasks`),
  task: (slug, id, key) => get(`${eff(slug, id)}/tasks/${enc(key)}`),
  /** editTask answers with the queue. A queue that was approved stays approved (the edit is the review) and a run goes on; one still in draft stays in draft. */
  editTask: (slug, id, key, def, o) => send("PUT", `${eff(slug, id)}/tasks/${enc(key)}?approve=true`, def, o),
  retryTask: (slug, id, key, clean) => send("POST", `${eff(slug, id)}/tasks/${enc(key)}/retry`, { clean }),
  /** applyResolution carries out the resolver's advice on a halted task (resolver_mode advise). */
  applyResolution: (slug, id, key) => send("POST", `${eff(slug, id)}/tasks/${enc(key)}/resolution/apply`, {}),
  /** allowTaskFiles adds the files a stopped task asked for to its list and retries it in the same worktree (AF-13); no files allows them all. */
  allowTaskFiles: (slug, id, key, files = []) => send("POST", `${eff(slug, id)}/tasks/${enc(key)}/allow-files`, { files }),
  overrideTask: (slug, id, key, reason) => send("POST", `${eff(slug, id)}/tasks/${enc(key)}/override`, { reason }),
  // Run control: answers with the project's overview
  run: (slug, op) => send("POST", `${proj(slug)}/run/${op}`)
};
function exportURL(effort, format, doc, revision) {
  const base = doc ? `/api/efforts/${effort}/${doc}/export` : `/api/efforts/${effort}/export`;
  return `${base}?format=${format}${revision ? `&revision=${revision}` : ""}`;
}
function stageStreamURL(stage) {
  return `/api/stages/${stage}/stream`;
}

// web/agentflow/js/dom.ts
function h(tag, props = {}, ...children) {
  const el = document.createElement(tag);
  if (props.class) el.className = props.class;
  if (props.text !== void 0) el.textContent = props.text;
  for (const [name, value] of Object.entries(props.attrs ?? {})) {
    if (/^on/i.test(name)) throw new Error(`h: attribute ${name} refused; use props.on`);
    if (value === null || value === void 0 || value === false) continue;
    el.setAttribute(name, value === true ? "" : String(value));
  }
  for (const [name, fn] of Object.entries(props.on ?? {})) el.addEventListener(name, fn);
  append(el, children);
  return el;
}
function append(el, children) {
  for (const c of children) {
    if (c === null || c === void 0 || c === false) continue;
    el.appendChild(typeof c === "string" || typeof c === "number" ? document.createTextNode(String(c)) : c);
  }
}
function replace(host, ...children) {
  host.textContent = "";
  append(host, children);
}
var CHIP_ALIAS = {
  running: "in-progress",
  pausing: "review",
  paused: "review",
  // Plan review verdicts (RP2).
  approve: "passed",
  revise: "repair",
  reject: "failed"
};
function statusKey(status) {
  const key = status.toLowerCase().replace(/[^a-z0-9]+/g, "-");
  return CHIP_ALIAS[key] ?? key;
}
function chip(status) {
  return h("span", { class: `status-chip status-${statusKey(status)}`, text: status.replace(/_/g, " ") });
}

// web/agentflow/js/autosave.ts
var AUTOSAVE_DELAY_MS = 5e3;
var timing = { delay: AUTOSAVE_DELAY_MS };
var registry = /* @__PURE__ */ new Set();
var listeners = /* @__PURE__ */ new Set();
var changed = () => listeners.forEach((fn) => fn());
var onScreen = (e) => e.status.isConnected !== false;
var MARKS = {
  idle: { text: "", title: "" },
  pending: { text: "\u2026", title: "Unsaved changes: saves after a few seconds without typing" },
  saving: { text: "\u21BB", title: "Saving" },
  saved: { text: "\u2713", title: "Saved" },
  failed: { text: "\u26A0", title: "Not saved: fix the field marked above, then change something to try again" }
};
function autosave(o) {
  const status = h("span", { class: "autosave-state", attrs: { "data-role": "autosave-state", "aria-live": "polite" } });
  let saved = o.startDirty ? null : o.snapshot();
  let timer2;
  let busy = false;
  let current = "idle";
  const mark = (k) => {
    current = k;
    status.textContent = MARKS[k].text;
    status.setAttribute("title", MARKS[k].title);
    status.setAttribute("data-state", k);
    changed();
  };
  mark("idle");
  const run = async () => {
    timer2 = void 0;
    if (busy) {
      timer2 = setTimeout(() => void run(), o.delay ?? timing.delay);
      return;
    }
    const now = o.snapshot();
    if (now === saved) {
      mark("idle");
      return;
    }
    busy = true;
    mark("saving");
    let ok = false;
    try {
      ok = await o.save();
    } catch {
      ok = false;
    }
    busy = false;
    if (ok) saved = now;
    if (timer2 === void 0) mark(ok ? "saved" : "failed");
  };
  const touch = () => {
    if (timer2 !== void 0) clearTimeout(timer2);
    mark(o.snapshot() === saved ? "idle" : "pending");
    timer2 = setTimeout(() => void run(), o.delay ?? timing.delay);
  };
  const entry = { status, state: () => current, flush: () => api2.flush() };
  registry.add(entry);
  const api2 = {
    status,
    watch(el) {
      el.addEventListener("input", touch);
      el.addEventListener("change", touch);
    },
    touch,
    async flush() {
      if (timer2 !== void 0) clearTimeout(timer2);
      await run();
    },
    stop() {
      if (timer2 !== void 0) clearTimeout(timer2);
      timer2 = void 0;
      registry.delete(entry);
      changed();
    }
  };
  return api2;
}
var ROLLUP = {
  idle: { text: "\u2713", title: "All changes are saved" },
  pending: { text: "\u2026", title: "Changes are saving by themselves: click to save now" },
  failed: { text: "\u26A0", title: "Something was not saved: click to go to it" }
};
function rollup() {
  let out = "idle";
  for (const e of registry) {
    if (!onScreen(e)) {
      if (e.state() !== "pending" && e.state() !== "saving") registry.delete(e);
      continue;
    }
    const s = e.state();
    if (s === "failed") return "failed";
    if (s === "pending" || s === "saving") out = "pending";
  }
  return out;
}
async function saveAll() {
  await Promise.all([...registry].filter((e) => e.state() === "pending").map((e) => e.flush()));
}
function mountSaveMark(host) {
  const el = h("button", { class: "icon-btn autosave-mark", attrs: { type: "button", "data-role": "save-mark", "aria-live": "polite" } });
  const draw = () => {
    const r = rollup();
    el.textContent = ROLLUP[r].text;
    el.setAttribute("title", ROLLUP[r].title);
    el.setAttribute("aria-label", ROLLUP[r].title);
    el.setAttribute("data-state", r);
  };
  el.addEventListener("click", () => {
    const failed = [...registry].find((e) => onScreen(e) && e.state() === "failed");
    if (failed) {
      failed.status.scrollIntoView?.({ block: "center", behavior: "smooth" });
      return;
    }
    void saveAll();
  });
  listeners.add(draw);
  draw();
  host.appendChild(el);
  return () => {
    listeners.delete(draw);
    el.parentNode?.removeChild(el);
  };
}

// web/agentflow/js/events.ts
function backoffMs(failures2) {
  return Math.min(3e4, 1e3 * 2 ** Math.max(0, failures2));
}
var EVENT_NAME = "agentflow";
var URL = "/api/events";
var listeners2 = /* @__PURE__ */ new Set();
var source = null;
var failures = 0;
var timer = null;
var wanted = false;
function subscribe(fn) {
  listeners2.add(fn);
  return () => listeners2.delete(fn);
}
function dispatch(data) {
  let ev;
  try {
    ev = JSON.parse(data);
  } catch {
    return;
  }
  if (!ev || typeof ev.type !== "string") return;
  for (const fn of [...listeners2]) fn(ev);
}
function connect() {
  if (!wanted || source) return;
  const es = new EventSource(URL);
  source = es;
  es.addEventListener(EVENT_NAME, (m) => {
    failures = 0;
    dispatch(m.data);
  });
  es.onerror = () => {
    es.close();
    if (source === es) source = null;
    if (!wanted || timer) return;
    timer = setTimeout(() => {
      timer = null;
      connect();
    }, backoffMs(failures));
    failures++;
  };
}
function startEvents() {
  wanted = true;
  connect();
}
var TRANSIENT = /* @__PURE__ */ new Set(["planning", "compiling", "running"]);
function isTransient(status) {
  return status !== void 0 && TRANSIENT.has(status);
}
var pollSettings = { ms: 5e3 };
function startPoll(active2, fn) {
  let busy = false;
  const id = setInterval(() => {
    if (busy || !active2()) return;
    busy = true;
    void Promise.resolve(fn()).finally(() => {
      busy = false;
    });
  }, pollSettings.ms);
  return () => clearInterval(id);
}

// web/agentflow/js/attention.ts
import { showToast } from "/shared/dist/shared.mjs";
function toast(text, kind) {
  try {
    void Promise.resolve(showToast(text, kind)).catch(() => {
    });
  } catch {
  }
}
function itemKey(i) {
  return `${i.project}/${i.effort}/${i.task ?? ""}/${i.kind}/${i.since}`;
}
function titleWith(base, count) {
  const bare = base.replace(/^\(\d+\) /, "");
  return count > 0 ? `(${count}) ${bare}` : bare;
}
var AttentionBanner = class {
  constructor(host, deps, open = () => {
  }) {
    this.host = host;
    this.deps = deps;
    this.open = open;
    this.items = [];
    this.seen = null;
    // null until the first load: what was already there is not news
    this.off = null;
    this.stopPoll = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      if (ev.type === "state.changed" || ev.type === "project.run_state") void this.load();
    });
    this.stopPoll = startPoll(() => true, () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.stopPoll?.();
      this.off = this.stopPoll = null;
    };
  }
  async load() {
    try {
      this.items = await this.deps.attention({ quiet: true });
    } catch {
      return;
    }
    this.announce();
    this.render();
  }
  announce() {
    const keys = new Set(this.items.map(itemKey));
    if (this.seen) {
      for (const i of this.items) {
        if (this.seen.has(itemKey(i))) continue;
        const text = `${i.project}, effort ${i.effort}: ${i.message}`;
        void showToast(text, "error");
        try {
          if (typeof Notification !== "undefined" && Notification.permission === "granted") new Notification("AgentFlow needs you", { body: text });
        } catch {
        }
      }
    }
    this.seen = keys;
  }
  render() {
    if (typeof document.title === "string") document.title = titleWith(document.title, this.items.length);
    if (this.items.length === 0) {
      replace(this.host);
      this.host.setAttribute("hidden", "");
      return;
    }
    replace(this.host, h(
      "div",
      { class: "warn-box attention-banner", attrs: { role: "alert", "data-role": "attention-banner" } },
      h("p", { text: this.items.length === 1 ? "One thing has stopped and needs you:" : `${this.items.length} things have stopped and need you:` }),
      h("ul", {}, ...this.items.map((i) => h(
        "li",
        {},
        h("a", { text: `${i.project}, effort ${i.effort}`, attrs: { href: "#", "data-project": i.project }, on: { click: (e) => {
          e.preventDefault();
          this.open(i.project);
        } } }),
        h("span", { text: ` \u2014 ${i.message}` }),
        ...this.needsFiles(i)
      )))
    ));
    this.host.removeAttribute("hidden");
  }
  /** needsFiles is a task_needs_files item's file list and its Allow button (AF-13). */
  needsFiles(i) {
    const allow = this.deps.allowTaskFiles;
    if (i.kind !== "task_needs_files" || !i.task || !i.needs_files?.length || !allow) return [];
    const task = i.task;
    return [
      h("ul", { class: "needs-files" }, ...i.needs_files.map((f) => h("li", {}, h("code", { text: f.path }), h("span", { text: ` \u2014 ${f.reason}` })))),
      h("button", {
        text: i.needs_files.length === 1 ? "Allow this file and retry" : `Allow these ${i.needs_files.length} files and retry`,
        attrs: { type: "button", "data-role": "allow-files" },
        on: { click: async (e) => {
          const btn = e.currentTarget ?? e.target;
          if (btn) btn.disabled = true;
          try {
            await allow(i.project, i.effort, task);
            toast(`${task}: files allowed; it continues in the same worktree`, "success");
          } catch (err) {
            toast(`${task}: ${err instanceof Error ? err.message : String(err)}`, "error");
            if (btn) btn.disabled = false;
          }
          void this.load();
        } }
      })
    ];
  }
};

// web/agentflow/js/seats.ts
function seatLabel(s) {
  const where = [s.project, s.effort ? `effort ${s.effort}` : "", s.task ?? ""].filter(Boolean).join(", ");
  return `${where} \u2014 ${s.role} on ${s.backend} (stage ${s.stage})`;
}
var SeatPanel = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.seats = [];
    this.armed = /* @__PURE__ */ new Set();
    // stages whose Cancel was pressed once
    this.off = null;
    this.stopPoll = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      if (ev.type === "state.changed") void this.load();
    });
    this.stopPoll = startPoll(() => true, () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.stopPoll?.();
      this.off = this.stopPoll = null;
    };
  }
  async load() {
    try {
      this.seats = await this.deps.seats({ quiet: true });
    } catch {
      return;
    }
    this.render();
  }
  async answer(stage, input) {
    const text = input.value.trim();
    if (!text) return;
    await this.deps.answerSeat(stage, text);
    input.value = "";
    await this.load();
  }
  async cancel(stage) {
    if (!this.armed.has(stage)) {
      this.armed.add(stage);
      this.render();
      return;
    }
    this.armed.delete(stage);
    await this.deps.cancelSeat(stage);
    await this.load();
  }
  render() {
    if (this.seats.length === 0) {
      replace(this.host);
      this.host.setAttribute("hidden", "");
      return;
    }
    replace(this.host, h(
      "div",
      { class: "seat-panel", attrs: { "data-role": "seat-panel" } },
      h("p", { text: this.seats.length === 1 ? "One seat is working:" : `${this.seats.length} seats are working:` }),
      h("ul", {}, ...this.seats.map((s) => {
        const input = h("input", { attrs: { type: "text", placeholder: "Answer the seat", "data-role": "seat-answer" } });
        return h(
          "li",
          { attrs: { "data-stage": String(s.stage) } },
          h("span", { text: seatLabel(s) }),
          s.attach ? h("code", { class: "seat-attach", text: s.attach, attrs: { "data-role": "seat-attach" } }) : h("span", { text: ` \u2014 ${s.model} on BotChat` }),
          s.waiting ? h("div", { class: "warn-box", text: s.waiting.message }) : h("span", {}),
          h(
            "div",
            { class: "seat-actions" },
            input,
            h("button", { text: "Send", attrs: { type: "button", "data-role": "seat-send" }, on: { click: () => {
              void this.answer(s.stage, input);
            } } }),
            h("button", { text: this.armed.has(s.stage) ? "Really cancel the stage" : "Cancel", attrs: { type: "button", "data-role": "seat-cancel" }, on: { click: () => {
              void this.cancel(s.stage);
            } } })
          )
        );
      }))
    ));
    this.host.removeAttribute("hidden");
  }
};

// web/agentflow/js/health.ts
function healthMessage(hl) {
  if (!hl || hl.status === "ok") return "";
  const oc = hl.opencode;
  const why = oc?.error ? `: ${oc.error}` : "";
  return `AgentFlow is degraded. opencode is ${oc?.status ?? "unavailable"}${why}. Planning and running tasks will fail until it is fixed.`;
}
var HealthBanner = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.health = null;
    this.stopPoll = null;
    this.off = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      if (ev.type === "settings.update" || ev.type === "state.changed") void this.load();
    });
    this.stopPoll = startPoll(() => healthMessage(this.health) !== "", () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.stopPoll?.();
      this.off = this.stopPoll = null;
    };
  }
  async load() {
    try {
      this.health = await this.deps.health({ quiet: true });
    } catch {
      return;
    }
    this.render();
  }
  render() {
    const msg = healthMessage(this.health);
    if (msg) {
      replace(this.host, h("div", { class: "warn-box health-banner", attrs: { role: "alert", "data-role": "health-banner" } }, h("p", { text: msg })));
      this.host.removeAttribute("hidden");
    } else {
      replace(this.host);
      this.host.setAttribute("hidden", "");
    }
  }
};

// web/agentflow/js/overview.ts
import { confirmDialog as confirmDialog3, copyText as copyText2 } from "/shared/dist/shared.mjs";

// web/agentflow/js/forms.ts
import { createToggle } from "/shared/dist/shared.mjs";

// web/agentflow/js/icons.ts
var SVG_NS = "http://www.w3.org/2000/svg";
var line = (x1, y1, x2, y2) => ["line", { x1: String(x1), y1: String(y1), x2: String(x2), y2: String(y2) }];
var poly = (points) => ["polyline", { points }];
var path = (d) => ["path", { d }];
var SHAPES = {
  save: [path("M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"), poly("17 21 17 13 7 13 7 21"), poly("7 3 7 8 15 8")],
  add: [line(12, 5, 12, 19), line(5, 12, 19, 12)],
  remove: [line(6, 6, 18, 18), line(18, 6, 6, 18)],
  trash: [poly("3 6 5 6 21 6"), path("M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"), path("M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2")],
  edit: [path("M17 3a2.83 2.83 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z")],
  cancel: [line(6, 6, 18, 18), line(18, 6, 6, 18)],
  approve: [poly("20 6 9 17 4 12")],
  start: [poly("6 4 20 12 6 20")],
  pause: [line(8, 5, 8, 19), line(16, 5, 16, 19)],
  stop: [["rect", { x: "6", y: "6", width: "12", height: "12", rx: "1" }]],
  retry: [poly("1 4 1 10 7 10"), path("M3.51 15a9 9 0 1 0 2.13-9.36L1 10")],
  refresh: [poly("23 4 23 10 17 10"), poly("1 20 1 14 7 14"), path("M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15")],
  copy: [["rect", { x: "9", y: "9", width: "13", height: "13", rx: "2" }], path("M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1")],
  up: [line(12, 19, 12, 5), poly("5 12 12 5 19 12")],
  folder: [path("M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z")],
  probe: [["circle", { cx: "11", cy: "11", r: "8" }], line(21, 21, 16.65, 16.65)],
  view: [path("M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"), ["circle", { cx: "12", cy: "12", r: "3" }]],
  import: [path("M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"), poly("17 8 12 3 7 8"), line(12, 3, 12, 15)],
  sparkle: [path("M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"), path("M19 15l.7 1.8 1.8.7-1.8.7L19 20l-.7-1.8-1.8-.7 1.8-.7z")],
  split: [["circle", { cx: "6", cy: "6", r: "3" }], ["circle", { cx: "6", cy: "18", r: "3" }], line(20, 4, 8.12, 15.88), line(14.47, 14.48, 20, 20), line(8.12, 8.12, 12, 12)],
  warn: [path("M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"), line(12, 9, 12, 13), line(12, 17, 12.01, 17)],
  next: [line(5, 12, 19, 12), poly("12 5 19 12 12 19")],
  gauge: [path("M3.34 19a10 10 0 1 1 17.32 0"), line(12, 14, 16, 10)]
};
function icon(name) {
  const svg = document.createElementNS(SVG_NS, "svg");
  const attrs = {
    viewBox: "0 0 24 24",
    width: "1em",
    height: "1em",
    "aria-hidden": "true",
    focusable: "false",
    fill: "none",
    stroke: "currentColor",
    "stroke-width": "2",
    "stroke-linecap": "round",
    "stroke-linejoin": "round"
  };
  for (const [k, v] of Object.entries(attrs)) svg.setAttribute(k, v);
  for (const [tag, a] of SHAPES[name]) {
    const node = document.createElementNS(SVG_NS, tag);
    for (const [k, v] of Object.entries(a)) node.setAttribute(k, v);
    svg.append(node);
  }
  return svg;
}

// web/agentflow/js/forms.ts
function textInput(name, value, opts = {}) {
  const el = h("input", {
    class: "settings-input",
    attrs: { type: opts.type ?? "text", name, "data-field": name, placeholder: opts.placeholder, readonly: opts.readOnly }
  });
  el.value = value;
  return el;
}
function textArea(name, value, rows = 4) {
  const el = h("textarea", { class: "settings-input", attrs: { name, "data-field": name, rows } });
  el.value = value;
  return el;
}
function checkbox(name, checked) {
  const wrap = createToggle({ checked, onChange: () => void 0 });
  const input = wrap.firstChild;
  input.setAttribute("name", name);
  wrap.setAttribute("data-field", name);
  Object.defineProperty(wrap, "checked", { get: () => input.checked, set: (v) => {
    input.checked = v;
  } });
  return wrap;
}
function selectInput(name, options, value) {
  const el = h(
    "select",
    { class: "settings-input", attrs: { name, "data-field": name } },
    ...options.map(([v, label]) => h("option", { text: label, attrs: { value: v } }))
  );
  el.value = options.some(([v]) => v === value) ? value : options[0]?.[0] ?? "";
  return el;
}
function fieldRow(label, control, hint) {
  const err = h("div", { class: "field-error", attrs: { role: "alert", hidden: true } });
  const row = h(
    "div",
    { class: "wizard-field" },
    h("label", { text: label }),
    control,
    hint ? h("div", { class: "field-hint", text: hint }) : null,
    err
  );
  return {
    el: row,
    control,
    error(msg) {
      err.textContent = msg;
      if (msg) err.removeAttribute("hidden");
      else err.setAttribute("hidden", "");
    }
  };
}
function button(label, onClick, opts = {}) {
  const glyph = opts.icon !== void 0;
  const asIcon = glyph && !opts.word;
  return h("button", {
    class: "ui-modal-btn" + (opts.primary ? " ui-modal-btn-primary" : "") + (glyph ? " af-has-icon" : "") + (asIcon ? " af-icon-only" : ""),
    text: glyph ? void 0 : label,
    attrs: { type: "button", "data-action": opts.field, "data-op": opts.op, title: opts.unavailable || (glyph ? label : void 0), "aria-label": asIcon ? label : void 0, disabled: !!opts.unavailable, "aria-disabled": opts.unavailable ? "true" : void 0 },
    on: { click: opts.unavailable ? () => {
    } : onClick }
  }, glyph ? icon(opts.icon) : null, glyph && opts.word ? h("span", { text: label }) : null);
}
function section(title, ...children) {
  return h("div", { class: "settings-section" }, h("h4", { text: title }), ...children);
}
function badge(label, kind = "paid") {
  return h("span", { class: `badge badge-${kind}`, text: label });
}
function matchKey(field, keys) {
  if (!field) return void 0;
  const set = new Set(keys);
  const tries = [field, field.replace(/^limits\./, ""), field.replace(/\[.*$/, ""), field.replace(/^limits\./, "").replace(/[[.].*$/, "")];
  return tries.find((k) => set.has(k));
}
var FormErrors = class {
  constructor() {
    this.general = h("div", { class: "field-error", attrs: { role: "alert", hidden: true } });
    this.fields = /* @__PURE__ */ new Map();
  }
  add(key, f) {
    this.fields.set(key, f);
    return f;
  }
  clear() {
    for (const f of this.fields.values()) f.error("");
    this.setGeneral("");
  }
  setGeneral(msg) {
    this.general.textContent = msg;
    if (msg) this.general.removeAttribute("hidden");
    else this.general.setAttribute("hidden", "");
  }
  /** show displays err next to its field, or in the form-level line when it names none. */
  show(err) {
    const message = err instanceof Error ? err.message : String(err);
    const key = err instanceof ApiError ? matchKey(err.field, this.fields.keys()) : void 0;
    if (key) this.fields.get(key)?.error(message);
    else this.setGeneral(message);
  }
};
var RowsEditor = class {
  constructor(build, items, blank, addLabel, onChange = () => {
  }) {
    this.build = build;
    this.blank = blank;
    this.onChange = onChange;
    this.rows = [];
    this.list = h("div", { class: "wizard-list" });
    for (const it of items) this.addRow(it);
    this.el = h("div", {}, this.list, button(addLabel, () => this.addRow(this.blank(), true), { field: "add-row", icon: "add" }));
  }
  addRow(item, notify = false) {
    const ctl = this.build(item);
    this.rows.push(ctl);
    const row = h(
      "div",
      { class: "inline-row", attrs: { "data-row": this.rows.length - 1 } },
      ctl.el,
      button("Remove", () => {
        this.rows.splice(this.rows.indexOf(ctl), 1);
        this.list.removeChild(row);
        this.onChange();
      }, { field: "remove-row", icon: "remove" })
    );
    this.list.appendChild(row);
    if (notify) this.onChange();
  }
  read() {
    return this.rows.map((r) => r.read());
  }
};

// web/agentflow/js/yolo.ts
import { confirmDialog, showToast as showToast2 } from "/shared/dist/shared.mjs";
var defaultYoloDeps = () => ({
  setYOLO: api.setYOLO,
  yoloInvariants: api.yoloInvariants,
  confirm: (m, label = "Confirm") => confirmDialog(m, { confirmLabel: label }),
  notify: (m) => void showToast2(m, "success")
});
function yoloBadge() {
  return h("span", { class: "badge badge-warn", text: "YOLO", attrs: { "data-role": "yolo-badge", title: "YOLO is on: the human gates approve themselves" } });
}
function confirmText(effort, invariants) {
  return `Enable YOLO for effort #${effort.id}? The FRD-to-plan, plan-to-queue and queue approvals will happen automatically, and a stuck task is retried once. Still enforced:
` + invariants.map((i) => `- ${i.id}: ${i.text}`).join("\n");
}
var invariantCache = /* @__PURE__ */ new WeakMap();
function invariantsFor(deps) {
  let p = invariantCache.get(deps);
  if (!p) {
    p = deps.yoloInvariants();
    invariantCache.set(deps, p);
    p.catch(() => invariantCache.delete(deps));
  }
  return p;
}
function yoloControl(slug, effort, deps, onChanged) {
  const listHost = h("div", { attrs: { "data-role": "yolo-invariants" } });
  let invariants = [];
  const loading = invariantsFor(deps).then((inv) => {
    invariants = inv;
    replace(listHost, h(
      "details",
      {},
      h("summary", { text: "Always enforced, even under YOLO" }),
      h("ul", { class: "plan-review-notes" }, ...inv.map((i) => h("li", {}, h("strong", { text: i.id }), ` ${i.text}`)))
    ));
  }).catch(() => void 0);
  const toggle = async () => {
    if (effort.yolo) {
      try {
        await deps.setYOLO(slug, effort.id, false, false);
      } catch {
        return;
      }
      deps.notify("YOLO disabled");
    } else {
      await loading;
      if (!await deps.confirm(confirmText(effort, invariants), "Enable YOLO")) return;
      try {
        await deps.setYOLO(slug, effort.id, true, true);
      } catch {
        return;
      }
      deps.notify("YOLO enabled");
    }
    onChanged();
  };
  if (effort.status === "complete" || effort.status === "abandoned") return h("div", { attrs: { "data-role": "yolo" } }, effort.yolo ? yoloBadge() : null);
  return h(
    "div",
    { class: "detail-section", attrs: { "data-role": "yolo" } },
    h(
      "div",
      { class: "row-actions" },
      effort.yolo ? yoloBadge() : null,
      button(effort.yolo ? "Disable YOLO" : "Enable YOLO", () => void toggle(), { field: "toggle-yolo", icon: effort.yolo ? "cancel" : "approve", word: true })
    ),
    listHost
  );
}

// web/agentflow/js/current.ts
var picked = /* @__PURE__ */ new Map();
function isFinished(e) {
  return e.status === "complete" || e.status === "abandoned";
}
function chooseEffort(slug, efforts) {
  const want = picked.get(slug);
  return efforts.find((e) => e.id === want) ?? efforts.find((e) => !isFinished(e)) ?? efforts[efforts.length - 1];
}
function rememberEffort(slug, id) {
  picked.set(slug, id);
}
function subTabBlock(id, ef) {
  if (id === "frd") return "";
  if (!ef) return "Create an effort on the FRD tab first";
  switch (id) {
    case "plan":
      return ef.frd.latest > 0 ? "" : "Available once the FRD is saved";
    case "queue":
      return ef.queue || ef.status === "plan_approved" || ef.status === "compiling" ? "" : "Available once the plan is approved";
    case "board":
      return ef.queue?.status === "approved" ? "" : "Available once the queue is approved";
    default:
      return "";
  }
}
function forgetEfforts() {
  picked.clear();
}
function effortBar(slug, efforts, current, onPick, yolo) {
  const sel = selectInput("effort", efforts.map((e) => [String(e.id), `#${e.id} ${e.title}`]), String(current?.id ?? ""));
  sel.addEventListener("change", () => {
    rememberEffort(slug, Number(sel.value));
    onPick(Number(sel.value));
  });
  return h(
    "div",
    { class: "console-header" },
    h("span", { class: "pane-title", text: "Effort" }),
    sel,
    current ? chip(current.status) : null,
    current?.yolo && !yolo ? yoloBadge() : null,
    current && yolo ? yoloControl(slug, current, yolo.deps, yolo.onChanged) : null
  );
}

// web/agentflow/js/merge-card.ts
function mergeBranch(ef, git) {
  return git?.branch || ef.integration_branch || `agentflow/effort-${ef.id}`;
}
function mergeCard(ef, git, copy) {
  const branch = mergeBranch(ef, git);
  const files = git?.files;
  return h(
    "div",
    { class: "merge-card", attrs: { "data-role": "git-commands" } },
    h("h4", { text: `Ready to merge: ${ef.title}` }),
    h("p", { text: `Nothing was merged into main. The finished work is on the branch ${branch}; main is unchanged until you merge it yourself. agentflow never merges into main and never pushes.` }),
    git?.commits?.length ? h("ul", { class: "plan-review-notes", attrs: { "data-role": "merge-commits" } }, ...git.commits.map((c) => h("li", { text: `${c.sha.slice(0, 7)} ${c.subject}` }))) : null,
    files !== void 0 || git?.diffstat ? h(
      "div",
      { attrs: { "data-role": "merge-diffstat" } },
      files !== void 0 ? h("p", { class: "field-hint", text: `${files} file${files === 1 ? "" : "s"} changed` }) : null,
      git?.diffstat ? h("pre", { class: "detail-mono", text: git.diffstat }) : null
    ) : null,
    git?.commands.length ? h(
      "div",
      {},
      h("p", { text: "To merge it, run:" }),
      ...git.commands.map((c) => h(
        "div",
        { class: "merge-command" },
        h("pre", { class: "detail-mono", text: c }),
        h("button", { class: "icon-btn", attrs: { type: "button", title: "Copy this command", "aria-label": "Copy this command", "data-action": "copy-command" }, on: { click: () => copy(c) } }, icon("copy"))
      ))
    ) : h("p", { class: "field-hint", text: `The commands could not be loaded. To merge, run: git merge --no-ff ${branch}` })
  );
}

// web/agentflow/js/unregister.ts
import { confirmDialog as confirmDialog2, copyText, openModal, showToast as showToast3 } from "/shared/dist/shared.mjs";
var defaultUnregisterDeps = () => ({
  deleteProject: api.deleteProject,
  confirm: (m, label = "Confirm") => confirmDialog2(m, { confirmLabel: label }),
  notify: (m) => void showToast3(m, "success"),
  showCommands: (slug, commands) => showBranchCommands(slug, commands, (t) => void copyText(t))
});
function unregisterBlock(p) {
  if (p.run_state !== "stopped") return "Stop the run first";
  if (p.effort) return "Finish or abandon the active effort first";
  return "";
}
async function unregister(deps, slug) {
  if (!await deps.confirm(`Unregister ${slug}? Its history is removed; the repository is not touched.`, "Unregister")) return false;
  let res;
  try {
    res = await deps.deleteProject(slug);
  } catch {
    return false;
  }
  deps.notify(`Unregistered ${slug}`);
  if (res.commands?.length) deps.showCommands(slug, res.commands);
  return true;
}
function branchCommands(slug, commands, copy) {
  return h(
    "div",
    { attrs: { "data-role": "branch-commands" } },
    h("p", { text: `${slug} is unregistered. Its branches are still in the repository; to delete them, run:` }),
    ...commands.map((c) => h(
      "div",
      { class: "merge-command" },
      h("pre", { class: "detail-mono", text: c }),
      h("button", { class: "icon-btn", attrs: { type: "button", title: "Copy this command", "aria-label": "Copy this command", "data-action": "copy-command" }, on: { click: () => copy(c) } }, icon("copy"))
    ))
  );
}
function showBranchCommands(slug, commands, copy) {
  openModal(branchCommands(slug, commands, copy), { title: "Branches left behind" });
}
var defaultDropAllDeps = () => ({
  dropAll: api.dropAllProjects,
  confirm: (m, label = "Confirm") => confirmDialog2(m, { confirmLabel: label }),
  notify: (m) => void showToast3(m, "success")
});
async function dropAll(deps, beforeDrop = () => {
}) {
  const warning = "Drop every project and effort? All their history, statistics and strikes are removed from AgentFlow, and any run in progress is stopped. Your backends, models, tiers, keys, candidate lists and settings stay, and so do your repositories and their branches. A copy of the database is saved first. This cannot be undone from here.";
  if (!await deps.confirm(warning, "Drop all")) return false;
  let res;
  beforeDrop();
  try {
    res = await deps.dropAll();
  } catch {
    return false;
  }
  deps.notify(`Dropped ${res.dropped.length} project${res.dropped.length === 1 ? "" : "s"}; database copy saved at ${res.archive}`);
  return true;
}

// web/agentflow/js/overview.ts
var defaultDeps = (open) => ({
  list: () => api.projects(),
  run: (slug, op) => api.run(slug, op),
  confirm: (m) => confirmDialog3(m, { confirmLabel: "Stop" }),
  open,
  efforts: api.efforts,
  gitCommands: api.gitCommands,
  copy: (t) => void copyText2(t),
  unregister: (slug) => unregister(defaultUnregisterDeps(), slug),
  dropAll: (before) => dropAll(defaultDropAllDeps(), before)
});
function buttonsFor(runState) {
  switch (runState) {
    case "running":
      return [{ op: "pause", label: "Pause", icon: "pause" }, { op: "stop", label: "Stop", icon: "stop" }];
    case "pausing":
      return [{ op: "start", label: "Resume", primary: true, icon: "start" }, { op: "stop", label: "Stop", icon: "stop" }];
    case "paused":
      return [
        { op: "start", label: "Resume", primary: true, icon: "start" },
        { op: "run-one", label: "Run one", icon: "start", word: true },
        { op: "stop", label: "Stop", icon: "stop" }
      ];
    default:
      return [{ op: "start", label: "Start", primary: true, icon: "start" }, { op: "run-one", label: "Run one", icon: "start", word: true }];
  }
}
function progressText(progress) {
  const total = progress.total ?? 0;
  const done = (progress.passed ?? 0) + (progress.overridden ?? 0);
  const pct2 = total > 0 ? Math.round(done / total * 100) : 0;
  const rest = Object.keys(progress).filter((k) => k !== "total" && k !== "passed" && progress[k] > 0).sort().map((k) => `${k.replace(/_/g, " ")} ${progress[k]}`);
  const head = total > 0 ? `${pct2}% \u2014 passed ${progress.passed ?? 0}/${total}` : "no queue yet";
  return { pct: pct2, text: [head, ...rest].join(" \xB7 ") };
}
function workLine(w) {
  return `${w.role} \xB7 ${w.task || "effort"}${w.attempt ? ` (attempt ${w.attempt})` : ""} \xB7 ${w.backend}/${w.model} \xB7 ${w.state}`;
}
var Overview = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.rows = [];
    /** Per project, the newest effort when it is complete (the project row's active effort is then null). */
    this.done = /* @__PURE__ */ new Map();
    this.git = /* @__PURE__ */ new Map();
    this.loaded = false;
    this.refreshing = false;
    /** true while Drop all projects runs: nothing is fetched and the stream is ignored. */
    this.dropping = false;
    this.again = false;
    this.off = null;
    this.stopPoll = null;
  }
  /** mount fetches the rows and starts listening to the event stream; it returns the dispose. */
  mount() {
    this.off = subscribe((ev) => this.handle(ev));
    this.stopPoll = startPoll(() => this.rows.some((r) => isTransient(r.effort?.status)), () => this.refresh());
    void this.refresh();
    return () => {
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
    };
  }
  /** refresh refetches the rows; overlapping calls collapse into one follow-up fetch. */
  async refresh() {
    if (this.dropping) return;
    if (this.refreshing) {
      this.again = true;
      return;
    }
    this.refreshing = true;
    try {
      do {
        this.again = false;
        try {
          const rows = await this.deps.list();
          if (this.dropping) return;
          this.rows = rows;
          await this.loadCompleted();
          this.loaded = true;
        } catch {
        }
        this.render();
      } while (this.again);
    } finally {
      this.refreshing = false;
    }
  }
  /** loadCompleted finds, for each project without an active effort, its newest effort when that is complete, with its git commands. */
  async loadCompleted() {
    const { efforts, gitCommands } = this.deps;
    this.done.clear();
    if (!efforts) return;
    for (const p of this.rows.filter((r) => !r.effort)) {
      try {
        const newest = (await efforts(p.slug)).reduce((m, e) => !m || e.id > m.id ? e : m, void 0);
        if (!newest || newest.status !== "complete") continue;
        this.done.set(p.slug, newest);
        if (gitCommands && !this.git.has(newest.id)) this.git.set(newest.id, await gitCommands(p.slug, newest.id, { quiet: true }).catch(() => null));
      } catch {
      }
    }
  }
  /** handle applies one stream event: run-state changes show at once, the rest refetch. */
  handle(ev) {
    if (this.dropping) return;
    switch (ev.type) {
      case "snapshot": {
        let stale = ev.projects.length !== this.rows.length;
        for (const p of ev.projects) {
          const row = this.rows.find((r) => r.slug === p.project);
          if (!row || (row.effort?.id ?? 0) !== p.effort) stale = true;
          else row.run_state = p.state;
        }
        this.render();
        if (stale) void this.refresh();
        return;
      }
      case "project.run_state": {
        const row = this.rows.find((r) => r.slug === ev.project);
        if (row) {
          row.run_state = ev.state;
          this.render();
        }
        void this.refresh();
        return;
      }
      case "project.update":
      case "project.delete":
      case "effort.update":
      case "task.update":
      case "state.changed":
        void this.refresh();
        return;
      default:
        return;
    }
  }
  async control(slug, op) {
    if (op === "stop" && !await this.deps.confirm(`Stop ${slug}? Running stages are cancelled.`)) return;
    try {
      const view = await this.deps.run(slug, op);
      const i = this.rows.findIndex((r) => r.slug === slug);
      if (i >= 0) this.rows[i] = view;
      this.render();
    } catch {
    }
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading projects\u2026" }));
      return;
    }
    if (this.dropping) {
      replace(this.host, h("p", { class: "field-hint", text: "Dropping every project\u2026" }));
      return;
    }
    if (this.rows.length === 0) {
      replace(
        this.host,
        h(
          "div",
          { class: "plan-empty" },
          h("h3", { text: "No projects yet" }),
          h("p", { text: "Create a project to get started." }),
          button("New project", () => this.deps.open("+new"), { primary: true, op: "new-project", icon: "add" })
        )
      );
      return;
    }
    const drop = this.deps.dropAll;
    replace(
      this.host,
      drop ? h(
        "div",
        { class: "settings-actions" },
        button("Drop all projects", () => void this.dropEverything(), { op: "drop-all", icon: "trash", word: true })
      ) : null,
      h("ul", { class: "task-list" }, ...this.rows.map((r) => this.row(r)))
    );
  }
  /**
   * dropEverything lets go of the screen first: the rows go, nothing is fetched
   * and the stream is ignored while the request runs, so no request names a
   * project that is vanishing. Afterwards (or if it failed or was declined) the
   * list is read again.
   */
  async dropEverything() {
    try {
      await this.deps.dropAll?.(() => {
        this.dropping = true;
        this.rows = [];
        this.done.clear();
        this.git.clear();
        forgetEfforts();
        this.render();
      });
    } finally {
      this.dropping = false;
      await this.refresh();
    }
  }
  async unregisterRow(slug) {
    const row = this.rows.find((r) => r.slug === slug);
    if (!row || unregisterBlock(row) || !await this.deps.unregister?.(slug)) return;
    await this.refresh();
  }
  row(p) {
    const { pct: pct2, text } = progressText(p.progress ?? {});
    const ef = p.effort;
    const bar = h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar", attrs: { style: `width: ${pct2}%` } }));
    const work = (p.current ?? []).length > 0 ? (p.current ?? []).map((w) => h("div", { class: "task-item-meta", text: workLine(w) })) : [h("div", { class: "task-item-meta", text: ef ? "no stage running" : "no active effort" })];
    const runnable = p.run_state !== "stopped" || !!ef && ["queue_approved", "running", "paused"].includes(ef.status);
    const buttons = (runnable ? buttonsFor(p.run_state) : []).map((b) => button(b.label, () => void this.control(p.slug, b.op), { primary: b.primary, op: b.op, icon: b.icon, word: b.word }));
    const why = unregisterBlock(p);
    const trash = this.deps.unregister ? button(why ? `Unregister: ${why}` : "Unregister", () => void this.unregisterRow(p.slug), { field: "unregister-project", icon: "trash" }) : null;
    if (trash && why) trash.disabled = true;
    return h(
      "li",
      { class: "task-item", attrs: { "data-project": p.slug } },
      h(
        "div",
        { class: "task-item-meta" },
        h("a", { class: "task-item-title", text: p.slug, attrs: { href: "#", "data-open": p.slug }, on: {
          click: (e) => {
            e.preventDefault();
            this.deps.open(p.slug);
          }
        } }),
        runnable ? chip(p.run_state) : null,
        ef?.yolo ? yoloBadge() : null
      ),
      h("div", { class: "task-item-meta", text: ef ? `${ef.title} \xB7 ${ef.status.replace(/_/g, " ")}` : "no active effort" }),
      bar,
      h("div", { class: "progress-pct", text }),
      ...work,
      h("div", { class: "task-item-meta" }, ...buttons, trash),
      this.done.has(p.slug) ? mergeCard(this.done.get(p.slug), this.git.get(this.done.get(p.slug).id) ?? null, (t) => this.deps.copy?.(t)) : null
    );
  }
};

// web/agentflow/js/backends.ts
import { confirmDialog as confirmDialog4, showToast as showToast5 } from "/shared/dist/shared.mjs";

// web/agentflow/js/installs.ts
var defaultInstallDeps = () => ({
  installs: (b) => api.installs(b),
  start: (b, m, t) => api.startInstall(b, m, t),
  cancel: (id) => api.cancelInstall(id)
});
var TIERS = [["0", "not rated"], ["1", "tier 1 (strong)"], ["2", "tier 2 (capable)"], ["3", "tier 3 (light)"]];
function size(n) {
  if (n >= 1e9) return `${(n / 1e9).toFixed(1)} GB`;
  if (n >= 1e6) return `${Math.round(n / 1e6)} MB`;
  return `${n} B`;
}
function progressText2(i) {
  if (i.state === "running" && i.total > 0) return `${Math.round(100 * i.completed / i.total)}% of ${size(i.total)}`;
  return i.status;
}
var InstallPanel = class {
  constructor(backend, deps) {
    this.backend = backend;
    this.deps = deps;
    /** el is the panel's own root: it is re-rendered in place, so a card can move it. */
    this.el = h("div", { class: "install-panel", attrs: { "data-role": "install-panel" } });
    this.items = [];
    this.error = "";
    this.model = "";
    this.tier = "0";
    this.off = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      if (ev.type !== "install.update" || ev.backend !== this.backend) return;
      const known = this.items.find((i) => i.id === ev.id);
      if (known) Object.assign(known, { state: ev.state, status: ev.status, completed: ev.completed, total: ev.total });
      else void this.load();
      this.render();
      if (known && ev.state !== "running") void this.load();
    });
    void this.load();
  }
  unmount() {
    this.off?.();
    this.off = null;
  }
  async load() {
    try {
      this.items = await this.deps.installs(this.backend);
    } catch {
    }
    this.render();
  }
  async start() {
    const model = this.model.trim();
    this.error = "";
    try {
      await this.deps.start(this.backend, model, Number(this.tier));
      this.model = "";
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
    }
    await this.load();
  }
  row(i) {
    const kind = i.state === "done" ? "ok" : i.state === "running" ? "muted" : "warn";
    return h(
      "div",
      { class: "install-row", attrs: { "data-install": String(i.id) } },
      h("strong", { text: i.model }),
      badge(i.state, kind),
      i.state === "running" && i.total > 0 ? h("progress", { attrs: { value: String(i.completed), max: String(i.total) } }) : null,
      h("span", { class: "field-hint", text: i.error ? i.error : progressText2(i) }),
      i.state === "running" ? button("Cancel", () => {
        void this.deps.cancel(i.id).then(() => this.load());
      }, { field: "cancel-install" }) : null
    );
  }
  render() {
    const model = textInput("install-model", this.model, { placeholder: "model to install, e.g. devstral-small-2" });
    model.addEventListener("input", () => {
      this.model = model.value;
    });
    const tier = selectInput("install-tier", TIERS, this.tier);
    tier.addEventListener("change", () => {
      this.tier = tier.value;
    });
    replace(
      this.el,
      h(
        "div",
        { class: "install-form inline-row" },
        model,
        tier,
        button("Install", () => {
          this.model = model.value;
          this.tier = tier.value;
          void this.start();
        }, { field: "install", primary: true, word: true, icon: "add" })
      ),
      this.error ? h("div", { class: "field-error", text: this.error, attrs: { "data-role": "install-error" } }) : null,
      ...this.items.slice(0, 5).map((i) => this.row(i))
    );
  }
};

// web/agentflow/js/candidates.ts
var ROLES = ["worker", "reviewer", "planner", "architect", "critic", "compiler", "resolver"];
function isPaid(backends, backend, model) {
  const b = backends.find((x) => x.name === backend);
  return !!b && (b.paid || b.models.some((m) => m.model === model && m.paid));
}
function unconsented(backends, rows) {
  return rows.filter((r) => isPaid(backends, r.backend, r.model) && !r.optIn);
}
function moveItem(list3, from, to) {
  const out = list3.slice();
  if (from < 0 || from >= out.length) return out;
  const [it] = out.splice(from, 1);
  out.splice(Math.max(0, Math.min(out.length, to)), 0, it);
  return out;
}
function dropIndex(mids, from, y) {
  return mids.filter((m, i) => i !== from && m < y).length;
}
function attachDrag(handle, row, getMove) {
  const release = (y) => {
    if (!Number.isFinite(y)) return;
    row.classList.remove("dragging");
    const rows = Array.from(row.parentElement?.children ?? []);
    const mids = rows.map((r) => {
      const b = r.getBoundingClientRect();
      return b.top + b.height / 2;
    });
    getMove()(dropIndex(mids, rows.indexOf(row), y));
  };
  handle.addEventListener("mousedown", (e) => {
    e.preventDefault();
    row.classList.add("dragging");
    const onUp = (ev) => {
      document.removeEventListener("mouseup", onUp);
      release(ev.clientY);
    };
    document.addEventListener("mouseup", onUp);
  });
  handle.addEventListener("touchstart", () => {
    row.classList.add("dragging");
    let y = NaN;
    const onMove = (ev) => {
      y = ev.touches[0].clientY;
      ev.preventDefault();
    };
    const onEnd = () => {
      handle.removeEventListener("touchmove", onMove);
      handle.removeEventListener("touchend", onEnd);
      release(y);
    };
    handle.addEventListener("touchmove", onMove, { passive: false });
    handle.addEventListener("touchend", onEnd);
  }, { passive: true });
}
function buildRoleEditor(o) {
  const { role } = o;
  const host = h("div", { class: "profile-card", attrs: { "data-role-editor": role } });
  const errors = new FormErrors();
  let focusIdx = -1;
  const edit = (fn) => {
    o.setDraft(fn(o.rows().map((r) => ({ ...r }))));
    draw();
    auto.touch();
  };
  const active2 = o.backends.filter((b) => !b.retired);
  const pickBackend = selectInput("backend", active2.map((b) => [b.name, b.name]), active2[0]?.name ?? "");
  const pickModel = selectInput("model", [], "");
  const fillModels = () => {
    const b = active2.find((x) => x.name === pickBackend.value);
    replace(pickModel, ...(b?.models ?? []).map((m) => h("option", { text: m.model + (m.paid ? " (paid)" : ""), attrs: { value: m.model } })));
    pickModel.value = b?.models[0]?.model ?? "";
  };
  pickBackend.addEventListener("change", fillModels);
  fillModels();
  const save = async () => {
    errors.clear();
    if (o.rows().length === 0) {
      errors.show(new Error("A role needs at least one candidate: pick a backend and model and press Add candidate"));
      return false;
    }
    const missing = unconsented(o.backends, o.rows());
    if (missing.length > 0) {
      draw();
      errors.show(new Error(`Tick the paid opt-in for: ${missing.map((m) => `${m.backend}/${m.model}`).join(", ")}`));
      return false;
    }
    try {
      await o.save(o.rows().map((r) => ({ backend: r.backend, model: r.model })));
    } catch (err) {
      errors.show(err);
      return false;
    }
    o.setDraft(null);
    draw();
    return true;
  };
  const auto = autosave({ snapshot: () => JSON.stringify(o.rows()), save, startDirty: o.hasDraft() });
  if (o.hasDraft()) auto.touch();
  const handles = [];
  const draw = () => {
    const list3 = o.rows();
    handles.length = 0;
    replace(
      host,
      h("div", { class: "profile-header" }, h("strong", { text: role }), o.hasDraft() ? badge("unsaved", "warn") : null),
      h("div", { class: "wizard-list" }, ...list3.length === 0 ? [h("p", { class: "field-hint", text: o.emptyNote })] : list3.map((r, i) => {
        const paid = isPaid(o.backends, r.backend, r.model);
        const optIn = checkbox("opt_in", r.optIn);
        optIn.addEventListener("change", () => edit((all) => {
          all[i].optIn = optIn.checked;
          return all;
        }));
        const row = h("div", { class: "inline-row", attrs: { "data-candidate": `${role}:${i}` } });
        const handle = h("span", {
          class: "drag-handle",
          text: "\u283F",
          attrs: { role: "button", tabindex: 0, "data-action": "reorder", "aria-label": `Reorder ${r.backend} / ${r.model}: drag, or press the up and down arrow keys`, title: "Drag to reorder (or use the up and down arrow keys)" }
        });
        handles.push(handle);
        const moveTo = (to) => {
          if (to < 0 || to >= list3.length || to === i) return;
          focusIdx = to;
          edit((all) => moveItem(all, i, to));
        };
        handle.addEventListener("keydown", (ev) => {
          const key = ev.key;
          if (key !== "ArrowUp" && key !== "ArrowDown") return;
          ev.preventDefault();
          moveTo(i + (key === "ArrowUp" ? -1 : 1));
        });
        attachDrag(handle, row, () => moveTo);
        replace(
          row,
          handle,
          h("span", { text: `${i + 1}. ${r.backend} / ${r.model}` }),
          paid ? badge("paid") : null,
          paid ? h("span", { class: "inline-check" }, optIn, "I opt in to paid use") : null,
          button("Remove", () => edit((all) => all.filter((_, j) => j !== i)), { field: "remove", icon: "remove" })
        );
        return row;
      })),
      h(
        "div",
        { class: "inline-row" },
        pickBackend,
        pickModel,
        button("Add candidate", () => {
          if (pickBackend.value && pickModel.value) {
            edit((all) => [...all, { backend: pickBackend.value, model: pickModel.value, optIn: !isPaid(o.backends, pickBackend.value, pickModel.value) }]);
          }
        }, { field: "add-candidate", icon: "add" })
      ),
      errors.general,
      h(
        "div",
        { class: "settings-actions" },
        ...o.extra ?? [],
        auto.status,
        o.hasDraft() ? button("Discard", () => {
          auto.stop();
          o.setDraft(null);
          draw();
        }, { field: "discard", icon: "cancel" }) : null
      )
    );
    if (focusIdx >= 0) handles[focusIdx]?.focus();
    focusIdx = -1;
  };
  draw();
  return host;
}

// web/agentflow/js/configfile.ts
var defaultConfigDeps = () => ({ importConfig: (c, apply) => api.importConfig(c, apply) });
function describe(r) {
  const out = [];
  if (r.created_backends.length) out.push(`create backend(s): ${r.created_backends.join(", ")}`);
  if (r.updated_backends.length) out.push(`update backend(s), replacing their model lists: ${r.updated_backends.join(", ")}`);
  if (r.candidate_roles.length) out.push(`replace the candidate list of: ${r.candidate_roles.join(", ")}`);
  if (r.settings.length) out.push(`set: ${r.settings.join(", ")}`);
  if (out.length === 0) out.push("nothing to change");
  return out;
}
var ConfigPanel = class {
  constructor(deps, onApplied = () => {
  }) {
    this.deps = deps;
    this.onApplied = onApplied;
    this.el = h("div", { class: "config-panel", attrs: { "data-role": "config-panel" } });
    this.pending = null;
    this.report = null;
    this.error = "";
    this.done = "";
  }
  /** loadText reads the chosen file's text and asks the server what importing it would do. */
  async loadText(text) {
    this.error = "";
    this.done = "";
    this.report = null;
    this.pending = null;
    try {
      this.pending = JSON.parse(text);
    } catch {
      this.error = "That file is not JSON.";
      this.render();
      return;
    }
    try {
      this.report = await this.deps.importConfig(this.pending, false);
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
      this.pending = null;
    }
    this.render();
  }
  async apply() {
    if (this.pending === null) return;
    try {
      const r = await this.deps.importConfig(this.pending, true);
      this.done = `Imported: ${describe(r).join("; ")}.`;
      this.pending = null;
      this.report = null;
      this.error = "";
      this.onApplied();
    } catch (e) {
      this.error = e instanceof Error ? e.message : String(e);
    }
    this.render();
  }
  cancel() {
    this.pending = null;
    this.report = null;
    this.error = "";
    this.render();
  }
  render() {
    const file = h("input", { attrs: { type: "file", accept: ".json,application/json", "data-field": "config-file" } });
    file.addEventListener("change", () => {
      const f = file.files?.[0];
      if (f) void f.text().then((t) => this.loadText(t));
    });
    const r = this.report;
    replace(
      this.el,
      h(
        "div",
        { class: "inline-row" },
        h("a", { class: "ui-modal-btn", text: "Export configuration", attrs: { href: "/api/config/export", download: "", "data-role": "export-config" } }),
        h("span", { class: "field-hint", text: "Import a configuration file:" }),
        file
      ),
      h("p", { class: "field-hint", text: "The file holds backends, models, tiers, tool flags, candidate lists and limits. It holds no projects and no API keys." }),
      this.error ? h("div", { class: "field-error", text: this.error, attrs: { "data-role": "config-error" } }) : null,
      this.done ? h("div", { class: "field-hint", text: this.done, attrs: { "data-role": "config-done" } }) : null,
      r ? h(
        "div",
        { class: "warn-box", attrs: { "data-role": "config-preview" } },
        h("strong", { text: "Importing this file would:" }),
        h("ul", {}, ...describe(r).map((t) => h("li", { text: t }))),
        ...r.warnings.map((w) => h("p", { class: "field-hint", text: w })),
        h(
          "div",
          { class: "inline-row" },
          button("Apply import", () => void this.apply(), { primary: true, field: "apply-import" }),
          button("Cancel", () => this.cancel(), { field: "cancel-import" })
        )
      ) : null
    );
  }
};

// web/agentflow/js/settings.ts
import { showToast as showToast4 } from "/shared/dist/shared.mjs";

// web/agentflow/js/dirpicker.ts
import { openModal as openModal2 } from "/shared/dist/shared.mjs";
var DirPicker = class {
  constructor(fsDirs, start, onChoose, onCancel) {
    this.fsDirs = fsDirs;
    this.start = start;
    this.onChoose = onChoose;
    this.onCancel = onCancel;
    this.cur = null;
    this.error = "";
    this.list = h("div", { class: "wizard-list", attrs: { "data-role": "dir-list" } });
    this.where = h("code", { attrs: { "data-role": "dir-path" } });
    this.err = h("div", { class: "field-error", attrs: { role: "alert", hidden: true } });
    this.up = button("Parent folder", () => void this.go(this.cur?.parent ?? ""), { field: "dir-up", icon: "up" });
    /** Bumped per request so a slow answer cannot overwrite a newer one. */
    this.seq = 0;
    this.el = h(
      "div",
      { class: "dir-picker", attrs: { "data-role": "dir-picker" } },
      h("div", { class: "row-actions" }, this.up, this.where),
      this.err,
      this.list,
      h(
        "div",
        { class: "settings-actions" },
        button("Cancel", () => this.onCancel(), { field: "dir-cancel", icon: "cancel" }),
        button("Choose this folder", () => {
          if (this.cur) this.onChoose(this.cur.path);
        }, { primary: true, field: "dir-choose", icon: "folder" })
      )
    );
  }
  /** mount shows the start directory, or the first browse root when it cannot be listed. */
  async mount() {
    if (!await this.go(this.start) && this.start) await this.go("");
  }
  /** go lists path; false (with the error shown) when the server refuses it. */
  async go(path2) {
    const mine = ++this.seq;
    try {
      const res = await this.fsDirs(path2);
      if (mine !== this.seq) return true;
      this.cur = res;
      this.error = "";
    } catch (err) {
      if (mine !== this.seq) return false;
      this.error = err instanceof Error ? err.message : String(err);
      this.draw();
      return false;
    }
    this.draw();
    return true;
  }
  draw() {
    const cur = this.cur;
    this.where.textContent = cur?.path ?? "";
    this.err.textContent = this.error;
    if (this.error) this.err.removeAttribute("hidden");
    else this.err.setAttribute("hidden", "");
    if (cur?.parent) this.up.removeAttribute("disabled");
    else this.up.setAttribute("disabled", "");
    replace(this.list, ...cur && cur.dirs.length > 0 ? cur.dirs.map((d) => h("button", {
      class: "ui-modal-btn dir-row",
      attrs: { type: "button", "data-dir": d.path, title: d.git ? `${d.path} (git repository)` : d.path },
      on: { click: () => void this.go(d.path) }
    }, d.name, d.git ? badge("git", "ok") : null)) : [h("p", { class: "field-hint", text: cur ? "No subfolders" : "" })]);
  }
};
function pickDirectory(start = "", fsDirs = api.fsDirs) {
  return new Promise((resolve) => {
    let picked2 = null;
    const body = h("div", {});
    const modal = openModal2(body, { title: "Choose a folder", onClose: () => resolve(picked2) });
    const picker = new DirPicker(fsDirs, start, (p) => {
      picked2 = p;
      modal.close();
    }, () => modal.close());
    body.appendChild(picker.el);
    void picker.mount();
  });
}

// web/agentflow/js/settings.ts
var LIMIT_FIELDS = [
  { key: "max_attempts", label: "Max attempts", hint: "Whole number, 2 to 50" },
  { key: "stuck_threshold", label: "Stuck threshold", hint: "Identical failures before a task is stuck; at most max attempts" },
  { key: "max_infra_retries", label: "Max infra retries", hint: "Whole number, 0 to 50" },
  { key: "plan_max_rounds", label: "Plan max rounds", hint: "ralplan rounds before the best plan is presented, 1 to 50" },
  { key: "acceptance_rounds", label: "Acceptance rounds", hint: "How many times a finished effort is checked against its FRD, with gaps turned into follow-up tasks; 0 turns the check off, 2 by default" },
  { key: "review_panel", label: "Review panel", hint: "How many different reviewer models must each pass a task before it merges; 1 to 3, 1 by default" },
  { key: "stall_minutes", label: "Stall minutes", hint: "Minutes a stage may say nothing, or a running effort sit idle, before it counts as stalled; 1 to 100, 10 by default" },
  { key: "worker_timeout", label: "Worker timeout", hint: "A duration such as 30m or 1h" },
  { key: "review_timeout", label: "Review timeout", hint: "A duration such as 15m" },
  { key: "plan_timeout", label: "Plan timeout", hint: "A duration such as 20m" },
  { key: "verify_timeout", label: "Verify timeout", hint: "A duration such as 20m" },
  { key: "resolver_mode", label: "Resolver mode", hint: "What the resolver may do with a stuck task: off, advise (its decision waits for you to apply it; the default) or auto (applied at once)" }
];
var EXECUTION_FIELDS = [
  { key: "verify_slots", label: "Verify slots", hint: "Verify and merge stages that run at once, 1 to 64" },
  { key: "context_floor_tokens", label: "Context floor", hint: "Tokens of context assumed for a model whose served window is not known, 2048 or more" },
  { key: "backend_cooldown", label: "Backend cooldown", hint: "How long an unreachable backend is skipped, such as 2m" }
];
var defaultSettingsDeps = () => ({
  settings: api.settings,
  putSettings: api.putSettings,
  opencodeVersion: api.opencodeVersion,
  restartStatus: api.restartStatus,
  restartService: api.restartService,
  cancelRestart: api.cancelRestart,
  serviceHold: api.serviceHold,
  holdService: api.holdService,
  liftHold: api.liftHold,
  pickDir: (start) => pickDirectory(start),
  notify: (m) => void showToast4(m, "success")
});
var str = (v) => v === void 0 || Array.isArray(v) ? "" : String(v);
var SettingsView = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.values = {};
    this.inputs = /* @__PURE__ */ new Map();
    /** The BotChat dispatch switch; off is mirror mode. It is saved as botchat_mode. */
    this.dispatch = null;
    this.errors = new FormErrors();
    this.oc = null;
    this.ocHost = h("div");
    this.roots = [];
    this.svc = null;
    this.hold = null;
    this.svcHost = h("div", { attrs: { "data-role": "service" } });
    this.svcTimer = null;
    this.rootsHost = h("div", { class: "wizard-list", attrs: { "data-role": "browse-roots" } });
    this.dirty = false;
    this.auto = null;
    this.off = null;
  }
  /** mount loads the settings and returns the dispose. */
  mount() {
    this.off = subscribe((ev) => {
      if (ev.type === "settings.update" && !this.dirty) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      if (this.svcTimer) clearInterval(this.svcTimer);
      this.svcTimer = null;
      void this.auto?.flush();
    };
  }
  async load() {
    try {
      const [settings, oc] = await Promise.all([this.deps.settings(), this.deps.opencodeVersion(false)]);
      const same = this.auto !== null && JSON.stringify(settings) === JSON.stringify(this.values);
      this.values = settings;
      this.oc = oc;
      this.svc = await this.deps.restartStatus({ quiet: true }).catch(() => null);
      this.hold = await this.deps.serviceHold({ quiet: true }).catch(() => null);
      if (same) return;
      this.roots = Array.isArray(settings.browse_roots) ? [...settings.browse_roots] : [];
    } catch {
      if (Object.keys(this.values).length === 0) return;
    }
    this.dirty = false;
    this.render();
  }
  row(f) {
    const input = textInput(f.key, str(this.values[f.key]));
    input.addEventListener("input", () => {
      this.dirty = true;
    });
    this.inputs.set(f.key, input);
    return this.errors.add(f.key, fieldRow(f.label, input, f.hint)).el;
  }
  renderOpenCode() {
    const oc = this.oc;
    replace(
      this.ocHost,
      h(
        "div",
        { class: "row-actions" },
        oc ? badge(oc.status === "ok" ? "available" : "unavailable", oc.status === "ok" ? "ok" : "warn") : null,
        h("span", {
          class: "field-hint",
          attrs: { "data-role": "oc-version" },
          text: oc ? oc.version ? `version ${oc.version}` : oc.error ?? "" : "not probed yet"
        }),
        button("Refresh", () => void this.refreshVersion(), { field: "oc-refresh", icon: "refresh" })
      )
    );
  }
  /**
   * The Service section: hold the whole service so it starts nothing new
   * (AF-10), and restart it once running work has ended (H25).
   */
  renderService() {
    const svc = this.svc;
    const hold = this.hold;
    const cancel = checkbox("cancel_running", false);
    const stages = (n) => `${n} running stage${n === 1 ? "" : "s"}`;
    const run = async (call, said) => {
      try {
        this.svc = await call();
      } catch {
        return;
      }
      this.deps.notify(said);
      this.renderService();
    };
    const runHold = async (call, said) => {
      try {
        this.hold = await call();
      } catch {
        return;
      }
      this.deps.notify(said);
      this.renderService();
    };
    if (this.svcTimer) clearInterval(this.svcTimer);
    this.svcTimer = null;
    if (svc?.state === "draining") {
      this.svcTimer = setInterval(() => {
        void this.deps.restartStatus({ quiet: true }).then((v) => {
          this.svc = v;
          this.renderService();
        }, () => void 0);
      }, 5e3);
    }
    if (hold?.active) {
      replace(
        this.svcHost,
        h(
          "div",
          { class: "warn-box", attrs: { "data-role": "service-hold" } },
          h("p", { text: `Service held: ${hold.reason ?? ""}` }),
          h("p", { class: "field-hint", text: hold.restart ? `A restart waits for the ${stages(hold.open_stages)} to end, and nothing new starts until it does.` : `${stages(hold.open_stages)} still finishing; no project starts anything new.` }),
          h(
            "div",
            { class: "row-actions" },
            button(hold.restart ? "Lift hold and cancel restart" : "Lift hold", () => void runHold(() => this.deps.liftHold(), "Hold lifted"), { field: "lift-hold", icon: "cancel", word: true })
          )
        )
      );
      return;
    }
    const reason = textInput("hold_reason", "", { placeholder: "why the service should start nothing" });
    const restartBox = svc?.state === "draining" ? h(
      "div",
      { class: "warn-box" },
      h("p", { text: `Restarting when the ${stages(svc.open_stages)} end. ${svc.plan?.cancel_running ? "Running stages were cancelled." : "Nothing new starts meanwhile."}` }),
      button("Cancel restart", () => void run(() => this.deps.cancelRestart(), "Restart cancelled"), { field: "cancel-restart", icon: "cancel", word: true })
    ) : h(
      "div",
      {},
      h("span", { class: "inline-check" }, cancel, "Cancel running stages instead of waiting for them"),
      h(
        "div",
        { class: "row-actions" },
        button("Restart service", () => void run(() => this.deps.restartService(cancel.checked), "Restart requested"), { field: "restart-service", icon: "refresh", word: true }),
        h("span", { class: "field-hint", text: svc ? `running since ${new Date(svc.started).toLocaleString()}` : "" })
      )
    );
    replace(
      this.svcHost,
      restartBox,
      h("p", { class: "field-hint", text: "A hold pauses every running project and lets nothing new start, for a change you make by hand." }),
      h(
        "div",
        { class: "inline-row" },
        reason,
        button("Hold service", () => void runHold(() => this.deps.holdService(reason.value.trim()), "Service held"), { field: "hold-service", icon: "pause", word: true })
      )
    );
  }
  renderRoots() {
    const edit = (next) => {
      this.roots = next;
      this.dirty = true;
      this.renderRoots();
      this.auto?.touch();
    };
    replace(
      this.rootsHost,
      ...this.roots.map((r, i) => h(
        "div",
        { class: "inline-row", attrs: { "data-root": i } },
        h("code", { text: r }),
        button("Remove", () => edit(this.roots.filter((_, j) => j !== i)), { field: "remove-root", icon: "remove" })
      )),
      button("Add folder", () => void this.addRoot(edit), { field: "add-root", icon: "add" })
    );
  }
  async addRoot(edit) {
    const chosen = await this.deps.pickDir(this.roots[this.roots.length - 1] ?? "");
    if (chosen !== null && !this.roots.includes(chosen)) edit([...this.roots, chosen]);
  }
  /** refreshVersion asks the server to probe opencode again (OC1). */
  async refreshVersion() {
    try {
      this.oc = await this.deps.opencodeVersion(true);
    } catch {
      return;
    }
    this.renderOpenCode();
  }
  /** saveNow saves a pending change now (what the pause would do). */
  saveNow() {
    return this.auto?.flush() ?? Promise.resolve();
  }
  body() {
    const body = {};
    for (const [key, input] of this.inputs) body[key] = input.value.trim();
    if (this.dispatch) body.botchat_mode = this.dispatch.checked ? "dispatch" : "mirror";
    if ("browse_roots" in this.values || this.roots.length > 0) body.browse_roots = this.roots;
    return body;
  }
  /** save sends every field; the server validates and names a bad field. It does not redraw: the form is being edited. */
  async save() {
    this.errors.clear();
    try {
      this.values = await this.deps.putSettings(this.body(), { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return false;
    }
    this.dirty = false;
    return true;
  }
  render() {
    this.inputs.clear();
    this.errors = new FormErrors();
    const limits = h("div", { class: "profile-fields" }, ...LIMIT_FIELDS.map((f) => this.row(f)));
    const exec = h("div", { class: "profile-fields" }, ...EXECUTION_FIELDS.map((f) => this.row(f)));
    const path2 = this.row({ key: "opencode_path", label: "opencode path", hint: "Program to run" });
    const bcUrl = this.row({ key: "botchat_url", label: "BotChat address", hint: "e.g. https://botchat.example.net; empty turns the relay off" });
    const dispatch2 = checkbox("botchat_dispatch", str(this.values.botchat_mode) === "dispatch");
    this.dispatch = dispatch2;
    dispatch2.addEventListener("change", () => {
      this.dirty = true;
      this.auto?.touch();
    });
    const bcMode = fieldRow("Send work through BotChat", dispatch2, "Off (the usual): agentflow runs each stage itself and posts to BotChat what happens. On: each stage is also handed to an agent through BotChat, with a direct run if BotChat does not answer; this adds model tokens and about 2 s per stage.");
    const bcKey = this.row({ key: "botchat_key_file", label: "BotChat key file", hint: "File holding the API key, e.g. ~/.botchat_apikey; read on every post" });
    this.renderOpenCode();
    this.renderService();
    this.renderRoots();
    this.auto?.stop();
    this.auto = autosave({ snapshot: () => JSON.stringify(this.body()), save: () => this.save() });
    const root = h(
      "div",
      { class: "settings-host settings-host-drawer" },
      section("Limits", limits),
      section("Execution", exec),
      section("opencode", path2, this.ocHost),
      section("BotChat", bcUrl, bcKey, bcMode.el),
      section("Service", this.svcHost),
      section("Browse limit", this.rootsHost),
      this.errors.general,
      h("div", { class: "settings-actions" }, this.auto.status)
    );
    replace(this.host, root);
    this.auto.watch(root);
  }
};

// web/agentflow/js/effort-settings.ts
var DELIBERATE_PATTERNS = [
  ["auth", /\b(oauth|auth(n|z|entication|enticate\w*|orization|orize\w*|orisation)?)\b/i],
  ["login", /\blog-?in\b/i],
  ["password", /\bpasswords?\b/i],
  ["credentials", /\bcredentials?\b/i],
  ["migration", /\bmigrat\w+/i],
  ["PII", /\bpii\b|\bpersonal(ly)?[ -](identifiable )?(data|information)\b/i],
  ["GDPR", /\bgdpr\b/i],
  ["encryption", /\bencrypt\w*/i],
  ["payment", /\bpayments?\b/i]
];
function deliberateTriggers(text) {
  return DELIBERATE_PATTERNS.filter(([, re]) => re.test(text)).map(([name]) => name);
}
function formatTokens(n) {
  if (n >= 1e6) return `${Number((n / 1e6).toFixed(1))}M`;
  if (n >= 1e3) return `${Number((n / 1e3).toFixed(1))}k`;
  return String(n);
}
function formatSeconds(s) {
  if (s >= 3600) return `${Number((s / 3600).toFixed(1))}h`;
  if (s >= 60) return `${Math.round(s / 60)}m`;
  return `${s}s`;
}
function budgetText(b) {
  const parts = [];
  if (b.token_limit > 0) parts.push(`tokens ${formatTokens(b.tokens)}/${formatTokens(b.token_limit)}`);
  if (b.seconds_limit > 0) parts.push(`time ${formatSeconds(b.seconds)}/${formatSeconds(b.seconds_limit)}`);
  return parts.join(" \xB7 ");
}
function budgetBar(b) {
  const pct2 = Math.min(100, Math.round(b.fraction * 100));
  const cls = b.state === "exhausted" ? " progress-over" : b.state === "no_new_tasks" ? " progress-warn" : "";
  const note = b.state === "exhausted" ? "budget used up: the run is stopped" : b.state === "no_new_tasks" ? "over 90%: no new task starts, running ones finish" : "";
  return h(
    "div",
    { class: "console-header", attrs: { "data-role": "budget" } },
    h("span", { class: "pane-title", text: "Budget" }),
    h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar" + cls, attrs: { style: `width: ${pct2}%` } })),
    h("span", { class: "progress-pct", text: `${pct2}% \u2014 ${budgetText(b)}` }),
    note ? badge(note, b.state === "exhausted" ? "warn" : "muted") : null
  );
}
function effortSettings(ef, frdText, deps, onSaved) {
  const errors = new FormErrors();
  const deliberate = checkbox("effort_deliberate", ef.deliberate);
  const tokens = textInput("budget_tokens", ef.budget_tokens ? String(ef.budget_tokens) : "", { type: "number", placeholder: "no limit" });
  const minutes = textInput("budget_minutes", ef.budget_seconds ? String(Number((ef.budget_seconds / 60).toFixed(2))) : "", { type: "number", placeholder: "no limit" });
  const triggers = ef.deliberate ? [] : deliberateTriggers(frdText);
  const suggestion = triggers.length === 0 ? null : h(
    "div",
    { class: "warn-box", attrs: { "data-role": "deliberate-suggestion" } },
    h("p", { text: `This FRD mentions ${triggers.join(", ")}. Deliberate planning adds a pre-mortem and a fuller test plan, which suits this kind of work.` }),
    button("Turn on deliberate planning", () => {
      deliberate.checked = true;
      void save().then((ok) => {
        if (ok) onSaved();
      });
    }, { field: "accept-suggestion", icon: "approve", word: true })
  );
  const body = () => ({
    deliberate: deliberate.checked,
    budget_tokens: Math.max(0, Math.round(Number(tokens.value) || 0)),
    budget_seconds: Math.max(0, Math.round((Number(minutes.value) || 0) * 60))
  });
  const save = async () => {
    errors.clear();
    try {
      await deps.patchEffort(ef.project, ef.id, body(), { quiet: true });
    } catch (err) {
      errors.show(err);
      return false;
    }
    return true;
  };
  const auto = autosave({ snapshot: () => JSON.stringify(body()), save });
  const el = section(
    `Settings for #${ef.id}`,
    suggestion,
    errors.add("deliberate", fieldRow(
      "Deliberate planning",
      deliberate,
      "The planner adds a pre-mortem (three failure scenarios) and an expanded test plan, and the critic checks for them"
    )).el,
    h(
      "div",
      { class: "profile-fields" },
      errors.add("budget_tokens", fieldRow("Token budget", tokens, "Runner tokens for the whole effort; blank is no limit")).el,
      errors.add("budget_seconds", fieldRow(
        "Time budget (minutes)",
        minutes,
        "Total stage run time, so stages running in parallel add up; blank is no limit. From 90% no new task starts; at 100% the run stops"
      )).el
    ),
    errors.general,
    h("div", { class: "settings-actions" }, auto.status)
  );
  auto.watch(el);
  return el;
}

// web/agentflow/js/backends.ts
function isPrivateHost(host) {
  const m = /^(?:[a-z][a-z0-9+.-]*:\/\/)?(?:[^@/]*@)?(\[[^\]]*\]|[^:/?#]*)/i.exec(host.trim());
  const name = (m?.[1] ?? "").toLowerCase();
  if (name === "localhost" || name === "[::1]") return true;
  const ip = /^(\d{1,3})\.(\d{1,3})\.\d{1,3}\.\d{1,3}$/.exec(name);
  if (!ip) return false;
  const [a, b] = [Number(ip[1]), Number(ip[2])];
  return a === 10 || a === 127 || a === 192 && b === 168 || a === 172 && b >= 16 && b <= 31;
}
var TIERS2 = [["0", "not rated"], ["1", "1 strong"], ["2", "2 capable"], ["3", "3 light"]];
var API_TYPES = [
  ["ollama", "ollama"],
  ["openai_compatible", "openai compatible"],
  ["anthropic", "anthropic"],
  ["opencode", "opencode built-in"],
  ["kilo", "Kilo CLI (free Kilo models)"],
  ["grok", "Grok CLI (your Grok plan)"],
  ["codex", "Codex CLI (your ChatGPT plan)"],
  ["claude", "Claude Code CLI (your Claude login)"],
  ["muse", "Muse Code CLI (your Muse login)"],
  ["agy", "Antigravity CLI (your Google login)"]
];
var cliTypes = /* @__PURE__ */ new Set(["opencode", "kilo", "grok", "codex", "claude", "muse", "agy"]);
var opencodeLike = /* @__PURE__ */ new Set(["opencode", "kilo"]);
var checkPollMs = 3e3;
var defaultBackendsDeps = () => ({
  backends: api.backends,
  createBackend: api.createBackend,
  probeBackend: api.probeBackend,
  patchBackend: api.patchBackend,
  retireBackend: api.retireBackend,
  enableBackend: api.enableBackend,
  deleteBackend: api.deleteBackend,
  rests: api.rests,
  wakeModel: api.wakeModel,
  holds: api.holds,
  setHold: api.setHold,
  checkModels: api.checkModels,
  checkStatus: api.checkStatus,
  checkReviewers: api.checkReviewers,
  evalStatus: api.evalStatus,
  extendWindows: api.extendWindows,
  candidates: api.candidates,
  putCandidates: api.putCandidates,
  projects: api.projects,
  limits: api.limits,
  putLimits: api.putLimits,
  settings: api.settings,
  notify: (m) => void showToast5(m, "success"),
  confirm: (m, label = "Confirm") => confirmDialog4(m, { confirmLabel: label })
});
function usedBy(cands, name) {
  const out = /* @__PURE__ */ new Set();
  for (const c of cands) {
    if (c.backend !== name) continue;
    out.add(c.scope === "project" ? `project ${c.project ?? "?"} ${c.role}` : `global ${c.role}`);
  }
  return [...out];
}
function lastForRoles(cands, name) {
  return ROLES.filter((role) => {
    const mine = cands.filter((c) => c.scope === "global" && c.role === role);
    return mine.length > 0 && mine.every((c) => c.backend === name);
  });
}
var BackendsView = class {
  constructor(host, deps, installDeps = defaultInstallDeps()) {
    this.host = host;
    this.deps = deps;
    this.installDeps = installDeps;
    this.backends = [];
    this.candidates = [];
    this.projects = [];
    this.globals = {};
    this.warnings = [];
    this.loaded = false;
    this.adding = false;
    this.editing = /* @__PURE__ */ new Set();
    this.retiring = /* @__PURE__ */ new Set();
    /** The last model check of each backend (H27), refreshed while one runs. */
    this.checks = /* @__PURE__ */ new Map();
    /** The models that are resting after a rate limit or a stall, keyed backend + NUL + model. */
    this.rests = /* @__PURE__ */ new Map();
    /** The models held: while one rests its work waits for it, keyed backend + NUL + model. */
    this.holds = /* @__PURE__ */ new Set();
    /** The last seeded-defect reviewer check of each backend (H32), refreshed while one runs. */
    this.evals = /* @__PURE__ */ new Map();
    /** role -> rows being edited; present only while the role has unsaved edits. */
    this.drafts = /* @__PURE__ */ new Map();
    this.limitsSlug = "";
    this.limitsHost = h("div");
    this.off = null;
    this.configPanel = null;
    /** One install panel per Ollama backend; each keeps its own list and event subscription. */
    this.panels = /* @__PURE__ */ new Map();
    // ─── Per-project limits ──────────────────────────────────────────────────
    this.limitsAuto = null;
  }
  panelFor(name) {
    let p = this.panels.get(name);
    if (!p) {
      p = new InstallPanel(name, this.installDeps);
      this.panels.set(name, p);
      p.mount();
    }
    return p;
  }
  mount() {
    this.off = subscribe((ev) => {
      if (["backend.update", "candidates.update", "state.changed", "settings.update"].includes(ev.type) && this.idle()) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.panels.forEach((p) => p.unmount());
      this.panels.clear();
    };
  }
  idle() {
    return !this.adding && this.editing.size === 0 && this.retiring.size === 0 && this.drafts.size === 0;
  }
  async load() {
    try {
      const [backends, candidates, projects, globals] = await Promise.all([
        this.deps.backends(),
        this.deps.candidates(),
        this.deps.projects(),
        this.deps.settings()
      ]);
      Object.assign(this, { backends, candidates, projects, globals, loaded: true });
      try {
        this.rests = new Map((await this.deps.rests()).map((r) => [r.backend + "\0" + r.model, r]));
      } catch {
      }
      try {
        this.holds = new Set((await this.deps.holds()).map((x) => x.backend + "\0" + x.model));
      } catch {
      }
      await Promise.all(backends.map(async (b) => {
        try {
          this.checks.set(b.name, await this.deps.checkStatus(b.name));
        } catch {
        }
        try {
          this.evals.set(b.name, await this.deps.evalStatus(b.name));
        } catch {
        }
      }));
    } catch {
      if (!this.loaded) return;
    }
    if (!this.projects.some((p) => p.slug === this.limitsSlug)) this.limitsSlug = this.projects[0]?.slug ?? "";
    this.render();
    void this.drawLimits();
  }
  // ─── Backend list ────────────────────────────────────────────────────────
  showResult(res, what) {
    this.warnings = res.warnings ?? [];
    this.deps.notify(what);
  }
  backendForm(initial, done) {
    const errors = new FormErrors();
    let auto = null;
    const bump = () => auto?.touch();
    const name = textInput("name", initial?.name ?? "", { readOnly: !!initial });
    const host = textInput("host", initial?.host ?? "", { placeholder: "http://host:port" });
    const apiType = selectInput("api_type", API_TYPES, initial?.api_type ?? "ollama");
    const cliPath = textInput("cli_path", initial?.cli_path ?? "", { placeholder: "blank: auto-detect" });
    const slots = textInput("slots", String(initial?.slots ?? 1), { type: "number" });
    const budget = textInput("memory_budget_gb", initial?.memory_budget_mb ? String(Math.round(initial.memory_budget_mb / 1024)) : "", { type: "number", placeholder: "not set" });
    const cap = textInput("window_cap_tokens", initial?.window_cap_tokens ? String(initial.window_cap_tokens) : "", { type: "number", placeholder: "no cap" });
    const hosted = checkbox("hosted", initial?.hosted ?? false);
    const paid = checkbox("paid", initial?.paid ?? false);
    const apiKey = textInput("api_key", "", { type: "password", placeholder: initial?.has_key ? "unchanged" : "API key" });
    apiKey.setAttribute("autocomplete", "new-password");
    let clearKey = false;
    const keyState = h("span", { class: "row-actions", attrs: { "data-role": "key-state" } });
    const drawKey = () => replace(
      keyState,
      initial?.has_key ? clearKey ? badge("key cleared on save", "warn") : badge("key set", "ok") : null,
      initial?.has_key ? button(clearKey ? "Keep key" : "Clear key", () => {
        clearKey = !clearKey;
        drawKey();
        bump();
      }, { field: "clear-key", icon: clearKey ? "cancel" : "remove", word: true }) : null
    );
    drawKey();
    const keyField = errors.add("api_key", fieldRow(
      "API key",
      h("div", { class: "row-actions" }, apiKey, keyState),
      "Sent to the server as its bearer credential, for a hosted service or a local one that asks for a key. Stored write-only; leave blank to keep the stored key"
    ));
    const hostField = errors.add("host", fieldRow("Host", host, "An http or https URL"));
    const cliField = errors.add("cli_path", fieldRow("CLI path", cliPath, "Program an opencode or Kilo backend runs; blank auto-detects (opencode for opencode, kilo for Kilo)"));
    const builtin = () => cliTypes.has(apiType.value);
    const canPickBinary = () => opencodeLike.has(apiType.value);
    const syncKey = () => {
      if (!builtin()) keyField.el.removeAttribute("hidden");
      else keyField.el.setAttribute("hidden", "");
      if (builtin()) hostField.el.setAttribute("hidden", "");
      else hostField.el.removeAttribute("hidden");
      if (canPickBinary()) cliField.el.removeAttribute("hidden");
      else cliField.el.setAttribute("hidden", "");
    };
    hosted.firstChild.addEventListener("change", syncKey);
    apiType.addEventListener("change", syncKey);
    syncKey();
    const models = [];
    let probed = [];
    let contexts = {};
    for (const m of initial?.models ?? []) if (m.context_tokens) contexts[m.model] = m.context_tokens;
    let tools = {};
    const modelControl = (m) => probed.length > 0 ? selectInput("model", probed.map((n) => [n, tools[n] === "no" ? `${n} (no tool support)` : n]), m) : textInput("model", m, { placeholder: "model name (or Probe host)" });
    const modelList = h("div", { class: "wizard-list" });
    const prefill = (_row) => {
    };
    const flag = (row) => {
      row.warn.hidden = tools[row.model.value] !== "no";
    };
    const draw = (row) => {
      row.model.addEventListener("change", () => {
        prefill(row);
        flag(row);
      });
      replace(
        row.el,
        row.model,
        row.warn,
        h("span", { class: "inline-check" }, row.paid, "paid"),
        h("span", { class: "inline-check", attrs: { title: "The context window the host serves this model with, in tokens. Blank keeps what agentflow knows (learned when a run is cut off, otherwise the context floor in Settings)." } }, "window", row.ctx),
        h("span", { class: "inline-check", attrs: { title: "How capable the model is. Light models are kept off planning roles; a reviewer is never weaker than the worker it checks." } }, "tier", row.tier),
        h("span", { class: "inline-check" }, "at once", row.max),
        button("Remove", () => {
          models.splice(models.indexOf(row), 1);
          row.el.parentNode?.removeChild(row.el);
          bump();
        }, { field: "remove-model", icon: "remove" })
      );
    };
    const addModel = (m = "", p = false, max = 0, ctx = 0, tier = 0) => {
      const row = {
        model: modelControl(m),
        paid: checkbox("model_paid", p),
        max: textInput("max_concurrent", max > 0 ? String(max) : "", { type: "number", placeholder: "no limit" }),
        ctx: textInput("window_tokens", ctx > 0 ? String(ctx) : "", { type: "number", placeholder: "?" }),
        tier: selectInput("tier", TIERS2, String(tier)),
        auto: "",
        warn: h("span", { class: "badge badge-warn", text: "no tool support: remove it", attrs: { "data-role": "no-tools", hidden: true, title: "The host says this model cannot make tool calls, which every agentflow role needs. It cannot be saved." } }),
        el: h("div", { class: "inline-row", attrs: { "data-role": "model-row" } })
      };
      models.push(row);
      draw(row);
      modelList.appendChild(row.el);
    };
    for (const m of initial?.models ?? []) addModel(m.model, m.paid, m.max_concurrent, m.window_tokens ?? 0, m.tier ?? 0);
    if (!initial) addModel();
    const probe = async () => {
      errors.clear();
      try {
        const res = await this.deps.probeBackend(builtin() ? "" : host.value.trim(), apiType.value, apiKey.value, initial?.name ?? "", canPickBinary() ? cliPath.value.trim() : "");
        probed = res.models;
        contexts = res.contexts ?? {};
        tools = res.tools ?? {};
        if (probed.length === 0) throw new Error("the host reported no models");
        for (const row of models) {
          const keep = row.model.value;
          row.model = modelControl(keep);
          draw(row);
          prefill(row);
          flag(row);
        }
        bump();
        this.deps.notify(`${probed.length} model(s) found${builtin() ? "" : ` on ${host.value.trim()}`}`);
      } catch (err) {
        errors.show(err);
      }
    };
    const modelsField = errors.add("models", fieldRow(
      "Models",
      modelList,
      'Probe lists what the host serves. A paid model is only used when listed for a role. "At once" caps how many stages run it together (blank = the slots limit)'
    ));
    modelsField.el.appendChild(button("Probe host for models", () => void probe(), { field: "probe-host", icon: "probe" }));
    modelsField.el.appendChild(button("Add model", () => addModel(), { field: "add-model", icon: "add" }));
    const readBody = () => {
      const body = {
        name: name.value.trim(),
        host: builtin() ? "" : host.value.trim(),
        api_type: apiType.value,
        hosted: hosted.checked,
        paid: paid.checked,
        slots: Number(slots.value),
        memory_budget_mb: Math.max(0, Math.round((Number(budget.value) || 0) * 1024)),
        window_cap_tokens: Math.max(0, Math.round(Number(cap.value) || 0)),
        models: models.map((m) => ({ model: m.model.value.trim(), paid: m.paid.checked, max_concurrent: Number(m.max.value) || 0, context_tokens: contexts[m.model.value.trim()] ?? 0, window_tokens: Math.max(0, Math.round(Number(m.ctx.value) || 0)), tier: Number(m.tier.value) || 0 })).filter((m) => m.model !== "")
      };
      if (canPickBinary()) body.cli_path = cliPath.value.trim();
      if (!builtin() && apiKey.value !== "") body.api_key = apiKey.value;
      else if (clearKey) body.api_key = "";
      return body;
    };
    const save = async () => {
      errors.clear();
      const body = readBody();
      try {
        const res = initial ? await this.deps.patchBackend(initial.name, body, { quiet: true }) : await this.deps.createBackend(body, { quiet: true });
        this.showResult(res, initial ? `Saved ${body.name}` : `Added ${body.name}`);
      } catch (err) {
        errors.show(err);
        return false;
      }
      if (!initial) done();
      return true;
    };
    auto = initial ? autosave({ snapshot: () => JSON.stringify(readBody()), save }) : null;
    const card = h(
      "div",
      { class: "profile-card", attrs: { "data-form": initial ? "edit" : "add" } },
      h(
        "div",
        { class: "profile-fields" },
        errors.add("name", fieldRow("Name", name, "Letters, digits and dashes; fixed once added")).el,
        hostField.el,
        errors.add("api_type", fieldRow("API type", apiType)).el,
        cliField.el,
        errors.add("slots", fieldRow("Slots", slots, "Stages this backend runs at once")).el,
        errors.add("memory_budget_mb", fieldRow("Memory budget (GB)", budget, "What one loaded model may use on the host, weights and context together. Needed to extend windows")).el,
        errors.add("window_cap_tokens", fieldRow("Window cap (tokens)", cap, "Never extend a window past this, for a host where a long context is too slow")).el,
        errors.add("hosted", fieldRow("Hosted", hosted, "A remote service rather than your own machine")).el,
        errors.add("paid", fieldRow("Paid", paid, "Every model on it costs money")).el,
        keyField.el
      ),
      modelsField.el,
      errors.general,
      h(
        "div",
        { class: "settings-actions" },
        auto?.status ?? null,
        initial ? null : button("Cancel", done, { field: "cancel", icon: "cancel" }),
        initial ? button("Done", () => void auto?.flush().then(done), { primary: true, field: "close", icon: "approve" }) : button("Add backend", () => void save(), { primary: true, field: "save", icon: "approve" })
      )
    );
    if (auto) auto.watch(card);
    return card;
  }
  retirePanel(b) {
    const cancel = checkbox("cancel_running", true);
    const last = lastForRoles(this.candidates, b.name);
    const onlyActive = this.backends.filter((x) => !x.retired && x.name !== b.name).length === 0;
    const notes = [
      ...onlyActive ? ["This is the only active backend: while it is disabled nothing can be dispatched until you add one or enable one."] : [],
      ...last.length ? [`It is the only backend listed for: ${last.join(", ")}. Those roles will wait with no eligible backend.`] : []
    ];
    const run = async () => {
      let res;
      try {
        res = await this.deps.retireBackend(b.name, cancel.checked);
      } catch {
        return;
      }
      this.retiring.delete(b.name);
      this.showResult(res, `${b.name} is disabled`);
      await this.load();
    };
    return h(
      "div",
      { class: "warn-box", attrs: { "data-panel": "retire" } },
      ...notes.map((n) => h("p", { text: n })),
      h("span", { class: "inline-check" }, cancel, "Stop what is running on this backend now"),
      h(
        "div",
        { class: "row-actions" },
        button("Disable", () => void run(), { primary: true, field: "confirm-retire", icon: "pause", word: true }),
        button("Keep enabled", () => {
          this.retiring.delete(b.name);
          this.render();
        }, { field: "cancel-retire", icon: "cancel", word: true })
      )
    );
  }
  card(b) {
    if (this.editing.has(b.name)) {
      return this.backendForm(b, () => {
        this.editing.delete(b.name);
        void this.load();
      });
    }
    const checked = new Map((this.checks.get(b.name)?.results ?? []).map((c) => [c.Model, c]));
    const models = b.models.map((m) => h(
      "div",
      { class: "model-row" },
      h("strong", { class: "model-name", text: m.model }),
      m.paid ? badge("paid") : null,
      this.checkBadge(checked.get(m.model), this.checks.get(b.name)?.current === m.model),
      ...this.evalBadges(b.name, m.model),
      m.max_concurrent > 0 ? badge(`max ${m.max_concurrent} at once`, "muted") : null,
      m.window_tokens ? badge(`window ${formatTokens(m.window_tokens)}${m.window_source === "observed" ? " (learned)" : ""}`, m.window_source === "observed" ? "warn" : "muted") : null,
      m.context_tokens ? badge(`max ${formatTokens(m.context_tokens)}`, "muted") : null,
      m.tier ? badge(`tier ${m.tier}`, m.tier === 3 ? "warn" : "muted") : null,
      m.too_big ? badge("too big for host", "warn") : null,
      ...this.restParts(b.name, m.model),
      ...this.holdParts(b, m.model),
      m.tools === "no" ? badge("no tools", "warn") : null
    ));
    return h(
      "div",
      { class: "profile-card", attrs: { "data-backend": b.name } },
      h(
        "div",
        { class: "profile-header" },
        h(
          "span",
          { class: "row-actions" },
          h("strong", { text: b.name }),
          b.paid ? badge("paid") : null,
          b.hosted ? badge("hosted", "muted") : null,
          b.has_key ? badge("key set", "ok") : null,
          b.hosted && !cliTypes.has(b.api_type) && isPrivateHost(b.host) ? h("span", { class: "badge badge-warn", text: "\u26A0 hosted?", attrs: { title: "Flagged hosted, but the host is a private or local address: it may be mis-flagged. Edit the backend and untick Hosted if it runs on your own machine or network.", "data-role": "hosted-warning" } }) : null,
          b.retired ? badge("disabled", "warn") : null,
          b.suspended_since ? badge("suspended: not answering", "warn") : b.down_until ? badge("unreachable", "warn") : null
        ),
        h(
          "div",
          { class: "profile-actions" },
          b.api_type === "ollama" && !b.retired && (b.memory_budget_mb ?? 0) > 0 ? button("Extend windows", () => void this.extend(b.name), { field: "extend-windows", icon: "approve", word: true }) : null,
          b.retired ? null : button(this.checks.get(b.name)?.state === "running" ? "Checking\u2026" : "Check models", () => void this.check(b.name), { field: "check-models", icon: "approve", word: true }),
          b.retired ? null : button(this.evals.get(b.name)?.state === "running" ? `Checking reviewers\u2026 ${this.evals.get(b.name)?.done}/${this.evals.get(b.name)?.total}` : "Check reviewers", () => void this.checkReviewers(b.name), { field: "check-reviewers", icon: "approve", word: true }),
          button("Edit", () => {
            this.editing.add(b.name);
            this.render();
          }, { field: "edit", icon: "edit" }),
          b.retired ? button("Enable", () => void this.enable(b.name), { field: "enable", icon: "approve", word: true }) : button("Disable", () => {
            this.retiring.add(b.name);
            this.render();
          }, { field: "retire", icon: "pause", word: true }),
          button("Delete", () => void this.remove(b), { field: "delete", icon: "trash", unavailable: this.whyNotDelete(b) || void 0 })
        )
      ),
      h(
        "div",
        { class: "profile-fields" },
        cliTypes.has(b.api_type) ? null : h("div", {}, h("label", { text: "Host" }), h("span", { text: b.host })),
        h("div", {}, h("label", { text: "API type" }), h("span", { text: b.api_type })),
        h("div", {}, h("label", { text: "Slots" }), h("span", { text: String(b.slots) })),
        b.retired ? null : h("div", {}, h("label", { text: "Pace" }), this.paceSelect(b)),
        b.memory_budget_mb ? h("div", {}, h("label", { text: "Memory budget" }), h("span", { text: `${Math.round(b.memory_budget_mb / 1024)} GB${b.window_cap_tokens ? `, windows capped at ${formatTokens(b.window_cap_tokens)}` : ""}` })) : null,
        h("div", { class: "profile-wide" }, h("label", { text: "Models" }), h("div", { class: "model-list" }, ...models))
      ),
      b.api_type === "ollama" && !b.retired ? this.panelFor(b.name).el : null,
      this.retiring.has(b.name) ? this.retirePanel(b) : null
    );
  }
  /** Pace is the duty cap (H20): how much of the time the backend may be kept busy. It applies at once, with no restart. */
  paceSelect(b) {
    const now = b.duty_percent && b.duty_percent < 100 ? b.duty_percent : 100;
    const opts = [["100", "Full speed"], ["75", "Busy 75% of the time"], ["50", "Busy 50% of the time"], ["25", "Busy 25% of the time"]];
    if (!opts.some(([v]) => v === String(now))) opts.push([String(now), `Busy ${now}% of the time`]);
    const sel = selectInput("duty_percent", opts, String(now));
    sel.addEventListener("change", () => {
      void (async () => {
        try {
          await this.deps.patchBackend(b.name, { duty_percent: Number(sel.value) }, { quiet: true });
          this.deps.notify(Number(sel.value) >= 100 ? `${b.name} runs at full speed` : `${b.name} is held to ${sel.value}% busy`);
        } catch {
        }
        await this.load();
      })();
    });
    return sel;
  }
  /** One model's last check: answered, or why not. A failed model is passed over until it passes again. */
  checkBadge(c, now) {
    if (now) return badge("checking\u2026", "muted");
    if (!c) return null;
    const label = { failing: "not answering", rate_limited: "out of quota", unreachable: "unreachable", timeout: "too slow", empty: "empty reply" };
    const span = c.OK ? badge(`answers (${(c.Millis / 1e3).toFixed(1)}s)`, "ok") : badge(label[c.Class] ?? c.Class, "warn");
    span.setAttribute("title", `${c.OK ? "Passed" : c.Message || "Failed"} \u2014 checked ${new Date(c.Checked).toLocaleString()}`);
    span.setAttribute("data-role", c.OK ? "check-ok" : "check-failed");
    return span;
  }
  /**
   * One model's score on the seeded-defect cases (H32), one badge per layer:
   * how many planted defects it caught, and how many fixed versions it
   * wrongly flagged. A reviewer that misses defects, or flags clean code, is
   * not one to trust with a gate.
   */
  evalBadges(backend, mdl) {
    const ev = this.evals.get(backend);
    if (!ev) return [];
    const rows = ev.rows.filter((r) => r.model === mdl);
    return ev.summary.filter((s) => s.model === mdl).map((s) => {
      const bad = s.caught < s.defects || s.false_alarms > 0 || s.unread > 0;
      const label = `${s.layer === "task" ? "task review" : "acceptance"} ${s.caught}/${s.defects}${s.false_alarms ? `, ${s.false_alarms} false alarm${s.false_alarms === 1 ? "" : "s"}` : ""}${s.unread ? `, ${s.unread} unread` : ""}`;
      const el = badge(label, bad ? "warn" : "ok");
      el.setAttribute("title", rows.filter((r) => r.layer === s.layer).map((r) => `${r.case} (${r.variant}): ${r.outcome}${r.detail ? ` \u2014 ${r.detail}` : ""}`).join("\n"));
      el.setAttribute("data-role", "eval-score");
      return el;
    });
  }
  /** Run the seeded-defect cases through every model of a backend (H32) and follow the run until it ends. */
  async checkReviewers(name) {
    try {
      this.evals.set(name, await this.deps.checkReviewers(name, { layers: ["task", "acceptance"] }));
    } catch {
      return;
    }
    this.render();
    while (this.evals.get(name)?.state === "running") {
      await new Promise((r) => setTimeout(r, checkPollMs));
      try {
        this.evals.set(name, await this.deps.evalStatus(name));
      } catch {
        break;
      }
      this.render();
    }
    const weak = (this.evals.get(name)?.summary ?? []).filter((s) => s.caught < s.defects || s.false_alarms > 0);
    this.deps.notify(weak.length ? `${weak.length} model score${weak.length === 1 ? "" : "s"} on ${name} missed a defect or raised a false alarm` : `Every model on ${name} caught every planted defect`);
  }
  /** Check every model of a backend, one after the other (H27), and follow the run until it ends. */
  async check(name) {
    try {
      this.checks.set(name, await this.deps.checkModels(name));
    } catch {
      return;
    }
    this.render();
    while (this.checks.get(name)?.state === "running") {
      await new Promise((r) => setTimeout(r, checkPollMs));
      try {
        this.checks.set(name, await this.deps.checkStatus(name));
      } catch {
        break;
      }
      this.render();
    }
    const bad = (this.checks.get(name)?.results ?? []).filter((c) => !c.OK);
    this.deps.notify(bad.length ? `${bad.length} model${bad.length === 1 ? "" : "s"} on ${name} did not answer: ${bad.map((c) => c.Model).join(", ")}` : `Every model on ${name} answered`);
  }
  /** Raise each model's served window as far as the backend's memory budget allows (H15d). */
  async extend(name) {
    try {
      const res = await this.deps.extendWindows(name);
      const done = res.filter((r) => r.applied).length;
      this.deps.notify(done > 0 ? `Extended ${done} of ${res.length} model windows on ${name}` : `No window changed on ${name}: ${res.map((r) => `${r.model}: ${r.note}`).join("; ")}`);
    } catch {
      return;
    }
    await this.load();
  }
  /** Why a backend cannot be deleted yet, or '' when it can. The server enforces the same two rules. */
  whyNotDelete(b) {
    if (!b.retired) return "Disable this backend first; only a disabled backend can be deleted.";
    const lists = usedBy(this.candidates, b.name);
    return lists.length > 0 ? `Still on these candidate lists: ${lists.join(", ")}. Remove it from them to delete it.` : "";
  }
  /** restParts shows a resting model with the time it comes back and a way to wake it now. */
  restParts(backend, model) {
    const r = this.rests.get(backend + "\0" + model);
    if (!r || r.until <= Date.now()) return [];
    const at = new Date(r.until).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    return [
      badge(`resting until ${at} (${r.reason}${r.streak > 1 ? `, ${r.streak} in a row` : ""})`, "warn"),
      button("Wake", () => void this.wake(backend, model), { field: "wake-model", word: true })
    ];
  }
  /** holdParts is the Hold toggle of a hosted model: held, its work waits while it rests instead of moving on. */
  holdParts(b, model) {
    if (!b.hosted) return [];
    const held = this.holds.has(b.name + "\0" + model);
    return [
      held ? badge("held: work waits for it when it rests", "muted") : null,
      button(held ? "Release" : "Hold", () => void this.hold(b.name, model, !held), { field: "hold-model", word: true })
    ];
  }
  async hold(backend, model, on) {
    try {
      await this.deps.setHold(backend, model, on);
      this.deps.notify(on ? `${model} is held: its work waits for it when it rests` : `${model} is released`);
    } catch {
      return;
    }
    await this.load();
  }
  async wake(backend, model) {
    try {
      await this.deps.wakeModel(backend, model);
      this.deps.notify(`${model} is back in use`);
    } catch {
      return;
    }
    await this.load();
  }
  async remove(b) {
    const sure = await this.deps.confirm(
      `Delete backend ${b.name} and its ${b.models.length} model${b.models.length === 1 ? "" : "s"}? Past stages keep the name, so history stays. This cannot be undone.`,
      "Delete"
    );
    if (!sure) return;
    try {
      await this.deps.deleteBackend(b.name);
      this.deps.notify(`${b.name} is deleted`);
    } catch {
      return;
    }
    await this.load();
  }
  async enable(name) {
    try {
      this.showResult(await this.deps.enableBackend(name), `${name} is enabled`);
    } catch {
      return;
    }
    await this.load();
  }
  backendsSection() {
    const addForm = this.adding ? this.backendForm(null, () => {
      this.adding = false;
      void this.load();
    }) : null;
    const addBtn = button("Add backend", () => {
      this.adding = true;
      this.render();
    }, { primary: true, field: "add-backend", icon: "add" });
    if (this.backends.length === 0) {
      return h(
        "div",
        { class: "plan-empty", attrs: { "data-role": "no-backends" } },
        h("h3", { text: "No backends configured" }),
        h("p", { text: "Nothing is dispatched until you add a backend that serves at least one model." }),
        this.adding ? addForm : addBtn
      );
    }
    return section(
      "Backends",
      h("div", { class: "section-header" }, h("span", { class: "field-hint", text: `${this.backends.length} configured` }), this.adding ? null : addBtn),
      addForm,
      h("div", { class: "profile-list profile-grid" }, ...this.backends.map((b) => this.card(b)))
    );
  }
  // ─── Role candidates ─────────────────────────────────────────────────────
  serverRows(role) {
    return this.candidates.filter((c) => c.scope === "global" && c.role === role).sort((a, b) => a.position - b.position).map((c) => ({ backend: c.backend, model: c.model, optIn: true }));
  }
  roleEditor(role) {
    return buildRoleEditor({
      role,
      backends: this.backends,
      rows: () => this.drafts.get(role) ?? this.serverRows(role),
      hasDraft: () => this.drafts.has(role),
      setDraft: (rows) => {
        if (rows) this.drafts.set(role, rows);
        else this.drafts.delete(role);
      },
      save: async (body) => {
        this.candidates = await this.deps.putCandidates(role, body, { quiet: true });
        this.deps.notify(`Saved ${role} candidates`);
      },
      emptyNote: "No candidates: this role has no eligible backend and its stages wait."
    });
  }
  candidatesSection() {
    return section(
      "Role candidates",
      h("p", { class: "field-hint", text: "Tried in order; a paid model is used only when you list it here and opt in." }),
      h("div", { class: "profile-list" }, ...ROLES.map((r) => this.roleEditor(r)))
    );
  }
  async drawLimits() {
    void this.limitsAuto?.flush();
    const slug = this.limitsSlug;
    if (!slug) {
      replace(this.limitsHost, h("p", { class: "field-hint", text: "No projects yet." }));
      return;
    }
    let overrides;
    try {
      overrides = (await this.deps.limits(slug)).limits ?? {};
    } catch {
      return;
    }
    const errors = new FormErrors();
    const inputs = /* @__PURE__ */ new Map();
    const rows = LIMIT_FIELDS.map((f) => {
      const input = textInput(f.key, overrides[f.key] ?? "", { placeholder: `inherit (${String(this.globals[f.key] ?? "")})` });
      inputs.set(f.key, input);
      return errors.add(f.key, fieldRow(f.label, input, "Blank uses the global value")).el;
    });
    const read = () => {
      const body = {};
      for (const [k, input] of inputs) body[k] = input.value.trim();
      return body;
    };
    const save = async () => {
      errors.clear();
      try {
        await this.deps.putLimits(slug, read(), { quiet: true });
      } catch (err) {
        errors.show(err);
        return false;
      }
      return true;
    };
    const auto = autosave({ snapshot: () => JSON.stringify(read()), save });
    this.limitsAuto = auto;
    const box = h("div", { class: "profile-fields", attrs: { "data-role": "project-limits" } }, ...rows);
    replace(this.limitsHost, box, errors.general, h("div", { class: "settings-actions" }, auto.status));
    auto.watch(box);
  }
  limitsSection() {
    const pick = selectInput("project", this.projects.map((p) => [p.slug, p.slug]), this.limitsSlug);
    pick.addEventListener("change", () => {
      this.limitsSlug = pick.value;
      void this.drawLimits();
    });
    return section("Project limits", fieldRow("Project", pick).el, this.limitsHost);
  }
  // ─── Whole view ──────────────────────────────────────────────────────────
  /** The configuration file section: export, and a previewed import (H13). The panel keeps its state across renders. */
  configSection() {
    if (!this.configPanel) {
      this.configPanel = new ConfigPanel(defaultConfigDeps(), () => void this.load());
      this.configPanel.render();
    }
    return section("Configuration file", this.configPanel.el);
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading backends\u2026" }));
      return;
    }
    const warn = this.warnings.length === 0 ? null : h(
      "div",
      { class: "warn-box", attrs: { "data-role": "warnings" } },
      ...this.warnings.map((w) => h("p", { text: w })),
      button("Dismiss", () => {
        this.warnings = [];
        this.render();
      }, { field: "dismiss", icon: "remove" })
    );
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host" },
        warn,
        this.backendsSection(),
        this.backends.length > 0 ? this.candidatesSection() : null,
        this.projects.length > 0 || this.backends.length > 0 ? this.limitsSection() : null,
        this.configSection()
      )
    );
  }
};
function mountBackends(host) {
  return new BackendsView(host, defaultBackendsDeps()).mount();
}

// web/agentflow/js/activity.ts
function describe2(ef, draft, tasks) {
  const n = `#${ef.id}`;
  switch (ef.status) {
    case "draft":
      if (draft?.state === "running") {
        const what = draft.purpose === "split" ? "Splitting the FRD into parts" : draft.purpose === "polish" ? "Tidying the FRD" : "Drafting the FRD";
        return { kind: "busy", text: `${what}\u2026`, tab: "frd", since: draft.started ?? 0 };
      }
      if (ef.frd?.latest) return { kind: "wait", text: `${n}: review the FRD and approve it`, tab: "frd", since: 0 };
      return { kind: "wait", text: `${n}: describe what to build`, tab: "frd", since: 0 };
    case "planning":
      return { kind: "busy", text: `${n}: planning\u2026`, tab: "plan", since: ef.updated };
    case "plan_ready":
      return { kind: "wait", text: `${n}: review the plan and approve it`, tab: "plan", since: 0 };
    case "plan_approved":
    case "compiling":
      return { kind: "busy", text: `${n}: turning the plan into tasks\u2026`, tab: "queue", since: ef.updated };
    case "queue_draft":
      return { kind: "wait", text: `${n}: review the queue and approve it`, tab: "queue", since: 0 };
    case "queue_approved":
      return { kind: "wait", text: `${n}: queue approved; start the run`, tab: "board", since: 0 };
    case "running": {
      const halted = tasks.find((t) => t.halted);
      if (halted) return { kind: "wait", text: `${n}: ${halted.key} needs attention`, tab: "board", since: 0 };
      const cur = tasks.map((t) => t.current).find((c) => c);
      const done = tasks.filter((t) => t.status === "passed").length;
      const progress = tasks.length ? ` (${done}/${tasks.length} done)` : "";
      return {
        kind: "busy",
        text: cur ? `${n}: ${cur.task} ${cur.role} on ${cur.model || cur.backend}${progress}` : `${n}: running${progress}`,
        tab: "board",
        since: 0
      };
    }
    case "paused":
      return { kind: "rest", text: `${n}: paused`, tab: "board", since: 0 };
    case "complete":
      return { kind: "rest", text: `${n}: complete; ready to merge`, tab: "board", since: 0 };
    case "abandoned":
      return { kind: "rest", text: `${n}: abandoned`, tab: "history", since: 0 };
    default:
      return { kind: "rest", text: `${n}: ${ef.status.replace(/_/g, " ")}`, tab: "frd", since: 0 };
  }
}
function clock(ms) {
  const s = Math.max(0, Math.floor(ms / 1e3));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`;
}
var Activity = class {
  constructor(el, open, deps = { efforts: api.efforts, draftState: (s, i) => api.draftState(s, i, { quiet: true }), tasks: api.tasks }) {
    this.el = el;
    this.open = open;
    this.deps = deps;
    this.slug = "";
    this.line = null;
    this.ticker = null;
    this.clockEl = null;
    this.off = null;
    this.stopPoll = null;
    this.disposed = false;
    this.seq = 0;
  }
  mount(slug) {
    this.slug = slug;
    this.off = subscribe((ev) => {
      if (["state.changed", "effort.update", "project.update", "draft.update"].includes(ev.type)) void this.refresh();
    });
    this.stopPoll = startPoll(() => this.line?.kind === "busy", () => this.refresh());
    void this.refresh();
    return () => {
      this.disposed = true;
      this.stopTicker();
      this.off?.();
      this.stopPoll?.();
    };
  }
  /** setProject points the strip at another project. */
  setProject(slug) {
    this.slug = slug;
    this.line = null;
    this.render();
    void this.refresh();
  }
  async refresh() {
    if (this.disposed) return;
    const slug = this.slug;
    const seq = ++this.seq;
    let line2 = null;
    try {
      const ef = slug ? chooseEffort(slug, await this.deps.efforts(slug)) : void 0;
      if (ef) {
        const draft = ef.status === "draft" ? await this.deps.draftState(slug, ef.id).catch(() => null) : null;
        const tasks = ef.status === "running" ? await this.deps.tasks(slug, ef.id).catch(() => []) : [];
        line2 = describe2(ef, draft, tasks);
      }
    } catch {
      return;
    }
    if (this.disposed || seq !== this.seq || slug !== this.slug) return;
    this.line = line2;
    this.render();
  }
  stopTicker() {
    if (this.ticker) clearInterval(this.ticker);
    this.ticker = null;
    this.clockEl = null;
  }
  render() {
    this.stopTicker();
    const l = this.line;
    if (!l) {
      replace(this.el);
      this.el.setAttribute("hidden", "");
      return;
    }
    this.el.removeAttribute("hidden");
    this.el.setAttribute("data-kind", l.kind);
    const clockEl = l.since > 0 ? h("span", { class: "field-hint", attrs: { "data-role": "elapsed" }, text: clock(Date.now() - l.since) }) : null;
    this.clockEl = clockEl;
    if (clockEl && l.since > 0) {
      const since = l.since;
      this.ticker = setInterval(() => {
        if (this.clockEl) this.clockEl.textContent = clock(Date.now() - since);
      }, 1e3);
    }
    replace(
      this.el,
      h("span", { class: "activity-dot", attrs: { "aria-hidden": "true" } }),
      h("span", { class: "activity-text", text: l.text, attrs: { "data-role": "activity-text" } }),
      clockEl,
      h("button", {
        class: "ui-modal-btn af-has-icon af-icon-only",
        attrs: { type: "button", title: "Open it", "aria-label": "Open it", "data-action": "open-activity" },
        on: { click: () => this.open(l.tab) }
      }, icon("next"))
    );
  }
};

// web/agentflow/js/board.ts
import { confirmDialog as confirmDialog5, copyText as copyText3, showToast as showToast6 } from "/shared/dist/shared.mjs";

// web/agentflow/js/stream.ts
var defaultStreamDeps = () => ({ open: (url) => new EventSource(url) });
var MAX_LINES = 2e3;
var StreamPanel = class {
  constructor(panel, deps) {
    this.panel = panel;
    this.deps = deps;
    this.source = null;
    this.out = h("div", { class: "stream-output", attrs: { "data-role": "stream-output" } });
    this.status = h("span", { class: "stream-status", attrs: { "data-role": "stream-status" } });
    this.lines = 0;
    /** The stage being shown; 0 when closed. */
    this.stageId = 0;
  }
  /** open shows a stage: live output for a running one, the stored output otherwise. */
  open(st) {
    this.closeSource();
    this.stageId = st.id;
    this.lines = 0;
    replace(this.out);
    replace(
      this.panel,
      h(
        "div",
        { class: "stream-header" },
        h("strong", { text: `${st.role} \xB7 ${st.backend}/${st.model}` }),
        this.status,
        button("Close", () => this.close(), { field: "close-stream", icon: "cancel" })
      ),
      this.out
    );
    this.panel.removeAttribute("hidden");
    if (st.state === "running") {
      this.setStatus("running", "running");
      const es = this.deps.open(stageStreamURL(st.id));
      this.source = es;
      es.addEventListener("output", (ev) => this.onOutput(ev.data ?? ""));
      es.addEventListener("status", (ev) => this.setStatus(ev.data ?? "", ev.data === "completed" ? "completed" : "running"));
      es.addEventListener("done", () => {
        this.setStatus("finished", "completed");
        this.closeSource();
      });
      es.addEventListener("error", () => {
        this.setStatus("stream ended", "");
        this.closeSource();
      });
    } else {
      this.setStatus(st.outcome || st.state, st.outcome === "pass" ? "completed" : "");
      this.append(st.raw_output || "(no output recorded)");
    }
  }
  close() {
    this.closeSource();
    this.stageId = 0;
    this.panel.setAttribute("hidden", "");
    replace(this.panel);
  }
  closeSource() {
    this.source?.close();
    this.source = null;
  }
  setStatus(text, cls) {
    this.status.textContent = text;
    this.status.className = "stream-status" + (cls ? " " + cls : "");
  }
  onOutput(data) {
    let line2 = data;
    try {
      const parsed = JSON.parse(data);
      if (typeof parsed.line === "string") line2 = parsed.line;
    } catch {
    }
    this.append(line2);
  }
  append(text) {
    this.out.appendChild(document.createTextNode(text + "\n"));
    this.lines++;
    while (this.lines > MAX_LINES && this.out.childNodes.length > 0) {
      this.out.removeChild(this.out.childNodes[0]);
      this.lines--;
    }
    this.out.scrollTop = this.out.scrollHeight;
  }
};

// web/agentflow/js/board.ts
var defaultBoardDeps = () => ({
  project: api.project,
  efforts: api.efforts,
  tasks: api.tasks,
  task: api.task,
  run: api.run,
  retryTask: api.retryTask,
  applyResolution: api.applyResolution,
  editTask: api.editTask,
  overrideTask: api.overrideTask,
  abandonEffort: api.abandonEffort,
  gitCommands: api.gitCommands,
  confirm: (m, label = "Confirm") => confirmDialog5(m, { confirmLabel: label }),
  copy: (t) => void copyText3(t),
  notify: (m) => void showToast6(m, "success")
});
var rec = (v) => v && typeof v === "object" ? v : {};
var strs = (v) => Array.isArray(v) ? v.map((x) => String(x)) : [];
function counts(tasks) {
  const out = { total: tasks.length };
  for (const t of tasks) out[t.status] = (out[t.status] ?? 0) + 1;
  return out;
}
var stageOf = (a, role) => a.stages.filter((s) => s.role === role).pop();
function block(title, ...children) {
  return h("div", { class: "detail-section" }, h("h4", { text: title }), ...children);
}
var mono = (text) => h("pre", { class: "detail-mono", text });
var bullets = (items) => h("ul", { class: "plan-review-notes" }, ...items.map((i) => h("li", { text: i })));
function renderVerify(parsed) {
  const r = rec(parsed);
  const checks = Array.isArray(r.checks) ? r.checks : [];
  return h(
    "div",
    { attrs: { "data-role": "verify-report" } },
    h("div", { class: "row-actions" }, chip(r.pass === false ? "failed" : "passed"), h("span", { class: "field-hint", text: `${checks.length} check${checks.length === 1 ? "" : "s"}` })),
    checks.length ? h("ul", { class: "plan-review-notes" }, ...checks.map((c) => h("li", {}, h("strong", { text: String(c.name ?? "") }), ` ${c.pass ? "passed" : "failed"}`, c.detail ? ` \u2014 ${String(c.detail)}` : null))) : null,
    strs(r.new_failures).length ? block("New failures", bullets(strs(r.new_failures))) : null,
    strs(r.baseline_failures).length ? block("Baseline failures", bullets(strs(r.baseline_failures))) : null,
    strs(r.warnings).length ? block("Warnings", bullets(strs(r.warnings))) : null
  );
}
function renderReviewOut(parsed) {
  const r = rec(parsed);
  const issues = Array.isArray(r.issues) ? r.issues : [];
  return h(
    "div",
    { attrs: { "data-role": "reviewer-report" } },
    h("div", { class: "row-actions" }, r.verdict ? chip(String(r.verdict)) : null),
    r.summary ? h("p", { text: String(r.summary) }) : null,
    issues.length ? h("ul", { class: "plan-review-notes" }, ...issues.map((i) => h("li", {}, h("strong", { text: String(i.severity ?? "") }), ` ${String(i.description ?? "")}`, i.file ? ` (${String(i.file)}${i.line ? ":" + String(i.line) : ""})` : null))) : null,
    r.repair_prompt ? block("Repair prompt", mono(String(r.repair_prompt))) : null,
    r.blocked ? h("p", { text: `Blocked: ${String(r.blocked)}` }) : null
  );
}
var Board = class {
  constructor(host, panel, slug, deps, streamDeps, yolo = defaultYoloDeps()) {
    this.host = host;
    this.panel = panel;
    this.slug = slug;
    this.deps = deps;
    this.yolo = yolo;
    this.efforts = [];
    this.tasks = [];
    this.selected = "";
    this.detail = null;
    /** runState is the LIVE state (project.run_state); the effort's persisted run_state is only a hint. */
    this.runState = "stopped";
    /** stopReason is why the system stopped the run ("budget"), else empty. */
    this.stopReason = "";
    this.git = null;
    this.editing = false;
    this.detailEl = null;
    this.reasonEl = null;
    this.cleanEl = null;
    this.fresh = true;
    // rebuild the detail pane on the next render
    this.stateSeq = 0;
    // bumped by run-state events so a slower fetch cannot overwrite them
    this.gitFailed = null;
    // effort id whose git commands failed
    this.loaded = false;
    this.off = null;
    this.stopPoll = null;
    this.errors = new FormErrors();
    this.stream = new StreamPanel(panel, streamDeps);
  }
  mount() {
    this.off = subscribe((ev) => this.handle(ev));
    this.stopPoll = startPoll(() => isTransient(this.current?.status), () => this.refresh());
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
      this.stream.close();
    };
  }
  /** handle applies one stream event: a run-state change shows at once, the rest refetch. */
  handle(ev) {
    if (ev.type === "state.changed") {
      void this.refresh();
    } else if (ev.type === "project.run_state" && ev.project === this.slug) {
      this.stateSeq++;
      this.runState = ev.state;
      if (ev.state !== "stopped") this.stopReason = "";
      this.render();
      void this.refresh();
    } else if ((ev.type === "effort.update" || ev.type === "task.update" || ev.type === "project.update") && ev.project === this.slug) {
      void this.refresh();
    }
  }
  async load() {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.current = chooseEffort(this.slug, this.efforts);
    this.selected = "";
    this.detail = null;
    this.git = null;
    this.fresh = true;
    this.stream.close();
    await this.refresh();
  }
  /** refresh refetches the efforts, the task list and the open task. */
  async refresh() {
    const seq = this.stateSeq;
    try {
      const live = await this.deps.project(this.slug);
      if (seq === this.stateSeq) {
        this.runState = live.run_state;
        this.stopReason = live.stop_reason ?? "";
      }
    } catch {
    }
    try {
      this.efforts = await this.deps.efforts(this.slug);
      this.current = this.efforts.find((e) => e.id === this.current?.id) ?? chooseEffort(this.slug, this.efforts);
      this.loaded = true;
      const ef = this.current;
      this.tasks = ef ? await this.deps.tasks(this.slug, ef.id) : [];
      if (ef && this.selected && this.tasks.some((t) => t.key === this.selected)) this.detail = await this.deps.task(this.slug, ef.id, this.selected);
      if (ef?.status === "complete" && !this.git && this.gitFailed !== ef.id) {
        try {
          this.git = await this.deps.gitCommands(this.slug, ef.id, { quiet: true });
        } catch {
          this.gitFailed = ef.id;
        }
      }
    } catch {
    }
    this.render();
  }
  async select(key) {
    const ef = this.current;
    if (!ef) return;
    this.selected = key;
    this.editing = false;
    this.fresh = true;
    try {
      this.detail = await this.deps.task(this.slug, ef.id, key);
    } catch {
      return;
    }
    this.render();
  }
  async control(op) {
    if (op === "stop" && !await this.deps.confirm(`Stop ${this.slug}? Running stages are cancelled.`, "Stop")) return;
    try {
      await this.deps.run(this.slug, op);
    } catch {
      return;
    }
    await this.refresh();
  }
  // ─── Halted-task actions (AT9) ───────────────────────────────────────────
  async retry(key, clean) {
    const ef = this.current;
    if (!ef || !await this.deps.confirm(`Retry ${key}? Its attempt counters are reset${clean ? " and its worktree is recreated clean" : ""}.`, "Retry")) return;
    try {
      await this.deps.retryTask(this.slug, ef.id, key, clean);
    } catch {
      return;
    }
    this.fresh = true;
    this.deps.notify(`Retrying ${key}`);
    await this.refresh();
  }
  async applyAdvice(key) {
    const ef = this.current;
    const apply = this.deps.applyResolution;
    if (!ef || !apply || !await this.deps.confirm(`Apply the resolver's advice to ${key} and retry it in the same worktree?`, "Apply")) return;
    try {
      await apply(this.slug, ef.id, key);
    } catch {
      return;
    }
    this.fresh = true;
    this.deps.notify(`Applied the resolver's advice to ${key}`);
    await this.refresh();
  }
  async override(key, reason) {
    const ef = this.current;
    if (!ef) return;
    if (reason.trim() === "") {
      this.errors.show(new ApiError(400, "A reason is required", "reason"));
      return;
    }
    if (!await this.deps.confirm(`Mark ${key} as passed without a passing attempt? The reason is recorded.`, "Override")) return;
    try {
      await this.deps.overrideTask(this.slug, ef.id, key, reason.trim());
    } catch {
      return;
    }
    this.fresh = true;
    this.deps.notify(`Overrode ${key}`);
    await this.refresh();
  }
  async abandon() {
    const ef = this.current;
    if (!ef || !await this.deps.confirm(`Abandon effort #${ef.id}? It ends the effort; its tasks and history stay for reference.`, "Abandon")) return;
    try {
      await this.deps.abandonEffort(this.slug, ef.id);
    } catch {
      return;
    }
    this.fresh = true;
    this.deps.notify("Effort abandoned");
    await this.refresh();
  }
  /** saveEdit stores an edited task. An approved queue stays approved and a run goes on, so the edit needs no second approval. The form stays as it is. */
  async saveEdit(def) {
    const ef = this.current;
    if (!ef) return false;
    this.errors.clear();
    try {
      await this.deps.editTask(this.slug, ef.id, def.key, def, { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return false;
    }
    this.deps.notify(`Saved ${def.key}`);
    return true;
  }
  editForm(d) {
    this.errors = new FormErrors();
    const title = textInput("title", d.title);
    const phase = textInput("phase", d.phase);
    const prompt = textArea("prompt", d.prompt, 6);
    const acceptance = textArea("acceptance", (d.acceptance ?? []).join("\n"), 3);
    const files = textArea("files_touched", (d.files_touched ?? []).join("\n"), 3);
    const deps = textInput("depends_on", (d.depends_on ?? []).join(", "));
    const split = (s) => s.split("\n").map((l) => l.trim()).filter((l) => l !== "");
    const e = this.errors;
    const read = () => ({
      key: d.key,
      title: title.value.trim(),
      phase: phase.value.trim(),
      prompt: prompt.value,
      acceptance: split(acceptance.value),
      files_touched: split(files.value),
      depends_on: deps.value.split(",").map((s) => s.trim()).filter((s) => s !== "")
    });
    const auto = autosave({ snapshot: () => JSON.stringify(read()), save: () => this.saveEdit(read()) });
    const form = h(
      "div",
      { attrs: { "data-role": "edit-task" } },
      h("p", { class: "field-hint", text: "A saved change to an approved queue is approved with it, and a run carries on. Stages built on the old task are discarded." }),
      e.add("title", fieldRow("Title", title)).el,
      e.add("phase", fieldRow("Phase", phase)).el,
      e.add("prompt", fieldRow("Prompt", prompt)).el,
      e.add("acceptance", fieldRow("Acceptance criteria", acceptance, "One per line")).el,
      e.add("files_touched", fieldRow("Files touched", files, "One path per line")).el,
      e.add("depends_on", fieldRow("Depends on", deps, "Task keys, comma separated")).el,
      e.general,
      h(
        "div",
        { class: "row-actions" },
        auto.status,
        button("Done", () => void auto.flush().then(() => {
          this.editing = false;
          this.fresh = true;
          return this.refresh();
        }), { primary: true, field: "close-edit", icon: "approve" })
      )
    );
    auto.watch(form);
    return form;
  }
  actions(d) {
    const clean = checkbox("clean_worktree", false);
    const reason = textArea("override_reason", "", 2);
    this.errors = new FormErrors();
    this.reasonEl = reason;
    this.cleanEl = clean;
    const reasonRow = this.errors.add("reason", fieldRow("Override reason", reason, "Recorded when you mark the task passed"));
    return h(
      "div",
      { class: "detail-section", attrs: { "data-panid": "actions" } },
      h("h4", { text: "Halted" }),
      h("span", { class: "inline-check" }, clean, "Clean worktree on retry"),
      button("Retry", () => void this.retry(d.key, clean.checked), { field: "retry", icon: "retry" }),
      button("Edit task", () => {
        this.editing = true;
        this.fresh = true;
        this.render();
      }, { field: "edit", icon: "edit" }),
      reasonRow.el,
      button("Override as passed", () => void this.override(d.key, reason.value), { field: "override", icon: "approve", word: true }),
      button("Abandon effort", () => void this.abandon(), { field: "abandon", icon: "trash", word: true }),
      this.errors.general
    );
  }
  // ─── Detail pane ─────────────────────────────────────────────────────────
  attempt(a) {
    const verify = stageOf(a, "verify");
    const review = stageOf(a, "reviewer");
    const worker = stageOf(a, "worker");
    return h(
      "div",
      { class: "profile-card", attrs: { "data-attempt": a.n } },
      h(
        "div",
        { class: "row-actions" },
        h("strong", { text: `Attempt ${a.n}` }),
        a.kind === "repair" ? badge("repair", "warn") : null,
        chip(a.outcome || "running"),
        h("span", { class: "field-hint", text: `worker ${a.worker_model || "-"} \xB7 reviewer ${a.reviewer_model || "-"}` }),
        a.review_independence === "same_family" ? badge("reviewer is the worker's model family", "warn") : null,
        a.review_independence === "same_model" ? badge("reviewer is the worker's model", "warn") : null
      ),
      h("div", { class: "wizard-list" }, ...a.stages.map((s) => h(
        "div",
        { class: "inline-row", attrs: { "data-stage": s.id } },
        h("span", { text: `${s.role} \xB7 ${s.backend ? `${s.backend}/${s.model}` : "engine"} \xB7 ${s.outcome || s.state}` }),
        s.relaxations?.length ? badge(`relaxed: ${s.relaxations.join(", ")}`, "warn") : null,
        button("Output", () => this.stream.open(s), { field: "open-stream", icon: "view" })
      ))),
      worker?.parsed ? block("Worker summary", h("p", { text: String(rec(worker.parsed).summary ?? "") })) : null,
      verify?.parsed ? block("VerifyReport", renderVerify(verify.parsed)) : null,
      review?.parsed ? block("Reviewer", renderReviewOut(review.parsed)) : null
    );
  }
  detailPane() {
    const d = this.detail;
    if (!d) return h("div", { class: "console-right", attrs: { "data-role": "detail-pane", hidden: true } });
    const cur = d.current;
    return h(
      "div",
      { class: "console-right", attrs: { "data-role": "detail-pane" } },
      h(
        "div",
        { class: "detail-body" },
        h(
          "div",
          { class: "detail-header" },
          h("h3", { text: `${d.key} \xB7 ${d.title}` }),
          chip(d.status),
          button("Close", () => {
            this.selected = "";
            this.detail = null;
            this.editing = false;
            this.fresh = true;
            this.render();
          }, { field: "close-detail", icon: "cancel" })
        ),
        h("div", { class: "detail-attempts", text: `Attempts: ${d.attempts}` }),
        d.waiting_reason ? block("Waiting", h("p", { text: d.waiting_reason, attrs: { "data-role": "waiting-reason" } })) : null,
        cur ? block(
          "Running now",
          h("p", { text: `${cur.role} \xB7 ${cur.backend}/${cur.model} \xB7 ${cur.state}` }),
          h("div", { class: "row-actions" }, button("Watch live", () => this.stream.open({
            id: cur.stage,
            role: cur.role,
            backend: cur.backend,
            model: cur.model,
            state: cur.state === "running" ? "running" : cur.state,
            outcome: "",
            raw_output: ""
          }), { field: "watch-live", icon: "view" }))
        ) : null,
        this.editing ? this.editForm(d) : null,
        d.halted && !this.editing ? this.actions(d) : null,
        block("Prompt", mono(d.prompt)),
        d.acceptance?.length ? block("Acceptance", bullets(d.acceptance ?? [])) : null,
        d.files_touched?.length ? block("Files touched", mono((d.files_touched ?? []).join("\n"))) : null,
        d.depends_on?.length ? block("Depends on", h("p", { text: (d.depends_on ?? []).join(", ") })) : null,
        block("Attempts", ...d.attempt_list.length ? d.attempt_list.map((a) => this.attempt(a)) : [h("p", { class: "field-hint", text: "No attempts yet." })]),
        d.resolutions?.length ? block(
          "Resolver",
          bullets(d.resolutions.map((r) => `${r.pending ? "advised" : "resolved"} by ${r.model ? `${r.model} on ${r.backend}` : "the engine"}: ${r.decision}: ${r.reason}${r.pending ? " (not applied yet)" : ""}`)),
          d.resolutions.some((r) => r.pending) && this.deps.applyResolution ? h("button", { text: "Apply the resolver's advice and retry", attrs: { type: "button", "data-role": "apply-resolution" }, on: { click: () => void this.applyAdvice(d.key) } }) : null
        ) : null,
        d.progress.length ? block("Progress log", bullets(d.progress)) : null
      )
    );
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading board\u2026" }));
      return;
    }
    const ef = this.current;
    if (!ef) {
      replace(this.host, h("div", { class: "plan-empty" }, h("h3", { text: "No effort yet" }), h("p", { text: "Create an effort on the FRD tab; its tasks appear here once the queue is approved." })));
      return;
    }
    const { pct: pct2, text } = progressText(counts(this.tasks));
    const bar = h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar", attrs: { style: `width: ${pct2}%` } }));
    const typed = (this.reasonEl?.value ?? "") !== "" || this.cleanEl?.checked === true;
    const keep = !this.fresh && this.detailEl !== null && (this.editing || typed);
    if (!keep) {
      this.reasonEl = null;
      this.cleanEl = null;
      this.detailEl = this.detailPane();
    }
    this.fresh = false;
    const controls = buttonsFor(this.runState).map((b) => button(b.label, () => void this.control(b.op), { primary: b.primary, op: b.op, icon: b.icon, word: b.word }));
    replace(
      this.host,
      effortBar(this.slug, this.efforts, ef, (id) => {
        this.current = this.efforts.find((e) => e.id === id);
        void this.load();
      }, { deps: this.yolo, onChanged: () => void this.refresh() }),
      h(
        "div",
        { class: "console-header" },
        h("span", { class: "pane-title", text: "Progress" }),
        bar,
        h("span", { class: "progress-pct", text }),
        chip(this.runState),
        ef.run_state !== this.runState && ef.run_state === "running" ? h("span", { class: "field-hint", text: "was running before restart", attrs: { "data-role": "was-running" } }) : null,
        ...controls
      ),
      ef.status === "complete" ? mergeCard(ef, this.git, this.deps.copy) : null,
      ef.budget ? budgetBar(ef.budget) : null,
      this.runState === "stopped" && this.stopReason === "budget" ? h(
        "div",
        { class: "warn-box", attrs: { "data-role": "budget-stop" } },
        h("p", { text: "The run was stopped because the effort used up its budget. Nothing failed: raise the budget on the FRD tab and start the run again." })
      ) : null,
      h(
        "div",
        { class: "console-body" },
        h(
          "div",
          { class: "console-left" },
          h("h3", { class: "pane-title", text: "Tasks" }),
          this.tasks.length === 0 ? h("p", { class: "field-hint", text: ef.queue ? "The queue has no tasks." : "No queue yet: plan, compile and approve the queue first." }) : h("ul", { class: "task-list" }, ...this.tasks.map((t) => h(
            "li",
            { class: `task-item status-${statusKey(t.status)}` + (t.key === this.selected ? " selected" : ""), attrs: { "data-task": t.key }, on: {
              click: () => void this.select(t.key)
            } },
            h("div", { class: "task-item-title", text: t.title }),
            h("div", { class: "task-item-meta" }, chip(t.status), h("span", { text: `${t.key} \xB7 attempts ${t.attempts}` })),
            t.waiting_reason ? h("div", { class: "task-item-meta", text: t.waiting_reason }) : null,
            t.current ? h("div", { class: "task-item-meta", text: `${t.current.role} \xB7 ${t.current.backend}/${t.current.model}` }) : null
          )))
        ),
        this.detailEl
      )
    );
  }
};
function mountBoard(host, slug) {
  const panel = document.getElementById("stream-panel");
  if (!panel) throw new Error("board: #stream-panel is missing from the page");
  return new Board(host, panel, slug, defaultBoardDeps(), defaultStreamDeps()).mount();
}

// web/agentflow/js/history.ts
import { showToast as showToast7 } from "/shared/dist/shared.mjs";
var defaultHistoryDeps = () => ({
  efforts: api.efforts,
  importFRD: api.importFRD,
  readFile: (f) => f.text(),
  notify: (m) => void showToast7(m, "success")
});
var when = (ms) => ms ? new Date(ms).toLocaleString() : "";
function link(label, href, name) {
  return h("a", { class: "ui-modal-btn", text: label, attrs: { href, download: "", "data-export": name } });
}
var History = class {
  constructor(host, slug, deps, yolo) {
    this.host = host;
    this.slug = slug;
    this.deps = deps;
    this.yolo = yolo;
    this.efforts = [];
    this.loaded = false;
    this.errors = new FormErrors();
    this.fileInput = h("input", { attrs: { type: "file", accept: ".json,application/json", name: "frd_file", "data-field": "frd_file" } });
    this.off = null;
    this.stopPoll = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "project.update")) void this.load();
    });
    this.stopPoll = startPoll(() => this.efforts.some((e) => isTransient(e.status)), () => this.load());
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
    };
  }
  async load() {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.render();
  }
  /** importFile posts the chosen FRD JSON; the server validates it and names a bad field. */
  async importFile() {
    this.errors.clear();
    const file = this.fileInput.files?.[0];
    if (!file) {
      this.errors.show(new Error("Choose a JSON file first"));
      return;
    }
    try {
      const text = await this.deps.readFile(file);
      const ef = await this.deps.importFRD(this.slug, text, { quiet: true });
      rememberEffort(this.slug, ef.id);
    } catch (err) {
      this.errors.show(err);
      return;
    }
    this.deps.notify("FRD imported as a new effort");
    await this.load();
  }
  row(e) {
    return h(
      "li",
      { class: "profile-card", attrs: { "data-effort": e.id } },
      h(
        "div",
        { class: "profile-header" },
        h("span", { class: "row-actions" }, h("strong", { text: `#${e.id} ${e.title}` }), chip(e.status), e.yolo ? yoloBadge() : null),
        h("span", { class: "field-hint", text: when(e.created) })
      ),
      h("div", { class: "task-item-meta", text: `FRD ${e.frd.latest ? `revision ${e.frd.latest}${e.frd.approved ? ` (approved ${e.frd.approved})` : ""}` : "none"} \xB7 plan ${e.plan.latest ? `revision ${e.plan.latest}${e.plan.approved ? ` (approved ${e.plan.approved})` : ""}` : "none"} \xB7 queue ${e.queue ? `revision ${e.queue.revision} ${e.queue.status}` : "none"}` }),
      h(
        "div",
        { class: "row-actions" },
        link("Effort JSON", exportURL(e.id, "json"), "effort-json"),
        link("Effort Markdown", exportURL(e.id, "md"), "effort-md"),
        e.frd.latest ? link("FRD JSON", exportURL(e.id, "json", "frd"), "frd-json") : null,
        e.frd.latest ? link("FRD Markdown", exportURL(e.id, "md", "frd"), "frd-md") : null,
        e.plan.latest ? link("Plan JSON", exportURL(e.id, "json", "plan"), "plan-json") : null,
        e.plan.latest ? link("Plan Markdown", exportURL(e.id, "md", "plan"), "plan-md") : null
      ),
      yoloControl(this.slug, e, this.yolo, () => void this.load())
    );
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading efforts\u2026" }));
      return;
    }
    this.errors = new FormErrors();
    const fileRow = this.errors.add("body", fieldRow("FRD file", this.fileInput, "A JSON FRD, for example one exported from here; it becomes a new effort at revision 1"));
    this.errors.add("frd", fileRow);
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host" },
        section(
          "Efforts",
          this.efforts.length ? h("ul", { class: "profile-list", attrs: { "data-role": "history-list" } }, ...[...this.efforts].reverse().map((e) => this.row(e))) : h("p", { class: "field-hint", text: "No efforts yet." })
        ),
        section(
          "Import an FRD",
          fileRow.el,
          this.errors.general,
          h("div", { class: "settings-actions" }, button("Import FRD", () => void this.importFile(), { primary: true, field: "import-frd", icon: "import" }))
        )
      )
    );
  }
};
function mountHistory(host, slug) {
  return new History(host, slug, defaultHistoryDeps(), defaultYoloDeps()).mount();
}

// web/agentflow/js/frd-editor.ts
import { alertDialog as alertDialog2, showToast as showToast9 } from "/shared/dist/shared.mjs";

// web/agentflow/js/plan-view.ts
import { confirmDialog as confirmDialog6, showToast as showToast8 } from "/shared/dist/shared.mjs";
var defaultPlanDeps = () => ({
  efforts: api.efforts,
  plan: api.plan,
  planRounds: api.planRounds,
  planRevisions: api.planRevisions,
  planRevision: api.planRevision,
  generatePlan: api.generatePlan,
  approvePlan: api.approvePlan,
  confirm: (m) => confirmDialog6(m, { confirmLabel: "Approve" }),
  notify: (m) => void showToast8(m, "success")
});
var PLAN_TABS = ["planner", "architect", "critic"];
var list = (title, items) => items && items.length > 0 ? h("div", { class: "plan-section" }, h("h4", { text: title }), h("ul", {}, ...items.map((t) => h("li", { text: t })))) : null;
function renderPlan(p) {
  return h(
    "div",
    { class: "plan-content", attrs: { "data-role": "plan-doc" } },
    list("Principles", p.principles),
    list("Decision drivers", p.drivers),
    p.options?.length ? h(
      "div",
      { class: "plan-section" },
      h("h4", { text: "Options" }),
      ...p.options.map((o) => h(
        "div",
        {},
        h("strong", { text: o.name }),
        h("p", { text: o.description }),
        list("Pros", o.pros),
        list("Cons", o.cons)
      ))
    ) : null,
    p.phases?.length ? h(
      "div",
      { class: "plan-section" },
      h("h4", { text: "Phases" }),
      ...p.phases.map((ph) => h(
        "div",
        { class: "phase-card" },
        h("div", { class: "phase-header" }, h("strong", { text: `${ph.id} ${ph.name}` })),
        h("div", { class: "phase-desc", text: ph.tasks_description }),
        ph.depends_on?.length ? h("div", { class: "phase-deps", text: `depends on ${ph.depends_on.join(", ")}` }) : null
      ))
    ) : null,
    p.adr ? h(
      "div",
      { class: "plan-section" },
      h("h4", { text: "Decision record" }),
      h("p", { text: p.adr.decision }),
      h("p", { text: p.adr.why }),
      list("Alternatives", p.adr.alternatives),
      list("Consequences", p.adr.consequences),
      list("Follow-ups", p.adr.followups)
    ) : null,
    list("Pre-mortem", p.premortem),
    list("Test plan", p.test_plan)
  );
}
function renderReview(r, role) {
  if (!r) return h("p", { class: "field-hint", text: `No ${role} review in this round yet.` });
  return h(
    "div",
    { class: "review-section", attrs: { "data-role": `review-${role}` } },
    h("div", { class: "row-actions" }, chip(r.verdict), h("span", { class: "field-hint", text: `on plan revision ${r.plan_revision}` })),
    r.summary ? h("p", { text: r.summary }) : null,
    r.issues?.length ? h("ul", { class: "plan-review-notes" }, ...r.issues.map((i) => h("li", {}, h("strong", { text: i.severity }), ` ${i.description}`, i.file ? ` (${i.file}${i.line ? ":" + i.line : ""})` : null))) : null
  );
}
function elapsedText(ms) {
  const s = Math.max(0, Math.floor(ms / 1e3));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, "0")}s`;
}
function stageName(status, rounds, latestRevision) {
  if (status === "plan_approved") return "Plan approved: the compile has not started or has stopped";
  if (status === "compiling") return "Compiler: turning the plan into tasks";
  if (status !== "planning") return "";
  const reviewed = Math.max(0, ...rounds.flatMap((r) => r.reviews.map((x) => x.plan_revision)));
  if (latestRevision === 0) return "Planner: drafting the first plan";
  if (latestRevision > reviewed) return `Architect: reviewing plan revision ${latestRevision}`;
  const last = rounds[rounds.length - 1];
  if (last && !last.reviews.some((x) => x.role === "critic")) return `Critic: reviewing round ${last.round}`;
  return `Planner: revising the plan after round ${last?.round ?? 1}`;
}
var PlanViewer = class {
  constructor(host, slug, deps) {
    this.host = host;
    this.slug = slug;
    this.deps = deps;
    this.efforts = [];
    this.state = null;
    this.rounds = [];
    this.revisions = [];
    this.round = 0;
    this.tab = "planner";
    this.docs = /* @__PURE__ */ new Map();
    this.viewing = null;
    this.loaded = false;
    // Set on dispose: a fetch still in flight must not draw or start a timer afterwards.
    this.disposed = false;
    this.off = null;
    this.stopPoll = null;
    this.ticker = null;
    this.elapsedEl = null;
    this.since = 0;
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "project.update")) void this.load();
    });
    this.stopPoll = startPoll(() => isTransient(this.current?.status), () => this.load());
    void this.load();
    return () => {
      this.disposed = true;
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
      this.stopTicker();
    };
  }
  stopTicker() {
    if (this.ticker) clearInterval(this.ticker);
    this.ticker = null;
    this.elapsedEl = null;
  }
  async load() {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.current = chooseEffort(this.slug, this.efforts);
    await this.loadPlan();
  }
  /**
   * loadPlan fetches the plan, rounds and revisions as one snapshot and swaps
   * it in whole, so the view never mixes a new status with old rounds. A failed
   * fetch keeps what is shown.
   */
  async loadPlan() {
    const ef = this.current;
    if (!ef) {
      this.state = null;
      this.rounds = [];
      this.revisions = [];
      this.viewing = null;
      this.latestDoc = void 0;
      this.render();
      return;
    }
    try {
      const [state, rounds, revisions] = await Promise.all([
        this.deps.plan(this.slug, ef.id),
        this.deps.planRounds(this.slug, ef.id),
        this.deps.planRevisions(this.slug, ef.id)
      ]);
      const revs = revisions ?? [];
      let latestDoc = state.plan;
      if (!latestDoc && revs.length > 0) {
        try {
          latestDoc = (await this.deps.planRevision(this.slug, ef.id, Math.max(...revs.map((r) => r.revision)))).json;
        } catch {
        }
      }
      if (this.current?.id !== ef.id) return;
      this.state = { ...state, warnings: rounds.warnings ?? state.warnings, waiting: rounds.waiting ?? state.waiting };
      this.current = { ...ef, status: state.status };
      this.rounds = (rounds.rounds ?? []).map((r) => ({ ...r, reviews: r.reviews ?? [] }));
      this.revisions = revs;
      this.latestDoc = latestDoc;
      if (this.viewing && !revs.some((r) => r.revision === this.viewing?.revision)) this.viewing = null;
    } catch {
    }
    const last = this.rounds[this.rounds.length - 1]?.round ?? 0;
    if (this.round === 0 || !this.rounds.some((r) => r.round === this.round)) this.round = last;
    await this.fetchRoundPlan();
    this.render();
  }
  /** The plan revision the planner produced for the selected round, from its reviews. */
  roundPlanRevision() {
    const r = this.rounds.find((x) => x.round === this.round);
    return r?.reviews[0]?.plan_revision ?? 0;
  }
  async fetchRoundPlan() {
    const ef = this.current;
    const rev = this.roundPlanRevision();
    if (!ef || rev === 0 || this.docs.has(rev * 1e3 + ef.id)) return;
    try {
      const d = await this.deps.planRevision(this.slug, ef.id, rev);
      if (d.json) this.docs.set(rev * 1e3 + ef.id, d.json);
    } catch {
    }
  }
  plannerDoc() {
    const ef = this.current;
    const rev = this.roundPlanRevision();
    if (ef && rev > 0) return this.docs.get(rev * 1e3 + ef.id);
    return this.state?.plan ?? this.latestDoc;
  }
  async generate() {
    const ef = this.current;
    if (!ef) return;
    try {
      await this.deps.generatePlan(this.slug, ef.id);
    } catch {
      return;
    }
    this.deps.notify("Planning started");
    await this.load();
  }
  async approve() {
    const ef = this.current;
    const st = this.state;
    if (!ef || !st) return;
    if (st.not_critic_approved && !await this.deps.confirm(
      "The critic did not approve this plan (the revision rounds ran out). Approve revision " + st.revision + " anyway?"
    )) return;
    try {
      await this.deps.approvePlan(this.slug, ef.id);
    } catch {
      return;
    }
    this.deps.notify(`Approved plan revision ${st.revision}`);
    await this.load();
  }
  async viewRevision(n) {
    const ef = this.current;
    if (!ef) return;
    try {
      this.viewing = await this.deps.planRevision(this.slug, ef.id, n);
    } catch {
      return;
    }
    this.render();
  }
  header(ef, st) {
    const approved = this.revisions.find((r) => r.approved)?.revision;
    const canGenerate = ef.frd.latest > 0 && (ef.status === "draft" || ef.status === "plan_ready");
    const canApprove = ef.status === "plan_ready" && st.revision > 0 && approved !== st.revision;
    return h(
      "div",
      { class: "plan-header", attrs: { "data-role": "plan-header" } },
      h(
        "div",
        { class: "plan-meta" },
        chip(ef.status),
        st.revision > 0 ? h("span", { class: "plan-frd-id", text: `revision ${st.revision} \xB7 ${st.rounds} round${st.rounds === 1 ? "" : "s"}` }) : h("span", { class: "plan-frd-id", text: this.latestDoc ? "first draft, not reviewed yet" : "no plan yet" }),
        st.not_critic_approved ? badge("not critic-approved", "warn") : null,
        approved ? badge(`revision ${approved} approved`, "ok") : null
      ),
      h(
        "div",
        { class: "plan-actions" },
        canGenerate ? button(st.revision > 0 ? "Regenerate plan" : "Generate plan", () => void this.generate(), { field: "generate-plan", icon: "refresh" }) : null,
        canApprove ? button("Approve plan", () => void this.approve(), { primary: true, field: "approve-plan", icon: "approve" }) : null
      )
    );
  }
  /** strip is the live indicator shown while the engine plans or compiles (compiling starts by itself once the plan is approved): stage, and time since the effort last changed. */
  strip(ef) {
    if (ef.status !== "planning" && ef.status !== "plan_approved" && ef.status !== "compiling") {
      this.stopTicker();
      return null;
    }
    const stage = stageName(ef.status, this.rounds, Math.max(0, ...this.revisions.map((r) => r.revision)));
    if (ef.status === "plan_approved") {
      this.stopTicker();
      return h(
        "div",
        { class: "progress-strip", attrs: { "data-role": "progress-strip", role: "status" } },
        h("div", { class: "row-actions" }, h("strong", { text: stage })),
        h("p", { class: "field-hint", text: "Press Compile on the Queue tab to try again." })
      );
    }
    this.since = ef.updated;
    const elapsed = h("span", { class: "field-hint", text: this.since > 0 ? elapsedText(Date.now() - this.since) : "", attrs: { "data-role": "elapsed" } });
    this.elapsedEl = elapsed;
    if (!this.ticker) {
      this.ticker = setInterval(() => {
        if (this.elapsedEl && this.since > 0) this.elapsedEl.textContent = elapsedText(Date.now() - this.since);
      }, 1e3);
    }
    return h(
      "div",
      { class: "progress-strip", attrs: { "data-role": "progress-strip", role: "status" } },
      h("div", { class: "row-actions" }, h("strong", { text: stage }), elapsed),
      h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar progress-indeterminate" }))
    );
  }
  roundTabs() {
    const review = (role) => this.rounds.find((r) => r.round === this.round)?.reviews.filter((x) => x.role === role).pop();
    return h(
      "div",
      {},
      h("div", { class: "tab-bar", attrs: { role: "tablist" } }, ...this.rounds.map((r) => h("button", {
        class: "tab-btn" + (r.round === this.round ? " active" : ""),
        text: `Round ${r.round}`,
        attrs: { type: "button", "data-round": r.round },
        on: { click: () => {
          this.round = r.round;
          void this.fetchRoundPlan().then(() => this.render());
        } }
      }))),
      h("div", { class: "tab-bar", attrs: { role: "tablist" } }, ...PLAN_TABS.map((t) => {
        const v = t === "planner" ? void 0 : review(t);
        return h("button", {
          class: "tab-btn" + (t === this.tab ? " active" : ""),
          attrs: { type: "button", "data-plan-tab": t },
          on: { click: () => {
            this.tab = t;
            this.render();
          } }
        }, t, v ? " " : null, v ? chip(v.verdict) : null);
      })),
      this.tab === "planner" ? this.plannerDoc() ? renderPlan(this.plannerDoc()) : h("p", { class: "field-hint", text: "No plan for this round yet." }) : renderReview(review(this.tab), this.tab)
    );
  }
  revisionList() {
    const v = this.viewing;
    return section(
      "Plan revisions",
      this.revisions.length ? h("div", { class: "wizard-list" }, ...this.revisions.map((r) => h(
        "div",
        { class: "inline-row", attrs: { "data-revision": r.revision } },
        h("span", { text: `Revision ${r.revision} \xB7 ${r.created_by}` }),
        r.approved ? badge("approved", "ok") : null,
        button("View", () => void this.viewRevision(r.revision), { field: "view-revision", icon: "view" })
      ))) : h("p", { class: "field-hint", text: "No revisions yet." }),
      v ? h(
        "div",
        { attrs: { "data-role": "revision-view" } },
        h("h4", { text: `Revision ${v.revision}${v.approved ? " (approved)" : ""}` }),
        v.json ? renderPlan(v.json) : null,
        button("Close", () => {
          this.viewing = null;
          this.render();
        }, { field: "close-revision", icon: "cancel" })
      ) : null
    );
  }
  render() {
    if (this.disposed) return;
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading efforts\u2026" }));
      return;
    }
    const ef = this.current;
    const st = this.state;
    if (!ef || !st) {
      this.stopTicker();
      replace(this.host, h("div", { class: "plan-empty" }, h("h3", { text: "No effort to plan" }), h("p", { text: "Create an effort and save its FRD on the FRD tab first." })));
      return;
    }
    const warnings = st.warnings ?? [];
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host" },
        effortBar(this.slug, this.efforts, ef, (id) => {
          this.current = this.efforts.find((e) => e.id === id);
          this.round = 0;
          void this.loadPlan();
        }),
        this.header(ef, st),
        warnings.length ? h("div", { class: "warn-box", attrs: { "data-role": "plan-warnings" } }, ...warnings.map((w) => h("p", { text: w }))) : null,
        st.waiting ? h("div", { class: "warn-box", attrs: { "data-role": "plan-waiting" } }, h("p", { text: `Waiting: ${st.waiting}` })) : null,
        this.strip(ef),
        this.rounds.length > 0 || this.plannerDoc() ? this.roundTabs() : h("div", { class: "plan-empty" }, h("h3", { text: "No plan yet" }), h("p", { text: "Generate a plan once the FRD is saved." })),
        this.revisionList()
      )
    );
  }
};
function mountPlan(host, slug) {
  return new PlanViewer(host, slug, defaultPlanDeps()).mount();
}

// web/agentflow/js/frd-editor.ts
var defaultFrdDeps = () => ({
  efforts: api.efforts,
  createEffort: api.createEffort,
  patchEffort: api.patchEffort,
  frd: api.frd,
  putFRD: api.putFRD,
  frdRevisions: api.frdRevisions,
  frdRevision: api.frdRevision,
  approveFRD: api.approveFRD,
  draftFRD: api.draftFRD,
  draftState: api.draftState,
  definitionOfDone: api.definitionOfDone,
  frdSize: api.frdSize,
  splitFRD: api.splitFRD,
  frdParts: api.frdParts,
  startPart: api.startPart,
  notify: (m) => void showToast9(m, "success")
});
var PRIORITIES = [["", "(none)"], ["must", "must"], ["should", "should"], ["could", "could"], ["wont", "won't"]];
var lines = (s) => s.split("\n").map((l) => l.trim()).filter((l) => l !== "");
var commaList = (s) => s.split(",").map((l) => l.trim()).filter((l) => l !== "");
var when2 = (ms) => ms ? new Date(ms).toLocaleString() : "";
function blankFRD(slug, effort) {
  return {
    id: `FRD-${slug}-${effort.id}`.toUpperCase(),
    title: effort.title,
    objective: "",
    requirements: [{ id: "", priority: "must", description: "" }],
    acceptance_criteria: []
  };
}
var FrdForm = class {
  constructor(base, onChange) {
    this.base = base;
    this.errors = new FormErrors();
    this.f = {
      id: textInput("id", base.id ?? "", { readOnly: true }),
      title: textInput("title", base.title ?? ""),
      status: textInput("status", base.status ?? "", { readOnly: true }),
      objective: textArea("objective", base.objective ?? "", 3),
      context: textArea("context", base.context ?? "", 3),
      constraints: textArea("constraints", (base.constraints ?? []).join("\n"), 3),
      toolchains: textInput("toolchains", (base.toolchains ?? []).join(", "), { placeholder: "dotnet" }),
      out_of_scope: textArea("out_of_scope", (base.out_of_scope ?? []).join("\n"), 3)
    };
    for (const c of Object.values(this.f)) c.addEventListener("input", onChange);
    this.groups = new RowsEditor((g) => {
      const id = textInput("group_id", g.id, { placeholder: "id" });
      const title = textInput("group_title", g.title, { placeholder: "title" });
      const summary = textInput("group_summary", g.summary ?? "", { placeholder: "summary" });
      return { el: h("div", { class: "row-actions" }, id, title, summary), read: () => ({ id: id.value.trim(), title: title.value.trim(), summary: summary.value.trim() || void 0 }) };
    }, base.groups ?? [], () => ({ id: "", title: "" }), "Add group", onChange);
    this.reqs = new RowsEditor((r) => {
      const group = textInput("req_group", r.group ?? "", { placeholder: "group" });
      const prio = selectInput("req_priority", PRIORITIES, r.priority ?? "");
      const desc = textArea("req_description", r.description, 2);
      return {
        el: h("div", { class: "wizard-field" }, h("div", { class: "row-actions" }, group, prio), desc),
        read: () => ({ ...r, group: group.value.trim() || void 0, priority: prio.value || void 0, description: desc.value.trim() })
      };
    }, base.requirements ?? [], () => ({ id: "", description: "" }), "Add requirement", onChange);
    this.acs = new RowsEditor((a) => {
      const crit = textArea("ac_criterion", a.criterion, 2);
      return { el: h("div", { class: "wizard-field" }, crit), read: () => ({ ...a, criterion: crit.value.trim() }) };
    }, base.acceptance_criteria ?? [], () => ({ id: "", criterion: "" }), "Add acceptance criterion", onChange);
    this.decisions = new RowsEditor((d) => {
      const id = textInput("decision_id", d.id, { placeholder: "id" });
      const dec = textInput("decision_text", d.decision, { placeholder: "decision" });
      const why = textInput("decision_rationale", d.rationale ?? "", { placeholder: "rationale" });
      return { el: h("div", { class: "row-actions" }, id, dec, why), read: () => ({ id: id.value.trim(), decision: dec.value.trim(), rationale: why.value.trim() || void 0 }) };
    }, base.decisions ?? [], () => ({ id: "", decision: "" }), "Add decision", onChange);
    const e = this.errors;
    const verbatim = ["state_model", "findings", "planning_spikes"].filter((k) => base[k] !== void 0);
    this.el = h(
      "div",
      { class: "detail-body", attrs: { "data-role": "frd-form" } },
      h(
        "div",
        { class: "profile-fields" },
        e.add("id", fieldRow("ID", this.f.id)).el,
        e.add("title", fieldRow("Title", this.f.title)).el,
        e.add("status", fieldRow("Status", this.f.status)).el
      ),
      e.add("objective", fieldRow("Objective", this.f.objective)).el,
      e.add("context", fieldRow("Context", this.f.context)).el,
      e.add("constraints", fieldRow("Constraints", this.f.constraints, "One per line")).el,
      e.add("toolchains", fieldRow("Toolchains", this.f.toolchains, "Comma-separated, for example dotnet")).el,
      e.add("groups", fieldRow("Groups", this.groups.el)).el,
      e.add("requirements", fieldRow("Requirements", this.reqs.el)).el,
      e.add("acceptance_criteria", fieldRow("Acceptance criteria", this.acs.el)).el,
      e.add("decisions", fieldRow("Decisions", this.decisions.el)).el,
      e.add("out_of_scope", fieldRow("Out of scope", this.f.out_of_scope, "One per line")).el,
      verbatim.length ? h("p", { class: "field-hint", text: `Kept as they are: ${verbatim.join(", ")}` }) : null,
      e.general
    );
  }
  /** read assembles the FRD: the loaded document with the edited fields replaced. */
  read() {
    const v = (k) => this.f[k].value.trim();
    const some = (a) => a.length ? a : void 0;
    const requirements = this.reqs.read().filter((r) => r.id || r.description);
    const ids = new Set(requirements.map((r) => r.id));
    const acs = this.acs.read().filter((a) => a.id || a.criterion).map((a) => a.requirements ? { ...a, requirements: a.requirements.filter((id) => ids.has(id)) } : a);
    return {
      ...this.base,
      id: v("id"),
      title: v("title"),
      status: v("status") || void 0,
      objective: v("objective"),
      context: v("context") || void 0,
      constraints: some(lines(this.f.constraints.value)),
      toolchains: some(commaList(this.f.toolchains.value)),
      groups: some(this.groups.read().filter((g) => g.id || g.title)),
      requirements,
      acceptance_criteria: acs,
      decisions: some(this.decisions.read().filter((d) => d.id || d.decision)),
      out_of_scope: some(lines(this.f.out_of_scope.value))
    };
  }
};
var FrdEditor = class {
  constructor(host, slug, deps) {
    this.host = host;
    this.slug = slug;
    this.deps = deps;
    this.efforts = [];
    this.revisions = [];
    this.latest = null;
    this.viewing = null;
    this.form = null;
    this.dirty = false;
    this.loaded = false;
    this.off = null;
    this.createErrors = new FormErrors();
    this.stopPoll = null;
    // Review mode shows the FRD as a card; editing swaps in the structured editor.
    this.editing = false;
    this.desc = "";
    /** The description last sent for drafting; it is cleared from the box when the draft finishes unchanged. */
    this.sentDesc = "";
    /** The describe box lives across renders so a refresh never rebuilds it under the cursor. */
    this.box = null;
    this.draftBtn = null;
    this.root = null;
    this.top = h("div", {});
    this.slot = h("div", {});
    this.slotDyn = h("div", {});
    this.slotFor = 0;
    this.bottom = h("div", {});
    this.draft = null;
    this.draftError = "";
    this.dod = null;
    this.size = null;
    this.parts = [];
    this.partErrors = /* @__PURE__ */ new Map();
    this.splitError = "";
    this.disposed = false;
    this.ticker = null;
    this.clocks = [];
    this.auto = null;
    this.revHost = h("div");
    this.hintEl = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (!this.dirty && (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "project.update"))) void this.load();
    });
    this.stopPoll = startPoll(() => !this.dirty && (isTransient(this.current?.status) || this.draft?.state === "running"), () => this.load());
    void this.load();
    return () => {
      this.disposed = true;
      if (this.ticker) clearInterval(this.ticker);
      this.ticker = null;
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
    };
  }
  async load() {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.current = chooseEffort(this.slug, this.efforts);
    await this.loadDoc();
  }
  async loadDoc() {
    this.latest = null;
    this.viewing = null;
    this.revisions = [];
    this.dirty = false;
    const ef = this.current;
    if (ef) {
      try {
        this.latest = await this.deps.frd(this.slug, ef.id, { quiet: true });
        this.revisions = await this.deps.frdRevisions(this.slug, ef.id);
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404)) void alertDialog2(err instanceof Error ? err.message : String(err));
      }
      const q = { quiet: true };
      const none = () => null;
      const [draft, dod, size2, parts] = await Promise.all([
        this.deps.draftState(this.slug, ef.id, q).catch(none),
        this.deps.definitionOfDone(this.slug, q).catch(none),
        this.latest ? this.deps.frdSize(this.slug, ef.id, q).catch(none) : null,
        this.deps.frdParts(this.slug, ef.id, q).catch(none)
      ]);
      if (this.disposed) return;
      if (this.draft?.state === "running" && draft?.state === "idle" && this.draft.purpose !== "split" && this.draft.purpose !== "polish" && this.desc.trim() === this.sentDesc) this.desc = "";
      this.draft = draft;
      this.draftError = "";
      this.dod = dod;
      this.size = size2;
      this.parts = parts ?? [];
      this.form = new FrdForm(this.latest?.json ?? blankFRD(this.slug, ef), () => {
        this.dirty = true;
        this.auto?.touch();
      });
      this.armAuto();
    } else {
      this.form = null;
      this.auto?.stop();
    }
    if (!this.latest) this.editing = false;
    this.render();
  }
  async startDraft(polish) {
    const ef = this.current;
    const description = polish ? "" : this.desc.trim();
    if (!ef || !polish && description === "") return;
    this.draftError = "";
    try {
      await this.deps.draftFRD(this.slug, ef.id, description, polish, { quiet: true });
    } catch (err) {
      this.draftError = err instanceof Error ? err.message : String(err);
      this.render();
      return;
    }
    this.sentDesc = description;
    this.draft = { state: "running", purpose: polish ? "polish" : "draft", started: Date.now() };
    this.render();
  }
  async split() {
    const ef = this.current;
    if (!ef) return;
    this.splitError = "";
    try {
      await this.deps.splitFRD(this.slug, ef.id, { quiet: true });
    } catch (err) {
      this.splitError = err instanceof Error ? err.message : String(err);
      this.render();
      return;
    }
    this.draft = { state: "running", purpose: "split", started: Date.now() };
    this.render();
  }
  async startPart(n) {
    const ef = this.current;
    if (!ef || this.efforts.some((e) => !isFinished(e))) return;
    this.partErrors.delete(n);
    try {
      const started = await this.deps.startPart(this.slug, ef.id, n, { quiet: true });
      if (started?.id) rememberEffort(this.slug, started.id);
    } catch (err) {
      this.partErrors.set(n, err instanceof Error ? err.message : String(err));
      this.render();
      return;
    }
    this.deps.notify(`Started part ${n}`);
    await this.load();
  }
  async create(title, deliberate, tokens, minutes) {
    this.createErrors.clear();
    try {
      const ef = await this.deps.createEffort(this.slug, title.trim(), deliberate, { quiet: true }, {
        budget_tokens: Math.max(0, Math.round(Number(tokens) || 0)),
        budget_seconds: Math.max(0, Math.round((Number(minutes) || 0) * 60))
      });
      rememberEffort(this.slug, ef.id);
    } catch (err) {
      this.createErrors.show(err);
      return;
    }
    this.deps.notify("Effort created");
    await this.load();
  }
  /** armAuto starts watching the form that was just built; a pause after a change stores a new revision. */
  armAuto(startDirty = false) {
    this.auto?.stop();
    this.auto = autosave({ snapshot: () => JSON.stringify(this.form?.read() ?? null), save: () => this.save(), startDirty });
    if (this.form) this.auto.watch(this.form.el);
    if (startDirty) this.auto.touch();
  }
  /** save stores the form as a new revision and stays in the editor, so typing is not interrupted. */
  async save() {
    const ef = this.current;
    if (!ef || !this.form) return false;
    this.form.errors.clear();
    try {
      const saved = await this.deps.putFRD(this.slug, ef.id, this.form.read(), { quiet: true });
      this.latest = { ...this.latest ?? { created_by: "", created: 0, approved: false }, ...saved };
      this.revisions = await this.deps.frdRevisions(this.slug, ef.id);
    } catch (err) {
      this.form.errors.show(err);
      return false;
    }
    this.dirty = false;
    replace(this.revHost, this.revisionList());
    if (this.hintEl) this.hintEl.textContent = `Latest revision ${this.latest?.revision ?? ""}; not approved`;
    return true;
  }
  /** closeEditor saves a pending change, then shows the saved revision for review and approval. */
  async closeEditor() {
    await this.auto?.flush();
    this.editing = false;
    await this.loadDoc();
  }
  async approve() {
    const ef = this.current;
    if (!ef || !this.latest) return;
    try {
      await this.deps.approveFRD(this.slug, ef.id, this.latest.revision);
    } catch {
      return;
    }
    this.deps.notify(`Approved revision ${this.latest.revision}`);
    await this.load();
  }
  async view(n) {
    const ef = this.current;
    if (!ef) return;
    try {
      this.viewing = await this.deps.frdRevision(this.slug, ef.id, n);
    } catch {
      return;
    }
    this.render();
  }
  loadIntoEditor(doc) {
    this.form = new FrdForm(doc, () => {
      this.dirty = true;
      this.auto?.touch();
    });
    this.armAuto(true);
    this.dirty = true;
    this.editing = true;
    this.viewing = null;
    this.render();
  }
  newEffortForm() {
    const title = textInput("effort_title", "", { placeholder: "What this effort delivers" });
    const deliberate = checkbox("deliberate", false);
    const tokens = textInput("budget_tokens", "", { type: "number", placeholder: "no limit" });
    const minutes = textInput("budget_minutes", "", { type: "number", placeholder: "no limit" });
    this.createErrors = new FormErrors();
    return section(
      "New effort",
      this.createErrors.add("title", fieldRow("Title", title)).el,
      fieldRow("Deliberate planning", deliberate, "Adds a pre-mortem and test plan to the planner prompt; after the FRD is written you are told when it mentions auth, migration or personal data").el,
      this.createErrors.add("budget_tokens", fieldRow("Token budget", tokens, "Optional; blank is no limit")).el,
      this.createErrors.add("budget_seconds", fieldRow("Time budget (minutes)", minutes, "Optional; blank is no limit")).el,
      this.createErrors.general,
      h("div", { class: "settings-actions" }, button("Create effort", () => void this.create(title.value, deliberate.checked, tokens.value, minutes.value), { primary: true, field: "create-effort", icon: "add", word: true }))
    );
  }
  list() {
    return h("ul", { class: "task-list", attrs: { "data-role": "frd-list" } }, ...this.efforts.map((e) => h(
      "li",
      { class: "task-item" + (e.id === this.current?.id ? " selected" : ""), attrs: { "data-effort": e.id }, on: {
        click: () => {
          rememberEffort(this.slug, e.id);
          this.current = e;
          void this.loadDoc();
        }
      } },
      h("div", { class: "task-item-title", text: e.title }),
      h(
        "div",
        { class: "task-item-meta" },
        chip(e.status),
        h("span", { text: e.frd.latest ? `FRD revision ${e.frd.latest}${e.frd.approved ? `, approved ${e.frd.approved}` : ", not approved"}` : "no FRD yet" })
      )
    )));
  }
  revisionList() {
    const rows = this.revisions.map((r) => h(
      "div",
      { class: "inline-row", attrs: { "data-revision": r.revision } },
      h("span", { text: `Revision ${r.revision} \xB7 ${r.created_by} \xB7 ${when2(r.created)}` }),
      r.approved ? badge("approved", "ok") : null,
      button("View", () => void this.view(r.revision), { field: "view-revision", icon: "view" })
    ));
    const viewing = this.viewing;
    return section(
      "Revisions",
      rows.length ? h("div", { class: "wizard-list" }, ...rows) : h("p", { class: "field-hint", text: "No revisions yet." }),
      viewing ? h(
        "div",
        { class: "detail-section", attrs: { "data-role": "revision-view" } },
        h("h4", { text: `Revision ${viewing.revision}${viewing.approved ? " (approved)" : ""}` }),
        h("pre", { class: "detail-mono", text: JSON.stringify(viewing.json, null, 2) }),
        h(
          "div",
          { class: "row-actions" },
          button("Load into editor", () => viewing.json && this.loadIntoEditor(viewing.json), { field: "load-revision", icon: "import", word: true }),
          button("Close", () => {
            this.viewing = null;
            this.render();
          }, { field: "close-revision", icon: "cancel" })
        )
      ) : null
    );
  }
  /** strip is the progress strip of a running draft or split: label, elapsed time and an animated bar. */
  strip(label, since) {
    const elapsed = h("span", { class: "field-hint", text: since > 0 ? elapsedText(Date.now() - since) : "", attrs: { "data-role": "elapsed" } });
    this.clocks.push({ el: elapsed, since });
    return h(
      "div",
      { class: "progress-strip", attrs: { "data-role": "progress-strip", role: "status" } },
      h("div", { class: "row-actions" }, h("strong", { text: label }), elapsed),
      h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar progress-indeterminate" }))
    );
  }
  /** syncTicker keeps one timer alive while any strip is on screen. */
  syncTicker() {
    if (this.clocks.length === 0 || this.disposed) {
      if (this.ticker) clearInterval(this.ticker);
      this.ticker = null;
      return;
    }
    if (!this.ticker) {
      this.ticker = setInterval(() => {
        for (const c of this.clocks) if (c.since > 0) c.el.textContent = elapsedText(Date.now() - c.since);
      }, 1e3);
    }
  }
  /** stripLabel names what the server is running, from the draft state's purpose. */
  stripLabel() {
    const p = this.draft?.purpose;
    return p === "split" ? "Splitting the FRD" : p === "polish" ? "Tidying the FRD" : "Drafting the FRD";
  }
  /** describeBody is the part of the describe section that changes: the strip, the error and the buttons. */
  describeBody() {
    const running = this.draft?.state === "running";
    const mine = this.draft?.purpose !== "split";
    const draftBtn = button("Draft the FRD", () => void this.startDraft(false), { primary: true, field: "draft-frd", icon: "sparkle" });
    this.draftBtn = draftBtn;
    if (this.desc.trim() === "") draftBtn.className += " af-disabled";
    const error = this.draftError || (this.draft?.state === "failed" && mine ? this.draft.error ?? "The draft failed" : "");
    return [
      running && mine ? this.strip(this.stripLabel(), this.draft?.started ?? 0) : null,
      error ? h("div", { class: "field-error", attrs: { role: "alert", "data-role": "draft-error" }, text: error }) : null,
      running ? null : h(
        "div",
        { class: "settings-actions" },
        this.latest ? null : button("Write it by hand", () => {
          this.editing = true;
          this.render();
        }, { field: "edit-frd", icon: "edit" }),
        draftBtn
      )
    ];
  }
  bullets(title, items) {
    return items.length ? h("div", { class: "plan-section" }, h("h4", { text: title }), h("ul", {}, ...items.map((t) => h("li", { text: t })))) : null;
  }
  /** reviewCard shows the latest FRD as a document: title, objective, requirements and criteria as plain lists. */
  reviewCard(doc, approvedRev, canApprove) {
    const rev = this.latest?.revision ?? 0;
    const running = this.draft?.state === "running";
    const open = this.current?.status === "draft" && !running;
    return h(
      "div",
      { class: "settings-section", attrs: { "data-role": "frd-review" } },
      h(
        "div",
        { class: "row-actions" },
        h("h3", { text: doc.title }),
        h("span", { class: "field-hint", text: `rev ${rev}` }),
        approvedRev === rev ? badge("approved", "ok") : badge(approvedRev ? `rev ${approvedRev} approved` : "not approved", "warn")
      ),
      doc.objective ? h("p", { text: doc.objective }) : null,
      this.bullets("Requirements", (doc.requirements ?? []).map((r) => (r.priority ? `${r.priority}: ` : "") + r.description)),
      this.bullets("Acceptance criteria", (doc.acceptance_criteria ?? []).map((a) => a.criterion)),
      this.bullets("Out of scope", doc.out_of_scope ?? []),
      running ? null : h(
        "div",
        { class: "settings-actions" },
        open ? button("Tidy the text", () => void this.startDraft(true), { field: "polish-frd", icon: "sparkle" }) : null,
        open ? button("Edit", () => {
          this.editing = true;
          this.render();
        }, { field: "edit-frd", icon: "edit" }) : null,
        canApprove ? button(`Approve revision ${rev}`, () => void this.approve(), { primary: true, field: "approve-frd", icon: "approve" }) : null
      )
    );
  }
  dodSection() {
    const items = this.dod?.items ?? [];
    if (items.length === 0) return null;
    return h(
      "div",
      { class: "detail-section", attrs: { "data-role": "definition-of-done" } },
      h("h4", { text: "Definition of done", attrs: { title: "Applies to every task, so it need not be written as acceptance criteria" } }),
      h("ul", { class: "field-hint" }, ...items.map((t) => h("li", { text: t })))
    );
  }
  /** sizeRow is the FRD's token count against the smallest planning context, with the split offer when it is over. */
  sizeRow() {
    const z = this.size;
    if (!z || !(z.budget_tokens > 0) || typeof z.over !== "boolean") {
      return h(
        "div",
        { class: "console-header", attrs: { "data-role": "size-unknown" } },
        h("span", { class: "field-hint", attrs: { title: "Set context sizes in Backends", "aria-label": "Set context sizes in Backends" } }, icon("gauge"))
      );
    }
    const pct2 = Math.min(100, Math.round(z.frd_tokens / z.budget_tokens * 100));
    const unknown = z.unknown_models ?? [];
    const unknownTip = `Context size unknown for: ${unknown.map((m) => `${m.backend}/${m.model}`).join(", ")}`;
    return h(
      "div",
      { class: "console-header", attrs: { "data-role": "size" } },
      h("span", { class: "field-hint", attrs: { title: "Size of the FRD against what the smallest planning model can read", "aria-label": "Size of the FRD against what the smallest planning model can read" } }, icon("gauge")),
      unknown.length ? h("span", { class: "field-hint", attrs: { "data-role": "unknown-models", title: unknownTip, "aria-label": unknownTip } }, icon("warn")) : null,
      h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar" + (z.over ? " progress-over" : ""), attrs: { style: `width: ${pct2}%` } })),
      h("span", { class: z.over ? "progress-pct field-error" : "progress-pct", text: `${formatTokens(z.frd_tokens)} / ${formatTokens(z.budget_tokens)} tokens`, attrs: { title: z.advice, "aria-label": z.advice } }),
      z.over && this.draft?.state !== "running" ? button("Split into smaller parts", () => void this.split(), { field: "split-frd", icon: "split" }) : null
    );
  }
  partsList() {
    if (this.parts.length === 0) return null;
    const busy = this.efforts.find((e) => !isFinished(e));
    return h("div", { class: "wizard-list", attrs: { "data-role": "frd-parts" } }, ...this.parts.map((p) => {
      const started = p.started_effort > 0;
      const pending = !started && p.n > 1;
      const start = pending ? button("Start this part", () => void this.startPart(p.n), { field: "start-part", icon: "start" }) : null;
      if (start && busy) {
        start.className += " af-disabled";
        start.setAttribute("aria-disabled", "true");
        start.setAttribute("title", `Finish or abandon #${busy.id} first`);
      }
      return h(
        "div",
        { class: "wizard-field", attrs: { "data-part": p.n } },
        h(
          "div",
          { class: "inline-row" },
          h("strong", { text: `${p.n}. ${p.title}` }),
          badge(`${formatTokens(p.tokens)} tokens`, "muted"),
          started ? badge("started", "ok") : null,
          start
        ),
        p.summary ? h("p", { class: "field-hint", text: p.summary }) : null,
        this.partErrors.get(p.n) ? h("div", { class: "field-error", attrs: { role: "alert" }, text: this.partErrors.get(p.n) }) : null
      );
    }));
  }
  /** renderDescribe keeps the describe box element across renders (so typing is never lost) and refreshes everything around it. */
  renderDescribe(ef) {
    if (!ef) {
      replace(this.slot);
      this.slotFor = 0;
      return;
    }
    if (!this.box) {
      this.box = textArea("description", this.desc, 4);
      this.box.setAttribute("placeholder", "What do you want?");
      this.box.addEventListener("input", () => {
        this.desc = this.box.value;
        const b = this.draftBtn;
        if (b) b.className = b.className.replace(" af-disabled", "") + (this.desc.trim() === "" ? " af-disabled" : "");
      });
    }
    if (this.box.value !== this.desc) this.box.value = this.desc;
    replace(this.slotDyn, ...this.describeBody());
    if (this.slotFor !== ef.id || this.slot.firstChild === null) {
      replace(this.slot, section(`FRD for #${ef.id}`, this.box, this.slotDyn));
      this.slotFor = ef.id;
    }
  }
  render() {
    if (this.disposed) return;
    if (!this.loaded) {
      this.root = null;
      replace(this.host, h("p", { class: "field-hint", text: "Loading efforts\u2026" }));
      return;
    }
    this.clocks = [];
    const ef = this.current;
    const approvedRev = this.revisions.find((r) => r.approved)?.revision;
    const canApprove = !!ef && !!this.latest && this.latest.revision !== approvedRev && !this.dirty && !isFinished(ef);
    const editor = ef && this.form ? section(
      `FRD for #${ef.id}`,
      h(
        "div",
        { class: "row-actions" },
        this.hintEl = h("span", { class: "field-hint", text: this.latest ? `Latest revision ${this.latest.revision}${approvedRev ? `; revision ${approvedRev} approved` : "; not approved"}` : "Not saved yet" })
      ),
      this.form.el,
      h(
        "div",
        { class: "settings-actions" },
        this.auto?.status ?? null,
        button("Done", () => void this.closeEditor(), { primary: true, field: "close-editor", icon: "approve" })
      )
    ) : null;
    const splitError = this.splitError || (this.draft?.state === "failed" && this.draft.purpose === "split" ? this.draft.error ?? "The split failed" : "");
    const review = ef && !this.editing && this.latest?.json ? [
      this.reviewCard(this.latest.json, approvedRev, canApprove),
      this.dodSection(),
      this.draft?.state === "running" && this.draft.purpose === "split" ? this.strip(this.stripLabel(), this.draft.started ?? 0) : this.sizeRow(),
      splitError ? h("div", { class: "field-error", attrs: { role: "alert", "data-role": "split-error" }, text: splitError }) : null,
      this.partsList()
    ] : [];
    const active2 = this.efforts.find((e) => !isFinished(e));
    const bar = this.efforts.length ? effortBar(this.slug, this.efforts, ef, (id) => {
      this.current = this.efforts.find((e) => e.id === id);
      void this.loadDoc();
    }) : null;
    if (bar && active2) {
      bar.appendChild(h("button", {
        class: "ui-modal-btn af-disabled",
        text: "+",
        attrs: {
          type: "button",
          "data-action": "new-effort",
          "aria-disabled": "true",
          "aria-label": "New effort",
          title: `New effort is unavailable: a project has one active effort at a time. Finish or abandon #${active2.id} (${active2.status.replace(/_/g, " ")}) first.`
        }
      }));
    }
    const showDescribe = !!ef && !this.editing && ef.status === "draft";
    replace(
      this.top,
      bar,
      active2 ? null : this.efforts.length ? null : h("div", { class: "plan-empty" }, h("h3", { text: "No efforts yet" }), h("p", { text: "An effort is one FRD, plan and queue. Create the first one below." })),
      active2 ? null : this.newEffortForm()
    );
    this.renderDescribe(showDescribe ? ef : void 0);
    replace(
      this.bottom,
      ...review,
      this.editing ? editor : null,
      ef && !isFinished(ef) ? effortSettings(ef, this.form ? JSON.stringify(this.form.read()) : "", this.deps, () => void this.load()) : null,
      ef ? (replace(this.revHost, this.revisionList()), this.revHost) : null,
      this.efforts.length ? section("Efforts", this.list()) : null
    );
    if (!this.root || this.host.firstChild !== this.root) {
      this.root = h("div", { class: "settings-host" }, this.top, this.slot, this.bottom);
      replace(this.host, this.root);
    }
    this.syncTicker();
  }
};
function mountFrd(host, slug) {
  return new FrdEditor(host, slug, defaultFrdDeps()).mount();
}

// web/agentflow/js/queue.ts
import { confirmDialog as confirmDialog7, showToast as showToast10 } from "/shared/dist/shared.mjs";
var defaultQueueDeps = () => ({
  efforts: api.efforts,
  queue: api.queue,
  compile: api.compile,
  approveQueue: api.approveQueue,
  confirm: (m, label = "Confirm") => confirmDialog7(m, { confirmLabel: label }),
  notify: (m) => void showToast10(m, "success")
});
var list2 = (title, items) => h(
  "div",
  { class: "detail-section" },
  h("h4", { text: title }),
  items?.length ? h("ul", { class: "plan-review-notes" }, ...items.map((i) => h("li", { text: i }))) : h("p", { class: "field-hint", text: "none" })
);
var QueueApproval = class {
  constructor(host, slug, deps) {
    this.host = host;
    this.slug = slug;
    this.deps = deps;
    this.efforts = [];
    this.queue = null;
    this.loaded = false;
    this.errors = new FormErrors();
    this.off = null;
    this.stopPoll = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "task.update" || ev.type === "project.update")) void this.refresh();
    });
    this.stopPoll = startPoll(() => isTransient(this.current?.status), () => this.refresh());
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
      this.stopPoll?.();
      this.stopPoll = null;
    };
  }
  async load() {
    try {
      this.efforts = await this.deps.efforts(this.slug);
    } catch {
      if (!this.loaded) return;
    }
    this.loaded = true;
    this.current = chooseEffort(this.slug, this.efforts);
    await this.loadQueue();
  }
  async refresh() {
    try {
      this.efforts = await this.deps.efforts(this.slug);
      this.current = this.efforts.find((e) => e.id === this.current?.id) ?? chooseEffort(this.slug, this.efforts);
    } catch {
      return;
    }
    await this.loadQueue();
  }
  async loadQueue() {
    const ef = this.current;
    this.queue = null;
    if (ef) {
      try {
        this.queue = await this.deps.queue(this.slug, ef.id, { quiet: true });
      } catch (err) {
        if (!(err instanceof ApiError && (err.status === 404 || err.status === 409))) this.errors.show(err);
      }
    }
    this.render();
  }
  async compile() {
    const ef = this.current;
    if (!ef) return;
    try {
      await this.deps.compile(this.slug, ef.id);
    } catch {
      return;
    }
    this.deps.notify("Compiling the plan into tasks");
    await this.refresh();
  }
  /** approve sends the hash of the queue on screen; a 409 means it changed, so it reloads for another review. */
  async approve() {
    const ef = this.current;
    const q = this.queue;
    if (!ef || !q) return;
    if (!await this.deps.confirm(`Approve all ${q.tasks.length} task${q.tasks.length === 1 ? "" : "s"} of revision ${q.revision}? Once approved the queue can run.`, "Approve queue")) return;
    this.errors.clear();
    try {
      await this.deps.approveQueue(this.slug, ef.id, q.hash, { quiet: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        this.deps.notify("The queue changed since you opened it; review it again");
        await this.refresh();
        this.errors.show(new Error(`The queue changed while you were reviewing it, so nothing was approved. (${err.message})`));
        return;
      }
      this.errors.show(err);
      return;
    }
    this.deps.notify("Queue approved; start the run from the Board tab");
    await this.refresh();
  }
  taskCard(t) {
    return h(
      "div",
      { class: "profile-card", attrs: { "data-task": t.key } },
      h("div", { class: "profile-header" }, h("strong", { text: `${t.key} \xB7 ${t.title}` }), t.phase ? badge(t.phase, "muted") : null),
      t.waiting_reason ? h("p", { class: "field-hint", text: t.waiting_reason }) : null,
      h("div", { class: "detail-section" }, h("h4", { text: "Prompt" }), h("pre", { class: "detail-mono", text: t.prompt })),
      list2("Acceptance criteria", t.acceptance),
      list2("Files touched", t.files_touched),
      list2("Depends on", t.depends_on)
    );
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading efforts\u2026" }));
      return;
    }
    const ef = this.current;
    if (!ef) {
      replace(this.host, h("div", { class: "plan-empty" }, h("h3", { text: "No effort yet" }), h("p", { text: "Create an effort on the FRD tab first." })));
      return;
    }
    const q = this.queue;
    const canCompile = ef.status === "queue_draft" && !!q;
    const compiling = ef.status === "compiling";
    const notStarted = ef.status === "plan_approved";
    const draft = !!q && q.status === "draft";
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host" },
        effortBar(this.slug, this.efforts, ef, (id) => {
          this.current = this.efforts.find((e) => e.id === id);
          void this.loadQueue();
        }),
        q ? h(
          "div",
          { class: "plan-header", attrs: { "data-role": "queue-header" } },
          h(
            "div",
            { class: "plan-meta" },
            chip(q.status),
            h("span", { class: "plan-frd-id", text: `revision ${q.revision} \xB7 ${q.tasks.length} task${q.tasks.length === 1 ? "" : "s"}` }),
            h("code", { class: "plan-frd-id", text: `hash ${q.hash.slice(0, 12)}`, attrs: { "data-role": "queue-hash" } }),
            q.approved_by ? badge(`approved by ${q.approved_by}`, "ok") : null
          ),
          h(
            "div",
            { class: "plan-actions" },
            canCompile ? button("Recompile", () => void this.compile(), { field: "compile", icon: "refresh" }) : null,
            draft ? button("Approve whole queue", () => void this.approve(), { primary: true, field: "approve-queue", icon: "approve" }) : null
          )
        ) : h(
          "div",
          { class: "plan-empty" },
          h("h3", { text: "No queue yet" }),
          h("p", { attrs: { "data-role": "queue-note" }, text: compiling ? "Compiling the plan into tasks..." : notStarted ? "The plan is approved but its compile has not started or has stopped. Press Compile." : "Approve a plan first; it is compiled into tasks automatically." }),
          notStarted ? button("Compile", () => void this.compile(), { field: "compile", icon: "refresh" }) : null
        ),
        this.errors.general,
        q ? section("Tasks", h("div", { class: "profile-list" }, ...q.tasks.map((t) => this.taskCard(t)))) : null
      )
    );
  }
};
function mountQueue(host, slug) {
  return new QueueApproval(host, slug, defaultQueueDeps()).mount();
}

// web/agentflow/js/projects.ts
import { confirmDialog as confirmDialog8, showToast as showToast11 } from "/shared/dist/shared.mjs";
var defaultProjectDeps = () => ({
  createProject: api.createProject,
  patchProject: api.patchProject,
  gitInit: api.gitInit,
  deleteProject: api.deleteProject,
  backends: api.backends,
  candidates: api.candidates,
  putProjectCandidates: api.putProjectCandidates,
  clearProjectCandidates: api.clearProjectCandidates,
  definitionOfDone: api.definitionOfDone,
  confirm: (m, label = "Confirm") => confirmDialog8(m, { confirmLabel: label }),
  pickDir: (start) => pickDirectory(start),
  notify: (m) => void showToast11(m, "success"),
  showCommands: defaultUnregisterDeps().showCommands
});
function splitArgv(s) {
  return s.split(/\s+/).filter((t) => t !== "");
}
function joinArgv(argv) {
  return argv.join(" ");
}
var SHELL_TOKENS = /* @__PURE__ */ new Set(["|", "||", "&", "&&", ";", ">", ">>", "<", "2>&1"]);
function shellOperators(argv) {
  return argv.filter((t) => SHELL_TOKENS.has(t) || t.includes("$(") || t.includes("`"));
}
function isNotARepo(err) {
  return err instanceof ApiError && err.field === "repo_path" && /not a git repository/.test(err.message);
}
var SCOPES = [["local", "local (only local backends)"], ["any", "any (hosted backends allowed)"]];
var TOOLCHAINS = [["dotnet", ".NET SDK and the host NuGet cache, read-only and offline"]];
var ProjectForm = class {
  constructor(host, deps, mode, done) {
    this.host = host;
    this.deps = deps;
    this.mode = mode;
    this.done = done;
    this.errors = new FormErrors();
    this.toolchains = /* @__PURE__ */ new Map();
    this.backends = [];
    this.cands = [];
    this.drafts = /* @__PURE__ */ new Map();
    this.rolesHost = h("div");
    /** Definition of done: the detected list, and the override rows once the project has or starts one. */
    this.dodDetected = [];
    this.dodRows = null;
    this.dodReset = false;
    this.dodHost = h("div", { attrs: { "data-role": "dod-editor" } });
    /** An existing project saves its edits by itself; creating one is one explicit step. */
    this.auto = null;
    this.bump = () => this.auto?.touch();
    const p = mode.project;
    this.slug = textInput("slug", p?.slug ?? "", { readOnly: !!p, placeholder: "my-project" });
    this.repoPath = p?.repo_path ?? "";
    this.repoBtn = h("button", {
      class: "ui-modal-btn repo-pick",
      attrs: { type: "button", "data-field": "repo_path", title: "Choose the repository folder" },
      on: { click: () => void this.pickRepo() }
    });
    this.drawRepo();
    this.scope = selectInput("model_scope", SCOPES, p?.model_scope ?? "local");
    this.cleanup = checkbox("cleanup_enabled", p?.cleanup_enabled ?? false);
    for (const [name] of TOOLCHAINS) this.toolchains.set(name, checkbox(`toolchain_${name}`, (p?.toolchains ?? []).includes(name)));
    this.verifyInCopy = checkbox("verify_in_copy", p?.verify_in_copy ?? false);
    this.cmds = new RowsEditor((c) => {
      const input = textInput("feedback_cmd", c, { placeholder: "go test ./..." });
      const preview = h("code", { class: "field-hint", attrs: { "data-role": "argv" } });
      const warn = h("div", { class: "field-error", attrs: { "data-role": "shell-warning", hidden: true } });
      const sync = () => {
        const argv = splitArgv(input.value);
        preview.textContent = argv.length ? `argv: ${JSON.stringify(argv)}` : "";
        const ops = shellOperators(argv);
        warn.textContent = ops.length ? `No shell is used: ${ops.join(" ")} would be passed as a plain argument` : "";
        if (ops.length) warn.removeAttribute("hidden");
        else warn.setAttribute("hidden", "");
      };
      input.addEventListener("input", sync);
      sync();
      return { el: h("div", { class: "wizard-field" }, input, preview, warn), read: () => joinArgv(splitArgv(input.value)) };
    }, p?.feedback_cmds ?? [], () => "", "Add command", () => this.bump());
    this.env = new RowsEditor((row) => {
      const key = textInput("feedback_env_key", row.key, { placeholder: "CIV3_PATH" });
      const value = textInput("feedback_env_value", row.value, { placeholder: "/path/to/civ3" });
      return { el: h("div", { class: "row-actions" }, key, value), read: () => ({ key: key.value.trim(), value: value.value }) };
    }, Object.entries(p?.feedback_env ?? {}).map(([key, value]) => ({ key, value })), () => ({ key: "", value: "" }), "Add variable", () => this.bump());
    this.plugins = new RowsEditor((pl) => {
      const name = textInput("plugin_name", pl.name, { placeholder: "package-name" });
      const version = textInput("plugin_version", pl.version, { placeholder: "1.2.3" });
      return { el: h("div", { class: "row-actions" }, name, version), read: () => ({ name: name.value.trim(), version: version.value.trim() }) };
    }, p?.plugins ?? [], () => ({ name: "", version: "" }), "Add plugin", () => this.bump());
  }
  drawRepo() {
    this.repoBtn.textContent = this.repoPath || "Choose a folder...";
  }
  /** pickRepo lets the user choose the repository folder on the server. */
  async pickRepo() {
    const chosen = await this.deps.pickDir(this.repoPath);
    if (chosen === null) return;
    this.repoPath = chosen;
    this.drawRepo();
    this.bump();
  }
  async mount() {
    if (this.mode.project) {
      try {
        [this.backends, this.cands] = await Promise.all([this.deps.backends(), this.deps.candidates()]);
      } catch {
      }
      try {
        const dod = await this.deps.definitionOfDone(this.mode.project.slug, { quiet: true });
        if (dod.source === "project") this.startDod(dod.items);
        else this.dodDetected = dod.items;
      } catch {
      }
    }
    this.render();
  }
  body() {
    return {
      slug: this.slug.value.trim(),
      repo_path: this.repoPath,
      model_scope: this.scope.value,
      feedback_cmds: this.cmds.read().filter((c) => c !== ""),
      cleanup_enabled: this.cleanup.checked,
      plugins: this.plugins.read().filter((pl) => pl.name !== "" || pl.version !== ""),
      toolchains: TOOLCHAINS.map(([name]) => name).filter((name) => this.toolchains.get(name)?.checked),
      feedback_env: Object.fromEntries(this.env.read().filter((r) => r.key !== "").map((r) => [r.key, r.value])),
      verify_in_copy: this.verifyInCopy.checked
    };
  }
  /** save creates or updates the project; a repo path that is not yet a repository offers git init (PJ1). */
  async save() {
    this.errors.clear();
    const body = this.body();
    const attempt = () => this.mode.project ? this.deps.patchProject(this.mode.project.slug, { repo_path: body.repo_path, model_scope: body.model_scope, feedback_cmds: body.feedback_cmds, cleanup_enabled: body.cleanup_enabled, plugins: body.plugins, toolchains: body.toolchains, feedback_env: body.feedback_env, verify_in_copy: body.verify_in_copy, ...this.dodPatch() }, { quiet: true }) : this.deps.createProject(body, { quiet: true });
    let saved;
    try {
      saved = await attempt();
    } catch (err) {
      if (!isNotARepo(err)) {
        this.errors.show(err);
        return;
      }
      if (!await this.deps.confirm(`${body.repo_path} is not a git repository. Run "git init" there? Agentflow needs a repository to work in.`, "Run git init")) {
        this.errors.show(err);
        return;
      }
      try {
        await this.deps.gitInit(body.repo_path);
        saved = await attempt();
      } catch (err2) {
        this.errors.show(err2);
        return;
      }
    }
    this.deps.notify(this.mode.project ? `Saved ${saved.slug}` : `Created ${saved.slug}`);
    this.done(saved.slug);
  }
  /** autoSave stores an edit of an existing project: no prompt, no navigation, and the form stays as it is. */
  async autoSave() {
    const p = this.mode.project;
    if (!p) return false;
    this.errors.clear();
    const b = this.body();
    try {
      await this.deps.patchProject(p.slug, { repo_path: b.repo_path, model_scope: b.model_scope, feedback_cmds: b.feedback_cmds, cleanup_enabled: b.cleanup_enabled, plugins: b.plugins, toolchains: b.toolchains, feedback_env: b.feedback_env, verify_in_copy: b.verify_in_copy, ...this.dodPatch() }, { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return false;
    }
    this.dodReset = false;
    return true;
  }
  async remove() {
    const p = this.mode.project;
    if (!p || unregisterBlock(p)) return;
    const showCommands = this.deps.showCommands ?? (() => {
    });
    if (await unregister({ ...this.deps, showCommands }, p.slug)) this.done("");
  }
  // ─── Definition of done: detected from the toolchain, optionally overridden per project ───
  startDod(items) {
    this.dodRows = new RowsEditor((t) => {
      const input = textInput("dod_item", t);
      return { el: input, read: () => input.value.trim() };
    }, items, () => "", "Add item", () => this.bump());
    this.dodReset = false;
  }
  /** dodPatch is the patch fields for the list: the override rows, null to go back to detected, nothing when untouched. */
  dodPatch() {
    if (this.dodReset) return { definition_of_done: null };
    return this.dodRows ? { definition_of_done: this.dodRows.read().filter((t) => t !== "") } : {};
  }
  drawDod() {
    if (!this.mode.project) return;
    replace(
      this.dodHost,
      this.dodRows ? h("div", {}, this.dodRows.el, button("Reset to detected", () => {
        this.dodRows = null;
        this.dodReset = true;
        this.drawDod();
        this.bump();
      }, { field: "reset-dod", icon: "retry" })) : h(
        "div",
        {},
        h("ul", { class: "field-hint" }, ...this.dodDetected.map((t) => h("li", { text: t }))),
        button("Override for this project", () => {
          this.startDod(this.dodDetected);
          this.drawDod();
        }, { field: "override-dod", icon: "edit" })
      )
    );
  }
  // ─── Role preferences: a per-project candidate list overriding the global one ───
  projectRows(role) {
    const slug = this.mode.project?.slug;
    return this.cands.filter((c) => c.scope === "project" && c.project === slug && c.role === role).sort((a, b) => a.position - b.position).map((c) => ({ backend: c.backend, model: c.model, optIn: true }));
  }
  drawRoles() {
    const p = this.mode.project;
    if (!p) return;
    replace(this.rolesHost, ...ROLES.map((role) => {
      const overridden = this.projectRows(role).length > 0;
      const global = this.cands.filter((c) => c.scope === "global" && c.role === role).sort((a, b) => a.position - b.position);
      return h(
        "div",
        {},
        buildRoleEditor({
          role,
          backends: this.backends,
          rows: () => this.drafts.get(role) ?? this.projectRows(role),
          hasDraft: () => this.drafts.has(role),
          setDraft: (rows) => {
            if (rows) this.drafts.set(role, rows);
            else this.drafts.delete(role);
          },
          save: async (body) => {
            this.cands = await this.deps.putProjectCandidates(p.slug, role, body, { quiet: true });
            this.deps.notify(`Saved ${role} override for ${p.slug}`);
          },
          emptyNote: global.length ? `Uses the global list: ${global.map((c) => `${c.backend}/${c.model}`).join(", ")}` : "Uses the global list, which is empty.",
          extra: overridden ? [button("Use global list", () => void this.clearRole(role), { field: "clear-override", icon: "retry", word: true })] : []
        })
      );
    }));
  }
  async clearRole(role) {
    const p = this.mode.project;
    if (!p) return;
    try {
      await this.deps.clearProjectCandidates(p.slug, role);
      this.cands = await this.deps.candidates();
    } catch {
      return;
    }
    this.drafts.delete(role);
    this.drawRoles();
  }
  unregisterButton(p) {
    const why = unregisterBlock(p);
    const b = button("Unregister", () => void this.remove(), { field: "delete-project", icon: "trash", word: true });
    if (why) {
      b.disabled = true;
      b.setAttribute("title", why);
      b.setAttribute("aria-label", `Unregister: ${why}`);
    }
    return b;
  }
  /** drawToolchains is one checkbox per known toolchain, with what it mounts. */
  drawToolchains() {
    return h("div", {}, ...TOOLCHAINS.map(([name, hint]) => {
      const box = this.toolchains.get(name);
      return h("div", { class: "field-hint" }, box ?? h("span"), " " + hint);
    }));
  }
  render() {
    const p = this.mode.project;
    const e = this.errors;
    this.drawRoles();
    this.drawDod();
    this.auto?.stop();
    this.auto = p ? autosave({ snapshot: () => JSON.stringify([this.body(), this.dodPatch()]), save: () => this.autoSave() }) : null;
    const root = h(
      "div",
      { class: "settings-host", attrs: { "data-form": p ? "project-edit" : "project-create" } },
      section(
        p ? `Edit ${p.slug}` : "New project",
        h(
          "div",
          { class: "profile-fields" },
          e.add("slug", fieldRow("Slug", this.slug, "Lowercase letters, digits and dashes; fixed once created")).el,
          e.add("repo_path", fieldRow("Repository", this.repoBtn, "A git repository on this machine; if the folder is not one you are offered git init")).el,
          e.add("model_scope", fieldRow("Model scope", this.scope, "local keeps this project off hosted backends")).el,
          e.add("cleanup_enabled", fieldRow(
            "Cleanup pass",
            this.cleanup,
            "After a task passes review, run a cleanup agent and verify again; if that breaks verification the cleanup is rolled back"
          )).el
        ),
        e.add("feedback_env", fieldRow(
          "Feedback environment",
          this.env.el,
          "Set for every feedback command, for values such as an install path; never a secret"
        )).el,
        e.add("toolchains", fieldRow(
          "Toolchains",
          this.drawToolchains(),
          "Mounted read-only in every sandbox for this project; dotnet brings the SDK and the host NuGet cache, so build and test run offline"
        )).el,
        e.add("verify_in_copy", fieldRow(
          "Verify in a copy",
          this.verifyInCopy,
          "Run feedback commands on a throwaway copy of the worktree, without .git, bin or obj; needed when the toolchain writes build output"
        )).el,
        e.add("feedback_cmds", fieldRow(
          "Feedback commands",
          this.cmds.el,
          "Run in the sandbox to verify each task, as a program and arguments, never through a shell. Blank: detected from go.mod, pyproject.toml or package.json"
        )).el,
        p ? e.add("definition_of_done", fieldRow("Definition of done", this.dodHost, "Applies to every task; need not be written as acceptance criteria")).el : null,
        e.add("plugins", fieldRow(
          "Runner plugins",
          this.plugins.el,
          "Opt-in only: none are loaded unless listed here. Each is pinned to an exact version such as 1.2.3 (no latest, no ranges); listed plugins are fetched by the runner when a stage starts"
        )).el,
        e.general,
        h(
          "div",
          { class: "settings-actions" },
          p ? this.unregisterButton(p) : null,
          this.auto?.status ?? null,
          p ? null : button("Cancel", () => this.done(""), { field: "cancel", icon: "cancel" }),
          p ? button("Done", () => void (this.auto?.flush() ?? Promise.resolve()).then(() => this.done(p.slug)), { primary: true, field: "close", icon: "approve" }) : button("Create project", () => void this.save(), { primary: true, field: "save-project", icon: "approve" })
        )
      ),
      p ? section(
        "Role preferences",
        h("p", { class: "field-hint", text: "Override the global candidate list for one role in this project; a role without an override uses the global list." }),
        this.rolesHost
      ) : h("p", { class: "field-hint", text: "Role preferences can be set once the project exists." })
    );
    replace(this.host, root);
    this.auto?.watch(root);
  }
};
function showProjectForm(host, mode, done) {
  void new ProjectForm(host, defaultProjectDeps(), mode, done).mount();
}

// web/agentflow/js/projecttab.ts
var SUB_TABS = [
  { id: "frd", label: "FRD", mount: mountFrd },
  { id: "plan", label: "Plan", mount: mountPlan },
  { id: "queue", label: "Queue", mount: mountQueue },
  { id: "board", label: "Board", mount: mountBoard },
  { id: "history", label: "History", mount: mountHistory }
];
var KEY = "agentflow:project";
function remembered() {
  try {
    return localStorage.getItem(KEY) ?? "";
  } catch {
    return "";
  }
}
function remember(slug) {
  try {
    localStorage.setItem(KEY, slug);
  } catch {
  }
}
function mountProjectTab(host, wanted2) {
  let dispose2;
  let disposed = false;
  let sub = SUB_TABS[0].id;
  let slug = wanted2 === "+new" ? remembered() : wanted2 || remembered();
  let projects = [];
  let blocked = /* @__PURE__ */ new Map();
  const picker = h("select", { class: "settings-input", attrs: { "aria-label": "Project" } });
  const bar = h("div", { class: "tab-bar", attrs: { role: "tablist" } });
  const body = h("div", { class: "project-body" });
  const activityEl = h("div", { class: "activity-strip", attrs: { role: "status", hidden: true, "data-role": "activity" } });
  const activity = new Activity(activityEl, (id) => {
    if (!blocked.has(id)) {
      sub = id;
      showSub();
    }
  });
  const stopActivity = activity.mount("");
  const closeSub = () => {
    if (typeof dispose2 === "function") dispose2();
    dispose2 = void 0;
  };
  const drawBar = () => {
    replace(bar, ...SUB_TABS.map((t) => {
      const why = blocked.get(t.id) ?? "";
      return h("button", {
        class: "tab-btn" + (t.id === sub ? " active" : ""),
        text: t.label,
        attrs: { type: "button", "data-sub": t.id, disabled: why !== "", title: why || void 0, "aria-disabled": why !== "" ? "true" : void 0 },
        on: { click: () => {
          if (blocked.has(t.id)) return;
          sub = t.id;
          showSub();
        } }
      });
    }));
  };
  const readBlocks = async () => {
    let efforts = [];
    if (slug) {
      try {
        efforts = await api.efforts(slug);
      } catch {
        efforts = [];
      }
    }
    const ef = chooseEffort(slug, efforts);
    blocked = new Map(SUB_TABS.map((t) => [t.id, subTabBlock(t.id, ef)]).filter(([, why]) => why !== ""));
  };
  const showSub = () => {
    closeSub();
    drawBar();
    if (!slug) {
      replace(body, h("p", { class: "field-hint", text: "Pick a project, or create one with New project." }));
      return;
    }
    if (blocked.has(sub)) {
      sub = SUB_TABS[0].id;
      drawBar();
    }
    const tab = SUB_TABS.find((t) => t.id === sub) ?? SUB_TABS[0];
    replace(body);
    dispose2 = tab.mount(body, slug);
  };
  const reload = async () => {
    try {
      projects = await api.projects();
    } catch {
      projects = [];
    }
    if (disposed) return;
    if (!projects.some((p) => p.slug === slug)) {
      slug = projects[0]?.slug ?? "";
      remember(slug);
    }
    const active2 = (p) => p.run_state === "running" || p.run_state === "pausing";
    replace(picker, ...projects.map((p) => h("option", { text: active2(p) ? `${p.slug}  \u25CF running` : p.slug, attrs: { value: p.slug, selected: p.slug === slug } })));
    picker.value = slug;
    activity.setProject(slug);
    await readBlocks();
    if (disposed) return;
    showSub();
  };
  const showForm = (project) => {
    closeSub();
    replace(bar);
    showProjectForm(body, { project }, (saved) => {
      if (saved) {
        slug = saved;
        remember(saved);
      }
      void reload();
    });
  };
  picker.addEventListener("change", () => {
    slug = picker.value;
    remember(slug);
    activity.setProject(slug);
    void readBlocks().then(() => {
      if (!disposed) showSub();
    });
  });
  replace(
    host,
    h(
      "div",
      { class: "console-header" },
      h("span", { class: "pane-title", text: "Project" }),
      picker,
      button("New project", () => showForm(), { field: "new-project", icon: "add" }),
      button("Edit project", () => {
        const cur = projects.find((p) => p.slug === slug);
        if (cur) showForm(cur);
      }, { field: "edit-project", icon: "edit" })
    ),
    activityEl,
    bar,
    body
  );
  const off = subscribe((ev) => {
    if (!slug || !["effort.update", "project.update", "state.changed", "task.update"].includes(ev.type)) return;
    if ("project" in ev && ev.project !== slug) return;
    void readBlocks().then(() => {
      if (disposed || !bar.firstChild) return;
      if (blocked.has(sub)) showSub();
      else drawBar();
    });
  });
  if (wanted2 === "+new") showForm();
  else void reload();
  return () => {
    disposed = true;
    off();
    stopActivity();
    closeSub();
  };
}

// web/agentflow/js/stats.ts
var defaultStatsDeps = () => ({
  stats: (project) => api.modelStats(project),
  projects: () => api.projects(),
  clear: (backend, model, role) => api.clearStrikes(backend, model, role),
  strikes: (project) => api.strikeHistory(project)
});
var pct = (r) => `${Math.round(r * 100)}%`;
function duration(ms) {
  if (ms <= 0) return "-";
  const s = ms / 1e3;
  if (s < 90) return `${Math.round(s)}s`;
  if (s < 5400) return `${(s / 60).toFixed(1)}m`;
  return `${(s / 3600).toFixed(1)}h`;
}
function tokensShort(n) {
  if (n <= 0) return "-";
  if (n < 1e3) return String(n);
  if (n < 1e6) return `${(n / 1e3).toFixed(1)}k`;
  return `${(n / 1e6).toFixed(1)}M`;
}
var ROLES2 = ["", "planner", "architect", "critic", "compiler", "worker", "reviewer"];
var StatsView = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.rows = [];
    this.history = { events: [], daily: [], total: 0 };
    this.projects = [];
    this.role = "";
    this.project = "";
    this.loaded = false;
    this.disposed = false;
    this.busy = false;
  }
  mount() {
    void this.load();
    const off = subscribe((ev) => {
      if (ev.type === "state.changed" || ev.type === "effort.update" || ev.type === "backend.update" || ev.type === "strikes.update") void this.load();
    });
    return () => {
      this.disposed = true;
      off();
    };
  }
  async load() {
    if (this.busy) return;
    this.busy = true;
    try {
      const [rows, projects, history] = await Promise.all([this.deps.stats(this.project), this.deps.projects(), this.deps.strikes(this.project)]);
      if (this.disposed) return;
      Object.assign(this, { rows, projects, history, loaded: true });
      this.render();
    } catch {
    } finally {
      this.busy = false;
    }
  }
  /** strikeCell shows a model's strikes out of three, a benched badge at three, and a Clear button. */
  strikeCell(s) {
    const total = s.strikes_total > s.strikes ? ` (${s.strikes_total} ever)` : "";
    if (!s.strikes) return h("td", { text: s.strikes_total ? `0${total}` : "0" });
    return h(
      "td",
      {},
      s.benched ? badge("benched", "warn") : h("span", { text: `${s.strikes}/3` }),
      h("span", { class: "field-hint", text: `${total} ` }),
      button("Clear", () => {
        void this.deps.clear(s.backend, s.model, s.role).then(() => this.load());
      }, { field: "clear-strikes" })
    );
  }
  table() {
    const shown = this.rows.filter((r) => this.role === "" || r.role === this.role);
    if (shown.length === 0) return h("p", { class: "field-hint", text: "No finished stages yet." });
    const head = ["Model", "Backend", "Tier", "Family", "Role", "Stages", "Passed", "Failed", "Infra", "Pass rate", "Avg time", "Total time", "Tokens", "Worker attempts passed", "Strikes"];
    return h(
      "table",
      { class: "stats-table", attrs: { "data-role": "pass-rates" } },
      h("thead", {}, h("tr", {}, ...head.map((t) => h("th", { text: t })))),
      h("tbody", {}, ...shown.map((s) => h(
        "tr",
        {},
        h("td", { text: s.model }),
        h("td", { text: s.backend }),
        h("td", {}, s.tier ? badge(`tier ${s.tier}`, s.tier === 3 ? "warn" : "muted") : h("span", { class: "field-hint", text: "not rated" })),
        h("td", { text: s.family }),
        h("td", { text: s.role }),
        h("td", { text: String(s.stages) }),
        h("td", { text: String(s.passed) }),
        h("td", { text: String(s.failed) }),
        h("td", { text: String(s.infra) }),
        h("td", { text: s.passed + s.failed > 0 ? pct(s.pass_rate) : "-" }),
        h("td", { text: duration(s.avg_ms) }),
        h("td", { text: duration(s.total_ms) }),
        h("td", { text: tokensShort(s.tokens) }),
        h("td", { text: s.role === "worker" ? `${s.attempts_passed}/${s.attempts_total}` : "-" }),
        this.strikeCell(s)
      )))
    );
  }
  /** historySection lists the recent strikes and the days they fell on; nothing in it is ever deleted. */
  historySection() {
    const hist = this.history;
    if (hist.total === 0) return section("Strike history", h("p", { class: "field-hint", text: "No strikes yet." }));
    const status = (e) => e.counted ? "counting" : `ended: ${e.ended ?? ""}`;
    const where = (e) => [e.project, e.effort ? `effort ${e.effort}` : "", e.task ? `task ${e.task}` : ""].filter(Boolean).join(" ");
    const day = (d) => `${d.day} ${d.model} ${d.role} ${d.kind} \xD7${d.count}`;
    return section(
      "Strike history",
      h("p", { class: "field-hint", text: `${hist.total} strike(s) in all. Only an unbroken run of three benches a model: a success in that role, or Clear, starts the count again. The strikes stay here.` }),
      h(
        "table",
        { class: "stats-table", attrs: { "data-role": "strike-history" } },
        h("thead", {}, h("tr", {}, ...["When", "Model", "Backend", "Role", "Kind", "Where", "Status"].map((t) => h("th", { text: t })))),
        h("tbody", {}, ...hist.events.map((e) => h(
          "tr",
          {},
          h("td", { text: new Date(e.at).toLocaleString() }),
          h("td", { text: e.model }),
          h("td", { text: e.backend }),
          h("td", { text: e.role }),
          h("td", { text: e.kind }),
          h("td", { text: where(e) }),
          h("td", { text: status(e) })
        )))
      ),
      h("p", { class: "field-hint", attrs: { "data-role": "strike-days" }, text: "By day: " + hist.daily.slice(0, 20).map(day).join("; ") })
    );
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading stats\u2026" }));
      return;
    }
    const role = selectInput("role", ROLES2.map((r) => [r, r === "" ? "All roles" : r]), this.role);
    role.addEventListener("change", () => {
      this.role = role.value;
      this.render();
    });
    const project = selectInput("project", [["", "All projects"], ...this.projects.map((p) => [p.slug, p.slug])], this.project);
    project.addEventListener("change", () => {
      this.project = project.value;
      void this.load();
    });
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host" },
        section(
          "Model results",
          h("div", { class: "inline-row" }, fieldRow("Role", role).el, fieldRow("Project", project).el),
          h("p", { class: "field-hint", text: "Infra failures (a crash, a timeout) say nothing about the model and are left out of the pass rate. Rate a model on the Backends tab once these numbers say how it does. A strike is output the engine could not read, a git policy break, or the same failure twice in a row on one task; three strikes bench a model in that role (local, free or paid alike) until you clear them here." }),
          this.table()
        ),
        this.historySection()
      )
    );
  }
};
function mountStats(host) {
  return new StatsView(host, defaultStatsDeps()).mount();
}

// web/agentflow/js/seatspage.ts
function seatState(s) {
  if (!s.running) return "no session";
  if (s.dead) return "session ended";
  if (s.halt) return `waits for the owner: ${s.halt}`;
  return s.ready ? `at its input box since ${new Date(s.since).toLocaleTimeString()}` : "working";
}
var SeatsPage = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.msg = "";
  }
  mount() {
    void this.load();
    return () => {
    };
  }
  async load() {
    let seats = [];
    let report = null;
    try {
      const st = await this.deps.coordStatus({ quiet: true });
      report = st.last_run;
      seats = await this.deps.externalSeats({ quiet: true });
      this.render(seats, report, st.enabled ? st.dry_run ? "on, dry run" : "on" : "configured, not running");
    } catch (err) {
      replace(this.host, h("p", { class: "field-hint", text: `The coordinator module is off: ${err instanceof Error ? err.message : String(err)}` }));
    }
  }
  render(seats, report, mode) {
    replace(
      this.host,
      h("h2", { text: `Hand-run seats (coordinator ${mode})` }),
      this.msg ? h("p", { class: "field-hint", attrs: { "data-role": "seats-msg" }, text: this.msg }) : null,
      h("ul", { class: "seat-list" }, ...seats.map((s) => h(
        "li",
        { attrs: { "data-seat": s.callsign } },
        h("strong", { text: s.callsign }),
        h("span", { text: ` (${s.session}) \u2014 ${seatState(s)}${s.current ? `; task ${s.current}` : ""}${s.last_action ? `; ${s.last_action}` : ""}` }),
        h("button", { text: "Queue", attrs: { type: "button", "data-role": "seat-queue" }, on: { click: () => void this.showQueue(s.callsign) } }),
        s.nudge ? h("button", { text: "Nudge", attrs: { type: "button", "data-role": "seat-nudge" }, on: { click: () => void this.nudge(s.callsign) } }) : null
      ))),
      h("h3", { text: report ? `Last pass ${new Date(report.finished).toLocaleString()}${report.dry ? " (dry run)" : ""}` : "No pass yet" }),
      report?.error ? h("p", { class: "warn-box", text: report.error }) : null,
      h("ul", { attrs: { "data-role": "coord-decisions" } }, ...(report?.decisions ?? []).map((d) => h("li", { text: `${d.kind} ${d.task ?? ""} ${d.seat ?? ""}: ${d.detail}` }))),
      h("button", { text: "Dry run now", attrs: { type: "button", "data-role": "coord-dry" }, on: { click: () => void this.dryRun() } })
    );
  }
  async showQueue(cs) {
    try {
      const q = await this.deps.seatQueue(cs);
      this.msg = "";
      await this.load();
      this.host.append(h("pre", { class: "mono", attrs: { "data-role": "seat-queue-text" }, text: q.text || "(empty)" }));
    } catch (err) {
      this.msg = `${cs}: ${err instanceof Error ? err.message : String(err)}`;
      await this.load();
    }
  }
  async nudge(cs) {
    try {
      await this.deps.nudgeSeat(cs);
      this.msg = `${cs} nudged`;
    } catch (err) {
      this.msg = `${cs}: ${err instanceof Error ? err.message : String(err)}`;
    }
    await this.load();
  }
  async dryRun() {
    try {
      const r = await this.deps.coordRun(true);
      this.msg = `dry run: ${(r.decisions ?? []).length} decision(s), ${(r.writes ?? []).length} BotChat write(s) held back`;
    } catch (err) {
      this.msg = err instanceof Error ? err.message : String(err);
    }
    await this.load();
  }
};
function mountSeatsPage(host) {
  return new SeatsPage(host, api).mount();
}

// web/agentflow/js/reviewpage.ts
function stateText(r) {
  if (r.state === "failed") return `failed: ${r.error ?? "unknown error"}`;
  if (r.state !== "done") return r.state;
  return `${r.verdict || "no verdict"}${r.reviewer_model ? ` by ${r.reviewer_model}` : ""}`;
}
var ReviewsPage = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.msg = "";
  }
  mount() {
    void this.load();
    return () => {
    };
  }
  async load() {
    try {
      const list3 = await this.deps.reviews("", { quiet: true });
      this.render(list3);
    } catch (err) {
      replace(this.host, h("p", { class: "field-hint", text: `Reviews are unavailable: ${err instanceof Error ? err.message : String(err)}` }));
    }
  }
  render(list3) {
    replace(
      this.host,
      h("h2", { text: "Reviews" }),
      h("p", { class: "field-hint", text: "Independent verify + reviewer runs requested through the review API." }),
      this.msg ? h("p", { class: "field-hint", attrs: { "data-role": "reviews-msg" }, text: this.msg }) : null,
      h("button", { text: "Refresh", attrs: { type: "button", "data-role": "reviews-refresh" }, on: { click: () => void this.load() } }),
      list3.length === 0 ? h("p", { class: "field-hint", text: "No reviews yet." }) : h("ul", { class: "review-list" }, ...list3.map((r) => h(
        "li",
        { attrs: { "data-review": String(r.id) } },
        h("strong", { text: `${r.task_id} \u2014 ${r.branch}` }),
        h("span", { text: ` (${r.project}) \u2014 ${stateText(r)}` }),
        r.findings ? h("pre", { class: "mono", text: r.findings }) : null
      )))
    );
  }
};
function mountReviewsPage(host) {
  return new ReviewsPage(host, api).mount();
}

// web/agentflow/js/main.ts
var themes = new ThemeManager({ module: "agentflow", default: "dark" });
themes.apply();
var TABS = [
  {
    id: "overview",
    label: "Overview",
    mount: (host) => new Overview(host, defaultDeps((slug) => showTab("project", slug))).mount()
  },
  { id: "project", label: "Project", mount: mountProjectTab },
  { id: "backends", label: "Backends", mount: mountBackends },
  { id: "stats", label: "Stats", mount: mountStats },
  { id: "seats", label: "Seats", mount: mountSeatsPage },
  { id: "reviews", label: "Reviews", mount: mountReviewsPage }
];
var active = "overview";
var dispose;
function renderTabs() {
  const host = document.getElementById("tab-bar");
  if (!host) return;
  replace(host, ...TABS.map((t) => h("button", {
    class: "tab-btn" + (t.id === active ? " active" : ""),
    text: t.label,
    attrs: { type: "button", "data-tab": t.id, role: "tab" },
    on: { click: () => showTab(t.id) }
  })));
}
function showTab(id, arg = "") {
  const tab = TABS.find((t) => t.id === id);
  const host = document.getElementById("panel-view");
  if (!tab || !host) return;
  if (typeof dispose === "function") dispose();
  active = id;
  renderTabs();
  replace(host);
  dispose = tab.mount(host, arg);
}
function initHamburger() {
  const trigger = document.getElementById("hamburger-trigger");
  if (!trigger) return;
  new HamburgerMenu({
    title: "AgentFlow",
    items: [
      { section: "Settings" },
      // Settings apply as soon as they are saved; the view lives as long as the drawer.
      { id: "settings", render: (host) => {
        new SettingsView(host, defaultSettingsDeps()).mount();
      } }
    ],
    themePicker: true,
    themes,
    side: "right",
    // Adopt the button index.html already ships; appending a second trigger into it nests a button in a button.
    mountTrigger: trigger
  });
}
function initServiceHold() {
  const right = document.querySelector("#nav .nav-right");
  if (!right) return;
  const chip2 = h("span", { class: "nav-hold", attrs: { "data-role": "service-hold", hidden: "", role: "status" } });
  right.prepend(chip2);
  const load = async () => {
    let hold;
    try {
      hold = await api.serviceHold({ quiet: true });
    } catch {
      return;
    }
    if (!hold?.active) {
      chip2.setAttribute("hidden", "");
      return;
    }
    chip2.textContent = `Service held: ${hold.reason ?? ""}`;
    chip2.title = hold.restart ? "A restart waits for a long stage to end" : "Nothing new starts until the hold is lifted";
    chip2.removeAttribute("hidden");
  };
  startPoll(() => true, load);
  void load();
}
function initSaveMark() {
  const right = document.querySelector("#nav .nav-right");
  if (!right) return;
  const holder = h("span", { class: "autosave-holder" });
  right.prepend(holder);
  mountSaveMark(holder);
}
initHamburger();
initSaveMark();
initServiceHold();
startEvents();
var attn = document.getElementById("attention-banner");
if (attn) new AttentionBanner(attn, api, (slug) => showTab("project", slug)).mount();
var seatPanel = document.getElementById("seat-panel");
if (seatPanel) new SeatPanel(seatPanel, api).mount();
var banner = document.getElementById("health-banner");
if (banner) new HealthBanner(banner, api).mount();
showTab("overview");
