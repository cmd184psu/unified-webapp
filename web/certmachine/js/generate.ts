/**
 * The "Generate certificate" form (FR-4): a modal with a primary FQDN field
 * and repeatable DNS/IP SAN rows. Deliberately has no validity field --
 * validity is always `default_validity_days` from server config (revision
 * log R-M4); the operator cannot request a longer or shorter lifetime.
 *
 * Client-side pre-validation exists only to catch the cheap, obvious
 * mistakes (empty FQDN, malformed IP, the 64-SAN cap) before spending a
 * round trip. Everything else -- wildcard shape, ASCII, normalization,
 * duplicate-active -- is left to the server, and the server's own error
 * text is what gets displayed when it rejects (FR-9: no alert()-and-hope).
 */

import { generateCert } from "./api";
import type { CertMutationResponse } from "./types";
import { showToast } from "./toast";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function isValidIPv4(s: string): boolean {
  const parts = s.split(".");
  if (parts.length !== 4) return false;
  return parts.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255 && String(Number(p)) === p);
}

/**
 * IPv6 shape check. The previous version tested `/^[0-9a-fA-F:]+$/`, which
 * rejects every address containing an embedded IPv4 tail *and*, worse, said
 * nothing about group counts -- but the real bug was the opposite direction:
 * it accepted `:::` and `1::2::3` while the *form* also had to accept the
 * compressed forms an operator actually types (`::1`, `2001:db8::1`). Since
 * this check only exists to save a round trip, it must never reject something
 * `net.ParseIP` (the server's authority) accepts, which is exactly what it did.
 *
 * Exported for `generate.test.ts`.
 */
export function isValidIPv6(s: string): boolean {
  if (!s.includes(":") || s.length > 45) return false;

  // "::" may appear at most once; splitting on it yields 2 parts when it does.
  const halves = s.split("::");
  if (halves.length > 2) return false;
  const compressed = halves.length === 2;

  const groupsOf = (half: string): string[] => (half === "" ? [] : half.split(":"));
  const groups = [...groupsOf(halves[0]), ...(compressed ? groupsOf(halves[1]) : [])];

  let count = 0;
  for (let i = 0; i < groups.length; i++) {
    const group = groups[i];
    // An embedded IPv4 tail ("::ffff:192.0.2.1") is legal only as the final
    // group, and occupies two 16-bit groups.
    if (i === groups.length - 1 && group.includes(".")) {
      if (!isValidIPv4(group)) return false;
      count += 2;
      continue;
    }
    if (!/^[0-9a-fA-F]{1,4}$/.test(group)) return false;
    count += 1;
  }

  // A compressed address stands for at least one elided zero group, so it can
  // never spell out all 8; an uncompressed one must spell out exactly 8.
  return compressed ? count <= 7 : count === 8;
}

function isValidIP(s: string): boolean {
  return isValidIPv4(s) || isValidIPv6(s);
}

/** FR-4's SAN cap, mirroring `maxSANCount` in internal/certmachine/pki.go. */
export const MAX_SAN_COUNT = 64;

/** Client-side mirror of the server's `NormalizeFQDN` for dedupe purposes
 * only: trim, strip one trailing dot, lowercase. The ASCII/wildcard rejections
 * stay server-side -- this exists solely so the client counts the same set the
 * server will count. */
function normalizeForCount(raw: string): string {
  return raw.trim().replace(/\.$/, "").toLowerCase();
}

/**
 * Both of the server's SAN-cap checks (`ValidateRequest`, pki.go): the cheap
 * raw-array check, then the authoritative deduplicated count -- which includes
 * the primary FQDN, because `GenerateLeaf` always injects it as a DNS SAN.
 * Counting only `dnsSans.length + ipSans.length` left the client one short of
 * the server, so a request with exactly 64 SANs plus the FQDN passed here and
 * was rejected there -- the form's own validation handing the operator a
 * server error it existed to prevent. Returns the message to show, or `null`.
 *
 * Exported for `generate.test.ts`.
 */
