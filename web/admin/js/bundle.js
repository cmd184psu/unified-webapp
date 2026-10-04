// web/admin/js/groups.ts
function parseRequiredGroups(raw) {
  const out = [];
  for (const piece of raw.split(",")) {
    const t = piece.trim();
    if (!t) continue;
    const eq = t.indexOf("=");
    let name = t;
    if (eq >= 0) {
      if (t.slice(0, eq).trim().toLowerCase() !== "cn") continue;
      name = t.slice(eq + 1).trim();
    }
    if (name && !out.includes(name)) out.push(name);
  }
  return out;
}

// web/admin/js/keyscope.ts
var ALL = "*";
function scopeFromList(list) {
  if (!list || list.length === 0 || list.includes(ALL)) return { all: true, modules: [] };
  return { all: false, modules: [...list].sort() };
}
function scopeToList(s) {
  return s.all ? [ALL] : [...s.modules].sort();
}
function scopeValid(s) {
  return s.all || s.modules.length > 0;
}
function setAll(s, all) {
  return { all, modules: s.modules };
}
function setModule(s, module, on) {
  const without = s.modules.filter((m) => m !== module);
  return { all: s.all, modules: on ? [...without, module].sort() : without };
}
function scopeSummary(list) {
  const s = scopeFromList(list);
  return s.all ? "All modules" : s.modules.join(", ");
}

// web/admin/js/passkeyform.ts
var MAX_ORIGINS = 64;
function normalizeOrigins(list) {
  const out = [];
  for (const o of list) {
    const t = o.trim();
    if (t && !out.includes(t)) out.push(t);
  }
  return out;
}
function addOrigin(list, origin = "") {
  return [...list, origin];
}
function removeOrigin(list, index) {
  return list.filter((_, i) => i !== index);
}
function validatePasskeyForm(rpIdRaw, originsRaw) {
  const id = rpIdRaw.trim().toLowerCase();
  const origins = normalizeOrigins(originsRaw);
  if (id === "" && origins.length === 0) return "";
  if (id === "") return "Relying Party ID is required when allowed origins are set";
  if (origins.length > MAX_ORIGINS) return "too many allowed origins: at most " + MAX_ORIGINS;
  if (/[\s:/?#@\\]/.test(id)) return "Relying Party ID " + id + " must be a bare host name: no scheme, port, path or spaces";
  if (!id.includes(".") && id !== "localhost") return "Relying Party ID " + id + " must contain at least one dot (for example example.com)";
  for (const o of origins) {
    let u;
    try {
      u = new URL(o);
    } catch {
      return o + " is not a valid origin: use a URL like https://host.example.com";
    }
    const host = u.hostname.toLowerCase();
    if (host === "") return o + " has no host";
    if (u.protocol !== "https:" && !(u.protocol === "http:" && host === "localhost")) {
      return o + " must use https (http is only allowed for http://localhost)";
    }
    if (u.pathname !== "/" && u.pathname !== "" || u.search !== "" || u.hash !== "") {
      return o + " must be an origin only: no path, query or fragment";
    }
    if (host !== id && !host.endsWith("." + id)) {
      return host + " is not under " + id + ": an origin's host must be the RP ID or a subdomain of it";
    }
  }
  return "";
}

// web/admin/js/passkeystate.ts
var PASSKEYS_NOT_CONFIGURED_TEXT = "Passkeys are not configured on this server: set the Relying Party ID and allowed origins in Admin > Passkey settings (passkeys also need HTTPS).";
function passkeyCardState(status, message) {
  if (status === 409) return { kind: "unconfigured", text: message || PASSKEYS_NOT_CONFIGURED_TEXT };
  if (status === 403) return { kind: "locked", text: "Sign in with LDAP to view and manage your passkeys." };
  if (status >= 200 && status < 300) return { kind: "ready", text: "" };
  return { kind: "error", text: (message || "Unable to load passkeys") + " (HTTP " + status + ")" };
}

// web/admin/js/main.ts
import { ThemeManager, HamburgerMenu, createCopyButton, createToggle, openModal, openTreePicker, showToast, watchSecrets } from "/shared/dist/shared.mjs";
var themes = new ThemeManager({ module: "admin", default: "dark" });
themes.apply();
watchSecrets();
var DEFAULT_IDLE_MINUTES = 60;
var MAX_IDLE_MINUTES = 7 * 24 * 60;
var statusEl = document.getElementById("status");
var panels = [
  "panel-matrix",
  "panel-keys",
  "panel-ldap",
  "panel-session",
  "panel-passkey-settings",
  "panel-passkeys",
  "panel-operator-pin"
].map((id) => document.getElementById(id));
var authConfig = null;
var matrix = {};
var passkeys = [];
var passkeysLocked = false;
var passkeysUnconfigured = "";
async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== void 0) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  if (res.status === 401) {
    location.reload();
    return new Promise(() => {
    });
  }
  let data = null;
  if (res.status !== 204) {
    data = await res.json().catch(() => null);
  }
  return { ok: res.ok, status: res.status, data };
}
function esc(str) {
  return String(str).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}
