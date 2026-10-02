// api.ts — typed client for every route in internal/haproxy/handler.go.
//
// Every non-2xx answer throws an ApiError carrying the server's own message
// ({"error": "..."}) and, for the 409 an Apply gets while the model has
// errors, the issues list. Callers surface it; nothing is swallowed here.
// Secrets never reach the browser (the API key is not in any response).

// ---- model (internal/haproxy/model.go) ------------------------------------

export interface Directive { key: string; value: string }
export interface Upstream { host: string; port: number }
export interface DefaultService { name: string; upstream: Upstream; check: boolean }
export interface Port { port: number; defaultService?: string }
export interface CertRef { fqdn: string }

export interface Service {
  id: string
  name: string
  enabled: boolean
  fqdns: string[]
  exposedPort: number
  upstream: Upstream
  check: boolean
  cert: CertRef
  extra: string[]
}

export interface RawSection { header: string; lines: string[] }

export interface Model {
  version: number
  global: Directive[]
  defaults: Directive[]
  defaultService: DefaultService
  ports: Port[]
  services: Service[]
  rawSections: RawSection[]
}

/** GET /api/model: the Model plus whether a state.json exists yet. */
export interface ModelResponse extends Model { imported: boolean }

// ---- referential checks (refcheck.go) --------------------------------------

export type IssueSeverity = 'error' | 'warning'
export interface Issue { severity: IssueSeverity; where: string; message: string }
export interface CheckModelResponse { issues: Issue[] }

// ---- status, changes, apply ------------------------------------------------

export type Outcome = 'applied' | 'no_changes' | 'validation_failed' | 'rolled_back' | 'rollback_failed'

export interface ApplyResult {
  applied: boolean
  rolledBack: boolean
  outcome: Outcome
  message: string
}

export interface LastApply { time: string; outcome: Outcome; message: string }

export interface Status {
  module: string
  moduleVersion: string
  service: { active: boolean; detail: string }
  version: string
  lastApply: LastApply | null
  pending: boolean
  certmachineConfigured: boolean
  /** Plain-text, read-only OS conditions the operator should know about; empty when healthy. */
  diagnostics: string[]
}

export interface Changes {
  hasChanges: boolean
  configDiff: string
  crtListDiff: string
  summary: string
}

export interface CheckResult { ok: boolean; message: string }
export interface StatRow {
  proxy: string
  server: string
  kind: 'frontend' | 'backend' | 'server' | 'listener'
  status: string
  sessionsCur: number
  sessionRate: number
  bytesIn: number
  bytesOut: number
  checkStatus: string
  lastCheck: string
}
export interface StatsInfo { version: string; uptimeSec: number; currConns: number; pid: number }
/** GET /api/stats: available:false (HTTP 200) when the stats socket is missing or unreachable. */
export interface StatsResponse { available: boolean; message: string; info: StatsInfo; rows: StatRow[] }

export interface RawConfig { config: string; crtList: string }

export interface ImportReport { [k: string]: unknown }
export interface ImportResponse { model: Model; report: ImportReport }

// ---- backups, ops ----------------------------------------------------------

export interface Backup { name: string; kind: string; timestamp: string; orig: boolean }
export interface BackupContent { name: string; content: string }
export interface OpEntry { time: string; message: string }

// ---- certs -----------------------------------------------------------------

export type ExpiryState = 'ok' | 'soon' | 'expired'
export interface CertDetails {
  fqdn?: string
  sansDns?: string[]
  sansIp?: string[]
  notBefore?: string
  notAfter?: string
  status?: string
  issuer?: string
  expiry?: ExpiryState
}

/** One tracking row from GET /api/certs (certs.go CertManaged + details). */
export interface CertRow {
  name: string
  enabled: boolean
  note: string
  certmachine: { id: number; fqdn: string }
  sha256: string
  superseded?: boolean
  missing: boolean
  details?: CertDetails
  detailsError?: string
  /** false while the cert is in use; a missing field is treated as not removable. */
  removable?: boolean
  /** Plain-text reason shown while removable is false. */
  inUseReason?: string
}

