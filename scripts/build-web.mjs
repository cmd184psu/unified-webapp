#!/usr/bin/env node
// The build driver — esbuild JS API over scripts/descriptors.mjs's single
// source-of-truth descriptor array (PLAN-ui-unification-phase1.md §3 Step 3).
//
// One build() per descriptor, sequential, in descriptors.mjs's array order.
// Shared descriptors are listed before their consumers there (from C4
// onward), which is what satisfies driver rule 1 ("shared descriptors run
// first") — this file does not need to know which descriptor is which.
//
// Driver rules enforced here (plan :465-481):
//   3.  every descriptor path field is asserted non-absolute before any
//       esbuild call — an absolute path rewrites the relative source-path
//       comments esbuild embeds in bundle output and fails G2 everywhere
//       at once.
//   4.  taskmaster's __TM_BUILD_TIME__ is a content digest (ADR-004),
//       computed in a two-pass build — see computeTaskmasterDigest() below.
//   5.  --dev sets sourcemap:true for every descriptor, and flips
//       process.env.NODE_ENV to "development" for the two React modules.
//   6.  target es2020, platform browser, logLevel warning — mirrored per
//       descriptor from the current chain.
//   7.  logLevel:"warning" on every esbuild call; any warnings are a
//       non-zero exit (this is what would have caught the import.meta
//       downgrade, which esbuild reports as a warning with exit 0).
//   8.  check-shared-css.mjs runs first, before the first esbuild call; a
//       failure aborts the build non-zero.
//   9.  check-shared-barrel.mjs is NOT invoked here — it lands at C4, in the
//       same commit that creates the script.
//   10. @shared is externalized via an onResolve plugin, applied only to
//       descriptors carrying sharedConsumer:true (none yet at C2 — the
//       plugin path is present but inert until C5/C6), which must also
//       declare format:"esm" (A9.1).
//
// Usage: node scripts/build-web.mjs [--dev]

