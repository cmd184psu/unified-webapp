// outputmodal.ts — the shared "View output" modal: a large, scrollable,
// live-streaming view of one execution's stdout/stderr.
//
// Used from two call sites — the lane board's running-row log control, and
// the task detail's history rows — so there is exactly one place that knows
// how to open and stream an execution's output, instead of two similar
// copies drifting apart (owner DRY policy).
//
// No lambdas/inline callbacks: every EventSource listener is a small named
// function (owner code policy — short, named, readable, testable).

import { api } from "./api.js";
import { openModal, ModalHandle } from "./ui/modal.js";

const STYLE_ATTR = "data-tm-output-modal-styles";

function ensureStyles(): void {
  if (document.head.querySelector("style[" + STYLE_ATTR + "]")) return;
  const style = document.createElement("style");
  style.setAttribute(STYLE_ATTR, "");
  style.textContent =
    ".output-modal-panel { max-width: min(90vw, 900px); width: 90vw; }\n" +
    ".output-modal-box { height: 70vh; max-height: 70vh; overflow: auto; margin: 0; " +
    "white-space: pre-wrap; word-break: break-word; font-family: var(--font-mono, monospace); font-size: 13px; }\n";
  document.head.appendChild(style);
}

interface OutputLinePayload {
  stream?: string;
  line?: string;
}

function parseOutputLine(raw: string): string | null {
  try {
    const data = JSON.parse(raw) as OutputLinePayload;
    return data.line ?? "";
  } catch {
    return null;
  }
}

function appendOutputLine(box: HTMLElement, ev: MessageEvent): void {
  const line = parseOutputLine(ev.data as string);
  if (line === null) return; // malformed line — skip it, don't corrupt the pane
  box.textContent += line + "\n";
  box.scrollTop = box.scrollHeight;
}

function showStatusIfEmpty(box: HTMLElement, statusText: string): void {
  if (box.textContent === "") box.textContent = "(execution " + statusText + ")";
}

function markDoneIfEmpty(box: HTMLElement): void {
  if (box.textContent === "") box.textContent = "(no output captured for this run)";
}

/**
 * Opens a large scrollable modal streaming `execId`'s stdout/stderr live
 * (replaying already-captured lines first, same as the underlying SSE
 * endpoint). Closes the stream when the modal closes, however it closes.
 */
export function openOutputModal(execId: number, title: string): ModalHandle {
  ensureStyles();

  const box = document.createElement("pre");
  box.className = "output-modal-box";
  box.textContent = "";

  const source = api.openExecutionOutput(execId);
  wireOutputSource(source, box);

  const handle = openModal(box, { title, onClose: makeCloseSource(source) });
  handle.panel.classList.add("output-modal-panel");
  return handle;
}

function wireOutputSource(source: EventSource, box: HTMLElement): void {
  source.addEventListener("output", makeAppendHandler(box));
  source.addEventListener("status", makeStatusHandler(box));
  source.addEventListener("done", makeDoneHandler(box, source));
}

function makeAppendHandler(box: HTMLElement): (ev: MessageEvent) => void {
  function onOutput(ev: MessageEvent): void {
    appendOutputLine(box, ev);
  }
  return onOutput;
}

function makeStatusHandler(box: HTMLElement): (ev: MessageEvent) => void {
  function onStatus(ev: MessageEvent): void {
    showStatusIfEmpty(box, ev.data as string);
  }
  return onStatus;
}

function makeDoneHandler(box: HTMLElement, source: EventSource): () => void {
  function onDone(): void {
    markDoneIfEmpty(box);
    source.close();
  }
  return onDone;
}

function makeCloseSource(source: EventSource): () => void {
  function closeSource(): void {
    source.close();
  }
  return closeSource;
}
