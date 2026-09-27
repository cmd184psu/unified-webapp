// web/certmachine/js/main.ts
import { ThemeManager, HamburgerMenu } from "/shared/dist/shared.mjs";

// web/certmachine/js/api.ts
var FALLBACK_CONFIG = {
  defaultValidityDays: 365,
  expiryWarnDays: 30,
  certCount: 0,
  legacyImportAvailable: false,
  legacyImportDir: "",
  legacyImportReason: "",
  trustDeviceAvailable: false,
  trustRemoteAvailable: false
};
async function fetchConfig() {
  try {
    const res = await fetch("/api/config");
    if (!res.ok) throw new Error(`config fetch failed: ${res.status}`);
    return await res.json();
  } catch (err) {
    console.warn("certmachine: falling back to default config:", err);
    return FALLBACK_CONFIG;
  }
}
async function errorMessage(res, fallback) {
  try {
    const body = await res.json();
    if (body.error) return body.error;
  } catch {
  }
  return `${fallback}: ${res.status}`;
}
async function fetchCerts() {
  const res = await fetch("/api/certs");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to load certificates"));
  }
  const body = await res.json();
  return body.certs ?? [];
}
async function fetchCertDetail(id) {
  const res = await fetch(`/api/certs/${id}`);
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to load certificate"));
  }
  return await res.json();
}
async function generateCert(input) {
  const res = await fetch("/api/certs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input)
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to generate certificate"));
  }
  return await res.json();
}
async function renewCert(id) {
  const res = await fetch(`/api/certs/${id}/renew`, { method: "POST" });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to renew certificate"));
  }
  return await res.json();
}
async function deleteCert(id, confirmFqdn) {
  const res = await fetch(`/api/certs/${id}?confirm=${encodeURIComponent(confirmFqdn)}`, {
    method: "DELETE"
  });
  if (!res.ok && res.status !== 204) {
    throw new Error(await errorMessage(res, "failed to delete certificate"));
  }
}
var FALLBACK_CA_STATUS = { exists: false };
async function fetchCA() {
  try {
    const res = await fetch("/api/ca");
    if (!res.ok) throw new Error(`CA status fetch failed: ${res.status}`);
    return await res.json();
  } catch (err) {
    console.warn("certmachine: falling back to 'no CA' status:", err);
    return FALLBACK_CA_STATUS;
  }
}
async function initCA(name = "") {
  const res = await fetch("/api/ca/init", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name })
  });
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to initialize the certificate authority"));
  }
  return await res.json();
}
async function fetchImportPreview() {
  const res = await fetch("/api/import/preview");
  if (!res.ok) {
    throw new Error(await errorMessage(res, "failed to preview the legacy import"));
  }
  return await res.json();
}
var ImportFailedError = class extends Error {
  constructor(message, report) {
    super(message);
    this.name = "ImportFailedError";
    this.report = report;
  }
};
async function runImport(confirmNonEmpty) {
  const res = await fetch("/api/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(confirmNonEmpty ? { confirmNonEmpty: true } : {})
  });
  if (!res.ok) {
    let message = `import failed: ${res.status}`;
    let report = null;
    try {
      const body = await res.json();
      if (body.error) message = body.error;
      if (body.report) report = body.report;
    } catch {
    }
    throw new ImportFailedError(message, report);
  }
  return await res.json();
}
var TrustFailedError = class extends Error {
  constructor(message, output) {
    super(message);
    this.name = "TrustFailedError";
    this.output = output;
  }
};
async function trustRemote(req) {
  const res = await fetch("/api/ca/trust/remote", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(req)
  });
  let body = {};
  try {
    body = await res.json();
  } catch {
  }
  if (!res.ok) {
    throw new TrustFailedError(body.error ?? `remote trust failed: ${res.status}`, body.output ?? "");
  }
  return body;
}
async function fetchSSHKeys() {
  try {
    const res = await fetch("/api/ssh/keys");
    if (!res.ok) return [];
    const body = await res.json();
    return body.keys ?? [];
  } catch {
    return [];
  }
}
async function trustDevice() {
  const res = await fetch("/api/ca/trust", { method: "POST" });
  let body = {};
  try {
    body = await res.json();
  } catch {
  }
  if (!res.ok) {
    throw new TrustFailedError(body.error ?? `device trust install failed: ${res.status}`, body.output ?? "");
  }
  return body;
}

