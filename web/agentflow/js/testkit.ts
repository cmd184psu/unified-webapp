// testkit.ts — a tiny async-aware runner and a fake DOM for the agentflow
// suites. test-web.mjs bundles each *.test.ts and pipes it to node (no jsdom).
// Not imported by the app bundle.

type Case = { name: string; fn: () => void | Promise<void> };
const cases: Case[] = [];

export function test(name: string, fn: () => void | Promise<void>): void {
  cases.push({ name, fn });
}

/** run executes the queued cases in order and throws if any failed. */
export async function run(suite: string): Promise<void> {
  let failures = 0;
  for (const c of cases) {
    try {
      await c.fn();
      console.log(`ok - ${c.name}`);
    } catch (e) {
      failures++;
      console.error(`FAIL - ${c.name}: ${e instanceof Error ? e.message : String(e)}`);
    }
  }
  if (failures > 0) throw new Error(`${suite}: ${failures} test(s) failed`);
}

export function eq<T>(got: T, want: T, what = 'value'): void {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) throw new Error(`${what}: got ${g}, want ${w}`);
}

export function ok(cond: unknown, what: string): void {
  if (!cond) throw new Error(what);
}

// ─── Fake DOM: just what dom.ts, overview.ts and the shell touch ─────────────

export class FakeText {
  readonly nodeType = 3;
  parentNode: FakeEl | null = null;
  constructor(public data: string) {}
  get textContent(): string {
    return this.data;
  }
}

export class FakeEl {
  readonly nodeType = 1;
  className = '';
  parentNode: FakeEl | null = null;
  childNodes: Array<FakeEl | FakeText> = [];
  attrs = new Map<string, string>();
  listeners = new Map<string, Array<(ev: unknown) => void>>();
  constructor(public tagName: string) {}

  get textContent(): string {
    return this.childNodes.map((c) => c.textContent).join('');
  }
  set textContent(v: string) {
    for (const c of this.childNodes) c.parentNode = null;
    this.childNodes = [];
    if (v !== '') this.appendChild(new FakeText(v));
  }
  appendChild<T extends FakeEl | FakeText>(c: T): T {
    c.parentNode = this;
    this.childNodes.push(c);
    return c;
  }
  setAttribute(k: string, v: string): void {
    this.attrs.set(k, v);
  }
  getAttribute(k: string): string | null {
    return this.attrs.has(k) ? (this.attrs.get(k) as string) : null;
  }
  addEventListener(type: string, fn: (ev: unknown) => void): void {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn]);
  }
  /** click dispatches a click and returns once the listeners ran. */
  click(): void {
    for (const fn of this.listeners.get('click') ?? []) fn({ preventDefault() {} });
  }
}

export function installFakeDom(): void {
  Object.defineProperty(globalThis, 'document', {
    value: {
      createElement: (tag: string) => new FakeEl(tag.toUpperCase()),
      createTextNode: (s: string) => new FakeText(s),
    },
    writable: true,
    configurable: true,
  });
}

/** descendants lists every element under root, depth first. */
export function descendants(root: FakeEl): FakeEl[] {
  const out: FakeEl[] = [];
  for (const c of root.childNodes) {
    if (c instanceof FakeEl) out.push(c, ...descendants(c));
  }
  return out;
}

export function findAll(root: FakeEl, pred: (e: FakeEl) => boolean): FakeEl[] {
  return descendants(root).filter(pred);
}

export const asEl = (e: FakeEl): HTMLElement => e as unknown as HTMLElement;
