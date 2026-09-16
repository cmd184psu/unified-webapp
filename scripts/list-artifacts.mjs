#!/usr/bin/env node
// Prints one build artifact path per line, derived from scripts/descriptors.mjs.
//
// This is the generator the gates consume: scripts/gates/artifacts.mjs runs it
// as a child process and counts its output, so a crash here is a non-zero exit
// there rather than an empty list (A7.2, A7.7). There is no hand-maintained
// copy of this list anywhere.
//
// The derivation itself lives in scripts/artifact-paths.mjs and is shared with
// scripts/gates/bundle-shape.mjs, so "which files does this descriptor emit?"
// has exactly one definition (B9.4). Its rules, for reference:
//   mode "transpile" — `out` is esbuild's outdir; each entry emits its own
//                      basename under a .js extension
//   mode "bundle"    — `out` is esbuild's outfile; `emitsCss: true` adds the
//                      sibling stylesheet esbuild writes beside it
//
// Usage: node scripts/list-artifacts.mjs

import path from "node:path";
import { descriptors } from "./descriptors.mjs";
import { artifactPaths } from "./artifact-paths.mjs";

function fail(message) {
  process.stderr.write(`list-artifacts: ${message}\n`);
  process.exit(1);
}

const artifacts = [];

for (const d of descriptors) {
  // Driver rule 3: no absolute path on any descriptor path field. Asserted
  // here as well as in the driver, so the property is gated from C1 onward.
  for (const p of [...d.entry, d.out]) {
    if (path.isAbsolute(p)) fail(`descriptor "${d.name}": absolute path ${p}`);
  }

  try {
    artifacts.push(...artifactPaths(d));
  } catch (e) {
    fail(e.message.replace(/^artifact-paths: /, ""));
  }
}

process.stdout.write(artifacts.join("\n") + "\n");
