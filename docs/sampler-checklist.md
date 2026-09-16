# Sampler manual-verification checklist

This is the manual verification surface for the behaviour Phase-1 automated
tests do not cover (ADR-005: no jsdom). It is checked once per theme before
C5 is considered done, and re-run in every later phase that touches shared
CSS (`web/shared/css/`) or a shared primitive (`web/shared/ts/modal.ts`,
`web/shared/ts/toast.ts`).

For each of the 8 themes (`THEMES` in `web/shared/ts/theme.ts`), open the
sampler page (`/`), select the theme from the theme `<select>`, then open
each of the four dialogs (`openModal`, `confirmDialog`, `alertDialog`,
`promptDialog`) from the "Modals & dialogs" section and verify:

- **Tab cycles** — Tab moves focus through every focusable element in the
  panel and wraps from the last back to the first.
- **Shift-Tab wraps back** — Shift-Tab moves focus backwards and wraps from
  the first element to the last.
- **Escape closes** — pressing Escape closes the dialog.
- **Focus returns to invoker** — after closing (by any means), focus returns
  to the button that opened the dialog.
- **Backdrop mousedown closes** — a mousedown on the overlay outside the
  panel closes the dialog.
- **Primary-button text legible** — the primary button's text
  (`--color-primary-fg` on `--color-primary`) is legible, not washed out
  against the button background.
- **Nothing unreadable** — no text or control anywhere in the dialog is
  unreadable against its background in this theme.

Note on the `openModal` row's first two columns (Phase-2 C1). Before C1 the
`openModal` demo's content was a single `<p>`, so `getFocusable()` in
`web/shared/ts/modal.ts` returned an empty array for it: Tab and Shift-Tab
were preventDefaulted and focus was parked on the panel. "Tab cycles+wraps"
and "Shift-Tab wraps back" were therefore **vacuous** for that one row in all
8 sections — 8 boolean pairs that could not have been false. C1 gives the demo
a `Close` button inside a wrapping `<div>`, so from C1 onward both columns are
meaningful for `openModal` as they already were for the three dialog helpers,
which have always built real buttons. Any tick in those two cells recorded
before C1 should be re-taken.

Each theme section also carries a **Toasts** row (Phase-2 C2). From the
"Toasts" section of the sampler, press each of the three tone buttons and then
the "3 at once" button, and verify per theme:

- **success / error / notice legible** — the toast's text and its tone border
  are both readable against `--color-surface-1` in this theme, and the three
  tones are distinguishable from one another.
- **error stays until dismissed** — the `error` toast is sticky
  (`durationMs === 0`); it must still be on screen after the `success` and
  `notice` toasts have auto-dismissed.
- **× dismisses** — the close button removes that one toast and leaves the
  others alone.
- **3 at once stack in one region** — the three toasts appear in a single
  bottom-right stack (one `role="status"` region), newest below, and none
  overlaps the others.

Do not check the boxes below without actually performing the pass in a
browser for that theme.

## dark

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## light

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## obsidian

Legibility note (deferred to this document, §9 item 1): obsidian's contrast
against `--color-primary`/`--color-primary-fg` needs a dedicated look here
before this theme's row can be checked off.

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## forest

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## ocean

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## ember

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## rose

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## puma

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

| Toasts | success legible | error legible | notice legible | error stays until dismissed | × dismisses | 3 at once stack in one region |
| --- | --- | --- | --- | --- | --- | --- |
| showToast | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