// web/certmachine/js/trustdialog.ts
import { openModal, showToast } from "/shared/dist/shared.mjs";
function el(tag, className) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}
var PLATFORM_NAMES = {
  darwin: "macOS",
  rhel: "Rocky/RHEL",
  debian: "Ubuntu/Debian",
  windows: "Windows"
};
function isLikelyPrivateKey(name) {
  if (name.endsWith(".pub")) return false;
  return !["known_hosts", "known_hosts.old", "config", "authorized_keys", "authorized_keys2"].includes(name);
}
function field(label, input) {
  const wrap = el("label", "cert-field");
  const span = el("span", "cert-field-label");
  span.textContent = label;
  wrap.append(span, input);
  return wrap;
}
function radio(name, value, text, checked) {
  const label = el("label", "cert-trust-radio");
  const input = el("input");
  input.type = "radio";
  input.name = name;
  input.value = value;
  input.checked = checked;
  label.append(input, document.createTextNode(text));
  return { label, input };
}
function openTrustDialog(config) {
  const localOK = config.trustDeviceAvailable;
  const remoteOK = config.trustRemoteAvailable;
  const content = el("div", "cert-trust-dialog");
  const targetRow = el("div", "cert-trust-targets");
  const localRadio = radio("trust-target", "local", `This server${config.trustPlatform ? ` (${PLATFORM_NAMES[config.trustPlatform] ?? config.trustPlatform})` : ""}`, localOK && !remoteOK);
  const remoteRadio = radio("trust-target", "remote", "Another machine over SSH", remoteOK);
  localRadio.input.disabled = !localOK;
  remoteRadio.input.disabled = !remoteOK;
  if (!localOK) localRadio.label.title = "Not enabled on this server (certmachine.trust_device_enabled), or its OS isn't supported.";
  if (!remoteOK) remoteRadio.label.title = `Unavailable: ${config.trustRemoteReason ?? "SSH isn't configured"}`;
  targetRow.append(localRadio.label, remoteRadio.label);
  content.append(targetRow);
  const sshBox = el("div", "cert-trust-ssh");
  const host = el("input", "cert-field-input");
  host.placeholder = "hostname or IP";
  host.autocomplete = "off";
  const port = el("input", "cert-field-input cert-trust-port");
  port.type = "number";
  port.min = "1";
  port.max = "65535";
  port.value = "22";
  const user = el("input", "cert-field-input");
  user.placeholder = "username";
  user.autocomplete = "off";
  const hostRow = el("div", "cert-trust-row");
  hostRow.append(field("Hostname", host), field("Port", port));
  const authRow = el("div", "cert-trust-auth");
  const keyRadio = radio("trust-auth", "key", "SSH key", true);
  const pwRadio = radio("trust-auth", "password", "Password", false);
  authRow.append(keyRadio.label, pwRadio.label);
  const keySelect = el("select", "cert-field-input");
  const keyField = field("Key (from the server's ~/.ssh)", keySelect);
  const password = el("input", "cert-field-input");
  password.type = "password";
  password.autocomplete = "off";
  const pwField = field("Password", password);
  pwField.hidden = true;
  const sudoNote = el("p", "cert-ca-note");
  sudoNote.textContent = "Installing needs admin rights: log in as root (Administrator on Windows), or as a user with passwordless sudo.";
  sshBox.append(hostRow, field("Username", user), authRow, keyField, pwField, sudoNote);
  content.append(sshBox);
  void fetchSSHKeys().then((keys) => {
    const usable = keys.filter((k) => !k.isDir && isLikelyPrivateKey(k.name));
    keySelect.textContent = "";
    if (usable.length === 0) {
      const opt = el("option");
      opt.value = "";
      opt.textContent = "No keys found";
      keySelect.append(opt);
      return;
    }
    for (const k of usable) {
      const opt = el("option");
      opt.value = k.name;
      opt.textContent = k.name;
      keySelect.append(opt);
    }
  });
  const status = el("p", "cert-trust-status");
  const output = el("pre", "cert-trust-output");
  output.hidden = true;
  const actions = el("div", "cert-trust-actions");
  const cancelBtn = el("button", "cert-btn");
  cancelBtn.type = "button";
  cancelBtn.textContent = "Close";
  const trustBtn = el("button", "cert-btn cert-btn-primary");
  trustBtn.type = "button";
  trustBtn.textContent = "Trust";
  actions.append(cancelBtn, trustBtn);
  content.append(status, output, actions);
  const sync = () => {
    sshBox.hidden = !remoteRadio.input.checked;
    const usePw = pwRadio.input.checked;
    keyField.hidden = usePw;
    pwField.hidden = !usePw;
  };
  for (const r of [localRadio, remoteRadio, keyRadio, pwRadio]) r.input.addEventListener("change", sync);
  sync();
  const modal = openModal(content, { title: "Trust this CA" });
  cancelBtn.addEventListener("click", () => modal.close());
  const show = (result, err) => {
    const text = err instanceof TrustFailedError ? err.output : result?.output;
    output.textContent = text ?? "";
    output.hidden = !text;
  };
  trustBtn.addEventListener("click", async () => {
    let request;
    let where;
    if (remoteRadio.input.checked) {
      if (!host.value.trim() || !user.value.trim()) {
        status.textContent = "Enter a hostname and a username.";
        return;
      }
      const usePw = pwRadio.input.checked;
      if (usePw ? !password.value : !keySelect.value) {
        status.textContent = usePw ? "Enter the password." : "Choose an SSH key.";
        return;
      }
      where = host.value.trim();
      request = trustRemote({
        host: where,
        port: parseInt(port.value, 10) || 22,
        user: user.value.trim(),
        ...usePw ? { password: password.value } : { key: keySelect.value }
      });
      status.textContent = `Connecting to ${where} and detecting its OS\u2026`;
    } else {
      where = "this server";
      request = trustDevice();
      status.textContent = "Installing on this server\u2026";
    }
    trustBtn.disabled = true;
    output.hidden = true;
    try {
      const result = await request;
      const os = result.platform ? ` (${PLATFORM_NAMES[result.platform] ?? result.platform})` : "";
      status.textContent = `Trusted on ${where}${os}.`;
      show(result, null);
      showToast(`CA trusted on ${where}${os}`, "success");
    } catch (err) {
      status.textContent = err instanceof Error ? err.message : String(err);
      show(null, err);
    } finally {
      trustBtn.disabled = false;
    }
  });
}

// web/certmachine/js/status.ts
var BADGE_LABEL = {
  quarantined: "Quarantined",
  archived: "Archived",
  expired: "Expired",
  "expiring-soon": "Expiring soon",
  valid: "Valid"
};
function isDeemphasized(kind) {
  return kind === "expired" || kind === "archived";
}
function badgeFor(notAfter, status, warnDays, now) {
  if (status === "quarantined") return "quarantined";
  if (status === "archived") return "archived";
  if (notAfter === null) return "valid";
  const notAfterMs = new Date(notAfter).getTime();
  if (Number.isNaN(notAfterMs)) return "valid";
  const nowMs = now.getTime();
  if (notAfterMs < nowMs) return "expired";
  const warnMs = warnDays * 24 * 60 * 60 * 1e3;
  if (notAfterMs <= nowMs + warnMs) return "expiring-soon";
  return "valid";
}
var NO_DOMAIN_GROUP = "(no domain)";
function domainGroup(fqdn) {
  const labels = fqdn.split(".").filter((l) => l.length > 0);
  if (labels.length < 2) return NO_DOMAIN_GROUP;
  return labels.slice(-2).join(".");
}

// web/certmachine/js/listmodel.ts
function filterCerts(certs, query) {
  const q = query.trim().toLowerCase();
  if (q === "") return certs;
  return certs.filter((cert) => {
    if (cert.fqdn.toLowerCase().includes(q)) return true;
    if (cert.sans.dns.some((d) => d.toLowerCase().includes(q))) return true;
    if (cert.sans.ip.some((ip) => ip.toLowerCase().includes(q))) return true;
    return false;
  });
}
function compareExpiry(a, b, dir) {
  if (a.notAfter === null && b.notAfter === null) return 0;
  if (a.notAfter === null) return 1;
  if (b.notAfter === null) return -1;
  const cmp = new Date(a.notAfter).getTime() - new Date(b.notAfter).getTime();
  return dir === "asc" ? cmp : -cmp;
}
function sortCerts(certs, key, dir) {
  const sign = dir === "asc" ? 1 : -1;
  const sorted = [...certs];
  sorted.sort((a, b) => {
    switch (key) {
      case "name":
        return sign * a.fqdn.toLowerCase().localeCompare(b.fqdn.toLowerCase());
      case "created":
        return sign * (a.created < b.created ? -1 : a.created > b.created ? 1 : 0);
      case "expiry":
        return compareExpiry(a, b, dir);
    }
  });
  return sorted;
}
function groupCerts(certs) {
  const order = [];
  const buckets = /* @__PURE__ */ new Map();
  for (const cert of certs) {
    const key = domainGroup(cert.fqdn);
    const bucket = buckets.get(key);
    if (bucket) {
      bucket.push(cert);
    } else {
      buckets.set(key, [cert]);
      order.push(key);
    }
  }
  return order.map((domain) => ({ domain, certs: buckets.get(domain) }));
}

