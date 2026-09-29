// outputmodal.ts — the shared "View output" modal: a large, scrollable,
// live-streaming view of one execution's stdout/stderr.
//
// Promoted to @shared so every module that has a per-execution output SSE
// endpoint (taskmaster's board/task-detail, utuber's failed-job icon) opens
// the literal same modal instead of drifting copies (owner DRY policy). The
// caller constructs the EventSource for its own endpoint and passes it in —
// this file knows how to stream and render, not how any one module builds its
// request URLs.
//
// No lambdas/inline callbacks: every EventSource listener is a small named
// function (owner code policy — short, named, readable, testable).

import { openModal } from "./modal.js";
import type { ModalHandle } from "./modal.js";

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

function markErrorIfEmpty(box: HTMLElement): void {
  // EventSource fires a plain "error" event (no message, no status code
  // surfaced to JS) both for a fatal connection failure (e.g. the endpoint
  // returned 404 — the job was removed) and, harmlessly, for a transient
  // network blip it will retry on its own. Without this, the first case
  // leaves the modal open and permanently empty with no feedback at all —
  // indistinguishable from the click having done nothing.
  if (box.textContent === "") box.textContent = "(could not load output — the job may have been removed)";
}

/**
 * Opens a large scrollable modal streaming `source`'s stdout/stderr live
 * (replaying already-captured lines first, same as the underlying SSE
 * endpoint). The caller builds `source` for its own module's output endpoint.
 * Closes the stream when the modal closes, however it closes.
 */
export function openOutputModal(source: EventSource, title: string): ModalHandle {
  ensureStyles();

  const box = document.createElement("pre");
  box.className = "output-modal-box";
  box.textContent = "";

  wireOutputSource(source, box);

  const handle = openModal(box, { title, onClose: makeCloseSource(source) });
  handle.panel.classList.add("output-modal-panel");
  return handle;
}

function wireOutputSource(source: EventSource, box: HTMLElement): void {
  source.addEventListener("output", makeAppendHandler(box));
  source.addEventListener("status", makeStatusHandler(box));
  source.addEventListener("done", makeDoneHandler(box, source));
  source.addEventListener("error", makeErrorHandler(box, source));
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

function makeErrorHandler(box: HTMLElement, source: EventSource): () => void {
  function onError(): void {
    // A transient network hiccup leaves the browser retrying (readyState
    // CONNECTING) — say nothing and let it reconnect. A non-2xx/non-SSE
    // response (404: job removed, 500, etc.) leaves it CLOSED for good —
    // that's the case with no other feedback path, so report it here.
    if (source.readyState === EventSource.CLOSED) markErrorIfEmpty(box);
  }
  return onError;
}

function makeCloseSource(source: EventSource): () => void {
  function closeSource(): void {
    source.close();
  }
  return closeSource;
}
