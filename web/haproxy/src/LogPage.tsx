import { useEffect, useState } from 'react'
import { showToast } from '@shared'
import { api, OpEntry } from './api'
import { MAX_LOG_ENTRIES, backoffMs, capEntries } from './livefeed'

const key = (e: OpEntry) => `${e.time}|${e.message}`

/** Operations log: snapshot plus a live SSE tail that reconnects with backoff and closes on unmount. */
export function LogPage() {
  const [entries, setEntries] = useState<OpEntry[]>([])
  const [live, setLive] = useState(false)

  useEffect(() => {
    let es: EventSource | null = null
    let timer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    let closed = false

    const snapshot = async () => {
      try {
        const snap = await api.ops()
        if (!closed) setEntries(capEntries(snap, MAX_LOG_ENTRIES))
      } catch (e) {
        showToast(`Could not load the log: ${e instanceof Error ? e.message : String(e)}`, 'error')
      }
    }

    const connect = () => {
      es = new EventSource(api.opsStreamUrl)
      es.onopen = () => { attempt = 0; setLive(true); snapshot() }
      es.onmessage = ev => {
        try {
          const e = JSON.parse(ev.data) as OpEntry
          setEntries(prev => (prev.some(p => key(p) === key(e)) ? prev : capEntries([...prev, e], MAX_LOG_ENTRIES)))
        } catch {
          showToast('A malformed log entry was ignored.', 'error')
        }
      }
      es.onerror = () => {
        setLive(false)
        es?.close()
        if (closed) return
        timer = setTimeout(connect, backoffMs(attempt++))
      }
    }

    snapshot()
    connect()
    return () => { closed = true; if (timer) clearTimeout(timer); es?.close() }
  }, [])

  return (
    <div className="stack">
      <div className="row">
        <span className={`badge ${live ? 'badge-ok' : 'badge-warning'}`}>{live ? 'live' : 'reconnecting…'}</span>
      </div>
      <pre className="rawpre logpre">
        {entries.length === 0 ? '(no operations yet)' : entries.map(e => `${new Date(e.time).toLocaleTimeString()}  ${e.message}`).join('\n')}
      </pre>
    </div>
  )
}