// web/certmachine/js/render.ts
function el2(tag, className) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}
function formatDate(iso) {
  if (iso === null) return "unknown";
  const date = iso.slice(0, 10);
  return date.length === 10 ? date : iso;
}
function badgeElement(kind) {
  const badge = el2("span", "cert-badge");
  badge.dataset.kind = kind;
  badge.textContent = BADGE_LABEL[kind];
  return badge;
}
function buildCertRow(cert, kind, onOpenDetail) {
  const row = el2("li", "cert-row");
  row.dataset.kind = kind;
  const main = el2("div", "cert-row-main");
  const fqdn = el2("span", "cert-fqdn");
  fqdn.textContent = cert.fqdn;
  main.append(fqdn, badgeElement(kind));
  const meta = el2("div", "cert-row-meta");
  const expiry = el2("span", "cert-meta-item");
  expiry.textContent = kind === "expired" ? `Expired ${formatDate(cert.notAfter)}` : `Expires ${formatDate(cert.notAfter)}`;
  meta.append(expiry);
  if (cert.importedFrom !== void 0) {
    const imported = el2("span", "cert-meta-item cert-meta-imported");
    imported.textContent = "Imported";
    imported.title = cert.importedFrom;
    meta.append(imported);
  }
  row.append(main, meta);
  if (cert.quarantineReason !== void 0) {
    const note = el2("p", "cert-row-note");
    note.dataset.tone = "danger";
    note.textContent = `Quarantined: ${cert.quarantineReason}`;
    row.append(note);
  }
  if (cert.importWarning !== void 0) {
    const note = el2("p", "cert-row-note");
    note.dataset.tone = "warn";
    note.textContent = cert.importWarning;
    row.append(note);
  }
  const actions = el2("div", "cert-row-actions");
  const details = el2("button", "cert-action cert-action-btn");
  details.type = "button";
  details.textContent = "Details";
  details.addEventListener("click", () => onOpenDetail(cert.id));
  actions.append(details);
  for (const [label, href] of downloadActions(cert.id)) {
    actions.append(
      cert.quarantineReason === void 0 ? downloadLink(label, href) : disabledAction(label, `Unavailable: quarantined -- ${cert.quarantineReason}`)
    );
  }
  row.append(actions);
  return row;
}
function downloadActions(id) {
  return [
    ["haproxy.pem", `/api/certs/${id}/files/haproxy.pem`],
    ["bundle (.tgz)", `/api/certs/${id}/bundle`]
  ];
}
function downloadLink(label, href) {
  const a = el2("a", "cert-action");
  a.href = href;
  a.textContent = label;
  return a;
}
function disabledAction(label, reason) {
  const span = el2("span", "cert-action cert-action-disabled");
  span.textContent = label;
  span.title = reason;
  span.setAttribute("aria-disabled", "true");
  return span;
}
function emptyState(message) {
  const p = el2("p", "cert-empty");
  p.textContent = message;
  return p;
}
function appendRowGroup(target, certs, warnDays, now, onOpenDetail, emptyMessage, emptyPrimaryMessage) {
  if (certs.length === 0) {
    target.appendChild(emptyState(emptyMessage));
    return;
  }
  const primary = [];
  const deemphasized = [];
  for (const cert of certs) {
    const kind = badgeFor(cert.notAfter, cert.status, warnDays, now);
    const row = buildCertRow(cert, kind, onOpenDetail);
    (isDeemphasized(kind) ? deemphasized : primary).push(row);
  }
  const primaryList = el2("ul", "cert-list");
  primaryList.append(...primary);
  if (primary.length === 0) {
    target.appendChild(emptyState(emptyPrimaryMessage));
  } else {
    target.appendChild(primaryList);
  }
  if (deemphasized.length > 0) {
    const toggle = el2("button", "cert-toggle");
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", "false");
    const deemphasizedList = el2("ul", "cert-list cert-list-deemphasized");
    deemphasizedList.hidden = true;
    deemphasizedList.append(...deemphasized);
    const label = (expanded) => `${expanded ? "Hide" : "Show"} ${deemphasized.length} expired/archived certificate${deemphasized.length === 1 ? "" : "s"}`;
    toggle.textContent = label(false);
    toggle.addEventListener("click", () => {
      const expanded = deemphasizedList.hidden;
      deemphasizedList.hidden = !expanded;
      toggle.setAttribute("aria-expanded", String(expanded));
      toggle.textContent = label(expanded);
    });
    target.append(toggle, deemphasizedList);
  }
}
function renderCertList(container, certs, warnDays, now, options) {
  container.textContent = "";
  if (certs.length === 0) {
    container.appendChild(
      emptyState(options.hasAnyCerts ? "No certificates match your search." : "No certificates yet.")
    );
    return;
  }
  const fragment = document.createDocumentFragment();
  if (!options.groupByDomain) {
    appendRowGroup(
      fragment,
      certs,
      warnDays,
      now,
      options.onOpenDetail,
      "No certificates match your search.",
      "No active certificates -- everything is expired or archived."
    );
  } else {
    for (const group of groupCerts(certs)) {
      const section = el2("section", "cert-group");
      const groupBody = el2("div", "cert-group-body");
      const headingBtn = el2("button", "cert-group-heading");
      headingBtn.type = "button";
      headingBtn.setAttribute("aria-expanded", "true");
      const headingText = `${group.domain} (${group.certs.length})`;
      headingBtn.textContent = headingText;
      headingBtn.addEventListener("click", () => {
        const expanded = headingBtn.getAttribute("aria-expanded") === "true";
        headingBtn.setAttribute("aria-expanded", String(!expanded));
        groupBody.hidden = expanded;
      });
      appendRowGroup(
        groupBody,
        group.certs,
        warnDays,
        now,
        options.onOpenDetail,
        "No certificates match your search.",
        "No active certificates in this group."
      );
      section.append(headingBtn, groupBody);
      fragment.appendChild(section);
    }
  }
  container.appendChild(fragment);
}

