// ndjson.test.ts -- unit coverage for ndjson.ts's pure line-splitter, run by
// `npm run test:web` (see scripts/test-web.mjs). There is no browser here --
// the file is bundled for node and throws on failure, so a non-zero exit is
// the whole report, matching listmodel.test.ts's own pattern.

import { parseNDJSONChunk, parseNDJSONFinal, percentFor, caKeySegmentCeiling, creepToward } from "./ndjson";

function check(name: string, cond: boolean, detail: string): void {
  if (!cond) throw new Error(`${name}: ${detail}`);
  console.log(`ok - ${name}`);
}

// Multiple complete lines arriving in a single chunk.
{
  const { values, remainder } = parseNDJSONChunk<{ n: number }>(
    "",
    '{"n":1}\n{"n":2}\n{"n":3}\n',
  );
  check("multiple lines per chunk: count", values.length === 3, `got ${values.length}`);
  check(
    "multiple lines per chunk: values",
    values[0].n === 1 && values[1].n === 2 && values[2].n === 3,
    JSON.stringify(values),
  );
  check("multiple lines per chunk: no remainder", remainder === "", `remainder=${JSON.stringify(remainder)}`);
}

// A line split across two chunk boundaries must not be parsed until the
// second chunk supplies its closing newline.
{
  const first = parseNDJSONChunk<{ n: number }>("", '{"n":1}\n{"n":2');
  check("split chunk: first chunk yields only the complete line", first.values.length === 1, JSON.stringify(first));
  check("split chunk: first chunk's value", first.values[0].n === 1, JSON.stringify(first.values));
  check("split chunk: remainder carries the partial line", first.remainder === '{"n":2', JSON.stringify(first.remainder));

  const second = parseNDJSONChunk<{ n: number }>(first.remainder, '}\n{"n":3}\n');
  check("split chunk: second chunk completes the carried line", second.values.length === 2, JSON.stringify(second));
  check(
    "split chunk: second chunk's values",
    second.values[0].n === 2 && second.values[1].n === 3,
    JSON.stringify(second.values),
  );
  check("split chunk: no remainder after completion", second.remainder === "", JSON.stringify(second.remainder));
}

// A trailing partial line at the very end of a chunk, with nothing yet to
// complete it, must be carried forward and never parsed early.
{
  const { values, remainder } = parseNDJSONChunk<{ n: number }>("", '{"n":1}\n{"partial":');
  check("trailing partial line: only the complete line parses", values.length === 1 && values[0].n === 1, JSON.stringify(values));
  check(
    "trailing partial line: remainder holds the rest",
    remainder === '{"partial":',
    JSON.stringify(remainder),
  );
}

// parseNDJSONFinal: the stream's closing remainder, when the server's last
// line had no trailing newline, is exactly one more value; a blank
// remainder (the normal, every-line-ends-in-\n case) yields null.
{
  const final = parseNDJSONFinal<{ n: number }>('{"n":4}');
  check("final: parses a trailing line with no newline", final !== null && final.n === 4, JSON.stringify(final));

  check("final: blank remainder yields null", parseNDJSONFinal("") === null, "expected null");
  check("final: whitespace-only remainder yields null", parseNDJSONFinal("   \n") === null, "expected null");
}

// A malformed line must throw, not silently disappear -- a protocol
// violation, not routine data.
{
  let threw = false;
  try {
    parseNDJSONChunk("", "{not json}\n");
  } catch {
    threw = true;
  }
  check("malformed line: parseNDJSONChunk throws", threw, "expected a thrown error");
}
{
  let threw = false;
  try {
    parseNDJSONFinal("{not json}");
  } catch {
    threw = true;
  }
  check("malformed line: parseNDJSONFinal throws", threw, "expected a thrown error");
}

// Blank lines (the stream's own trailing empty line after its last \n) are
// skipped, not errors and not spurious values.
{
  const { values, remainder } = parseNDJSONChunk<{ n: number }>("", '{"n":1}\n\n{"n":2}\n');
  check("blank line is skipped", values.length === 2 && values[0].n === 1 && values[1].n === 2, JSON.stringify(values));
  check("blank line: no remainder", remainder === "", JSON.stringify(remainder));
}

// percentFor: the overall 0-100 percentage computed from a ReplaceCA
// progress event, per the CA-replacement progress-bar follow-up (owner
// requirement: the bar counts up to 100% wherever possible).

