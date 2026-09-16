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