// web/certmachine/js/generate.ts
import { showToast as showToast2 } from "/shared/dist/shared.mjs";
function el3(tag, className) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}
function isValidIPv4(s) {
  const parts = s.split(".");
  if (parts.length !== 4) return false;
  return parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255 && String(Number(p)) === p);
}
function isValidIPv6(s) {
  if (!s.includes(":") || s.length > 45) return false;
  const halves = s.split("::");
  if (halves.length > 2) return false;
  const compressed = halves.length === 2;
  const groupsOf = (half) => half === "" ? [] : half.split(":");
  const groups = [...groupsOf(halves[0]), ...compressed ? groupsOf(halves[1]) : []];
  let count = 0;
  for (let i = 0; i < groups.length; i++) {
    const group = groups[i];
    if (i === groups.length - 1 && group.includes(".")) {
      if (!isValidIPv4(group)) return false;
      count += 2;
      continue;
    }
    if (!/^[0-9a-fA-F]{1,4}$/.test(group)) return false;
    count += 1;
  }
  return compressed ? count <= 7 : count === 8;
}
function isValidIP(s) {
  return isValidIPv4(s) || isValidIPv6(s);
}
var MAX_SAN_COUNT = 64;
function normalizeForCount(raw) {
  return raw.trim().replace(/\.$/, "").toLowerCase();
}
function sanCountError(fqdn, dnsSans, ipSans) {
  const raw = dnsSans.length + ipSans.length;
  if (raw > MAX_SAN_COUNT) {
    return `Too many Subject Alternative Names: ${raw} (maximum ${MAX_SAN_COUNT}).`;
  }
  const unique = /* @__PURE__ */ new Set([normalizeForCount(fqdn), ...dnsSans.map(normalizeForCount)]);
  const total = unique.size + ipSans.length;
  if (total > MAX_SAN_COUNT) {
    return `Too many Subject Alternative Names: ${total} including the FQDN itself (maximum ${MAX_SAN_COUNT}).`;
  }
  return null;
}
function daysBetween(startISO, endISO) {
  const ms = new Date(endISO).getTime() - new Date(startISO).getTime();
  return Math.round(ms / (24 * 60 * 60 * 1e3));
}
function clampedNoticeText(defaultValidityDays, resp) {
  if (!resp.validityClamped) return null;
  const { notBefore, notAfter } = resp.cert;
  const actualDays = notBefore !== null && notAfter !== null ? daysBetween(notBefore, notAfter) : null;
  const dateStr = notAfter !== null ? notAfter.slice(0, 10) : "unknown";
  const daysPart = actualDays !== null ? `${actualDays} days` : "a shorter validity";
  const requested = resp.requestedNotAfter !== void 0 ? ` The full ${defaultValidityDays} days would have run to ${resp.requestedNotAfter.slice(0, 10)}; renew the CA to get there.` : "";
  return `Issued for ${daysPart} instead of ${defaultValidityDays}: the CA expires ${dateStr}.${requested}`;
}
function buildSanRows(container, kind) {
  const rows = [];
  const list = el3("div", "cert-san-list");
  function addRow() {
    const row = el3("div", "cert-san-row");
    const input = el3("input", "cert-field-input");
    input.type = "text";
    input.placeholder = kind === "dns" ? "www.example.local" : "10.0.0.1";
    input.autocomplete = "off";
    const removeBtn = el3("button", "cert-san-remove");
    removeBtn.type = "button";
    removeBtn.textContent = "\u2212";
    removeBtn.setAttribute("aria-label", kind === "dns" ? "Remove DNS name" : "Remove IP address");
    removeBtn.addEventListener("click", () => {
      row.remove();
      const idx = rows.indexOf(input);
      if (idx >= 0) rows.splice(idx, 1);
    });
    row.append(input, removeBtn);
    list.append(row);
    rows.push(input);
  }
  const addBtn = el3("button", "cert-san-add");
  addBtn.type = "button";
  addBtn.textContent = kind === "dns" ? "+ Add DNS name" : "+ Add IP address";
  addBtn.addEventListener("click", addRow);
  container.append(list, addBtn);
  return {
    getValues: () => rows.map((r) => r.value.trim()).filter((v) => v !== "")
  };
}
function openGenerateForm(defaultValidityDays, onCreated) {
  const overlay = el3("div", "cert-modal-overlay");
  const dialog = el3("div", "cert-modal");
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.setAttribute("aria-label", "Generate certificate");
  const header = el3("div", "cert-modal-header");
  const title = el3("h2", "cert-modal-title");
  title.textContent = "Generate certificate";
  const closeBtn = el3("button", "cert-modal-close");
  closeBtn.type = "button";
  closeBtn.textContent = "\xD7";
  closeBtn.setAttribute("aria-label", "Close");
  header.append(title, closeBtn);
  const body = el3("div", "cert-modal-body");
  const form = el3("form", "cert-form");
  const fqdnField = el3("div", "cert-field");
  const fqdnLabel = el3("label", "cert-field-label");
  fqdnLabel.textContent = "FQDN";
  const fqdnInput = el3("input", "cert-field-input");
  fqdnInput.type = "text";
  fqdnInput.placeholder = "example.local";
  fqdnInput.autocomplete = "off";
  fqdnLabel.append(fqdnInput);
  fqdnField.append(fqdnLabel);
  form.append(fqdnField);
  const dnsField = el3("div", "cert-field");
  const dnsLabel = el3("p", "cert-field-label");
  dnsLabel.textContent = "DNS Subject Alternative Names (optional)";
  dnsField.append(dnsLabel);
  const dnsRows = buildSanRows(dnsField, "dns");
  form.append(dnsField);
  const ipField = el3("div", "cert-field");
  const ipLabel = el3("p", "cert-field-label");
  ipLabel.textContent = "IP Subject Alternative Names (optional)";
  ipField.append(ipLabel);
  const ipRows = buildSanRows(ipField, "ip");
  form.append(ipField);
  const errorText2 = el3("p", "cert-field-error");
  errorText2.hidden = true;
  form.append(errorText2);
  const footer = el3("div", "cert-modal-footer");
  const cancelBtn = el3("button", "cert-btn cert-btn-secondary");
  cancelBtn.type = "button";
  cancelBtn.textContent = "Cancel";
  const submitBtn = el3("button", "cert-btn cert-btn-primary");
  submitBtn.type = "submit";
  submitBtn.textContent = "Generate";
  footer.append(cancelBtn, submitBtn);
  form.append(footer);
  body.append(form);
  dialog.append(header, body);
  overlay.append(dialog);
  document.body.append(overlay);
  const close = () => {
    document.removeEventListener("keydown", onKey);
    overlay.remove();
  };
  const onKey = (e) => {
    if (e.key === "Escape") close();
  };
  document.addEventListener("keydown", onKey);
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) close();
  });
  closeBtn.addEventListener("click", close);
  cancelBtn.addEventListener("click", close);
  function showError(message) {
    errorText2.textContent = message;
    errorText2.hidden = false;
  }
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    errorText2.hidden = true;
    const fqdn = fqdnInput.value.trim();
    if (fqdn === "") {
      showError("FQDN is required.");
      return;
    }
    const dnsSans = dnsRows.getValues();
    const ipSans = ipRows.getValues();
    const invalidIp = ipSans.find((ip) => !isValidIP(ip));
    if (invalidIp !== void 0) {
      showError(`"${invalidIp}" is not a valid IP address.`);
      return;
    }
    const sanError = sanCountError(fqdn, dnsSans, ipSans);
    if (sanError !== null) {
      showError(sanError);
      return;
    }
    submitBtn.disabled = true;
    generateCert({ fqdn, dnsSans, ipSans }).then((resp) => {
      close();
      const notice = clampedNoticeText(defaultValidityDays, resp);
      showToast2(
        notice ? `Generated ${resp.cert.fqdn}. ${notice}` : `Generated ${resp.cert.fqdn}.`,
        notice ? "notice" : "success"
      );
      onCreated();
    }).catch((err) => {
      submitBtn.disabled = false;
      showError(err instanceof Error ? err.message : String(err));
    });
  });
  fqdnInput.focus();
}

