// buildinfo.ts — the frontend bundle's own build timestamp.
//
// Injected by esbuild's --define at build time (see package.json's "build"
// and "build:dev" scripts). Shown in the hamburger's Server section
// alongside the BACKEND's build time (from GET /api/health) so a stale
// frontend bundle and a stale backend binary are each visible on their own
// — this exists because they are two independent build artifacts (the Go
// binary embeds nothing about the JS bundle's freshness), and a stale one
// once looked identical to a fresh one with no way to tell short of
// comparing file mtimes by hand.
declare const __TM_BUILD_TIME__: string;

export const FRONTEND_BUILD_TIME: string = __TM_BUILD_TIME__;
