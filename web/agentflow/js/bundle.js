// web/agentflow/js/main.ts
import { ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";

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
  paused: "review"
};
function chip(status) {
  const key = status.toLowerCase().replace(/[^a-z0-9]+/g, "-");
  return h("span", { class: `status-chip status-${CHIP_ALIAS[key] ?? key}`, text: status.replace(/_/g, " ") });
}

// web/agentflow/js/events.ts
function backoffMs(failures2) {
  return Math.min(3e4, 1e3 * 2 ** Math.max(0, failures2));
}
var EVENT_NAME = "agentflow";
var URL = "/api/events";
var listeners = /* @__PURE__ */ new Set();
var source = null;
var failures = 0;
var timer = null;
var wanted = false;
function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
function dispatch(data) {
  let ev;
  try {
    ev = JSON.parse(data);
  } catch {
    return;
  }
  if (!ev || typeof ev.type !== "string") return;
  for (const fn of [...listeners]) fn(ev);
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

// web/agentflow/js/overview.ts
import { confirmDialog } from "/shared/dist/shared.mjs";

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
async function apiFetch(path, opts = {}) {
  if (!path.startsWith("/api/")) throw new Error(`apiFetch: ${path} is not an /api path`);
  const init = { method: opts.method ?? "GET" };
  if (opts.body !== void 0) {
    init.headers = { "Content-Type": "application/json" };
    init.body = typeof opts.body === "string" ? opts.body : JSON.stringify(opts.body);
  }
  let resp;
  try {
    resp = await fetch(path, init);
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
var get = (path, o) => apiFetch(path, o);
var send = (method, path, body, o) => apiFetch(path, { ...o, method, body });
var api = {
  // Health and opencode
  health: () => get("/api/health"),
  opencodeVersion: (refresh = false) => get(`/api/opencode/version${refresh ? "?refresh=1" : ""}`),
  // Projects
  projects: () => get("/api/projects"),
  project: (slug) => get(proj(slug)),
  createProject: (p, o) => send("POST", "/api/projects", p, o),
  gitInit: (repoPath) => send("POST", "/api/projects/git-init", { repo_path: repoPath, confirm: true }),
  patchProject: (slug, p, o) => send("PATCH", proj(slug), p, o),
  deleteProject: (slug) => send("DELETE", proj(slug)),
  limits: (slug) => get(`${proj(slug)}/limits`),
  putLimits: (slug, limits, o) => send("PUT", `${proj(slug)}/limits`, { limits }, o),
  // Backends, candidates, settings
  backends: () => get("/api/backends"),
  createBackend: (b, o) => send("POST", "/api/backends", b, o),
  patchBackend: (name, b, o) => send("PATCH", `/api/backends/${enc(name)}`, b, o),
  retireBackend: (name, cancelRunning) => send("POST", `/api/backends/${enc(name)}/retire`, { cancel_running: cancelRunning }),
  enableBackend: (name) => send("POST", `/api/backends/${enc(name)}/enable`),
  candidates: () => get("/api/candidates"),
  putCandidates: (role, candidates, o) => send("PUT", `/api/candidates/${enc(role)}`, { candidates }, o),
  putProjectCandidates: (slug, role, candidates, o) => send("PUT", `${proj(slug)}/candidates/${enc(role)}`, { candidates }, o),
  clearProjectCandidates: (slug, role) => send("DELETE", `${proj(slug)}/candidates/${enc(role)}`),
  settings: () => get("/api/settings"),
  putSettings: (s, o) => send("PUT", "/api/settings", s, o),
  // Efforts
  efforts: (slug) => get(`${proj(slug)}/efforts`),
  effort: (slug, id) => get(eff(slug, id)),
  createEffort: (slug, title, deliberate, o) => send("POST", `${proj(slug)}/efforts`, { title, deliberate }, o),
  importFRD: (slug, frdJSON, o) => send("POST", `${proj(slug)}/efforts/import`, frdJSON, o),
  abandonEffort: (slug, id) => send("POST", `${eff(slug, id)}/abandon`),
  setYOLO: (slug, id, on, confirm) => send("POST", `${eff(slug, id)}/yolo`, { on, confirm }),
  yoloInvariants: () => get("/api/yolo/invariants"),
  // FRD and plan documents
  frd: (slug, id) => get(`${eff(slug, id)}/frd`),
  putFRD: (slug, id, frd, o) => send("PUT", `${eff(slug, id)}/frd`, frd, o),
  frdRevisions: (slug, id) => get(`${eff(slug, id)}/frd/revisions`),
  frdRevision: (slug, id, n) => get(`${eff(slug, id)}/frd/revisions/${n}`),
  approveFRD: (slug, id, revision) => send("POST", `${eff(slug, id)}/frd/approve`, revision === void 0 ? {} : { revision }),
  generatePlan: (slug, id) => send("POST", `${eff(slug, id)}/plan/generate`),
  plan: (slug, id) => get(`${eff(slug, id)}/plan`),
  putPlan: (slug, id, plan, o) => send("PUT", `${eff(slug, id)}/plan`, plan, o),
  planRounds: (slug, id) => get(`${eff(slug, id)}/plan/rounds`),
  planRevisions: (slug, id) => get(`${eff(slug, id)}/plan/revisions`),
  planRevision: (slug, id, n) => get(`${eff(slug, id)}/plan/revisions/${n}`),
  approvePlan: (slug, id) => send("POST", `${eff(slug, id)}/plan/approve`),
  // Queue and tasks
  compile: (slug, id) => send("POST", `${eff(slug, id)}/compile`),
  queue: (slug, id) => get(`${eff(slug, id)}/queue`),
  approveQueue: (slug, id, hash) => send("POST", `${eff(slug, id)}/queue/approve`, { hash }),
  tasks: (slug, id) => get(`${eff(slug, id)}/tasks`),
  task: (slug, id, key) => get(`${eff(slug, id)}/tasks/${enc(key)}`),
  editTask: (slug, id, key, def, o) => send("PUT", `${eff(slug, id)}/tasks/${enc(key)}`, def, o),
  retryTask: (slug, id, key, clean) => send("POST", `${eff(slug, id)}/tasks/${enc(key)}/retry`, { clean }),
  overrideTask: (slug, id, key, reason) => send("POST", `${eff(slug, id)}/tasks/${enc(key)}/override`, { reason }),
  // Run control: answers with the project's overview
  run: (slug, op) => send("POST", `${proj(slug)}/run/${op}`)
};

// web/agentflow/js/overview.ts
var defaultDeps = (open) => ({
  list: () => api.projects(),
  run: (slug, op) => api.run(slug, op),
  confirm: (m) => confirmDialog(m, { confirmLabel: "Stop" }),
  open
});
function buttonsFor(runState) {
  switch (runState) {
    case "running":
      return [{ op: "pause", label: "Pause" }, { op: "stop", label: "Stop" }];
    case "pausing":
      return [{ op: "start", label: "Resume", primary: true }, { op: "stop", label: "Stop" }];
    case "paused":
      return [
        { op: "start", label: "Resume", primary: true },
        { op: "run-one", label: "Run one" },
        { op: "stop", label: "Stop" }
      ];
    default:
      return [{ op: "start", label: "Start", primary: true }, { op: "run-one", label: "Run one" }];
  }
}
function progressText(progress) {
  const total = progress.total ?? 0;
  const done = (progress.passed ?? 0) + (progress.overridden ?? 0);
  const pct = total > 0 ? Math.round(done / total * 100) : 0;
  const rest = Object.keys(progress).filter((k) => k !== "total" && k !== "passed" && progress[k] > 0).sort().map((k) => `${k.replace(/_/g, " ")} ${progress[k]}`);
  const head = total > 0 ? `${pct}% \u2014 passed ${progress.passed ?? 0}/${total}` : "no queue yet";
  return { pct, text: [head, ...rest].join(" \xB7 ") };
}
function workLine(w) {
  return `${w.role} \xB7 ${w.task || "effort"}${w.attempt ? ` (attempt ${w.attempt})` : ""} \xB7 ${w.backend}/${w.model} \xB7 ${w.state}`;
}
var Overview = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.rows = [];
    this.loaded = false;
    this.refreshing = false;
    this.again = false;
    this.off = null;
  }
  /** mount fetches the rows and starts listening to the event stream; it returns the dispose. */
  mount() {
    this.off = subscribe((ev) => this.handle(ev));
    void this.refresh();
    return () => {
      this.off?.();
      this.off = null;
    };
  }
  /** refresh refetches the rows; overlapping calls collapse into one follow-up fetch. */
  async refresh() {
    if (this.refreshing) {
      this.again = true;
      return;
    }
    this.refreshing = true;
    try {
      do {
        this.again = false;
        try {
          this.rows = await this.deps.list();
          this.loaded = true;
        } catch {
        }
        this.render();
      } while (this.again);
    } finally {
      this.refreshing = false;
    }
  }
  /** handle applies one stream event: run-state changes show at once, the rest refetch. */
  handle(ev) {
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
    if (this.rows.length === 0) {
      replace(
        this.host,
        h(
          "div",
          { class: "plan-empty" },
          h("h3", { text: "No projects yet" }),
          h("p", { text: "Create a project to get started (coming in E3)." })
        )
      );
      return;
    }
    replace(this.host, h("ul", { class: "task-list" }, ...this.rows.map((r) => this.row(r))));
  }
  row(p) {
    const { pct, text } = progressText(p.progress ?? {});
    const ef = p.effort;
    const bar = h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar", attrs: { style: `width: ${pct}%` } }));
    const work = (p.current ?? []).length > 0 ? (p.current ?? []).map((w) => h("div", { class: "task-item-meta", text: workLine(w) })) : [h("div", { class: "task-item-meta", text: ef ? "no stage running" : "no active effort" })];
    const buttons = buttonsFor(p.run_state).map((b) => h("button", {
      class: "ui-modal-btn" + (b.primary ? " ui-modal-btn-primary" : ""),
      text: b.label,
      attrs: { type: "button", "data-op": b.op },
      on: { click: () => void this.control(p.slug, b.op) }
    }));
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
        chip(p.run_state)
      ),
      h("div", { class: "task-item-meta", text: ef ? `${ef.title} \xB7 ${ef.status.replace(/_/g, " ")}` : "no active effort" }),
      bar,
      h("div", { class: "progress-pct", text }),
      ...work,
      h("div", { class: "task-item-meta" }, ...buttons)
    );
  }
};

