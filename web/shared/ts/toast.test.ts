// Toast unit tests: donor-compatible API surface, live-region attributes,
// textContent-only rendering, dismiss idempotence, single stack for N
// toasts, close-button markup, and barrel export shape.
//
// No jsdom, no @types/node — the same hand-rolled element stub
// idiom as modal.test.ts, plus a setTimeout/clearTimeout recorder, because
// "error is sticky" is a claim about a timer that must NOT have been created
// and there is no other way to observe its absence. There is no browser here;
// a non-zero exit is the whole report.

import { showToast } from "./toast";
import * as barrel from "./index";

// --- a timer recorder -------------------------------------------------------

interface TimerRecord {
  id: number;
  ms: number;
  cleared: boolean;
}

const timers: TimerRecord[] = [];
let nextTimerId = 1;

(globalThis as unknown as { setTimeout: unknown }).setTimeout = (_fn: () => void, ms: number): number => {
  const id = nextTimerId++;
  timers.push({ id, ms, cleared: false });
  return id;
};
(globalThis as unknown as { clearTimeout: unknown }).clearTimeout = (id: number): void => {
  const t = timers.find((x) => x.id === id);
  if (t) t.cleared = true;
};

// --- a hand-rolled element stub, no jsdom -----------------------------------

class FakeElement {
  tagName: string;
  className = "";
  type = "";
  textContent: string | null = "";
  dataset: Record<string, string> = {};
  attrs: Record<string, string> = {};
  children: FakeElement[] = [];
  listeners: Record<string, Array<() => void>> = {};
  parent: FakeElement | null = null;

  constructor(tagName: string) {
    this.tagName = tagName;
  }
  append(...nodes: FakeElement[]): void {
    for (const node of nodes) {
      node.parent = this;
      this.children.push(node);
    }
  }
  setAttribute(name: string, value: string): void {
    this.attrs[name] = value;
  }
  addEventListener(name: string, fn: () => void): void {
    (this.listeners[name] ??= []).push(fn);
  }
  contains(node: FakeElement): boolean {
    return this.children.includes(node) || this.children.some((c) => c.contains(node));
  }
  remove(): void {
    if (!this.parent) return;
    const i = this.parent.children.indexOf(this);
    if (i >= 0) this.parent.children.splice(i, 1);
    this.parent = null;
  }
}

const fakeBody = new FakeElement("body");

(globalThis as unknown as { document: unknown }).document = {
  body: fakeBody,
  createElement: (tag: string) => new FakeElement(tag),
};

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

function stack(): FakeElement {
  if (fakeBody.children.length !== 1) throw new Error(`expected exactly one stack, got ${fakeBody.children.length}`);
  return fakeBody.children[0];
}

// --- the live region: one element, and its exact attributes ----------------

{
  const before = timers.length;
  const handle = showToast("first", "notice");
  const s = stack();
  check("a stack element is appended to document.body", fakeBody.children.length === 1, `got ${fakeBody.children.length}`);
  check("the stack carries the ui-toast-stack class", s.className === "ui-toast-stack", `got "${s.className}"`);
  check('the stack carries role="status"', s.attrs.role === "status", `got "${s.attrs.role}"`);
  check('the stack carries aria-live="polite"', s.attrs["aria-live"] === "polite", `got "${s.attrs["aria-live"]}"`);
  check("the stack declares no other attribute", Object.keys(s.attrs).length === 2, `got ${JSON.stringify(s.attrs)}`);
  check("notice gets its 8000ms default", timers[before].ms === 8000, `got ${timers[before].ms}`);
  handle.dismiss();
}

// --- markup in a message is not interpreted ---------------------------------

{
  const raw = "<b>evil</b> & <script>alert(1)</script>";
  const handle = showToast(raw, "notice");
  const node = stack().children[0];
  const text = node.children[0];
  check("the toast node carries the ui-toast class", node.className === "ui-toast", `got "${node.className}"`);
  check("the text element carries the ui-toast-text class", text.className === "ui-toast-text", `got "${text.className}"`);
  check("the text element is a <p>", text.tagName === "p", `got "${text.tagName}"`);
  check("the message goes through textContent verbatim, unescaped", text.textContent === raw, `got "${text.textContent}"`);
  handle.dismiss();
}

