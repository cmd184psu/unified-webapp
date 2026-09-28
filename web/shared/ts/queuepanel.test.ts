// queuepanel.test.ts (plan docs/PLAN-utuber-taskmaster-lane.md §7.1):
// bucketQueue (order preserved, recentLimit, null section dropped);
// progressView (null -> hidden; pct clamp; null pct -> indeterminate);
// ACTION_GLYPHS completeness; barrel exports; and a stubbed-DOM
// QueuePanel.update proving row reuse (same node identity across updates)
// and that the progress element exists only when progress() is non-null.
//
// No jsdom — a hand-rolled element/document stub, the same idiom as
// toast.test.ts/modal.test.ts, extended with just enough of
// querySelector/classList/style/hidden for QueuePanel's row rendering.

import { bucketQueue, progressView, ACTION_GLYPHS, QueuePanel } from "./queuepanel";
import type { QueuePanelAdapter, QueueSection, QueueActionKind } from "./queuepanel";
import * as barrel from "./index";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

// --- bucketQueue (pure) -------------------------------------------------------

{
  interface Item {
    id: string;
    sec: QueueSection | null;
  }
  const items: Item[] = [
    { id: "a", sec: "running" },
    { id: "b", sec: "upnext" },
    { id: "c", sec: "running" },
    { id: "d", sec: null },
    { id: "e", sec: "recent" },
    { id: "f", sec: "recent" },
  ];
  const out = bucketQueue(items, (i) => i.sec);
  check("running bucket preserves input order", out.running.map((i) => i.id).join(",") === "a,c", out.running.map((i) => i.id).join(","));
  check("upnext bucket has the one item", out.upnext.map((i) => i.id).join(",") === "b", out.upnext.map((i) => i.id).join(","));
  check("a null section is dropped entirely", !out.running.some((i) => i.id === "d") && !out.upnext.some((i) => i.id === "d") && !out.recent.some((i) => i.id === "d"), "item d leaked into a bucket");
  check("recent bucket without a limit keeps everything", out.recent.map((i) => i.id).join(",") === "e,f", out.recent.map((i) => i.id).join(","));

  const limited = bucketQueue(items, (i) => i.sec, 1);
  check("recentLimit truncates the recent bucket", limited.recent.length === 1 && limited.recent[0].id === "e", JSON.stringify(limited.recent));
  check("recentLimit does not affect running/upnext", limited.running.length === 2 && limited.upnext.length === 1, "non-recent buckets were truncated");
}

// --- progressView (pure) ------------------------------------------------------

{
  const hidden = progressView(null);
  check("null progress is hidden", hidden.show === false, JSON.stringify(hidden));
  const alsoHidden = progressView(undefined);
  check("undefined progress is hidden", alsoHidden.show === false, JSON.stringify(alsoHidden));

  const low = progressView({ pct: -5, label: "" });
  check("pct below 0 clamps to 0", low.show && low.pct === 0 && !low.indeterminate, JSON.stringify(low));

  const high = progressView({ pct: 150, label: "" });
  check("pct above 100 clamps to 100", high.show && high.pct === 100 && !high.indeterminate, JSON.stringify(high));

  const indet = progressView({ pct: null, label: "Fetching metadata" });
  check("null pct is indeterminate and shown", indet.show && indet.indeterminate && indet.label === "Fetching metadata", JSON.stringify(indet));
}

// --- ACTION_GLYPHS completeness ----------------------------------------------

{
  const kinds: QueueActionKind[] = ["cancel", "pause", "resume", "rerun", "remove"];
  for (const k of kinds) {
    check(`ACTION_GLYPHS has an entry for "${k}"`, !!ACTION_GLYPHS[k]?.glyph && !!ACTION_GLYPHS[k]?.label, JSON.stringify(ACTION_GLYPHS[k]));
  }
  check("ACTION_GLYPHS declares exactly the 5 action kinds", Object.keys(ACTION_GLYPHS).length === 5, JSON.stringify(Object.keys(ACTION_GLYPHS)));
}

// --- barrel export shape -------------------------------------------------------

check("barrel exposes QueuePanel", typeof barrel.QueuePanel === "function", `got ${typeof barrel.QueuePanel}`);
check("barrel's QueuePanel is this module's QueuePanel", barrel.QueuePanel === QueuePanel, "the barrel re-exports a different binding");

// --- a hand-rolled DOM stub, no jsdom ------------------------------------------

class FakeStyle {
  width = "";
}

class FakeElement {
  tagName: string;
  private _className = "";
  attrs: Record<string, string> = {};
  style = new FakeStyle();
  hidden = false;
  title = "";
  type = "";
  tabIndex = -1;
  onclick: (() => void) | null = null;
  onkeydown: ((e: { key: string; preventDefault(): void }) => void) | null = null;
  listeners: Record<string, Array<() => void>> = {};
  private _children: FakeElement[] = [];
  parentEl: FakeElement | null = null;
  private _text = "";
  open = false; // <details>.open

  constructor(tagName: string) {
    this.tagName = tagName;
  }

  get className(): string {
    return this._className;
  }
  set className(v: string) {
    this._className = v;
  }

