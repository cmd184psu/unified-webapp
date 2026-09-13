import { useEffect, useRef, useState } from 'react'

interface LogEntry {
  time: string
  message: string
}

function LogPanel({ title, icon, url }: { title: string; icon: string; url: string }) {
  const [lines, setLines] = useState<LogEntry[]>([])
  const [connected, setConnected] = useState(false)
  const boxRef = useRef<HTMLDivElement>(null)
  const stickToBottom = useRef(true)

  useEffect(() => {
    const es = new EventSource(url)
    es.onopen = () => setConnected(true)
    es.onerror = () => setConnected(false)
    es.onmessage = ev => {
      try {
        const entry: LogEntry = JSON.parse(ev.data)
        setLines(prev => {
          const next = [...prev, entry]
          return next.length > 1000 ? next.slice(next.length - 1000) : next
        })
      } catch {
        // ignore malformed event
      }
    }
    return () => es.close()
  }, [url])

  useEffect(() => {
    const el = boxRef.current
    if (el && stickToBottom.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [lines])

  const onScroll = () => {
    const el = boxRef.current
    if (!el) return
    stickToBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40
  }

  return (
    <div className="card log-panel">
      <div className="card-title">
        {icon} {title}
        <span
          className={`status-dot${connected ? '' : ' error'}`}
          style={{ marginLeft: 'auto' }}
          title={connected ? 'live' : 'disconnected'}
        />
      </div>
      <div className="log-box" ref={boxRef} onScroll={onScroll}>
        {lines.length === 0 ? (
          <div className="text-muted">Waiting for log output…</div>
        ) : (
          lines.map((l, i) => (
            <div className="log-line" key={i}>
              <span className="log-time">{new Date(l.time).toLocaleTimeString()}</span> {l.message}
            </div>
          ))
        )}
      </div>
    </div>
  )
}

export function LogsPage() {
  return (
    <div>
      <div className="page-header">
        <div className="page-title">Logs</div>
        <div className="page-subtitle">
          Live tail of smbed's own operations (config writes, backups, restarts) and the Samba
          daemon's own log.
        </div>
      </div>

      <LogPanel title="smbed operations" icon="🛠" url="/api/logs/ops/stream" />
      <LogPanel title="Samba (smbd) log" icon="📄" url="/api/logs/samba/stream" />
    </div>
  )
}
