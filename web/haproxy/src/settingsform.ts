// settingsform.ts — pure logic for the Settings tab: form state, dirty
// detection, the client-side mirror of the cheap server validations, and the
// PUT payload. The server stays the authority (it also checks the CA file).
import type { SettingsPayload, SettingsValues } from './api'

/** Form values: numbers are kept as the typed text until Save. */
export interface FormValues {
  os: string
  certmachineUrl: string
  certmachineCaFile: string
  configPath: string
  certsDir: string
  crtListPath: string
  statsSocketPath: string
  backupDir: string
  serviceName: string
  backupKeep: string
  expiryWarnDays: string
}

export interface FormState {
  values: FormValues
  /** A new key typed by the user; '' means keep the stored one. */
  apiKey: string
  clearApiKey: boolean
}

export type FieldErrors = Partial<Record<keyof FormValues | 'apiKey', string>>

export function fromEffective(e: SettingsValues): FormState {
  return {
    values: { ...e, backupKeep: String(e.backupKeep), expiryWarnDays: String(e.expiryWarnDays) },
    apiKey: '',
    clearApiKey: false,
  }
}

export function isDirty(saved: FormState, draft: FormState): boolean {
  return JSON.stringify(saved) !== JSON.stringify(draft)
}

const SERVICE_RE = /^[A-Za-z0-9_.@-]+$/
const LOOPBACK = /^(localhost|127(\.\d{1,3}){3}|\[::1\])$/i

function pathError(label: string, p: string): string | undefined {
  if (p === '') return undefined
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(p)) return `The ${label} must not contain control characters.`
  if (!p.startsWith('/')) return `The ${label} must be an absolute path (start with /).`
  if (p.split('/').includes('..')) return `The ${label} must not contain "..".`
  return undefined
}

function urlError(raw: string): string | undefined {
  if (raw === '') return undefined
  let u: URL
  try { u = new URL(raw) } catch { return 'The CertMachine URL is not a valid address; use something like https://certmachine.example.com.' }
  if (u.protocol === 'https:') return undefined
  if (u.protocol === 'http:') {
    return LOOPBACK.test(u.hostname) ? undefined : 'The CertMachine URL uses plain http; https is required for a non-loopback address.'
  }
  return `The CertMachine URL scheme "${u.protocol.replace(':', '')}" is not supported (use https).`
}

function intError(label: string, raw: string, lo: number, hi: number, unit: string): string | undefined {
  const n = Number(raw)
  if (raw.trim() === '' || !Number.isInteger(n) || n < lo || n > hi) return `The ${label} must be between ${lo} and ${hi}${unit}.`
  return undefined
}

export function validate(s: FormState): FieldErrors {
  const v = s.values
  const errs: FieldErrors = {}
  const set = (k: keyof FormValues | 'apiKey', m: string | undefined) => { if (m) errs[k] = m }
  set('certmachineUrl', urlError(v.certmachineUrl.trim()))
  set('configPath', pathError('config path', v.configPath.trim()))
  set('certsDir', pathError('certs directory', v.certsDir.trim()))
  set('crtListPath', pathError('crt-list path', v.crtListPath.trim()))
  set('statsSocketPath', pathError('stats socket path', v.statsSocketPath.trim()))
  set('backupDir', pathError('backup directory', v.backupDir.trim()))
  const svc = v.serviceName.trim()
  if (svc !== '' && !SERVICE_RE.test(svc)) errs.serviceName = 'The service name may only contain letters, digits and . _ @ -'
  set('certmachineCaFile', pathError('CA file path', v.certmachineCaFile.trim()))
  set('backupKeep', intError('backup count', v.backupKeep, 1, 100, ''))
  set('expiryWarnDays', intError('expiry warning', v.expiryWarnDays, 1, 365, ' days'))
  if (s.apiKey !== '' && s.clearApiKey) errs.apiKey = 'Give a new API key or clear the stored one, not both.'
  return errs
}

export function hasErrors(e: FieldErrors): boolean {
  return Object.keys(e).length > 0
}

export function buildPayload(s: FormState): SettingsPayload {
  const v = s.values
  const t = (x: string) => x.trim()
  return {
    os: t(v.os), certmachineUrl: t(v.certmachineUrl), certmachineCaFile: t(v.certmachineCaFile),
    configPath: t(v.configPath), certsDir: t(v.certsDir), crtListPath: t(v.crtListPath),
    statsSocketPath: t(v.statsSocketPath), backupDir: t(v.backupDir), serviceName: t(v.serviceName),
    backupKeep: Number(v.backupKeep), expiryWarnDays: Number(v.expiryWarnDays),
    apiKey: s.apiKey, clearApiKey: s.clearApiKey,
  }
}

/** The state of the API key line: what is stored, and what Save will do. */
export function apiKeyLabel(apiKeySet: boolean, s: FormState): string {
  if (s.apiKey !== '') return 'Will be replaced on Save'
  if (s.clearApiKey) return 'Will be cleared on Save'
  return apiKeySet ? 'Set' : 'Not set'
}
