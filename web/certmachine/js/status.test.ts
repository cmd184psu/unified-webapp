// status.ts is the single authority for expiry badges and domain grouping;
// this suite is the proof that its rules match the plan's badge and
// grouping tables exactly, including the precedence order and the exact
// warnDays boundary.
//
// Run with `npm run test:web`. There is no browser here -- the file is
// bundled for node and throws on failure, so a non-zero exit is the whole
// report.

import { badgeFor, domainGroup, isDeemphasized, NO_DOMAIN_GROUP } from "./status";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

const DAY_MS = 24 * 60 * 60 * 1000;
const now = new Date("2026-09-11T00:00:00Z");
const warnDays = 30;

function daysFromNow(days: number): string {
  return new Date(now.getTime() + days * DAY_MS).toISOString();
}

// ---------- badge table ----------

check(
  "past notAfter is expired",
  badgeFor(daysFromNow(-1), "active", warnDays, now) === "expired",
  "expected expired",
);

check(
  "notAfter one day inside the warn window is expiring-soon",
  badgeFor(daysFromNow(warnDays - 1), "active", warnDays, now) === "expiring-soon",
  "expected expiring-soon",
);

check(
  "notAfter one day past the warn window is valid",
  badgeFor(daysFromNow(warnDays + 1), "active", warnDays, now) === "valid",
  "expected valid",
);

check(
  "exact now + warnDays boundary is expiring-soon (inclusive)",
  badgeFor(daysFromNow(warnDays), "active", warnDays, now) === "expiring-soon",
  "expected expiring-soon at the exact boundary",
);

check(
  "archived status wins over a future notAfter",
  badgeFor(daysFromNow(365), "archived", warnDays, now) === "archived",
  "expected archived",
);

check(
  "archived status wins over a past notAfter (archived beats expired)",
  badgeFor(daysFromNow(-30), "archived", warnDays, now) === "archived",
  "expected archived, not expired",
);

check(
  "quarantined beats everything, including a past notAfter",
  badgeFor(daysFromNow(-100), "quarantined", warnDays, now) === "quarantined",
  "expected quarantined",
);

check(
  "quarantined beats archived",
  badgeFor(daysFromNow(365), "quarantined", warnDays, now) === "quarantined",
  "expected quarantined",
);

check(
  "a valid, non-expiring active cert reads valid",
  badgeFor(daysFromNow(365), "active", warnDays, now) === "valid",
  "expected valid",
);

check(
  "a null notAfter (unparsed cert) never crashes and does not read expired",
  badgeFor(null, "active", warnDays, now) === "valid",
  "expected valid as the safe default for an unparseable date",
);

// ---------- de-emphasis ----------

check("expired rows are de-emphasised", isDeemphasized("expired"), "expected true");
check("archived rows are de-emphasised", isDeemphasized("archived"), "expected true");
check("valid rows are not de-emphasised", !isDeemphasized("valid"), "expected false");
check(
  "expiring-soon rows are not de-emphasised",
  !isDeemphasized("expiring-soon"),
  "expected false",
);
check(
  "quarantined rows are not de-emphasised (they need attention, not hiding)",
  !isDeemphasized("quarantined"),
  "expected false",
);

// ---------- grouping table ----------

check(
  "foo.cmdhome.net groups under cmdhome.net",
  domainGroup("foo.cmdhome.net") === "cmdhome.net",
  `got ${domainGroup("foo.cmdhome.net")}`,
);

check(
  "*.cmdhome.net groups under cmdhome.net",
  domainGroup("*.cmdhome.net") === "cmdhome.net",
  `got ${domainGroup("*.cmdhome.net")}`,
);

check(
  "localhost has no parent domain",
  domainGroup("localhost") === NO_DOMAIN_GROUP,
  `got ${domainGroup("localhost")}`,
);

check(
  "a.b.c.example.net groups under example.net",
  domainGroup("a.b.c.example.net") === "example.net",
  `got ${domainGroup("a.b.c.example.net")}`,
);
