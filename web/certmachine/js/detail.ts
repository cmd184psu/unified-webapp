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
import { fetchCertDetail, renewCert, deleteCert } from "./api";
import { badgeFor, BADGE_LABEL } from "./status";
import { clampedNoticeText } from "./generate";
import { showToast } from "./toast";

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

/** Copy text to the clipboard. The Clipboard API needs a secure context and
 * this module is reachable over plain HTTP, so a hidden-textarea +
 * `execCommand('copy')` fallback is required, not optional. */
async function copyText(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text);
      return true;
    }
  } catch {
    /* fall through to the legacy path */
  }
  try {
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.append(ta);
    ta.select();
    const ok = document.execCommand("copy");
    ta.remove();
    return ok;
  } catch {
    return false;
  }
}

function addMetaRow(dl: HTMLDListElement, label: string, value: string): void {
  const dt = el("dt", "cert-detail-key");
  dt.textContent = label;
  const dd = el("dd", "cert-detail-value");
  dd.textContent = value;
  dl.append(dt, dd);
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
    body.append(heading);

    const meta = el("dl", "cert-detail-meta");
    addMetaRow(meta, "Common Name", cert.fqdn);
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

    const copyBtn = el("button", "cert-action cert-action-btn");
    copyBtn.type = "button";
    copyBtn.textContent = "Copy cert.pem";
    copyBtn.addEventListener("click", () => {
      if (cert.certPem === undefined) {
        showToast("Nothing to copy: this certificate has no readable PEM.", "error");
        return;
      }
      void copyText(cert.certPem).then((ok) => {
        showToast(
          ok ? "cert.pem copied to clipboard." : "Copy failed — select and copy the file instead.",
          ok ? "success" : "error",
        );
      });
    });
    downloads.append(copyBtn);
    body.append(downloads);

    const renewBtn = el("button", "cert-btn cert-btn-primary");
    renewBtn.type = "button";
    renewBtn.textContent = "Renew";
    renewBtn.addEventListener("click", () => {
      renewBtn.disabled = true;
      renewCert(cert.id)
        .then((resp) => {
          const notice = clampedNoticeText(config.defaultValidityDays, resp);
          showToast(
            notice ? `Renewed ${cert.fqdn}. ${notice}` : `Renewed ${cert.fqdn}.`,
            notice ? "notice" : "success",
          );
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

    footer.append(renewBtn, deleteBtn);
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
        .then(() => {
          showToast(`Deleted ${cert.fqdn}.`, "success");
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
