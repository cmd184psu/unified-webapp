/**
 * The import wizard (FR-8): step 1 dry-run preview (`GET
 * /api/import/preview`), step 2 confirm (only shown when the cert list is
 * already non-empty), step 3 execute (`POST /api/import`) and its report --
 * including the failed-import case, which must render the error, the
 * partial classification the scan reached, and an accurate statement of what
 * was and was not written -- the root CA commits before the leaf transaction
 * opens, so "nothing was written" is not automatically true -- with a path back
 * to retry. The wizard never sends a manifest of
 * what to import -- only the one `confirmNonEmpty` flag -- so preview and
 * execute can never disagree about which files exist on disk.
 */

import { fetchImportPreview, runImport, fetchCA, ImportFailedError } from "./api";
import type { ImportItem, ImportReport } from "./types";
import { showToast } from "./toast";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function reportSummary(report: ImportReport): string {
  return `${report.importable} importable, ${report.expired} expired, ${report.broken} broken, ${report.skipped} skipped`;
}

function buildItemList(items: ImportItem[]): HTMLElement {
  const list = el("ul", "cert-wizard-items");
  if (items.length === 0) {
    const empty = el("li", "cert-wizard-item-empty");
    empty.textContent = "No legacy certificate directories found.";
    list.append(empty);
    return list;
  }
  for (const item of items) {
    const li = el("li", "cert-wizard-item");
    li.dataset.status = item.status;
    const path = el("span", "cert-wizard-item-path");
    path.textContent = item.path;
    const status = el("span", "cert-wizard-item-status");
    status.textContent = item.status;
    li.append(path, status);
    if (item.reason !== undefined) {
      const reason = el("p", "cert-wizard-item-reason");
      reason.textContent = item.reason;
      li.append(reason);
    }
    list.append(li);
  }
  return list;
}

function buildStrayFiles(files: string[]): HTMLElement | null {
  if (files.length === 0) return null;
  const wrap = el("div", "cert-wizard-stray");
  const heading = el("p", "cert-wizard-stray-heading");
  heading.textContent = "Stray files found (not certificate directories, not imported):";
  const list = el("ul", "cert-wizard-stray-list");
  for (const f of files) {
    const li = el("li");
    li.textContent = f;
    list.append(li);
  }
  wrap.append(heading, list);
  return wrap;
}

/**
 * Replace the failure note with a statement about the root CA once its actual
 * state is known. `fetchCA` never rejects (it degrades to "no CA"), so the worst
 * case is the note staying as written rather than an unhandled rejection.
 */
async function refineWrittenNote(note: HTMLElement): Promise<void> {
  const ca = await fetchCA();
  if (!ca.exists) {
    note.textContent =
      "Nothing was written: no certificate authority and no certificate rows. Fix the issue above and retry.";
    return;
  }
  if (ca.importedFrom !== undefined) {
    note.textContent =
      "The legacy root CA was imported -- that step commits before the leaf import -- but no certificate rows " +
      "were written. Fix the issue above and retry; the CA is skipped on a re-run because its fingerprint matches.";
    return;
  }
  note.textContent =
    "No certificate rows were written -- the leaf import is a single transaction and it rolled back. " +
    "The existing certificate authority is unchanged.";
}

/**
 * Open the import wizard modal. `certCount` decides whether step 2's
 * explicit confirmation is required (the server refuses `POST /api/import`
 * without `confirmNonEmpty` whenever the cert list is already non-empty --
 * `ErrImportConfirmRequired`). `onImported` runs after a successful execute
 * so the caller can refresh the cert list, CA panel, and config.
 */
