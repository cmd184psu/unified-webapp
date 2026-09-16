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

Each theme section also carries a **Theme picker** row (Phase-2 C3). From the
"Theme picker" section of the sampler — the shared `.ui-theme-picker` swatch
grid that `HamburgerMenu` will mount at C4 — verify per theme:

- **8 swatch fills distinct** — all eight swatches are visible at once and no
  two render the same colour. Each swatch carries its own `data-theme`, so it
  previews *that* theme's `--color-primary` rather than the active theme's
  (ADR-015); eight identical fills is the specific defect this row catches.
- **Swatch border legible** — every swatch's `var(--color-border)` ring is
  visible against the row background, on light and dark swatches alike. This
  is the row that replaces the donor's hard-coded
  `rgba(255,255,255,0.15)`, which was invisible on light themes.
- **Active row marked** — exactly one `.ui-theme-btn` carries `.is-active`, and
  it is the theme currently applied.
- **Hover and focus visible** — hovering a row changes its background, and
  keyboard focus draws a ring (`:focus-visible`) distinguishable from hover.
- **Picking applies and persists** — clicking a swatch re-themes the page
  immediately, moves `.is-active`, updates the header `<select>` to match, and
  survives a reload (`localStorage` key `ui-theme:sampler`).

Note on the sampler's *first* render (Phase-2 C3, sanctioned delta). Before C3
the page rendered `dark` unconditionally: `index.html:2` carries no
`data-theme` and `web/shared/css/themes.css:15`'s `:root` half supplies dark.
From C3 the page is backed by a `ThemeManager`, so with no stored choice its
first render follows the **OS preference** — `light` under
`prefers-color-scheme: light` or `no-preference`, `dark` only when the OS asks
for dark. Set the OS preference explicitly before recording anything that
depends on the starting theme, and note that once any theme has been picked the
stored choice wins over the OS on every later load.

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |

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

| Theme picker | 8 swatch fills distinct | Swatch border legible | Active row marked | Hover and focus visible | Picking applies and persists |
| --- | --- | --- | --- | --- | --- |
| .ui-theme-picker | [ ] | [ ] | [ ] | [ ] | [ ] |
