#!/usr/bin/env node
// The import-cycle gate — UI4. A DFS over the relative imports of every
// module in CYCLE_CHECKED (<module>/js/**/*.ts); any cycle fails the gate.
//
// Usage: node scripts/gates/import-cycles.mjs

import fs from "node:fs";
import path from "node:path";

process.chdir(path.resolve(import.meta.dirname, "..", ".."));

const CYCLE_CHECKED = ["agentflow"];

const IMPORT = /(?:import|export)\s[^'"]*?from\s+['"](\.[^'"]+)['"]|import\s+['"](\.[^'"]+)['"]/g;

function* walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) yield* walk(p);
    else if (e.name.endsWith(".ts")) yield p;
  }
}

function resolve(from, spec) {
  const base = path.join(path.dirname(from), spec.replace(/\.js$/, ""));
  for (const c of [`${base}.ts`, path.join(base, "index.ts")]) if (fs.existsSync(c)) return c;
  return null;
}

let total = 0;
for (const mod of CYCLE_CHECKED) {
  const dir = path.join("web", mod, "js");
  const graph = new Map();
  for (const file of walk(dir)) {
    const src = fs.readFileSync(file, "utf8");
    const deps = [];
    for (const m of src.matchAll(IMPORT)) {
      const r = resolve(file, m[1] ?? m[2]);
      if (r) deps.push(r);
    }
    graph.set(file, deps);
  }
  if (graph.size === 0) {
    process.stderr.write(`import-cycles: FAIL no .ts files under ${dir}\n`);
    process.exit(1);
  }
  total += graph.size;

  const state = new Map();
  const stack = [];
  const visit = (n) => {
    if (state.get(n) === 2) return null;
    if (state.get(n) === 1) return [...stack.slice(stack.indexOf(n)), n];
    state.set(n, 1);
    stack.push(n);
    for (const d of graph.get(n) ?? []) {
      const c = visit(d);
      if (c) return c;
    }
    stack.pop();
    state.set(n, 2);
    return null;
  };
  for (const n of graph.keys()) {
    const c = visit(n);
    if (c) {
      process.stderr.write(`import-cycles: FAIL ${c.join(" -> ")}\n`);
      process.exit(1);
    }
  }
}
process.stdout.write(`import-cycles: PASS — ${total} file(s) in ${CYCLE_CHECKED.join(", ")} checked\n`);