// --- the close button: aria-label and a text-node glyph --------------------

{
  const handle = showToast("closable", "notice");
  const close = stack().children[0].children[1];
  check("the close button carries the ui-toast-close class", close.className === "ui-toast-close", `got "${close.className}"`);
  check("the close button is a <button>", close.tagName === "button", `got "${close.tagName}"`);
  check('the close button is type="button"', close.type === "button", `got "${close.type}"`);
  check('the close button carries aria-label="Dismiss"', close.attrs["aria-label"] === "Dismiss", `got "${close.attrs["aria-label"]}"`);
  check("the close glyph is a text node, not markup", close.textContent === "×", `got "${close.textContent}"`);
  handle.dismiss();
}

// --- tone defaults, and error's stickiness ---------------------------------

{
  const before = timers.length;
  const handle = showToast("saved", "success");
  check("success registers exactly one timer", timers.length === before + 1, `got ${timers.length - before}`);
  check("success gets its 6000ms default", timers[before].ms === 6000, `got ${timers[before].ms}`);
  handle.dismiss();
}

{
  const before = timers.length;
  const handle = showToast("it broke", "error");
  check("error registers NO timer — durationMs is 0, so the toast is sticky", timers.length === before, `got ${timers.length - before} new timer(s)`);
  check("the error toast is still in the stack", stack().children.length === 1, `got ${stack().children.length}`);
  check("the error toast carries data-tone=error", stack().children[0].dataset.tone === "error", `got "${stack().children[0].dataset.tone}"`);
  handle.dismiss();
}

{
  const before = timers.length;
  const handle = showToast("default tone", undefined, 0);
  check("an explicit durationMs of 0 registers no timer either", timers.length === before, `got ${timers.length - before} new timer(s)`);
  check("the default tone is notice", stack().children[0].dataset.tone === "notice", `got "${stack().children[0].dataset.tone}"`);
  handle.dismiss();
}

// --- exactly one stack element for N toasts ---------------------------------

{
  const a = showToast("one", "notice");
  const b = showToast("two", "success");
  const c = showToast("three", "error");
  check("three toasts share exactly one stack element", fakeBody.children.length === 1, `got ${fakeBody.children.length}`);
  check("all three toast nodes are children of that stack", stack().children.length === 3, `got ${stack().children.length}`);
  a.dismiss();
  b.dismiss();
  c.dismiss();
  check("dismissing all three leaves the stack in place and empty", fakeBody.children.length === 1 && stack().children.length === 0, `got ${fakeBody.children.length} stack(s), ${stack().children.length} toast(s)`);
}

// --- dismiss() is idempotent and clears its timer --------------------------

{
  const before = timers.length;
  const handle = showToast("twice", "notice");
  const timer = timers[before];
  check("notice registered a timer to clear", timers.length === before + 1 && !timer.cleared, `got ${JSON.stringify(timer)}`);
  handle.dismiss();
  check("dismiss() removes the toast node", stack().children.length === 0, `got ${stack().children.length}`);
  check("dismiss() clears the auto-dismiss timer", timer.cleared, "timer was left running");
  handle.dismiss();
  handle.dismiss();
  check("dismiss() is idempotent — repeat calls are no-ops", stack().children.length === 0 && fakeBody.children.length === 1, `got ${stack().children.length} toast(s), ${fakeBody.children.length} stack(s)`);
  check("dismiss() registered no further timers", timers.length === before + 1, `got ${timers.length - before}`);
}

// --- the close button's click handler is the same dismiss -------------------

{
  const handle = showToast("click me", "error");
  const node = stack().children[0];
  const close = node.children[1];
  check("the close button has a click listener", (close.listeners.click ?? []).length === 1, `got ${(close.listeners.click ?? []).length}`);
  for (const fn of close.listeners.click) fn();
  check("clicking close removes the toast", stack().children.length === 0, `got ${stack().children.length}`);
  handle.dismiss();
}

// --- barrel export shape ----------------------------------------------------

check("barrel exposes showToast", typeof barrel.showToast === "function", `got ${typeof barrel.showToast}`);
check("barrel's showToast is this module's showToast", barrel.showToast === showToast, "the barrel re-exports a different binding");
