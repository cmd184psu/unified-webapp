// Pure logic behind the admin "Passkey settings" panel. Run with `npm run test:web`.

import {
  addOrigin,
  normalizeOrigins,
  removeOrigin,
  suggestFromOrigin,
  validatePasskeyForm,
} from "./passkeyform";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}
const j = (v: unknown): string => JSON.stringify(v);

check("normalize trims, drops blanks, dedupes",
  j(normalizeOrigins([" https://a.example.com ", "", "https://a.example.com", "https://b.example.com"])) ===
    j(["https://a.example.com", "https://b.example.com"]), j(normalizeOrigins([" https://a.example.com ", "", "https://a.example.com"])));
check("addOrigin appends", j(addOrigin(["a"], "b")) === j(["a", "b"]), j(addOrigin(["a"], "b")));
check("addOrigin with no arg appends an empty row", j(addOrigin(["a"])) === j(["a", ""]), j(addOrigin(["a"])));
check("removeOrigin removes by index", j(removeOrigin(["a", "b", "c"], 1)) === j(["a", "c"]), j(removeOrigin(["a", "b", "c"], 1)));
check("removeOrigin out of range is a no-op", j(removeOrigin(["a"], 5)) === j(["a"]), "noop");

const s1 = suggestFromOrigin("https://certmachine.cmdhome.net", "", []);
check("suggest fills empty RP ID with parent domain", s1.rpId === "cmdhome.net", j(s1));
check("suggest adds the origin", j(s1.origins) === j(["https://certmachine.cmdhome.net"]), j(s1));
check("suggest hint says what it did", s1.hint.includes("cmdhome.net") && s1.hint.includes("https://certmachine.cmdhome.net"), s1.hint);
const s2 = suggestFromOrigin("https://certmachine.cmdhome.net", "example.org", ["https://x.example.org"]);
check("suggest keeps an existing RP ID", s2.rpId === "example.org", j(s2));
check("suggest appends to existing origins", s2.origins.length === 2, j(s2));
const s3 = suggestFromOrigin("https://certmachine.cmdhome.net", "", ["https://certmachine.cmdhome.net"]);
check("suggest does not duplicate an origin", s3.origins.length === 1, j(s3));
check("two-label host is its own RP ID", suggestFromOrigin("https://cmdhome.net", "", []).rpId === "cmdhome.net", "2 labels");
check("localhost keeps localhost", suggestFromOrigin("http://localhost:8080", "", []).rpId === "localhost", "localhost");
check("IP address gets no RP ID guess", suggestFromOrigin("https://10.0.0.5", "", []).rpId === "", "ip");
check("port is kept in the origin", suggestFromOrigin("https://a.b.example.com:8443", "", []).origins[0] === "https://a.b.example.com:8443", "port");

const ok = ["https://certmachine.cmdhome.net"];
check("both empty is fine (disabled)", validatePasskeyForm("", []) === "", validatePasskeyForm("", []));
check("valid passes", validatePasskeyForm("cmdhome.net", ok) === "", validatePasskeyForm("cmdhome.net", ok));
check("id without origins", validatePasskeyForm("cmdhome.net", []).includes("origin"), "x");
check("origins without id", validatePasskeyForm("", ok).includes("Relying Party ID"), "x");
check("id with scheme", validatePasskeyForm("https://cmdhome.net", ok).includes("bare host name"), "x");
check("id with port", validatePasskeyForm("cmdhome.net:443", ok).includes("bare host name"), "x");
check("single label id", validatePasskeyForm("net", ["https://net"]).includes("dot"), "x");
check("localhost dev allowed", validatePasskeyForm("localhost", ["http://localhost:8080"]) === "", "x");
check("http non-localhost", validatePasskeyForm("example.com", ["http://example.com"]).includes("https"), "x");
check("origin with path", validatePasskeyForm("example.com", ["https://example.com/a"]).includes("no path"), "x");
check("origin outside rp id",
  validatePasskeyForm("cmdhome.example", ok) ===
    "certmachine.cmdhome.net is not under cmdhome.example: an origin's host must be the RP ID or a subdomain of it",
  validatePasskeyForm("cmdhome.example", ok));
check("suffix lookalike rejected", validatePasskeyForm("example.com", ["https://badexample.com"]) !== "", "x");
check("blank rows ignored", validatePasskeyForm("cmdhome.net", ["", ...ok]) === "", "x");
check("id is compared case-insensitively", validatePasskeyForm("CmdHome.NET", ok) === "", "x");