function errorText(res, fallback) {
  return res.data?.error || fallback;
}
function baseName(p) {
  const i = p.lastIndexOf("/");
  return i >= 0 ? p.slice(i + 1) : p;
}
function fmtSize(n) {
  if (n < 1024) return n + " B";
  if (n < 1048576) return (n / 1024).toFixed(1) + " KB";
  return (n / 1048576).toFixed(1) + " MB";
}
function markMatrixDirty() {
  const el = document.getElementById("matrix-status");
  el.textContent = "Unsaved changes";
  el.className = "status";
}
async function pickServerFile(title, current) {
  const start = current || Object.values(matrix).map((e) => e.pinFile).find(Boolean) || "";
  const chosen = await openTreePicker({
    title,
    select: "file",
    confirmLabel: "Use this file",
    emptyText: "Nothing here.",
    startPath: start || void 0,
    load: async (dir) => {
      const res = await api(
        "GET",
        "/api/config/fs?path=" + encodeURIComponent(dir ? dir.path : "/")
      );
      if (!res.ok) throw new Error(errorText(res, "That folder cannot be opened."));
      return (res.data?.entries || []).map((e) => ({
        name: e.name,
        path: e.path,
        isDir: e.isDir,
        meta: e.isDir ? void 0 : fmtSize(e.size)
      }));
    }
  });
  return chosen ? chosen.path : "";
}
async function chooseKeyFile(mod) {
  const path = await pickServerFile("Choose key file for " + mod, matrix[mod].pinFile);
  if (!path) return;
  matrix[mod].pinFile = path;
  renderMatrix();
  markMatrixDirty();
}
function buildModulesPayload(adminOverride) {
  const toSave = {};
  Object.keys(matrix).forEach((m) => {
    if (!matrix[m].protected) return;
    const pinFile = matrix[m].pinFile.trim();
    const entry = pinFile ? { pin_file: pinFile } : {};
    if (matrix[m].idle > 0) entry.idle_minutes = matrix[m].idle;
    toSave[m] = entry;
  });
  if (adminOverride !== void 0) {
    toSave.admin = adminOverride;
  } else if (authConfig.modules && authConfig.modules.admin) {
    toSave.admin = authConfig.modules.admin;
  }
  return toSave;
}
function currentAdminPinFilePath() {
  const adminModule = (authConfig.modules || {}).admin;
  if (adminModule && adminModule.pin_file) return adminModule.pin_file;
  const adminPin = authConfig.admin_pin || {};
  if (adminPin.defined_by === "file") return adminPin.path || "";
  return "";
}
async function loadAll() {
  const [authRes, passkeysRes] = await Promise.all([
    api("GET", "/api/config/auth"),
    api("GET", "/api/auth/passkeys")
  ]);
  if (!authRes.ok) {
    statusEl.textContent = "Unable to load admin config.";
    return;
  }
  authConfig = authRes.data;
  matrix = {};
  (authConfig.known_modules || []).forEach((m) => {
    if (m === "admin") return;
    matrix[m] = { protected: false, pinFile: "", idle: 0 };
  });
  Object.keys(authConfig.modules || {}).forEach((m) => {
    if (m === "admin") return;
    const entry = (authConfig.modules || {})[m] || {};
    matrix[m] = { protected: true, pinFile: entry.pin_file || "", idle: entry.idle_minutes || 0 };
  });
  passkeys = passkeysRes.ok && passkeysRes.data && passkeysRes.data.passkeys || [];
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
function adminSourceDisplay() {
  const adminModule = (authConfig.modules || {}).admin;
  if (adminModule && adminModule.pin_file) {
    return adminModule.pin_file + " (modules.admin.pin_file)";
  }
  const adminPin = authConfig.admin_pin || {};
  if (adminPin.defined_by === "file") return adminPin.path + " (admin_pin_file)";
  if (adminPin.defined_by === "config") return "(inline config hash)";
  return "(not configured)";
}
function renderMatrix() {
  const container = document.getElementById("matrix-table");
  const modules = Object.keys(matrix).sort();
  const table = document.createElement("table");
  table.className = "matrix";
  const thead = document.createElement("thead");
  thead.innerHTML = '<tr><th>Module</th><th>Protected</th><th title="Sign out after this many minutes without use (clicks, typing, page loads, saves). Blank = ' + DEFAULT_IDLE_MINUTES + '.">Idle sign-out (min)</th><th>Pin file</th></tr>';
  table.appendChild(thead);
  const tbody = document.createElement("tbody");
  modules.forEach((mod) => {
    const entry = matrix[mod];
    const tr = document.createElement("tr");
    tr.innerHTML = `<td>${esc(mod)}</td><td><label class="ui-toggle" title="Protected"><input type="checkbox" class="matrix-protected" data-module="${esc(mod)}" aria-label="Protect ${esc(mod)}"${entry.protected ? " checked" : ""}><span class="ui-toggle-track"></span></label></td><td><input type="number" class="matrix-idle" data-module="${esc(mod)}" min="1" max="${MAX_IDLE_MINUTES}" step="1" placeholder="${DEFAULT_IDLE_MINUTES}" value="${entry.idle > 0 ? entry.idle : ""}" aria-label="Idle sign-out for ${esc(mod)}, in minutes"${entry.protected ? "" : " disabled"}></td><td><span class="matrix-pinfile" title="${esc(entry.pinFile)}">${entry.pinFile ? esc(baseName(entry.pinFile)) : '<span class="hint">no key file</span>'}</span> <button type="button" class="btn btn-outline btn-sm matrix-choose-btn" data-module="${esc(mod)}"${entry.protected ? "" : " disabled"}>Choose&hellip;</button> ` + (entry.pinFile ? `<button type="button" class="btn btn-ghost btn-sm matrix-clear-btn" data-module="${esc(mod)}" title="Use no key file" aria-label="Use no key file for ${esc(mod)}"${entry.protected ? "" : " disabled"}>&#x2715;</button> ` : "") + `<button type="button" class="btn btn-outline btn-sm matrix-setpin-btn" data-module="${esc(mod)}"${entry.protected ? "" : " disabled"}>Set PIN&hellip;</button><div class="matrix-setpin-form inline-form hidden" data-module="${esc(mod)}"><input type="text" class="matrix-pin-input ui-secret" placeholder="new PIN" autocomplete="off" data-lpignore="true" data-1p-ignore data-form-type="other" spellcheck="false"><button type="button" class="btn btn-primary btn-sm matrix-pin-save" data-module="${esc(mod)}">Save</button><button type="button" class="btn btn-ghost btn-sm matrix-pin-cancel" data-module="${esc(mod)}">Cancel</button></div><span class="matrix-pin-status status" data-module="${esc(mod)}"></span><p class="matrix-pin-error error" data-module="${esc(mod)}"></p></td>`;
    tbody.appendChild(tr);
  });
  const adminTr = document.createElement("tr");
  adminTr.innerHTML = `<td>admin</td><td class="hint">n/a</td><td class="hint">${authConfig.modules?.admin?.idle_minutes || DEFAULT_IDLE_MINUTES}</td><td class="hint">${esc(adminSourceDisplay())}</td>`;
  tbody.appendChild(adminTr);
  table.appendChild(tbody);
  container.innerHTML = "";
  container.appendChild(table);
  container.querySelectorAll("input.matrix-protected").forEach((box) => {
    box.addEventListener("change", () => {
      const mod = box.dataset.module;
      matrix[mod].protected = box.checked;
      container.querySelectorAll(
        `.matrix-choose-btn[data-module="${CSS.escape(mod)}"], .matrix-clear-btn[data-module="${CSS.escape(mod)}"]`
      ).forEach((b) => b.disabled = !box.checked);
      const idle = container.querySelector(
        `input.matrix-idle[data-module="${CSS.escape(mod)}"]`
      );
      if (idle) idle.disabled = !box.checked;
      const setBtn = container.querySelector(
        `.matrix-setpin-btn[data-module="${CSS.escape(mod)}"]`
      );
      if (setBtn) setBtn.disabled = !box.checked;
      markMatrixDirty();
    });
  });
  container.querySelectorAll("input.matrix-idle").forEach((input) => {
    input.addEventListener("change", () => {
      const n = Math.round(Number(input.value));
      const idle = input.value.trim() === "" || !(n >= 1 && n <= MAX_IDLE_MINUTES) ? 0 : n;
      input.value = idle > 0 ? String(idle) : "";
      matrix[input.dataset.module].idle = idle;
      markMatrixDirty();
    });
  });
  container.querySelectorAll(".matrix-choose-btn").forEach((btn) => {
    btn.addEventListener("click", () => void chooseKeyFile(btn.dataset.module));
  });
  container.querySelectorAll(".matrix-clear-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      matrix[btn.dataset.module].pinFile = "";
      renderMatrix();
      markMatrixDirty();
    });
  });
  container.querySelectorAll(".matrix-setpin-btn").forEach((btn) => {
    btn.addEventListener("click", () => {
      const mod = btn.dataset.module;
      const form = container.querySelector(
        `.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`
      );
      form.classList.toggle("hidden");
      const input = form.querySelector(".matrix-pin-input");
      input.value = "";
      if (!form.classList.contains("hidden")) input.focus();
    });
  });
  container.querySelectorAll(".matrix-pin-cancel").forEach((btn) => {
    btn.addEventListener("click", () => {
      const mod = btn.dataset.module;
      const form = container.querySelector(
        `.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`
      );
      form.classList.add("hidden");
      form.querySelector(".matrix-pin-input").value = "";
    });
  });
  container.querySelectorAll(".matrix-pin-save").forEach((btn) => {
    btn.addEventListener("click", () => matrixSetPinSubmit(btn.dataset.module));
  });
}
async function matrixSetPinSubmit(mod) {
  const container = document.getElementById("matrix-table");
  const form = container.querySelector(
    `.matrix-setpin-form[data-module="${CSS.escape(mod)}"]`
  );
  const errorEl = container.querySelector(
    `.matrix-pin-error[data-module="${CSS.escape(mod)}"]`
  );
  const input = form.querySelector(".matrix-pin-input");
  const pin = input.value;
  errorEl.textContent = "";
  const selectedPath = matrix[mod].pinFile;
  const payload = selectedPath ? { path: selectedPath, pin } : { name: mod + ".pin", pin };
  const res = await api("POST", "/api/config/pin-files", payload);
  input.value = "";
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to set PIN.");
    return;
  }
  matrix[mod].pinFile = res.data && res.data.path || "";
  renderMatrix();
  const statusEl2 = document.getElementById("matrix-table").querySelector(`.matrix-pin-status[data-module="${CSS.escape(mod)}"]`);
  if (statusEl2) {
    statusEl2.textContent = "Saved.";
    setTimeout(() => {
      statusEl2.textContent = "";
    }, 3e3);
  }
}
async function saveMatrix() {
  const errorEl = document.getElementById("matrix-error");
  const statusEl2 = document.getElementById("matrix-status");
  errorEl.textContent = "";
  statusEl2.textContent = "";
  const toSave = buildModulesPayload();
  const res = await api(
    "PUT",
    "/api/config/modules",
    toSave
  );
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to save matrix.");
    return;
  }
  authConfig.modules = res.data && res.data.modules || toSave;
  Object.keys(matrix).forEach((m) => {
    const saved = authConfig.modules[m];
    if (!saved && !matrix[m].protected) return;
    matrix[m].pinFile = saved && saved.pin_file || "";
    matrix[m].idle = saved && saved.idle_minutes || 0;
  });
  renderMatrix();
  statusEl2.textContent = "Saved.";
  setTimeout(() => {
    statusEl2.textContent = "";
  }, 3e3);
}
document.getElementById("matrix-save").addEventListener("click", async () => {
  await saveMatrix();
});
function chooseableModules() {
  return (authConfig.known_modules || []).filter((m) => m !== "admin").sort();
}
function buildScopeChooser(initial, onChange) {
  let scope = initial;
  const root = document.createElement("div");
  root.className = "scope-chooser";
  const draw = () => {
    root.innerHTML = "";
    root.append(
      createToggle({
        checked: scope.all,
        label: "All modules",
        onChange: (on) => {
          scope = setAll(scope, on);
          onChange(scope);
          draw();
        }
      })
    );
    if (scope.all) return;
    const grid = document.createElement("div");
    grid.className = "scope-grid";
    chooseableModules().forEach((m) => {
      grid.append(
        createToggle({
          checked: scope.modules.includes(m),
          label: m,
          onChange: (on) => {
            scope = setModule(scope, m, on);
            onChange(scope);
            draw();
          }
        })
      );
    });
    root.append(grid);
  };
  draw();
  return root;
}
var newKeyScope = { all: false, modules: [] };
function updateGenerateButton() {
  const name = document.getElementById("key-name").value.trim();
  document.getElementById("key-generate").disabled = !(name && scopeValid(newKeyScope));
}
function renderKeyForm() {
  const host = document.getElementById("key-scope");
  host.innerHTML = "";
  host.append(
    buildScopeChooser(newKeyScope, (s) => {
      newKeyScope = s;
      updateGenerateButton();
    })
  );
  updateGenerateButton();
}
document.getElementById("key-name").addEventListener("input", updateGenerateButton);
function renderKeys() {
  const list = document.getElementById("keys-list");
  list.innerHTML = "";
  const keys = authConfig.api_keys || [];
  if (keys.length === 0) {
    list.innerHTML = '<li class="named-list-empty">No API keys yet.</li>';
  }
  keys.forEach((k) => {
    const li = document.createElement("li");
    const scope = scopeFromList(k.modules);
    const tags = scope.all ? '<span class="tag tag-all">All modules</span>' : scope.modules.map((m) => `<span class="tag">${esc(m)}</span>`).join("");
    li.innerHTML = `<span class="named-list-name">${esc(k.name)}</span><span class="key-tags" title="${esc(scopeSummary(k.modules))}">${tags}</span><button type="button" class="btn btn-outline btn-sm btn-scope" data-name="${esc(k.name)}">Scope&hellip;</button><button type="button" class="btn btn-danger btn-sm btn-remove" data-name="${esc(k.name)}">Revoke</button>`;
    list.appendChild(li);
  });
  list.querySelectorAll(".btn-remove").forEach((btn) => {
    btn.addEventListener("click", () => revokeKey(btn.dataset.name));
  });
  list.querySelectorAll(".btn-scope").forEach((btn) => {
    btn.addEventListener("click", () => editKeyScope(btn.dataset.name));
  });
  renderKeyForm();
}
function editKeyScope(name) {
  const key = (authConfig.api_keys || []).find((k) => k.name === name);
  if (!key) return;
  let scope = scopeFromList(key.modules);
  const body = document.createElement("div");
  body.className = "scope-dialog";
  const save = document.createElement("button");
  save.type = "button";
  save.className = "btn btn-primary";
  save.textContent = "Save";
  const cancel = document.createElement("button");
  cancel.type = "button";
  cancel.className = "btn btn-ghost";
  cancel.textContent = "Cancel";
  const error = document.createElement("p");
  error.className = "error";
  const actions = document.createElement("div");
  actions.className = "panel-actions";
  actions.append(save, cancel);
  body.append(
    buildScopeChooser(scope, (s) => {
      scope = s;
      save.disabled = !scopeValid(scope);
    }),
    error,
    actions
  );
  const modal = openModal(body, { title: "Where " + name + " works" });
  cancel.addEventListener("click", () => modal.close());
  save.addEventListener("click", async () => {
    const res = await api("PUT", "/api/keys/" + encodeURIComponent(name), {
      modules: scopeToList(scope)
    });
    if (!res.ok) {
      error.textContent = errorText(res, "Unable to change this key.");
      return;
    }
    key.modules = res.data && res.data.modules || scopeToList(scope);
    modal.close();
    renderKeys();
    showToast("Saved.", "success");
  });
}
document.getElementById("key-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const errorEl = document.getElementById("key-error");
  errorEl.textContent = "";
  const nameInput = document.getElementById("key-name");
  const name = nameInput.value.trim();
  if (!name || !scopeValid(newKeyScope)) return;
  const res = await api("POST", "/api/keys", {
    name,
    modules: scopeToList(newKeyScope)
  });
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to generate key.");
    return;
  }
  authConfig.api_keys = authConfig.api_keys || [];
  const idx = authConfig.api_keys.findIndex((k) => k.name === name);
  const entry = { name, hash: "(set)", modules: res.data.modules || scopeToList(newKeyScope) };
  if (idx !== -1) authConfig.api_keys[idx] = entry;
  else authConfig.api_keys.push(entry);
  nameInput.value = "";
  newKeyScope = { all: false, modules: [] };
  renderKeys();
  showKeyModal(res.data.key);
});
async function revokeKey(name) {
  const errorEl = document.getElementById("key-error");
  errorEl.textContent = "";
  const res = await api("DELETE", "/api/keys/" + encodeURIComponent(name));
  if (!res.ok && res.status !== 404) {
    errorEl.textContent = errorText(res, "Unable to revoke key.");
    return;
  }
  authConfig.api_keys = (authConfig.api_keys || []).filter((k) => k.name !== name);
  renderKeys();
}
var keyModal = document.getElementById("key-modal");
var keyModalValue = document.getElementById("key-modal-value");
function showKeyModal(key) {
  keyModalValue.textContent = key;
  keyModal.classList.remove("hidden");
}
function closeKeyModal() {
  keyModal.classList.add("hidden");
  keyModalValue.textContent = "";
}
document.getElementById("key-modal-copy").replaceWith(
  createCopyButton({ text: () => keyModalValue.textContent ?? "", label: "API key", className: "btn btn-primary" })
);
document.getElementById("key-modal-close").addEventListener("click", closeKeyModal);
keyModal.addEventListener("click", (e) => {
  if (e.target === keyModal) closeKeyModal();
});
function renderLdap() {
  const l = authConfig.ldap || {};
  document.getElementById("ldap-url").value = l.url || "";
  document.getElementById("ldap-base-dn").value = l.base_dn || "";
  document.getElementById("ldap-bind-dn").value = l.bind_dn || "";
  const pwInput = document.getElementById("ldap-bind-password");
  pwInput.value = "";
  pwInput.placeholder = l.bind_password === "(set)" ? "(unchanged)" : "(none)";
  document.getElementById("ldap-user-filter").value = l.user_filter || "";
  document.getElementById("ldap-required-groups").value = (l.required_groups || []).join(", ");
  document.getElementById("ldap-timeout").value = String(
    l.timeout_seconds || 0
  );
  document.getElementById("ldap-start-tls").checked = !!l.start_tls;
  document.getElementById("ldap-insecure-tls").checked = !!l.insecure_tls;
}
async function saveLdap() {
  const errorEl = document.getElementById("ldap-error");
  const statusEl2 = document.getElementById("ldap-status");
  errorEl.textContent = "";
  statusEl2.textContent = "";
  statusEl2.className = "status";
  const url = document.getElementById("ldap-url").value.trim();
  const start_tls = document.getElementById("ldap-start-tls").checked;
  const insecure_tls = document.getElementById("ldap-insecure-tls").checked;
  const bind_dn = document.getElementById("ldap-bind-dn").value.trim();
  const bind_password = document.getElementById("ldap-bind-password").value;
  const base_dn = document.getElementById("ldap-base-dn").value.trim();
  const user_filter = document.getElementById("ldap-user-filter").value.trim();
  const required_groups = parseRequiredGroups(
    document.getElementById("ldap-required-groups").value
  );
  const timeout_seconds = parseInt(document.getElementById("ldap-timeout").value, 10) || 0;
  const res = await api("PUT", "/api/config/ldap", {
    url,
    start_tls,
    insecure_tls,
    bind_dn,
    bind_password,
    base_dn,
    user_filter,
    required_groups,
    timeout_seconds
  });
  document.getElementById("ldap-bind-password").value = "";
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to save LDAP settings.");
    return;
  }
  authConfig.ldap = res.data;
  renderLdap();
  statusEl2.textContent = "Saved.";
  statusEl2.className = "status status-good";
  setTimeout(() => {
    statusEl2.textContent = "";
    statusEl2.className = "status";
  }, 3e3);
}
document.getElementById("ldap-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  await saveLdap();
});
document.getElementById("ldap-check-btn").addEventListener("click", async () => {
  const resultEl = document.getElementById("ldap-check-result");
  resultEl.textContent = "Testing\u2026";
  resultEl.className = "status";
  const res = await api(
    "POST",
    "/api/ldap/check"
  );
  if (!res.ok) {
    resultEl.textContent = errorText(res, "Test failed.");
    resultEl.className = "status status-bad";
    return;
  }
  const d = res.data;
  const labels = {
    unreachable: "LDAP server unreachable.",
    tls_error: "TLS error (certificate not trusted, or wrong host).",
    bind_failed: "Service bind failed \u2014 check Bind DN and password." + (d.detail ? " (" + d.detail + ")" : ""),
    search_failed: "Connected, but the search failed \u2014 check Base DN and User filter." + (d.detail ? " (" + d.detail + ")" : "")
  };
  if (d.result !== "ok") {
    resultEl.textContent = labels[d.result] || d.result;
    resultEl.className = "status status-bad";
    return;
  }
  const missing = d.missingGroups || [];
  let msg = "Connected and bound. " + d.users + " user" + (d.users === 1 ? "" : "s") + " match the filter.";
  if (d.users === 0) msg += " (Nothing matches \u2014 check Base DN and User filter.)";
  if (missing.length > 0) msg += " Required group(s) not found: " + missing.join(", ") + ".";
  resultEl.textContent = msg;
  resultEl.className = "status " + (d.users === 0 || missing.length > 0 ? "status-bad" : "status-good");
});
document.getElementById("ldap-test-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const resultEl = document.getElementById("ldap-test-result");
  resultEl.textContent = "Testing\u2026";
  resultEl.className = "status";
  const username = document.getElementById("ldap-test-username").value;
  const password = document.getElementById("ldap-test-password").value;
  const res = await api("POST", "/api/ldap/test", { username, password });
  document.getElementById("ldap-test-password").value = "";
  if (!res.ok) {
    resultEl.textContent = errorText(res, "Test failed.");
    resultEl.className = "status status-bad";
    return;
  }
  const labels = {
    ok: "Bind succeeded.",
    bad_credentials: "Bad credentials.",
    unreachable: "LDAP server unreachable.",
    tls_error: "TLS error."
  };
  const cls = res.data.result === "ok" ? "status-good" : "status-bad";
  resultEl.textContent = labels[res.data.result] || res.data.result;
  resultEl.className = "status " + cls;
});
function renderSession() {
  const s = authConfig.session || {};
  document.getElementById("session-ttl").value = String(
    s.ttl_hours || 0
  );
  document.getElementById("session-cookie-domain").value = authConfig.cookie_domain || "";
  document.getElementById("session-cookie-secure").checked = !!authConfig.cookie_secure;
}
async function saveSession() {
  const statusEl2 = document.getElementById("session-status");
  statusEl2.textContent = "";
  statusEl2.className = "status";
  const ttl_hours = parseInt(document.getElementById("session-ttl").value, 10) || 0;
  const cookie_domain = document.getElementById("session-cookie-domain").value;
  const cookie_secure = document.getElementById("session-cookie-secure").checked;
  const res = await api(
    "PUT",
    "/api/config/session",
    { ttl_hours, cookie_domain, cookie_secure }
  );
  if (!res.ok) {
    statusEl2.textContent = errorText(res, "Unable to save session settings.");
    statusEl2.className = "status status-bad";
    return;
  }
  authConfig.session = authConfig.session || {};
  authConfig.session.ttl_hours = res.data.ttl_hours;
  authConfig.cookie_domain = res.data.cookie_domain;
  authConfig.cookie_secure = res.data.cookie_secure;
  statusEl2.textContent = "Saved.";
  statusEl2.className = "status status-good";
  setTimeout(() => {
    statusEl2.textContent = "";
    statusEl2.className = "status";
  }, 3e3);
}
document.getElementById("session-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  await saveSession();
});
document.querySelectorAll("#session-form input").forEach((el) => {
  const eventName = el instanceof HTMLInputElement && el.type === "checkbox" ? "change" : "input";
  el.addEventListener(eventName, () => {
    const st = document.getElementById("session-status");
    st.textContent = "Unsaved changes";
    st.className = "status";
  });
});
var passkeyOrigins = [];
function passkeyRpIdInput() {
  return document.getElementById("passkey-rp-id");
}
function renderPasskeyOrigins() {
  const list = document.getElementById("passkey-origins-list");
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
function renderPasskeySettings() {
  const p = authConfig.passkey || {};
  passkeyRpIdInput().value = p.rp_id || "";
  passkeyOrigins = (p.rp_origins || []).slice();
  renderPasskeyOrigins();
  const routed = document.getElementById("passkey-routed-list");
  routed.innerHTML = "";
  (authConfig.routed_origins || []).forEach((o) => {
    const li = document.createElement("li");
    li.textContent = o.replace(/^https:\/\//, "");
    routed.appendChild(li);
  });
  routed.classList.toggle("hidden", routed.children.length === 0);
  const chip = document.getElementById("passkey-rp-suggest");
  const sug = authConfig.suggested_rp_id || "";
  chip.textContent = sug ? "\uFF0B " + sug : "";
  chip.dataset.value = sug;
  chip.classList.toggle("hidden", !(sug && !(p.rp_id || "")));
  const extras = document.querySelector(".passkey-extra");
  if (extras) extras.open = passkeyOrigins.length > 0;
}
function setPasskeySettingsError(text) {
  document.getElementById("passkey-settings-error").textContent = text;
}
async function reloadPasskeyCard() {
  const res = await api("GET", "/api/auth/passkeys");
  applyPasskeyStatus(res);
  passkeys = res.ok && res.data && res.data.passkeys || [];
  renderPasskeys();
}
async function savePasskeySettings() {
  const statusEl2 = document.getElementById("passkey-settings-status");
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
  const cur = authConfig.passkey || {};
  const unchanged = (cur.rp_id || "") === rp_id.toLowerCase() && JSON.stringify(cur.rp_origins || []) === JSON.stringify(rp_origins);
  if (unchanged) {
    statusEl2.textContent = "Nothing changed.";
    showToast("Passkey settings: nothing changed.", "notice");
    return;
  }
  const res = await api("PUT", "/api/config/passkey", {
    rp_id,
    rp_origins
  });
  if (!res.ok) {
    const msg = errorText(res, "Unable to save passkey settings.");
    setPasskeySettingsError(msg);
    showToast("Passkey settings not saved: " + msg, "error");
    return;
  }
  authConfig.passkey = res.data || {};
  const fresh = await api("GET", "/api/config/auth");
  if (fresh.ok && fresh.data) authConfig = fresh.data;
  renderPasskeySettings();
  statusEl2.textContent = "Saved.";
  statusEl2.className = "status status-good";
  showToast("Passkey settings saved.", "success");
  await reloadPasskeyCard();
}
document.getElementById("passkey-settings-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  await savePasskeySettings();
});
document.getElementById("passkey-origin-add").addEventListener("click", () => {
  passkeyOrigins = addOrigin(passkeyOrigins);
  renderPasskeyOrigins();
  const inputs = document.querySelectorAll("#passkey-origins-list input");
  inputs[inputs.length - 1]?.focus();
});
document.getElementById("passkey-rp-suggest").addEventListener("click", (e) => {
  passkeyRpIdInput().value = e.currentTarget.dataset.value || "";
  e.currentTarget.classList.add("hidden");
});
function formatTimestamp(sec) {
  if (!sec) return "";
  return new Date(sec * 1e3).toLocaleString();
}
function askLdapLogin() {
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
    const finish = (identity) => {
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
          body: JSON.stringify({ method: "ldap", username: user.value.trim(), password: pass.value })
        });
        pass.value = "";
        if (res.ok) {
          const body = await res.json().catch(() => null);
          finish(body?.identity || user.value.trim());
          modal.close();
          return;
        }
        err.textContent = res.status === 401 ? "Invalid username or password, or that account isn't in an allowed group." : res.status === 429 ? "Too many attempts. Wait a moment and try again." : res.status === 400 ? "LDAP sign-in isn't available for this session. Sign in to admin again." : "Sign-in failed (" + res.status + ").";
      } catch {
        err.textContent = "Couldn't reach the server.";
      }
      submit.disabled = false;
    });
  });
}
function applyPasskeyStatus(res) {
  const state = passkeyCardState(res.status, errorText(res, ""));
  passkeysLocked = state.kind === "locked";
  passkeysUnconfigured = state.kind === "unconfigured" ? state.text : "";
}
async function unlockPasskeys() {
  const identity = await askLdapLogin();
  if (identity === null) return false;
  const res = await api("GET", "/api/auth/passkeys");
  applyPasskeyStatus(res);
  passkeysLocked = !res.ok && !passkeysUnconfigured;
  passkeys = res.ok && res.data && res.data.passkeys || [];
  renderPasskeys();
  if (!res.ok) {
    showToast(errorText(res, "Signed in as " + identity + ", but passkeys are still unavailable."), "error");
    return false;
  }
  showToast("Signed in as " + identity + ".", "success");
  return true;
}
function renderPasskeys() {
  const list = document.getElementById("passkeys-list");
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
        document.getElementById("panel-passkey-settings").scrollIntoView({ behavior: "smooth" });
        passkeyRpIdInput().focus();
      });
      notice.appendChild(link);
    }
    notice.classList.toggle("hidden", !passkeysUnconfigured);
  }
  if (submitBtn) {
    submitBtn.textContent = passkeysLocked ? "Sign in with LDAP\u2026" : "Register new passkey";
    submitBtn.disabled = !!passkeysUnconfigured;
    submitBtn.title = passkeysUnconfigured;
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
    li.innerHTML = `<span class="named-list-name">${esc(name)}</span><span class="named-list-value">${esc(created ? "added " + created : "")}${lastUsed ? esc(", last used " + lastUsed) : ""}</span><button type="button" class="btn btn-danger btn-sm btn-remove" data-id="${esc(p.id)}">Delete</button>`;
    list.appendChild(li);
  });
  list.querySelectorAll(".btn-remove").forEach((btn) => {
    btn.addEventListener("click", () => deletePasskey(btn.dataset.id));
  });
}
async function deletePasskey(id) {
  const errorEl = document.getElementById("passkey-error");
  errorEl.textContent = "";
  const res = await api("DELETE", "/api/auth/passkeys/" + encodeURIComponent(id));
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to delete passkey.");
    return;
  }
  passkeys = passkeys.filter((p) => p.id !== id);
  renderPasskeys();
}
function b64urlToBuffer(value) {
  const normalized = value.replace(/-/g, "+").replace(/_/g, "/");
  const padded = normalized + "===".slice((normalized.length + 3) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}
function bufferToB64url(value) {
  if (!value) return null;
  const bytes = new Uint8Array(value);
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/g, "");
}
function creationOptions(options) {
  const publicKey = Object.assign({}, options.publicKey);
  publicKey.challenge = b64urlToBuffer(publicKey.challenge);
  if (publicKey.user) {
    publicKey.user = Object.assign({}, publicKey.user, {
      id: b64urlToBuffer(publicKey.user.id)
    });
  }
  if (Array.isArray(publicKey.excludeCredentials)) {
    publicKey.excludeCredentials = publicKey.excludeCredentials.map(
      (c) => Object.assign({}, c, { id: b64urlToBuffer(c.id) })
    );
  }
  return publicKey;
}
function attestationToJSON(credential) {
  const response = credential.response;
  return {
    id: credential.id,
    rawId: bufferToB64url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment,
    response: {
      attestationObject: bufferToB64url(response.attestationObject),
      clientDataJSON: bufferToB64url(response.clientDataJSON)
    },
    clientExtensionResults: credential.getClientExtensionResults()
  };
}
var passkeySupported = window.isSecureContext && !!window.PublicKeyCredential && !!navigator.credentials;
if (!passkeySupported) {
  const form = document.getElementById("passkey-form");
  const btn = form.querySelector("button");
  btn.disabled = true;
  const why = "Passkey registration is disabled: this page is served over plain HTTP. Browsers only allow passkeys (WebAuthn) over HTTPS or on localhost. Serve the admin module over HTTPS (or access it as localhost) to enable this.";
  btn.title = why;
  form.title = why;
  const note = document.getElementById("passkey-https-note");
  note.textContent = why;
  note.classList.remove("hidden");
}
async function registerPasskey(friendlyName, retried = false) {
  const errorEl = document.getElementById("passkey-error");
  const fail = (msg) => {
    errorEl.textContent = msg;
    showToast(msg, "error");
    return false;
  };
  if (passkeysUnconfigured) return fail(passkeysUnconfigured);
  if (passkeysLocked && !await unlockPasskeys()) return false;
  const beginRes = await api(
    "POST",
    "/api/auth/passkey/register/begin",
    { friendlyName }
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
  const ceremony = beginRes.data;
  let credential;
  try {
    credential = await navigator.credentials.create({
      publicKey: creationOptions(ceremony.options)
    });
  } catch {
    return fail("Passkey registration canceled.");
  }
  if (!credential) return fail("Passkey registration canceled.");
  const finishRes = await api("POST", "/api/auth/passkey/register/finish", {
    challengeId: ceremony.challengeId,
    friendlyName,
    credential: attestationToJSON(credential)
  });
  if (!finishRes.ok) return fail(errorText(finishRes, "Passkey registration failed."));
  passkeys.push(finishRes.data);
  renderPasskeys();
  showToast('Passkey "' + (finishRes.data.friendlyName || friendlyName || "unnamed") + '" registered.', "success");
  return true;
}
document.getElementById("passkey-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  if (!passkeySupported) return;
  document.getElementById("passkey-error").textContent = "";
  const nameInput = document.getElementById("passkey-name");
  if (await registerPasskey(nameInput.value.trim())) nameInput.value = "";
});
function renderOperatorPin() {
  const el = document.getElementById("operator-pin-status");
  const adminPin = authConfig.admin_pin || {};
  if (adminPin.defined_by === "config") {
    el.textContent = "Operator PIN: Configured (via inline config).";
  } else if (adminPin.defined_by === "file") {
    el.textContent = "Operator PIN: Configured (via " + adminPin.path + ").";
  } else {
    el.textContent = "Operator PIN: Not set \u2014 the admin module has its own PIN, separate from module PINs.";
  }
  const file = currentAdminPinFilePath();
  const nameEl = document.getElementById("operator-pin-file-name");
  nameEl.textContent = file ? baseName(file) : "no key file";
  nameEl.title = file;
}
document.getElementById("operator-pin-change-file").addEventListener("click", async () => {
  const statusEl2 = document.getElementById("operator-pin-file-status");
  const errorEl = document.getElementById("operator-pin-error");
  statusEl2.textContent = "";
  errorEl.textContent = "";
  const selectedPath = await pickServerFile("Choose the Operator PIN file", currentAdminPinFilePath());
  if (!selectedPath) return;
  const toSave = buildModulesPayload({ pin_file: selectedPath });
  const res = await api(
    "PUT",
    "/api/config/modules",
    toSave
  );
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to change PIN file.");
    return;
  }
  authConfig.modules = res.data && res.data.modules || toSave;
  Object.keys(matrix).forEach((m) => {
    const saved = authConfig.modules[m];
    if (!saved && !matrix[m].protected) return;
    matrix[m].pinFile = saved && saved.pin_file || "";
    matrix[m].idle = saved && saved.idle_minutes || 0;
  });
  renderMatrix();
  renderOperatorPin();
  statusEl2.textContent = "Saved.";
  setTimeout(() => {
    statusEl2.textContent = "";
  }, 3e3);
});
document.getElementById("operator-pin-set-btn").addEventListener("click", () => {
  const form = document.getElementById("operator-pin-form");
  const input = document.getElementById("operator-pin-value");
  form.classList.toggle("hidden");
  input.value = "";
  if (!form.classList.contains("hidden")) input.focus();
});
document.getElementById("operator-pin-cancel").addEventListener("click", () => {
  document.getElementById("operator-pin-form").classList.add("hidden");
  document.getElementById("operator-pin-value").value = "";
});
document.getElementById("operator-pin-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  const errorEl = document.getElementById("operator-pin-error");
  const statusEl2 = document.getElementById("operator-pin-file-status");
  errorEl.textContent = "";
  statusEl2.textContent = "";
  const input = document.getElementById("operator-pin-value");
  const pin = input.value;
  const currentPath = currentAdminPinFilePath();
  const payload = currentPath ? { path: currentPath, pin } : { name: "admin.pin", pin };
  const res = await api("POST", "/api/config/pin-files", payload);
  input.value = "";
  if (!res.ok) {
    errorEl.textContent = errorText(res, "Unable to set PIN.");
    return;
  }
  const toSave = buildModulesPayload({ pin_file: res.data.path });
  const modRes = await api(
    "PUT",
    "/api/config/modules",
    toSave
  );
  if (!modRes.ok) {
    errorEl.textContent = errorText(
      modRes,
      "PIN set, but unable to update the admin pin file reference."
    );
    return;
  }
  authConfig.modules = modRes.data && modRes.data.modules || toSave;
  Object.keys(matrix).forEach((m) => {
    const saved = authConfig.modules[m];
    if (!saved && !matrix[m].protected) return;
    matrix[m].pinFile = saved && saved.pin_file || "";
    matrix[m].idle = saved && saved.idle_minutes || 0;
  });
  renderMatrix();
  renderOperatorPin();
  document.getElementById("operator-pin-form").classList.add("hidden");
  statusEl2.textContent = "Saved.";
  setTimeout(() => {
    statusEl2.textContent = "";
  }, 3e3);
});
function buildHamburger() {
  const items = [];
  return new HamburgerMenu({
    title: "Admin",
    items,
    themePicker: true,
    themes,
    side: "right"
  });
}
var hamburger = buildHamburger();
var topbar = document.querySelector(".topbar");
var logoutBtn = document.getElementById("logout-btn");
if (logoutBtn) logoutBtn.remove();
var topbarActions = document.createElement("div");
topbarActions.className = "topbar-actions";
topbarActions.append(hamburger.trigger);
topbar.appendChild(topbarActions);
loadAll();
