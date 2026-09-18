#!/usr/bin/env node
// The test driver — the same esbuild JS API driver as build-web.mjs, over a
// small fixed list of test suites (PLAN-ui-unification-phase1.md §3 Step 3,
// driver rule 11).
//
// Two runner modes:
//   "esbuild-cjs"  bundle:true, platform:"node", format:"cjs",
//                  target:"node18", logLevel:"warning" — the exact four
//                  flags test:web uses today — piped to `node`.
//   "node-test"    spawnSync(process.execPath, ["--test", file],
//                  { cwd: repoRoot }) — for web/grocery/app.test.js, which
//                  uses node:test and import.meta.dirname and cannot run
//                  through the esbuild-cjs pipeline.
//
// Warnings are failures here too (rule 7): both drivers set
// logLevel:"warning" and exit non-zero when result.warnings.length > 0.
//
// This is a fixed list, not scripts/descriptors.mjs's array: that module is
// build-artifact data only (§4's descriptors.mjs edit-trail — test suites
// are not build artifacts, so WEB_ARTIFACTS never needs to know about them).
//
// Usage: node scripts/test-web.mjs

import * as esbuild from "esbuild";
import { spawnSync } from "node:child_process";
import path from "node:path";

const repoRoot = path.resolve(import.meta.dirname, "..");
process.chdir(repoRoot);

function fail(message) {
  process.stderr.write(`test-web: FAIL ${message}\n`);
  process.exit(1);
}

// The four suites test:web ran before C4, plus web/grocery/app.test.js
// (driver rule 11), plus web/shared/ts/modal.test.ts, added here at C4
// (A7.13), plus web/shared/ts/toast.test.ts, added at phase2 C2 (§5 Step 2.5),
// plus web/shared/ts/theme.test.ts, added at phase2 C3 (§5 Step 3.4), plus
// web/shared/ts/menu.test.ts, added at phase2 C4 (§5 Step 4.4).
const suites = [
  { name: "sshcommand", entry: "web/multissh/js/sshcommand.test.ts", runner: "esbuild-cjs" },
  { name: "certmachine-status", entry: "web/certmachine/js/status.test.ts", runner: "esbuild-cjs" },
  { name: "certmachine-listmodel", entry: "web/certmachine/js/listmodel.test.ts", runner: "esbuild-cjs" },
  { name: "certmachine-generate", entry: "web/certmachine/js/generate.test.ts", runner: "esbuild-cjs" },
  { name: "shared-modal", entry: "web/shared/ts/modal.test.ts", runner: "esbuild-cjs" },
  { name: "shared-toast", entry: "web/shared/ts/toast.test.ts", runner: "esbuild-cjs" },
  { name: "shared-theme", entry: "web/shared/ts/theme.test.ts", runner: "esbuild-cjs" },
  { name: "shared-menu", entry: "web/shared/ts/menu.test.ts", runner: "esbuild-cjs" },
  { name: "grocery", entry: "web/grocery/js/main.test.ts", runner: "node-test" },
];

async function runEsbuildCjs(suite) {
  if (path.isAbsolute(suite.entry)) fail(`suite "${suite.name}": absolute path ${suite.entry}`);

  const result = await esbuild.build({
    entryPoints: [suite.entry],
    bundle: true,
    platform: "node",
    format: "cjs",
    target: "node18",
    logLevel: "warning",
    write: false,
  });
  if (result.warnings.length > 0) {
    fail(`suite "${suite.name}": ${result.warnings.length} esbuild warning(s)`);
  }

  const code = result.outputFiles[0].text;
  const run = spawnSync(process.execPath, ["-"], {
    input: code,
    stdio: ["pipe", "inherit", "inherit"],
    cwd: repoRoot,
  });
  if (run.error) fail(`suite "${suite.name}": could not run node: ${run.error.message}`);
  if (run.status !== 0) fail(`suite "${suite.name}" failed (exit ${run.status})`);
}

function runNodeTest(suite) {
  if (path.isAbsolute(suite.entry)) fail(`suite "${suite.name}": absolute path ${suite.entry}`);

  const run = spawnSync(process.execPath, ["--test", suite.entry], { cwd: repoRoot, stdio: "inherit" });
  if (run.error) fail(`suite "${suite.name}": could not run node --test: ${run.error.message}`);
  if (run.status !== 0) fail(`suite "${suite.name}" failed (exit ${run.status})`);
}

for (const suite of suites) {
  if (suite.runner === "esbuild-cjs") {
    await runEsbuildCjs(suite);
  } else if (suite.runner === "node-test") {
    runNodeTest(suite);
  } else {
    fail(`suite "${suite.name}": unknown runner ${JSON.stringify(suite.runner)}`);
  }
  process.stdout.write(`test-web: ${suite.name} passed (${suite.runner})\n`);
}

process.stdout.write(`test-web: ${suites.length} suite(s) passed\n`);
