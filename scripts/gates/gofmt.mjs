#!/usr/bin/env node
// The gofmt gate — CL3, GT4 (gofmt half).
//
// Runs `gofmt -l` over every Go file git knows about: tracked files
// (`git ls-files '*.go'`) AND new untracked ones that are not gitignored
// (`git ls-files --others --exclude-standard '*.go'`), so a freshly written,
// unformatted file fails the gate before it is ever committed. Exits non-zero
// if gofmt lists any file, or if gofmt/git themselves cannot run.
//
// Why this is a file and not a recipe (G11): see the comment above the
// `web-verify` target in the Makefile. The gate is one process whose exit
// code is the gate. Every child is spawned with spawnSync and no shell.
//
// Usage: node scripts/gates/gofmt.mjs

import { spawnSync } from "node:child_process";
import path from "node:path";

process.chdir(path.resolve(import.meta.dirname, "..", ".."));

function fail(message) {
  process.stderr.write(`gofmt: FAIL ${message}\n`);
  process.exit(1);
}

function run(cmd, args) {
  const r = spawnSync(cmd, args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
  if (r.error) fail(`could not run ${cmd}: ${r.error.message}`);
  if (r.status !== 0) fail(`${cmd} ${args.join(" ")} exited ${r.status}: ${(r.stderr || "").trim()}`);
  return r;
}

const lines = (s) => s.split("\n").filter((l) => l !== "");

// -z is not needed: paths here never contain newlines, and an exotic name
// would only make the gofmt call below fail loudly rather than pass.
const tracked = lines(run("git", ["ls-files", "*.go"]).stdout);
const untracked = lines(run("git", ["ls-files", "--others", "--exclude-standard", "*.go"]).stdout);
const files = [...new Set([...tracked, ...untracked])];
if (files.length === 0) fail("no .go files found — refusing to pass vacuously");

const result = run("gofmt", ["-l", ...files]);
// gofmt -l prints syntax errors on stderr and exits non-zero (caught by run);
// a clean run prints only the names of unformatted files on stdout.
const bad = lines(result.stdout);
if (bad.length > 0) {
  process.stderr.write(`gofmt: FAIL ${bad.length} unformatted file(s) — run gofmt -w:\n`);
  for (const f of bad) process.stderr.write(`  ${f}\n`);
  process.exit(1);
}

process.stdout.write(`gofmt: PASS — ${files.length} file(s) checked\n`);
