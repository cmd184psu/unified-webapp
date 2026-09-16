#!/usr/bin/env node
// Linter for web/shared/css/ — the 12 clauses of PLAN-ui-unification-phase1.md
// §3 Step 1.7 (1-11, A1.1-A1.11) + phase2 §5 Step 1.3's clause 12 (G9).
//
// It lints each file INDIVIDUALLY and does not resolve @import targets. Two
// structural reasons, both load-bearing: at C1 fonts.css does not exist yet,
// and a linter that followed imports would make the C1/C1b split unlandable.
//
// No CSS parser dependency (G4) — but it parses selectors and declarations
// rather than pattern-matching lines, because a line-oriented grep cannot
// tell `a:hover {` or `.ui-card > button {` apart from a token declaration.
//
// Exits 1 on the FIRST failure, printing `file:line: clause N: message`.
// Invoked by scripts/build-web.mjs before the first esbuild call (C2), by
// `make gates`, and directly in each commit's verification block.
//
// Usage: node scripts/check-shared-css.mjs

import fs from "node:fs";
import path from "node:path";

// Run from the repo root whatever the caller's cwd is, so the paths in the
// `file:line:` output are the repo-relative ones a reader can act on.
process.chdir(path.resolve(import.meta.dirname, ".."));

const CSS_DIR = "web/shared/css";
const SHARED_DIR = "web/shared";
const FONT_DIR = "web/shared/public/fonts";

// T1 — the canonical per-theme colour vocabulary (Step 1.1).
const T1 = [
  "--color-bg",
  "--color-surface-1",
  "--color-surface-2",
  "--color-surface-3",
  "--color-surface-dynamic",
  "--color-border",
  "--color-divider",
  "--color-text",
  "--color-text-muted",
  "--color-text-faint",
  "--color-primary",
  "--color-primary-hover",
  "--color-primary-active",
  "--color-primary-tint",
  "--color-primary-fg",
  "--color-danger",
  "--color-success",
  "--color-warning",
];

// T2 — the structural tokens declared on :root in tokens.css (Step 1.2).
const T2 = [
  "--radius-sm",
  "--radius-md",
  "--radius-lg",
  "--radius-full",
  "--shadow-sm",
  "--shadow-md",
  "--space-1",
  "--space-2",
  "--space-3",
  "--space-4",
  "--space-5",
  "--space-6",
  "--space-7",
  "--space-8",
  "--text-xs",
  "--text-sm",
  "--text-base",
  "--text-lg",
  "--text-xl",
  "--font-body",
  "--font-mono",
  "--font-body-fallback",
  "--font-mono-fallback",
  "--sidebar-width",
  "--topbar-height",
  "--transition",
  "--overlay-scrim",
];

// The depth-1 selector roster themes.css must declare, in no particular order.
const THEME_SELECTORS = [
  ':root, [data-theme="dark"]',
  '[data-theme="light"]',
  '[data-theme="obsidian"]',
  '[data-theme="forest"]',
  '[data-theme="ocean"]',
  '[data-theme="ember"]',
  '[data-theme="rose"]',
  '[data-theme="puma"]',
];

// The only non---color-* properties a theme block may declare: puma's two
// --font-* overrides (FRD :222-223) and light's scrim (Q1). A fourth fails.
const THEME_ALLOW = ["--font-body", "--font-mono", "--overlay-scrim"];

const EXPECTED_COLOR_DECLARATIONS = THEME_SELECTORS.length * T1.length; // 144
const EXPECTED_FONT_FACES = 15;

const COLOUR_LITERAL = /#[0-9a-f]{3,8}\b|rgba?\(|hsla?\(/i;
const UI_SELECTOR = /^\.ui-[a-z0-9-]+/;
// Font binaries and images carry no custom-property names; clause 11 skips them.
const BINARY_EXT = /\.(woff2?|ttf|otf|eot|png|jpg|jpeg|gif|webp|ico|pdf|zip)$/i;

function fail(file, line, clause, message) {
  process.stderr.write(`${file}:${line}: clause ${clause}: ${message}\n`);
  process.exit(1);
}

function bail(message) {
  process.stderr.write(`check-shared-css: ${message}\n`);
  process.exit(1);
}

// Clause 1 — strip comments before any other clause runs, so every "outside
// comments" qualifier below is structural rather than a regex afterthought.
// Newlines inside the comment are preserved so line numbers stay true.
function stripComments(text) {
  return text.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, " "));
}

function countNewlines(s) {
  let n = 0;
  for (const ch of s) if (ch === "\n") n++;
  return n;
}

