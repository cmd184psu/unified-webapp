/**
 * The cert detail modal (FR-9.4): full parsed metadata (CN/FQDN, all SANs,
 * serial, SHA-256 fingerprint, validity window, computed status,
 * `importedFrom`, `quarantineReason`/`importWarning` when present),
 * per-file downloads, copy-PEM, and the renew/delete actions.
 *
 * Opened from a row's "Details" button (`render.ts`), never by making the
 * `.cert-fqdn` span itself interactive -- that node's "always zero child
 * elements, textContent only" invariant from slice 9 is left untouched.
 */

import type { AppConfig, Cert } from "./types";
import { fetchCertDetail, renewCert, deleteCert, editCert } from "./api";
import type { EditInput } from "./api";
import { badgeFor, BADGE_LABEL } from "./status";
import { clampedNoticeText, isValidIP, sanCountError } from "./generate";
import { showToast, createCopyButton } from "@shared";

/**
 * The single sentence appended to a mutation's own success toast whenever
 * its response reports `previousDropped: true` (CA-replacement plan §4).
 * Appended, not a second toast, because `showToast`'s stack could bury one
 * toast under the other -- this is folded into the single message instead.
 */
const PREVIOUS_DROPPED_NOTICE =
  "The previous CA no longer signed any active certificate and was removed, along with its archived certificates.";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function formatDate(iso: string | null): string {
  if (iso === null) return "unknown";
  const date = iso.slice(0, 10);
  return date.length === 10 ? date : iso;
}

function addMetaRow(dl: HTMLDListElement, label: string, value: string): void {
  const dt = el("dt", "cert-detail-key");
  dt.textContent = label;
  const dd = el("dd", "cert-detail-value");
  dd.textContent = value;
  dl.append(dt, dd);
}

/**
 * One repeatable list of text inputs (DNS names or IP addresses), pre-filled
 * with `initial` values, for the Edit form. Mirrors `generate.ts`'s
 * `buildSanRows`, which has no pre-fill parameter (the generate form always
 * starts empty) -- duplicated here rather than widened there, since adding
 * an unused parameter to that module's one call site would be pure churn.
 */
