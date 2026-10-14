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
import { confirmDialog as confirmDialog2 } from "/shared/dist/shared.mjs";

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
  modelStats: () => get("/api/stats/models"),
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
  /** editTask answers with the queue, which is back in draft until it is approved again (AT9). */
  editTask: (slug, id, key, def, o) => send("PUT", `${eff(slug, id)}/tasks/${enc(key)}`, def, o),
  retryTask: (slug, id, key, clean) => send("POST", `${eff(slug, id)}/tasks/${enc(key)}/retry`, { clean }),
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

// web/agentflow/js/yolo.ts
import { confirmDialog, showToast } from "/shared/dist/shared.mjs";

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
  const el = h("input", { attrs: { type: "checkbox", name, "data-field": name } });
  el.checked = checked;
  return el;
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
  return h("button", {
    class: "ui-modal-btn" + (opts.primary ? " ui-modal-btn-primary" : ""),
    text: label,
    attrs: { type: "button", "data-action": opts.field },
    on: { click: onClick }
  });
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
    this.el = h("div", {}, this.list, button(addLabel, () => this.addRow(this.blank(), true), { field: "add-row" }));
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
      }, { field: "remove-row" })
    );
    this.list.appendChild(row);
    if (notify) this.onChange();
  }
  read() {
    return this.rows.map((r) => r.read());
  }
};

// web/agentflow/js/yolo.ts
var defaultYoloDeps = () => ({
  setYOLO: api.setYOLO,
  yoloInvariants: api.yoloInvariants,
  confirm: (m, label = "Confirm") => confirmDialog(m, { confirmLabel: label }),
  notify: (m) => void showToast(m, "success")
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
      button(effort.yolo ? "Disable YOLO" : "Enable YOLO", () => void toggle(), { field: "toggle-yolo" })
    ),
    listHost
  );
}

// web/agentflow/js/overview.ts
var defaultDeps = (open) => ({
  list: () => api.projects(),
  run: (slug, op) => api.run(slug, op),
  confirm: (m) => confirmDialog2(m, { confirmLabel: "Stop" }),
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
          h("p", { text: "Create a project to get started." }),
          h("button", { class: "ui-modal-btn ui-modal-btn-primary", text: "New project", attrs: { type: "button", "data-op": "new-project" }, on: {
            click: () => this.deps.open("+new")
          } })
        )
      );
      return;
    }
    replace(this.host, h("ul", { class: "task-list" }, ...this.rows.map((r) => this.row(r))));
  }
  row(p) {
    const { pct: pct2, text } = progressText(p.progress ?? {});
    const ef = p.effort;
    const bar = h("div", { class: "progress-bar-wrap" }, h("div", { class: "progress-bar", attrs: { style: `width: ${pct2}%` } }));
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
        chip(p.run_state),
        ef?.yolo ? yoloBadge() : null
      ),
      h("div", { class: "task-item-meta", text: ef ? `${ef.title} \xB7 ${ef.status.replace(/_/g, " ")}` : "no active effort" }),
      bar,
      h("div", { class: "progress-pct", text }),
      ...work,
      h("div", { class: "task-item-meta" }, ...buttons)
    );
  }
};

// web/agentflow/js/backends.ts
import { showToast as showToast3 } from "/shared/dist/shared.mjs";

// web/agentflow/js/candidates.ts
var ROLES = ["worker", "reviewer", "planner", "architect", "critic", "compiler"];
function isPaid(backends, backend, model) {
  const b = backends.find((x) => x.name === backend);
  return !!b && (b.paid || b.models.some((m) => m.model === model && m.paid));
}
function unconsented(backends, rows) {
  return rows.filter((r) => isPaid(backends, r.backend, r.model) && !r.optIn);
}
function buildRoleEditor(o) {
  const { role } = o;
  const host = h("div", { class: "profile-card", attrs: { "data-role-editor": role } });
  const errors = new FormErrors();
  const edit = (fn) => {
    o.setDraft(fn(o.rows().map((r) => ({ ...r }))));
    draw();
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
    const missing = unconsented(o.backends, o.rows());
    if (missing.length > 0) {
      errors.show(new Error(`Tick the paid opt-in for: ${missing.map((m) => `${m.backend}/${m.model}`).join(", ")}`));
      return;
    }
    try {
      await o.save(o.rows().map((r) => ({ backend: r.backend, model: r.model })));
    } catch (err) {
      errors.show(err);
      return;
    }
    o.setDraft(null);
    draw();
  };
  const draw = () => {
    const list3 = o.rows();
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
        return h(
          "div",
          { class: "inline-row", attrs: { "data-candidate": `${role}:${i}` } },
          h("span", { text: `${i + 1}. ${r.backend} / ${r.model}` }),
          paid ? badge("paid") : null,
          paid ? h("label", { class: "inline-check" }, optIn, "I opt in to paid use") : null,
          button("Up", () => edit((all) => {
            if (i > 0) [all[i - 1], all[i]] = [all[i], all[i - 1]];
            return all;
          }), { field: "up" }),
          button("Down", () => edit((all) => {
            if (i < all.length - 1) [all[i + 1], all[i]] = [all[i], all[i + 1]];
            return all;
          }), { field: "down" }),
          button("Remove", () => edit((all) => all.filter((_, j) => j !== i)), { field: "remove" })
        );
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
        }, { field: "add-candidate" })
      ),
      errors.general,
      h(
        "div",
        { class: "settings-actions" },
        ...o.extra ?? [],
        button("Discard", () => {
          o.setDraft(null);
          draw();
        }, { field: "discard" }),
        button("Save order", () => void save(), { primary: true, field: "save-candidates" })
      )
    );
  };
  draw();
  return host;
}