function normalizeSelector(selector) {
  return selector
    .replace(/'/g, '"')
    .split(",")
    .map((part) => part.trim().replace(/\s+/g, " "))
    .join(", ");
}

// Clause 2 — block scanner. Walks the file tracking brace depth and collects
// every depth-1 block as {selector, declarations[], line}. Blocks whose
// selector starts with `@` are skipped, so a @media/@supports wrapper cannot
// masquerade as a rule.
function parseBlocks(text) {
  const blocks = [];
  let depth = 0;
  let line = 1;
  let selBuf = "";
  let selLine = 1;
  let bodyBuf = "";
  let bodyLine = 1;

  for (const ch of text) {
    if (ch === "{") {
      depth++;
      if (depth === 1) {
        bodyBuf = "";
        bodyLine = line;
      } else {
        bodyBuf += ch;
      }
    } else if (ch === "}") {
      if (depth === 1) {
        blocks.push({
          selector: selBuf.trim().replace(/\s+/g, " "),
          declarations: parseDeclarations(bodyBuf, bodyLine),
          line: selLine,
        });
        selBuf = "";
      } else if (depth > 1) {
        bodyBuf += ch;
      }
      depth = Math.max(0, depth - 1);
    } else {
      if (depth === 0) {
        if (selBuf.trim() === "" && ch.trim() !== "") selLine = line;
        selBuf += ch;
      } else {
        bodyBuf += ch;
      }
    }
    if (ch === "\n") line++;
  }

  return blocks.filter((b) => !b.selector.startsWith("@"));
}

// Declarations are split on `;` and each parsed as `prop: value`, with the
// property trimmed and the line of its first character recorded.
function parseDeclarations(body, bodyLine) {
  const declarations = [];
  let offset = 0;
  for (const chunk of body.split(";")) {
    const before = body.slice(0, offset);
    const lead = chunk.length - chunk.trimStart().length;
    const text = chunk.trim();
    offset += chunk.length + 1;
    if (text === "") continue;
    const colon = text.indexOf(":");
    if (colon === -1) continue;
    declarations.push({
      prop: text.slice(0, colon).trim(),
      value: text.slice(colon + 1).trim(),
      line: bodyLine + countNewlines(before) + countNewlines(chunk.slice(0, lead)),
    });
  }
  return declarations;
}

function setDiff(actual, expected) {
  const a = new Set(actual);
  const e = new Set(expected);
  return {
    missing: [...e].filter((k) => !a.has(k)),
    extra: [...a].filter((k) => !e.has(k)),
  };
}

// --- input ------------------------------------------------------------------

if (!fs.existsSync(CSS_DIR) || !fs.statSync(CSS_DIR).isDirectory()) {
  bail(`${CSS_DIR}/ does not exist — nothing to lint`);
}

const present = fs.readdirSync(CSS_DIR).filter((f) => f.endsWith(".css")).sort();
const sources = new Map();
for (const name of present) {
  sources.set(name, stripComments(fs.readFileSync(path.join(CSS_DIR, name), "utf8")));
}

for (const required of ["tokens.css", "themes.css", "components.css", "index.css"]) {
  if (!sources.has(required)) bail(`${CSS_DIR}/${required} is missing`);
}

// --- clause 3 — tokens.css --------------------------------------------------

{
  const file = `${CSS_DIR}/tokens.css`;
  const blocks = parseBlocks(sources.get("tokens.css"));
  if (blocks.length !== 1) {
    fail(file, blocks[1] ? blocks[1].line : 1, 3, `expected exactly one depth-1 block, found ${blocks.length}`);
  }
  if (blocks[0].selector !== ":root") {
    fail(file, blocks[0].line, 3, `selector must be ":root", found "${blocks[0].selector}"`);
  }
  const { missing, extra } = setDiff(blocks[0].declarations.map((d) => d.prop), T2);
  if (missing.length) fail(file, blocks[0].line, 3, `missing ${missing.length} T2 token(s): ${missing.join(", ")}`);
  if (extra.length) fail(file, blocks[0].line, 3, `${extra.length} token(s) not in T2: ${extra.join(", ")}`);
}

// --- clauses 4, 5, 6 — themes.css -------------------------------------------

{
  const file = `${CSS_DIR}/themes.css`;
  const blocks = parseBlocks(sources.get("themes.css"));

  // Clause 4 — the depth-1 selector roster must equal the 8 expected strings.
  const roster = blocks.map((b) => normalizeSelector(b.selector));
  const rosterDiff = setDiff(roster, THEME_SELECTORS);
  if (rosterDiff.missing.length) {
    fail(file, 1, 4, `missing theme block(s): ${rosterDiff.missing.join(" | ")}`);
  }
  if (rosterDiff.extra.length) {
    const offender = blocks.find((b) => rosterDiff.extra.includes(normalizeSelector(b.selector)));
    fail(file, offender.line, 4, `unexpected depth-1 selector(s): ${rosterDiff.extra.join(" | ")}`);
  }
  if (roster.length !== THEME_SELECTORS.length) {
    fail(file, 1, 4, `expected ${THEME_SELECTORS.length} theme blocks, found ${roster.length} (duplicate selector)`);
  }

  // Clause 5 — per-block set equality against T1, then the running total of
  // --color-* declarations across all 8 blocks. Both are needed: the set test
  // alone passes a block that declares one key twice and omits another.
  let colourTotal = 0;
  for (const block of blocks) {
    const colours = block.declarations.filter((d) => d.prop.startsWith("--color-"));
    colourTotal += colours.length;
    const { missing, extra } = setDiff(colours.map((d) => d.prop), T1);
    if (missing.length) {
      fail(file, block.line, 5, `${block.selector}: missing ${missing.length} T1 key(s): ${missing.join(", ")}`);
    }
    if (extra.length) {
      fail(file, block.line, 5, `${block.selector}: ${extra.length} key(s) not in T1: ${extra.join(", ")}`);
    }
  }
  if (colourTotal !== EXPECTED_COLOR_DECLARATIONS) {
    fail(file, 1, 5, `expected ${EXPECTED_COLOR_DECLARATIONS} --color-* declarations across the 8 blocks, found ${colourTotal}`);
  }

  // Clause 6 — the three-member allowlist for everything else.
  for (const block of blocks) {
    for (const d of block.declarations) {
      if (d.prop.startsWith("--color-")) continue;
      if (!THEME_ALLOW.includes(d.prop)) {
        fail(file, d.line, 6, `${d.prop} is not in the theme-block allowlist {${THEME_ALLOW.join(", ")}}`);
      }
    }
  }
}

// --- clause 7 — components.css ----------------------------------------------

{
  const file = `${CSS_DIR}/components.css`;
  const text = sources.get("components.css");

  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const hit = lines[i].match(COLOUR_LITERAL);
    if (hit) fail(file, i + 1, 7, `colour literal "${hit[0]}" — every colour must come from a token`);
  }

  for (const block of parseBlocks(text)) {
    for (const selector of block.selector.split(",")) {
      const one = selector.trim();
      if (one === "") continue;
      if (!UI_SELECTOR.test(one)) {
        fail(file, block.line, 7, `selector "${one}" does not start with .ui-*`);
      }
    }
  }
}

