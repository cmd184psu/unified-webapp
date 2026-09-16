// test-dom.ts — the DOM stub the shared unit suites need, added at phase2 C3
// (docs/PLAN-ui-unification-phase2.md §7 B3.1's precondition, Critic M-11).
//
// scripts/test-web.mjs bundles each suite with platform:"node" / format:"cjs"
// (:52-60) and pipes it to a fresh `node` (:65-69): there is no DOM, no jsdom
// and no `window` (ADR-005). modal.test.ts hand-rolls the five `document`
// members modal.ts touches; ThemeManager touches a DISJOINT set — the
// documentElement's dataset, localStorage, and matchMedia — so B3.1 and B3.4
// are unwritable without this helper, and each suite is its own process so
// nothing can be inherited from another suite's stub.
//
// Two gate consequences, both deliberate: this file is NOT a barrel export and
// is not imported by index.ts, so check-shared-barrel.mjs's counts are
// unaffected (it reads web/shared/ts/index.ts only, :23) and the shared bundle
// does not grow; and it IS matched by tsconfig.json:15's include globs, so
// `tsc --noEmit` type-checks it like any other file. modal.test.ts is
// deliberately not retrofitted onto it in this phase (§11 item 14).

/** The single MediaQueryList every stubbed matchMedia() call returns. */
export interface FakeMediaQueryList {
  matches: boolean;
  media: string;
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
  /** Flip the OS preference and dispatch `change` to every live listener. */
  fireChange(matches: boolean): void;
}

/** The handles a suite seeds and asserts through. */
export interface FakeDom {
  /** The element setTheme() stamps; `dataset` is a plain object. */
  documentElement: { dataset: Record<string, string> };
  /** The store behind globalThis.localStorage. */
  storage: Map<string, string>;
  /** The MediaQueryList the `system` resolution step reads. */
  media: FakeMediaQueryList;
}

// defineProperty rather than plain assignment: node exposes `localStorage` as
// an experimental accessor, and defining over it is the one form that works
// whether or not the bundled suite runs in strict mode.
function define(name: string, value: unknown): void {
  Object.defineProperty(globalThis, name, { value, writable: true, configurable: true });
}

export function installFakeDom(): FakeDom {
  const documentElement = { dataset: {} as Record<string, string> };
  const storage = new Map<string, string>();
  const listeners: Array<() => void> = [];

  const media: FakeMediaQueryList = {
    matches: false,
    media: "(prefers-color-scheme: dark)",
    addEventListener(type, listener) {
      if (type === "change") listeners.push(listener);
    },
    removeEventListener(type, listener) {
      if (type !== "change") return;
      const i = listeners.indexOf(listener);
      if (i >= 0) listeners.splice(i, 1);
    },
    fireChange(matches) {
      media.matches = matches;
      for (const listener of [...listeners]) listener();
    },
  };

  define("document", { documentElement });
  define("localStorage", {
    getItem: (key: string): string | null => (storage.has(key) ? (storage.get(key) as string) : null),
    setItem: (key: string, value: string): void => {
      storage.set(key, String(value));
    },
    removeItem: (key: string): void => {
      storage.delete(key);
    },
    clear: (): void => {
      storage.clear();
    },
  });
  define("matchMedia", () => media);

  return { documentElement, storage, media };
}
