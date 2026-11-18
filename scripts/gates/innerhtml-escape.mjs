#!/usr/bin/env node
// The innerHTML escape gate — GT4 (escape half), UI1.
//
// For every module in ESCAPE_GATED, scans <module>/js/**/*.ts and fails on any
// assignment to innerHTML / outerHTML, and any insertAdjacentHTML call, whose
// value is not either
//   - a string literal (quote or template) with no `${` in it, or
//   - a template literal in which every `${...}` expression is a call to esc(.
// Anything else (a variable, a concatenation, an unescaped interpolation) is
// model- or user-controlled text that could become markup. Build elements with
// h() from dom.ts instead.
//
// Usage: node scripts/gates/innerhtml-escape.mjs

import fs from "node:fs";
import path from "node:path";

process.chdir(path.resolve(import.meta.dirname, "..", ".."));

// Modules held to the gate. Other modules predate it and are not scanned.
const ESCAPE_GATED = ["agentflow"];

function fail(message) {
  process.stderr.write(`innerhtml-escape: FAIL ${message}\n`);
  process.exit(1);
}

function* walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) yield* walk(p);
    else if (e.name.endsWith(".ts")) yield p;
  }
}

// readValue returns the expression text starting at `i` up to the statement
// end: a `;` or newline outside any string, template or bracket.
function readValue(src, i) {
  let depth = 0;
  let j = i;
  while (j < src.length) {
    const c = src[j];
    if (c === "'" || c === '"') {
      j++;
      while (j < src.length && src[j] !== c) j += src[j] === "\\" ? 2 : 1;
    } else if (c === "`") {
      j = skipTemplate(src, j);
      continue;
    } else if ("([{".includes(c)) depth++;
    else if (")]}".includes(c)) {
      if (depth === 0) break;
      depth--;
    } else if (depth === 0 && (c === ";" || c === "\n")) break;
    j++;
  }
  return src.slice(i, j).trim();
}

// skipTemplate returns the index just past the template literal opening at j.
function skipTemplate(src, j) {
  j++;
  while (j < src.length && src[j] !== "`") {
    if (src[j] === "\\") j += 2;
    else if (src[j] === "$" && src[j + 1] === "{") j = skipBraces(src, j + 1);
    else j++;
  }
  return j + 1;
}

function skipBraces(src, j) {
  let depth = 0;
  while (j < src.length) {
    const c = src[j];
    if (c === "`") {
      j = skipTemplate(src, j);
      continue;
    }
    if (c === "{") depth++;
    if (c === "}" && --depth === 0) return j + 1;
    j++;
  }
  return j;
}

// interpolations returns the `${...}` expression texts of a template literal,
// or null when the text is not exactly one template literal.
function interpolations(value) {
  if (!value.startsWith("`") || skipTemplate(value, 0) !== value.length) return null;
  const out = [];
  let j = 1;
  while (j < value.length - 1) {
    if (value[j] === "\\") j += 2;
    else if (value[j] === "$" && value[j + 1] === "{") {
      const end = skipBraces(value, j + 1);
      out.push(value.slice(j + 2, end - 1).trim());
      j = end;
    } else j++;
  }
  return out;
}

function isSafe(value) {
  if (/^(['"])(?:\\.|(?!\1).)*\1$/.test(value)) return true; // plain string literal
  const parts = interpolations(value);
  if (parts === null) return false;
  return parts.every((p) => /^esc\(/.test(p) && skipParens(p));
}

// skipParens: the whole expression is one esc(...) call (not esc(a) + b).
function skipParens(expr) {
  let depth = 0;
  for (let j = 3; j < expr.length; j++) {
    const c = expr[j];
    if (c === "'" || c === '"') {
      j++;
      while (j < expr.length && expr[j] !== c) j += expr[j] === "\\" ? 2 : 1;
    } else if (c === "`") j = skipTemplate(expr, j) - 1;
    else if (c === "(") depth++;
    else if (c === ")" && --depth === 0) return j === expr.length - 1;
  }
  return false;
}

const SINK = /\.(innerHTML|outerHTML)\s*(\+?=)(?!=)|\.insertAdjacentHTML\s*\(\s*(?:'[^']*'|"[^"]*")\s*,/g;

let files = 0;
const problems = [];
for (const mod of ESCAPE_GATED) {
  const dir = path.join("web", mod, "js");
  if (!fs.existsSync(dir)) fail(`${dir} does not exist; refusing to pass vacuously`);
  for (const file of walk(dir)) {
    files++;
    const src = fs.readFileSync(file, "utf8");
    for (const m of src.matchAll(SINK)) {
      const value = readValue(src, m.index + m[0].length);
      if (m[2] === "+=" || !isSafe(value)) {
        const line = src.slice(0, m.index).split("\n").length;
        problems.push(`${file}:${line}: ${m[0].trim()} ${value.slice(0, 60)}`);
      }
    }
  }
}
if (files === 0) fail("no .ts files found; refusing to pass vacuously");
if (problems.length > 0) {
  process.stderr.write("innerhtml-escape: FAIL unescaped HTML sink(s); wrap each ${...} in esc( or build with h():\n");
  for (const p of problems) process.stderr.write(`  ${p}\n`);
  process.exit(1);
}
process.stdout.write(`innerhtml-escape: PASS — ${files} file(s) in ${ESCAPE_GATED.join(", ")} checked\n`);