// web/agentflow/js/settings.ts
import { showToast as showToast2 } from "/shared/dist/shared.mjs";
var LIMIT_FIELDS = [
  { key: "max_attempts", label: "Max attempts", hint: "Whole number, 2 to 50" },
  { key: "stuck_threshold", label: "Stuck threshold", hint: "Identical failures before a task is stuck; at most max attempts" },
  { key: "max_infra_retries", label: "Max infra retries", hint: "Whole number, 0 to 50" },
  { key: "plan_max_rounds", label: "Plan max rounds", hint: "ralplan rounds before the best plan is presented, 1 to 50" },
  { key: "worker_timeout", label: "Worker timeout", hint: "A duration such as 30m or 1h" },
  { key: "review_timeout", label: "Review timeout", hint: "A duration such as 15m" },
  { key: "plan_timeout", label: "Plan timeout", hint: "A duration such as 20m" },
  { key: "verify_timeout", label: "Verify timeout", hint: "A duration such as 20m" }
];
var EXECUTION_FIELDS = [
  { key: "verify_slots", label: "Verify slots", hint: "Verify and merge stages that run at once, 1 to 64" },
  { key: "backend_cooldown", label: "Backend cooldown", hint: "How long an unreachable backend is skipped, such as 2m" }
];
var defaultSettingsDeps = () => ({
  settings: api.settings,
  putSettings: api.putSettings,
  opencodeVersion: api.opencodeVersion,
  notify: (m) => void showToast2(m, "success")
});
var str = (v) => v === void 0 ? "" : String(v);
var SettingsView = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.values = {};
    this.inputs = /* @__PURE__ */ new Map();
    this.errors = new FormErrors();
    this.oc = null;
    this.ocHost = h("div");
    this.dirty = false;
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
    };
  }
  async load() {
    try {
      const [settings, oc] = await Promise.all([this.deps.settings(), this.deps.opencodeVersion(false)]);
      this.values = settings;
      this.oc = oc;
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
        button("Refresh", () => void this.refreshVersion(), { field: "oc-refresh" })
      )
    );
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
  /** save sends every field; the server validates and names a bad field. */
  async save() {
    this.errors.clear();
    const body = {};
    for (const [key, input] of this.inputs) body[key] = input.value.trim();
    try {
      this.values = await this.deps.putSettings(body, { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return;
    }
    this.dirty = false;
    this.deps.notify("Settings saved");
    this.render();
  }
  render() {
    this.inputs.clear();
    this.errors = new FormErrors();
    const limits = h("div", { class: "profile-fields" }, ...LIMIT_FIELDS.map((f) => this.row(f)));
    const exec = h("div", { class: "profile-fields" }, ...EXECUTION_FIELDS.map((f) => this.row(f)));
    const path = this.row({ key: "opencode_path", label: "opencode path", hint: "Program to run; a change takes effect after a restart" });
    this.renderOpenCode();
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host" },
        section("Limits", limits),
        section("Execution", exec),
        section("opencode", path, this.ocHost),
        this.errors.general,
        h("div", { class: "settings-actions" }, button("Save settings", () => void this.save(), { primary: true, field: "save" }))
      )
    );
  }
};
function mountSettings(host) {
  return new SettingsView(host, defaultSettingsDeps()).mount();
}

