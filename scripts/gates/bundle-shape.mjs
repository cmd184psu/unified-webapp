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
// The set is NOT empty and has not been since Phase 1: `sampler`
// (scripts/descriptors.mjs:77) and `taskmaster` (:123) both declare the flag.
// The zero-input branch below is therefore unreachable on today's descriptor
// set, and --expect-nonempty with it. Both are kept — neither is wrong — but
// neither can fail while any one consumer remains, which is why --require
// exists: it asserts that each NAMED descriptor is in the inspected set, and
// fails by name the moment one leaves or was never added. The Makefile recipe
// passes the list the plan relies on, per G16.
//
// Usage: node scripts/gates/bundle-shape.mjs [--expect-nonempty]
//                                            [--require=<name>,…]

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
const required = [];
for (const arg of argv) {
  if (arg === "--expect-nonempty") expectNonEmpty = true;
  else if (arg.startsWith("--require=")) {
    for (const name of arg.slice("--require=".length).split(",")) {
      const trimmed = name.trim();
      if (trimmed !== "") required.push(trimmed);
    }
  } else {
    process.stderr.write(`bundle-shape: FAIL unknown argument ${arg}\n`);
    process.exit(1);
  }
}

const inputs = descriptors.filter((d) => d.sharedConsumer === true);

// --require, checked before the zero-input branch below so that an empty set
// cannot pass vacuously while names were demanded. Per-descriptor and by
// name: "these n are in the set" fails the moment one leaves, where "the set
// is non-empty" cannot fail while any one member remains.
{
  const missing = [];
  for (const name of required) {
    const d = descriptors.find((x) => x.name === name);
    if (!d) missing.push(`${name}: no descriptor by that name (--require)`);
    else if (d.sharedConsumer !== true) {
      missing.push(`${name}: descriptor does not declare sharedConsumer: true (--require)`);
    }
  }
  if (missing.length) {
    for (const f of missing) process.stderr.write(`bundle-shape: FAIL ${f}\n`);
    process.exit(1);
  }
}

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
