#!/usr/bin/env node
// The token-overlap gate — A9.5.
//
// Computes the intersection of the custom-property NAMES declared in
// web/shared/css/*.css and in web/taskmaster/style.css, and passes iff that
// intersection is exactly {--font-mono}.
//
// Both sheets declare their properties on :root at specificity (0,1,0), so
// source order decides every overlap. shared.css is linked first, therefore
// taskmaster's --font-mono wins at all five of its consumers — analysed in
// Step 7 and harmless. If the set ever grows, this gate fails and the new
// overlap must be analysed before landing.
//
// The mechanism is `comm -12` over two sorted -u name lists; it lives in a
// file rather than a make recipe because process substitution is not portable
// inside a recipe and because a gate's exit code must be its own (G11).
//
// Usage: node scripts/gates/token-overlap.mjs

import fs from "node:fs";
import path from "node:path";

process.chdir(path.resolve(import.meta.dirname, "..", ".."));

const SHARED_CSS_DIR = "web/shared/css";
const TASKMASTER_CSS = "web/taskmaster/style.css";
const EXPECTED = ["--font-mono"];

function fail(message) {
  process.stderr.write(`token-overlap: FAIL ${message}\n`);
  process.exit(1);
}

// Declared names only — `--x: value`, not `var(--x)` references. Comments are
// stripped first so a commented-out declaration is not counted.
function declaredNames(text) {
  const names = new Set();
  const source = text.replace(/\/\*[\s\S]*?\*\//g, " ");
  for (const m of source.matchAll(/(^|[;{]\s*)(--[a-zA-Z0-9_-]+)\s*:/g)) names.add(m[2]);
  return names;
}

if (!fs.existsSync(SHARED_CSS_DIR) || !fs.statSync(SHARED_CSS_DIR).isDirectory()) {
  fail(`${SHARED_CSS_DIR}/ does not exist — the shared token vocabulary cannot be read`);
}
if (!fs.existsSync(TASKMASTER_CSS)) fail(`${TASKMASTER_CSS} does not exist`);

const sharedFiles = fs.readdirSync(SHARED_CSS_DIR).filter((f) => f.endsWith(".css")).sort();
if (sharedFiles.length === 0) fail(`${SHARED_CSS_DIR}/ contains no .css file`);

const shared = new Set();
for (const name of sharedFiles) {
  for (const n of declaredNames(fs.readFileSync(path.join(SHARED_CSS_DIR, name), "utf8"))) shared.add(n);
}
const taskmaster = declaredNames(fs.readFileSync(TASKMASTER_CSS, "utf8"));

const intersection = [...shared].filter((n) => taskmaster.has(n)).sort();
const unexpected = intersection.filter((n) => !EXPECTED.includes(n));
const absent = EXPECTED.filter((n) => !intersection.includes(n));

process.stdout.write(
  `token-overlap: ${shared.size} shared name(s) vs ${taskmaster.size} taskmaster name(s); ` +
    `intersection: ${intersection.length ? intersection.join(", ") : "(empty)"}\n`,
);

if (unexpected.length) fail(`unanalysed overlap(s): ${unexpected.join(", ")}`);
if (absent.length) {
  fail(`expected overlap(s) missing — the analysis in Step 7 no longer holds: ${absent.join(", ")}`);
}

process.stdout.write(`token-overlap: PASS — intersection is exactly ${EXPECTED.join(", ")}\n`);
