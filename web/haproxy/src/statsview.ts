// statsview.ts — pure view helpers for the read-only Stats tab.
import type { StatRow, StatsResponse } from './api'

export type StatusClass = 'up' | 'down' | 'nocheck' | 'maint' | 'other'

export function classifyStatus(r: StatRow): StatusClass {
  const s = r.status.trim().toUpperCase()
  if (s === 'NO CHECK') return 'nocheck'
  if (s.startsWith('MAINT')) return 'maint'
  if (s.startsWith('DOWN')) return 'down'
  if (s.startsWith('UP') || s === 'OPEN') return 'up'
  return 'other'
}

export interface ProxyGroup { proxy: string; rows: StatRow[] }

const RANK: Record<string, number> = { frontend: 0, listener: 1, server: 2, backend: 3 }

/** Group rows by proxy (first-seen order); within one: frontend, listeners, servers, backend. */
export function groupByProxy(rows: StatRow[]): ProxyGroup[] {
  const groups: ProxyGroup[] = []
  const byName = new Map<string, ProxyGroup>()
  for (const r of rows) {
    let g = byName.get(r.proxy)
    if (!g) { g = { proxy: r.proxy, rows: [] }; byName.set(r.proxy, g); groups.push(g) }
    g.rows.push(r)
  }
  for (const g of groups) g.rows = g.rows.map((r, i) => ({ r, i })).sort((a, b) => (RANK[a.r.kind] ?? 9) - (RANK[b.r.kind] ?? 9) || a.i - b.i).map(x => x.r)
  return groups
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${Math.max(0, n)} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB']
  let v = n / 1024
  let u = 0
  while (v >= 1024 && u < units.length - 1) { v /= 1024; u++ }
  return `${v.toFixed(1)} ${units[u]}`
}

export function formatUptime(sec: number): string {
  const s = Math.max(0, Math.floor(sec))
  const d = Math.floor(s / 86400)
  const h = Math.floor((s % 86400) / 3600)
  const m = Math.floor((s % 3600) / 60)
  if (d > 0) return `${d}d ${h}h ${m}m`
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s % 60}s`
  return `${s}s`
}

export type StatsView =
  | { kind: 'loading'; message: string }
  | { kind: 'unavailable'; message: string }
  | { kind: 'ready'; message: string }

export function statsViewState(r: StatsResponse | null): StatsView {
  if (!r) return { kind: 'loading', message: 'Loading statistics...' }
  if (!r.available) {
    return { kind: 'unavailable', message: r.message.trim() || 'HAProxy statistics are not available. HAProxy may not have been applied yet, or it is stopped.' }
  }
  return { kind: 'ready', message: '' }
}
