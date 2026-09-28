// cadialog.ts -- the CA-replacement plan's two CA-panel dialogs: "Replace
// CA..." (D1/D2/D5/D7) and the D9 "switch back" confirm, plus the shared D6
// post-mutation reminder. Built on trustdialog.ts's own pattern (an
// `openModal` content element with its own field/radio helpers) rather than
// importing from trustdialog.ts, which keeps its helpers private to its one
// module the same way generate.ts and detail.ts each keep their own `el`.

import { openModal, confirmDialog, showToast } from "@shared";
import { replaceCAWithProgress, switchBackCA } from "./api";
import type { CAStatus } from "./api";
import type { ReplaceProgressEvent } from "./types";
import { percentFor, caKeySegmentCeiling, creepToward } from "./ndjson";

/** How often the "ca-key" phase's creep timer ticks, and how much it can
 * advance per tick (capped further by creepToward's own easing so it never
 * reaches the ceiling). */
const CA_KEY_CREEP_INTERVAL_MS = 200;
const CA_KEY_CREEP_STEP = 2;

function el<K extends keyof HTMLElementTagNameMap>(tag: K, className?: string): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

function field(label: string, input: HTMLElement): HTMLLabelElement {
  const wrap = el("label", "cert-field");
  const span = el("span", "cert-field-label");
  span.textContent = label;
  wrap.append(span, input);
  return wrap;
}

function radio(
  name: string,
  value: string,
  text: string,
  checked: boolean,
): { label: HTMLLabelElement; input: HTMLInputElement } {
  const label = el("label", "cert-trust-radio");
  const input = el("input");
  input.type = "radio";
  input.name = name;
  input.value = value;
  input.checked = checked;
  label.append(input, document.createTextNode(text));
  return { label, input };
}

