// focusable.ts — the one definition of "what counts as a focusable element",
// shared by modal.ts's focus trap and HamburgerMenu's focus trap.
//
// PRIVATE, deliberately: this module is NOT re-exported from index.ts, so it
// is not part of the shared library's public surface and does not move
// check-shared-barrel.mjs's counts. Both consumers import it directly.

/**
 * Tab-reachable element kinds, in document order once queried. `[tabindex]`
 * with an explicit -1 is excluded because such an element is
 * programmatically focusable but not Tab-reachable, and a focus trap is about
 * the Tab order.
 */
const FOCUSABLE_SELECTOR =
  'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** Every Tab-reachable descendant of `root`, in document order. */
export function getFocusable(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR));
}
