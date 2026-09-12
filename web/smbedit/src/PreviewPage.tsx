import { useState, useCallback } from 'react'
import { api } from './api'

function highlight(text: string): string {
  return text
    .split('\n')
    .map(line => {
      const trimmed = line.trim()
      if (trimmed.startsWith('#')) {
        return `<span class="token-comment">${escHtml(line)}</span>`
      }
      if (/^\[.+\]$/.test(trimmed)) {
        return `<span class="token-section">${escHtml(line)}</span>`
      }
      const eqIdx = line.indexOf('=')
      if (eqIdx !== -1) {
        const key = line.slice(0, eqIdx)
        const val = line.slice(eqIdx)
        return `<span class="token-key">${escHtml(key)}</span>${escHtml(val)}`
      }
      return escHtml(line)
    })
    .join('\n')
}

function escHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

export function PreviewPage() {
  const [content, setContent] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(() => {
    setLoading(true)
    setError('')
    api.preview()
      .then(setContent)
      .catch(e => setError(String(e)))
      .finally(() => setLoading(false))
  }, [])

  return (
    <div>
      <div className="page-header">
        <div className="row-between">
          <div>
            <div className="page-title">Preview</div>
            <div className="page-subtitle">
              Rendered smb.conf based on current settings — not yet written to disk.
            </div>
          </div>
          <button className="btn btn-primary" onClick={load} disabled={loading}>
            {loading ? '⟳ Rendering…' : '👁 Render preview'}
          </button>
        </div>
      </div>

      {error && (
        <div className="card" style={{ borderColor: 'var(--red)' }}>
          <span className="text-red">{error}</span>
        </div>
      )}

      {content !== null && !error && (
        <pre
          className="preview-pre"
          dangerouslySetInnerHTML={{ __html: highlight(content) }}
        />
      )}

      {content === null && !error && (
        <div className="empty-state">
          <div className="empty-state-icon">📄</div>
          <div className="empty-state-text">Click "Render preview" to see the generated smb.conf</div>
        </div>
      )}
    </div>
  )
}
