// issues.ts — pure helpers over the /api/model/check issues list.
import type { Issue } from './api'

export interface GroupedIssues {
  byService: Record<string, Issue[]>
  byPort: Record<number, Issue[]>
  other: Issue[]
}

/** Buckets issues by where: "service:<id>", "port:<n>", anything else. */
export function groupIssues(issues: Issue[] | null | undefined): GroupedIssues {
  const out: GroupedIssues = { byService: {}, byPort: {}, other: [] }
  for (const i of issues ?? []) {
    if (i.where.startsWith('service:')) {
      const id = i.where.slice('service:'.length)
      ;(out.byService[id] ??= []).push(i)
    } else if (i.where.startsWith('port:') && Number.isInteger(Number(i.where.slice(5)))) {
      const p = Number(i.where.slice('port:'.length))
      ;(out.byPort[p] ??= []).push(i)
    } else {
      out.other.push(i)
    }
  }
  return out
}

export function issueCounts(issues: Issue[]): { errors: number; warnings: number } {
  let errors = 0
  let warnings = 0
  for (const i of issues) {
    if (i.severity === 'error') errors++
    else warnings++
  }
  return { errors, warnings }
}

export function worstSeverity(issues: Issue[]): 'error' | 'warning' | null {
  if (issues.some(i => i.severity === 'error')) return 'error'
  return issues.length > 0 ? 'warning' : null
}

const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? '' : 's'}`

/** The toast text for a freshly loaded issues list. */
export function issuesSummary(issues: Issue[]): string {
  const { errors, warnings } = issueCounts(issues)
  if (errors === 0 && warnings === 0) return 'No issues found in the saved model.'
  const parts: string[] = []
  if (errors > 0) parts.push(plural(errors, 'error'))
  if (warnings > 0) parts.push(plural(warnings, 'warning'))
  return `${parts.join(' and ')} in the saved model.`
}