function buildEditableSanRows(
  container: HTMLElement,
  kind: "dns" | "ip",
  initial: string[],
): { getValues: () => string[] } {
  const rows: HTMLInputElement[] = [];
  const list = el("div", "cert-san-list");

  function addRow(value: string): void {
    const row = el("div", "cert-san-row");
    const input = el("input", "cert-field-input");
    input.type = "text";
    input.placeholder = kind === "dns" ? "www.example.local" : "10.0.0.1";
    input.autocomplete = "off";
    input.value = value;

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
  addBtn.addEventListener("click", () => addRow(""));

  container.append(list, addBtn);
  for (const value of initial) addRow(value);

  return {
    getValues: () => rows.map((r) => r.value.trim()).filter((v) => v !== ""),
  };
}

function buildSanList(heading: string, values: string[]): HTMLElement {
  const wrap = el("div", "cert-detail-sans-group");
  const h = el("p", "cert-detail-sans-heading");
  h.textContent = `${heading} (${values.length})`;
  wrap.append(h);
  if (values.length === 0) {
    const empty = el("p", "cert-detail-sans-empty");
    empty.textContent = "None.";
    wrap.append(empty);
    return wrap;
  }
  const list = el("ul", "cert-detail-sans-list");
  for (const v of values) {
    const li = el("li");
    li.textContent = v;
    list.append(li);
  }
  wrap.append(list);
  return wrap;
}

export interface DetailCallbacks {
  /** Invoked after a successful renew or delete so the caller can refresh the list/CA panel. */
  onChanged: () => void;
}

/** Open the certificate detail modal for row `id`. */
export function openCertDetail(
  id: number,
  config: AppConfig,
  now: Date,
  callbacks: DetailCallbacks,
): void {
  const overlay = el("div", "cert-modal-overlay");
  const dialog = el("div", "cert-modal");
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.setAttribute("aria-label", "Certificate detail");

  const header = el("div", "cert-modal-header");
  const title = el("h2", "cert-modal-title");
  title.textContent = "Certificate detail";
  const closeBtn = el("button", "cert-modal-close");
  closeBtn.type = "button";
  closeBtn.textContent = "×";
  closeBtn.setAttribute("aria-label", "Close");
  header.append(title, closeBtn);

  const body = el("div", "cert-modal-body");
  const footer = el("div", "cert-modal-footer");
  dialog.append(header, body, footer);
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

  const loading = el("p", "cert-modal-note");
  loading.textContent = "Loading…";
  body.append(loading);

  fetchCertDetail(id)
    .then((cert) => renderDetail(cert))
    .catch((err: unknown) => {
      body.textContent = "";
      const error = el("p", "cert-modal-error");
      error.textContent = err instanceof Error ? err.message : String(err);
      body.append(error);
    });

  function renderDetail(cert: Cert): void {
    body.textContent = "";
    footer.textContent = "";

    const kind = badgeFor(cert.notAfter, cert.status, config.expiryWarnDays, now);

    const heading = el("div", "cert-detail-heading");
    const fqdn = el("span", "cert-fqdn");
    fqdn.textContent = cert.fqdn;
    const badge = el("span", "cert-badge");
    badge.dataset.kind = kind;
    badge.textContent = BADGE_LABEL[kind];
    heading.append(fqdn, badge);
    // Stale is independent of the expiry-derived `kind` badge above (CA-replacement plan FR-R4).
    if (cert.stale) {
      const staleBadge = el("span", "cert-badge");
      staleBadge.dataset.kind = "stale";
      staleBadge.textContent = "Stale";
      heading.append(staleBadge);
    }
    body.append(heading);

    const meta = el("dl", "cert-detail-meta");
    addMetaRow(meta, "Common Name", cert.fqdn);
    addMetaRow(meta, "Signing CA", cert.caSubject ?? (cert.caId === null ? "unknown signer" : "unknown"));
    addMetaRow(meta, "Serial", cert.serial ?? "unknown (certificate did not parse)");
    addMetaRow(meta, "SHA-256 fingerprint", cert.fingerprint ?? "unknown");
    addMetaRow(meta, "Valid from", formatDate(cert.notBefore));
    addMetaRow(meta, "Valid until", formatDate(cert.notAfter));
    addMetaRow(meta, "Created", formatDate(cert.created));
    if (cert.importedFrom !== undefined) addMetaRow(meta, "Imported from", cert.importedFrom);
    body.append(meta);

    body.append(buildSanList("DNS names", cert.sans.dns), buildSanList("IP addresses", cert.sans.ip));

    if (cert.quarantineReason !== undefined) {
      const note = el("p", "cert-row-note");
      note.dataset.tone = "danger";
      note.textContent = `Quarantined: ${cert.quarantineReason}`;
      body.append(note);
    }
    if (cert.importWarning !== undefined) {
      const note = el("p", "cert-row-note");
      note.dataset.tone = "warn";
      note.textContent = cert.importWarning;
      body.append(note);
    }

    const downloads = el("div", "cert-detail-downloads");
    // Each download is offered only when the server can actually produce it.
    // haproxy.pem and the bundle are assembled from cert+key against the CA and
    // are refused outright for a quarantined row (ErrQuarantinedDownload);
    // cert.pem 404s when the row has no stored certificate (a quarantined row
    // whose source file was not a certificate at all). These are plain anchors,
    // so a click on one that fails replaces the SPA with a raw JSON error page
    // -- the reason belongs on a disabled span instead.
    const quarantined = cert.quarantineReason;
    const files: Array<[string, string, string | null]> = [
      [
        "cert.pem",
        `/api/certs/${cert.id}/files/cert.pem`,
        cert.certPem === undefined ? "Unavailable: this row has no stored certificate" : null,
      ],
      ["key.pem", `/api/certs/${cert.id}/files/key.pem`, null],
      [
        "haproxy.pem",
        `/api/certs/${cert.id}/files/haproxy.pem`,
        quarantined === undefined ? null : `Unavailable: quarantined -- ${quarantined}`,
      ],
      [
        "bundle (.tgz)",
        `/api/certs/${cert.id}/bundle`,
        quarantined === undefined ? null : `Unavailable: quarantined -- ${quarantined}`,
      ],
    ];
    for (const [label, href, unavailable] of files) {
      if (unavailable !== null) {
        const span = el("span", "cert-action cert-action-disabled");
        span.textContent = label;
        span.title = unavailable;
        span.setAttribute("aria-disabled", "true");
        downloads.append(span);
        continue;
      }
      const a = el("a", "cert-action");
      a.href = href;
      a.textContent = label;
      downloads.append(a);
    }

    const copyBtn = createCopyButton({ text: () => cert.certPem ?? "", label: "cert.pem", className: "cert-action cert-action-btn" });
    if (cert.certPem === undefined) {
      copyBtn.disabled = true;
      copyBtn.title = "Nothing to copy: this certificate has no readable PEM.";
    }
    downloads.append(copyBtn);
    body.append(downloads);

    const renewBtn = el("button", "cert-btn cert-btn-primary");
    renewBtn.type = "button";
    renewBtn.textContent = "Re-issue";
    renewBtn.addEventListener("click", () => {
      renewBtn.disabled = true;
      renewCert(cert.id)
        .then((resp) => {
          const notice = clampedNoticeText(config.defaultValidityDays, resp);
          let message = notice ? `Renewed ${cert.fqdn}. ${notice}` : `Renewed ${cert.fqdn}.`;
          if (resp.previousDropped) message += ` ${PREVIOUS_DROPPED_NOTICE}`;
          showToast(message, notice ? "notice" : "success");
          close();
          callbacks.onChanged();
        })
        .catch((err: unknown) => {
          renewBtn.disabled = false;
          showToast(err instanceof Error ? err.message : String(err), "error");
        });
    });

    const deleteBtn = el("button", "cert-btn cert-btn-danger");
    deleteBtn.type = "button";
    deleteBtn.textContent = "Delete…";
    deleteBtn.addEventListener("click", () => renderDeleteConfirm(cert));

    footer.append(renewBtn);
    // Edit is available on every non-quarantined cert (D4); a quarantined
    // row's source did not parse into a usable request in the first place,
    // and the server refuses it with ErrQuarantined regardless.
    if (cert.quarantineReason === undefined) {
      const editBtn = el("button", "cert-btn cert-btn-secondary");
      editBtn.type = "button";
      editBtn.textContent = "Edit…";
      editBtn.addEventListener("click", () => renderEditForm(cert));
      footer.append(editBtn);
    }
    footer.append(deleteBtn);
  }

  function renderEditForm(cert: Cert): void {
    body.textContent = "";
    footer.textContent = "";

    const form = el("form", "cert-form");
    // The Save button lives in the modal footer, outside this form, so it must
    // name its form owner explicitly or it submits nothing.
    form.id = "cert-edit-form";

    const fqdnField = el("div", "cert-field");
    const fqdnLabel = el("label", "cert-field-label");
    fqdnLabel.textContent = "FQDN";
    const fqdnInput = el("input", "cert-field-input");
    fqdnInput.type = "text";
    fqdnInput.value = cert.fqdn;
    fqdnInput.autocomplete = "off";
    fqdnLabel.append(fqdnInput);
    fqdnField.append(fqdnLabel);
    form.append(fqdnField);

    const dnsField = el("div", "cert-field");
    const dnsLabel = el("p", "cert-field-label");
    dnsLabel.textContent = "DNS Subject Alternative Names (optional)";
    dnsField.append(dnsLabel);
    const dnsRows = buildEditableSanRows(dnsField, "dns", cert.sans.dns);
    form.append(dnsField);

    const ipField = el("div", "cert-field");
    const ipLabel = el("p", "cert-field-label");
    ipLabel.textContent = "IP Subject Alternative Names (optional)";
    ipField.append(ipLabel);
    const ipRows = buildEditableSanRows(ipField, "ip", cert.sans.ip);
    form.append(ipField);

    const validityField = el("div", "cert-field");
    const validityLabel = el("label", "cert-field-label");
    validityLabel.textContent = "Validity (days)";
    const validityInput = el("input", "cert-field-input");
    validityInput.type = "number";
    validityInput.min = "1";
    validityInput.max = "3650";
    // Defaults to the server's own default_validity_days (GET /api/config),
    // per the plan -- the FR-4 narrowing lets Edit request a validity, but
    // it should start from the same default every other issuance route uses.
    validityInput.value = String(config.defaultValidityDays);
    validityLabel.append(validityInput);
    validityField.append(validityLabel);
    form.append(validityField);

    const errorText = el("p", "cert-field-error");
    errorText.hidden = true;
    form.append(errorText);

    body.append(form);

    const cancelBtn = el("button", "cert-btn cert-btn-secondary");
    cancelBtn.type = "button";
    cancelBtn.textContent = "Cancel";
    cancelBtn.addEventListener("click", () => renderDetail(cert));
    const submitBtn = el("button", "cert-btn cert-btn-primary");
    submitBtn.type = "submit";
    submitBtn.setAttribute("form", form.id);
    submitBtn.textContent = "Save";
    footer.append(cancelBtn, submitBtn);

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
      const validityDays = parseInt(validityInput.value, 10);
      if (!Number.isFinite(validityDays) || validityDays < 1 || validityDays > 3650) {
        showError("Validity must be a whole number of days between 1 and 3650.");
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

      const input: EditInput = { fqdn, dnsSans, ipSans, validityDays };
      submitBtn.disabled = true;
      editCert(cert.id, input)
        .then((resp) => {
          const notice = clampedNoticeText(validityDays, resp);
          let message = notice ? `Edited ${resp.cert.fqdn}. ${notice}` : `Edited ${resp.cert.fqdn}.`;
          if (resp.previousDropped) message += ` ${PREVIOUS_DROPPED_NOTICE}`;
          showToast(message, notice ? "notice" : "success");
          close();
          callbacks.onChanged();
        })
        .catch((err: unknown) => {
          submitBtn.disabled = false;
          showError(err instanceof Error ? err.message : String(err));
        });
    });
  }

  function renderDeleteConfirm(cert: Cert): void {
    footer.textContent = "";

    const confirmWrap = el("div", "cert-delete-confirm");
    const label = el("label", "cert-field-label");
    label.textContent = `Type "${cert.fqdn}" to confirm deletion (case does not matter):`;
    const input = el("input", "cert-field-input");
    input.type = "text";
    input.autocomplete = "off";
    label.append(input);
    confirmWrap.append(label);
    body.append(confirmWrap);

    const cancelBtn = el("button", "cert-btn cert-btn-secondary");
    cancelBtn.type = "button";
    cancelBtn.textContent = "Cancel";
    cancelBtn.addEventListener("click", () => {
      confirmWrap.remove();
      renderDetail(cert);
    });

    const confirmBtn = el("button", "cert-btn cert-btn-danger");
    confirmBtn.type = "button";
    confirmBtn.textContent = "Delete permanently";
    confirmBtn.disabled = true;
    input.addEventListener("input", () => {
      confirmBtn.disabled = input.value.trim().toLowerCase() !== cert.fqdn.toLowerCase();
    });
    confirmBtn.addEventListener("click", () => {
      confirmBtn.disabled = true;
      deleteCert(cert.id, input.value.trim())
        .then((resp) => {
          const message = resp.previousDropped
            ? `Deleted ${cert.fqdn}. ${PREVIOUS_DROPPED_NOTICE}`
            : `Deleted ${cert.fqdn}.`;
          showToast(message, "success");
          close();
          callbacks.onChanged();
        })
        .catch((err: unknown) => {
          confirmBtn.disabled = false;
          showToast(err instanceof Error ? err.message : String(err), "error");
        });
    });

    footer.append(cancelBtn, confirmBtn);
    input.focus();
  }
}