// web/agentflow/js/backends.ts
var API_TYPES = [
  ["ollama", "ollama"],
  ["openai_compatible", "openai compatible"],
  ["anthropic", "anthropic"]
];
var defaultBackendsDeps = () => ({
  backends: api.backends,
  createBackend: api.createBackend,
  patchBackend: api.patchBackend,
  retireBackend: api.retireBackend,
  enableBackend: api.enableBackend,
  candidates: api.candidates,
  putCandidates: api.putCandidates,
  projects: api.projects,
  limits: api.limits,
  putLimits: api.putLimits,
  settings: api.settings,
  modelStats: api.modelStats,
  notify: (m) => void showToast3(m, "success")
});
function lastForRoles(cands, name) {
  return ROLES.filter((role) => {
    const mine = cands.filter((c) => c.scope === "global" && c.role === role);
    return mine.length > 0 && mine.every((c) => c.backend === name);
  });
}
var pct = (r) => `${Math.round(r * 100)}%`;
var BackendsView = class {
  constructor(host, deps) {
    this.host = host;
    this.deps = deps;
    this.backends = [];
    this.candidates = [];
    this.projects = [];
    this.stats = [];
    this.globals = {};
    this.warnings = [];
    this.loaded = false;
    this.adding = false;
    this.editing = /* @__PURE__ */ new Set();
    this.retiring = /* @__PURE__ */ new Set();
    /** role -> rows being edited; present only while the role has unsaved edits. */
    this.drafts = /* @__PURE__ */ new Map();
    this.limitsSlug = "";
    this.limitsHost = h("div");
    this.off = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      if (["backend.update", "candidates.update", "state.changed", "settings.update"].includes(ev.type) && this.idle()) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
    };
  }
  idle() {
    return !this.adding && this.editing.size === 0 && this.retiring.size === 0 && this.drafts.size === 0;
  }
  async load() {
    try {
      const [backends, candidates, projects, stats, globals] = await Promise.all([
        this.deps.backends(),
        this.deps.candidates(),
        this.deps.projects(),
        this.deps.modelStats(),
        this.deps.settings()
      ]);
      Object.assign(this, { backends, candidates, projects, stats, globals, loaded: true });
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
    const name = textInput("name", initial?.name ?? "", { readOnly: !!initial });
    const host = textInput("host", initial?.host ?? "", { placeholder: "http://host:port" });
    const apiType = selectInput("api_type", API_TYPES, initial?.api_type ?? "ollama");
    const slots = textInput("slots", String(initial?.slots ?? 1), { type: "number" });
    const hosted = checkbox("hosted", initial?.hosted ?? false);
    const paid = checkbox("paid", initial?.paid ?? false);
    const models = [];
    const modelList = h("div", { class: "wizard-list" });
    const addModel = (m = "", p = false, max = 0) => {
      const row = {
        model: textInput("model", m, { placeholder: "model name" }),
        paid: checkbox("model_paid", p),
        max: textInput("max_concurrent", max > 0 ? String(max) : "", { type: "number", placeholder: "no limit" })
      };
      models.push(row);
      const el = h(
        "div",
        { class: "inline-row", attrs: { "data-role": "model-row" } },
        row.model,
        h("label", { class: "inline-check" }, row.paid, "paid"),
        h("label", { class: "inline-check" }, "at once", row.max),
        button("Remove", () => {
          models.splice(models.indexOf(row), 1);
          el.parentNode?.removeChild(el);
        })
      );
      modelList.appendChild(el);
    };
    for (const m of initial?.models ?? []) addModel(m.model, m.paid, m.max_concurrent);
    if (!initial) addModel();
    const modelsField = errors.add("models", fieldRow(
      "Models",
      modelList,
      'The models this backend serves; mark a paid model so it is only used when listed for a role. "At once" caps how many stages run that model together (blank = only the slots limit it)'
    ));
    modelsField.el.appendChild(button("Add model", () => addModel(), { field: "add-model" }));
    const save = async () => {
      errors.clear();
      const body = {
        name: name.value.trim(),
        host: host.value.trim(),
        api_type: apiType.value,
        hosted: hosted.checked,
        paid: paid.checked,
        slots: Number(slots.value),
        models: models.map((m) => ({ model: m.model.value.trim(), paid: m.paid.checked, max_concurrent: Number(m.max.value) || 0 })).filter((m) => m.model !== "")
      };
      try {
        const res = initial ? await this.deps.patchBackend(initial.name, body, { quiet: true }) : await this.deps.createBackend(body, { quiet: true });
        this.showResult(res, initial ? `Saved ${body.name}` : `Added ${body.name}`);
      } catch (err) {
        errors.show(err);
        return;
      }
      done();
    };
    return h(
      "div",
      { class: "profile-card", attrs: { "data-form": initial ? "edit" : "add" } },
      h(
        "div",
        { class: "profile-fields" },
        errors.add("name", fieldRow("Name", name, "Letters, digits and dashes; fixed once added")).el,
        errors.add("host", fieldRow("Host", host, "An http or https URL")).el,
        errors.add("api_type", fieldRow("API type", apiType)).el,
        errors.add("slots", fieldRow("Slots", slots, "Stages this backend runs at once")).el,
        errors.add("hosted", fieldRow("Hosted", hosted, "A remote service rather than your own machine")).el,
        errors.add("paid", fieldRow("Paid", paid, "Every model on it costs money")).el
      ),
      modelsField.el,
      errors.general,
      h(
        "div",
        { class: "settings-actions" },
        button("Cancel", done, { field: "cancel" }),
        button(initial ? "Save backend" : "Add backend", () => void save(), { primary: true, field: "save" })
      )
    );
  }
  retirePanel(b) {
    const cancel = checkbox("cancel_running", false);
    const last = lastForRoles(this.candidates, b.name);
    const onlyActive = this.backends.filter((x) => !x.retired && x.name !== b.name).length === 0;
    const notes = [
      ...onlyActive ? ["This is the only active backend: after it is retired nothing can be dispatched until you add or enable one."] : [],
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
      this.showResult(res, `Retired ${b.name}`);
      await this.load();
    };
    return h(
      "div",
      { class: "warn-box", attrs: { "data-panel": "retire" } },
      ...notes.map((n) => h("p", { text: n })),
      h("label", { class: "inline-check" }, cancel, "Cancel stages running on this backend now"),
      h(
        "div",
        { class: "row-actions" },
        button("Retire backend", () => void run(), { primary: true, field: "confirm-retire" }),
        button("Keep it", () => {
          this.retiring.delete(b.name);
          this.render();
        }, { field: "cancel-retire" })
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
    const models = b.models.map((m) => h(
      "span",
      { class: "row-actions" },
      m.model,
      m.paid ? badge("paid") : null,
      m.max_concurrent > 0 ? badge(`max ${m.max_concurrent} at once`, "muted") : null
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
          b.retired ? badge("retired", "warn") : null,
          b.down_until ? badge("unreachable", "warn") : null
        ),
        h(
          "div",
          { class: "profile-actions" },
          button("Edit", () => {
            this.editing.add(b.name);
            this.render();
          }, { field: "edit" }),
          b.retired ? button("Enable", () => void this.enable(b.name), { field: "enable" }) : button("Retire", () => {
            this.retiring.add(b.name);
            this.render();
          }, { field: "retire" })
        )
      ),
      h(
        "div",
        { class: "profile-fields" },
        h("div", {}, h("label", { text: "Host" }), h("span", { text: b.host })),
        h("div", {}, h("label", { text: "API type" }), h("span", { text: b.api_type })),
        h("div", {}, h("label", { text: "Slots" }), h("span", { text: String(b.slots) })),
        h("div", {}, h("label", { text: "Models" }), ...models)
      ),
      this.retiring.has(b.name) ? this.retirePanel(b) : null
    );
  }
  async enable(name) {
    try {
      this.showResult(await this.deps.enableBackend(name), `Enabled ${name}`);
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
    }, { primary: true, field: "add-backend" });
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
      h("div", { class: "profile-list" }, ...this.backends.map((b) => this.card(b)))
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
  // ─── Per-project limits ──────────────────────────────────────────────────
  async drawLimits() {
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
    const save = async () => {
      errors.clear();
      const body = {};
      for (const [k, input] of inputs) body[k] = input.value.trim();
      try {
        await this.deps.putLimits(slug, body, { quiet: true });
      } catch (err) {
        errors.show(err);
        return;
      }
      this.deps.notify(`Saved limits for ${slug}`);
    };
    replace(
      this.limitsHost,
      h("div", { class: "profile-fields", attrs: { "data-role": "project-limits" } }, ...rows),
      errors.general,
      h("div", { class: "settings-actions" }, button("Save project limits", () => void save(), { primary: true, field: "save-limits" }))
    );
  }
  limitsSection() {
    const pick = selectInput("project", this.projects.map((p) => [p.slug, p.slug]), this.limitsSlug);
    pick.addEventListener("change", () => {
      this.limitsSlug = pick.value;
      void this.drawLimits();
    });
    return section("Project limits", fieldRow("Project", pick).el, this.limitsHost);
  }
  // ─── Pass rates ──────────────────────────────────────────────────────────
  statsSection() {
    if (this.stats.length === 0) {
      return section("Model pass rates", h("p", { class: "field-hint", text: "No finished stages yet." }));
    }
    const head = ["Model", "Backend", "Role", "Stages", "Passed", "Failed", "Infra", "Pass rate", "Worker attempts passed"];
    return section(
      "Model pass rates",
      h(
        "table",
        { class: "stats-table", attrs: { "data-role": "pass-rates" } },
        h("thead", {}, h("tr", {}, ...head.map((t) => h("th", { text: t })))),
        h("tbody", {}, ...this.stats.map((s) => h(
          "tr",
          {},
          h("td", { text: s.model }),
          h("td", { text: s.backend }),
          h("td", { text: s.role }),
          h("td", { text: String(s.stages) }),
          h("td", { text: String(s.passed) }),
          h("td", { text: String(s.failed) }),
          h("td", { text: String(s.infra) }),
          h("td", { text: s.passed + s.failed > 0 ? pct(s.pass_rate) : "-" }),
          h("td", { text: s.role === "worker" ? `${s.attempts_passed}/${s.attempts_total}` : "-" })
        )))
      )
    );
  }
  // ─── Whole view ──────────────────────────────────────────────────────────
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
      }, { field: "dismiss" })
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
        this.statsSection()
      )
    );
  }
};
function mountBackends(host) {
  return new BackendsView(host, defaultBackendsDeps()).mount();
}

