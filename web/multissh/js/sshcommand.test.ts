// FR-H5 regression: the copy-ssh-command string must never carry a credential.
//
// Run with `npm run test:web`. There is no browser here -- the file is bundled
// for node and throws on failure, so a non-zero exit is the whole report.

import { buildSSHCommand } from "./hosts";
import type { HostConfig } from "./types";

const SECRET = "hunter2-do-not-leak";

function host(over: Partial<HostConfig>): HostConfig {
  return {
    ip: "10.0.0.5",
    port: 22,
    user: "ops",
    key: "",
    remoteDir: "/tmp",
    authMethod: "key",
    password: "",
    ...over,
  };
}

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

const keyCmd = buildSSHCommand(host({ key: "id_ed25519", port: 2222 }));
check(
  "key host keeps -i",
  keyCmd === "ssh -i ~/.ssh/id_ed25519 -p 2222 ops@10.0.0.5",
  `got ${keyCmd}`,
);

// A host switched to password auth may still hold the key name it had before;
// the command must reflect the auth method, not whatever is left in the field.
const pwCmd = buildSSHCommand(
  host({ authMethod: "password", key: "id_ed25519", password: SECRET }),
);
check("password host omits -i", !pwCmd.includes("-i"), `got ${pwCmd}`);
check("password host omits the key name", !pwCmd.includes("id_ed25519"), `got ${pwCmd}`);
check("password host omits the secret", !pwCmd.includes(SECRET), `got ${pwCmd}`);
check("password host omits sshpass", !pwCmd.includes("sshpass"), `got ${pwCmd}`);
check("password host is a plain ssh line", pwCmd === "ssh ops@10.0.0.5", `got ${pwCmd}`);
