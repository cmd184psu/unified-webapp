// File and directory picker tests: the byte formatter the pickers' meta lines
// use, and the barrel export shape. The tree itself (filetree.ts) is the
// shared widget both pickers wrap; there is no browser here, so a thrown
// error is the whole report.

import { fmtBytes } from "./filepicker";
import * as barrel from "./index";

function eq(actual: unknown, expected: unknown, what: string): void {
  if (actual !== expected) {
    throw new Error(`FAIL ${what}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}`);
  }
}

eq(fmtBytes(0), "0 B", "zero");
eq(fmtBytes(1023), "1023 B", "just under a kilobyte");
eq(fmtBytes(1024), "1.0 KB", "a kilobyte");
eq(fmtBytes(1536), "1.5 KB", "one and a half kilobytes");
eq(fmtBytes(1_048_575), "1024.0 KB", "just under a megabyte");
eq(fmtBytes(1_048_576), "1.00 MB", "a megabyte");
eq(fmtBytes(5_242_880), "5.00 MB", "five megabytes");

const b = barrel as unknown as Record<string, unknown>;
for (const name of ["openFilePicker", "openDirPicker", "fmtBytes"]) {
  eq(typeof b[name], "function", `barrel exports ${name}`);
}
