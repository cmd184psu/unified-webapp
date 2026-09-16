// The single source of truth for what the web build produces.
//
// One descriptor array feeds the build driver, the test driver and the
// artifact list, so those three can never disagree about which files the
// build emits (PLAN-ui-unification-phase1.md §3 Step 3).
//
// This is a STATIC DATA MODULE: it imports nothing and executes nothing, so
// it can land with the gate harness (C1) ahead of the drivers that consume it
// (C2). Every path is relative to the repo root — never absolute. esbuild
// embeds relative source paths as comments inside the bundles it writes, so a
// single absolute path would rewrite every comment in every artifact and fail
// byte-identity everywhere at once (Step 3 driver rule 3, R2).
//
// Descriptor schema:
//   name           module name, also the descriptor's identity in gate output
//   entry          entry point(s), relative to the repo root
//   mode           "transpile" (esbuild outdir) | "bundle" (esbuild outfile)
//   out            the outdir for "transpile", the outfile for "bundle"
//   bundle         esbuild `bundle`
//   format         esbuild `format`; omitted means esbuild's default
//   jsx            esbuild `jsx`
//   define         esbuild `define`
//   logLevel       esbuild `logLevel`
//   loader         esbuild `loader`
//   external       esbuild `external`
//   emitsCss       true when the entry graph imports CSS, so esbuild writes a
//                  sibling `.css` next to `out` — a property of the import
//                  graph, not derivable from any other field
//   runner         test-runner mode for `.test.ts` descriptors (C2)
//   sharedConsumer true for descriptors that import `@shared` — selects the
//                  `@shared` onResolve plugin, the driver's format:"esm"
//                  assertion, and bundle-shape.mjs's input set. Absent on all
//                  seven descriptors below; first appears at C5.

// Placeholder for a `define` value the build driver computes rather than one
// this data module can hold. The driver substitutes the ADR-004 content
// digest; nothing here resolves it.
export const BUILD_TIME_DIGEST = "@build-time-digest";

export const descriptors = [
  {
    name: "obsidianoid",
    entry: ["web/obsidianoid/js/threads.ts", "web/obsidianoid/js/app.ts"],
    mode: "transpile",
    out: "web/obsidianoid/js/",
    bundle: false,
    target: "es2020",
  },
  {
    name: "slideshow",
    entry: ["web/slideshow/js/app.ts"],
    mode: "transpile",
    out: "web/slideshow/js/",
    bundle: false,
    target: "es2020",
  },
  {
    name: "multissh",
    entry: ["web/multissh/js/main.ts"],
    mode: "bundle",
    out: "web/multissh/js/bundle.js",
    bundle: true,
    format: "iife",
    target: "es2020",
    emitsCss: true,
  },
  {
    name: "certmachine",
    entry: ["web/certmachine/js/main.ts"],
    mode: "bundle",
    out: "web/certmachine/js/bundle.js",
    bundle: true,
    format: "iife",
    target: "es2020",
    emitsCss: true,
  },
  {
    name: "taskmaster",
    entry: ["web/taskmaster/js/main.ts"],
    mode: "bundle",
    out: "web/taskmaster/js/bundle.js",
    bundle: true,
    format: "iife",
    target: "es2020",
    // The value is the sentinel BUILD_TIME_DIGEST, not a literal: this module
    // is static data and the frontend stamp is a content digest the driver
    // computes in its two-pass build (Step 3 driver rule 4, ADR-004).
    define: { __TM_BUILD_TIME__: BUILD_TIME_DIGEST },
  },
  {
    name: "smbedit",
    entry: ["web/smbedit/src/main.tsx"],
    mode: "bundle",
    out: "web/smbedit/js/bundle.js",
    bundle: true,
    format: "iife",
    target: "es2020",
    jsx: "automatic",
    logLevel: "warning",
    define: { "process.env.NODE_ENV": '"production"' },
    emitsCss: true,
  },
  {
    name: "issuetracker",
    entry: ["web/issuetracker/src/main.tsx"],
    mode: "bundle",
    out: "web/issuetracker/js/bundle.js",
    bundle: true,
    format: "iife",
    target: "es2020",
    jsx: "automatic",
    logLevel: "warning",
    define: { "process.env.NODE_ENV": '"production"' },
    emitsCss: true,
  },
];

// How many files the descriptors above emit. Defined ONCE, here: the
// generator and every gate that counts import it, so a truncated list is a
// failure rather than a silent pass (A7.3). The value moves during the
// sequence — 12 through C3, 14 after C4, 15 after C5 — and each move is an
// edit to this one line.
export const EXPECTED_ARTIFACT_COUNT = 12;
