// pending.ts — pure view logic for the pending-changes bar (D4, D5): what the
// bar says, the Apply outcome -> toast mapping, and diff line classes.

import type { ApplyResult, Changes, Issue } from './api'

export type Tone = 'success' | 'error' | 'notice'
export interface ToastSpec { tone: Tone; message: string }

export interface PendingView {
  label: string
  summary: string
  applyDisabled: boolean
  applyLabel: string
  canReview: boolean
}

/** null = the first /api/changes answer has not arrived (never claim "No changes" then). */
export function pendingView(c: Changes | null, busy = false): PendingView {
  if (!c) return { label: 'Checking for changes…', summary: '', applyDisabled: true, applyLabel: 'Apply changes', canReview: false }
  if (!c.hasChanges) return { label: 'No changes', summary: '', applyDisabled: true, applyLabel: 'No changes to apply', canReview: false }
  return { label: 'Pending changes', summary: c.summary, applyDisabled: busy, applyLabel: 'Apply changes', canReview: true }
}

/** One status-panel notice per non-blank /api/status diagnostics line (absent = none). */
export function diagnosticNotices(lines: string[] | null | undefined): ToastSpec[] {
  return (lines ?? []).map(l => l.trim()).filter(l => l !== '').map(message => ({ tone: 'notice' as const, message }))
}

/** The toast for an HTTP 200 Apply/Restore answer. */
export function applyToast(r: ApplyResult): ToastSpec {
  switch (r.outcome) {
    case 'applied':
      return { tone: 'success', message: r.message || 'Changes applied.' }
    case 'no_changes':
      return { tone: 'notice', message: r.message || 'Nothing to apply: the live configuration already matches.' }
    case 'validation_failed':
      return { tone: 'error', message: `HAProxy rejected the configuration; nothing was changed. ${r.message}`.trim() }
    case 'rolled_back':
      return { tone: 'error', message: `The new configuration failed to load; the previous configuration is live again. ${r.message}`.trim() }
    case 'rollback_failed':
      return { tone: 'error', message: `The apply failed and the rollback also failed: ${r.message}` }
    default:
      return { tone: 'error', message: `Unexpected apply result "${String(r.outcome)}": ${r.message}` }
  }
}

/** The toast for a non-2xx Apply/Restore answer (409 with issues, 500, others). */
export function applyErrorToast(status: number, message: string, issues: Issue[]): ToastSpec {
  if (status === 409 && issues.length > 0) {
    const errs = issues.filter(i => i.severity === 'error')
    if (errs.length > 0) {
      return { tone: 'error', message: `Apply blocked by ${errs.length} error${errs.length === 1 ? '' : 's'}: ${errs.map(i => `${i.where}: ${i.message}`).join('; ')}` }
    }
  }
  return { tone: 'error', message: status >= 500 ? `Apply failed: ${message}` : message }
}

export type DiffKind = 'added' | 'removed' | 'context' | 'hunk' | 'meta'
export interface DiffLine { kind: DiffKind; text: string }

/** Classifies a unified diff (internal/haproxy/diff.go) line by line. */
export function classifyDiff(diff: string): DiffLine[] {
  if (!diff) return []
  const lines = diff.split('\n')
  if (lines[lines.length - 1] === '') lines.pop()
  let inHunk = false
  return lines.map(text => {
    if (text.startsWith('@@')) { inHunk = true; return { kind: 'hunk' as const, text } }
    if (!inHunk && (text.startsWith('--- ') || text.startsWith('+++ '))) return { kind: 'meta' as const, text }
    if (text.startsWith('+')) return { kind: 'added' as const, text }
    if (text.startsWith('-')) return { kind: 'removed' as const, text }
    return { kind: 'context' as const, text }
  })
}

/** Restart always drops connections; Start does only when the service is not already running. */
export function serviceActionNeedsConfirm(action: 'reload' | 'restart' | 'start', active: boolean): boolean {
  if (action === 'restart') return true
  if (action === 'start') return !active
  return false
}
