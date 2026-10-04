// passkeyCardState decides what the admin Passkeys card shows from the
// status of GET /api/auth/passkeys. Run with `npm run test:web`.

import { passkeyCardState, PASSKEYS_NOT_CONFIGURED_TEXT } from "./passkeystate";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

const unconf = passkeyCardState(409, "server says no");
check("409 -> unconfigured", unconf.kind === "unconfigured", JSON.stringify(unconf));
check("409 carries the server message", unconf.text === "server says no", unconf.text);
check(
  "409 without a message falls back to the shared text",
  passkeyCardState(409).text === PASSKEYS_NOT_CONFIGURED_TEXT && PASSKEYS_NOT_CONFIGURED_TEXT.includes("Passkey settings") && !PASSKEYS_NOT_CONFIGURED_TEXT.includes("auth.passkey"),
  passkeyCardState(409).text,
);
check("403 -> locked", passkeyCardState(403, "x").kind === "locked", "kind");
check("200 -> ready", passkeyCardState(200).kind === "ready", "kind");
const err = passkeyCardState(500, "boom");
check("other -> error", err.kind === "error", err.kind);
check("error text has status and message", err.text.includes("500") && err.text.includes("boom"), err.text);
check("error text without message still has status", passkeyCardState(0).text.includes("0"), passkeyCardState(0).text);
