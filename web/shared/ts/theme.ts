// theme.ts — the one JS-side source of the theme list (FRD :226-228:
// "Adding a theme = adding one CSS block + one entry to a single THEMES
// array"). THEMES must match the 8 depth-1 selectors web/shared/css/themes.css
// declares (A1.4/clause 4 there), and setTheme(name) is the one function that
// switches themes at runtime, by writing the data-theme attribute themes.css
// keys off of (Step 1.0).

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

// --- ThemeManager (phase2 §5 Step 3.1, ADR-008) -----------------------------
//
// One class over the three strategies FR-3 unifies (FRD :230-261). THEMES and
// setTheme above are unchanged: the class CALLS setTheme rather than
// re-implementing the DOM write, so "how a theme is applied" keeps exactly one
// definition and setTheme stays the documented primitive.

export interface ThemeManagerOptions {
  /** Namespaces the default storage key, `ui-theme:<module>`. */
  module: string;
  /**
   * The module's shipped default — step 4 of the resolution order, and a
   * TYPED-CONFIG FLOOR rather than a reachable branch: the `system` step
   * resolves under every `matchMedia` outcome, so resolution never falls out
   * of step 3 (§5 Step 3.1, §18 Critic finding 1). It is asserted by
   * construction — the option exists and each adopter's construction site
   * carries a value — never by resolution.
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
type SwatchRows = ReadonlyArray<{ readonly name: string; readonly button: HTMLButtonElement }>;

export class ThemeManager {
  /**
   * The roster pickers enumerate (FRD :256). Deliberately the same set as
   * THEMES and nothing more (B3.5): `system` is the resolver's implicit third
   * step, not an offerable name — it is absent here, absent from THEMES, and
   * rejected on read-back from storage (§10 ledger row 22).
   */
  readonly list: readonly string[] = THEMES;

  private readonly options: ThemeManagerOptions;
  private readonly media: MediaQueryList;

  /**
   * One row set per picker this instance has rendered, because one instance
   * may render SEVERAL (the sampler's page section and its drawer; a module's
   * drawer alone) and they are three views of one state. Kept on the instance
   * because the active mark has to follow every theme change, including the
   * ones made from outside a picker — set() from another picker, reresolve()
   * once serverDefault() becomes answerable, and the system `change` listener
   * (C5's browser leg: an eagerly built obsidianoid drawer marked the swatch
   * resolution reached at RENDER time and then never moved it, so the mark sat
   * on the wrong swatch, which is worse than no mark at all).
   */
  private readonly pickers: SwatchRows[] = [];

  constructor(options: ThemeManagerOptions) {
    this.options = options;
    // No feature detection, deliberately: matchMedia is universally available
    // in every browser this repo serves, and a guard would be an untested
    // branch (B3.1's "Runtime guard — deliberately none").
    this.media = matchMedia("(prefers-color-scheme: dark)");
    this.media.addEventListener("change", () => {
      // Live only while the resolution is still REACHING the system step: once
      // storage or the server answers, an OS flip must change nothing (B3.4).
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
   * Re-runs the resolution order against the closures' CURRENT values and
   * applies the result, without writing storage (ADR-008's v2 amendment,
   * B3.10). Same operation as apply(): the separate name is the entry point
   * obsidianoid's fetchVaults/switchVault call when the per-vault storage key
   * changes under a live instance, and delegating keeps resolution in one
   * place. Not writing is the load-bearing half — a writing reresolve() would
   * overwrite the destination vault's saved choice with the source vault's.
   */
  reresolve(): void {
    this.apply();
  }

  /** Writes storage, applies, and fires onChange. */
  set(name: string): void {
    localStorage.setItem(this.storageKey(), name);
    this.stamp(name);
    this.options.onChange?.(name);
  }

  /**
   * The one place a theme becomes the applied theme: apply() (and therefore
   * reresolve() and the system `change` listener) and set() both route through
   * here, so a change made anywhere re-marks every rendered picker. setTheme
   * stays the single definition of the DOM write; this adds the mark beside
   * it, and nothing else.
   */
  private stamp(name: string): void {
    setTheme(name);
    this.mark(name);
  }

  /** Moves the active mark to `name`'s swatch in every rendered picker. */
  private mark(name: string): void {
    for (const rows of this.pickers) {
      for (const row of rows) {
        row.button.className = row.name === name ? "ui-theme-btn is-active" : "ui-theme-btn";
      }
    }
  }

  /**
   * Renders the shared swatch picker into `host` — the widget HamburgerMenu
   * mounts at C4, so it has one definition and lives here beside the
   * resolution it drives. Every node is built through createElement and every
   * label is a text node — no markup string is assigned anywhere in this file
   * (B3.6), unlike the donor at obsidianoid's `app.ts:438`. No colour value
   * reaches this file either: each swatch carries `data-theme`, and themes.css
   * keys every palette on a BARE attribute selector, so a swatch sets its OWN
   * --color-primary and eight swatches render eight fills in one open picker
   * (ADR-015, B4.2).
   */
  renderPicker(host: HTMLElement): void {
    const picker = document.createElement("div");
    picker.className = "ui-theme-picker";

    const rows: Array<{ name: string; button: HTMLButtonElement }> = [];

    for (const name of this.list) {
      const button = document.createElement("button");
      button.type = "button";
      button.className = "ui-theme-btn";

      const swatch = document.createElement("span");
      swatch.className = "ui-theme-swatch";
      swatch.dataset.theme = name; // <- this is what makes the fill differ
      button.append(swatch, name); // the label is a text node, not markup

      // set() re-marks every picker on this instance, so a choice made here
      // needs no local mark-moving loop and lands on sibling pickers too.
      button.addEventListener("click", () => this.set(name));

      rows.push({ name, button });
      picker.append(button);
    }

    host.append(picker);
    this.pickers.push(rows);
    // The initial mark, from the resolution in force right now. It marks only:
    // rendering must not stamp the attribute, because a picker can be built
    // before its module has applied anything.
    this.mark(this.resolve());
  }

  /** The storage key in force right now — `ui-theme:<module>` unless overridden. */
  private storageKey(): string {
    const override = this.options.storageKey;
    return override ? override() : `ui-theme:${this.options.module}`;
  }

  /**
   * Step 1. Every value read from storage is validated against THEMES before
   * use, so an unknown stored string falls through to the next step rather
   * than stamping a nonexistent theme (B3.2).
   */
  private storedTheme(): string | undefined {
    const raw = localStorage.getItem(this.storageKey());
    if (raw === null) return undefined;
    return (THEMES as readonly string[]).includes(raw) ? raw : undefined;
  }

  /**
   * Step 3. `matches` → dark; everything else, `no-preference` included,
   * → light (§15 row 9). This step therefore ALWAYS resolves.
   */
  private systemTheme(): string {
    return this.media.matches ? "dark" : "light";
  }

  /**
   * The resolution order, FRD :245-247: localStorage → serverDefault() →
   * system → default. Step 3 always answers, which makes the trailing
   * `?? this.options.default` the typed-config floor §5 Step 3.1 declares it
   * to be — present, in order, and unreachable at runtime.
   */
  private resolve(): string {
    const stored = this.storedTheme();
    if (stored !== undefined) return stored;
    const server = this.options.serverDefault?.();
    if (server !== undefined) return server;
    return this.systemTheme() ?? this.options.default;
  }
}