// --- clause 8 — index.css imports, and no external @import anywhere ---------

{
  for (const [name, text] of sources) {
    const file = `${CSS_DIR}/${name}`;
    const lines = text.split("\n");
    for (let i = 0; i < lines.length; i++) {
      if (!/@import/.test(lines[i])) continue;
      const target = lines[i].match(/@import\s+(?:url\(\s*)?["']?([^"')\s;]+)/);
      if (!target) fail(file, i + 1, 8, "unparseable @import");
      const spec = target[1];
      if (/^[a-z][a-z0-9+.-]*:/i.test(spec) || spec.startsWith("//")) {
        fail(file, i + 1, 8, `@import names an external URL: ${spec}`);
      }
      if (spec.includes("/") || spec.includes("..")) {
        fail(file, i + 1, 8, `@import must be a relative same-directory path: ${spec}`);
      }
      if (name === "index.css" && !present.includes(spec)) {
        fail(file, i + 1, 8, `@import "${spec}" names a file not present in ${CSS_DIR}/`);
      }
    }
  }
}

// --- clause 9 — no !important ------------------------------------------------

{
  for (const [name, text] of sources) {
    const lines = text.split("\n");
    for (let i = 0; i < lines.length; i++) {
      if (lines[i].includes("!important")) {
        fail(`${CSS_DIR}/${name}`, i + 1, 9, "!important is not permitted in shared CSS (R17)");
      }
    }
  }
}

// --- clause 10 — fonts.css (inert until C1b creates the file) ---------------

if (sources.has("fonts.css")) {
  const file = `${CSS_DIR}/fonts.css`;
  const text = sources.get("fonts.css");

  const faces = text.match(/@font-face\b/g) || [];
  if (faces.length !== EXPECTED_FONT_FACES) {
    fail(file, 1, 10, `expected exactly ${EXPECTED_FONT_FACES} @font-face rules, found ${faces.length}`);
  }

  // Forward: every url() resolves, mapping the served /shared/ prefix back to
  // the repo path web/shared/. Reverse: every woff2 present is referenced
  // exactly once. Both halves are required by G5.
  const referenced = new Map();
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    for (const m of lines[i].matchAll(/url\(\s*["']?([^"')]+)["']?\s*\)/g)) {
      const spec = m[1].trim();
      if (!spec.startsWith("/shared/")) {
        fail(file, i + 1, 10, `url("${spec}") must be served from /shared/`);
      }
      const repoPath = path.posix.join(SHARED_DIR, spec.slice("/shared/".length));
      if (!fs.existsSync(repoPath)) fail(file, i + 1, 10, `url("${spec}") does not resolve to ${repoPath}`);
      referenced.set(repoPath, (referenced.get(repoPath) || 0) + 1);
    }
  }
  for (const [repoPath, n] of referenced) {
    if (n !== 1) fail(file, 1, 10, `${repoPath} is referenced ${n} times, expected exactly once`);
  }

  const onDisk = [];
  const walk = (dir) => {
    if (!fs.existsSync(dir)) return;
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.posix.join(dir, e.name);
      if (e.isDirectory()) walk(full);
      else if (e.name.endsWith(".woff2")) onDisk.push(full);
    }
  };
  walk(FONT_DIR);
  const orphans = onDisk.filter((f) => !referenced.has(f));
  if (orphans.length) fail(file, 1, 10, `woff2 file(s) referenced by no url(): ${orphans.join(", ")}`);
}

// --- clause 11 — --color-error appears nowhere under web/shared/ -------------

{
  const stack = [SHARED_DIR];
  while (stack.length) {
    const dir = stack.pop();
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.posix.join(dir, e.name);
      if (e.isDirectory()) {
        stack.push(full);
        continue;
      }
      if (BINARY_EXT.test(e.name)) continue;
      const lines = fs.readFileSync(full, "utf8").split("\n");
      for (let i = 0; i < lines.length; i++) {
        if (lines[i].includes("--color-error")) {
          fail(full, i + 1, 11, "--color-error is not the canonical name; use --color-danger");
        }
      }
    }
  }
}

