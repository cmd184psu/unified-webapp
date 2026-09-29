// patchlist.test.ts — insert, reorder, remove, update in place, and the
// data-ui-key attribute (plan docs/PLAN-utuber-taskmaster-lane.md §7.1).
//
// No jsdom — the same hand-rolled element stub idiom as toast.test.ts, since
// patchList only needs a small subset of the DOM (children, insertBefore,
// firstChild/nextSibling, getAttribute/setAttribute, remove).

import { patchList } from "./patchlist";
import * as barrel from "./index";

class FakeElement {
  tagName: string;
  attrs: Record<string, string> = {};
  updated: string[] = [];
  private _children: FakeElement[] = [];
  parentEl: FakeElement | null = null;

  constructor(tagName: string) {
    this.tagName = tagName;
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

  insertBefore(node: FakeElement, ref: FakeElement | null): void {
    this.removeChild(node);
    const idx = ref ? this._children.indexOf(ref) : -1;
    if (ref && idx === -1) throw new Error("ref not found");
    if (idx === -1) {
      this._children.push(node);
    } else {
      this._children.splice(idx, 0, node);
    }
    node.parentEl = this;
  }

  appendChild(node: FakeElement): void {
    this.insertBefore(node, null);
  }

  removeChild(node: FakeElement): void {
    const i = this._children.indexOf(node);
    if (i >= 0) this._children.splice(i, 1);
  }

  remove(): void {
    this.parentEl?.removeChild(this);
    this.parentEl = null;
  }
}

// patchList reads container.children (Array.from) and container.firstChild,
// and calls insertBefore/remove on it — the fake above provides exactly
// that surface, cast through unknown since it isn't a real HTMLElement.
function asHost(el: FakeElement): HTMLElement {
  return el as unknown as HTMLElement;
}
function asRow(el: FakeElement): HTMLElement {
  return el as unknown as HTMLElement;
}

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

interface Item {
  id: string;
  val: number;
}

function makeItem(id: string, val: number): Item {
  return { id, val };
}

function keysOf(container: FakeElement): string[] {
  return container.children.map((c) => c.getAttribute("data-ui-key") ?? "");
}

// --- insert ------------------------------------------------------------------
{
  const container = new FakeElement("div");
  const created: string[] = [];
  patchList<Item>(asHost(container), [makeItem("a", 1), makeItem("b", 2)], {
    key: (i) => i.id,
    create: (i) => {
      created.push(i.id);
      return asRow(new FakeElement("div"));
    },
    update: () => {
      throw new Error("update should not run for brand-new items");
    },
  });
  check("two new items are inserted", container.children.length === 2, `got ${container.children.length}`);
  check("data-ui-key is set on insert", keysOf(container).join(",") === "a,b", keysOf(container).join(","));
  check("create ran for both new items", created.join(",") === "a,b", created.join(","));
}

// --- update in place, no re-creation -----------------------------------------
{
  const container = new FakeElement("div");
  let createCalls = 0;
  const updated: Item[] = [];
  const opts = {
    key: (i: Item) => i.id,
    create: (i: Item) => {
      createCalls++;
      const el = new FakeElement("div");
      return asRow(el);
    },
    update: (el: HTMLElement, i: Item) => {
      updated.push(i);
      (el as unknown as FakeElement).updated.push(String(i.val));
    },
  };
  patchList(asHost(container), [makeItem("a", 1)], opts);
  const firstNode = container.children[0];
  patchList(asHost(container), [makeItem("a", 2)], opts);
  check("no new node is created for an existing key", createCalls === 1, `got ${createCalls} create calls`);
  check("the same node identity is reused", container.children[0] === firstNode, "node was replaced");
  check("update received the new value", updated[updated.length - 1].val === 2, `got ${updated[updated.length - 1].val}`);
}

// --- reorder ------------------------------------------------------------------
{
  const container = new FakeElement("div");
  const opts = {
    key: (i: Item) => i.id,
    create: () => asRow(new FakeElement("div")),
    update: () => {},
  };
  patchList(asHost(container), [makeItem("a", 1), makeItem("b", 2), makeItem("c", 3)], opts);
  const [a, b, c] = container.children;
  patchList(asHost(container), [makeItem("c", 3), makeItem("a", 1), makeItem("b", 2)], opts);
  check("reorder preserves node identity", container.children[0] === c && container.children[1] === a && container.children[2] === b, keysOf(container).join(","));
  check("reorder yields the new key order", keysOf(container).join(",") === "c,a,b", keysOf(container).join(","));
}

// --- remove -------------------------------------------------------------------
{
  const container = new FakeElement("div");
  const opts = {
    key: (i: Item) => i.id,
    create: () => asRow(new FakeElement("div")),
    update: () => {},
  };
  patchList(asHost(container), [makeItem("a", 1), makeItem("b", 2), makeItem("c", 3)], opts);
  patchList(asHost(container), [makeItem("a", 1), makeItem("c", 3)], opts);
  check("the dropped key's node is removed", keysOf(container).join(",") === "a,c", keysOf(container).join(","));
  check("removing to empty leaves zero children", (() => {
    patchList(asHost(container), [], opts);
    return container.children.length === 0;
  })(), `got ${container.children.length}`);
}

// --- data-ui-key (not data-tm-key) -------------------------------------------
{
  const container = new FakeElement("div");
  patchList<Item>(asHost(container), [makeItem("x", 1)], {
    key: (i) => i.id,
    create: () => asRow(new FakeElement("div")),
    update: () => {},
  });
  check("the key attribute is data-ui-key", container.children[0].getAttribute("data-ui-key") === "x", JSON.stringify(container.children[0].attrs));
  check("the legacy data-tm-key attribute is not set", container.children[0].getAttribute("data-tm-key") === null, JSON.stringify(container.children[0].attrs));
}

// --- barrel export shape -------------------------------------------------------

check("barrel exposes patchList", typeof barrel.patchList === "function", `got ${typeof barrel.patchList}`);
check("barrel's patchList is this module's patchList", barrel.patchList === patchList, "the barrel re-exports a different binding");
