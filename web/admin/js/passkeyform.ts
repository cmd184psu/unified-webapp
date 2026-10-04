// Pure logic behind the admin "Passkey settings" panel, free of DOM so it is
// unit-testable. Client validation mirrors only the cheap server checks and is
// advisory: the server (PUT /api/config/passkey) is authoritative.

export const MAX_ORIGINS = 64;

/** Trim every origin, drop blanks and duplicates, keep order. */
export function normalizeOrigins(list: string[]): string[] {
  const out: string[] = [];
  for (const o of list) {
    const t = o.trim();
    if (t && !out.includes(t)) out.push(t);
  }
  return out;
}

/** Append an origin (or an empty row for typing when none is given). */
export function addOrigin(list: string[], origin = ""): string[] {
  return [...list, origin];
}

export function removeOrigin(list: string[], index: number): string[] {
  return list.filter((_, i) => i !== index);
}

export interface Suggestion {
  origins: string[];
  rpId: string;
  hint: string;
}

/**
 * "Use this page's address": add the origin and, when the RP ID is empty, fill
 * it with the host minus its first label (a two-label host or localhost is
 * used as is; an IP address gets no guess). `hint` says what was done.
 */
export function suggestFromOrigin(origin: string, rpId: string, origins: string[]): Suggestion {
  const list = origins.includes(origin) ? origins.slice() : addOrigin(origins.filter((o) => o.trim() !== ""), origin);
  let host = "";
  try {
    host = new URL(origin).hostname.toLowerCase();
  } catch {
    host = "";
  }
  let hint = origins.includes(origin) ? origin + " was already in the list." : "Added " + origin + ".";
  let id = rpId;
  if (rpId.trim() === "") {
    const isIP = /^[0-9.]+$/.test(host) || host.includes(":");
    if (host && !isIP) {
      const labels = host.split(".");
      id = labels.length >= 3 ? labels.slice(1).join(".") : host;
      hint += " Set the Relying Party ID to " + id + ", the parent domain of this page.";
    } else {
      hint += " Could not guess a Relying Party ID from this address; passkeys need a domain name, not an IP.";
    }
  }
  return { origins: list, rpId: id, hint };
}

/** Advisory mirror of the server's checks; "" means nothing wrong found. */
export function validatePasskeyForm(rpIdRaw: string, originsRaw: string[]): string {
  const id = rpIdRaw.trim().toLowerCase();
  const origins = normalizeOrigins(originsRaw);
  if (id === "" && origins.length === 0) return "";
  if (id === "") return "Relying Party ID is required when allowed origins are set";
  if (origins.length > MAX_ORIGINS) return "too many allowed origins: at most " + MAX_ORIGINS;
  if (/[\s:/?#@\\]/.test(id)) return "Relying Party ID " + id + " must be a bare host name: no scheme, port, path or spaces";
  if (!id.includes(".") && id !== "localhost") return "Relying Party ID " + id + " must contain at least one dot (for example example.com)";
  for (const o of origins) {
    let u: URL;
    try {
      u = new URL(o);
    } catch {
      return o + " is not a valid origin: use a URL like https://host.example.com";
    }
    const host = u.hostname.toLowerCase();
    if (host === "") return o + " has no host";
    if (u.protocol !== "https:" && !(u.protocol === "http:" && host === "localhost")) {
      return o + " must use https (http is only allowed for http://localhost)";
    }
    if ((u.pathname !== "/" && u.pathname !== "") || u.search !== "" || u.hash !== "") {
      return o + " must be an origin only: no path, query or fragment";
    }
    if (host !== id && !host.endsWith("." + id)) {
      return host + " is not under " + id + ": an origin's host must be the RP ID or a subdomain of it";
    }
  }
  return "";
}
