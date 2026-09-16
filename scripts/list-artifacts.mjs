#!/usr/bin/env node
// Prints one build artifact path per line, derived from scripts/descriptors.mjs.
//
// This is the generator the gates consume: scripts/gates/artifacts.mjs runs it
// as a child process and counts its output, so a crash here is a non-zero exit
// there rather than an empty list (A7.2, A7.7). There is no hand-maintained
// copy of this list anywhere.
//
// Derivation rules, mechanical:
//   mode "transpile" — `out` is esbuild's outdir; each entry emits its own
//                      basename with the .ts/.tsx extension replaced by .js
//   mode "bundle"    — `out` is esbuild's outfile; `emitsCss: true` adds the
//                      sibling stylesheet esbuild writes beside it
//
// Usage: node scripts/list-artifacts.mjs

import path from "node:path";
import { descriptors } from "./descriptors.mjs";

function fail(message) {
  process.stderr.write(`list-artifacts: ${message}\n`);
  process.exit(1);
}

function jsName(entry) {
  return path.basename(entry).replace(/\.tsx?$/, ".js");
}

const artifacts = [];

for (const d of descriptors) {
  // Driver rule 3: no absolute path on any descriptor path field. Asserted
  // here as well as in the driver, so the property is gated from C1 onward.
  for (const p of [...d.entry, d.out]) {
    if (path.isAbsolute(p)) fail(`descriptor "${d.name}": absolute path ${p}`);
  }

  if (d.mode === "transpile") {
    for (const entry of d.entry) artifacts.push(path.posix.join(d.out, jsName(entry)));
  } else if (d.mode === "bundle") {
    artifacts.push(d.out);
    if (d.emitsCss) artifacts.push(d.out.replace(/\.js$/, ".css"));
  } else {
    fail(`descriptor "${d.name}": unknown mode ${JSON.stringify(d.mode)}`);
  }
}

process.stdout.write(artifacts.join("\n") + "\n");