import * as esbuild from "esbuild";
import { spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { descriptors, BUILD_TIME_DIGEST } from "./descriptors.mjs";

// Run from the repo root whatever the caller's cwd is, so every descriptor
// path — passed to esbuild unmodified, per rule 3 — resolves exactly as
// `npm run build` / `make web` resolve it today. This chdir target is not
// an esbuild input/output field, so it is exempt from rule 3's ban on
// path.resolve.
process.chdir(path.resolve(import.meta.dirname, ".."));

function fail(message) {
  process.stderr.write(`build-web: FAIL ${message}\n`);
  process.exit(1);
}

const dev = process.argv.includes("--dev");

// --- rule 8: the shared-CSS linter runs before the first esbuild call ------

const lint = spawnSync(process.execPath, ["scripts/check-shared-css.mjs"], { stdio: "inherit" });
if (lint.error) fail(`could not run check-shared-css.mjs: ${lint.error.message}`);
if (lint.status !== 0) fail(`check-shared-css.mjs exited ${lint.status}`);

// --- rule 3: no descriptor path field may be absolute -----------------------

function assertRelative(descriptor) {
  for (const p of [...descriptor.entry, descriptor.out]) {
    if (path.isAbsolute(p)) {
      fail(
        `descriptor "${descriptor.name}": absolute path ${p} — esbuild embeds this path ` +
          `as a source comment in bundle output; an absolute path rewrites every one and ` +
          `fails G2 everywhere at once`,
      );
    }
  }
}

// --- rule 10: @shared is externalized, never inlined ------------------------

function sharedExternalPlugin() {
  return {
    name: "shared-external",
    setup(build) {
      build.onResolve({ filter: /^@shared(\/.*)?$/ }, () => ({
        path: "/shared/dist/shared.mjs",
        external: true,
      }));
    },
  };
}

// --- ADR-004: taskmaster's build stamp is a content digest ------------------
//
// __TM_BUILD_TIME__ is a 12-character digest over the union of taskmaster's
// esbuild metafile inputs and an explicit EXTRA set. Pass 1 builds with
// metafile:true and a placeholder define (the placeholder never affects the
// import graph, so it cannot affect which files are in the input set) to
// discover that set; pass 2 (the caller, buildDescriptor) rebuilds with the
// real digest substituted for the BUILD_TIME_DIGEST sentinel.

const EXTRA = ["web/taskmaster/index.html", "web/taskmaster/style.css"];

async function computeTaskmasterDigest(descriptor) {
  const pass1 = await esbuild.build({
    entryPoints: descriptor.entry,
    bundle: descriptor.bundle,
    format: descriptor.format,
    target: descriptor.target || "es2020",
    platform: "browser",
    logLevel: "warning",
    define: { __TM_BUILD_TIME__: JSON.stringify("digest-pass-1") },
    metafile: true,
    write: false,
  });
  if (pass1.warnings.length > 0) {
    fail(`taskmaster digest pass: ${pass1.warnings.length} esbuild warning(s)`);
  }

  const inputs = new Set(Object.keys(pass1.metafile.inputs));
  for (const extra of EXTRA) inputs.add(extra);
  const files = [...inputs].sort();

  for (const p of files) {
    if (path.isAbsolute(p)) fail(`taskmaster digest input "${p}" is absolute`);
  }

  const hash = crypto.createHash("sha256");
  for (const file of files) {
    hash.update(file);
    hash.update("\0");
    hash.update(fs.readFileSync(file));
  }
  const digest = hash.digest("hex").slice(0, 12);
  return { digest, files };
}

// --- one build() per descriptor ---------------------------------------------

async function buildDescriptor(descriptor) {
  assertRelative(descriptor);

  const options = {
    entryPoints: descriptor.entry,
    bundle: descriptor.bundle,
    target: descriptor.target || "es2020",
    platform: "browser",
    logLevel: "warning",
  };

  if (descriptor.mode === "transpile") {
    options.outdir = descriptor.out;
  } else if (descriptor.mode === "bundle") {
    options.outfile = descriptor.out;
  } else {
    fail(`descriptor "${descriptor.name}": unknown mode ${JSON.stringify(descriptor.mode)}`);
  }

  if (descriptor.format) options.format = descriptor.format;
  if (descriptor.jsx) options.jsx = descriptor.jsx;
  if (descriptor.loader) options.loader = descriptor.loader;
  if (descriptor.external) options.external = descriptor.external;
  if (descriptor.define) options.define = { ...descriptor.define };

  // rule 5: --dev sets sourcemap:true for every descriptor, and flips
  // NODE_ENV to "development" for the two React modules only.
  if (dev) {
    options.sourcemap = true;
    if (options.define && "process.env.NODE_ENV" in options.define) {
      options.define["process.env.NODE_ENV"] = '"development"';
    }
  }

  // ADR-004's two-pass build, scoped to the one descriptor carrying the
  // digest sentinel.
  if (options.define && options.define.__TM_BUILD_TIME__ === BUILD_TIME_DIGEST) {
    const { digest, files } = await computeTaskmasterDigest(descriptor);
    options.define.__TM_BUILD_TIME__ = JSON.stringify(digest);
    process.stdout.write(
      `build-web: ${descriptor.name} content digest ${digest} over ${files.length} file(s)\n`,
    );
  }

  // rule 10: @shared externalization, sharedConsumer descriptors only.
  if (descriptor.sharedConsumer) {
    if (descriptor.format !== "esm") {
      fail(
        `descriptor "${descriptor.name}": sharedConsumer:true requires format "esm", ` +
          `found ${JSON.stringify(descriptor.format)}`,
      );
    }
    options.plugins = [sharedExternalPlugin()];
  }

  const result = await esbuild.build(options);
  if (result.warnings.length > 0) {
    fail(`descriptor "${descriptor.name}": ${result.warnings.length} esbuild warning(s)`);
  }
  process.stdout.write(`build-web: ${descriptor.name} -> ${descriptor.out}\n`);
}

for (const descriptor of descriptors) {
  await buildDescriptor(descriptor);
}

process.stdout.write(`build-web: ${descriptors.length} descriptor(s) built${dev ? " (dev)" : ""}\n`);
