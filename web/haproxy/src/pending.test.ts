import { test, done, eq, ok } from './testkit'
import type { Changes, ApplyResult } from './api'
import { pendingView, applyToast, applyErrorToast, classifyDiff, serviceActionNeedsConfirm, diagnosticNotices } from './pending'

const none: Changes = { hasChanges: false, configDiff: '', crtListDiff: '', summary: '' }
const some: Changes = { hasChanges: true, configDiff: '--- a\n+++ b\n', crtListDiff: '', summary: 'config changed' }

test('no changes: label, Apply disabled and labelled so', () => {
  const v = pendingView(none)
  eq(v.label, 'No changes')
  eq(v.applyDisabled, true)
  eq(v.applyLabel, 'No changes to apply')
  eq(v.canReview, false)
})

test('changes: Pending label, summary, Apply enabled', () => {
  const v = pendingView(some)
  eq(v.label, 'Pending changes')
  eq(v.summary, 'config changed')
  eq(v.applyDisabled, false)
  eq(v.applyLabel, 'Apply changes')
  eq(v.canReview, true)
})

test('unknown (not loaded yet) is not claimed as no changes and disables Apply', () => {
  const v = pendingView(null)
  eq(v.label, 'Checking for changes…')
  eq(v.applyDisabled, true)
})

test('busy disables Apply even with changes', () => {
  eq(pendingView(some, true).applyDisabled, true)
})

const res = (outcome: ApplyResult['outcome'], message = 'm'): ApplyResult =>
  ({ applied: outcome === 'applied', rolledBack: outcome === 'rolled_back', outcome, message })

test('apply outcome maps to a toast for every outcome', () => {
  eq(applyToast(res('applied')).tone, 'success')
  eq(applyToast(res('no_changes')).tone, 'notice')
  const v = applyToast(res('validation_failed', 'unknown keyword foo'))
  eq(v.tone, 'error')
  ok(v.message.includes('unknown keyword foo'), 'haproxy message shown')
  const r = applyToast(res('rolled_back', 'reload failed'))
  eq(r.tone, 'error')
  ok(/previous configuration is (still )?live/i.test(r.message), 'says previous config is live')
  const f = applyToast(res('rollback_failed', 'boom'))
  eq(f.tone, 'error')
  ok(f.message.includes('boom'), 'message shown')
})

test('unknown outcome is an error, never silent', () => {
  const t = applyToast({ applied: false, rolledBack: false, outcome: 'weird' as never, message: 'x' })
  eq(t.tone, 'error')
})

test('HTTP 409 with issues lists the blocking errors; 500 shows the message', () => {
  const t = applyErrorToast(409, 'model has errors', [
    { severity: 'error', where: 'service:s1', message: 'no cert' },
    { severity: 'warning', where: 'service:s2', message: 'meh' },
  ])
  eq(t.tone, 'error')
  ok(t.message.includes('no cert') && !t.message.includes('meh'), 'only error issues listed')
  const s = applyErrorToast(500, 'rollback failed: disk', [])
  eq(s.tone, 'error')
  ok(s.message.includes('rollback failed: disk'), 'message')
  ok(applyErrorToast(409, 'nope', []).message.includes('nope'), '409 without issues keeps message')
})

test('classifyDiff marks added/removed/context/hunk/meta', () => {
  const d = '--- live\n+++ candidate\n@@ -1,3 +1,3 @@\n a\n-b\n+c\n d'
  eq(classifyDiff(d).map(l => l.kind), ['meta', 'meta', 'hunk', 'context', 'removed', 'added', 'context'])
  eq(classifyDiff(d)[4].text, '-b')
  eq(classifyDiff(''), [])
})

test('classifyDiff ignores the trailing newline and keeps a +++ body line honest', () => {
  eq(classifyDiff('@@ -1 +1 @@\n+x\n').length, 2)
})

test('service actions that drop connections need a confirm', () => {
  eq(serviceActionNeedsConfirm('restart', true), true)
  eq(serviceActionNeedsConfirm('restart', false), true)
  eq(serviceActionNeedsConfirm('start', false), true)
  eq(serviceActionNeedsConfirm('start', true), false)
  eq(serviceActionNeedsConfirm('reload', true), false)
})

done('pending')

test('diagnosticNotices: one notice per non-blank line, none when absent', () => {
  eq(diagnosticNotices(undefined).length, 0)
  eq(diagnosticNotices(null).length, 0)
  eq(diagnosticNotices([]).length, 0)
  const n = diagnosticNotices(['  SELinux is enforcing  ', '', 'second'])
  eq(n.length, 2)
  eq(n[0].message, 'SELinux is enforcing')
  eq(n[0].tone, 'notice')
  eq(n[1].message, 'second')
})