// web/certmachine/js/detail.ts
import { showToast as showToast3, createCopyButton } from "/shared/dist/shared.mjs";
function el4(tag, className) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}
function formatDate2(iso) {
  if (iso === null) return "unknown";
  const date = iso.slice(0, 10);
  return date.length === 10 ? date : iso;
}
function addMetaRow(dl, label, value) {
  const dt = el4("dt", "cert-detail-key");
  dt.textContent = label;
  const dd = el4("dd", "cert-detail-value");
  dd.textContent = value;
  dl.append(dt, dd);
}
function buildSanList(heading, values) {
  const wrap = el4("div", "cert-detail-sans-group");
  const h = el4("p", "cert-detail-sans-heading");
  h.textContent = `${heading} (${values.length})`;
  wrap.append(h);
  if (values.length === 0) {
    const empty = el4("p", "cert-detail-sans-empty");
    empty.textContent = "None.";
    wrap.append(empty);
    return wrap;
  }
  const list = el4("ul", "cert-detail-sans-list");
  for (const v of values) {
    const li = el4("li");
    li.textContent = v;
    list.append(li);
  }
  wrap.append(list);
  return wrap;
}
function openCertDetail(id, config, now, callbacks) {
  const overlay = el4("div", "cert-modal-overlay");
  const dialog = el4("div", "cert-modal");
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.setAttribute("aria-label", "Certificate detail");
  const header = el4("div", "cert-modal-header");
  const title = el4("h2", "cert-modal-title");
  title.textContent = "Certificate detail";
  const closeBtn = el4("button", "cert-modal-close");
  closeBtn.type = "button";
  closeBtn.textContent = "\xD7";
  closeBtn.setAttribute("aria-label", "Close");
  header.append(title, closeBtn);
  const body = el4("div", "cert-modal-body");
  const footer = el4("div", "cert-modal-footer");
  dialog.append(header, body, footer);
  overlay.append(dialog);
  document.body.append(overlay);
  const close = () => {
    document.removeEventListener("keydown", onKey);
    overlay.remove();
  };
  const onKey = (e) => {
    if (e.key === "Escape") close();
  };
  document.addEventListener("keydown", onKey);
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) close();
  });
  closeBtn.addEventListener("click", close);
  const loading = el4("p", "cert-modal-note");
  loading.textContent = "Loading\u2026";
  body.append(loading);
  fetchCertDetail(id).then((cert) => renderDetail(cert)).catch((err) => {
    body.textContent = "";
    const error = el4("p", "cert-modal-error");
    error.textContent = err instanceof Error ? err.message : String(err);
    body.append(error);
  });
  function renderDetail(cert) {
    body.textContent = "";
    footer.textContent = "";
    const kind = badgeFor(cert.notAfter, cert.status, config.expiryWarnDays, now);
    const heading = el4("div", "cert-detail-heading");
    const fqdn = el4("span", "cert-fqdn");
    fqdn.textContent = cert.fqdn;
    const badge = el4("span", "cert-badge");
    badge.dataset.kind = kind;
    badge.textContent = BADGE_LABEL[kind];
    heading.append(fqdn, badge);
    body.append(heading);
    const meta = el4("dl", "cert-detail-meta");
    addMetaRow(meta, "Common Name", cert.fqdn);
    addMetaRow(meta, "Serial", cert.serial ?? "unknown (certificate did not parse)");
    addMetaRow(meta, "SHA-256 fingerprint", cert.fingerprint ?? "unknown");
    addMetaRow(meta, "Valid from", formatDate2(cert.notBefore));
    addMetaRow(meta, "Valid until", formatDate2(cert.notAfter));
    addMetaRow(meta, "Created", formatDate2(cert.created));
    if (cert.importedFrom !== void 0) addMetaRow(meta, "Imported from", cert.importedFrom);
    body.append(meta);
    body.append(buildSanList("DNS names", cert.sans.dns), buildSanList("IP addresses", cert.sans.ip));
    if (cert.quarantineReason !== void 0) {
      const note = el4("p", "cert-row-note");
      note.dataset.tone = "danger";
      note.textContent = `Quarantined: ${cert.quarantineReason}`;
      body.append(note);
    }
    if (cert.importWarning !== void 0) {
      const note = el4("p", "cert-row-note");
      note.dataset.tone = "warn";
      note.textContent = cert.importWarning;
      body.append(note);
    }
    const downloads = el4("div", "cert-detail-downloads");
    const quarantined = cert.quarantineReason;
    const files = [
      [
        "cert.pem",
        `/api/certs/${cert.id}/files/cert.pem`,
        cert.certPem === void 0 ? "Unavailable: this row has no stored certificate" : null
      ],
      ["key.pem", `/api/certs/${cert.id}/files/key.pem`, null],
      [
        "haproxy.pem",
        `/api/certs/${cert.id}/files/haproxy.pem`,
        quarantined === void 0 ? null : `Unavailable: quarantined -- ${quarantined}`
      ],
      [
        "bundle (.tgz)",
        `/api/certs/${cert.id}/bundle`,
        quarantined === void 0 ? null : `Unavailable: quarantined -- ${quarantined}`
      ]
    ];
    for (const [label, href, unavailable] of files) {
      if (unavailable !== null) {
        const span = el4("span", "cert-action cert-action-disabled");
        span.textContent = label;
        span.title = unavailable;
        span.setAttribute("aria-disabled", "true");
        downloads.append(span);
        continue;
      }
      const a = el4("a", "cert-action");
      a.href = href;
      a.textContent = label;
      downloads.append(a);
    }
    const copyBtn = createCopyButton({ text: () => cert.certPem ?? "", label: "cert.pem", className: "cert-action cert-action-btn" });
    if (cert.certPem === void 0) {
      copyBtn.disabled = true;
      copyBtn.title = "Nothing to copy: this certificate has no readable PEM.";
    }
    downloads.append(copyBtn);
    body.append(downloads);
    const renewBtn = el4("button", "cert-btn cert-btn-primary");
    renewBtn.type = "button";
    renewBtn.textContent = "Renew";
    renewBtn.addEventListener("click", () => {
      renewBtn.disabled = true;
      renewCert(cert.id).then((resp) => {
        const notice = clampedNoticeText(config.defaultValidityDays, resp);
        showToast3(
          notice ? `Renewed ${cert.fqdn}. ${notice}` : `Renewed ${cert.fqdn}.`,
          notice ? "notice" : "success"
        );
        close();
        callbacks.onChanged();
      }).catch((err) => {
        renewBtn.disabled = false;
        showToast3(err instanceof Error ? err.message : String(err), "error");
      });
    });
    const deleteBtn = el4("button", "cert-btn cert-btn-danger");
    deleteBtn.type = "button";
    deleteBtn.textContent = "Delete\u2026";
    deleteBtn.addEventListener("click", () => renderDeleteConfirm(cert));
    footer.append(renewBtn, deleteBtn);
  }
  function renderDeleteConfirm(cert) {
    footer.textContent = "";
    const confirmWrap = el4("div", "cert-delete-confirm");
    const label = el4("label", "cert-field-label");
    label.textContent = `Type "${cert.fqdn}" to confirm deletion (case does not matter):`;
    const input = el4("input", "cert-field-input");
    input.type = "text";
    input.autocomplete = "off";
    label.append(input);
    confirmWrap.append(label);
    body.append(confirmWrap);
    const cancelBtn = el4("button", "cert-btn cert-btn-secondary");
    cancelBtn.type = "button";
    cancelBtn.textContent = "Cancel";
    cancelBtn.addEventListener("click", () => {
      confirmWrap.remove();
      renderDetail(cert);
    });
    const confirmBtn = el4("button", "cert-btn cert-btn-danger");
    confirmBtn.type = "button";
    confirmBtn.textContent = "Delete permanently";
    confirmBtn.disabled = true;
    input.addEventListener("input", () => {
      confirmBtn.disabled = input.value.trim().toLowerCase() !== cert.fqdn.toLowerCase();
    });
    confirmBtn.addEventListener("click", () => {
      confirmBtn.disabled = true;
      deleteCert(cert.id, input.value.trim()).then(() => {
        showToast3(`Deleted ${cert.fqdn}.`, "success");
        close();
        callbacks.onChanged();
      }).catch((err) => {
        confirmBtn.disabled = false;
        showToast3(err instanceof Error ? err.message : String(err), "error");
      });
    });
    footer.append(cancelBtn, confirmBtn);
    input.focus();
  }
}

