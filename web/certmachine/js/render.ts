import type { Cert } from "./types";
import { badgeFor, isDeemphasized, BADGE_LABEL } from "./status";
import type { BadgeKind } from "./status";
import { groupCerts } from "./listmodel";

function el<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  if (className) node.className = className;
  return node;
}

/**
 * `notAfter`/`notBefore` are RFC3339 timestamps; the list only needs the
 * calendar date, not the time-of-day. `null` (unparsed certificate) reads as
 * "unknown" rather than a blank cell, so a quarantined row's missing date is
 * visibly a fact, not a rendering gap.
 */
function formatDate(iso: string | null): string {
  if (iso === null) return "unknown";
  const date = iso.slice(0, 10);
  return date.length === 10 ? date : iso;
}

function badgeElement(kind: BadgeKind): HTMLSpanElement {
  const badge = el("span", "cert-badge");
  badge.dataset.kind = kind;
  // textContent only, never innerHTML: this is what keeps a legacy-imported
  // FQDN (or anything else server-derived) from ever being interpreted as
  // markup, without needing a separate escaping step anywhere in this file.
  badge.textContent = BADGE_LABEL[kind];
  return badge;
}

/**
 * Build one `<li>` for a single cert row, including its inline hot actions
 * and the "Details" action that opens the detail modal (`detail.ts`). The
 * detail entry point is a dedicated button here, not the `.cert-fqdn` span
 * itself -- that node keeps its slice-9 invariant of zero child elements /
 * textContent-only rendering.
 */
export function buildCertRow(
  cert: Cert,
  kind: BadgeKind,
  onOpenDetail: (id: number) => void,
): HTMLLIElement {
  const row = el("li", "cert-row");
  row.dataset.kind = kind;

  const main = el("div", "cert-row-main");
  const fqdn = el("span", "cert-fqdn");
  fqdn.textContent = cert.fqdn;
  main.append(fqdn, badgeElement(kind));

  const meta = el("div", "cert-row-meta");
  const expiry = el("span", "cert-meta-item");
  expiry.textContent =
    kind === "expired"
      ? `Expired ${formatDate(cert.notAfter)}`
      : `Expires ${formatDate(cert.notAfter)}`;
  meta.append(expiry);
  if (cert.importedFrom !== undefined) {
    const imported = el("span", "cert-meta-item cert-meta-imported");
    imported.textContent = "Imported";
    imported.title = cert.importedFrom;
    meta.append(imported);
  }
  row.append(main, meta);

  // Quarantine reason and import-chain warnings each get their own reason
  // text surfaced right on the row (FR-4/FR-9) -- an operator should never
  // have to open a detail view just to learn *why* a row needs attention.
  if (cert.quarantineReason !== undefined) {
    const note = el("p", "cert-row-note");
    note.dataset.tone = "danger";
    note.textContent = `Quarantined: ${cert.quarantineReason}`;
    row.append(note);
  }
  if (cert.importWarning !== undefined) {
    const note = el("p", "cert-row-note");
    note.dataset.tone = "warn";
    note.textContent = cert.importWarning;
    row.append(note);
  }

  const actions = el("div", "cert-row-actions");
  const details = el("button", "cert-action cert-action-btn");
  details.type = "button";
  details.textContent = "Details";
  details.addEventListener("click", () => onOpenDetail(cert.id));
  actions.append(details);
  // haproxy.pem and the bundle are assembled from the row's cert+key against
  // the CA, and the server refuses both for a quarantined row
  // (ErrQuarantinedDownload) -- so rendering them as links offers the operator
  // two actions that are *guaranteed* to fail, and because they are plain
  // anchors rather than fetch calls, clicking one navigates the SPA away to a
  // raw JSON error page and loses the list state. A disabled span carrying the
  // reason says the same thing without the trap.
  for (const [label, href] of downloadActions(cert.id)) {
    actions.append(
      cert.quarantineReason === undefined
        ? downloadLink(label, href)
        : disabledAction(label, `Unavailable: quarantined -- ${cert.quarantineReason}`),
    );
  }
  row.append(actions);

  return row;
}

/** The two inline row downloads, as `[label, href]` pairs. */
function downloadActions(id: number): Array<[string, string]> {
  return [
    ["haproxy.pem", `/api/certs/${id}/files/haproxy.pem`],
    ["bundle (.tgz)", `/api/certs/${id}/bundle`],
  ];
}

/** Plain navigation, not fetch+blob: the server sets Content-Disposition on
 * every download route, so the browser handles the save on its own. */
function downloadLink(label: string, href: string): HTMLAnchorElement {
  const a = el("a", "cert-action");
  a.href = href;
  a.textContent = label;
  return a;
}

function disabledAction(label: string, reason: string): HTMLSpanElement {
  const span = el("span", "cert-action cert-action-disabled");
  span.textContent = label;
  span.title = reason;
  span.setAttribute("aria-disabled", "true");
  return span;
}

