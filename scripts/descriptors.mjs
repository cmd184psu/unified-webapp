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
//                  nine descriptors below (including `shared` and
//                  `shared-css`, added at C4 — they ARE the library, not a
//                  consumer of it); first appears at C5.

// Placeholder for a `define` value the build driver computes rather than one
// this data module can hold. The driver substitutes the ADR-004 content
// digest; nothing here resolves it.
export const BUILD_TIME_DIGEST = "@build-time-digest";

export const descriptors = [
  // Shared descriptors run first (driver rule 1): every other descriptor's
  // consumer status is orthogonal, but the shared bundle and shared sheet
  // must exist before anything that might resolve @shared or link
  // /shared/dist/shared.css. Neither carries sharedConsumer — they ARE the
  // library, not a consumer of it.
  {
    name: "shared",
    entry: ["web/shared/ts/index.ts"],
    mode: "bundle",
    out: "web/shared/dist/shared.mjs",
    bundle: true,
    format: "esm",
    target: "es2020",
  },
  {
    name: "shared-css",
    entry: ["web/shared/css/index.css"],
    mode: "bundle",
    out: "web/shared/dist/shared.css",
    bundle: true,
    external: ["*.woff2"],
  },
  // sampler is the first sharedConsumer descriptor: it selects the @shared
  // onResolve plugin (driver rule 10), triggers the driver's format:"esm"
  // assertion, and is the first member of bundle-shape.mjs's input set
  // (A9.1/A9.2, PLAN-ui-unification-phase1.md Step 6).
  {
    name: "sampler",
    entry: ["web/sampler/js/main.ts"],
    mode: "bundle",
    out: "web/sampler/js/bundle.js",
    bundle: true,
    format: "esm",
    target: "es2020",
    sharedConsumer: true,
  },
  {
    name: "obsidianoid",
    entry: ["web/obsidianoid/js/threads.ts", "web/obsidianoid/js/app.ts"],
    mode: "transpile",
    out: "web/obsidianoid/js/",
    bundle: true,
    format: "esm",
    target: "es2020",
    sharedConsumer: true,
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
    format: "esm",
    target: "es2020",
    emitsCss: true,
    sharedConsumer: true,
  },
  {
    name: "certmachine",
    entry: ["web/certmachine/js/main.ts"],
    mode: "bundle",
    out: "web/certmachine/js/bundle.js",
    bundle: true,
    format: "esm",
    target: "es2020",
    emitsCss: true,
    sharedConsumer: true,
  },
  {
    name: "taskmaster",
    entry: ["web/taskmaster/js/main.ts"],
    mode: "bundle",
    out: "web/taskmaster/js/bundle.js",
    bundle: true,
    format: "esm",
    target: "es2020",
    sharedConsumer: true,
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
  {
    name: "todo",
    entry: ["web/todo/js/shell.ts"],
    mode: "bundle",
    out: "web/todo/js/shell.js",
    bundle: true,
    format: "esm",
    target: "es2020",
    sharedConsumer: true,
  },
];

// How many files the descriptors above emit. Defined ONCE, here: the
// generator and every gate that counts import it, so a truncated list is a
// failure rather than a silent pass (A7.3). The value moves during the
// sequence — 12 through C3, 14 after C4, 15 after C5 — and each move is an
// edit to this one line.
export const EXPECTED_ARTIFACT_COUNT = 16;