export interface CertsResponse { certs: CertRow[]; unmanaged: number }

export interface CertMachineCert {
  id: number
  fqdn: string
  status?: string
  covers: boolean
  [k: string]: unknown
}

export interface CoverageResult {
  service: string
  certFqdn: string
  covered: string[] | null
  uncovered: string[] | null
  status: 'covered' | 'warning' | 'unknown'
}
export interface Freshness { name: string; fqdn: string; status: string; error?: string }

// ---- transport -------------------------------------------------------------

export class ApiError extends Error {
  status: number
  issues: Issue[]
  constructor(message: string, status: number, issues: Issue[] = []) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.issues = issues
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    headers: {
      Accept: 'application/json',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try { data = JSON.parse(text) } catch { data = null }
  }
  if (!res.ok) {
    const obj = (data ?? {}) as { error?: string; issues?: Issue[] }
    throw new ApiError(obj.error || text.trim() || `${method} ${path}: HTTP ${res.status}`, res.status, obj.issues ?? [])
  }
  return data as T
}

const enc = encodeURIComponent

export const api = {
  status: () => request<Status>('GET', '/api/status'),

  getModel: () => request<ModelResponse>('GET', '/api/model'),
  /** The only function that writes the model. Called from the Save button path only. */
  saveModel: (m: Model) => request<{ ok: boolean }>('PUT', '/api/model', m),
  checkModel: () => request<CheckModelResponse>('GET', '/api/model/check'),
  importLive: (commit: boolean) => request<ImportResponse>('POST', '/api/import', { commit }),

  changes: () => request<Changes>('GET', '/api/changes'),
  stats: () => request<StatsResponse>('GET', '/api/stats'),
  raw: () => request<RawConfig>('GET', '/api/raw'),
  check: () => request<CheckResult>('POST', '/api/check'),
  apply: () => request<ApplyResult>('POST', '/api/apply'),
  reload: () => request<{ ok: boolean }>('POST', '/api/reload'),
  restart: () => request<{ ok: boolean }>('POST', '/api/restart'),
  start: () => request<{ ok: boolean }>('POST', '/api/start'),

  backups: () => request<Backup[]>('GET', '/api/backups'),
  backup: (name: string) => request<BackupContent>('GET', `/api/backups/${enc(name)}`),
  restoreBackup: (name: string) => request<ApplyResult>('POST', `/api/backups/${enc(name)}/restore`),

  ops: () => request<OpEntry[]>('GET', '/api/ops'),
  /** SSE endpoint; open with `new EventSource(api.opsStreamUrl)`. */
  opsStreamUrl: '/api/ops/stream',

  certs: () => request<CertsResponse>('GET', '/api/certs'),
  certMachineCerts: (forFqdn?: string, all = false) => {
    const q = new URLSearchParams()
    if (forFqdn) q.set('forFqdn', forFqdn)
    if (all) q.set('all', '1')
    const qs = q.toString()
    return request<{ certs: CertMachineCert[] }>('GET', `/api/certmachine/certs${qs ? `?${qs}` : ''}`)
  },
  pullCert: (certmachineId: number, note: string) =>
    request<{ name: string; superseded: string[] }>('POST', '/api/certs/pull', { certmachineId, note }),
  setCertEnabled: (name: string, enabled: boolean) =>
    request<{ ok: boolean }>('PUT', `/api/certs/${enc(name)}/enabled`, { enabled }),
  deleteCert: (name: string) => request<{ ok: boolean }>('DELETE', `/api/certs/${enc(name)}`),
  coverage: () => request<CoverageResult[]>('GET', '/api/coverage'),
  freshness: () => request<Freshness[]>('GET', '/api/certs/freshness'),
}
