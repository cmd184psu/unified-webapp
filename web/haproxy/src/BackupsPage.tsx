import { useCallback, useEffect, useState } from 'react'
import { showToast, confirmDialog } from '@shared'
import { api, Backup } from './api'

interface Props {
  refreshKey: number
  onRestore: (name: string) => Promise<void>
}

export function BackupsPage({ refreshKey, onRestore }: Props) {
  const [list, setList] = useState<Backup[] | null>(null)
  const [view, setView] = useState<{ name: string; content: string } | null>(null)

  const load = useCallback(async () => {
    try {
      setList(await api.backups())
    } catch (e) {
      console.warn('backups unavailable', e)
      setList([])
    }
  }, [])
  useEffect(() => { load() }, [load, refreshKey])

  const open = async (name: string) => {
    try {
      setView(await api.backup(name))
    } catch (e) {
      showToast(`Could not read ${name}: ${e instanceof Error ? e.message : String(e)}`, 'error')
    }
  }

  const restore = async (name: string) => {
    if (!(await confirmDialog(`Restore ${name}? It becomes the live configuration (validated first; the current one is backed up).`, { confirmLabel: 'Restore' }))) {
      showToast('Restore canceled.', 'notice')
      return
    }
    await onRestore(name)
  }

  return (
    <div className="stack">
      {list && list.length === 0 && <p className="muted">No backups yet. One is made every time you Apply.</p>}
      {list && list.map(b => (
        <div className="card row" key={b.name}>
          <span className="grow">{b.name}</span>
          <span className="badge">{b.kind}</span>
          {b.orig && <span className="badge">original</span>}
          <span className="muted">{b.timestamp}</span>
          <button className="btn btn-ghost btn-sm" onClick={() => open(b.name)}>View</button>
          <button className="btn btn-danger btn-sm" onClick={() => restore(b.name)}>Restore</button>
        </div>
      ))}
      {view && (
        <div className="card">
          <div className="card-title row"><span className="grow">{view.name}</span>
            <button className="btn btn-ghost btn-sm" onClick={() => setView(null)}>Close</button></div>
          <pre className="rawpre">{view.content}</pre>
        </div>
      )}
    </div>
  )
}