export function sanCountError(fqdn: string, dnsSans: string[], ipSans: string[]): string | null {
  const raw = dnsSans.length + ipSans.length;
  if (raw > MAX_SAN_COUNT) {
    return `Too many Subject Alternative Names: ${raw} (maximum ${MAX_SAN_COUNT}).`;
  }
  const unique = new Set<string>([normalizeForCount(fqdn), ...dnsSans.map(normalizeForCount)]);
  const total = unique.size + ipSans.length;
  if (total > MAX_SAN_COUNT) {
    return `Too many Subject Alternative Names: ${total} including the FQDN itself (maximum ${MAX_SAN_COUNT}).`;
  }
  return null;
}

function daysBetween(startISO: string, endISO: string): number {
  const ms = new Date(endISO).getTime() - new Date(startISO).getTime();
  return Math.round(ms / (24 * 60 * 60 * 1000));
}

/**
 * Build the clamped-validity notice text for a generate/renew response, or
 * `null` when the response wasn't clamped. Exported so `detail.ts`'s renew
 * action reuses this exact wording instead of inventing its own -- a cert
 * quietly shorter than the configured default is precisely the kind of
 * silent deviation the plan's Principle 4 exists to prevent.
 *
 * `resp.requestedNotAfter` (sent only when clamped) is the expiry the cert
 * would have had; naming it turns "shorter than you asked for" into a concrete
 * pair of dates the operator can act on -- and is the reason the server sends
 * the field at all, which until now nothing displayed.
 */
export function clampedNoticeText(
  defaultValidityDays: number,
  resp: CertMutationResponse,
): string | null {
  if (!resp.validityClamped) return null;
  const { notBefore, notAfter } = resp.cert;
  const actualDays = notBefore !== null && notAfter !== null ? daysBetween(notBefore, notAfter) : null;
  const dateStr = notAfter !== null ? notAfter.slice(0, 10) : "unknown";
  const daysPart = actualDays !== null ? `${actualDays} days` : "a shorter validity";
  const requested =
    resp.requestedNotAfter !== undefined
      ? ` The full ${defaultValidityDays} days would have run to ${resp.requestedNotAfter.slice(0, 10)}; renew the CA to get there.`
      : "";
  return `Issued for ${daysPart} instead of ${defaultValidityDays}: the CA expires ${dateStr}.${requested}`;
}

