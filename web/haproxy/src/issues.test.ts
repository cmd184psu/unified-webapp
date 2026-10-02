import { test, done, eq, ok } from './testkit'
import type { Issue } from './api'
import { groupIssues, issueCounts, issuesSummary, worstSeverity } from './issues'

const issues: Issue[] = [
  { severity: 'error', where: 'service:s1', message: 'duplicate service name "a"' },
  { severity: 'warning', where: 'service:s1', message: 'cert disabled' },
  { severity: 'error', where: 'service:s2', message: 'no tracked certificate' },
  { severity: 'error', where: 'port:8443', message: 'default service not on port' },
  { severity: 'error', where: 'model', message: 'something global' },
]

test('groupIssues buckets by service id, by port, and the rest', () => {
  const g = groupIssues(issues)
  eq(g.byService['s1'].length, 2)
  eq(g.byService['s2'].length, 1)
  eq(g.byPort[8443].length, 1)
  eq(g.other.length, 1)
})

test('groupIssues on empty / null input is empty', () => {
  const g = groupIssues([])
  eq(g.byService, {})
  eq(g.byPort, {})
  eq(g.other, [])
  eq(groupIssues(null as unknown as Issue[]).other, [])
})

test('issueCounts and worstSeverity', () => {
  eq(issueCounts(issues), { errors: 4, warnings: 1 })
  eq(worstSeverity(issues.slice(1, 2)), 'warning')
  eq(worstSeverity(issues), 'error')
  eq(worstSeverity([]), null)
})

test('issuesSummary words every case (used for the toast)', () => {
  eq(issuesSummary([]), 'No issues found in the saved model.')
  ok(issuesSummary(issues).includes('4 errors') && issuesSummary(issues).includes('1 warning'), 'counts shown')
  eq(issuesSummary(issues.slice(1, 2)), '1 warning in the saved model.')
  eq(issuesSummary(issues.slice(0, 1)), '1 error in the saved model.')
})

done('issues')
