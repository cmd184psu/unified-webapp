// listmodel.ts is the pure list-model layer behind search/sort/group; this
// suite is what makes that extraction real coverage rather than an untested
// refactor (the Critic's minor #7 on the slice 9/10 split).
//
// Run with `npm run test:web`. There is no browser here -- the file is
// bundled for node and throws on failure, so a non-zero exit is the whole
// report.

import { filterCerts, sortCerts, groupCerts } from "./listmodel";
import type { Cert } from "./types";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

function makeCert(overrides: Partial<Cert> & { id: number; fqdn: string }): Cert {
  return {
    id: overrides.id,
    fqdn: overrides.fqdn,
    serial: overrides.serial ?? `SERIAL-${overrides.id}`,
    notBefore: overrides.notBefore ?? "2026-01-01T00:00:00Z",
    // `??` would treat an explicitly-passed `null` (the "unparsed cert"
    // fixture) as nullish and silently fall back to the default date, which
    // is exactly the bug this comment is here to prevent from recurring.
    notAfter: "notAfter" in overrides ? (overrides.notAfter as string | null) : "2027-01-01T00:00:00Z",
    sans: overrides.sans ?? { dns: [], ip: [] },
    fingerprint: overrides.fingerprint ?? `FP-${overrides.id}`,
    status: overrides.status ?? "active",
    created: overrides.created ?? "2026-01-01T00:00:00Z",
    ...(overrides.certPem !== undefined ? { certPem: overrides.certPem } : {}),
    ...(overrides.importedFrom !== undefined ? { importedFrom: overrides.importedFrom } : {}),
    ...(overrides.quarantineReason !== undefined
      ? { quarantineReason: overrides.quarantineReason }
      : {}),
    ...(overrides.importWarning !== undefined ? { importWarning: overrides.importWarning } : {}),
  };
}

// ---------- filterCerts ----------

const certs: Cert[] = [
  makeCert({ id: 1, fqdn: "app.cmdhome.net", sans: { dns: ["app.cmdhome.net"], ip: [] } }),
  makeCert({
    id: 2,
    fqdn: "db.cmdhome.net",
    sans: { dns: ["db.cmdhome.net", "sql.internal.local"], ip: ["10.0.0.5"] },
  }),
  makeCert({ id: 3, fqdn: "printer.local", sans: { dns: ["printer.local"], ip: [] } }),
];

check(
  "empty query matches everything, unchanged order",
  filterCerts(certs, "").map((c) => c.id).join(",") === "1,2,3",
  `got ${filterCerts(certs, "").map((c) => c.id).join(",")}`,
);

check(
  "whitespace-only query matches everything",
  filterCerts(certs, "   ").length === 3,
  `got ${filterCerts(certs, "   ").length}`,
);

check(
  "query matches on FQDN",
  filterCerts(certs, "printer").map((c) => c.id).join(",") === "3",
  `got ${filterCerts(certs, "printer").map((c) => c.id).join(",")}`,
);

check(
  "query is case-insensitive",
  filterCerts(certs, "PRINTER").length === 1,
  `got ${filterCerts(certs, "PRINTER").length}`,
);

check(
  "query matches a DNS SAN even when it does not match the primary FQDN",
  filterCerts(certs, "sql.internal.local").map((c) => c.id).join(",") === "2",
  `got ${filterCerts(certs, "sql.internal.local").map((c) => c.id).join(",")}`,
);

check(
  "query matches an IP SAN",
  filterCerts(certs, "10.0.0.5").map((c) => c.id).join(",") === "2",
  `got ${filterCerts(certs, "10.0.0.5").map((c) => c.id).join(",")}`,
);

check(
  "query matching nothing returns an empty array",
  filterCerts(certs, "nonexistent.example").length === 0,
  `got ${filterCerts(certs, "nonexistent.example").length}`,
);

check(
  "filterCerts does not mutate its input",
  certs.length === 3,
  "expected the original certs array to be untouched",
);

// ---------- sortCerts: name ----------

const byName: Cert[] = [
  makeCert({ id: 1, fqdn: "zebra.local" }),
  makeCert({ id: 2, fqdn: "apple.local" }),
  makeCert({ id: 3, fqdn: "mango.local" }),
];

check(
  "sort by name ascending",
  sortCerts(byName, "name", "asc").map((c) => c.fqdn).join(",") ===
    "apple.local,mango.local,zebra.local",
  `got ${sortCerts(byName, "name", "asc").map((c) => c.fqdn).join(",")}`,
);

check(
  "sort by name descending",
  sortCerts(byName, "name", "desc").map((c) => c.fqdn).join(",") ===
    "zebra.local,mango.local,apple.local",
  `got ${sortCerts(byName, "name", "desc").map((c) => c.fqdn).join(",")}`,
);

const nameTies: Cert[] = [
  makeCert({ id: 1, fqdn: "tie.local" }),
  makeCert({ id: 2, fqdn: "tie.local" }),
  makeCert({ id: 3, fqdn: "tie.local" }),
];

