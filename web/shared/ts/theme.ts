// theme.ts — the one JS-side source of the theme list. Adding a theme =
// adding one CSS block + one entry to the THEMES array. THEMES must match the
// depth-1 selectors web/shared/css/themes.css declares, and setTheme(name)
// is the one function that switches themes at runtime by writing the
// data-theme attribute themes.css keys off of.

export const THEMES = [
  "dark",
  "light",
  "obsidian",
  "forest",
  "ocean",
  "ember",
  "rose",
  "puma",
] as const;

export function setTheme(name: string): void {
  document.documentElement.dataset.theme = name;
}

// --- ThemeManager ------------------------------------------------------------
//
// One class over the three theme-resolution strategies: localStorage,
// server/vault default, and system preference. THEMES and setTheme above are
// unchanged: the class CALLS setTheme rather than re-implementing the DOM
// write, so "how a theme is applied" keeps exactly one definition.

export interface ThemeManagerOptions {
  /** Namespaces the default storage key, `ui-theme:<module>`. */
  module: string;
  /**
   * The module's shipped default — step 4 of the resolution order, and a
   * typed-config floor: the `system` step resolves under every `matchMedia`
   * outcome, so resolution never falls past step 3. Present by construction
   * — unreachable at runtime.
   */
  default: string;
  /** Step 2 — an optional server- or vault-provided default. */
  serverDefault?: () => string | undefined;
  /** Optional key override; obsidianoid's per-vault template is the client. */
  storageKey?: () => string;
  /** Fired by set() and by nothing else — never by apply() or reresolve(). */
  onChange?: (name: string) => void;
}

/** One rendered picker's buttons, each paired with the theme it selects. */
/** Moves one rendered picker's active mark (or selected option) to `name`. */
type PickerMarker = (name: string) => void;

export class ThemeManager {
  /** The roster pickers enumerate. Deliberately the same set as THEMES and
   * nothing more: `system` is the resolver's implicit step, not an offerable
   * name — absent here, absent from THEMES, and rejected on read-back. */
  readonly list: readonly string[] = THEMES;

  private readonly options: ThemeManagerOptions;
  private readonly media: MediaQueryList;

  /**
   * One row set per picker this instance has rendered, because one instance
   * may render several and they are views of one state. Kept on the instance
   * because the active mark has to follow every theme change, including ones
   * made from outside a picker — set() from another picker, reresolve() once
   * serverDefault() becomes answerable, and the system `change` listener.
   */
  private readonly pickers: PickerMarker[] = [];

  constructor(options: ThemeManagerOptions) {
    this.options = options;
    this.media = matchMedia("(prefers-color-scheme: dark)");
    this.media.addEventListener("change", () => {
      // Live only while the resolution is still reaching the system step: once
      // storage or the server answers, an OS flip must change nothing.
      if (this.storedTheme() !== undefined) return;
      if (this.options.serverDefault?.() !== undefined) return;
      this.apply();
    });
  }

  /** Applies the resolved theme. Callable pre-paint, idempotent, and silent. */
  apply(): void {
    this.stamp(this.resolve());
  }

  /**
   * Re-runs the resolution order against the closures' current values and
   * applies the result, without writing storage. The separate name is the
   * entry point obsidianoid's fetchVaults/switchVault call when the per-vault
   * storage key changes under a live instance. Not writing is the load-bearing
   * half — a writing reresolve() would overwrite the destination vault's saved
   * choice with the source vault's.
   */
  reresolve(): void {
    this.apply();
  }

  /** Writes storage, applies, and fires onChange. */
  set(name: string): void {
    this.adopt(name);
    this.options.onChange?.(name);
  }

  /**
   * Writes storage and applies, WITHOUT firing onChange: for a theme that
   * arrives from elsewhere (slideshow's server state). Using set() there
   * would echo the theme back to its source, which answers with the same
   * state again -- an endless loop.
   */
  adopt(name: string): void {
    localStorage.setItem(this.storageKey(), name);
    this.stamp(name);
  }

  /**
   * The one place a theme becomes the applied theme: apply() and set() both
   * route through here, so a change made anywhere re-marks every rendered
   * picker. setTheme stays the single definition of the DOM write; this adds
   * the mark beside it, and nothing else.
   */
  private stamp(name: string): void {
    setTheme(name);
    this.mark(name);
  }

  private mark(name: string): void {
    for (const marker of this.pickers) marker(name);
  }

  /**
   * Renders the shared theme picker into `host`. Every node is built through
   * createElement and every label is a text node — no markup string is assigned
   * anywhere in this file. No color value reaches this file either: each
   * swatch carries `data-theme`, and themes.css keys every palette on a bare
   * attribute selector.
   *
   * `mode` is a hint from the caller about the space it has: "list" (the
   * default) is one swatch button per theme, for a picker that stands alone;
   * "select" is a compact dropdown, for a picker sharing its space with other
   * settings. HamburgerMenu chooses it from what else the drawer holds.
   */
  renderPicker(host: HTMLElement, mode: "list" | "select" = "list"): void {
    if (mode === "select") {
      const select = document.createElement("select");
      select.className = "ui-theme-select";
      select.setAttribute("aria-label", "Theme");
      for (const name of this.list) {
        const option = document.createElement("option");
        option.value = name;
        option.append(name);
        select.append(option);
      }
      select.addEventListener("change", () => this.set(select.value));
      host.append(select);
      this.pickers.push((name) => {
        select.value = name;
      });
      this.mark(this.resolve());
      return;
    }

    const picker = document.createElement("div");
    picker.className = "ui-theme-picker";

    const rows: Array<{ name: string; button: HTMLButtonElement }> = [];

    for (const name of this.list) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "ui-theme-btn";

      const swatch = document.createElement("span");
      swatch.className = "ui-theme-swatch";
      swatch.dataset.theme = name;
      button.append(swatch, name);

      button.addEventListener("click", () => this.set(name));

      rows.push({ name, button });
      picker.append(button);
    }

    host.append(picker);
    this.pickers.push((name) => {
      for (const row of rows) {
        row.button.className = row.name === name ? "ui-theme-btn is-active" : "ui-theme-btn";
      }
    });
    this.mark(this.resolve());
  }

  private storageKey(): string {
    const override = this.options.storageKey;
    return override ? override() : `ui-theme:${this.options.module}`;
  }

  /**
   * Step 1. Every value read from storage is validated against THEMES before
   * use, so an unknown stored string falls through to the next step rather
   * than stamping a nonexistent theme.
   */
  private storedTheme(): string | undefined {
    const raw = localStorage.getItem(this.storageKey());
    if (raw === null) return undefined;
    return (THEMES as readonly string[]).includes(raw) ? raw : undefined;
  }

  /** Step 3. `matches` → dark; everything else → light. Always resolves. */
  private systemTheme(): string {
    return this.media.matches ? "dark" : "light";
  }

  /**
   * Resolution order: localStorage → serverDefault() → system → default.
   * Step 3 always answers, which makes the trailing `?? this.options.default`
   * a typed-config floor — present, in order, and unreachable at runtime.
   */
  private resolve(): string {
    const stored = this.storedTheme();
    if (stored !== undefined) return stored;
    const server = this.options.serverDefault?.();
    if (server !== undefined) return server;
    return this.systemTheme() ?? this.options.default;
  }
}
