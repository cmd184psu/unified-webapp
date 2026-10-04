// Pure logic for the LDAP "Required groups" field. Run with `npm run test:web`.

/**
 * Turns what was typed or pasted into the group names the server matches on
 * (a group's cn, for example "household"). Entries are comma-separated. A full
 * DN such as "cn=household,ou=groups,dc=example,dc=com" is reduced to its cn
 * instead of being split into pieces that can never match: any other
 * "attribute=value" piece of a DN (ou, dc, ...) is dropped.
 */
export function parseRequiredGroups(raw: string): string[] {
  const out: string[] = [];
  for (const piece of raw.split(",")) {
    const t = piece.trim();
    if (!t) continue;
    const eq = t.indexOf("=");
    let name = t;
    if (eq >= 0) {
      if (t.slice(0, eq).trim().toLowerCase() !== "cn") continue;
      name = t.slice(eq + 1).trim();
    }
    if (name && !out.includes(name)) out.push(name);
  }
  return out;
}
