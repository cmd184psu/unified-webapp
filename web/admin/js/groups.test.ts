// Run with `npm run test:web`.

import { parseRequiredGroups } from "./groups";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}
const j = (v: unknown): string => JSON.stringify(v);

check("plain names", j(parseRequiredGroups("household, family")) === j(["household", "family"]), j(parseRequiredGroups("household, family")));
check("blank gives none", parseRequiredGroups("  ,  ").length === 0, "blank");
check("a pasted DN keeps its cn", j(parseRequiredGroups("cn=household,ou=groups,dc=cmdhome,dc=net")) === j(["household"]), j(parseRequiredGroups("cn=household,ou=groups,dc=cmdhome,dc=net")));
check("a DN and a plain name together", j(parseRequiredGroups("cn=household,ou=groups,dc=x,dc=y, family")) === j(["household", "family"]), j(parseRequiredGroups("cn=household,ou=groups,dc=x,dc=y, family")));
check("CN is case-insensitive", j(parseRequiredGroups("CN=Admins,OU=g")) === j(["Admins"]), "case");
check("duplicates collapse", j(parseRequiredGroups("a, a, cn=a,ou=g")) === j(["a"]), j(parseRequiredGroups("a, a, cn=a,ou=g")));
