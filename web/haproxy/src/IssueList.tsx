import { showToast } from '@shared'
import { api, type Issue } from './api'

/** Asks the app to reload everything after an issue was fixed in place. */
const REFRESH_EVENT = 'haproxy-refresh'
export const onRefreshRequest = (fn: () => void) => {
  window.addEventListener(REFRESH_EVENT, fn)
  return () => window.removeEventListener(REFRESH_EVENT, fn)
}

const adoptFile = async (name: string) => {
  try {
    await api.importCerts([name])
    showToast(`${name} added. Run Check to test it.`, 'success')
  } catch {
    showToast('Could not add that certificate just now.', 'notice')
  }
  window.dispatchEvent(new Event(REFRESH_EVENT))
}

/** Inline display of the issues that concern one thing (a service, a port, the model). */
export function IssueList({ issues }: { issues: Issue[] | undefined }) {
  if (!issues || issues.length === 0) return null
  return (
    <ul className="issues">
      {issues.map((i, n) => (
        <li key={n} className={`issue issue-${i.severity}`}>
          <span className="issue-tag" title={i.severity} aria-label={i.severity}>{i.severity === 'error' ? '✕' : '⚠'}</span>
          <span className="issue-msg">{i.message}</span>
          {(i.suggest ?? []).map(f => (
            <button key={f} className="chip" title="Add this certificate" onClick={() => adoptFile(f)}>＋ {f}</button>
          ))}
        </li>
      ))}
    </ul>
  )
}
