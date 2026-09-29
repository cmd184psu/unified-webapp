// Fake DOM for the shared unit suites. Each suite runs in a fresh node
// process with no jsdom; this stubs documentElement.dataset, localStorage,
// and matchMedia so ThemeManager tests can run. Not a barrel export.

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
