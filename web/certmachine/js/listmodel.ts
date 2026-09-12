/**
 * Pure, DOM-free list-model functions for the cert list: filtering, sorting,
 * and domain grouping. Extracted as their own module (rather than living
 * inline in `ui.ts`/`render.ts`) specifically so they can be unit-tested
 * under `npm run test:web` without a browser -- this was one of the Critic's
 * conditions on splitting slice 9/10 (their minor #7).
 *
 * `status.ts` remains the single authority for expiry *badges*; this file
 * only reorders/filters rows and reuses `status.domainGroup` for the
 * grouping key rather than re-deriving it.
 */

import type { Cert } from "./types";
import { domainGroup } from "./status";

/**
 * Case-insensitive substring match against a cert's FQDN and every DNS/IP
 * SAN. An empty (or whitespace-only) query matches everything, unchanged
 * order -- this is what lets the toolbar call `filterCerts` unconditionally
 * on every keystroke, including the empty string at load time.
 */
export function filterCerts(certs: Cert[], query: string): Cert[] {
  const q = query.trim().toLowerCase();
  if (q === "") return certs;

  return certs.filter((cert) => {
    if (cert.fqdn.toLowerCase().includes(q)) return true;
    if (cert.sans.dns.some((d) => d.toLowerCase().includes(q))) return true;
    if (cert.sans.ip.some((ip) => ip.toLowerCase().includes(q))) return true;
    return false;
  });
}

export type SortKey = "name" | "created" | "expiry";
export type SortDir = "asc" | "desc";

/**
 * Compare two certs by `notAfter` for the "expiry" sort key. A `null`
 * `notAfter` (an unparsed certificate) always sorts to the end of the list,
 * in *either* direction -- an unknown expiry isn't meaningfully "later" or
 * "earlier" than a real date, so flipping the sort direction shouldn't move
 * these rows to the front. This mirrors `status.ts`'s treatment of `null`
 * as "skip the real comparison, use the safe/boring default".
 */
function compareExpiry(a: Cert, b: Cert, dir: SortDir): number {
  if (a.notAfter === null && b.notAfter === null) return 0;
  if (a.notAfter === null) return 1;
  if (b.notAfter === null) return -1;
  const cmp = new Date(a.notAfter).getTime() - new Date(b.notAfter).getTime();
  return dir === "asc" ? cmp : -cmp;
}

/**
 * Sort a copy of `certs` by the given key/direction. Never mutates the
 * input array. Relies on `Array.prototype.sort` being a stable sort (spec'd
 * since ES2019, true of every target this app ships to) so that equal keys
 * preserve their relative input order in both directions -- there is no
 * explicit tiebreaker here, and none is needed.
 */
export function sortCerts(certs: Cert[], key: SortKey, dir: SortDir): Cert[] {
  const sign = dir === "asc" ? 1 : -1;
  const sorted = [...certs];

  sorted.sort((a, b) => {
    switch (key) {
      case "name":
        return sign * a.fqdn.toLowerCase().localeCompare(b.fqdn.toLowerCase());
      case "created":
        // `created` is a fixed-width RFC3339 UTC string (see the slice 8
        // handoff), so lexical comparison is chronological comparison.
        return sign * (a.created < b.created ? -1 : a.created > b.created ? 1 : 0);
      case "expiry":
        return compareExpiry(a, b, dir);
    }
  });

  return sorted;
}

/** One domain bucket, in the order `groupCerts` first encountered it. */
export interface CertGroup {
  domain: string;
  certs: Cert[];
}

/**
 * Bucket `certs` by `status.domainGroup(cert.fqdn)`. Buckets are returned in
 * first-appearance order and each bucket's certs keep their relative order
 * from the input -- grouping never re-sorts. Callers that want a sorted
 * grouped view should call `sortCerts` first and pass the result in here.
 */
export function groupCerts(certs: Cert[]): CertGroup[] {
  const order: string[] = [];
  const buckets = new Map<string, Cert[]>();

  for (const cert of certs) {
    const key = domainGroup(cert.fqdn);
    const bucket = buckets.get(key);
    if (bucket) {
      bucket.push(cert);
    } else {
      buckets.set(key, [cert]);
      order.push(key);
    }
  }

  return order.map((domain) => ({ domain, certs: buckets.get(domain) as Cert[] }));
}
