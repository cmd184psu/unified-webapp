// Pure state decision for the admin Passkeys card, from the status of
// GET /api/auth/passkeys. Kept free of DOM so it is unit-testable.

/** Mirrors auth.ErrPasskeysNotConfiguredMsg; used only if the server sent no text. */
export const PASSKEYS_NOT_CONFIGURED_TEXT =
  "Passkeys are not configured on this server: set the Relying Party ID and allowed origins in Admin > Passkey settings (passkeys also need HTTPS).";

export interface PasskeyCardState {
  kind: "unconfigured" | "locked" | "ready" | "error";
  text: string;
}

export function passkeyCardState(status: number, message?: string): PasskeyCardState {
  if (status === 409) return { kind: "unconfigured", text: message || PASSKEYS_NOT_CONFIGURED_TEXT };
  if (status === 403) return { kind: "locked", text: "Sign in with LDAP to view and manage your passkeys." };
  if (status >= 200 && status < 300) return { kind: "ready", text: "" };
  return { kind: "error", text: (message || "Unable to load passkeys") + " (HTTP " + status + ")" };
}