function errorText(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

/**
 * The aria-live status line shown next to the progress bar, per phase, with
 * the overall percent appended (owner requirement: the bar counts up to
 * 100% wherever possible, and the percent is shown alongside it).
 */
function progressStatusText(ev: ReplaceProgressEvent, percent: number): string {
  const pct = `${Math.round(percent)}%`;
  switch (ev.phase) {
    case "ca-key":
      return `Generating the new certificate authority key… ${pct}`;
    case "leaf-keys":
      return `Generating certificate keys ${ev.done ?? 0} / ${ev.total ?? 0} — ${pct}`;
    case "saving":
      return `Saving… ${pct}`;
    default:
      return pct;
  }
}

/**
 * The D6 reminder shown after a successful replace or switch-back:
 * "Machines that trusted the old CA are unchanged." It carries a button that
 * opens the existing "Trust this CA..." dialog (`onOpenTrustDialog`) so the
 * operator can act on the reminder immediately instead of hunting for the
 * button back in the CA panel.
 */
export function showD6Reminder(onOpenTrustDialog: () => void): void {
  const content = el("div", "cert-ca-reminder");
  const msg = el("p", "cert-ca-note");
  msg.textContent =
    "Machines that trusted the old CA are unchanged. Use “Trust this CA…” for the new CA and redeploy the re-issued certificates.";
  content.append(msg);

  const actions = el("div", "cert-ca-actions");
  const trustBtn = el("button", "cert-btn cert-btn-primary");
  trustBtn.type = "button";
  trustBtn.textContent = "Trust this CA…";
  const closeBtn = el("button", "cert-btn cert-btn-secondary");
  closeBtn.type = "button";
  closeBtn.textContent = "Close";
  actions.append(trustBtn, closeBtn);
  content.append(actions);

  const modal = openModal(content, { title: "Redeploy trust" });
  closeBtn.addEventListener("click", () => modal.close());
  trustBtn.addEventListener("click", () => {
    modal.close();
    onOpenTrustDialog();
  });
}

/**
 * "Replace CA..." dialog (D1). An are-you-sure step, then the form: a name
 * field, the blanket existing-certificate choice (D2), and -- only when the
 * previous CA still signs active rows (D7) -- the previousStale choice,
 * whose text names the archived-row discard (P2). `onReplaced` runs after a
 * successful replace, before the D6 reminder is shown, so the caller can
 * refresh the cert list/CA panel first.
 */
export function openReplaceCADialog(
  ca: CAStatus,
  onReplaced: () => void,
  onOpenTrustDialog: () => void,
): void {
  const content = el("div", "cert-trust-dialog");

  const confirmMsg = el("p", "ui-modal-message");
  confirmMsg.textContent =
    "This installs a new certificate authority and retires the current one. " +
    "Existing certificates keep working only if you choose to re-issue or keep them below -- " +
    "this cannot be undone once the old CA is retired.";
  content.append(confirmMsg);

  const confirmActions = el("div", "cert-trust-actions");
  const cancelBtn = el("button", "cert-btn");
  cancelBtn.type = "button";
  cancelBtn.textContent = "Cancel";
  const continueBtn = el("button", "cert-btn cert-btn-primary");
  continueBtn.type = "button";
  continueBtn.textContent = "Continue";
  confirmActions.append(cancelBtn, continueBtn);
  content.append(confirmActions);

  const modal = openModal(content, { title: "Replace CA…" });
  cancelBtn.addEventListener("click", () => modal.close());
  continueBtn.addEventListener("click", () => renderForm());

  function renderForm(): void {
    content.textContent = "";

    const nameInput = el("input", "cert-field-input");
    nameInput.type = "text";
    nameInput.placeholder = "CertMachine Root CA";
    nameInput.autocomplete = "off";
    content.append(field("Name for the new certificate authority", nameInput));

    const previous = ca.previous;
    const previousActive = previous !== undefined && previous.activeCount > 0;

    const existingHeading = el("p", "cert-field-label");
    existingHeading.textContent = "Certificates currently signed by the outgoing CA";
    content.append(existingHeading);
    const existingRow = el("div", "cert-trust-targets");
    const reissueR = radio("ca-existing", "reissue", "Re-issue under the new CA", true);
    const deleteR = radio("ca-existing", "delete", "Delete", false);
    const keepR = radio("ca-existing", "keep", "Keep as-is (marked stale)", false);
    existingRow.append(reissueR.label, deleteR.label, keepR.label);
    content.append(existingRow);

    let previousStaleReissue: { label: HTMLLabelElement; input: HTMLInputElement } | null = null;
    let previousStaleDelete: { label: HTMLLabelElement; input: HTMLInputElement } | null = null;
    if (previousActive) {
      const note = el("p", "cert-ca-note cert-ca-note-warn");
      note.textContent =
        `The previous CA still signs ${previous.activeCount} active certificate` +
        `${previous.activeCount === 1 ? "" : "s"} and is about to be removed. ` +
        "Either choice below also discards all of its archived certificates -- " +
        "with re-issue, the new copies are the only ones kept.";
      content.append(note);

      const staleRow = el("div", "cert-trust-targets");
      previousStaleReissue = radio("ca-previous-stale", "reissue", "Re-issue under the new CA", true);
      previousStaleDelete = radio("ca-previous-stale", "delete", "Delete", false);
      staleRow.append(previousStaleReissue.label, previousStaleDelete.label);
      content.append(staleRow);
    }

    if (ca.unknownSignerActiveCount > 0) {
      const unknownNote = el("p", "cert-ca-note");
      unknownNote.textContent =
        `${ca.unknownSignerActiveCount} certificate${ca.unknownSignerActiveCount === 1 ? "" : "s"} ` +
        "with an unknown signer are included in this choice.";
      content.append(unknownNote);
    }

    const switchBackWarning = el("p", "cert-ca-note cert-ca-note-warn");
    switchBackWarning.textContent =
      "The current CA will be removed once no certificate uses it, so Switch back will not be available afterwards.";
    switchBackWarning.hidden = keepR.input.checked;
    content.append(switchBackWarning);

    const syncWarning = (): void => {
      switchBackWarning.hidden = keepR.input.checked;
    };
    for (const r of [reissueR, deleteR, keepR]) r.input.addEventListener("change", syncWarning);

    const errorEl = el("p", "cert-field-error");
    errorEl.hidden = true;
    // Focusable (not by tab order) so a run that ends in error can move
    // keyboard focus straight to the message instead of leaving the
    // keyboard-user's focus stranded on whatever control setRunning(false)
    // just re-enabled.
    errorEl.tabIndex = -1;
    content.append(errorEl);

    // The progress bar (owner feedback: a blanket re-issue of dozens of
    // certs gave no sign anything was happening) -- hidden until a submit
    // is in flight. `progressBar` is always determinate (value/max=100):
    // the owner requirement is that it counts up to 100% wherever possible,
    // so every phase -- including ca-key and saving -- maps to a percent via
    // `percentFor` rather than showing an indeterminate spinner.
    // `statusEl` is aria-live so a screen reader announces each phase change
    // (and its percent) on its own.
    const progressWrap = el("div", "cert-ca-progress");
    progressWrap.hidden = true;
    const progressBar = el("progress", "cert-ca-progress-bar");
    progressBar.setAttribute("aria-label", "Replace CA progress");
    progressBar.max = 100;
    progressBar.value = 0;
    const statusEl = el("p", "cert-ca-progress-status");
    statusEl.setAttribute("aria-live", "polite");
    progressWrap.append(progressBar, statusEl);
    content.append(progressWrap);

    const actions = el("div", "cert-trust-actions");
    const backBtn = el("button", "cert-btn");
    backBtn.type = "button";
    backBtn.textContent = "Close";
    const submitBtn = el("button", "cert-btn cert-btn-danger");
    submitBtn.type = "button";
    submitBtn.textContent = "Replace CA";
    actions.append(backBtn, submitBtn);
    content.append(actions);

    backBtn.addEventListener("click", () => modal.close());

    // Every control a running request must disable, and closing must be
    // blocked for -- there is nothing to undo once crypto and the
    // transaction are underway (R2), so an accidental Esc/backdrop/Close
    // must not look like it canceled anything.
    const controls: Array<HTMLInputElement | HTMLButtonElement> = [
      nameInput,
      reissueR.input,
      deleteR.input,
      keepR.input,
      backBtn,
      submitBtn,
    ];
    if (previousStaleReissue && previousStaleDelete) {
      controls.push(previousStaleReissue.input, previousStaleDelete.input);
    }

    function setRunning(running: boolean): void {
      for (const c of controls) c.disabled = running;
      progressWrap.hidden = !running;
      modal.setClosable(!running);
    }

    // percent never decreases across a single run (percentFor's own floor
    // guards each call, but a run-scoped variable is still needed to carry
    // the previous value from one event to the next).
    let percent = 0;

    // The "ca-key" phase's creep timer (owner requirement: "count up
    // wherever possible") -- the RSA-4096 CA key generation is the longest
    // single step for a keep/delete run, and the bar would otherwise sit
    // frozen at its ca-key percentage the whole time. Cleared on every
    // subsequent event (onProgress), on error, and on close, so no timer
    // ever outlives the dialog.
    let creepTimer: ReturnType<typeof setInterval> | null = null;

    function stopCreep(): void {
      if (creepTimer !== null) {
        clearInterval(creepTimer);
        creepTimer = null;
      }
    }

    function startCreep(total: number): void {
      stopCreep();
      const ceiling = caKeySegmentCeiling(total);
      creepTimer = setInterval(() => {
        percent = creepToward(percent, ceiling, CA_KEY_CREEP_STEP);
        progressBar.value = percent;
        statusEl.textContent = `Generating the new certificate authority key… ${Math.round(percent)}%`;
      }, CA_KEY_CREEP_INTERVAL_MS);
    }

    function onProgress(ev: ReplaceProgressEvent): void {
      stopCreep();
      // percentFor's own prevPercent floor means the crept value (already
      // in `percent`) is never lost even though this event's own raw value
      // may be lower than what the timer crept to -- it can only be equal
      // or higher, per caKeySegmentCeiling's own margin.
      percent = percentFor(ev.phase, ev.done ?? 0, ev.total ?? 0, percent);
      progressBar.value = percent;
      statusEl.textContent = progressStatusText(ev, percent);
      if (ev.phase === "ca-key") {
        startCreep(ev.total ?? 0);
      }
    }

    submitBtn.addEventListener("click", () => {
      errorEl.hidden = true;
      const name = nameInput.value.trim();
      if (name === "") {
        errorEl.textContent = "A name is required.";
        errorEl.hidden = false;
        return;
      }
      const existing = (reissueR.input.checked && "reissue") ||
        (deleteR.input.checked && "delete") ||
        "keep";
      const previousStale = previousActive
        ? (previousStaleReissue?.input.checked ? "reissue" : "delete")
        : undefined;

      stopCreep();
      percent = 0;
      progressBar.value = 0;
      setRunning(true);
      statusEl.textContent = "Starting… 0%";
      replaceCAWithProgress(
        { name, existing, ...(previousStale !== undefined ? { previousStale } : {}) },
        onProgress,
      )
        .then((result) => {
          stopCreep();
          percent = percentFor("done", 0, 0, percent);
          progressBar.value = percent;
          statusEl.textContent = `Done — ${Math.round(percent)}%`;
          modal.close();
          showToast(
            `Replaced the certificate authority. Re-issued ${result.reissued}, deleted ${result.deleted}, kept ${result.kept}.`,
            "success",
          );
          onReplaced();
          showD6Reminder(onOpenTrustDialog);
        })
        .catch((err: unknown) => {
          // On error, the bar stays exactly where it stopped -- no jump to
          // 100%, no reset to 0.
          stopCreep();
          setRunning(false);
          errorEl.textContent = errorText(err);
          errorEl.hidden = false;
          // Move keyboard focus to the error message itself, not just
          // re-enable the controls -- otherwise a keyboard user's focus is
          // left on whatever element it happened to be on when setRunning
          // disabled everything.
          errorEl.focus();
        });
    });
  }
}

/**
 * "Switch back to previous CA" (D9): an are-you-sure confirm, then the
 * request. Rejects with the server's own message on failure (e.g.
 * `ErrNoPreviousCA`, or the previous CA now failing its own expiry check).
 */
export async function confirmSwitchBackCA(onSwitchedBack: () => void, onOpenTrustDialog: () => void): Promise<void> {
  const ok = await confirmDialog(
    "Switch back to the previous certificate authority? The certificate that is current now becomes the previous one.",
    { title: "Switch back to previous CA", confirmLabel: "Switch back" },
  );
  if (!ok) return;

  try {
    await switchBackCA();
    showToast("Switched back to the previous certificate authority.", "success");
    onSwitchedBack();
    showD6Reminder(onOpenTrustDialog);
  } catch (err) {
    showToast(errorText(err), "error");
  }
}
