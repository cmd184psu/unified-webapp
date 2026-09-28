// ndjson.ts -- the pure line-splitting/parsing core behind
// api.ts's replaceCAWithProgress (the CA-replacement plan's "real progress
// bar": ReplaceCA's crypto can take a noticeable time for a large blanket
// re-issue, and POST /api/ca/replace streams NDJSON progress lines while it
// runs when the request carries `Accept: application/x-ndjson`).
//
// Kept in its own module, with no DOM/fetch dependency, so it can be
// unit-tested directly against raw strings -- chunk boundaries can split a
// line anywhere, including mid-object, so the split/parse logic is the part
// most worth testing in isolation from any real network stream.

/** One call's result: every complete line's parsed value, plus whatever
 * incomplete trailing text should be carried into the next chunk. */
export interface NDJSONChunkResult<T> {
  values: T[];
  remainder: string;
}

/**
 * Feeds one new chunk of text, together with `remainder` left over from the
 * previous call (pass `""` for the first chunk), into a pure NDJSON
 * splitter: split on `\n`, keep the last (possibly incomplete) piece as the
 * new remainder, and JSON-parse every complete, non-blank line in order.
 *
 * A blank line (the ndjson stream ends with a single trailing empty line
 * after its last `\n`) is skipped, not an error. A line that fails to parse
 * as JSON is a real protocol violation -- it throws, rather than being
 * silently dropped, so a malformed stream surfaces as a caught error instead
 * of quietly losing an event.
 */
export function parseNDJSONChunk<T = unknown>(remainder: string, chunk: string): NDJSONChunkResult<T> {
  const buffer = remainder + chunk;
  const pieces = buffer.split("\n");
  const newRemainder = pieces.pop() ?? "";
  const values: T[] = [];
  for (const piece of pieces) {
    if (piece.trim() === "") continue;
    values.push(JSON.parse(piece) as T);
  }
  return { values, remainder: newRemainder };
}

/**
 * Parses whatever remainder is left once the stream has ended, in case the
 * server's very last line had no trailing `\n`. Returns `null` for a blank
 * (or empty) remainder -- the common case, since every line this package
 * writes does end in `\n`. Throws on a non-blank remainder that fails to
 * parse, exactly like parseNDJSONChunk does for a mid-stream line.
 */
export function parseNDJSONFinal<T = unknown>(remainder: string): T | null {
  if (remainder.trim() === "") return null;
  return JSON.parse(remainder) as T;
}

/**
 * The phases `percentFor` accepts: the server's three ReplaceCA progress
 * phases, plus `"done"` for the stream's own final result line, which has no
 * server-side "phase" of its own but still needs a percentage (100%).
 */
export type ReplacePercentPhase = "ca-key" | "leaf-keys" | "saving" | "done";

// CA_KEY_WEIGHT and SAVE_WEIGHT are the two fixed-size steps of the overall
// bar, expressed in the same "units" as one leaf key: the new CA's RSA-4096
// key takes noticeably longer than one leaf's RSA-2048 key (roughly several
// of them), and the closing transaction is comparatively quick, hence 4 and
// 1. Owner requirement: "the progress bar should count up to 100%, wherever
// possible" -- with these weights, `total===0` (keep/delete, no leaf re-issue
// at all) still produces a bar that moves: 0% at ca-key, (4+0)/5=80% at
// saving, 100% at done.
const CA_KEY_WEIGHT = 4;
const SAVE_WEIGHT = 1;

/**
 * Computes the overall 0-100 percentage for one point in a ReplaceCA
 * progress stream, given `total` -- the overall leaf-key count, present on
 * every progress event as of this follow-up -- and `prevPercent`, the
 * percentage returned by the previous call (0 for the very first call).
 * Pure: no DOM/fetch/state of its own, so it is unit-tested directly.
 *
 * The model treats the whole operation as `units = CA_KEY_WEIGHT + total +
 * SAVE_WEIGHT` steps: the CA key is worth CA_KEY_WEIGHT steps, each leaf key
 * is worth 1, and the save is worth SAVE_WEIGHT. "ca-key" is 0/units (nothing
 * finished yet); "leaf-keys" is (CA_KEY_WEIGHT+done)/units; "saving" is
 * (CA_KEY_WEIGHT+total)/units (every leaf key is done); "done" is always
 * 100%. The result is clamped to [0,100] and never allowed to drop below
 * `prevPercent` -- a defensive floor, since every phase's own value is
 * already monotonic by construction.
 */
export function percentFor(phase: ReplacePercentPhase, done: number, total: number, prevPercent: number): number {
  if (phase === "done") return 100;

  const units = CA_KEY_WEIGHT + Math.max(total, 0) + SAVE_WEIGHT;
  let raw: number;
  switch (phase) {
    case "ca-key":
      raw = 0;
      break;
    case "leaf-keys":
      raw = ((CA_KEY_WEIGHT + Math.max(done, 0)) / units) * 100;
      break;
    case "saving":
      raw = ((CA_KEY_WEIGHT + Math.max(total, 0)) / units) * 100;
      break;
  }
  const clamped = Math.min(100, Math.max(0, raw));
  return Math.max(clamped, prevPercent);
}

/**
 * The percentage the "ca-key" phase's bar creeps toward (but never reaches)
 * while the new CA's RSA-4096 key is generated -- the longest single step
 * for a keep/delete run, where the bar would otherwise sit at 0% the whole
 * time and then jump straight to the "leaf-keys"/"saving" percentage (owner
 * requirement: "count up wherever possible"). This is exactly
 * `percentFor("leaf-keys", 0, total, 0)` (the percentage the very first real
 * event after "ca-key" will report) minus a small margin, so the crept value
 * is always strictly below whatever the next real event computes --
 * `percentFor`'s own `prevPercent` floor then guarantees the bar never drops
 * back down once that real event arrives.
 */
export function caKeySegmentCeiling(total: number): number {
  const units = CA_KEY_WEIGHT + Math.max(total, 0) + SAVE_WEIGHT;
  const marginUnits = 0.5;
  const ceiling = ((CA_KEY_WEIGHT - marginUnits) / units) * 100;
  return Math.max(0, ceiling);
}

/**
 * Eases `current` a fraction of the way toward `ceiling`, capped at `step`
 * per call, and never reaching (let alone passing) `ceiling`: each call
 * covers at most half of whatever distance remains, so the result strictly
 * approaches `ceiling` without ever equaling it (for `current < ceiling`).
 * Pure -- no timers, no DOM -- so the easing math is unit-tested directly;
 * the caller (cadialog.ts) owns the actual interval timer.
 */
export function creepToward(current: number, ceiling: number, step: number): number {
  const remaining = ceiling - current;
  if (remaining <= 0) return current;
  return current + Math.min(step, remaining / 2);
}
