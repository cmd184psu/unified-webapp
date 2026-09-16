#!/usr/bin/env node
// The artifact gate — A7.2, A7.3, A7.7, AX.3, AX.4.
//
// One process does all four things a make recipe cannot do safely (G11):
//
//   1. runs scripts/list-artifacts.mjs as a CHILD PROCESS and propagates a
//      non-zero exit, then compares the printed line count against
//      EXPECTED_ARTIFACT_COUNT imported from scripts/descriptors.mjs. It is
//      the generator's own output that WEB_ARTIFACTS consumes, so it is the
//      generator's own output that must be counted (A7.7). A crashed
//      generator is a failure here, not an empty list.
//   2. runs `git ls-files --error-unmatch -- <paths>` with an explicit,
//      NON-EMPTY argv: zero-argument `git ls-files --error-unmatch` lists the
//      whole repo and exits 0, a silent pass (ADR-001 consequences).
//   3. optionally rebuilds (see --build below).
//   4. runs `git diff --exit-code -- <paths>` and reports differing paths.
//
// Flags:
//   --allow <path>   exempt <path> from the byte-identity assertion only. It
//                    is still required to exist in the generated list and to
//                    be tracked. This is the C1/C1b exemption's mechanism:
//                    until C2 lands the digest, package.json:5-6 embeds
//                    $(date -u …), so any rebuild re-stamps
//                    web/taskmaster/js/bundle.js unconditionally (§4).
//                    Repeatable.
//   --build          run `npm run build` before the byte-identity comparison
//                    and fail on a non-zero exit (Step 3's third action).
//                    OFF by default: at the C1/C1b boundary the gate compares
//                    the COMMITTED artifacts, `npm run build` still runs the
//                    old package.json chain, and a rebuild there would only
//                    re-stamp the one artifact --allow already exempts. The
//                    boundary that wants a fresh build passes --build.
//
// Usage: node scripts/gates/artifacts.mjs [--allow <path>]… [--build]

import { spawnSync } from "node:child_process";
import path from "node:path";
import { EXPECTED_ARTIFACT_COUNT } from "../descriptors.mjs";

// Every path below is repo-root-relative, so the gate runs from the repo root
// whatever the caller's cwd is. This keeps git's pathspecs and the paths in
// the failure output identical to the ones in descriptors.mjs.
process.chdir(path.resolve(import.meta.dirname, "..", ".."));

const GENERATOR = "scripts/list-artifacts.mjs";

function fail(message) {
  process.stderr.write(`artifacts: FAIL ${message}\n`);
  process.exit(1);
}

function note(message) {
  process.stdout.write(`artifacts: ${message}\n`);
}

// --- argv -------------------------------------------------------------------

const allow = [];
let build = false;
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--allow") {
    const value = argv[++i];
    if (!value) fail("--allow requires a path");
    allow.push(value);
  } else if (argv[i] === "--build") {
    build = true;
  } else {
    fail(`unknown argument ${argv[i]}`);
  }
}

// --- 1. run the generator as a child process and count its output -----------

const generated = spawnSync(process.execPath, [GENERATOR], { encoding: "utf8" });
if (generated.error) fail(`could not run ${GENERATOR}: ${generated.error.message}`);
if (generated.status !== 0) {
  process.stderr.write(generated.stderr || "");
  fail(`${GENERATOR} exited ${generated.status}`);
}

const artifacts = generated.stdout.split("\n").map((l) => l.trim()).filter((l) => l !== "");
if (artifacts.length !== EXPECTED_ARTIFACT_COUNT) {
  fail(`${GENERATOR} printed ${artifacts.length} path(s), EXPECTED_ARTIFACT_COUNT is ${EXPECTED_ARTIFACT_COUNT}`);
}
note(`${artifacts.length} artifact(s), matching EXPECTED_ARTIFACT_COUNT`);

// The guard ADR-001's consequences require: never hand git an empty path list.
if (artifacts.length === 0) fail("empty artifact list — refusing to run git with zero paths");

for (const path of allow) {
  if (!artifacts.includes(path)) fail(`--allow ${path} is not in the generated artifact list`);
}

// --- 2. every artifact is tracked by git ------------------------------------

const tracked = spawnSync("git", ["ls-files", "--error-unmatch", "--", ...artifacts], { encoding: "utf8" });
if (tracked.error) fail(`could not run git ls-files: ${tracked.error.message}`);
if (tracked.status !== 0) {
  process.stderr.write(tracked.stderr || "");
  fail("at least one artifact is not tracked by git");
}
note(`all ${artifacts.length} artifact(s) tracked`);

// --- 3. optional rebuild ----------------------------------------------------

if (build) {
  const built = spawnSync("npm", ["run", "build"], { stdio: "inherit" });
  if (built.error) fail(`could not run npm run build: ${built.error.message}`);
  if (built.status !== 0) fail(`npm run build exited ${built.status}`);
}

// --- 4. byte-identity -------------------------------------------------------

const compared = artifacts.filter((p) => !allow.includes(p));
if (compared.length === 0) fail("every artifact is exempted from byte-identity — nothing left to assert");

const diff = spawnSync("git", ["diff", "--exit-code", "--name-only", "--", ...compared], { encoding: "utf8" });
if (diff.error) fail(`could not run git diff: ${diff.error.message}`);
if (diff.status !== 0) {
  const differing = diff.stdout.split("\n").filter((l) => l.trim() !== "");
  fail(`byte difference in ${differing.length} artifact(s):\n  ${differing.join("\n  ")}`);
}
note(`byte-identical: ${compared.length} artifact(s)${allow.length ? `, exempted: ${allow.join(", ")}` : ""}`);

process.stdout.write("artifacts: PASS\n");