/** One repeatable list of text inputs (DNS names or IP addresses), with add/remove rows. */
function buildSanRows(
  container: HTMLElement,
  kind: "dns" | "ip",
): { getValues: () => string[] } {
  const rows: HTMLInputElement[] = [];
  const list = el("div", "cert-san-list");

  function addRow(): void {
    const row = el("div", "cert-san-row");
    const input = el("input", "cert-field-input");
    input.type = "text";
    input.placeholder = kind === "dns" ? "www.example.local" : "10.0.0.1";
    input.autocomplete = "off";

    const removeBtn = el("button", "cert-san-remove");
    removeBtn.type = "button";
    removeBtn.textContent = "−";
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

  const addBtn = el("button", "cert-san-add");
  addBtn.type = "button";
  addBtn.textContent = kind === "dns" ? "+ Add DNS name" : "+ Add IP address";
  addBtn.addEventListener("click", addRow);

  container.append(list, addBtn);

  return {
    getValues: () => rows.map((r) => r.value.trim()).filter((v) => v !== ""),
  };
}

/**
 * Open the "Generate certificate" modal. `onCreated` runs after a
 * successful generate (the modal is already closed by then) so the caller
 * can refresh the cert list and CA panel.
 */
export function openGenerateForm(defaultValidityDays: number, onCreated: () => void): void {
  const overlay = el("div", "cert-modal-overlay");
  const dialog = el("div", "cert-modal");
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.setAttribute("aria-label", "Generate certificate");

  const header = el("div", "cert-modal-header");
  const title = el("h2", "cert-modal-title");
  title.textContent = "Generate certificate";
  const closeBtn = el("button", "cert-modal-close");
  closeBtn.type = "button";
  closeBtn.textContent = "×";
  closeBtn.setAttribute("aria-label", "Close");
  header.append(title, closeBtn);

  const body = el("div", "cert-modal-body");
  const form = el("form", "cert-form");

  const fqdnField = el("div", "cert-field");
  const fqdnLabel = el("label", "cert-field-label");
  fqdnLabel.textContent = "FQDN";
  const fqdnInput = el("input", "cert-field-input");
  fqdnInput.type = "text";
  fqdnInput.placeholder = "example.local";
  fqdnInput.autocomplete = "off";
  fqdnLabel.append(fqdnInput);
  fqdnField.append(fqdnLabel);
  form.append(fqdnField);

  const dnsField = el("div", "cert-field");
  const dnsLabel = el("p", "cert-field-label");
  dnsLabel.textContent = "DNS Subject Alternative Names (optional)";
  dnsField.append(dnsLabel);
  const dnsRows = buildSanRows(dnsField, "dns");
  form.append(dnsField);

  const ipField = el("div", "cert-field");
  const ipLabel = el("p", "cert-field-label");
  ipLabel.textContent = "IP Subject Alternative Names (optional)";
  ipField.append(ipLabel);
  const ipRows = buildSanRows(ipField, "ip");
  form.append(ipField);

  const errorText = el("p", "cert-field-error");
  errorText.hidden = true;
  form.append(errorText);

  const footer = el("div", "cert-modal-footer");
  const cancelBtn = el("button", "cert-btn cert-btn-secondary");
  cancelBtn.type = "button";
  cancelBtn.textContent = "Cancel";
  const submitBtn = el("button", "cert-btn cert-btn-primary");
  submitBtn.type = "submit";
  submitBtn.textContent = "Generate";
  footer.append(cancelBtn, submitBtn);
  form.append(footer);

  body.append(form);
  dialog.append(header, body);
  overlay.append(dialog);
  document.body.append(overlay);

  const close = (): void => {
    document.removeEventListener("keydown", onKey);
    overlay.remove();
  };
  const onKey = (e: KeyboardEvent): void => {
    if (e.key === "Escape") close();
  };
  document.addEventListener("keydown", onKey);
  overlay.addEventListener("click", (e) => {
    if (e.target === overlay) close();
  });
  closeBtn.addEventListener("click", close);
  cancelBtn.addEventListener("click", close);

  function showError(message: string): void {
    errorText.textContent = message;
    errorText.hidden = false;
  }

  form.addEventListener("submit", (e) => {
    e.preventDefault();
    errorText.hidden = true;

    const fqdn = fqdnInput.value.trim();
    if (fqdn === "") {
      showError("FQDN is required.");
      return;
    }

    const dnsSans = dnsRows.getValues();
    const ipSans = ipRows.getValues();

    const invalidIp = ipSans.find((ip) => !isValidIP(ip));
    if (invalidIp !== undefined) {
      showError(`"${invalidIp}" is not a valid IP address.`);
      return;
    }

    const sanError = sanCountError(fqdn, dnsSans, ipSans);
    if (sanError !== null) {
      showError(sanError);
      return;
    }

    submitBtn.disabled = true;
    generateCert({ fqdn, dnsSans, ipSans })
      .then((resp) => {
        close();
        const notice = clampedNoticeText(defaultValidityDays, resp);
        showToast(
          notice
            ? `Generated ${resp.cert.fqdn}. ${notice}`
            : `Generated ${resp.cert.fqdn}.`,
          notice ? "notice" : "success",
        );
        onCreated();
      })
      .catch((err: unknown) => {
        submitBtn.disabled = false;
        showError(err instanceof Error ? err.message : String(err));
      });
  });

  fqdnInput.focus();
}
