import { useCallback, useEffect, useRef, useState } from 'react'
import { showToast } from '@shared'
import { api, StatsResponse } from './api'
import { POLL_MS, shouldPoll } from './livefeed'
import { classifyStatus, formatBytes, formatUptime, groupByProxy, statsViewState } from './statsview'

/** Read-only HAProxy statistics. The only control is Refresh; polls every 5s while the tab is visible. */
export function StatsPage() {
  const [data, setData] = useState<StatsResponse | null>(null)
  const inFlight = useRef(false)

  const load = useCallback(async () => {
    if (inFlight.current) return
    inFlight.current = true
    try {
      setData(await api.stats())
    } catch (e) {
      showToast(`Could not load statistics: ${e instanceof Error ? e.message : String(e)}`, 'error')
    } finally {
      inFlight.current = false
    }
  }, [])

  useEffect(() => {
    load()
    const tick = () => { if (shouldPoll(document.hidden, inFlight.current)) load() }
    const id = setInterval(tick, POLL_MS)
    const onVisible = () => { if (!document.hidden) tick() }
    document.addEventListener('visibilitychange', onVisible)
    return () => { clearInterval(id); document.removeEventListener('visibilitychange', onVisible) }
  }, [load])

  const view = statsViewState(data)
  return (
    <div className="stack">
      <div className="row">
        <span className="muted">Read-only live statistics from HAProxy.</span>
        <span className="grow" />
        <button className="btn btn-ghost btn-sm" onClick={async () => { await load(); showToast('Statistics refreshed.', 'notice') }}>Refresh</button>
      </div>
      {view.kind !== 'ready' && <p className="muted">{view.message}</p>}
      {data && view.kind === 'ready' && (
        <>
          <div className="card">
            <span>Version <strong>{data.info.version}</strong></span>
            {' · '}<span>Uptime <strong>{formatUptime(data.info.uptimeSec)}</strong></span>
            {' · '}<span>Current connections <strong>{data.info.currConns}</strong></span>
          </div>
          <div className="card">
            <table className="stats-table">
              <thead>
                <tr><th>Proxy</th><th>Name</th><th>Status</th><th>Sessions</th><th>Rate</th><th>In</th><th>Out</th><th>Last check</th></tr>
              </thead>
              <tbody>
                {groupByProxy(data.rows).flatMap(g => g.rows.map((r, i) => {
                  const cls = classifyStatus(r)
                  return (
                    <tr key={`${g.proxy}/${r.kind}/${r.server}/${i}`}>
                      <td>{i === 0 ? g.proxy : ''}</td>
                      <td>{r.server || r.kind}</td>
                      <td><span className={`badge stat-${cls}`}>{r.status}</span></td>
                      <td>{r.sessionsCur}</td>
                      <td>{r.sessionRate}/s</td>
                      <td>{formatBytes(r.bytesIn)}</td>
                      <td>{formatBytes(r.bytesOut)}</td>
                      <td>{r.checkStatus ? `${r.checkStatus}${r.lastCheck ? `: ${r.lastCheck}` : ''}` : ''}</td>
                    </tr>
                  )
                }))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  )
}