// web/agentflow/js/board.ts
import { confirmDialog as confirmDialog3, copyText, showToast as showToast4 } from "/shared/dist/shared.mjs";

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
      void save();
    }, { field: "accept-suggestion" })
  );
  const save = async () => {
    errors.clear();
    try {
      await deps.patchEffort(ef.project, ef.id, {
        deliberate: deliberate.checked,
        budget_tokens: Math.max(0, Math.round(Number(tokens.value) || 0)),
        budget_seconds: Math.max(0, Math.round((Number(minutes.value) || 0) * 60))
      }, { quiet: true });
    } catch (err) {
      errors.show(err);
      return;
    }
    deps.notify("Saved effort settings");
    onSaved();
  };
  return section(
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
    h("div", { class: "settings-actions" }, button("Save settings", () => void save(), { primary: true, field: "save-effort-settings" }))
  );
}

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
        h("button", { class: "icon-btn detail-close-btn", text: "Close", attrs: { type: "button", "data-action": "close-stream" }, on: { click: () => this.close() } })
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
    let line = data;
    try {
      const parsed = JSON.parse(data);
      if (typeof parsed.line === "string") line = parsed.line;
    } catch {
    }
    this.append(line);
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
  editTask: api.editTask,
  overrideTask: api.overrideTask,
  abandonEffort: api.abandonEffort,
  gitCommands: api.gitCommands,
  confirm: (m, label = "Confirm") => confirmDialog3(m, { confirmLabel: label }),
  copy: (t) => void copyText(t),
  notify: (m) => void showToast4(m, "success")
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
    this.gitLines = null;
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
    this.errors = new FormErrors();
    this.stream = new StreamPanel(panel, streamDeps);
  }
  mount() {
    this.off = subscribe((ev) => this.handle(ev));
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
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
    this.gitLines = null;
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
      if (ef?.status === "complete" && !this.gitLines && this.gitFailed !== ef.id) {
        try {
          this.gitLines = (await this.deps.gitCommands(this.slug, ef.id, { quiet: true })).commands;
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
  async saveEdit(def) {
    const ef = this.current;
    if (!ef) return;
    this.errors.clear();
    if (!await this.deps.confirm(`Save ${def.key}? The queue returns to draft and must be approved again before anything runs.`, "Save task")) return;
    try {
      await this.deps.editTask(this.slug, ef.id, def.key, def, { quiet: true });
    } catch (err) {
      this.errors.show(err);
      return;
    }
    this.editing = false;
    this.fresh = true;
    this.deps.notify(`Saved ${def.key}; approve the queue again`);
    await this.refresh();
  }
  editForm(d) {
    this.errors = new FormErrors();
    const title = textInput("title", d.title);
    const phase = textInput("phase", d.phase);
    const prompt = textArea("prompt", d.prompt, 6);
    const acceptance = textArea("acceptance", d.acceptance.join("\n"), 3);
    const files = textArea("files_touched", d.files_touched.join("\n"), 3);
    const deps = textInput("depends_on", d.depends_on.join(", "));
    const split = (s) => s.split("\n").map((l) => l.trim()).filter((l) => l !== "");
    const e = this.errors;
    return h(
      "div",
      { attrs: { "data-role": "edit-task" } },
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
        button("Save task", () => void this.saveEdit({
          key: d.key,
          title: title.value.trim(),
          phase: phase.value.trim(),
          prompt: prompt.value,
          acceptance: split(acceptance.value),
          files_touched: split(files.value),
          depends_on: deps.value.split(",").map((s) => s.trim()).filter((s) => s !== "")
        }), { primary: true, field: "save-task" }),
        button("Cancel", () => {
          this.editing = false;
          this.fresh = true;
          this.render();
        }, { field: "cancel-edit" })
      )
    );
  }
  actions(d) {
    const clean = checkbox("clean_worktree", false);
    const reason = textArea("override_reason", "", 2);
    this.errors = new FormErrors();
    this.reasonEl = reason;
    this.cleanEl = clean;
    const reasonRow = this.errors.add("reason", fieldRow("Override reason", reason, "Recorded with your name when you mark the task passed"));
    return h(
      "div",
      { class: "detail-section", attrs: { "data-panid": "actions" } },
      h("h4", { text: "Halted: choose how to continue" }),
      h("label", { class: "inline-check" }, clean, "Recreate the worktree clean when retrying"),
      button("Retry", () => void this.retry(d.key, clean.checked), { field: "retry" }),
      button("Edit task", () => {
        this.editing = true;
        this.fresh = true;
        this.render();
      }, { field: "edit" }),
      reasonRow.el,
      button("Override as passed", () => void this.override(d.key, reason.value), { field: "override" }),
      button("Abandon effort", () => void this.abandon(), { field: "abandon" }),
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
        h("span", { class: "field-hint", text: `worker ${a.worker_model || "-"} \xB7 reviewer ${a.reviewer_model || "-"}` })
      ),
      h("div", { class: "wizard-list" }, ...a.stages.map((s) => h(
        "div",
        { class: "inline-row", attrs: { "data-stage": s.id } },
        h("span", { text: `${s.role} \xB7 ${s.backend ? `${s.backend}/${s.model}` : "engine"} \xB7 ${s.outcome || s.state}` }),
        button("Output", () => this.stream.open(s), { field: "open-stream" })
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
          h("button", { class: "icon-btn detail-close-btn", text: "Close", attrs: { type: "button", "data-action": "close-detail" }, on: {
            click: () => {
              this.selected = "";
              this.detail = null;
              this.editing = false;
              this.fresh = true;
              this.render();
            }
          } })
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
          }), { field: "watch-live" }))
        ) : null,
        this.editing ? this.editForm(d) : null,
        d.halted && !this.editing ? this.actions(d) : null,
        block("Prompt", mono(d.prompt)),
        d.acceptance.length ? block("Acceptance", bullets(d.acceptance)) : null,
        d.files_touched.length ? block("Files touched", mono(d.files_touched.join("\n"))) : null,
        d.depends_on.length ? block("Depends on", h("p", { text: d.depends_on.join(", ") })) : null,
        block("Attempts", ...d.attempt_list.length ? d.attempt_list.map((a) => this.attempt(a)) : [h("p", { class: "field-hint", text: "No attempts yet." })]),
        d.progress.length ? block("Progress log", bullets(d.progress)) : null
      )
    );
  }
  gitBox() {
    if (!this.gitLines) return null;
    const text = this.gitLines.join("\n");
    return h(
      "div",
      { class: "detail-section", attrs: { "data-role": "git-commands" } },
      h("h4", { text: "Merge and push (run these yourself; agentflow never pushes)" }),
      mono(text),
      button("Copy commands", () => this.deps.copy(text), { field: "copy-git" })
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
    const controls = buttonsFor(this.runState).map((b) => h("button", {
      class: "ui-modal-btn" + (b.primary ? " ui-modal-btn-primary" : ""),
      text: b.label,
      attrs: { type: "button", "data-op": b.op },
      on: { click: () => void this.control(b.op) }
    }));
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
      ef.budget ? budgetBar(ef.budget) : null,
      this.runState === "stopped" && this.stopReason === "budget" ? h(
        "div",
        { class: "warn-box", attrs: { "data-role": "budget-stop" } },
        h("p", { text: "The run was stopped because the effort used up its budget. Nothing failed: raise the budget on the FRD tab and start the run again." })
      ) : null,
      this.gitBox(),
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
import { showToast as showToast5 } from "/shared/dist/shared.mjs";
var defaultHistoryDeps = () => ({
  efforts: api.efforts,
  importFRD: api.importFRD,
  readFile: (f) => f.text(),
  notify: (m) => void showToast5(m, "success")
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
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "project.update")) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
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
          h("div", { class: "settings-actions" }, button("Import FRD", () => void this.importFile(), { primary: true, field: "import-frd" }))
        )
      )
    );
  }
};
function mountHistory(host, slug) {
  return new History(host, slug, defaultHistoryDeps(), defaultYoloDeps()).mount();
}

