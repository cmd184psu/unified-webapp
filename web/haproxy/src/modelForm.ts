// modelForm.ts — pure model <-> form-state transformations for the Services
// page. Every function returns a new Model and never mutates its input; the
// UI keeps the draft in local state and only the Save button persists it.
import type { Model, Service, Port } from './api'

export const DEFAULT_UPSTREAM_HOST = '127.0.0.1'
export const DEFAULT_PORT = 443

const effectivePort = (p: number) => (p === 0 ? DEFAULT_PORT : p)
const validPort = (p: number) => Number.isInteger(p) && p >= 1 && p <= 65535

/** Strips response-only fields (imported) and turns null arrays into []. */
export function normalizeModel(raw: Model): Model {
  const r = raw as Model & { imported?: boolean }
  const svc = (s: Service): Service => ({
    ...s,
    fqdns: s.fqdns ?? [],
    extra: s.extra ?? [],
    cert: s.cert ?? { fqdn: '' },
    upstream: s.upstream ?? { host: DEFAULT_UPSTREAM_HOST, port: 0 },
  })
  return {
    version: r.version,
    global: r.global ?? [],
    defaults: r.defaults ?? [],
    defaultService: r.defaultService,
    ports: r.ports ?? [],
    services: (r.services ?? []).map(svc),
    rawSections: r.rawSections ?? [],
  }
}

/** Dirty exactly when the draft differs from the last saved model. */
export function isDirty(saved: Model, draft: Model): boolean {
  return JSON.stringify(normalizeModel(saved)) !== JSON.stringify(normalizeModel(draft))
}

/** The next free service id ("s<N>"), above every id now in the model. */
export function newModelId(m: Model): string {
  let max = 0
  for (const s of m.services) {
    const n = /^s(\d+)$/.exec(s.id)
    if (n) max = Math.max(max, Number(n[1]))
  }
  return `s${max + 1}`
}

function newName(m: Model): string {
  const taken = new Set(m.services.map(s => s.name))
  for (let n = m.services.length + 1; ; n++) {
    if (!taken.has(`service${n}`)) return `service${n}`
  }
}

export function addService(m: Model, port: number): Model {
  const s: Service = {
    id: newModelId(m),
    name: newName(m),
    enabled: true,
    fqdns: [],
    exposedPort: port,
    upstream: { host: DEFAULT_UPSTREAM_HOST, port: 8080 },
    check: true,
    cert: { fqdn: '' },
    extra: [],
  }
  return { ...m, services: [...m.services, s] }
}

/**
 * Patches one service. A rename carries a port default that pointed at the old
 * name; moving the service off a port clears that port's default if it was
 * this service.
 */
export function updateService(m: Model, id: string, patch: Partial<Service>): Model {
  const cur = m.services.find(s => s.id === id)
  if (!cur) return m
  const next: Service = { ...cur, ...patch }
  let ports = m.ports
  const moved = effectivePort(next.exposedPort) !== effectivePort(cur.exposedPort)
  if (next.name !== cur.name || moved) {
    ports = ports.map(p => {
      if (p.defaultService !== cur.name) return p
      if (moved || p.port !== effectivePort(next.exposedPort)) return dropDefault(p)
      return { ...p, defaultService: next.name }
    })
  }
  return { ...m, ports, services: m.services.map(s => (s.id === id ? next : s)) }
}

function dropDefault(p: Port): Port {
  return { port: p.port }
}

export function removeService(m: Model, id: string): Model {
  const cur = m.services.find(s => s.id === id)
  if (!cur) return m
  return {
    ...m,
    ports: m.ports.map(p => (p.defaultService === cur.name ? dropDefault(p) : p)),
    services: m.services.filter(s => s.id !== id),
  }
}

export const setServiceEnabled = (m: Model, id: string, enabled: boolean): Model =>
  updateService(m, id, { enabled })

export function addFqdn(m: Model, id: string, fqdn: string): Model {
  const f = fqdn.trim()
  const s = m.services.find(x => x.id === id)
  if (!s || !f || s.fqdns.some(x => x.toLowerCase() === f.toLowerCase())) return m
  return updateService(m, id, { fqdns: [...s.fqdns, f] })
}

