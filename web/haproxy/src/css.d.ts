// esbuild resolves CSS imports through its loader; tsc has no notion of them.
// Without this the stylesheet import in main.tsx fails typecheck with TS2307.
declare module "*.css";