// web/certmachine/js/wizard.ts
import { showToast as showToast4 } from "/shared/dist/shared.mjs";
function el5(tag, className) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}
function reportSummary(report) {
  return `${report.importable} importable, ${report.expired} expired, ${report.broken} broken, ${report.skipped} skipped`;
}
function buildItemList(items) {
  const list = el5("ul", "cert-wizard-items");
  if (items.length === 0) {
    const empty = el5("li", "cert-wizard-item-empty");
    empty.textContent = "No legacy certificate directories found.";
    list.append(empty);
    return list;
  }
  for (const item of items) {
    const li = el5("li", "cert-wizard-item");
    li.dataset.status = item.status;
    const path = el5("span", "cert-wizard-item-path");
    path.textContent = item.path;
    const status = el5("span", "cert-wizard-item-status");
    status.textContent = item.status;
    li.append(path, status);
    if (item.reason !== void 0) {
      const reason = el5("p", "cert-wizard-item-reason");
      reason.textContent = item.reason;
      li.append(reason);
    }
    list.append(li);
  }
  return list;
}
function buildStrayFiles(files) {
  if (files.length === 0) return null;
  const wrap = el5("div", "cert-wizard-stray");
  const heading = el5("p", "cert-wizard-stray-heading");
  heading.textContent = "Stray files found (not certificate directories, not imported):";
  const list = el5("ul", "cert-wizard-stray-list");
  for (const f of files) {
    const li = el5("li");
    li.textContent = f;
    list.append(li);
  }
  wrap.append(heading, list);
  return wrap;
}
async function refineWrittenNote(note) {
  const ca = await fetchCA();
  if (!ca.exists) {
    note.textContent = "Nothing was written: no certificate authority and no certificate rows. Fix the issue above and retry.";
    return;
  }
  if (ca.importedFrom !== void 0) {
    note.textContent = "The legacy root CA was imported -- that step commits before the leaf import -- but no certificate rows were written. Fix the issue above and retry; the CA is skipped on a re-run because its fingerprint matches.";
    return;
  }
  note.textContent = "No certificate rows were written -- the leaf import is a single transaction and it rolled back. The existing certificate authority is unchanged.";
}
function openImportWizard(certCount, onImported) {
  const overlay = el5("div", "cert-modal-overlay");
  const dialog = el5("div", "cert-modal");
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.setAttribute("aria-label", "Import legacy certificates");
  const header = el5("div", "cert-modal-header");
  const title = el5("h2", "cert-modal-title");
  title.textContent = "Import legacy certificates";
  const closeBtn = el5("button", "cert-modal-close");
  closeBtn.type = "button";
  closeBtn.textContent = "\xD7";
  closeBtn.setAttribute("aria-label", "Close");
  header.append(title, closeBtn);
  const body = el5("div", "cert-modal-body");
  const footer = el5("div", "cert-modal-footer");
  dialog.append(header, body, footer);
  overlay.append(dialog);
  document.body.append(overlay);
  const close = () => {
    document.removeEventListener("keydown", onKey);
    overlay.remove();
  };
  const onKey = (e) => {
    if (e.key === "Escape") close();
  };
  document.addEventListener("keydown", onKey);
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) close();
  });
  closeBtn.addEventListener("click", close);
  function renderStep1() {
    body.textContent = "";
    footer.textContent = "";
    const loading = el5("p", "cert-modal-note");
    loading.textContent = "Scanning the legacy directory\u2026";
    body.append(loading);
    fetchImportPreview().then((report) => {
      body.textContent = "";
      const stepLabel = el5("p", "cert-wizard-step");
      stepLabel.textContent = "Step 1 of 2 \u2014 preview (nothing has been written yet)";
      const summary = el5("p", "cert-wizard-summary");
      summary.textContent = reportSummary(report);
      body.append(stepLabel, summary, buildItemList(report.items));
      const stray = buildStrayFiles(report.strayFiles);
      if (stray) body.append(stray);
      const cancelBtn = el5("button", "cert-btn cert-btn-secondary");
      cancelBtn.type = "button";
      cancelBtn.textContent = "Cancel";
      cancelBtn.addEventListener("click", close);
      const proceedBtn = el5("button", "cert-btn cert-btn-primary");
      proceedBtn.type = "button";
      proceedBtn.textContent = "Import now";
      proceedBtn.disabled = report.importable === 0 && report.expired === 0 && report.broken === 0;
      proceedBtn.addEventListener("click", () => renderStep2(certCount > 0));
      footer.append(cancelBtn, proceedBtn);
    }).catch((err) => {
      body.textContent = "";
      const error = el5("p", "cert-modal-error");
      error.textContent = err instanceof Error ? err.message : String(err);
      body.append(error);
      const retryBtn = el5("button", "cert-btn cert-btn-primary");
      retryBtn.type = "button";
      retryBtn.textContent = "Retry";
      retryBtn.addEventListener("click", renderStep1);
      footer.append(retryBtn);
    });
  }
  function renderStep2(needsConfirm) {
    if (!needsConfirm) {
      execute(false);
      return;
    }
    body.textContent = "";
    footer.textContent = "";
    const stepLabel = el5("p", "cert-wizard-step");
    stepLabel.textContent = "Step 2 of 2 \u2014 confirm";
    const warn = el5("p", "cert-modal-note cert-modal-note-warn");
    warn.textContent = "The certificate list already has entries. Confirm to run the import anyway -- certificates already imported are skipped, nothing existing is overwritten.";
    body.append(stepLabel, warn);
    const backBtn = el5("button", "cert-btn cert-btn-secondary");
    backBtn.type = "button";
    backBtn.textContent = "Back";
    backBtn.addEventListener("click", renderStep1);
    const confirmBtn = el5("button", "cert-btn cert-btn-primary");
    confirmBtn.type = "button";
    confirmBtn.textContent = "Confirm import";
    confirmBtn.addEventListener("click", () => execute(true));
    footer.append(backBtn, confirmBtn);
  }
  function execute(confirmNonEmpty) {
    body.textContent = "";
    footer.textContent = "";
    const loading = el5("p", "cert-modal-note");
    loading.textContent = "Importing\u2026";
    body.append(loading);
    runImport(confirmNonEmpty).then((report) => {
      body.textContent = "";
      const stepLabel = el5("p", "cert-wizard-step");
      stepLabel.textContent = "Import complete";
      const summary = el5("p", "cert-wizard-summary");
      summary.textContent = reportSummary(report);
      body.append(stepLabel, summary, buildItemList(report.items));
      const stray = buildStrayFiles(report.strayFiles);
      if (stray) body.append(stray);
      const doneBtn = el5("button", "cert-btn cert-btn-primary");
      doneBtn.type = "button";
      doneBtn.textContent = "Done";
      doneBtn.addEventListener("click", close);
      footer.append(doneBtn);
      showToast4(`Import complete: ${reportSummary(report)}`, "success");
      onImported();
    }).catch((err) => {
      body.textContent = "";
      const message = err instanceof Error ? err.message : String(err);
      const stepLabel = el5("p", "cert-wizard-step");
      stepLabel.textContent = "Import failed";
      const error = el5("p", "cert-modal-error");
      error.textContent = message;
      const note = el5("p", "cert-modal-note");
      note.textContent = "No certificate rows were written -- the leaf import is a single transaction and it rolled back.";
      body.append(stepLabel, error, note);
      const report = err instanceof ImportFailedError ? err.report : null;
      if (report !== null) {
        const partial = el5("p", "cert-wizard-summary");
        partial.textContent = `Partial scan: ${reportSummary(report)}`;
        body.append(partial, buildItemList(report.items));
        const stray = buildStrayFiles(report.strayFiles);
        if (stray) body.append(stray);
      }
      void refineWrittenNote(note);
      const cancelBtn = el5("button", "cert-btn cert-btn-secondary");
      cancelBtn.type = "button";
      cancelBtn.textContent = "Cancel";
      cancelBtn.addEventListener("click", close);
      const retryBtn = el5("button", "cert-btn cert-btn-primary");
      retryBtn.type = "button";
      retryBtn.textContent = "Retry";
      retryBtn.addEventListener("click", renderStep1);
      footer.append(cancelBtn, retryBtn);
    });
  }
  renderStep1();
}

