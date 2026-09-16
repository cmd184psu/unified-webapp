// modal.test.ts — Phase-1 coverage (docs/PLAN-ui-unification-phase1.md §3
// Step 5): option normalization/defaults, the textContent-only escaping
// path, and the barrel's export shape.
//
// No jsdom, no @types/node (ADR-005) — just enough of a hand-rolled element
// stub for modal.ts's DOM calls to run under `npm run test:web`'s
// esbuild-cjs/node18 pipeline. There is no browser here; a non-zero exit is
// the whole report.

import { openModal, confirmDialog, alertDialog, promptDialog } from "./modal";
import * as barrel from "./index";

// --- a hand-rolled element stub, no jsdom ------------------------------------

class FakeElement {
  tagName: string;
  className = "";
  textContent: string | null = "";
  value = "";
  placeholder = "";
  children: FakeElement[] = [];
  constructor(tagName: string) {
    this.tagName = tagName;
  }
  appendChild(child: FakeElement): FakeElement {
    this.children.push(child);
    return child;
  }
  setAttribute(): void {}
  addEventListener(): void {}
  removeEventListener(): void {}
  querySelectorAll(): FakeElement[] {
    return [];
  }
  focus(): void {}
  select(): void {}
  remove(): void {}
}

let lastOverlay: FakeElement | null = null;
const fakeBody = new FakeElement("body");
fakeBody.appendChild = (child: FakeElement): FakeElement => {
  lastOverlay = child;
  return child;
};

(globalThis as unknown as { document: unknown }).document = {
  body: fakeBody,
  activeElement: null,
  createElement: (tag: string) => new FakeElement(tag),
  addEventListener: () => {},
  removeEventListener: () => {},
};

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

// --- option normalization / defaults ----------------------------------------

{
  const content = new FakeElement("div") as unknown as HTMLElement;
  const handle = openModal(content, {});
  const panel = handle.panel as unknown as FakeElement;
  const overlay = handle.overlay as unknown as FakeElement;
  check("openModal overlay carries the ui-modal-overlay class", overlay.className === "ui-modal-overlay", `got "${overlay.className}"`);
  check("openModal panel carries the ui-modal-panel class", panel.className === "ui-modal-panel", `got "${panel.className}"`);
  check("openModal with no title appends only the content", panel.children.length === 1, `got ${panel.children.length} children`);
}

{
  const content = new FakeElement("div") as unknown as HTMLElement;
  const handle = openModal(content, { title: "Details" });
  const panel = handle.panel as unknown as FakeElement;
  check("openModal with a title adds a title element", panel.children.length === 2, `got ${panel.children.length} children`);
  const titleEl = panel.children[0];
  check("title element carries the ui-modal-title class", titleEl.className === "ui-modal-title", `got "${titleEl.className}"`);
  check("title element's text arrives via textContent", titleEl.textContent === "Details", `got "${titleEl.textContent}"`);
}

// --- confirmDialog: defaults + textContent-only escaping --------------------

{
  const raw = "<b>evil</b> & <script>alert(1)</script>";
  void confirmDialog(raw);
  const overlay = lastOverlay as unknown as FakeElement;
  const panel = overlay.children[0];
  const content = panel.children[0];
  const msg = content.children[0];
  const actions = content.children[1];
  const cancelBtn = actions.children[0];
  const okBtn = actions.children[1];

  check("confirmDialog message carries the ui-modal-message class", msg.className === "ui-modal-message", `got "${msg.className}"`);
  check("confirmDialog message goes through textContent verbatim, unescaped", msg.textContent === raw, `got "${msg.textContent}"`);
  check("confirmDialog default cancel label is Cancel", cancelBtn.textContent === "Cancel", `got "${cancelBtn.textContent}"`);
  check("confirmDialog default confirm label is OK", okBtn.textContent === "OK", `got "${okBtn.textContent}"`);
  check("confirmDialog primary button carries both btn classes", okBtn.className === "ui-modal-btn ui-modal-btn-primary", `got "${okBtn.className}"`);
}

// --- alertDialog: single-button default ------------------------------------

{
  void alertDialog("hi");
  const overlay = lastOverlay as unknown as FakeElement;
  const panel = overlay.children[0];
  const content = panel.children[0];
  const actions = content.children[1];
  check("alertDialog has exactly one action button", actions.children.length === 1, `got ${actions.children.length}`);
  check("alertDialog default confirm label is OK", actions.children[0].textContent === "OK", `got "${actions.children[0].textContent}"`);
}

// --- promptDialog: defaultValue is assigned, not injected as markup --------

{
  void promptDialog("name?", { defaultValue: "<i>x</i>" });
  const overlay = lastOverlay as unknown as FakeElement;
  const panel = overlay.children[0];
  const content = panel.children[0];
  const input = content.children[1];
  check("promptDialog carries the ui-modal-input class", input.className === "ui-modal-input", `got "${input.className}"`);
  check("promptDialog defaultValue is assigned verbatim via .value", input.value === "<i>x</i>", `got "${input.value}"`);
}

// --- the barrel's export shape (A5.2) ---------------------------------------

check("barrel exposes openModal", typeof barrel.openModal === "function", "");
check("barrel exposes confirmDialog", typeof barrel.confirmDialog === "function", "");
check("barrel exposes alertDialog", typeof barrel.alertDialog === "function", "");
check("barrel exposes promptDialog", typeof barrel.promptDialog === "function", "");
check("barrel exposes setTheme", typeof barrel.setTheme === "function", "");
check(
  "barrel exposes THEMES as the canonical 8-name array",
  Array.isArray(barrel.THEMES) && barrel.THEMES.length === 8,
  `got ${JSON.stringify(barrel.THEMES)}`,
);