export function setFqdn(m: Model, id: string, index: number, fqdn: string): Model {
  const s = m.services.find(x => x.id === id)
  if (!s || index < 0 || index >= s.fqdns.length) return m
  return updateService(m, id, { fqdns: s.fqdns.map((f, i) => (i === index ? fqdn : f)) })
}

export function removeFqdn(m: Model, id: string, index: number): Model {
  const s = m.services.find(x => x.id === id)
  if (!s) return m
  return updateService(m, id, { fqdns: s.fqdns.filter((_, i) => i !== index) })
}

export function setUpstreamHost(m: Model, id: string, host: string): Model {
  const s = m.services.find(x => x.id === id)
  return s ? updateService(m, id, { upstream: { ...s.upstream, host } }) : m
}

/** Edits the unified default service (443); it is never added or removed. */
export function updateDefaultService(m: Model, patch: Partial<Model['defaultService']>): Model {
  return { ...m, defaultService: { ...m.defaultService, ...patch } }
}

export const extraToText = (extra: string[]): string => extra.join('\n')

export const textToExtra = (text: string): string[] =>
  text.split('\n').map(l => l.trim()).filter(l => l !== '')

/**
 * The model as PUT: trimmed names and FQDNs, blank FQDNs/extra lines dropped,
 * a blank upstream host replaced by the default.
 */
export function finalizeForSave(m: Model): Model {
  return {
    ...m,
    services: m.services.map(s => ({
      ...s,
      name: s.name.trim(),
      fqdns: s.fqdns.map(f => f.trim()).filter(f => f !== ''),
      extra: s.extra.map(l => l.trim()).filter(l => l !== ''),
      cert: { fqdn: (s.cert?.fqdn ?? '').trim() },
      upstream: { ...s.upstream, host: s.upstream.host.trim() || DEFAULT_UPSTREAM_HOST },
    })),
  }
}

export interface PortResult { model: Model; error: string | null }

export function addPort(m: Model, port: number): PortResult {
  if (!validPort(port)) return { model: m, error: 'A port must be a whole number from 1 to 65535.' }
  if (port === DEFAULT_PORT) return { model: m, error: 'Port 443 is always present and serves the default service.' }
  if (m.ports.some(p => p.port === port)) return { model: m, error: `Port ${port} is already listed.` }
  return { model: { ...m, ports: [...m.ports, { port }] }, error: null }
}

export function removePort(m: Model, port: number): PortResult {
  if (m.services.some(s => effectivePort(s.exposedPort) === port)) {
    return { model: m, error: `Port ${port} still has services; move or delete them first.` }
  }
  return { model: { ...m, ports: m.ports.filter(p => p.port !== port) }, error: null }
}

/** Names of the services on a port: the only valid choices for its default. */
export function portDefaultOptions(m: Model, port: number): string[] {
  return m.services.filter(s => effectivePort(s.exposedPort) === port).map(s => s.name)
}

/** Sets (or, with "", clears) a port's default service. */
export function setPortDefault(m: Model, port: number, name: string): PortResult {
  if (name !== '' && !portDefaultOptions(m, port).includes(name)) {
    return { model: m, error: `"${name}" is not one of the services on port ${port}.` }
  }
  const ports = m.ports.some(p => p.port === port) ? m.ports : [...m.ports, { port }]
  return {
    model: { ...m, ports: ports.map(p => (p.port !== port ? p : name === '' ? dropDefault(p) : { ...p, defaultService: name })) },
    error: null,
  }
}

export interface PortGroup {
  port: number
  /** 443: always present, carries the unified default service, not removable. */
  isDefaultPort: boolean
  defaultServiceName: string
  services: Service[]
}

/** Services grouped by exposed port: 443 first, then the rest ascending. */
export function groupByPort(m: Model): PortGroup[] {
  const set = new Set<number>([DEFAULT_PORT])
  for (const p of m.ports) set.add(p.port)
  for (const s of m.services) set.add(effectivePort(s.exposedPort))
  return [...set].sort((a, b) => (a === DEFAULT_PORT ? -1 : b === DEFAULT_PORT ? 1 : a - b)).map(port => ({
    port,
    isDefaultPort: port === DEFAULT_PORT,
    defaultServiceName: port === DEFAULT_PORT ? m.defaultService.name : m.ports.find(p => p.port === port)?.defaultService ?? '',
    services: m.services.filter(s => effectivePort(s.exposedPort) === port),
  }))
}
