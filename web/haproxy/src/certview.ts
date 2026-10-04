// certview.ts — pure view logic for the Certificates tab, the CertMachine
// picker and the coverage badges.

import type { CertMachineCert, CertRow, CoverageResult, Freshness } from './api'
import type { ToastSpec } from './pending'

export type BadgeTone = 'ok' | 'warning' | 'error' | 'muted'
export interface Badge { label: string; tone: BadgeTone; title?: string }

export function expiryBadge(d: { expiry?: string } | undefined): Badge | null {
  switch (d?.expiry) {
    case 'ok': return { label: 'valid', tone: 'ok' }
    case 'soon': return { label: 'expires soon', tone: 'warning' }
    case 'expired': return { label: 'expired', tone: 'error' }
    default: return null
  }
}

export function freshnessFor(list: Freshness[], name: string): Freshness | undefined {
  return list.find(f => f.name === name)
}

export function freshnessBadge(f: Freshness | undefined): Badge & { canUpdate: boolean } {
  if (!f) return { label: 'freshness not checked', tone: 'muted', canUpdate: false }
  switch (f.status) {
    case 'update available': return { label: 'update available', tone: 'warning', canUpdate: true }
    case 'up to date': return { label: 'up to date', tone: 'ok', canUpdate: false }
    case 'no active cert': return { label: 'no active cert', tone: 'muted', title: 'CertMachine has no active certificate for this name', canUpdate: false }
    default: return { label: 'freshness unknown', tone: 'muted', title: f.error, canUpdate: false }
  }
}

export interface DetailsView {
  available: boolean
  message: string
  fields: { label: string; value: string }[]
}

export function certDetailsView(row: CertRow): DetailsView {
  const d = row.details
  if (!d) {
    return { available: false, message: row.detailsError ? `details unavailable: ${row.detailsError}` : 'details unavailable', fields: [] }
  }
  const sans = [...(d.sansDns ?? []), ...(d.sansIp ?? [])]
  const fields: { label: string; value: string }[] = []
  const add = (label: string, value: string | undefined) => { if (value) fields.push({ label, value }) }
  add('FQDN', d.fqdn)
  add('SANs', sans.join(', '))
  add('Issuer', d.issuer)
  if (d.notBefore || d.notAfter) add('Valid', `${d.notBefore ?? '?'} to ${d.notAfter ?? '?'}`)
  add('Status', d.status)
  return { available: true, message: '', fields }
}

export interface PickerRow { cert: CertMachineCert; flags: string[] }

/** Covering certs only by default; with showAll everything, flagged. hasFqdn=false: nothing to cover-check. */
export function pickerRows(certs: CertMachineCert[], showAll: boolean, hasFqdn = true): PickerRow[] {
  const rows: PickerRow[] = []
  for (const cert of certs) {
    const flags: string[] = []
    if (hasFqdn && !cert.covers) flags.push('does not cover this name')
    if (cert.status && cert.status !== 'active') flags.push(cert.status)
    if (!showAll && flags.length > 0) continue
    rows.push({ cert, flags })
  }
  return rows
}

export function pickerNeedsConfirm(r: PickerRow): boolean {
  return r.flags.length > 0
}

export function pullErrorToast(status: number, message: string): ToastSpec {
  if (status === 409 && /not configured/i.test(message)) {
    return { tone: 'error', message: 'CertMachine is not configured. Set certmachine.url in the haproxy settings.' }
  }
  return { tone: 'error', message: `Could not install the certificate: ${message}` }
}

export function coverageBadge(c: CoverageResult | undefined): Badge | null {
  if (!c) return null
  switch (c.status) {
    case 'covered': return { label: 'cert covers all names', tone: 'ok' }
    case 'warning': return { label: 'cert does not cover all names', tone: 'warning', title: `Not covered: ${(c.uncovered ?? []).join(', ')}` }
    default: return { label: 'coverage unknown', tone: 'muted', title: 'CertMachine could not be reached to check coverage' }
  }
}

/** Remove button state. A missing `removable` is treated as in use so a stale or older server fails safe. */
export function removeButtonState(c: CertRow): { disabled: boolean; title: string } {
  if (c.removable === true) return { disabled: false, title: 'Remove this certificate' }
  return { disabled: true, title: c.inUseReason || 'This certificate is in use.' }
}
