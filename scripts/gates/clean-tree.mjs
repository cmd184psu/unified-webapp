#!/usr/bin/env node
// The clean-tree gate — AX.1, precondition P2.
//
// Runs `git status --porcelain --untracked-files=no -- <paths>` and exits
// non-zero if the output is non-empty. It is PER-COMMIT and PATH-SCOPED to
// that commit's paths, not an assertion that the whole tree is clean.
//
// UNTRACKED FILES ARE IGNORED BY DESIGN: fonts-staging/ and the verification
// screenshots are permanently untracked, so a `??` line must never be a gate
// failure (P2). --untracked-files=no is what makes that structural.
//
// Flags:
//   --exclude <path>  drop <path> from the status report. This is the C1/C1b
//                     byte-identity exemption's clean-tree half: until C2
//                     lands the digest, package.json:5-6 embeds $(date -u …),
//                     so any rebuild re-stamps web/taskmaster/js/bundle.js
//                     unconditionally (§4). Repeatable.
//
// Why this is a file and not a recipe (G11): make expands `$(git status …)`
// inside a recipe as a make variable, which is empty, so `[ -z "$(git …)" ]`
// reaches the shell as `[ -z "" ]` and passes whatever the tree contains.
//
// Usage: node scripts/gates/clean-tree.mjs <path>… [--exclude <path>]…

import { spawnSync } from "node:child_process";
import path from "node:path";

process.chdir(path.resolve(import.meta.dirname, "..", ".."));

function fail(message) {
  process.stderr.write(`clean-tree: FAIL ${message}\n`);
  process.exit(1);
}

const paths = [];
const excluded = [];
const argv = process.argv.slice(2);
for (let i = 0; i < argv.length; i++) {
  if (argv[i] === "--exclude") {
    const value = argv[++i];
    if (!value) fail("--exclude requires a path");
    excluded.push(value.replace(/\/+$/, ""));
  } else if (argv[i].startsWith("--")) {
    fail(`unknown argument ${argv[i]}`);
  } else {
    paths.push(argv[i]);
  }
}

// Never hand git an empty pathspec: `git status --porcelain --` with no paths
// reports the whole tree, which is a different assertion from the one AX.1
// makes. An empty argv is a usage error, not a pass.
if (paths.length === 0) fail("no paths given — usage: clean-tree.mjs <path>… [--exclude <path>]…");

const status = spawnSync("git", ["status", "--porcelain", "--untracked-files=no", "--", ...paths], {
  encoding: "utf8",
});
if (status.error) fail(`could not run git status: ${status.error.message}`);
if (status.status !== 0) {
  process.stderr.write(status.stderr || "");
  fail(`git status exited ${status.status}`);
}

// Porcelain v1 lines are `XY <path>`, or `XY <old> -> <new>` for a rename;
// the reported path is the one after the arrow.
const dirty = status.stdout
  .split("\n")
  .filter((line) => line.trim() !== "")
  .map((line) => ({ line, file: line.slice(3).split(" -> ").pop().replace(/^"|"$/g, "") }))
  .filter(({ file }) => !excluded.some((ex) => file === ex || file.startsWith(`${ex}/`)));

if (dirty.length) {
  process.stderr.write(dirty.map(({ line }) => `clean-tree: FAIL ${line}\n`).join(""));
  fail(`${dirty.length} tracked modification(s) under: ${paths.join(" ")}`);
}

process.stdout.write(
  `clean-tree: PASS — no tracked modifications under: ${paths.join(" ")}` +
    `${excluded.length ? ` (excluded: ${excluded.join(", ")})` : ""}\n`,
);
