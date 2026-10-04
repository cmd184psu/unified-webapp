import { test, done, eq, ok } from './testkit'
import type { CertRow, CertMachineCert } from './api'
import {
  expiryBadge, freshnessBadge, certDetailsView, pickerRows, pickerNeedsConfirm,
  pullErrorToast, coverageBadge, freshnessFor, removeButtonState,
} from './certview'

const row = (p: Partial<CertRow> = {}): CertRow => ({
  name: 'a.pem', enabled: true, note: '', certmachine: { id: 1, fqdn: 'a.example.com' }, sha256: 'x', missing: false, ...p,
})

test('expiry badge ok/soon/expired, absent when unknown', () => {
  eq(expiryBadge({ expiry: 'ok' })?.tone, 'ok')
  eq(expiryBadge({ expiry: 'soon' })?.label, 'expires soon')
  eq(expiryBadge({ expiry: 'expired' })?.tone, 'error')
  eq(expiryBadge({}), null)
  eq(expiryBadge(undefined), null)
})

test('freshness badge: update available, up to date, no active cert, unknown', () => {
  eq(freshnessBadge({ name: 'a', fqdn: 'f', status: 'update available' }), { label: 'update available', tone: 'warning', canUpdate: true })
  eq(freshnessBadge({ name: 'a', fqdn: 'f', status: 'up to date' }).canUpdate, false)
  const n = freshnessBadge({ name: 'a', fqdn: 'f', status: 'no active cert' })
  eq(n.label, 'no active cert')
  eq(n.canUpdate, false)
  const u = freshnessBadge({ name: 'a', fqdn: 'f', status: 'unknown', error: 'CertMachine is unreachable' })
  ok(u.label.includes('unknown') && (u.title ?? '').includes('unreachable'), 'unknown carries the reason')
  eq(freshnessBadge(undefined).label, 'freshness not checked')
})

test('freshnessFor finds a row by name', () => {
  eq(freshnessFor([{ name: 'a.pem', fqdn: 'f', status: 'up to date' }], 'a.pem')?.status, 'up to date')
  eq(freshnessFor([], 'a.pem'), undefined)
})

test('details view lists fqdn, SANs, issuer, validity, status', () => {
  const v = certDetailsView(row({
    details: { fqdn: 'a.example.com', sansDns: ['a.example.com', 'b.example.com'], sansIp: ['10.0.0.1'], issuer: 'CA', notBefore: '2026-01-01', notAfter: '2027-01-01', status: 'active', expiry: 'ok' },
  }))
  eq(v.available, true)
  const get = (k: string) => v.fields.find(f => f.label === k)?.value
  eq(get('FQDN'), 'a.example.com')
  eq(get('SANs'), 'a.example.com, b.example.com, 10.0.0.1')
  eq(get('Issuer'), 'CA')
  eq(get('Valid'), '2026-01-01 to 2027-01-01')
  eq(get('Status'), 'active')
})

test('detailsError is shown plainly as details unavailable', () => {
  const v = certDetailsView(row({ detailsError: 'CertMachine is unreachable' }))
  eq(v.available, false)
  eq(v.message, 'details unavailable: CertMachine is unreachable')
  eq(certDetailsView(row()).message, 'details unavailable')
})

const cm = (p: Partial<CertMachineCert>): CertMachineCert => ({ id: 1, fqdn: 'a.example.com', status: 'active', covers: true, ...p })

test('picker shows only covering certs by default; all shows everything flagged', () => {
  const list = [cm({ id: 1 }), cm({ id: 2, covers: false }), cm({ id: 3, status: 'archived' }), cm({ id: 4, status: 'quarantined', covers: false })]
  eq(pickerRows(list, false).map(r => r.cert.id), [1])
  const all = pickerRows(list, true)
  eq(all.map(r => r.cert.id), [1, 2, 3, 4])
  eq(all[0].flags, [])
  eq(all[1].flags, ['does not cover this name'])
  eq(all[2].flags, ['archived'])
  eq(all[3].flags, ['does not cover this name', 'quarantined'])
})

test('picker: with no service fqdn every active cert is listed unflagged', () => {
  eq(pickerRows([cm({ covers: false })], false, false).length, 1)
  eq(pickerRows([cm({ covers: false })], false, false)[0].flags, [])
})

test('choosing a flagged cert needs an explicit confirm; a clean one does not', () => {
  eq(pickerNeedsConfirm(pickerRows([cm({})], true)[0]), false)
  eq(pickerNeedsConfirm(pickerRows([cm({ covers: false })], true)[0]), true)
  eq(pickerNeedsConfirm(pickerRows([cm({ status: 'expired' })], true)[0]), true)
})

test('pull errors map to error toasts carrying the server message', () => {
  eq(pullErrorToast(502, 'CertMachine is unreachable'), { tone: 'error', message: 'Could not install the certificate: CertMachine is unreachable' })
  eq(pullErrorToast(409, 'refused: not active').tone, 'error')
  eq(pullErrorToast(409, 'CertMachine is not configured').message, 'CertMachine is not configured. Set certmachine.url in the haproxy settings.')
  eq(pullErrorToast(500, 'boom').tone, 'error')
})

test('coverage badge: covered / warning lists uncovered / unknown', () => {
  eq(coverageBadge({ service: 's', certFqdn: 'c', covered: ['a'], uncovered: [], status: 'covered' })!.tone, 'ok')
  const w = coverageBadge({ service: 's', certFqdn: 'c', covered: [], uncovered: ['x.example.com'], status: 'warning' })!
  eq(w.tone, 'warning')
  ok((w.title ?? '').includes('x.example.com'), 'names the uncovered')
  const u = coverageBadge({ service: 's', certFqdn: 'c', covered: [], uncovered: [], status: 'unknown' })!
  eq(u.label, 'coverage unknown')
  eq(coverageBadge(undefined), null)
})

test('remove button: enabled only when the server says removable', () => {
  eq(removeButtonState(row({ removable: true, inUseReason: '' })), { disabled: false, title: 'Remove this certificate' })
})

test('remove button: disabled with the server reason while in use', () => {
  for (const r of ['Listed in the live configuration. Disable it, then Apply, before removing it.', 'Enabled and used by service app.', 'Could not read the live configuration.']) {
    eq(removeButtonState(row({ removable: false, inUseReason: r })), { disabled: true, title: r })
  }
})

test('remove button: a missing removable field fails safe (disabled)', () => {
  const s = removeButtonState(row())
  eq(s.disabled, true)
  ok(s.title.length > 0, 'has a title')
})

done('certview')
