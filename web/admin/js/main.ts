import { ThemeManager, HamburgerMenu } from "@shared";
import type { MenuItem } from "@shared";

const themes = new ThemeManager({ module: "admin", default: "dark" });
themes.apply();

interface ApiResponse<T = Record<string, unknown>> {
  ok: boolean;
  status: number;
  data: T | null;
}

interface AuthModule {
  pin_file?: string;
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
}

const statusEl = document.getElementById("status")!;
const panels = [
  "panel-matrix",
  "panel-keys",
  "panel-ldap",
  "panel-session",
  "panel-passkeys",
  "panel-operator-pin",
].map((id) => document.getElementById(id)!);

let authConfig: AuthConfig | null = null;
let matrix: Record<string, MatrixEntry> = {};
let passkeys: PasskeyEntry[] = [];
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
    toSave[m] = pinFile ? { pin_file: pinFile } : {};
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
    matrix[m] = { protected: false, pinFile: "" };
  });
  Object.keys(authConfig!.modules || {}).forEach((m) => {
    if (m === "admin") return;
    const entry = (authConfig!.modules || {})[m] || {};
    matrix[m] = { protected: true, pinFile: entry.pin_file || "" };
  });
  passkeys = (passkeysRes.ok && passkeysRes.data && passkeysRes.data.passkeys) || [];

  statusEl.classList.add("hidden");
  panels.forEach((p) => p.classList.remove("hidden"));

  renderMatrix();
  renderKeys();
  renderLdap();
  renderSession();
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
  thead.innerHTML = "<tr><th>Module</th><th>Protected</th><th>Pin file</th></tr>";
  table.appendChild(thead);

  const tbody = document.createElement("tbody");
  modules.forEach((mod) => {
    const entry = matrix[mod];
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${esc(mod)}</td>` +
      `<td><input type="checkbox" class="matrix-protected" data-module="${esc(mod)}"${entry.protected ? " checked" : ""}></td>` +
      `<td>` +
      `<select class="matrix-pinfile" data-module="${esc(mod)}"${entry.protected ? "" : " disabled"}>${pinFileOptions(entry.pinFile)}</select> ` +
      `<button type="button" class="matrix-setpin-btn" data-module="${esc(mod)}"${entry.protected ? "" : " disabled"}>Set PIN&hellip;</button>` +
      `<div class="matrix-setpin-form inline-form hidden" data-module="${esc(mod)}">` +
      `<input type="password" class="matrix-pin-input" placeholder="new PIN" autocomplete="off">` +
      `<button type="button" class="matrix-pin-save" data-module="${esc(mod)}">Save</button>` +
      `<button type="button" class="matrix-pin-cancel" data-module="${esc(mod)}">Cancel</button>` +
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
      const setBtn = container.querySelector<HTMLButtonElement>(
        `.matrix-setpin-btn[data-module="${CSS.escape(mod)}"]`,
      );
      if (setBtn) setBtn.disabled = !box.checked;
    });
  });
  container.querySelectorAll<HTMLSelectElement>("select.matrix-pinfile").forEach((select) => {
    select.addEventListener("change", () => {
      matrix[select.dataset.module!].pinFile = select.value;
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

document.getElementById("matrix-save")!.addEventListener("click", async () => {
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
  });
  renderMatrix();
  statusEl2.textContent = "Saved.";
  setTimeout(() => {
    statusEl2.textContent = "";
  }, 3000);
});

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
      `<button type="button" class="btn-remove" data-name="${esc(k.name)}">Revoke</button>`;
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
const keyModalCopied = document.getElementById("key-modal-copied")!;

function showKeyModal(key: string): void {
  keyModalValue.textContent = key;
  keyModalCopied.textContent = "";
  keyModal.classList.remove("hidden");
}

function closeKeyModal(): void {
  keyModal.classList.add("hidden");
  keyModalValue.textContent = "";
}

document.getElementById("key-modal-copy")!.addEventListener("click", () => {
  const value = keyModalValue.textContent!;
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard
      .writeText(value)
      .then(() => {
        keyModalCopied.textContent = "Copied.";
      })
      .catch(() => {
        keyModalCopied.textContent = "Copy failed -- select and copy manually.";
      });
  } else {
    keyModalCopied.textContent = "Copy not supported -- select and copy manually.";
  }
});
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

document.getElementById("ldap-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
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

document.getElementById("session-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
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
});

// ── Passkeys ────────────────────────────────────────────────────

function formatTimestamp(sec: number | undefined): string {
  if (!sec) return "";
  return new Date(sec * 1000).toLocaleString();
}

function renderPasskeys(): void {
  const list = document.getElementById("passkeys-list")!;
  list.innerHTML = "";
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
      `<button type="button" class="btn-remove" data-id="${esc(p.id)}">Delete</button>`;
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
    "Disabled: this page is served over plain HTTP. Browsers only " +
    "allow passkeys (WebAuthn) over HTTPS or on localhost.";
  btn.title = why;
  form.title = why;
}

document.getElementById("passkey-form")!.addEventListener("submit", async (e) => {
  e.preventDefault();
  if (!passkeySupported) return;
  const errorEl = document.getElementById("passkey-error")!;
  errorEl.textContent = "";
  const nameInput = document.getElementById("passkey-name") as HTMLInputElement;
  const friendlyName = nameInput.value.trim();

  const beginRes = await api<{ options: { publicKey: Record<string, unknown> }; challengeId: string }>(
    "POST",
    "/api/auth/passkey/register/begin",
    { friendlyName },
  );
  if (!beginRes.ok) {
    errorEl.textContent = errorText(beginRes, "Unable to begin passkey registration.");
    return;
  }
  const ceremony = beginRes.data!;

  let credential: Credential | null;
  try {
    credential = await navigator.credentials.create({
      publicKey: creationOptions(ceremony.options) as unknown as PublicKeyCredentialCreationOptions,
    });
  } catch {
    errorEl.textContent = "Passkey registration cancelled.";
    return;
  }
  if (!credential) {
    errorEl.textContent = "Passkey registration cancelled.";
    return;
  }

  const finishRes = await api<PasskeyEntry>("POST", "/api/auth/passkey/register/finish", {
    challengeId: ceremony.challengeId,
    friendlyName,
    credential: attestationToJSON(credential as PublicKeyCredential),
  });
  if (!finishRes.ok) {
    errorEl.textContent = errorText(finishRes, "Passkey registration failed.");
    return;
  }
  passkeys.push(finishRes.data!);
  nameInput.value = "";
  renderPasskeys();
});

// ── Operator PIN ────────────────────────────────────────────────

function renderOperatorPin(): void {
  const el = document.getElementById("operator-pin-status")!;
  const adminPin = authConfig!.admin_pin || {};
  if (adminPin.defined_by === "config") {
    el.textContent = "Defined by config.";
  } else if (adminPin.defined_by === "file") {
    el.textContent = "Defined by file (" + adminPin.path + ").";
  } else {
    el.textContent = "Not configured.";
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
  const items: MenuItem[] = [
    {
      id: "logout",
      label: "Sign out",
      onSelect: async () => {
        await api("POST", "/api/auth/logout");
        location.reload();
      },
    },
  ];
  return new HamburgerMenu({
    title: "Admin",
    items,
    themePicker: true,
    themes,
  });
}

const hamburger = buildHamburger();

// Replace topbar content with just the hamburger trigger
const topbar = document.querySelector<HTMLElement>(".topbar")!;
const logoutBtn = document.getElementById("logout-btn");
if (logoutBtn) logoutBtn.remove();
topbar.appendChild(hamburger.trigger);

loadAll();
