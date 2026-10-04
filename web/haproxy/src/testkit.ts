// testkit.ts — a tiny assertion/runner for the pure-logic suites. test-web.mjs
// bundles each *.test.ts and pipes it to node (no jsdom, no test framework);
// a thrown error is the whole failure report. Not imported by the app bundle.

let failures = 0

export function test(name: string, fn: () => void): void {
  try {
    fn()
    console.log(`ok - ${name}`)
  } catch (e) {
    failures++
    console.error(`FAIL - ${name}: ${e instanceof Error ? e.message : String(e)}`)
  }
}

export function done(suite: string): void {
  if (failures > 0) throw new Error(`${suite}: ${failures} test(s) failed`)
}

export function eq<T>(got: T, want: T, what = 'value'): void {
  const g = JSON.stringify(got)
  const w = JSON.stringify(want)
  if (g !== w) throw new Error(`${what}: got ${g}, want ${w}`)
}

export function ok(cond: unknown, what: string): void {
  if (!cond) throw new Error(what)
}
