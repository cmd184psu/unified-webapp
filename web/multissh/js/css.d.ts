// esbuild resolves CSS imports through its loader; tsc has no notion of them.
// Without this the two stylesheet imports in main.ts and terminal.ts fail
// typecheck with TS2307 the moment this directory joins the tsconfig include
// list.
declare module "*.css";
