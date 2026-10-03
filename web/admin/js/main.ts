import { addOrigin, normalizeOrigins, removeOrigin, suggestFromOrigin, validatePasskeyForm } from "./passkeyform";
import { passkeyCardState, PASSKEYS_NOT_CONFIGURED_TEXT } from "./passkeystate";
import { ThemeManager, HamburgerMenu, createCopyButton, openModal, showToast, watchSecrets } from "@shared";
import type { MenuItem } from "@shared";

function debounce<Args extends unknown[]>(
  fn: (...args: Args) => void,
  ms: number,
): (...args: Args) => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  return (...args: Args) => {
    if (timer !== null) clearTimeout(timer);
    timer = setTimeout(() => fn(...args), ms);
  };
}

const themes = new ThemeManager({ module: "admin", default: "dark" });
themes.apply();
watchSecrets();

interface ApiResponse<T = Record<string, unknown>> {
  ok: boolean;
  status: number;
  data: T | null;
}

interface AuthModule {
  pin_file?: string;
  /** Idle sign-out in minutes; absent/0 = the server default (60). */
  idle_minutes?: number;
}

interface AdminPin {
  defined_by?: string;
  path?: string;
}

interface AuthConfig {
  modules?: Record<string, AuthModule>;
  admin_pin?: AdminPin;
  known_modules?: string[];
  api_keys?: Array<{ name: string; hash?: string }>;
  ldap?: Record<string, unknown>;
  passkey?: { rp_id?: string; rp_origins?: string[] };
  session?: { ttl_hours?: number };
  cookie_domain?: string;
  cookie_secure?: boolean;
}

interface PasskeyEntry {
  id: string;
  friendlyName?: string;
  createdAt?: number;
  lastUsedAt?: number;
}

interface PinFile {
  name: string;
  path: string;
}

interface MatrixEntry {
  protected: boolean;
  pinFile: string;
  /** Idle sign-out in minutes; 0 = the default (DEFAULT_IDLE_MINUTES). */
  idle: number;
}

const DEFAULT_IDLE_MINUTES = 60;
const MAX_IDLE_MINUTES = 7 * 24 * 60;

const statusEl = document.getElementById("status")!;
const panels = [
  "panel-matrix",
  "panel-keys",
  "panel-ldap",
  "panel-session",
  "panel-passkey-settings",
  "panel-passkeys",
  "panel-operator-pin",
].map((id) => document.getElementById(id)!);

let authConfig: AuthConfig | null = null;
let matrix: Record<string, MatrixEntry> = {};
let passkeys: PasskeyEntry[] = [];
// True while this session has no LDAP identity: the server answers the passkey
// routes 403 until the operator signs in with LDAP (see askLdapLogin).
let passkeysLocked = false;
// Set when the server answers 409: passkeys are not configured at all.
let passkeysUnconfigured = "";
let pinFiles: PinFile[] = [];

// ────────────────────────────────────────────────────────────────
// API helper
// ────────────────────────────────────────────────────────────────