// web/certmachine/js/ui.ts
import { showToast as showToast5, promptDialog } from "/shared/dist/shared.mjs";
function el6(tag, className) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}
function errorText(err) {
  return err instanceof Error ? err.message : String(err);
}
var SORT_LABELS = {
  name: "Name",
  created: "Created",
  expiry: "Expiry"
};
function formatCADate(iso) {
  return iso !== void 0 ? iso.slice(0, 10) : "unknown";
}
function addMetaRow2(dl, label, value) {
  const dt = el6("dt", "cert-detail-key");
  dt.textContent = label;
  const dd = el6("dd", "cert-detail-value");
  dd.textContent = value;
  dl.append(dt, dd);
}
async function handleInitCA(button, onCAChanged) {
  const name = await promptDialog("Name for the new certificate authority:", {
    title: "Initialize CA",
    defaultValue: "CertMachine Root CA",
    confirmLabel: "Initialize"
  });
  if (name === null) return;
  button.disabled = true;
  initCA(name.trim()).then(() => {
    showToast5("Certificate authority initialized.", "success");
    onCAChanged();
  }).catch((err) => {
    button.disabled = false;
    showToast5(errorText(err), "error");
  });
}
function renderCAPanel(container, ca, config, onCAChanged, onOpenWizard) {
  const panel = el6("section", "cert-ca-panel");
  const heading = el6("h2", "cert-ca-heading");
  heading.textContent = "Certificate authority";
  panel.append(heading);
  if (ca.exists) {
    const meta = el6("dl", "cert-detail-meta");
    addMetaRow2(meta, "Subject", ca.subject ?? "unknown");
    addMetaRow2(meta, "Serial", ca.serial ?? "unknown");
    addMetaRow2(meta, "Valid", `${formatCADate(ca.notBefore)} \u2013 ${formatCADate(ca.notAfter)}`);
    addMetaRow2(meta, "Fingerprint", ca.fingerprint ?? "unknown");
    if (ca.importedFrom !== void 0) addMetaRow2(meta, "Imported from", ca.importedFrom);
    panel.append(meta);
    const actions = el6("div", "cert-ca-actions");
    const download = el6("a", "cert-btn");
    download.href = "/api/ca/root.crt";
    download.textContent = "Download root CA";
    actions.append(download);
    if (config.trustDeviceAvailable || config.trustRemoteAvailable) {
      const trustBtn = el6("button", "cert-btn cert-btn-secondary");
      trustBtn.type = "button";
      trustBtn.textContent = "Trust this CA\u2026";
      trustBtn.addEventListener("click", () => openTrustDialog(config));
      actions.append(trustBtn);
    }
    panel.append(actions);
    const trustNote = el6("p", "cert-ca-note");
    trustNote.textContent = "For other devices, or if the button above isn't available: manual per-OS trust instructions are in the README (and docs/certmachine.md).";
    panel.append(trustNote);
  } else {
    const preferImport = config.legacyImportAvailable && config.certCount === 0;
    const note = el6("p", "cert-ca-note");
    note.textContent = preferImport ? "No certificate authority yet. Import the existing legacy certificates to bring the current root CA forward, or start fresh." : "No certificate authority yet. Initialize one to start issuing certificates.";
    panel.append(note);
    const actions = el6("div", "cert-ca-actions");
    if (preferImport) {
      const importBtn = el6("button", "cert-btn cert-btn-primary");
      importBtn.type = "button";
      importBtn.textContent = "Import legacy certificates";
      importBtn.addEventListener("click", onOpenWizard);
      const initBtn = el6("button", "cert-btn cert-btn-secondary");
      initBtn.type = "button";
      initBtn.textContent = "Initialize a new CA instead";
      initBtn.addEventListener("click", () => void handleInitCA(initBtn, onCAChanged));
      actions.append(importBtn, initBtn);
    } else {
      const initBtn = el6("button", "cert-btn cert-btn-primary");
      initBtn.type = "button";
      initBtn.textContent = "Initialize root CA";
      initBtn.addEventListener("click", () => void handleInitCA(initBtn, onCAChanged));
      actions.append(initBtn);
    }
    panel.append(actions);
    if (config.legacyImportDir !== "" && !config.legacyImportAvailable) {
      const reason = el6("p", "cert-ca-note cert-ca-note-warn");
      reason.textContent = `Legacy import unavailable: ${config.legacyImportReason}`;
      panel.append(reason);
    }
  }
  container.append(panel);
}
function buildImportButton(config, onOpenWizard) {
  const btn = el6("button", "cert-btn");
  btn.type = "button";
  btn.textContent = "Import";
  if (config.legacyImportAvailable) {
    btn.title = "Import the legacy certificates";
    btn.addEventListener("click", onOpenWizard);
  } else {
    btn.disabled = true;
    btn.title = config.legacyImportDir !== "" ? `Nothing to import: ${config.legacyImportReason}` : "Nothing to import: no legacy import directory is configured.";
  }
  return btn;
}
async function mountCertApp(root) {
  root.textContent = "";
  root.classList.add("cert-app");
  const main = el6("main", "cert-main");
  const caPanelWrap = el6("div", "cert-ca-panel-wrap");
  const toolbar = el6("div", "cert-toolbar");
  const listWrap = el6("div", "cert-list-wrap");
  main.append(caPanelWrap, toolbar, listWrap);
  root.append(main);
  let config;
  let certs;
  let ca;
  let query = "";
  let sortKey = "name";
  let sortDir = "asc";
  let groupByDomain = false;
  function renderList() {
    const filtered = filterCerts(certs, query);
    const sorted = sortCerts(filtered, sortKey, sortDir);
    renderCertList(listWrap, sorted, config.expiryWarnDays, /* @__PURE__ */ new Date(), {
      groupByDomain,
      hasAnyCerts: certs.length > 0,
      onOpenDetail: (id) => {
        openCertDetail(id, config, /* @__PURE__ */ new Date(), {
          onChanged: () => {
            void refresh();
          }
        });
      }
    });
  }
  function renderChrome() {
    caPanelWrap.textContent = "";
    renderCAPanel(
      caPanelWrap,
      ca,
      config,
      () => {
        void refresh();
      },
      () => openImportWizard(config.certCount, () => void refresh())
    );
    toolbar.textContent = "";
    const search = el6("input", "cert-search");
    search.type = "search";
    search.placeholder = "Search FQDN or SAN\u2026";
    search.value = query;
    search.setAttribute("aria-label", "Search certificates");
    search.addEventListener("input", () => {
      query = search.value;
      renderList();
    });
    const sortSelect = el6("select", "cert-sort");
    sortSelect.setAttribute("aria-label", "Sort by");
    Object.keys(SORT_LABELS).forEach((key) => {
      const opt = el6("option");
      opt.value = key;
      opt.textContent = SORT_LABELS[key];
      if (key === sortKey) opt.selected = true;
      sortSelect.append(opt);
    });
    sortSelect.addEventListener("change", () => {
      sortKey = sortSelect.value;
      renderList();
    });
    const dirLabel = () => sortDir === "asc" ? "\u2191 Ascending" : "\u2193 Descending";
    const dirBtn = el6("button", "cert-sort-dir");
    dirBtn.type = "button";
    dirBtn.textContent = dirLabel();
    dirBtn.addEventListener("click", () => {
      sortDir = sortDir === "asc" ? "desc" : "asc";
      dirBtn.textContent = dirLabel();
      renderList();
    });
    const groupToggle = el6("label", "ui-toggle cert-group-toggle");
    const groupCheckbox = el6("input");
    groupCheckbox.type = "checkbox";
    groupCheckbox.checked = groupByDomain;
    groupCheckbox.addEventListener("change", () => {
      groupByDomain = groupCheckbox.checked;
      renderList();
    });
    groupToggle.append(groupCheckbox, el6("span", "ui-toggle-track"), document.createTextNode("Group by domain"));
    const newBtn = el6("button", "cert-btn cert-btn-primary");
    newBtn.type = "button";
    newBtn.textContent = "New certificate";
    newBtn.disabled = !ca.exists;
    newBtn.title = ca.exists ? "" : "Initialize or import a certificate authority first.";
    newBtn.addEventListener("click", () => {
      openGenerateForm(config.defaultValidityDays, () => void refresh());
    });
    const importBtn = buildImportButton(config, () => openImportWizard(config.certCount, () => void refresh()));
    toolbar.append(search, sortSelect, dirBtn, groupToggle, newBtn, importBtn);
  }
  let refreshGeneration = 0;
  async function refresh() {
    const generation = ++refreshGeneration;
    let loaded;
    try {
      loaded = await Promise.all([fetchConfig(), fetchCerts(), fetchCA()]);
    } catch (err) {
      if (generation !== refreshGeneration) return;
      toolbar.textContent = "";
      caPanelWrap.textContent = "";
      listWrap.textContent = "";
      const banner = el6("p", "cert-error");
      banner.textContent = `Failed to load certificates: ${errorText(err)}`;
      listWrap.appendChild(banner);
      return;
    }
    if (generation !== refreshGeneration) return;
    [config, certs, ca] = loaded;
    renderChrome();
    renderList();
  }
  await refresh();
}

// web/certmachine/js/main.ts
var themes = new ThemeManager({ module: "certmachine", default: "dark" });
themes.apply();
var hamburger = new HamburgerMenu({
  title: "CertMachine",
  items: [],
  themePicker: true,
  themes,
  side: "right"
});
async function bootstrap() {
  const root = document.getElementById("cert-app");
  if (!root) {
    throw new Error("missing #cert-app root element");
  }
  const header = document.createElement("header");
  header.className = "app-header";
  const title = document.createElement("h1");
  title.textContent = "CertMachine";
  header.append(title, hamburger.trigger);
  root.before(header);
  await mountCertApp(root);
}
void bootstrap();