  get classList() {
    const self = this;
    return {
      add(...names: string[]): void {
        const set = new Set(self._className.split(/\s+/).filter(Boolean));
        for (const n of names) set.add(n);
        self._className = [...set].join(" ");
      },
      toggle(name: string, force?: boolean): void {
        const set = new Set(self._className.split(/\s+/).filter(Boolean));
        const on = force === undefined ? !set.has(name) : force;
        if (on) set.add(name);
        else set.delete(name);
        self._className = [...set].join(" ");
      },
      contains(name: string): boolean {
        return self._className.split(/\s+/).filter(Boolean).includes(name);
      },
    };
  }

  get textContent(): string {
    return this._text;
  }
  set textContent(v: string) {
    this._text = v;
    if (v === "") this._children = [];
  }

  get children(): FakeElement[] {
    return this._children;
  }
  get firstChild(): FakeElement | null {
    return this._children[0] ?? null;
  }
  get nextSibling(): FakeElement | null {
    if (!this.parentEl) return null;
    const i = this.parentEl._children.indexOf(this);
    return this.parentEl._children[i + 1] ?? null;
  }

  setAttribute(name: string, value: string): void {
    this.attrs[name] = value;
  }
  getAttribute(name: string): string | null {
    return name in this.attrs ? this.attrs[name] : null;
  }
  removeAttribute(name: string): void {
    delete this.attrs[name];
  }

  addEventListener(name: string, fn: () => void): void {
    (this.listeners[name] ??= []).push(fn);
  }

  insertBefore(node: FakeElement, ref: FakeElement | null): void {
    this.removeChild(node);
    const idx = ref ? this._children.indexOf(ref) : -1;
    if (idx === -1) {
      this._children.push(node);
    } else {
      this._children.splice(idx, 0, node);
    }
    node.parentEl = this;
  }
  appendChild(node: FakeElement): FakeElement {
    this.insertBefore(node, null);
    return node;
  }
  append(...nodes: FakeElement[]): void {
    for (const n of nodes) this.appendChild(n);
  }
  removeChild(node: FakeElement): void {
    const i = this._children.indexOf(node);
    if (i >= 0) this._children.splice(i, 1);
  }
  remove(): void {
    this.parentEl?.removeChild(this);
    this.parentEl = null;
  }

  private matches(selector: string): boolean {
    if (selector.startsWith(".")) return this.classList.contains(selector.slice(1));
    return this.tagName === selector;
  }

  querySelector(selector: string): FakeElement | null {
    for (const c of this._children) {
      if (c.matches(selector)) return c;
      const found = c.querySelector(selector);
      if (found) return found;
    }
    return null;
  }
  querySelectorAll(selector: string): FakeElement[] {
    const out: FakeElement[] = [];
    for (const c of this._children) {
      if (c.matches(selector)) out.push(c);
      out.push(...c.querySelectorAll(selector));
    }
    return out;
  }
}

(globalThis as unknown as { document: unknown }).document = {
  createElement: (tag: string) => new FakeElement(tag),
};

function asEl(el: FakeElement): HTMLElement {
  return el as unknown as HTMLElement;
}

// --- QueuePanel.update: row reuse + conditional progress element -------------

interface Job {
  id: string;
  sec: QueueSection;
  title: string;
  status: string;
  pct: number | null;
  hasProgress: boolean;
}

function makeAdapter(): QueuePanelAdapter<Job> {
  return {
    key: (j) => j.id,
    section: (j) => j.sec,
    title: (j) => j.title,
    status: (j) => j.status,
    progress: (j) => (j.hasProgress ? { pct: j.pct, label: j.status } : null),
    actions: () => ["cancel"],
    onAction: () => {},
  };
}

{
  const host = new FakeElement("div");
  const panel = new QueuePanel<Job>(asEl(host), makeAdapter());

  const job: Job = { id: "j1", sec: "running", title: "download A", status: "running", pct: 10, hasProgress: true };
  panel.update([job]);

  const runningList = panel.list("running") as unknown as FakeElement;
  check("one row is rendered in the running section", runningList.children.length === 1, `got ${runningList.children.length}`);
  const row1 = runningList.children[0];
  const progressWrap1 = row1.querySelector(".ui-queue-progress");
  check("a progress element exists when progress() is non-null", !!progressWrap1 && progressWrap1.hidden === false, `hidden=${progressWrap1?.hidden}`);

  panel.update([{ ...job, pct: 55 }]);
  const runningListAfter = panel.list("running") as unknown as FakeElement;
  check("the row node identity is reused across updates", runningListAfter.children[0] === row1, "row was re-created instead of patched");
  const bar = row1.querySelector(".ui-queue-progress-bar");
  check("the progress bar reflects the new pct", bar?.style.width === "55%", `width=${bar?.style.width}`);

  const noProgressJob: Job = { id: "j2", sec: "upnext", title: "download B", status: "queued", pct: null, hasProgress: false };
  panel.update([{ ...job, pct: 55 }, noProgressJob]);
  const upnextList = panel.list("upnext") as unknown as FakeElement;
  const row2 = upnextList.children[0];
  const progressWrap2 = row2.querySelector(".ui-queue-progress");
  check("the progress element is hidden when progress() is null", !!progressWrap2 && progressWrap2.hidden === true, `hidden=${progressWrap2?.hidden}`);

  panel.update([]);
  const emptiedRunning = panel.list("running") as unknown as FakeElement;
  check(
    "removing every item leaves only the empty-state note in the running list",
    emptiedRunning.children.length === 1 && emptiedRunning.children[0].getAttribute("data-ui-key") === "__empty__",
    JSON.stringify(emptiedRunning.children.map((c) => c.getAttribute("data-ui-key")))
  );
}