// web/agentflow/js/frd-editor.ts
import { alertDialog as alertDialog2, showToast as showToast6 } from "/shared/dist/shared.mjs";
var defaultFrdDeps = () => ({
  efforts: api.efforts,
  createEffort: api.createEffort,
  patchEffort: api.patchEffort,
  frd: api.frd,
  putFRD: api.putFRD,
  frdRevisions: api.frdRevisions,
  frdRevision: api.frdRevision,
  approveFRD: api.approveFRD,
  notify: (m) => void showToast6(m, "success")
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
    requirements: [{ id: "R1", priority: "must", description: "" }],
    acceptance_criteria: []
  };
}
var FrdForm = class {
  constructor(base, onChange) {
    this.base = base;
    this.errors = new FormErrors();
    this.f = {
      id: textInput("id", base.id ?? ""),
      title: textInput("title", base.title ?? ""),
      status: textInput("status", base.status ?? ""),
      objective: textArea("objective", base.objective ?? "", 3),
      context: textArea("context", base.context ?? "", 3),
      constraints: textArea("constraints", (base.constraints ?? []).join("\n"), 3),
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
      const id = textInput("req_id", r.id, { placeholder: "id" });
      const group = textInput("req_group", r.group ?? "", { placeholder: "group" });
      const prio = selectInput("req_priority", PRIORITIES, r.priority ?? "");
      const desc = textArea("req_description", r.description, 2);
      return {
        el: h("div", { class: "wizard-field" }, h("div", { class: "row-actions" }, id, group, prio), desc),
        read: () => ({ ...r, id: id.value.trim(), group: group.value.trim() || void 0, priority: prio.value || void 0, description: desc.value.trim() })
      };
    }, base.requirements ?? [], () => ({ id: "", description: "" }), "Add requirement", onChange);
    this.acs = new RowsEditor((a) => {
      const id = textInput("ac_id", a.id, { placeholder: "id" });
      const reqs = textInput("ac_requirements", (a.requirements ?? []).join(", "), { placeholder: "R1, R2" });
      const crit = textArea("ac_criterion", a.criterion, 2);
      return {
        el: h("div", { class: "wizard-field" }, h("div", { class: "row-actions" }, id, reqs), crit),
        read: () => ({ id: id.value.trim(), requirements: commaList(reqs.value), criterion: crit.value.trim() })
      };
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
      e.add("groups", fieldRow("Groups", this.groups.el)).el,
      e.add("requirements", fieldRow("Requirements", this.reqs.el)).el,
      e.add("acceptance_criteria", fieldRow("Acceptance criteria", this.acs.el, "Requirements are a comma-separated list of requirement ids")).el,
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
    return {
      ...this.base,
      id: v("id"),
      title: v("title"),
      status: v("status") || void 0,
      objective: v("objective"),
      context: v("context") || void 0,
      constraints: some(lines(this.f.constraints.value)),
      groups: some(this.groups.read().filter((g) => g.id || g.title)),
      requirements: this.reqs.read().filter((r) => r.id || r.description),
      acceptance_criteria: this.acs.read().filter((a) => a.id || a.criterion),
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
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (!this.dirty && (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "project.update"))) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
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
      this.form = new FrdForm(this.latest?.json ?? blankFRD(this.slug, ef), () => {
        this.dirty = true;
      });
    } else {
      this.form = null;
    }
    this.render();
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
  async save() {
    const ef = this.current;
    if (!ef || !this.form) return;
    this.form.errors.clear();
    try {
      await this.deps.putFRD(this.slug, ef.id, this.form.read(), { quiet: true });
    } catch (err) {
      this.form.errors.show(err);
      return;
    }
    this.deps.notify("Saved a new FRD revision");
    await this.load();
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
    });
    this.dirty = true;
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
      h("div", { class: "settings-actions" }, button("Create effort", () => void this.create(title.value, deliberate.checked, tokens.value, minutes.value), { primary: true, field: "create-effort" }))
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
      button("View", () => void this.view(r.revision), { field: "view-revision" })
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
          button("Load into editor", () => viewing.json && this.loadIntoEditor(viewing.json), { field: "load-revision" }),
          button("Close", () => {
            this.viewing = null;
            this.render();
          }, { field: "close-revision" })
        )
      ) : null
    );
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading efforts\u2026" }));
      return;
    }
    const ef = this.current;
    const approvedRev = this.revisions.find((r) => r.approved)?.revision;
    const editor = ef && this.form ? section(
      `FRD for #${ef.id}`,
      h(
        "div",
        { class: "row-actions" },
        h("span", { class: "field-hint", text: this.latest ? `Latest revision ${this.latest.revision}${approvedRev ? `; revision ${approvedRev} approved` : "; not approved"}` : "Not saved yet" }),
        this.dirty ? badge("unsaved", "warn") : null
      ),
      this.form.el,
      h(
        "div",
        { class: "settings-actions" },
        button("Save as new revision", () => void this.save(), { primary: true, field: "save-frd" }),
        this.latest && this.latest.revision !== approvedRev && !this.dirty && !isFinished(ef) ? button(`Approve revision ${this.latest.revision}`, () => void this.approve(), { field: "approve-frd" }) : null
      )
    ) : null;
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host" },
        this.efforts.length ? effortBar(this.slug, this.efforts, ef, (id) => {
          this.current = this.efforts.find((e) => e.id === id);
          void this.loadDoc();
        }) : null,
        this.efforts.length ? section("FRDs", this.list()) : h("div", { class: "plan-empty" }, h("h3", { text: "No efforts yet" }), h("p", { text: "An effort is one FRD, plan and queue. Create the first one below." })),
        editor,
        ef && !isFinished(ef) ? effortSettings(ef, this.form ? JSON.stringify(this.form.read()) : "", this.deps, () => void this.load()) : null,
        ef ? this.revisionList() : null,
        this.newEffortForm()
      )
    );
  }
};
function mountFrd(host, slug) {
  return new FrdEditor(host, slug, defaultFrdDeps()).mount();
}

