/**
 * The single authority for a cert's displayed expiry status.
 *
 * The server sends `notAfter` and its own three-value `status` column
 * (active/archived/quarantined) -- it deliberately never sends "expired" or
 * an "expiringSoon" boolean (see the model.go doc comment: "expired" is
 * never stored). Iteration 1 of this app computed the same expiry rule in
 * two places -- the Go store and the frontend -- against two different
 * clocks, with nothing asserting they agreed, so a superseded ("archived")
 * row with a past `notAfter` could read "expired" in one place and
 * "archived" in the other. Every function in this file is pure (no DOM, no
 * fetch, no `Date.now()` -- `now` is always a parameter) specifically so it
 * can run under `test:web` and so nothing else in the app is tempted to
 * duplicate this logic against a different clock.
 */

/** Single-badge outcome for one cert row. Precedence is fixed, see `badgeFor`. */
export type BadgeKind =
  | "quarantined"
  | "archived"
  | "expired"
  | "expiring-soon"
  | "valid";

/** Display text for each badge kind, in one place so labels can't drift from kinds. */
export const BADGE_LABEL: Record<BadgeKind, string> = {
  quarantined: "Quarantined",
  archived: "Archived",
  expired: "Expired",
  "expiring-soon": "Expiring soon",
  valid: "Valid",
};

/**
 * Rows in this state are de-emphasised and collapsed behind a disclosure by
 * default (FR-9): a fresh list paint should read as "the certs still doing
 * work", not a full history of everything ever issued.
 */
export function isDeemphasized(kind: BadgeKind): boolean {
  return kind === "expired" || kind === "archived";
}

/**
 * Compute the one badge a row shows, highest precedence first:
 * quarantined -> archived -> expired -> expiring-soon -> valid. A row never
 * shows two badges -- a quarantined-and-expired cert shows only "quarantined",
 * an archived-and-expired cert shows only "archived".
 *
 * `expired` is `notAfter < now`, strictly. `expiring-soon` is
 * `now <= notAfter <= now + warnDays`, inclusive on both ends -- a cert
 * expiring at exactly the boundary is still a warning, not yet "valid" nor
 * "expired" until the clock actually passes it. `notAfter === null` (an
 * unparsed legacy certificate) skips the expiry checks entirely; those rows
 * are quarantined in practice, so quarantined/archived have already been
 * resolved by the time a null date would matter here.
 */
export function badgeFor(
  notAfter: string | null,
  status: "active" | "archived" | "quarantined",
  warnDays: number,
  now: Date,
): BadgeKind {
  if (status === "quarantined") return "quarantined";
  if (status === "archived") return "archived";

  if (notAfter === null) return "valid";

  const notAfterMs = new Date(notAfter).getTime();
  if (Number.isNaN(notAfterMs)) return "valid";

  const nowMs = now.getTime();
  if (notAfterMs < nowMs) return "expired";

  const warnMs = warnDays * 24 * 60 * 60 * 1000;
  if (notAfterMs <= nowMs + warnMs) return "expiring-soon";

  return "valid";
}

/** Sentinel returned by `domainGroup` for a name with no parent domain to group under. */
export const NO_DOMAIN_GROUP = "(no domain)";

/**
 * Parent-domain grouping key for a FQDN: the last two dot-separated labels.
 * `foo.cmdhome.net` and `*.cmdhome.net` both group under `cmdhome.net`; a
 * single-label name (`localhost`) has no parent domain and groups under
 * `NO_DOMAIN_GROUP`. Deliberately not public-suffix-aware -- these are
 * local-network names, so `cmdhome.net` grouping a two-label name is exactly
 * right and a `co.uk`-style suffix table would be solving a problem this
 * module doesn't have.
 */
export function domainGroup(fqdn: string): string {
  const labels = fqdn.split(".").filter((l) => l.length > 0);
  if (labels.length < 2) return NO_DOMAIN_GROUP;
  return labels.slice(-2).join(".");
}