function emptyState(message: string): HTMLParagraphElement {
  const p = el("p", "cert-empty");
  p.textContent = message;
  return p;
}

/**
 * Partition `certs` into primary + collapsed-by-default de-emphasized rows
 * and append them to `target` (an element or a fragment), or append a single
 * empty-state message when `certs` is empty. Shared by the flat view and
 * each domain group in the grouped view, so both get the identical
 * expired/archived collapse behavior rather than two subtly different
 * implementations of the same toggle.
 */
function appendRowGroup(
  target: HTMLElement | DocumentFragment,
  certs: Cert[],
  warnDays: number,
  now: Date,
  onOpenDetail: (id: number) => void,
  emptyMessage: string,
  emptyPrimaryMessage: string,
): void {
  if (certs.length === 0) {
    target.appendChild(emptyState(emptyMessage));
    return;
  }

  const primary: HTMLLIElement[] = [];
  const deemphasized: HTMLLIElement[] = [];
  for (const cert of certs) {
    const kind = badgeFor(cert.notAfter, cert.status, warnDays, now);
    const row = buildCertRow(cert, kind, onOpenDetail);
    (isDeemphasized(kind) ? deemphasized : primary).push(row);
  }

  const primaryList = el("ul", "cert-list");
  primaryList.append(...primary);
  if (primary.length === 0) {
    target.appendChild(emptyState(emptyPrimaryMessage));
  } else {
    target.appendChild(primaryList);
  }

  if (deemphasized.length > 0) {
    const toggle = el("button", "cert-toggle");
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", "false");
    const deemphasizedList = el("ul", "cert-list cert-list-deemphasized");
    deemphasizedList.hidden = true;
    deemphasizedList.append(...deemphasized);

    const label = (expanded: boolean): string =>
      `${expanded ? "Hide" : "Show"} ${deemphasized.length} expired/archived certificate${deemphasized.length === 1 ? "" : "s"}`;
    toggle.textContent = label(false);

    toggle.addEventListener("click", () => {
      const expanded = deemphasizedList.hidden;
      deemphasizedList.hidden = !expanded;
      toggle.setAttribute("aria-expanded", String(expanded));
      toggle.textContent = label(expanded);
    });

    target.append(toggle, deemphasizedList);
  }
}

export interface RenderListOptions {
  /** When true, buckets `certs` by `listmodel.groupCerts` before rendering. */
  groupByDomain: boolean;
  /** True when the *unfiltered* cert list is non-empty, so the empty state can
   * distinguish "no certs exist" from "the current search matches nothing". */
  hasAnyCerts: boolean;
  /** Invoked with a cert's id when its "Details" action is activated. */
  onOpenDetail: (id: number) => void;
}

/**
 * Render `certs` -- already filtered and sorted by the caller via
 * `listmodel.ts` (`ui.ts` owns that pipeline) -- into `container` as a
 * single paint: one `DocumentFragment` built up front and appended once,
 * never a per-row `innerHTML +=` (the legacy failure mode this module
 * replaces -- see reference/certmachine/static/app.js).
 *
 * When `options.groupByDomain` is true, certs are bucketed by
 * `listmodel.groupCerts` (itself keyed by `status.domainGroup`); each bucket
 * gets its own collapsible heading and its own independent expired/archived
 * collapse, in the order `groupCerts` returns buckets -- never re-sorted
 * here.
 */
export function renderCertList(
  container: HTMLElement,
  certs: Cert[],
  warnDays: number,
  now: Date,
  options: RenderListOptions,
): void {
  container.textContent = "";

  if (certs.length === 0) {
    container.appendChild(
      emptyState(options.hasAnyCerts ? "No certificates match your search." : "No certificates yet."),
    );
    return;
  }

  const fragment = document.createDocumentFragment();

  if (!options.groupByDomain) {
    appendRowGroup(
      fragment,
      certs,
      warnDays,
      now,
      options.onOpenDetail,
      "No certificates match your search.",
      "No active certificates -- everything is expired or archived.",
    );
  } else {
    for (const group of groupCerts(certs)) {
      const section = el("section", "cert-group");
      const groupBody = el("div", "cert-group-body");

      const headingBtn = el("button", "cert-group-heading");
      headingBtn.type = "button";
      headingBtn.setAttribute("aria-expanded", "true");
      const headingText = `${group.domain} (${group.certs.length})`;
      headingBtn.textContent = headingText;
      headingBtn.addEventListener("click", () => {
        const expanded = headingBtn.getAttribute("aria-expanded") === "true";
        headingBtn.setAttribute("aria-expanded", String(!expanded));
        groupBody.hidden = expanded;
      });

      appendRowGroup(
        groupBody,
        group.certs,
        warnDays,
        now,
        options.onOpenDetail,
        "No certificates match your search.",
        "No active certificates in this group.",
      );

      section.append(headingBtn, groupBody);
      fragment.appendChild(section);
    }
  }

  container.appendChild(fragment);
}
