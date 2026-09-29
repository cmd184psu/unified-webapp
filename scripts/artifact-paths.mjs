// artifact-paths.mjs — the ONE definition of "which files does this
// descriptor emit?" (phase2 §5 Step 5.1, B9.4).
//
// Lifted verbatim out of scripts/list-artifacts.mjs, where the derivation was
// private to the generator. Two consumers need it now, for two different
// reasons, and a second inline copy would be the same fact stated twice:
//
//   scripts/list-artifacts.mjs   — prints one path per line for the artifact
//                                  count gate
//   scripts/gates/bundle-shape.mjs — inspects each emitted file, rather than
//                                  reading `d.out`, which is a DIRECTORY for
//                                  any multi-entry transpile and made the gate
//                                  throw EISDIR (§4.2.1(b), B9.3)
//
// Derivation rules, mechanical and unchanged:
//   mode "transpile" — `out` is esbuild's outdir; each entry emits its own
//                      basename with the .ts/.tsx extension replaced by .js
//   mode "bundle"    — `out` is esbuild's outfile; `emitsCss: true` adds the
//                      sibling stylesheet esbuild writes beside it
//
// RELATIVE PATHS ONLY, deliberately: list-artifacts.mjs keeps its own
// absolute-path assertion over the descriptor's own fields (driver rule 3),
// and this helper must not launder a path into an absolute one behind it.

import path from "node:path";

/**
 * Every artifact path `descriptor` emits, repo-relative and in emit order.
 * Throws on an unknown mode rather than returning an empty list, so a
 * mistyped mode is a loud failure in both consumers instead of a silently
 * uninspected descriptor.
 */
export function artifactPaths(descriptor) {
  if (descriptor.mode === "transpile") {
    return descriptor.entry.map((entry) =>
      path.posix.join(descriptor.out, path.basename(entry).replace(/\.tsx?$/, ".js")),
    );
  }
  if (descriptor.mode === "bundle") {
    const out = [descriptor.out];
    if (descriptor.emitsCss) out.push(descriptor.out.replace(/\.js$/, ".css"));
    return out;
  }
  throw new Error(
    `artifact-paths: descriptor "${descriptor.name}": unknown mode ${JSON.stringify(descriptor.mode)}`,
  );
}
