import { useEffect, useState } from 'react'
import { api, FolderEntry } from './api'

interface Props {
  onSelect: (folder: FolderEntry) => void
  onClose: () => void
}

export function FolderPicker({ onSelect, onClose }: Props) {
  const [folders, setFolders] = useState<FolderEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [selected, setSelected] = useState<FolderEntry | null>(null)

  useEffect(() => {
    api.getFolders()
      .then(setFolders)
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  // Close on Escape
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [onClose])

  return (
    <div className="modal-overlay" onClick={e => { if (e.target === e.currentTarget) onClose() }}>
      <div className="modal" role="dialog" aria-modal="true" aria-label="Pick a folder">
        <div className="modal-header">
          <span className="modal-title">📁 Pick a folder from /opt</span>
          <button className="btn btn-icon" onClick={onClose}>×</button>
        </div>

        <div className="modal-body">
          {loading && <div className="text-muted" style={{ padding: '20px 0', textAlign: 'center' }}>Loading…</div>}
          {error && <div className="text-red">{error}</div>}
          {!loading && !error && folders.length === 0 && (
            <div className="empty-state">
              <div className="empty-state-icon">🗂</div>
              <div className="empty-state-text">/opt is empty</div>
              <div className="empty-state-sub">No subdirectories found</div>
            </div>
          )}
          {!loading && !error && (
            <div className="folder-list">
              {folders.map(f => (
                <div
                  key={f.path}
                  className={`folder-item${selected?.path === f.path ? ' selected' : ''}`}
                  onClick={() => setSelected(f)}
                  onDoubleClick={() => { onSelect(f); onClose() }}
                >
                  <span className="folder-icon">📂</span>
                  <span style={{ flex: 1 }}>{f.name}</span>
                  <span className="text-muted" style={{ fontSize: 10 }}>{f.path}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <div className="modal-footer">
          <button className="btn btn-ghost" onClick={onClose}>Cancel</button>
          <button
            className="btn btn-primary"
            disabled={!selected}
            onClick={() => { if (selected) { onSelect(selected); onClose() } }}
          >
            Select
          </button>
        </div>
      </div>
    </div>
  )
}
