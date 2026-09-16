# Sampler manual-verification checklist

This is the manual verification surface for the behaviour Phase-1 automated
tests do not cover (ADR-005: no jsdom). It is checked once per theme before
C5 is considered done, and re-run in every later phase that touches shared
CSS (`web/shared/css/`) or the shared modal primitive
(`web/shared/ts/modal.ts`).

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

Do not check the boxes below without actually performing the pass in a
browser for that theme.

## dark

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## light

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

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

## forest

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## ocean

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## ember

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## rose

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |

## puma

| Dialog | Tab cycles+wraps | Shift-Tab wraps back | Escape closes | Focus returns to invoker | Backdrop mousedown closes | Primary-button text legible | Nothing unreadable |
| --- | --- | --- | --- | --- | --- | --- | --- |
| openModal | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| confirmDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| alertDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
| promptDialog | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] | [ ] |