// web/agentflow/js/plan-view.ts
import { confirmDialog as confirmDialog4, showToast as showToast7 } from "/shared/dist/shared.mjs";
var defaultPlanDeps = () => ({
  efforts: api.efforts,
  plan: api.plan,
  planRounds: api.planRounds,
  planRevisions: api.planRevisions,
  planRevision: api.planRevision,
  generatePlan: api.generatePlan,
  approvePlan: api.approvePlan,
  confirm: (m) => confirmDialog4(m, { confirmLabel: "Approve" }),
  notify: (m) => void showToast7(m, "success")
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
    this.off = null;
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "project.update")) void this.load();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
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
    await this.loadPlan();
  }
  async loadPlan() {
    const ef = this.current;
    this.state = null;
    this.rounds = [];
    this.revisions = [];
    this.viewing = null;
    if (ef) {
      try {
        const [state, rounds, revisions] = await Promise.all([
          this.deps.plan(this.slug, ef.id),
          this.deps.planRounds(this.slug, ef.id),
          this.deps.planRevisions(this.slug, ef.id)
        ]);
        this.state = state;
        this.rounds = (rounds.rounds ?? []).map((r) => ({ ...r, reviews: r.reviews ?? [] }));
        this.revisions = revisions ?? [];
        this.state = { ...state, warnings: rounds.warnings ?? state.warnings, waiting: rounds.waiting ?? state.waiting };
      } catch {
      }
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
    return this.state?.plan;
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
        st.revision > 0 ? h("span", { class: "plan-frd-id", text: `revision ${st.revision} \xB7 ${st.rounds} round${st.rounds === 1 ? "" : "s"}` }) : h("span", { class: "plan-frd-id", text: "no plan yet" }),
        st.not_critic_approved ? badge("not critic-approved", "warn") : null,
        approved ? badge(`revision ${approved} approved`, "ok") : null
      ),
      h(
        "div",
        { class: "plan-actions" },
        canGenerate ? button(st.revision > 0 ? "Regenerate plan" : "Generate plan", () => void this.generate(), { field: "generate-plan" }) : null,
        canApprove ? button("Approve plan", () => void this.approve(), { primary: true, field: "approve-plan" }) : null
      )
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
        button("View", () => void this.viewRevision(r.revision), { field: "view-revision" })
      ))) : h("p", { class: "field-hint", text: "No revisions yet." }),
      v ? h(
        "div",
        { attrs: { "data-role": "revision-view" } },
        h("h4", { text: `Revision ${v.revision}${v.approved ? " (approved)" : ""}` }),
        v.json ? renderPlan(v.json) : null,
        button("Close", () => {
          this.viewing = null;
          this.render();
        }, { field: "close-revision" })
      ) : null
    );
  }
  render() {
    if (!this.loaded) {
      replace(this.host, h("p", { class: "field-hint", text: "Loading efforts\u2026" }));
      return;
    }
    const ef = this.current;
    const st = this.state;
    if (!ef || !st) {
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
        ef.status === "planning" && !st.waiting ? h("p", { class: "field-hint", text: "Planning is running; this page updates as rounds finish." }) : null,
        this.rounds.length > 0 || st.plan ? this.roundTabs() : h("div", { class: "plan-empty" }, h("h3", { text: "No plan yet" }), h("p", { text: "Generate a plan once the FRD is saved." })),
        this.revisionList()
      )
    );
  }
};
function mountPlan(host, slug) {
  return new PlanViewer(host, slug, defaultPlanDeps()).mount();
}

