// focusable.ts — the one definition of "what counts as a focusable element",
// extracted from web/shared/ts/modal.ts at phase2 C4 (§5 Step 4.1, §10 ledger
// row 8) rather than copied into menu.ts (rule 1: one definition per fact).
//
// modal.ts's focus trap and HamburgerMenu's focus trap are the same trap over
// different roots, so the predicate they share is a parameter-less fact about
// the DOM and belongs in one module. B4.8 is the assertion: the selector
// string occurs exactly once across web/shared/ts/, and the one occurrence is
// here.
//
// PRIVATE, deliberately: this module is NOT re-exported from index.ts, so it
// is not part of the shared library's public surface and does not move
// check-shared-barrel.mjs's counts. Both consumers import it directly.
//
// The selector is modal.ts:68's, byte-for-byte — the "proven predicate" §5
// Step 4.1 names. Extraction is a move, not a rewrite: no element kind was
// added, removed, or reordered, which is what keeps modal.test.ts a valid
// proof that modal.ts's behaviour is unchanged.

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
