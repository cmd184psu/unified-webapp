// @ts-nocheck
// Source guards for the owner's binding rules (FRD §9): boolean settings are
// toggle switches, never checkboxes, and nothing saves except the Save button.
// They scan web/haproxy/src as text (cwd is the repo root under test-web.mjs).
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { test, done, ok, eq } from './testkit'

const DIR = join(process.cwd(), 'web/haproxy/src')
const files = readdirSync(DIR)
  .filter(f => /\.(ts|tsx)$/.test(f) && !/\.test\.ts$/.test(f) && f !== 'testkit.ts')
  .map(f => ({ name: f, text: readFileSync(join(DIR, f), 'utf8') }))

const CHECKBOX = /type\s*[=:]\s*\{?\s*['"]checkbox['"]/g
const HANDLER = /\bon(Change|Input|Blur|KeyUp|KeyDown)\s*=\s*\{/g

// Returns the balanced {...} body starting at the "{" at index i.
function braceBody(text: string, i: number): string {
  let depth = 0
  for (let j = i; j < text.length; j++) {
    if (text[j] === '{') depth++
    else if (text[j] === '}') { depth--; if (depth === 0) return text.slice(i, j + 1) }
  }
  return text.slice(i)
}

test('guard sees the sources (a scan of nothing proves nothing)', () => {
  ok(files.length >= 6, `scanned ${files.length} files`)
  ok(files.some(f => f.name === 'App.tsx') && files.some(f => f.name === 'api.ts'), 'App.tsx and api.ts are scanned')
})

test('no checkbox input outside the shared toggle markup (Toggle.tsx only, inside .ui-toggle)', () => {
  for (const f of files) {
    const hits = f.text.match(CHECKBOX) ?? []
    if (f.name === 'Toggle.tsx') {
      ok(hits.length === 1, 'Toggle.tsx has exactly one checkbox input')
      ok(/className="ui-toggle"/.test(f.text) && /ui-toggle-track/.test(f.text), 'Toggle.tsx uses the ui-toggle markup')
    } else {
      eq(hits.length, 0, `${f.name}: bare checkbox`)
    }
  }
})

test('no change/input/blur/keyup handler saves (no autosave)', () => {
  let seen = 0
  for (const f of files) {
    for (const m of f.text.matchAll(HANDLER)) {
      seen++
      const body = braceBody(f.text, m.index + m[0].length - 1)
      ok(!/saveModel|\bsave\b|fetch\(|\bPUT\b|api\.\w*(put|save)/i.test(body), `${f.name}: ${m[0]} handler saves: ${body.slice(0, 80)}`)
    }
  }
  ok(seen > 5, `inspected ${seen} handlers`)
})

test('fetch and PUT live only in api.ts', () => {
  for (const f of files) {
    if (f.name === 'api.ts') continue
    ok(!/\bfetch\(/.test(f.text), `${f.name} calls fetch directly`)
    ok(!/['"]PUT['"]/.test(f.text), `${f.name} issues a PUT`)
  }
})

// saveModel has exactly two callers, both explicit buttons in App.tsx: the Save
// button (save) and the first-run "Start with defaults" button (startWithDefaults).
test('only the Save and Start-with-defaults buttons call saveModel', () => {
  const callers = []
  for (const f of files) {
    if (f.name === 'api.ts') continue
    for (const m of f.text.matchAll(/\bsaveModel\(/g)) callers.push({ file: f.name, at: m.index })
  }
  eq(callers.length, 2, 'saveModel( call sites outside api.ts')
  ok(callers.every(c => c.file === 'App.tsx'), 'all in App.tsx')
  const app = files.find(f => f.name === 'App.tsx').text
  const inFn = (decl: string, endMark: string, at: number) => {
    const start = app.indexOf(decl)
    ok(start >= 0, `App.tsx defines ${decl}`)
    const end = app.indexOf(endMark, start)
    return at > start && at < end
  }
  ok(callers.some(c => inFn('const save = ', '\n  }, [', c.at)), 'one call is inside save()')
  ok(callers.some(c => inFn('const startWithDefaults = ', '\n  }\n', c.at)), 'one call is inside startWithDefaults()')
  eq((app.match(/onClick=\{save\}/g) ?? []).length, 1, 'save is wired to exactly one onClick')
  const idx = app.indexOf('onClick={save}')
  ok(/Save/.test(app.slice(idx, idx + 200)), 'that onClick is the Save button')
  eq((app.match(/onClick=\{startWithDefaults\}/g) ?? []).length, 1, 'startWithDefaults is wired to exactly one onClick')
  const sd = app.indexOf('onClick={startWithDefaults}')
  ok(/Start with defaults/.test(app.slice(sd, sd + 200)), 'that onClick is the Start with defaults button')
  // neither is handed to anything else (child props, effects, timers).
  eq((app.match(/[=({,]\s*save\b(?!\s*=)/g) ?? []).length, 1, 'save referenced exactly once (the button)')
  eq((app.match(/[=({,]\s*startWithDefaults\b(?!\s*=)/g) ?? []).length, 1, 'startWithDefaults referenced exactly once')
})

test('no text/change handler triggers a mutating action (only buttons and toggles act)', () => {
  const MUTATE = /\b(apply|restore|remove|update|choose|check|startWithDefaults|importCommit|importPreview|service)\(|api\.(apply|pullCert|deleteCert|restoreBackup|reload|restart|start|setCertEnabled|check)\b/
  for (const f of files) {
    for (const m of f.text.matchAll(/\bon(Input|Blur|KeyUp|KeyDown)\s*=\s*\{/g)) {
      const body = braceBody(f.text, m.index + m[0].length - 1)
      ok(!MUTATE.test(body), `${f.name}: ${m[0]} runs an action: ${body.slice(0, 80)}`)
    }
    // onChange may only drive a Toggle (cert enable) or edit local/draft state, never apply/restore/delete/pull.
    for (const m of f.text.matchAll(/\bonChange\s*=\s*\{/g)) {
      const body = braceBody(f.text, m.index + m[0].length - 1)
      ok(!/\b(apply|restore|remove|update|choose|startWithDefaults|importCommit)\(|api\.(apply|pullCert|deleteCert|restoreBackup|reload|restart|start)\b/.test(body), `${f.name}: onChange runs an action: ${body.slice(0, 80)}`)
    }
  }
})

test('the new B6b files are scanned', () => {
  for (const n of ['PendingBar.tsx', 'CertsPage.tsx', 'CertPicker.tsx', 'BackupsPage.tsx', 'LogPage.tsx', 'RawPage.tsx', 'applyAction.ts', 'pending.ts', 'certview.ts', 'livefeed.ts', 'statsview.ts', 'StatsPage.tsx']) {
    ok(files.some(f => f.name === n), `${n} is scanned`)
  }
})

test('Apply is triggered only by the Apply button path', () => {
  const app = files.find(f => f.name === 'App.tsx').text
  eq((app.match(/api\.apply\b/g) ?? []).length, 1, 'api.apply used once')
  eq((app.match(/onApply=\{apply\}/g) ?? []).length, 1, 'apply wired once, to the bar')
  const bar = files.find(f => f.name === 'PendingBar.tsx').text
  eq((bar.match(/onClick=\{onApply\}/g) ?? []).length, 1, 'bar calls onApply from one onClick')
})

test('the Stats page is read-only: only Refresh, no checkbox/input, no mutating api call', () => {
  const t = files.find(f => f.name === 'StatsPage.tsx').text
  eq((t.match(/<button\b/g) ?? []).length, 1, 'exactly one button')
  ok(/Refresh/.test(t), 'it is Refresh')
  ok(!/<input\b|<select\b|<textarea\b/.test(t), 'no form controls')
  ok(!/api\.(?!stats\b)\w+/.test(t), 'only api.stats is used')
})

test('the Remove button is driven by removeButtonState, never hard-coded enabled', () => {
  const t = files.find(f => f.name === 'CertsPage.tsx').text
  ok(/removeButtonState\(/.test(t), 'uses removeButtonState')
  const m = t.split('\n').filter(l => />\s*Remove\s*<\/button>/.test(l))
  eq(m.length, 1, 'one Remove button line')
  ok(/disabled=\{[^}]*\.disabled\}/.test(m[0]), 'disabled attribute comes from the state')
  ok(/title=\{[^}]*\.title\}/.test(m[0]), 'title comes from the state')
  eq((t.match(/>\s*Remove\s*<\/button>/g) ?? []).length, 1, 'exactly one Remove button')
})

done('sourceguard')
