// index.ts — the shared library's complete public surface (barrel).
//
// Explicit named re-exports only — no `export *`. This is the one file
// @shared collapses to (driver rule 10 rewrites every `@shared/...`
// specifier to /shared/dist/shared.mjs, this file's bundle), so a symbol
// absent from here is unreachable by any consumer, and a symbol present here
// but forgotten is caught by scripts/check-shared-barrel.mjs's allowlist
// (A5.2) rather than surfacing as a runtime `undefined` import.

export { openModal, confirmDialog, alertDialog, promptDialog } from "./modal.js";
export type { ModalOptions, ModalHandle, DialogOptions, PromptOptions } from "./modal.js";
export { THEMES, setTheme, ThemeManager } from "./theme.js";
export type { ThemeManagerOptions } from "./theme.js";
export { showToast } from "./toast.js";
export type { ToastTone, ToastHandle } from "./toast.js";
export { HamburgerMenu } from "./menu.js";
export type { HamburgerMenuOptions, MenuItem } from "./menu.js";