// web/agentflow/js/projecttab.ts
function placeholder(story) {
  return (host) => replace(host, h("p", { class: "field-hint", text: `coming in ${story}` }));
}
var SUB_TABS = [
  { id: "frd", label: "FRD", mount: placeholder("E3") },
  { id: "plan", label: "Plan", mount: placeholder("E3") },
  { id: "queue", label: "Queue", mount: placeholder("E4") },
  { id: "board", label: "Board", mount: placeholder("E4") },
  { id: "history", label: "History", mount: placeholder("E5") }
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
  let sub = SUB_TABS[0].id;
  let slug = wanted2 || remembered();
  const picker = h("select", { class: "settings-input", attrs: { "aria-label": "Project" } });
  const bar = h("div", { class: "tab-bar", attrs: { role: "tablist" } });
  const body = h("div", { class: "project-body" });
  const showSub = () => {
    if (typeof dispose2 === "function") dispose2();
    dispose2 = void 0;
    replace(bar, ...SUB_TABS.map((t) => h("button", {
      class: "tab-btn" + (t.id === sub ? " active" : ""),
      text: t.label,
      attrs: { type: "button", "data-sub": t.id },
      on: { click: () => {
        sub = t.id;
        showSub();
      } }
    })));
    if (!slug) {
      replace(body, h("p", { class: "field-hint", text: "Pick a project." }));
      return;
    }
    const tab = SUB_TABS.find((t) => t.id === sub) ?? SUB_TABS[0];
    replace(body);
    dispose2 = tab.mount(body, slug);
  };
  picker.addEventListener("change", () => {
    slug = picker.value;
    remember(slug);
    showSub();
  });
  replace(host, h("div", { class: "console-header" }, h("span", { class: "pane-title", text: "Project" }), picker), bar, body);
  void api.projects().then((ps) => {
    if (!ps.some((p) => p.slug === slug)) slug = ps[0]?.slug ?? "";
    replace(picker, ...ps.map((p) => h("option", { text: p.slug, attrs: { value: p.slug, selected: p.slug === slug } })));
    picker.value = slug;
    showSub();
  }).catch(() => showSub());
  showSub();
  return () => {
    if (typeof dispose2 === "function") dispose2();
  };
}

// web/agentflow/js/main.ts
var themes = new ThemeManager({ module: "agentflow", default: "dark" });
themes.apply();
function placeholder2(story) {
  return (host) => replace(host, h("p", { class: "field-hint", text: `coming in ${story}` }));
}
var TABS = [
  {
    id: "overview",
    label: "Overview",
    mount: (host) => new Overview(host, defaultDeps((slug) => showTab("project", slug))).mount()
  },
  { id: "project", label: "Project", mount: mountProjectTab },
  { id: "settings", label: "Settings", mount: placeholder2("E2") },
  { id: "backends", label: "Backends", mount: placeholder2("E2") }
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
  const hamburger = new HamburgerMenu({
    title: "AgentFlow",
    items: [{ section: "AgentFlow" }],
    themePicker: true,
    themes,
    side: "right"
  });
  trigger.appendChild(hamburger.trigger);
}
function initLogout() {
  const btn = document.getElementById("logout-btn");
  if (!btn) return;
  btn.removeAttribute("hidden");
  btn.addEventListener("click", () => {
    window.location.href = "/logout";
  });
}
initHamburger();
initLogout();
startEvents();
showTab("overview");