// --- clause 12 — the fonts-tree file-type allowlist and SHA256SUMS (G9) -----
//
// Phase 1 stated G9 as prose: web/shared/public/fonts/ may hold only *.woff2,
// OFL.txt, and the tracked SHA256SUMS. Clause 10 resolves fonts.css's url()s
// and forbids an unreferenced woff2; neither half notices a stray .zip, a
// .ttf, or an unlisted digest. This clause is that check, and unlike clause 10
// it does not depend on fonts.css existing — the tree is the subject, not the
// sheet. Both directions of the SHA256SUMS <-> disk correspondence are
// asserted, per G5, because either one alone passes half of a drift.

{
  const SUMS = path.posix.join(FONT_DIR, "SHA256SUMS");
  if (!fs.existsSync(SUMS)) {
    fail(SUMS, 1, 12, "the fonts tree's SHA256SUMS does not exist");
  }
  const sumsText = fs.readFileSync(SUMS, "utf8");
  if (sumsText.trim() === "") fail(SUMS, 1, 12, "SHA256SUMS is empty");

  // The allowlist, by name. A directory is descended into; every other entry
  // that is not a woff2, an OFL.txt or the SHA256SUMS itself fails by name.
  const woff2 = new Set();
  const walkFonts = (dir) => {
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.posix.join(dir, e.name);
      if (e.isDirectory()) {
        walkFonts(full);
        continue;
      }
      if (e.name.endsWith(".woff2")) {
        woff2.add(path.posix.relative(FONT_DIR, full));
        continue;
      }
      if (e.name === "OFL.txt" || e.name === "SHA256SUMS") continue;
      fail(full, 1, 12, `${e.name} is not in the fonts-tree allowlist {*.woff2, OFL.txt, SHA256SUMS}`);
    }
  };
  if (fs.existsSync(FONT_DIR)) walkFonts(FONT_DIR);

  // Forward: every digest line names a file that exists. A line's path is its
  // last whitespace-separated field, so sha256sum's two-space output parses
  // without a regex. Reverse: every woff2 on disk is listed.
  const listed = new Set();
  const sumLines = sumsText.split("\n");
  for (let i = 0; i < sumLines.length; i++) {
    const line = sumLines[i].trim();
    if (line === "") continue;
    const fields = line.split(/\s+/);
    const rel = fields[fields.length - 1];
    listed.add(rel);
    if (!fs.existsSync(path.posix.join(FONT_DIR, rel))) {
      fail(SUMS, i + 1, 12, `SHA256SUMS lists ${rel}, which is not on disk`);
    }
  }
  const unlisted = [...woff2].filter((f) => !listed.has(f)).sort();
  if (unlisted.length) {
    fail(SUMS, 1, 12, `woff2 file(s) absent from SHA256SUMS: ${unlisted.join(", ")}`);
  }
}

process.stdout.write(`check-shared-css: 12 clauses pass over ${present.length} file(s) in ${CSS_DIR}/\n`);
