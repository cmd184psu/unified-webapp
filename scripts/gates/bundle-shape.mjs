#!/usr/bin/env node
// The bundle-shape gate — A9.1, A9.2, A9.3 (ADR-001).
//
// Input set: exactly the `out` artifacts of descriptors declaring
// `sharedConsumer: true`. That is a descriptor field, not a guess — the same
// field selects the `@shared` onResolve plugin and triggers the driver's
// format:"esm" assertion, so the gate, the plugin and the assertion cannot
// range over different sets (§5 A9 preamble).
//
// Why both sides are required: with format:"iife" esbuild does not error on
// an ESM-only construct — it silently downgrades, emits `__require(…)` and a
// `Dynamic require of` shim, and exits 0 with zero warnings. The positive
// check alone cannot see that; the negative check alone passes an empty file.
//
//   A9.1  a top-level `import … from "/shared/dist/shared.mjs"` is present
//   A9.2  neither `__require(` nor `Dynamic require of` appears
//   A9.3  taskmaster's bundle contains `var FRONTEND_BUILD_TIME` exactly once
//
// The set is EMPTY until C5 (sampler at C5, taskmaster at C6; the `shared`
// descriptor is the barrel, not a consumer of it). On an empty set this gate
// passes VACUOUSLY and says so — recorded, never counted as evidence. It is
// not a stub: the code path that passes on zero inputs is the one that fails
// on a bad input at C6. Pass --expect-nonempty at a boundary where the set
// must not be empty, and an empty set becomes a failure.
//
// Usage: node scripts/gates/bundle-shape.mjs [--expect-nonempty]

import fs from "node:fs";
import path from "node:path";
import { descriptors } from "../descriptors.mjs";

process.chdir(path.resolve(import.meta.dirname, "..", ".."));

const BARREL = "/shared/dist/shared.mjs";
// Top-level: the statement starts at column 0, so it is not nested inside a
// function body or an iife wrapper. Matches both the named form
// (`import { x } from "…"`) and the bare side-effect form (`import "…"`).
const TOP_LEVEL_IMPORT = /^import\b[^\n]*["']\/shared\/dist\/shared\.mjs["']/m;

const failures = [];

const argv = process.argv.slice(2);
let expectNonEmpty = false;
for (const arg of argv) {
  if (arg === "--expect-nonempty") expectNonEmpty = true;
  else {
    process.stderr.write(`bundle-shape: FAIL unknown argument ${arg}\n`);
    process.exit(1);
  }
}

const inputs = descriptors.filter((d) => d.sharedConsumer === true);

if (inputs.length === 0) {
  if (expectNonEmpty) {
    process.stderr.write(
      "bundle-shape: FAIL no descriptor declares sharedConsumer: true, but --expect-nonempty was given\n",
    );
    process.exit(1);
  }
  process.stdout.write(
    "bundle-shape: PASS VACUOUSLY — no descriptor declares sharedConsumer: true, " +
      "so the input set is empty and nothing was inspected\n",
  );
  process.exit(0);
}

for (const d of inputs) {
  if (!fs.existsSync(d.out)) {
    failures.push(`${d.name}: ${d.out} does not exist`);
    continue;
  }
  const text = fs.readFileSync(d.out, "utf8");

  // A9.1 — positive.
  if (!TOP_LEVEL_IMPORT.test(text)) {
    failures.push(`${d.name}: ${d.out} has no top-level import from "${BARREL}" (A9.1)`);
  }

  // A9.2 — negative, both markers.
  for (const marker of ["__require(", "Dynamic require of"]) {
    if (text.includes(marker)) {
      failures.push(`${d.name}: ${d.out} contains "${marker}" — format downgraded to iife (A9.2)`);
    }
  }

  // A9.3 — the define was applied, and applied once.
  if (d.name === "taskmaster") {
    const n = (text.match(/var FRONTEND_BUILD_TIME\b/g) || []).length;
    if (n !== 1) {
      failures.push(`${d.name}: ${d.out} contains "var FRONTEND_BUILD_TIME" ${n} time(s), expected exactly 1 (A9.3)`);
    }
  }
}

if (failures.length) {
  for (const f of failures) process.stderr.write(`bundle-shape: FAIL ${f}\n`);
  process.exit(1);
}

process.stdout.write(`bundle-shape: PASS — ${inputs.length} sharedConsumer bundle(s) inspected\n`);
