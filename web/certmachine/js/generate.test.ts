// Coverage for the generate form's pure pre-validation helpers. All three
// exist only to spare a round trip, which makes a *wrong* answer worse than no
// check at all: rejecting what the server accepts (isValidIPv6 and compressed
// addresses) or accepting what the server rejects (the SAN cap, which counts
// the FQDN) both hand the operator a contradiction.
//
// Run with `npm run test:web`. There is no browser here -- the file is bundled
// for node and throws on failure, so a non-zero exit is the whole report.

import { isValidIPv6, sanCountError, clampedNoticeText, MAX_SAN_COUNT } from "./generate";
import type { CertMutationResponse } from "./types";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

// --- isValidIPv6 (F13) -----------------------------------------------------

const validV6 = [
  "::1",
  "::",
  "2001:db8::1",
  "2001:0db8:0000:0000:0000:0000:0000:0001",
  "fe80::a00:27ff:fe4e:66a1",
  "::ffff:192.0.2.1",
  "2001:db8:0:0:0:0:192.0.2.1",
];
for (const addr of validV6) {
  check(`isValidIPv6 accepts ${addr}`, isValidIPv6(addr), "expected accept, got reject");
}

const invalidV6 = [
  ":::",
  "1::2::3",
  "2001:db8:::1",
  "12345::1",
  "2001:db8:0:0:0:0:0:0:1", // nine groups
  "2001:0db8:0000:0000:0000:0000:0000", // seven groups, uncompressed
  "1:2:3:4:5:6:7:8:9",
  "192.0.2.1", // IPv4 is isValidIPv4's job, not this one's
  "",
  "nope",
  "2001:db8::1::",
  "::192.0.2.1:ffff", // embedded IPv4 is only legal as the final group
  "2001:db8::g1",
];
for (const addr of invalidV6) {
  check(`isValidIPv6 rejects ${JSON.stringify(addr)}`, !isValidIPv6(addr), "expected reject, got accept");
}

// --- sanCountError (F12) ---------------------------------------------------

const names = (n: number, prefix = "h"): string[] =>
  Array.from({ length: n }, (_, i) => `${prefix}${i}.example.local`);

check(
  "the FQDN plus 63 distinct DNS SANs is exactly at the cap",
  sanCountError("example.local", names(MAX_SAN_COUNT - 1), []) === null,
  `got ${String(sanCountError("example.local", names(MAX_SAN_COUNT - 1), []))}`,
);

check(
  // The off-by-one the server rejected and the old client let through: 64
  // dnsSans passes the raw check, but the issued cert also carries the FQDN.
  "the FQDN plus 64 distinct DNS SANs is over the cap",
  sanCountError("example.local", names(MAX_SAN_COUNT), []) !== null,
  "expected the deduplicated count to reject 65 total names",
);

check(
  "the over-cap message names the total including the FQDN",
  (sanCountError("example.local", names(MAX_SAN_COUNT), []) ?? "").includes("65"),
  `got ${String(sanCountError("example.local", names(MAX_SAN_COUNT), []))}`,
);

check(
  // Matches the server's dedupeDNSNames: the FQDN is already in the set, so
  // listing it again as a SAN costs nothing.
  "a DNS SAN repeating the FQDN is not counted twice",
  sanCountError("example.local", ["example.local", ...names(MAX_SAN_COUNT - 1)], []) === null,
  `got ${String(sanCountError("example.local", ["example.local", ...names(MAX_SAN_COUNT - 1)], []))}`,
);

check(
  "dedupe matches the server's normalization (trailing dot, case)",
  sanCountError("Example.Local", ["example.local.", "EXAMPLE.LOCAL"], []) === null &&
    sanCountError("Example.Local", ["example.local.", "EXAMPLE.LOCAL"], names(MAX_SAN_COUNT - 1)) !== null,
  "expected the three spellings to collapse to one counted name",
);

check(
  "IP SANs count toward the same cap and never deduplicate against DNS names",
  sanCountError("example.local", names(MAX_SAN_COUNT - 2), ["10.0.0.1"]) === null &&
    sanCountError("example.local", names(MAX_SAN_COUNT - 1), ["10.0.0.1"]) !== null,
  "expected FQDN + 63 DNS + 1 IP to exceed the cap",
);

check(
  // The server's cheap pre-check fires on raw array length before any
  // normalization, so a wildly oversized array is refused even if it would
  // have deduplicated down under the cap.
  "a raw array over the cap is refused even when every entry is identical",
  sanCountError("example.local", new Array<string>(MAX_SAN_COUNT + 1).fill("dup.example.local"), []) !== null,
  "expected the raw-length pre-check to reject",
);

check(
  "no SANs at all is fine",
  sanCountError("example.local", [], []) === null,
  `got ${String(sanCountError("example.local", [], []))}`,
);

// --- clampedNoticeText (F20) ----------------------------------------------

function mutation(overrides: Partial<CertMutationResponse>): CertMutationResponse {
  return {
    cert: {
      id: 1,
      fqdn: "example.local",
      sans: { dns: [], ip: [] },
      serial: "01",
      fingerprint: "aa:bb",
      notBefore: "2026-01-01T00:00:00Z",
      notAfter: "2026-03-02T00:00:00Z",
      created: "2026-01-01T00:00:00Z",
      status: "active",
      ...overrides.cert,
    },
    validityClamped: true,
    ...overrides,
  };
}

check(
  "an unclamped response produces no notice",
  clampedNoticeText(365, mutation({ validityClamped: false })) === null,
  "expected null",
);

const clamped = clampedNoticeText(365, mutation({ requestedNotAfter: "2026-12-27T00:00:00Z" }));
check(
  "a clamped notice states the issued length and the CA's expiry",
  (clamped ?? "").includes("60 days") && (clamped ?? "").includes("2026-03-02"),
  `got ${String(clamped)}`,
);
check(
  // The server sends requestedNotAfter only when clamping; before F20 nothing
  // displayed it, so the operator never learned what the full term would have been.
  "a clamped notice reports the expiry the full term would have reached",
  (clamped ?? "").includes("2026-12-27"),
  `got ${String(clamped)}`,
);
check(
  "a clamped response without requestedNotAfter still produces a usable notice",
  (clampedNoticeText(365, mutation({})) ?? "").includes("60 days") &&
    !(clampedNoticeText(365, mutation({})) ?? "").includes("undefined"),
  `got ${String(clampedNoticeText(365, mutation({})))}`,
);