export function openImportWizard(certCount: number, onImported: () => void): void {
  const overlay = el("div", "cert-modal-overlay");
  const dialog = el("div", "cert-modal");
  dialog.setAttribute("role", "dialog");
  dialog.setAttribute("aria-modal", "true");
  dialog.setAttribute("aria-label", "Import legacy certificates");

  const header = el("div", "cert-modal-header");
  const title = el("h2", "cert-modal-title");
  title.textContent = "Import legacy certificates";
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

  function renderStep1(): void {
    body.textContent = "";
    footer.textContent = "";
    const loading = el("p", "cert-modal-note");
    loading.textContent = "Scanning the legacy directory…";
    body.append(loading);

    fetchImportPreview()
      .then((report) => {
        body.textContent = "";
        const stepLabel = el("p", "cert-wizard-step");
        stepLabel.textContent = "Step 1 of 2 — preview (nothing has been written yet)";
        const summary = el("p", "cert-wizard-summary");
        summary.textContent = reportSummary(report);
        body.append(stepLabel, summary, buildItemList(report.items));
        const stray = buildStrayFiles(report.strayFiles);
        if (stray) body.append(stray);

        const cancelBtn = el("button", "cert-btn cert-btn-secondary");
        cancelBtn.type = "button";
        cancelBtn.textContent = "Cancel";
        cancelBtn.addEventListener("click", close);

        const proceedBtn = el("button", "cert-btn cert-btn-primary");
        proceedBtn.type = "button";
        proceedBtn.textContent = "Import now";
        // Broken directories are importable too: FR-3's import-everything
        // principle stores them as quarantined rows carrying their reason, which
        // is the whole point -- a tree of nothing but broken leaves is exactly
        // the tree an operator most needs inventoried. Gating on
        // importable+expired alone left "Import now" dead in that case with no
        // explanation. Step 2's confirmation still applies unchanged.
        proceedBtn.disabled = report.importable === 0 && report.expired === 0 && report.broken === 0;
        proceedBtn.addEventListener("click", () => renderStep2(certCount > 0));

        footer.append(cancelBtn, proceedBtn);
      })
      .catch((err: unknown) => {
        body.textContent = "";
        const error = el("p", "cert-modal-error");
        error.textContent = err instanceof Error ? err.message : String(err);
        body.append(error);

        const retryBtn = el("button", "cert-btn cert-btn-primary");
        retryBtn.type = "button";
        retryBtn.textContent = "Retry";
        retryBtn.addEventListener("click", renderStep1);
        footer.append(retryBtn);
      });
  }

  function renderStep2(needsConfirm: boolean): void {
    if (!needsConfirm) {
      execute(false);
      return;
    }

    body.textContent = "";
    footer.textContent = "";

    const stepLabel = el("p", "cert-wizard-step");
    stepLabel.textContent = "Step 2 of 2 — confirm";
    const warn = el("p", "cert-modal-note cert-modal-note-warn");
    warn.textContent =
      "The certificate list already has entries. Confirm to run the import anyway -- " +
      "certificates already imported are skipped, nothing existing is overwritten.";
    body.append(stepLabel, warn);

    const backBtn = el("button", "cert-btn cert-btn-secondary");
    backBtn.type = "button";
    backBtn.textContent = "Back";
    backBtn.addEventListener("click", renderStep1);

    const confirmBtn = el("button", "cert-btn cert-btn-primary");
    confirmBtn.type = "button";
    confirmBtn.textContent = "Confirm import";
    confirmBtn.addEventListener("click", () => execute(true));

    footer.append(backBtn, confirmBtn);
  }

  function execute(confirmNonEmpty: boolean): void {
    body.textContent = "";
    footer.textContent = "";
    const loading = el("p", "cert-modal-note");
    loading.textContent = "Importing…";
    body.append(loading);

    runImport(confirmNonEmpty)
      .then((report) => {
        body.textContent = "";
        const stepLabel = el("p", "cert-wizard-step");
        stepLabel.textContent = "Import complete";
        const summary = el("p", "cert-wizard-summary");
        summary.textContent = reportSummary(report);
        body.append(stepLabel, summary, buildItemList(report.items));
        const stray = buildStrayFiles(report.strayFiles);
        if (stray) body.append(stray);

        const doneBtn = el("button", "cert-btn cert-btn-primary");
        doneBtn.type = "button";
        doneBtn.textContent = "Done";
        doneBtn.addEventListener("click", close);
        footer.append(doneBtn);

        showToast(`Import complete: ${reportSummary(report)}`, "success");
        onImported();
      })
      .catch((err: unknown) => {
        body.textContent = "";
        const message = err instanceof Error ? err.message : String(err);

        const stepLabel = el("p", "cert-wizard-step");
        stepLabel.textContent = "Import failed";
        const error = el("p", "cert-modal-error");
        error.textContent = message;
        const note = el("p", "cert-modal-note");
        // Deliberately not "Nothing was written": the root CA is imported and
        // committed *before* the leaf transaction opens, so on a leaf failure the
        // CA may well be in the database already. Telling the operator nothing
        // happened would send them looking for a CA that is actually there --
        // and the CA is the one row with no delete route, so a wrong belief
        // about it is expensive. What is guaranteed is that the leaf half is
        // all-or-nothing; refineWrittenNote() below replaces this with the
        // stronger statement once the CA's real state is known.
        note.textContent =
          "No certificate rows were written -- the leaf import is a single transaction and it rolled back.";
        body.append(stepLabel, error, note);

        // The partial classification: the only part of the response that names
        // which directory the import stopped on.
        const report = err instanceof ImportFailedError ? err.report : null;
        if (report !== null) {
          const partial = el("p", "cert-wizard-summary");
          partial.textContent = `Partial scan: ${reportSummary(report)}`;
          body.append(partial, buildItemList(report.items));
          const stray = buildStrayFiles(report.strayFiles);
          if (stray) body.append(stray);
        }

        void refineWrittenNote(note);

        const cancelBtn = el("button", "cert-btn cert-btn-secondary");
        cancelBtn.type = "button";
        cancelBtn.textContent = "Cancel";
        cancelBtn.addEventListener("click", close);

        const retryBtn = el("button", "cert-btn cert-btn-primary");
        retryBtn.type = "button";
        retryBtn.textContent = "Retry";
        retryBtn.addEventListener("click", renderStep1);

        footer.append(cancelBtn, retryBtn);
      });
  }

  renderStep1();
}
