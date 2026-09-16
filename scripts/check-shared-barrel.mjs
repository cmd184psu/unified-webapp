#!/usr/bin/env node
// check-shared-barrel.mjs — the barrel gate (A5.2, PLAN-ui-unification-phase1.md
// §3 Step 5).
//
// Asserts that web/shared/ts/index.ts's exported-name set EQUALS the
// allowlist below: 8 named values + 7 types (phase2 §5 Step 2.4 added
// showToast, ToastTone and ToastHandle, and §5 Step 3.3 added ThemeManager and
// ThemeManagerOptions, each in the same commit as the barrel line that exports
// them — BX.2. ThemeManager's reresolve() is a METHOD, not an export, so it
// moves neither count). Missing and extra are both
// failures, reported by name — a symbol added to modal.ts or theme.ts and
// forgotten in the barrel fails here rather than producing an `undefined`
// import in a browser, and a symbol added to the barrel without a decision
// fails here too.
//
// `export *` is banned outright: a star export makes the public surface
// implicit, which is exactly what an allowlist exists to prevent.
//
// Usage: node scripts/check-shared-barrel.mjs

import fs from "node:fs";
import path from "node:path";

// Run from the repo root whatever the caller's cwd is.
process.chdir(path.resolve(import.meta.dirname, ".."));

const BARREL = "web/shared/ts/index.ts";

const ALLOWED_VALUES = ["openModal", "confirmDialog", "alertDialog", "promptDialog", "THEMES", "setTheme", "ThemeManager", "showToast"];
const ALLOWED_TYPES = ["ModalOptions", "ModalHandle", "DialogOptions", "PromptOptions", "ThemeManagerOptions", "ToastTone", "ToastHandle"];

function fail(message) {
  process.stderr.write(`check-shared-barrel: FAIL ${message}\n`);
  process.exit(1);
}

if (!fs.existsSync(BARREL)) fail(`${BARREL} does not exist`);

const raw = fs.readFileSync(BARREL, "utf8");

// Strip comments so a name mentioned in prose can never be mistaken for an
// export. Newlines are preserved (not that this script reports line numbers
// today, but it keeps the invariant the same way check-shared-css.mjs does).
const text = raw
  .replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "))
  .replace(/\/\/[^\n]*/g, "");

// Star exports are banned outright, before anything else is parsed.
if (/export\s*\*/.test(text)) {
  fail(`"export *" found in ${BARREL} — the barrel must name every export explicitly`);
}

function splitNames(body) {
  return body
    .split(",")
    .map((s) => s.trim())
    .filter((s) => s !== "")
    .map((s) => {
      // `Local as Exported` — the exported name is the one after `as`.
      const asMatch = s.match(/^\S+\s+as\s+(\S+)$/);
      return asMatch ? asMatch[1] : s;
    });
}

const values = new Set();
const types = new Set();

// `export type { A, B, ... } from "..."` must be matched BEFORE the generic
// `export { ... }` pattern below, which would otherwise also match its braces.
const typeExportRe = /export\s+type\s*\{([^}]*)\}\s*(?:from\s*["'][^"']*["'])?\s*;?/g;
let m;
while ((m = typeExportRe.exec(text))) {
  for (const name of splitNames(m[1])) types.add(name);
}
const withoutTypeExports = text.replace(typeExportRe, "");

// `export { A, B, ... } from "..."` and the bare `export { A, B };` form.
const namedExportRe = /export\s*\{([^}]*)\}\s*(?:from\s*["'][^"']*["'])?\s*;?/g;
while ((m = namedExportRe.exec(withoutTypeExports))) {
  for (const name of splitNames(m[1])) values.add(name);
}
const withoutNamedExports = withoutTypeExports.replace(namedExportRe, "");

// Direct declarations: `export const X`, `export function X`, `export class X`,
// `export let X`, `export var X`, `export async function X` — this is what
// catches an unreviewed symbol added straight to the barrel (AX.6).
const declRe = /export\s+(?:async\s+function|function|class|const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)/g;
while ((m = declRe.exec(withoutNamedExports))) {
  values.add(m[1]);
}

// `export interface X` / `export type X = ...` direct declarations.
const typeDeclRe = /export\s+(?:type|interface)\s+([A-Za-z_$][A-Za-z0-9_$]*)/g;
while ((m = typeDeclRe.exec(withoutNamedExports))) {
  types.add(m[1]);
}

function diff(actual, expected) {
  const a = new Set(actual);
  const e = new Set(expected);
  return {
    missing: [...e].filter((k) => !a.has(k)),
    extra: [...a].filter((k) => !e.has(k)),
  };
}

const valueDiff = diff(values, ALLOWED_VALUES);
const typeDiff = diff(types, ALLOWED_TYPES);

const failures = [];
if (valueDiff.missing.length) failures.push(`missing named value export(s): ${valueDiff.missing.join(", ")}`);
if (valueDiff.extra.length) failures.push(`unexpected named value export(s): ${valueDiff.extra.join(", ")}`);
if (typeDiff.missing.length) failures.push(`missing type export(s): ${typeDiff.missing.join(", ")}`);
if (typeDiff.extra.length) failures.push(`unexpected type export(s): ${typeDiff.extra.join(", ")}`);

if (failures.length) {
  for (const f of failures) process.stderr.write(`check-shared-barrel: FAIL ${f}\n`);
  process.exit(1);
}

process.stdout.write(
  `check-shared-barrel: PASS — ${values.size} named value export(s), ${types.size} type export(s)\n`,
);