check(
  "sort by name preserves input order for ties (ascending)",
  sortCerts(nameTies, "name", "asc").map((c) => c.id).join(",") === "1,2,3",
  `got ${sortCerts(nameTies, "name", "asc").map((c) => c.id).join(",")}`,
);

check(
  "sort by name preserves input order for ties (descending)",
  sortCerts(nameTies, "name", "desc").map((c) => c.id).join(",") === "1,2,3",
  `got ${sortCerts(nameTies, "name", "desc").map((c) => c.id).join(",")}`,
);

check(
  "sortCerts does not mutate its input",
  byName[0].fqdn === "zebra.local",
  "expected the original array's order to be untouched",
);

// ---------- sortCerts: created ----------

const byCreated: Cert[] = [
  makeCert({ id: 1, fqdn: "a.local", created: "2026-06-01T00:00:00Z" }),
  makeCert({ id: 2, fqdn: "b.local", created: "2026-01-01T00:00:00Z" }),
  makeCert({ id: 3, fqdn: "c.local", created: "2026-12-01T00:00:00Z" }),
];

check(
  "sort by created ascending",
  sortCerts(byCreated, "created", "asc").map((c) => c.id).join(",") === "2,1,3",
  `got ${sortCerts(byCreated, "created", "asc").map((c) => c.id).join(",")}`,
);

check(
  "sort by created descending",
  sortCerts(byCreated, "created", "desc").map((c) => c.id).join(",") === "3,1,2",
  `got ${sortCerts(byCreated, "created", "desc").map((c) => c.id).join(",")}`,
);

// ---------- sortCerts: expiry ----------

const byExpiry: Cert[] = [
  makeCert({ id: 1, fqdn: "a.local", notAfter: "2027-06-01T00:00:00Z" }),
  makeCert({ id: 2, fqdn: "b.local", notAfter: "2026-01-01T00:00:00Z" }),
  makeCert({ id: 3, fqdn: "c.local", notAfter: null }),
  makeCert({ id: 4, fqdn: "d.local", notAfter: "2026-12-01T00:00:00Z" }),
];

check(
  "sort by expiry ascending: earliest first, null (unparsed) last",
  sortCerts(byExpiry, "expiry", "asc").map((c) => c.id).join(",") === "2,4,1,3",
  `got ${sortCerts(byExpiry, "expiry", "asc").map((c) => c.id).join(",")}`,
);

check(
  "sort by expiry descending: latest first, null (unparsed) STILL last",
  sortCerts(byExpiry, "expiry", "desc").map((c) => c.id).join(",") === "1,4,2,3",
  `got ${sortCerts(byExpiry, "expiry", "desc").map((c) => c.id).join(",")}`,
);

const expiryTies: Cert[] = [
  makeCert({ id: 1, fqdn: "a.local", notAfter: "2027-01-01T00:00:00Z" }),
  makeCert({ id: 2, fqdn: "b.local", notAfter: "2027-01-01T00:00:00Z" }),
];

check(
  "sort by expiry preserves input order for ties",
  sortCerts(expiryTies, "expiry", "asc").map((c) => c.id).join(",") === "1,2",
  `got ${sortCerts(expiryTies, "expiry", "asc").map((c) => c.id).join(",")}`,
);

// ---------- groupCerts ----------

const toGroup: Cert[] = [
  makeCert({ id: 1, fqdn: "foo.cmdhome.net" }),
  makeCert({ id: 2, fqdn: "bar.cmdhome.net" }),
  makeCert({ id: 3, fqdn: "printer.local" }),
  makeCert({ id: 4, fqdn: "standalone" }),
];

const groups = groupCerts(toGroup);

check(
  "groupCerts collapses multiple FQDNs under one parent domain",
  groups.find((g) => g.domain === "cmdhome.net")?.certs.map((c) => c.id).join(",") === "1,2",
  `got ${JSON.stringify(groups)}`,
);

check(
  "groupCerts preserves first-appearance order of groups",
  // printer.local is itself exactly two labels, so it groups under its own
  // full name (domainGroup has no public-suffix awareness -- see status.ts).
  groups.map((g) => g.domain).join("|") === "cmdhome.net|printer.local|(no domain)",
  `got ${groups.map((g) => g.domain).join("|")}`,
);

check(
  "groupCerts preserves each group's cert order from the input",
  groups.find((g) => g.domain === "cmdhome.net")?.certs[0].fqdn === "foo.cmdhome.net",
  "expected foo.cmdhome.net to remain first within its group",
);

check(
  "a single-label FQDN groups under the (no domain) sentinel",
  groups.find((g) => g.domain === "(no domain)")?.certs.map((c) => c.id).join(",") === "4",
  `got ${JSON.stringify(groups)}`,
);

check(
  "groupCerts covers every input cert exactly once",
  groups.reduce((n, g) => n + g.certs.length, 0) === toGroup.length,
  `got ${groups.reduce((n, g) => n + g.certs.length, 0)}`,
);