async function api<T = Record<string, unknown>>(
  method: string,
  path: string,
  body?: unknown,
): Promise<ApiResponse<T>> {
  const opts: RequestInit = { method, headers: {} };
  if (body !== undefined) {
    (opts.headers as Record<string, string>)["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (res.status === 401) {
    location.reload();
    return new Promise<never>(() => {});
  }
  let data: T | null = null;
  if (res.status !== 204) {
    data = await (res.json() as Promise<T>).catch(() => null);
  }
  return { ok: res.ok, status: res.status, data };
}

function esc(str: string): string {
  return String(str)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function errorText(res: ApiResponse<unknown>, fallback: string): string {
  return ((res.data as Record<string, unknown> | null)?.error as string) || fallback;
}

// ────────────────────────────────────────────────────────────────
// Pin files
// ────────────────────────────────────────────────────────────────

async function loadPinFiles(): Promise<void> {
  const res = await api<{ files?: PinFile[] }>("GET", "/api/config/pin-files");
  if (res.ok && res.data) {
    pinFiles = res.data.files || [];
  }
}

function pinFileOptions(currentValue: string): string {
  const opts = [
    `<option value=""${currentValue ? "" : " selected"}>(no pin file)</option>`,
  ];
  let found = !currentValue;
  pinFiles.forEach((f) => {
    if (f.path === currentValue) found = true;
    opts.push(
      `<option value="${esc(f.path)}"${f.path === currentValue ? " selected" : ""}>${esc(f.name)}</option>`,
    );
  });
  if (currentValue && !found) {
    opts.push(`<option value="${esc(currentValue)}" selected>${esc(currentValue)}</option>`);
  }
  return opts.join("");
}

function buildModulesPayload(adminOverride?: AuthModule): Record<string, AuthModule> {
  const toSave: Record<string, AuthModule> = {};
  Object.keys(matrix).forEach((m) => {
    if (!matrix[m].protected) return;
    const pinFile = matrix[m].pinFile.trim();
    const entry: AuthModule = pinFile ? { pin_file: pinFile } : {};
    if (matrix[m].idle > 0) entry.idle_minutes = matrix[m].idle;
    toSave[m] = entry;
  });
  if (adminOverride !== undefined) {
    toSave.admin = adminOverride;
  } else if (authConfig!.modules && authConfig!.modules.admin) {
    toSave.admin = authConfig!.modules.admin;
  }
  return toSave;
}

function currentAdminPinFilePath(): string {
  const adminModule = (authConfig!.modules || {}).admin;
  if (adminModule && adminModule.pin_file) return adminModule.pin_file;
  const adminPin = authConfig!.admin_pin || {};
  if (adminPin.defined_by === "file") return adminPin.path || "";
  return "";
}

// ────────────────────────────────────────────────────────────────
// Load + render
// ────────────────────────────────────────────────────────────────

async function loadAll(): Promise<void> {
  const [authRes, passkeysRes] = await Promise.all([
    api<AuthConfig>("GET", "/api/config/auth"),
    api<{ passkeys?: PasskeyEntry[] }>("GET", "/api/auth/passkeys"),
    loadPinFiles(),
  ]);
  if (!authRes.ok) {
    statusEl.textContent = "Unable to load admin config.";
    return;
  }
  authConfig = authRes.data;
  matrix = {};
  (authConfig!.known_modules || []).forEach((m) => {
    if (m === "admin") return;
    matrix[m] = { protected: false, pinFile: "", idle: 0 };
  });
  Object.keys(authConfig!.modules || {}).forEach((m) => {
    if (m === "admin") return;
    const entry = (authConfig!.modules || {})[m] || {};
    matrix[m] = { protected: true, pinFile: entry.pin_file || "", idle: entry.idle_minutes || 0 };
  });
  passkeys = (passkeysRes.ok && passkeysRes.data && passkeysRes.data.passkeys) || [];
  applyPasskeyStatus(passkeysRes);

  statusEl.classList.add("hidden");
  panels.forEach((p) => p.classList.remove("hidden"));

  renderMatrix();
  renderKeys();
  renderLdap();
  renderSession();
  renderPasskeySettings();
  renderPasskeys();
  renderOperatorPin();
}

// ── Matrix ──────────────────────────────────────────────────────

function adminSourceDisplay(): string {
  const adminModule = (authConfig!.modules || {}).admin;
  if (adminModule && adminModule.pin_file) {
    return adminModule.pin_file + " (modules.admin.pin_file)";
  }
  const adminPin = authConfig!.admin_pin || {};
  if (adminPin.defined_by === "file") return adminPin.path + " (admin_pin_file)";
  if (adminPin.defined_by === "config") return "(inline config hash)";
  return "(not configured)";
}

function renderMatrix(): void {
  const container = document.getElementById("matrix-table")!;
  const modules = Object.keys(matrix).sort();

  const table = document.createElement("table");
  table.className = "matrix";

  const thead = document.createElement("thead");
  thead.innerHTML =
    '<tr><th>Module</th><th>Protected</th><th title="Sign out after this many minutes without use (clicks, typing, page loads, saves). Blank = ' +
    DEFAULT_IDLE_MINUTES +
    '.">Idle sign-out (min)</th><th>Pin file</th></tr>';
  table.appendChild(thead);

  const tbody = document.createElement("tbody");
  modules.forEach((mod) => {
    const entry = matrix[mod];
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${esc(mod)}</td>` +
      `<td><label class="ui-toggle" title="Protected">` +
      `<input type="checkbox" class="matrix-protected" data-module="${esc(mod)}" aria-label="Protect ${esc(mod)}"${entry.protected ? " checked" : ""}>` +
      `<span class="ui-toggle-track"></span></label></td>` +
      `<td><input type="number" class="matrix-idle" data-module="${esc(mod)}" min="1" max="${MAX_IDLE_MINUTES}" step="1" ` +
      `placeholder="${DEFAULT_IDLE_MINUTES}" value="${entry.idle > 0 ? entry.idle : ""}" aria-label="Idle sign-out for ${esc(mod)}, in minutes"${entry.protected ? "" : " disabled"}></td>` +
      `<td>` +
      `<select class="matrix-pinfile" data-module="${esc(mod)}"${entry.protected ? "" : " disabled"}>${pinFileOptions(entry.pinFile)}</select> ` +
      `<button type="button" class="btn btn-outline btn-sm matrix-setpin-btn" data-module="${esc(mod)}"${entry.protected ? "" : " disabled"}>Set PIN&hellip;</button>` +
      `<div class="matrix-setpin-form inline-form hidden" data-module="${esc(mod)}">` +
      `<input type="text" class="matrix-pin-input ui-secret" placeholder="new PIN" autocomplete="off" data-lpignore="true" data-1p-ignore data-form-type="other" spellcheck="false">` +
      `<button type="button" class="btn btn-primary btn-sm matrix-pin-save" data-module="${esc(mod)}">Save</button>` +
      `<button type="button" class="btn btn-ghost btn-sm matrix-pin-cancel" data-module="${esc(mod)}">Cancel</button>` +
      `</div>` +
      `<span class="matrix-pin-status status" data-module="${esc(mod)}"></span>` +
      `<p class="matrix-pin-error error" data-module="${esc(mod)}"></p>` +
      `</td>`;
    tbody.appendChild(tr);
  });

  const adminTr = document.createElement("tr");
  adminTr.innerHTML =
    `<td>admin</td>` +
    `<td class="hint">n/a</td>` +
    `<td class="hint">${(authConfig!.modules?.admin?.idle_minutes || DEFAULT_IDLE_MINUTES)}</td>` +
    `<td class="hint">${esc(adminSourceDisplay())}</td>`;
  tbody.appendChild(adminTr);

  table.appendChild(tbody);

  container.innerHTML = "";
  container.appendChild(table);

  container.querySelectorAll<HTMLInputElement>("input.matrix-protected").forEach((box) => {
    box.addEventListener("change", () => {
      const mod = box.dataset.module!;
      matrix[mod].protected = box.checked;
      const select = container.querySelector<HTMLSelectElement>(
        `select.matrix-pinfile[data-module="${CSS.escape(mod)}"]`,
      );
      if (select) select.disabled = !box.checked;
      const idle = container.querySelector<HTMLInputElement>(
        `input.matrix-idle[data-module="${CSS.escape(mod)}"]`,
      );
      if (idle) idle.disabled = !box.checked;
      const setBtn = container.querySelector<HTMLButtonElement>(
        `.matrix-setpin-btn[data-module="${CSS.escape(mod)}"]`,
      );
      if (setBtn) setBtn.disabled = !box.checked;
      autoSaveMatrix();
    });
  });
  container.querySelectorAll<HTMLInputElement>("input.matrix-idle").forEach((input) => {
    input.addEventListener("change", () => {
      const n = Math.round(Number(input.value));
      // Blank or out of range: back to the default.
      const idle = input.value.trim() === "" || !(n >= 1 && n <= MAX_IDLE_MINUTES) ? 0 : n;
      input.value = idle > 0 ? String(idle) : "";
      matrix[input.dataset.module!].idle = idle;
      autoSaveMatrix();
    });
  });
  container.querySelectorAll<HTMLSelectElement>("select.matrix-pinfile").forEach((select) => {
    select.addEventListener("change", () => {
      matrix[select.dataset.module!].pinFile = select.value;
      autoSaveMatrix();
    });
  });
  container.querySelectorAll<HTMLButtonElement>(".matrix-setpin-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const mod = btn.dataset.module!;
      const form = container.querySelector<HTMLElement>(
        `.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`,
      )!;
      form.classList.toggle("hidden");
      const input = form.querySelector<HTMLInputElement>(".matrix-pin-input")!;
      input.value = "";
      if (!form.classList.contains("hidden")) input.focus();
    });
  });
  container.querySelectorAll<HTMLButtonElement>(".matrix-pin-cancel").forEach((btn) => {
    btn.addEventListener("click", () => {
      const mod = btn.dataset.module!;
      const form = container.querySelector<HTMLElement>(
        `.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`,
      )!;
      form.classList.add("hidden");
      form.querySelector<HTMLInputElement>(".matrix-pin-input")!.value = "";
    });
  });
  container.querySelectorAll<HTMLButtonElement>(".matrix-pin-save").forEach((btn) => {
    btn.addEventListener("click", () => matrixSetPinSubmit(btn.dataset.module!));
  });
}

async function matrixSetPinSubmit(mod: string): Promise<void> {
  const container = document.getElementById("matrix-table")!;
  const form = container.querySelector<HTMLElement>(
    `.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`,
  )!;
  const select = container.querySelector<HTMLSelectElement>(
    `select.matrix-pinfile[data-module="${CSS.escape(mod)}"]`,
  )!;
  const errorEl = container.querySelector<HTMLElement>(
    `.matrix-pin-error[data-module="${CSS.escape(mod)}"]`,
  )!;
  const input = form.querySelector<HTMLInputElement>(".matrix-pin-input")!;
  const pin = input.value;
  errorEl.textContent = "";
  const selectedPath = select.value;
  const payload = selectedPath
    ? { path: selectedPath, pin }
    : { name: mod + ".pin", pin };
  const res = await api<{ path?: string }>("POST", "/api/config/pin-files", payload);
  input.value = "";
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to set PIN.");
    return;
  }
  matrix[mod].pinFile = (res.data && res.data.path) || "";
  await loadPinFiles();
  renderMatrix();
  const statusEl2 = document
    .getElementById("matrix-table")!
    .querySelector<HTMLElement>(`.matrix-pin-status[data-module="${CSS.escape(mod)}"]`);
  if (statusEl2) {
    statusEl2.textContent = "Saved.";
    setTimeout(() => {
      statusEl2.textContent = "";
    }, 3000);
  }
}

async function saveMatrix(): Promise<void> {
  const errorEl = document.getElementById("matrix-error")!;
  const statusEl2 = document.getElementById("matrix-status")!;
  errorEl.textContent = "";
  statusEl2.textContent = "";
  const toSave = buildModulesPayload();
  const res = await api<{ modules?: Record<string, AuthModule> }>(
    "PUT",
    "/api/config/modules",
    toSave,
  );
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to save matrix.");
    return;
  }
  authConfig!.modules = (res.data && res.data.modules) || toSave;
  Object.keys(matrix).forEach((m) => {
    matrix[m].pinFile = (authConfig!.modules![m] && authConfig!.modules![m].pin_file) || "";
    matrix[m].idle = (authConfig!.modules![m] && authConfig!.modules![m].idle_minutes) || 0;
  });
  renderMatrix();
  statusEl2.textContent = "Saved.";
  setTimeout(() => {
    statusEl2.textContent = "";
  }, 3000);
}

document.getElementById("matrix-save")!.addEventListener("click", async () => {
  await saveMatrix();
});

const autoSaveMatrix = debounce(() => {
  void saveMatrix();
}, 500);

// ── API keys ────────────────────────────────────────────────────

function renderKeys(): void {
  const list = document.getElementById("keys-list")!;
  list.innerHTML = "";
  const keys = authConfig!.api_keys || [];
  if (keys.length === 0) {
    list.innerHTML = '<li class="named-list-empty">No API keys configured.</li>';
    return;
  }
  keys.forEach((k) => {
    const li = document.createElement("li");
    li.innerHTML =
      `<span class="named-list-name">${esc(k.name)}</span>` +
      `<span class="named-list-value">(set)</span>` +
      `<button type="button" class="btn btn-danger btn-sm btn-remove" data-name="${esc(k.name)}">Revoke</button>`;
    list.appendChild(li);
  });
  list.querySelectorAll<HTMLButtonElement>(".btn-remove").forEach((btn) => {
    btn.addEventListener("click", () => revokeKey(btn.dataset.name!));
  });
}

document.getElementById("key-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  const errorEl = document.getElementById("key-error")!;
  errorEl.textContent = "";
  const nameInput = document.getElementById("key-name") as HTMLInputElement;
  const name = nameInput.value.trim();
  if (!name) return;
  const res = await api<{ key: string }>("POST", "/api/keys", { name });
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to generate key.");
    return;
  }
  authConfig!.api_keys = authConfig!.api_keys || [];
  const idx = authConfig!.api_keys.findIndex((k) => k.name === name);
  const entry = { name, hash: "(set)" };
  if (idx !== -1) authConfig!.api_keys[idx] = entry;
  else authConfig!.api_keys.push(entry);
  nameInput.value = "";
  renderKeys();
  showKeyModal(res.data!.key);
});

async function revokeKey(name: string): Promise<void> {
  const errorEl = document.getElementById("key-error")!;
  errorEl.textContent = "";
  const res = await api("DELETE", "/api/keys/" + encodeURIComponent(name));
  if (!res.ok && res.status !== 404) {
    errorEl.textContent = errorText(res, "Unable to revoke key.");
    return;
  }
  authConfig!.api_keys = (authConfig!.api_keys || []).filter((k) => k.name !== name);
  renderKeys();
}

// ── Show-once key modal ─────────────────────────────────────────

const keyModal = document.getElementById("key-modal")!;
const keyModalValue = document.getElementById("key-modal-value")!;

function showKeyModal(key: string): void {
  keyModalValue.textContent = key;
  keyModal.classList.remove("hidden");
}

function closeKeyModal(): void {
  keyModal.classList.add("hidden");
  keyModalValue.textContent = "";
}

// The shared copy button, in place of the markup's placeholder.
document.getElementById("key-modal-copy")!.replaceWith(
  createCopyButton({ text: () => keyModalValue.textContent ?? "", label: "API key", className: "btn btn-primary" }),
);
document.getElementById("key-modal-close")!.addEventListener("click", closeKeyModal);
keyModal.addEventListener("click", (e) => {
  if (e.target === keyModal) closeKeyModal();
});

// ── LDAP ────────────────────────────────────────────────────────

function renderLdap(): void {
  const l = (authConfig!.ldap || {}) as Record<string, unknown>;
  (document.getElementById("ldap-url") as HTMLInputElement).value = (l.url as string) || "";
  (document.getElementById("ldap-base-dn") as HTMLInputElement).value =
    (l.base_dn as string) || "";
  (document.getElementById("ldap-bind-dn") as HTMLInputElement).value =
    (l.bind_dn as string) || "";
  const pwInput = document.getElementById("ldap-bind-password") as HTMLInputElement;
  pwInput.value = "";
  pwInput.placeholder = l.bind_password === "(set)" ? "(unchanged)" : "(none)";
  (document.getElementById("ldap-user-filter") as HTMLInputElement).value =
    (l.user_filter as string) || "";
  (document.getElementById("ldap-required-groups") as HTMLInputElement).value = (
    (l.required_groups as string[]) || []
  ).join(", ");
  (document.getElementById("ldap-timeout") as HTMLInputElement).value = String(
    (l.timeout_seconds as number) || 0,
  );
  (document.getElementById("ldap-start-tls") as HTMLInputElement).checked = !!(l.start_tls as boolean);
  (document.getElementById("ldap-insecure-tls") as HTMLInputElement).checked = !!(l.insecure_tls as boolean);
}

async function saveLdap(): Promise<void> {
  const errorEl = document.getElementById("ldap-error")!;
  const statusEl2 = document.getElementById("ldap-status")!;
  errorEl.textContent = "";
  statusEl2.textContent = "";
  statusEl2.className = "status";
  const url = (document.getElementById("ldap-url") as HTMLInputElement).value.trim();
  const start_tls = (document.getElementById("ldap-start-tls") as HTMLInputElement).checked;
  const insecure_tls = (document.getElementById("ldap-insecure-tls") as HTMLInputElement).checked;
  const bind_dn = (document.getElementById("ldap-bind-dn") as HTMLInputElement).value.trim();
  const bind_password = (document.getElementById("ldap-bind-password") as HTMLInputElement).value;
  const base_dn = (document.getElementById("ldap-base-dn") as HTMLInputElement).value.trim();
  const user_filter = (
    document.getElementById("ldap-user-filter") as HTMLInputElement
  ).value.trim();
  const required_groups = (
    document.getElementById("ldap-required-groups") as HTMLInputElement
  ).value
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
  const timeout_seconds =
    parseInt((document.getElementById("ldap-timeout") as HTMLInputElement).value, 10) || 0;
  const res = await api("PUT", "/api/config/ldap", {
    url,
    start_tls,
    insecure_tls,
    bind_dn,
    bind_password,
    base_dn,
    user_filter,
    required_groups,
    timeout_seconds,
  });
  (document.getElementById("ldap-bind-password") as HTMLInputElement).value = "";
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to save LDAP settings.");
    return;
  }
  authConfig!.ldap = res.data as Record<string, unknown>;
  renderLdap();
  statusEl2.textContent = "Saved.";
  statusEl2.className = "status status-good";
  setTimeout(() => {
    statusEl2.textContent = "";
    statusEl2.className = "status";
  }, 3000);
}

document.getElementById("ldap-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  await saveLdap();
});

document.getElementById("ldap-check-btn")!.addEventListener("click", async () => {
  const resultEl = document.getElementById("ldap-check-result")!;
  resultEl.textContent = "Testing…";
  resultEl.className = "status";
  const res = await api<{ result: string; users: number; missingGroups: string[] | null; detail?: string }>(
    "POST",
    "/api/ldap/check",
  );
  if (!res.ok) {
    resultEl.textContent = errorText(res, "Test failed.");
    resultEl.className = "status status-bad";
    return;
  }
  const d = res.data!;
  const labels: Record<string, string> = {
    unreachable: "LDAP server unreachable.",
    tls_error: "TLS error (certificate not trusted, or wrong host).",
    bind_failed: "Service bind failed — check Bind DN and password." + (d.detail ? " (" + d.detail + ")" : ""),
    search_failed: "Connected, but the search failed — check Base DN and User filter." + (d.detail ? " (" + d.detail + ")" : ""),
  };
  if (d.result !== "ok") {
    resultEl.textContent = labels[d.result] || d.result;
    resultEl.className = "status status-bad";
    return;
  }
  const missing = d.missingGroups || [];
  let msg = "Connected and bound. " + d.users + " user" + (d.users === 1 ? "" : "s") + " match the filter.";
  if (d.users === 0) msg += " (Nothing matches — check Base DN and User filter.)";
  if (missing.length > 0) msg += " Required group(s) not found: " + missing.join(", ") + ".";
  resultEl.textContent = msg;
  resultEl.className = "status " + (d.users === 0 || missing.length > 0 ? "status-bad" : "status-good");
});

document.getElementById("ldap-test-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  const resultEl = document.getElementById("ldap-test-result")!;
  resultEl.textContent = "Testing…";
  resultEl.className = "status";
  const username = (document.getElementById("ldap-test-username") as HTMLInputElement).value;
  const password = (document.getElementById("ldap-test-password") as HTMLInputElement).value;
  const res = await api<{ result: string }>("POST", "/api/ldap/test", { username, password });
  (document.getElementById("ldap-test-password") as HTMLInputElement).value = "";
  if (!res.ok) {
    resultEl.textContent = errorText(res, "Test failed.");
    resultEl.className = "status status-bad";
    return;
  }
  const labels: Record<string, string> = {
    ok: "Bind succeeded.",
    bad_credentials: "Bad credentials.",
    unreachable: "LDAP server unreachable.",
    tls_error: "TLS error.",
  };
  const cls = res.data!.result === "ok" ? "status-good" : "status-bad";
  resultEl.textContent = labels[res.data!.result] || res.data!.result;
  resultEl.className = "status " + cls;
});

// ── Session ─────────────────────────────────────────────────────

function renderSession(): void {
  const s = authConfig!.session || {};
  (document.getElementById("session-ttl") as HTMLInputElement).value = String(
    s.ttl_hours || 0,
  );
  (document.getElementById("session-cookie-domain") as HTMLInputElement).value =
    authConfig!.cookie_domain || "";
  (document.getElementById("session-cookie-secure") as HTMLInputElement).checked =
    !!authConfig!.cookie_secure;
}

async function saveSession(): Promise<void> {
  const statusEl2 = document.getElementById("session-status")!;
  statusEl2.textContent = "";
  statusEl2.className = "status";
  const ttl_hours =
    parseInt((document.getElementById("session-ttl") as HTMLInputElement).value, 10) || 0;
  const cookie_domain = (
    document.getElementById("session-cookie-domain") as HTMLInputElement
  ).value;
  const cookie_secure = (
    document.getElementById("session-cookie-secure") as HTMLInputElement
  ).checked;
  const res = await api<{ ttl_hours: number; cookie_domain: string; cookie_secure: boolean }>(
    "PUT",
    "/api/config/session",
    { ttl_hours, cookie_domain, cookie_secure },
  );
  if (!res.ok) {
    statusEl2.textContent = errorText(res, "Unable to save session settings.");
    statusEl2.className = "status status-bad";
    return;
  }
  authConfig!.session = authConfig!.session || {};
  authConfig!.session.ttl_hours = res.data!.ttl_hours;
  authConfig!.cookie_domain = res.data!.cookie_domain;
  authConfig!.cookie_secure = res.data!.cookie_secure;
  statusEl2.textContent = "Saved.";
  statusEl2.className = "status status-good";
  setTimeout(() => {
    statusEl2.textContent = "";
    statusEl2.className = "status";
  }, 3000);
}

document.getElementById("session-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  await saveSession();
});

const autoSaveSession = debounce(() => {
  void saveSession();
}, 500);

document.querySelectorAll<HTMLElement>("#session-form input").forEach((el) => {
  const eventName = el instanceof HTMLInputElement && el.type === "checkbox" ? "change" : "input";
  el.addEventListener(eventName, autoSaveSession);
});

// ── Passkey settings ────────────────────────────────────────────

// Working copy of the origins list; edited only in memory until Save.
let passkeyOrigins: string[] = [];

function passkeyRpIdInput(): HTMLInputElement {
  return document.getElementById("passkey-rp-id") as HTMLInputElement;
}

function renderPasskeyOrigins(): void {
  const list = document.getElementById("passkey-origins-list")!;
  list.innerHTML = "";
  passkeyOrigins.forEach((origin, i) => {
    const li = document.createElement("li");
    const input = document.createElement("input");
    input.type = "text";
    input.value = origin;
    input.placeholder = "https://app.example.com";
    input.autocomplete = "off";
    input.spellcheck = false;
    input.setAttribute("aria-label", "Allowed origin " + (i + 1));
    input.addEventListener("input", () => {
      passkeyOrigins[i] = input.value;
    });
    const rm = document.createElement("button");
    rm.type = "button";
    rm.className = "btn btn-ghost";
    rm.textContent = "Remove";
    rm.addEventListener("click", () => {
      passkeyOrigins = removeOrigin(passkeyOrigins, i);
      renderPasskeyOrigins();
    });
    li.append(input, rm);
    list.appendChild(li);
  });
}

function renderPasskeySettings(): void {
  const p = authConfig!.passkey || {};
  passkeyRpIdInput().value = p.rp_id || "";
  passkeyOrigins = (p.rp_origins || []).slice();
  renderPasskeyOrigins();
}

function setPasskeySettingsError(text: string): void {
  document.getElementById("passkey-settings-error")!.textContent = text;
}

async function reloadPasskeyCard(): Promise<void> {
  const res = await api<{ passkeys?: PasskeyEntry[] }>("GET", "/api/auth/passkeys");
  applyPasskeyStatus(res);
  passkeys = (res.ok && res.data && res.data.passkeys) || [];
  renderPasskeys();
}

async function savePasskeySettings(): Promise<void> {
  const statusEl2 = document.getElementById("passkey-settings-status")!;
  statusEl2.textContent = "";
  statusEl2.className = "status";
  setPasskeySettingsError("");
  const rp_id = passkeyRpIdInput().value.trim();
  const rp_origins = normalizeOrigins(passkeyOrigins);
  const advice = validatePasskeyForm(rp_id, rp_origins);
  if (advice) {
    setPasskeySettingsError(advice);
    showToast("Passkey settings not saved: " + advice, "error");
    return;
  }
  const cur = authConfig!.passkey || {};
  const unchanged =
    (cur.rp_id || "") === rp_id.toLowerCase() &&
    JSON.stringify(cur.rp_origins || []) === JSON.stringify(rp_origins);
  if (unchanged) {
    statusEl2.textContent = "Nothing changed.";
    showToast("Passkey settings: nothing changed.", "notice");
    return;
  }
  const res = await api<{ rp_id: string; rp_origins: string[] }>("PUT", "/api/config/passkey", {
    rp_id,
    rp_origins,
  });
  if (!res.ok) {
    const msg = errorText(res, "Unable to save passkey settings.");
    setPasskeySettingsError(msg);
    showToast("Passkey settings not saved: " + msg, "error");
    return;
  }
  authConfig!.passkey = res.data || {};
  renderPasskeySettings();
  statusEl2.textContent = "Saved.";
  statusEl2.className = "status status-good";
  showToast("Passkey settings saved.", "success");
  await reloadPasskeyCard();
}

document.getElementById("passkey-settings-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  await savePasskeySettings();
});

document.getElementById("passkey-origin-add")!.addEventListener("click", () => {
  passkeyOrigins = addOrigin(passkeyOrigins);
  renderPasskeyOrigins();
  const inputs = document.querySelectorAll<HTMLInputElement>("#passkey-origins-list input");
  inputs[inputs.length - 1]?.focus();
});

document.getElementById("passkey-origin-here")!.addEventListener("click", () => {
  const s = suggestFromOrigin(location.origin, passkeyRpIdInput().value, passkeyOrigins);
  passkeyOrigins = s.origins;
  passkeyRpIdInput().value = s.rpId;
  renderPasskeyOrigins();
  document.getElementById("passkey-settings-hint")!.textContent = s.hint + " Press Save to apply.";
});

// ── Passkeys ────────────────────────────────────────────────────

function formatTimestamp(sec: number | undefined): string {
  if (!sec) return "";
  return new Date(sec * 1000).toLocaleString();
}

/**
 * Modal LDAP sign-in for passkey management. Passkeys belong to a person and
 * the admin PIN has none, so the operator adds an LDAP identity to this same
 * admin session (the server keeps the PIN grant). Resolves the signed-in
 * identity, or null if dismissed. Uses fetch directly: api() reloads the page on 401,
 * which would throw away the dialog on a mistyped password.
 */
function askLdapLogin(): Promise<string | null> {
  return new Promise((resolve) => {
    const form = document.createElement("form");
    form.className = "settings-form ldap-login-form";

    const hint = document.createElement("p");
    hint.className = "hint";
    hint.textContent = "Passkeys belong to a person. Sign in with your LDAP account to manage yours; you stay signed in to admin.";

    const userLabel = document.createElement("label");
    userLabel.textContent = "Username";
    const user = document.createElement("input");
    user.type = "text";
    user.autocomplete = "username";
    user.required = true;
    userLabel.append(user);

    const passLabel = document.createElement("label");
    passLabel.textContent = "Password";
    const pass = document.createElement("input");
    pass.type = "text";
    pass.className = "ui-secret";
    pass.autocomplete = "off";
    pass.setAttribute("data-lpignore", "true");
    pass.setAttribute("data-1p-ignore", "");
    pass.setAttribute("data-form-type", "other");
    pass.spellcheck = false;
    pass.required = true;
    passLabel.append(pass);

    const err = document.createElement("p");
    err.className = "error";

    const buttons = document.createElement("div");
    buttons.className = "inline-form";
    const cancel = document.createElement("button");
    cancel.type = "button";
    cancel.className = "btn btn-outline";
    cancel.textContent = "Cancel";
    const submit = document.createElement("button");
    submit.type = "submit";
    submit.className = "btn btn-primary";
    submit.textContent = "Sign in";
    buttons.append(cancel, submit);

    form.append(hint, userLabel, passLabel, err, buttons);

    let settled = false;
    const finish = (identity: string | null): void => {
      if (settled) return;
      settled = true;
      resolve(identity);
    };
    const modal = openModal(form, { title: "Sign in with LDAP", onClose: () => finish(null) });
    cancel.addEventListener("click", () => modal.close());
    user.focus();

    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      err.textContent = "";
      submit.disabled = true;
      try {
        const res = await fetch("/api/auth/login", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ method: "ldap", username: user.value.trim(), password: pass.value }),
        });
        pass.value = "";
        if (res.ok) {
          const body = (await res.json().catch(() => null)) as { identity?: string } | null;
          finish(body?.identity || user.value.trim());
          modal.close();
          return;
        }
        err.textContent =
          res.status === 401
            ? "Invalid username or password, or that account isn't in an allowed group."
            : res.status === 429
              ? "Too many attempts. Wait a moment and try again."
              : res.status === 400
                ? "LDAP sign-in isn't available for this session. Sign in to admin again."
                : "Sign-in failed (" + res.status + ").";
      } catch {
        err.textContent = "Couldn't reach the server.";
      }
      submit.disabled = false;
    });
  });
}

function applyPasskeyStatus(res: ApiResponse<unknown>): void {
  const state = passkeyCardState(res.status, errorText(res, ""));
  passkeysLocked = state.kind === "locked";
  passkeysUnconfigured = state.kind === "unconfigured" ? state.text : "";
}

/** Prompt for LDAP sign-in, then reload the passkey list. True if now unlocked. */
async function unlockPasskeys(): Promise<boolean> {
  const identity = await askLdapLogin();
  if (identity === null) return false;
  const res = await api<{ passkeys?: PasskeyEntry[] }>("GET", "/api/auth/passkeys");
  applyPasskeyStatus(res);
  passkeysLocked = !res.ok && !passkeysUnconfigured;
  passkeys = (res.ok && res.data && res.data.passkeys) || [];
  renderPasskeys();
  if (!res.ok) {
    showToast(errorText(res, "Signed in as " + identity + ", but passkeys are still unavailable.") , "error");
    return false;
  }
  showToast("Signed in as " + identity + ".", "success");
  return true;
}

function renderPasskeys(): void {
  const list = document.getElementById("passkeys-list")!;
  list.innerHTML = "";
  const submitBtn = document.getElementById("passkey-submit");
  const notice = document.getElementById("passkey-unconfigured-note");
  if (notice) {
    notice.textContent = passkeysUnconfigured;
    if (passkeysUnconfigured) {
      const link = document.createElement("a");
      link.href = "#panel-passkey-settings";
      link.textContent = " Go to Passkey settings.";
      link.addEventListener("click", (e) => {
        e.preventDefault();
        document.getElementById("panel-passkey-settings")!.scrollIntoView({ behavior: "smooth" });
        passkeyRpIdInput().focus();
      });
      notice.appendChild(link);
    }
    notice.classList.toggle("hidden", !passkeysUnconfigured);
  }
  if (submitBtn) {
    submitBtn.textContent = passkeysLocked ? "Sign in with LDAP…" : "Register new passkey";
    (submitBtn as HTMLButtonElement).disabled = !!passkeysUnconfigured;
    (submitBtn as HTMLButtonElement).title = passkeysUnconfigured;
  }
  if (passkeysUnconfigured) {
    list.innerHTML = '<li class="named-list-empty">Passkeys are not available.</li>';
    return;
  }
  if (passkeysLocked) {
    list.innerHTML = '<li class="named-list-empty">Sign in with LDAP to view and manage your passkeys.</li>';
    return;
  }
  if (passkeys.length === 0) {
    list.innerHTML = '<li class="named-list-empty">No passkeys registered.</li>';
    return;
  }
  passkeys.forEach((p) => {
    const li = document.createElement("li");
    const name = p.friendlyName || "(unnamed)";
    const created = formatTimestamp(p.createdAt);
    const lastUsed = formatTimestamp(p.lastUsedAt);
    li.innerHTML =
      `<span class="named-list-name">${esc(name)}</span>` +
      `<span class="named-list-value">${esc(created ? "added " + created : "")}${lastUsed ? esc(", last used " + lastUsed) : ""}</span>` +
      `<button type="button" class="btn btn-danger btn-sm btn-remove" data-id="${esc(p.id)}">Delete</button>`;
    list.appendChild(li);
  });
  list.querySelectorAll<HTMLButtonElement>(".btn-remove").forEach((btn) => {
    btn.addEventListener("click", () => deletePasskey(btn.dataset.id!));
  });
}

async function deletePasskey(id: string): Promise<void> {
  const errorEl = document.getElementById("passkey-error")!;
  errorEl.textContent = "";
  const res = await api("DELETE", "/api/auth/passkeys/" + encodeURIComponent(id));
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to delete passkey.");
    return;
  }
  passkeys = passkeys.filter((p) => p.id !== id);
  renderPasskeys();
}

// --- base64url helpers (WebAuthn ceremony encode/decode) ---

function b64urlToBuffer(value: string): ArrayBuffer {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized + "===".slice((normalized.length + 3) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}

function bufferToB64url(value: ArrayBuffer | null): string | null {
  if (!value) return null;
  const bytes = new Uint8Array(value);
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}

function creationOptions(options: { publicKey: Record<string, unknown> }): Record<string, unknown> {
  const publicKey = Object.assign({}, options.publicKey);
  publicKey.challenge = b64urlToBuffer(publicKey.challenge as string);
  if (publicKey.user) {
    publicKey.user = Object.assign({}, publicKey.user as Record<string, unknown>, {
      id: b64urlToBuffer((publicKey.user as Record<string, unknown>).id as string),
    });
  }
  if (Array.isArray(publicKey.excludeCredentials)) {
    publicKey.excludeCredentials = (publicKey.excludeCredentials as Array<Record<string, unknown>>).map(
      (c: Record<string, unknown>) => Object.assign({}, c, { id: b64urlToBuffer(c.id as string) }),
    );
  }
  return publicKey;
}

function attestationToJSON(credential: PublicKeyCredential): Record<string, unknown> {
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: bufferToB64url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    response: {
      attestationObject: bufferToB64url(response.attestationObject),
      clientDataJSON: bufferToB64url(response.clientDataJSON),
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}

const passkeySupported =
  window.isSecureContext && !!window.PublicKeyCredential && !!navigator.credentials;
if (!passkeySupported) {
  const form = document.getElementById("passkey-form")!;
  const btn = form.querySelector("button")!;
  btn.disabled = true;
  const why =
    "Passkey registration is disabled: this page is served over plain HTTP. " +
    "Browsers only allow passkeys (WebAuthn) over HTTPS or on localhost. " +
    "Serve the admin module over HTTPS (or access it as localhost) to enable this.";
  btn.title = why;
  form.title = why;
  const note = document.getElementById("passkey-https-note")!;
  note.textContent = why;
  note.classList.remove("hidden");
}

/**
 * Register a passkey. If the session has no LDAP identity yet, sign in first
 * (modal) and carry straight on with the registration the operator asked for,
 * rather than dropping it. Every outcome gets a toast.
 */
async function registerPasskey(friendlyName: string, retried = false): Promise<boolean> {
  const errorEl = document.getElementById("passkey-error")!;
  const fail = (msg: string): false => {
    errorEl.textContent = msg;
    showToast(msg, "error");
    return false;
  };
  if (passkeysUnconfigured) return fail(passkeysUnconfigured);
  if (passkeysLocked && !(await unlockPasskeys())) return false;

  const beginRes = await api<{ options: { publicKey: Record<string, unknown> }; challengeId: string }>(
    "POST",
    "/api/auth/passkey/register/begin",
    { friendlyName },
  );
  if (!beginRes.ok) {
    if (beginRes.status === 409) {
      passkeysUnconfigured = errorText(beginRes, PASSKEYS_NOT_CONFIGURED_TEXT);
      renderPasskeys();
      return fail(passkeysUnconfigured);
    }
    if (beginRes.status === 403 && !retried) {
      passkeysLocked = true;
      renderPasskeys();
      return registerPasskey(friendlyName, true);
    }
    return fail(errorText(beginRes, "Unable to begin passkey registration."));
  }
  const ceremony = beginRes.data!;

  let credential: Credential | null;
  try {
    credential = await navigator.credentials.create({
      publicKey: creationOptions(ceremony.options) as unknown as PublicKeyCredentialCreationOptions,
    });
  } catch {
    return fail("Passkey registration canceled.");
  }
  if (!credential) return fail("Passkey registration canceled.");

  const finishRes = await api<PasskeyEntry>("POST", "/api/auth/passkey/register/finish", {
    challengeId: ceremony.challengeId,
    friendlyName,
    credential: attestationToJSON(credential as PublicKeyCredential),
  });
  if (!finishRes.ok) return fail(errorText(finishRes, "Passkey registration failed."));
  passkeys.push(finishRes.data!);
  renderPasskeys();
  showToast('Passkey "' + (finishRes.data!.friendlyName || friendlyName || "unnamed") + '" registered.', "success");
  return true;
}

document.getElementById("passkey-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  if (!passkeySupported) return;
  document.getElementById("passkey-error")!.textContent = "";
  const nameInput = document.getElementById("passkey-name") as HTMLInputElement;
  if (await registerPasskey(nameInput.value.trim())) nameInput.value = "";
});

// ── Operator PIN ────────────────────────────────────────────────

function renderOperatorPin(): void {
  const el = document.getElementById("operator-pin-status")!;
  const adminPin = authConfig!.admin_pin || {};
  if (adminPin.defined_by === "config") {
    el.textContent = "Operator PIN: Configured (via inline config).";
  } else if (adminPin.defined_by === "file") {
    el.textContent = "Operator PIN: Configured (via " + adminPin.path + ").";
  } else {
    el.textContent =
      "Operator PIN: Not set — the admin module has its own PIN, separate from module PINs.";
  }
  document.getElementById("operator-pin-file-select")!.innerHTML = pinFileOptions(
    currentAdminPinFilePath(),
  );
}

document.getElementById("operator-pin-change-file")!.addEventListener("click", async () => {
  const statusEl2 = document.getElementById("operator-pin-file-status")!;
  const errorEl = document.getElementById("operator-pin-error")!;
  statusEl2.textContent = "";
  errorEl.textContent = "";
  const selectedPath = (document.getElementById("operator-pin-file-select") as HTMLSelectElement)
    .value;
  const toSave = buildModulesPayload({ pin_file: selectedPath });
  const res = await api<{ modules?: Record<string, AuthModule> }>(
    "PUT",
    "/api/config/modules",
    toSave,
  );
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to change PIN file.");
    return;
  }
  authConfig!.modules = (res.data && res.data.modules) || toSave;
  Object.keys(matrix).forEach((m) => {
    matrix[m].pinFile = (authConfig!.modules![m] && authConfig!.modules![m].pin_file) || "";
    matrix[m].idle = (authConfig!.modules![m] && authConfig!.modules![m].idle_minutes) || 0;
  });
  renderMatrix();
  renderOperatorPin();
  statusEl2.textContent = "Saved.";
  setTimeout(() => {
    statusEl2.textContent = "";
  }, 3000);
});

document.getElementById("operator-pin-set-btn")!.addEventListener("click", () => {
  const form = document.getElementById("operator-pin-form")!;
  const input = document.getElementById("operator-pin-value") as HTMLInputElement;
  form.classList.toggle("hidden");
  input.value = "";
  if (!form.classList.contains("hidden")) input.focus();
});

document.getElementById("operator-pin-cancel")!.addEventListener("click", () => {
  document.getElementById("operator-pin-form")!.classList.add("hidden");
  (document.getElementById("operator-pin-value") as HTMLInputElement).value = "";
});

document.getElementById("operator-pin-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  const errorEl = document.getElementById("operator-pin-error")!;
  const statusEl2 = document.getElementById("operator-pin-file-status")!;
  errorEl.textContent = "";
  statusEl2.textContent = "";
  const input = document.getElementById("operator-pin-value") as HTMLInputElement;
  const pin = input.value;
  const currentPath = currentAdminPinFilePath();
  const payload = currentPath ? { path: currentPath, pin } : { name: "admin.pin", pin };
  const res = await api<{ path: string }>("POST", "/api/config/pin-files", payload);
  input.value = "";
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to set PIN.");
    return;
  }
  const toSave = buildModulesPayload({ pin_file: res.data!.path });
  const modRes = await api<{ modules?: Record<string, AuthModule> }>(
    "PUT",
    "/api/config/modules",
    toSave,
  );
  if (!modRes.ok) {
    errorEl.textContent = errorText(
      modRes,
      "PIN set, but unable to update the admin pin file reference.",
    );
    return;
  }
  authConfig!.modules = (modRes.data && modRes.data.modules) || toSave;
  Object.keys(matrix).forEach((m) => {
    matrix[m].pinFile = (authConfig!.modules![m] && authConfig!.modules![m].pin_file) || "";
    matrix[m].idle = (authConfig!.modules![m] && authConfig!.modules![m].idle_minutes) || 0;
  });
  await loadPinFiles();
  renderMatrix();
  renderOperatorPin();
  document.getElementById("operator-pin-form")!.classList.add("hidden");
  statusEl2.textContent = "Saved.";
  setTimeout(() => {
    statusEl2.textContent = "";
  }, 3000);
});

// ── Hamburger ───────────────────────────────────────────────────

function buildHamburger(): HamburgerMenu {
  const items: MenuItem[] = [];
  return new HamburgerMenu({
    title: "Admin",
    items,
    themePicker: true,
    themes,
    side: "right",
  });
}

const hamburger = buildHamburger();

const topbar = document.querySelector<HTMLElement>(".topbar")!;
const logoutBtn = document.getElementById("logout-btn");
if (logoutBtn) logoutBtn.remove();
const topbarActions = document.createElement("div");
topbarActions.className = "topbar-actions";
// The shared sign-out icon (HamburgerMenu) lands just before the trigger.
topbarActions.append(hamburger.trigger);
topbar.appendChild(topbarActions);

loadAll();