// A reissue run with total=59 leaves: ca-key -> 0%, each leaf-keys step
// increases monotonically, saving -> (4+59)/64, done -> 100%.
{
  const total = 59;
  const units = 4 + total + 1; // CA_KEY_WEIGHT + total + SAVE_WEIGHT

  let p = percentFor("ca-key", 0, total, 0);
  check("reissue: ca-key is 0%", p === 0, `got ${p}`);

  p = percentFor("leaf-keys", 1, total, p);
  const first = ((4 + 1) / units) * 100;
  check("reissue: first leaf-keys matches (CA_KEY_WEIGHT+1)/units", p === first, `got ${p}, want ${first}`);

  let prev = p;
  for (let done = 2; done <= total; done++) {
    const next = percentFor("leaf-keys", done, total, prev);
    check(`reissue: leaf-keys done=${done} never decreases`, next >= prev, `prev=${prev} next=${next}`);
    const expected = ((4 + done) / units) * 100;
    check(`reissue: leaf-keys done=${done} matches (CA_KEY_WEIGHT+done)/units`, next === expected, `got ${next}, want ${expected}`);
    prev = next;
  }

  const atSaving = percentFor("saving", 0, total, prev);
  const expectedSaving = ((4 + total) / units) * 100;
  check("reissue: saving matches (CA_KEY_WEIGHT+total)/units", atSaving === expectedSaving, `got ${atSaving}, want ${expectedSaving}`);

  const atDone = percentFor("done", 0, total, atSaving);
  check("reissue: done is 100%", atDone === 100, `got ${atDone}`);
}

// total=0 (keep/delete, nothing re-issued): 0% at ca-key, ~80% at saving,
// 100% at done -- the bar still moves even with no leaf keys at all.
{
  let p = percentFor("ca-key", 0, 0, 0);
  check("total=0: ca-key is 0%", p === 0, `got ${p}`);

  p = percentFor("saving", 0, 0, p);
  check("total=0: saving is 80%", p === 80, `got ${p}`);

  p = percentFor("done", 0, 0, p);
  check("total=0: done is 100%", p === 100, `got ${p}`);
}

// Percent never decreases even if called with an out-of-order/lower raw
// value than what was already reached.
{
  const p = percentFor("ca-key", 0, 100, 42);
  check("percent never decreases below prevPercent", p === 42, `got ${p}`);
}

// Clamping: negative done/total never push the result outside [0,100].
{
  const p = percentFor("leaf-keys", -5, 10, 0);
  check("negative done clamps to >= 0", p >= 0, `got ${p}`);
}

// caKeySegmentCeiling: the "ca-key" phase's bar creeps toward, but must
// never reach, whatever percentFor("leaf-keys", 0, total, 0) would report --
// otherwise the crept value could equal or exceed the first real event's
// percentage and defeat percentFor's own monotonic floor.
{
  const total = 59;
  const ceiling = caKeySegmentCeiling(total);
  const firstLeafKeysPercent = percentFor("leaf-keys", 0, total, 0);
  check(
    "caKeySegmentCeiling stays strictly below the first real leaf-keys percent",
    ceiling < firstLeafKeysPercent,
    `ceiling=${ceiling}, firstLeafKeysPercent=${firstLeafKeysPercent}`,
  );
  check("caKeySegmentCeiling is never negative", ceiling >= 0, `got ${ceiling}`);
}
{
  // total=0 (keep/delete): the ceiling must still stay below saving's own
  // 80% (the total=0 case's first real event after "ca-key" is "saving",
  // not "leaf-keys").
  const ceiling = caKeySegmentCeiling(0);
  const savingPercent = percentFor("saving", 0, 0, 0);
  check(
    "caKeySegmentCeiling(0) stays strictly below saving's percent",
    ceiling < savingPercent,
    `ceiling=${ceiling}, savingPercent=${savingPercent}`,
  );
  check("caKeySegmentCeiling(0) is never negative", ceiling >= 0, `got ${ceiling}`);
}

// creepToward: approaches but never reaches or exceeds the ceiling, and
// never moves backwards.
{
  let value = 1;
  const ceiling = 20;
  for (let i = 0; i < 50; i++) {
    const next = creepToward(value, ceiling, 5);
    check(`creepToward step ${i} never decreases`, next >= value, `value=${value} next=${next}`);
    check(`creepToward step ${i} never reaches the ceiling`, next < ceiling, `next=${next} ceiling=${ceiling}`);
    value = next;
  }
  check("creepToward gets meaningfully close to the ceiling over many steps", value > 19, `got ${value}`);
}
{
  // Already at or past the ceiling: creepToward must not move at all.
  const atCeiling = creepToward(20, 20, 5);
  check("creepToward at the ceiling stays put", atCeiling === 20, `got ${atCeiling}`);
  const pastCeiling = creepToward(25, 20, 5);
  check("creepToward past the ceiling stays put", pastCeiling === 25, `got ${pastCeiling}`);
}
{
  // step caps the per-call increment even far from the ceiling.
  const next = creepToward(0, 1000, 3);
  check("creepToward is capped by step even far from the ceiling", next === 3, `got ${next}`);
}

console.log("ndjson.test.ts: all checks passed");
