import { useCallback, useEffect, useState } from 'react'
import { showToast } from '@shared'
import { api, RawConfig } from './api'

/** FR-H7: read-only view of exactly what Apply would write. */
export function RawPage({ refreshKey }: { refreshKey: number }) {
  const [raw, setRaw] = useState<RawConfig | null>(null)
  const [error, setError] = useState<string | null>(null)
  const load = useCallback(async () => {
    try {
      setRaw(await api.raw())
      setError(null)
    } catch (e) {
      const m = e instanceof Error ? e.message : String(e)
      setError(m)
      showToast(`Could not load the raw configuration: ${m}`, 'error')
    }
  }, [])
  useEffect(() => { load() }, [load, refreshKey])
  return (
    <div className="stack">
      <div className="row">
        <span className="muted" title="Exactly what Apply would write">read-only</span>
        <span className="grow" />
        <button className="btn btn-ghost btn-sm" onClick={async () => { await load(); showToast('Raw view refreshed.', 'notice') }}>Refresh</button>
      </div>
      {error && <p className="error">{error}</p>}
      {raw && (
        <>
          <div className="card"><div className="card-title">haproxy.cfg</div><pre className="rawpre">{raw.config}</pre></div>
          <div className="card"><div className="card-title">crt-list</div><pre className="rawpre">{raw.crtList || '(empty)'}</pre></div>
        </>
      )}
    </div>
  )
}
