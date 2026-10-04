// globalsForm.ts — pure helpers for the ordered key/value rows of the
// `global` and `defaults` sections (FR-H2).
import type { Directive } from './api'

export interface CommonKey { key: string; value: string; hint: string }

/** Pick-list offered on both the global and defaults groups; `value` is the starting value of an added row. */
export const COMMON_KEYS: CommonKey[] = [
  { key: 'maxconn', value: '2000', hint: 'maximum concurrent connections' },
  { key: 'log', value: '127.0.0.1 local0 notice', hint: 'log target' },
  { key: 'user', value: 'haproxy', hint: 'user to run as' },
  { key: 'group', value: 'haproxy', hint: 'group to run as' },
  { key: 'chroot', value: '/var/lib/haproxy', hint: 'chroot directory' },
  { key: 'nbthread', value: '2', hint: 'worker threads' },
  { key: 'mode', value: 'http', hint: 'http or tcp' },
  { key: 'timeout', value: 'connect 20000ms', hint: 'timeout connect' },
  { key: 'timeout', value: 'client 1h', hint: 'timeout client' },
  { key: 'timeout', value: 'server 1h', hint: 'timeout server' },
  { key: 'timeout', value: 'tunnel 1h', hint: 'timeout tunnel (websockets)' },
]

// Keys a platform driver supplies in its global baseline (FRD §4). The server
// adds them to the generated config unless the model overrides the key, so
// the UI marks them as OS-managed.
const OS_MANAGED = new Set(['user', 'group', 'chroot', 'log', 'stats socket', 'pidfile'])

export function isOsManaged(key: string): boolean {
  return OS_MANAGED.has(key.trim())
}

export function addDirective(list: Directive[], key: string, value: string): Directive[] {
  const k = key.trim()
  if (!k) return list
  return [...list, { key: k, value: value.trim() }]
}

export function updateDirective(list: Directive[], i: number, patch: Partial<Directive>): Directive[] {
  if (i < 0 || i >= list.length) return list
  return list.map((d, idx) => (idx === i ? { ...d, ...patch } : d))
}

export function removeDirective(list: Directive[], i: number): Directive[] {
  return list.filter((_, idx) => idx !== i)
}

/** Moves row i by delta (-1 up, +1 down); out-of-range moves are a no-op. */
export function moveDirective(list: Directive[], i: number, delta: number): Directive[] {
  const j = i + delta
  if (i < 0 || i >= list.length || j < 0 || j >= list.length) return list
  const next = [...list]
  ;[next[i], next[j]] = [next[j], next[i]]
  return next
}