// web/agentflow/js/queue.ts
import { confirmDialog as confirmDialog5, showToast as showToast8 } from "/shared/dist/shared.mjs";
var defaultQueueDeps = () => ({
  efforts: api.efforts,
  queue: api.queue,
  compile: api.compile,
  approveQueue: api.approveQueue,
  confirm: (m, label = "Confirm") => confirmDialog5(m, { confirmLabel: label }),
  notify: (m) => void showToast8(m, "success")
});
var list2 = (title, items) => h(
  "div",
  { class: "detail-section" },
  h("h4", { text: title }),
  items.length ? h("ul", { class: "plan-review-notes" }, ...items.map((i) => h("li", { text: i }))) : h("p", { class: "field-hint", text: "none" })
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
  }
  mount() {
    this.off = subscribe((ev) => {
      const mine = "project" in ev && ev.project === this.slug;
      if (ev.type === "state.changed" || mine && (ev.type === "effort.update" || ev.type === "task.update")) void this.refresh();
    });
    void this.load();
    return () => {
      this.off?.();
      this.off = null;
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
    const canCompile = ef.status === "plan_approved" || ef.status === "queue_draft" && !!q;
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
            canCompile ? button("Recompile", () => void this.compile(), { field: "compile" }) : null,
            draft ? button("Approve whole queue", () => void this.approve(), { primary: true, field: "approve-queue" }) : null
          )
        ) : h(
          "div",
          { class: "plan-empty" },
          h("h3", { text: "No queue yet" }),
          h("p", { text: ef.status === "plan_approved" ? "The plan is approved. Compile it into tasks to review." : "Approve a plan first, then compile it into tasks." }),
          ef.status === "plan_approved" ? button("Compile plan", () => void this.compile(), { primary: true, field: "compile" }) : null
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
import { confirmDialog as confirmDialog6, showToast as showToast9 } from "/shared/dist/shared.mjs";
var defaultProjectDeps = () => ({
  createProject: api.createProject,
  patchProject: api.patchProject,
  gitInit: api.gitInit,
  deleteProject: api.deleteProject,
  backends: api.backends,
  candidates: api.candidates,
  putProjectCandidates: api.putProjectCandidates,
  clearProjectCandidates: api.clearProjectCandidates,
  confirm: (m, label = "Confirm") => confirmDialog6(m, { confirmLabel: label }),
  notify: (m) => void showToast9(m, "success")
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
var ProjectForm = class {
  constructor(host, deps, mode, done) {
    this.host = host;
    this.deps = deps;
    this.mode = mode;
    this.done = done;
    this.errors = new FormErrors();
    this.backends = [];
    this.cands = [];
    this.drafts = /* @__PURE__ */ new Map();
    this.rolesHost = h("div");
    const p = mode.project;
    this.slug = textInput("slug", p?.slug ?? "", { readOnly: !!p, placeholder: "my-project" });
    this.repo = textInput("repo_path", p?.repo_path ?? "", { placeholder: "/absolute/path/to/repository" });
    this.scope = selectInput("model_scope", SCOPES, p?.model_scope ?? "local");
    this.cleanup = checkbox("cleanup_enabled", p?.cleanup_enabled ?? false);
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
    }, p?.feedback_cmds ?? [], () => "", "Add command");
    this.plugins = new RowsEditor((pl) => {
      const name = textInput("plugin_name", pl.name, { placeholder: "package-name" });
      const version = textInput("plugin_version", pl.version, { placeholder: "1.2.3" });
      return { el: h("div", { class: "row-actions" }, name, version), read: () => ({ name: name.value.trim(), version: version.value.trim() }) };
    }, p?.plugins ?? [], () => ({ name: "", version: "" }), "Add plugin");
  }
  async mount() {
    if (this.mode.project) {
      try {
        [this.backends, this.cands] = await Promise.all([this.deps.backends(), this.deps.candidates()]);
      } catch {
      }
    }
    this.render();
  }
  body() {
    return {
      slug: this.slug.value.trim(),
      repo_path: this.repo.value.trim(),
      model_scope: this.scope.value,
      feedback_cmds: this.cmds.read().filter((c) => c !== ""),
      cleanup_enabled: this.cleanup.checked,
      plugins: this.plugins.read().filter((pl) => pl.name !== "" || pl.version !== "")
    };
  }
  /** save creates or updates the project; a repo path that is not yet a repository offers git init (PJ1). */
  async save() {
    this.errors.clear();
    const body = this.body();
    const attempt = () => this.mode.project ? this.deps.patchProject(this.mode.project.slug, { repo_path: body.repo_path, model_scope: body.model_scope, feedback_cmds: body.feedback_cmds, cleanup_enabled: body.cleanup_enabled, plugins: body.plugins }, { quiet: true }) : this.deps.createProject(body, { quiet: true });
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
  async remove() {
    const p = this.mode.project;
    if (!p || !await this.deps.confirm(`Delete project ${p.slug}? Its history is kept only if it has none; a project with efforts cannot be deleted.`, "Delete")) return;
    try {
      await this.deps.deleteProject(p.slug);
    } catch {
      return;
    }
    this.deps.notify(`Deleted ${p.slug}`);
    this.done("");
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
          extra: overridden ? [button("Use global list", () => void this.clearRole(role), { field: "clear-override" })] : []
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
  render() {
    const p = this.mode.project;
    const e = this.errors;
    this.drawRoles();
    replace(
      this.host,
      h(
        "div",
        { class: "settings-host", attrs: { "data-form": p ? "project-edit" : "project-create" } },
        section(
          p ? `Edit ${p.slug}` : "New project",
          h(
            "div",
            { class: "profile-fields" },
            e.add("slug", fieldRow("Slug", this.slug, "Lowercase letters, digits and dashes; fixed once created")).el,
            e.add("repo_path", fieldRow("Repository path", this.repo, "Absolute path of an existing git repository; if it is not one you are offered git init")).el,
            e.add("model_scope", fieldRow("Model scope", this.scope, "local keeps this project off hosted backends")).el,
            e.add("cleanup_enabled", fieldRow(
              "Cleanup pass",
              this.cleanup,
              "After a task passes review, run a cleanup agent and verify again; if that breaks verification the cleanup is rolled back"
            )).el
          ),
          e.add("feedback_cmds", fieldRow(
            "Feedback commands",
            this.cmds.el,
            "Run in the sandbox to verify each task, as a program and arguments, never through a shell. Blank: detected from go.mod, pyproject.toml or package.json"
          )).el,
          e.add("plugins", fieldRow(
            "Runner plugins",
            this.plugins.el,
            "Opt-in only: none are loaded unless listed here. Each is pinned to an exact version such as 1.2.3 (no latest, no ranges); listed plugins are fetched by the runner when a stage starts"
          )).el,
          e.general,
          h(
            "div",
            { class: "settings-actions" },
            p ? button("Delete project", () => void this.remove(), { field: "delete-project" }) : null,
            button("Cancel", () => this.done(p?.slug ?? ""), { field: "cancel" }),
            button(p ? "Save project" : "Create project", () => void this.save(), { primary: true, field: "save-project" })
          )
        ),
        p ? section(
          "Role preferences",
          h("p", { class: "field-hint", text: "Override the global candidate list for one role in this project; a role without an override uses the global list." }),
          this.rolesHost
        ) : h("p", { class: "field-hint", text: "Role preferences can be set once the project exists." })
      )
    );
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
  const picker = h("select", { class: "settings-input", attrs: { "aria-label": "Project" } });
  const bar = h("div", { class: "tab-bar", attrs: { role: "tablist" } });
  const body = h("div", { class: "project-body" });
  const closeSub = () => {
    if (typeof dispose2 === "function") dispose2();
    dispose2 = void 0;
  };
  const showSub = () => {
    closeSub();
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
      replace(body, h("p", { class: "field-hint", text: "Pick a project, or create one with New project." }));
      return;
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
    if (!projects.some((p) => p.slug === slug)) slug = projects[0]?.slug ?? "";
    replace(picker, ...projects.map((p) => h("option", { text: p.slug, attrs: { value: p.slug, selected: p.slug === slug } })));
    picker.value = slug;
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
    showSub();
  });
  replace(
    host,
    h(
      "div",
      { class: "console-header" },
      h("span", { class: "pane-title", text: "Project" }),
      picker,
      button("New project", () => showForm(), { field: "new-project" }),
      button("Edit project", () => {
        const cur = projects.find((p) => p.slug === slug);
        if (cur) showForm(cur);
      }, { field: "edit-project" })
    ),
    bar,
    body
  );
  if (wanted2 === "+new") showForm();
  else void reload();
  return () => {
    disposed = true;
    closeSub();
  };
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
  { id: "settings", label: "Settings", mount: mountSettings },
  { id: "backends", label: "Backends", mount: mountBackends }
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
initHamburger();
startEvents();
showTab("overview");
