import type { Issue } from './api'

/** Inline display of the issues that concern one thing (a service, a port, the model). */
export function IssueList({ issues }: { issues: Issue[] | undefined }) {
  if (!issues || issues.length === 0) return null
  return (
    <ul className="issues">
      {issues.map((i, n) => (
        <li key={n} className={`issue issue-${i.severity}`}>
          <span className="issue-tag">{i.severity === 'error' ? 'Error' : 'Warning'}</span> {i.message}
        </li>
      ))}
    </ul>
  )
}
