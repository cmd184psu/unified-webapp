import { useState } from 'react'
import { showToast } from '@shared'
import { api, Share } from './api'
import { pickFolder } from './FolderPicker'

interface Props {
  shares: Share[]
  onChange: (shares: Share[]) => void
  /** The Linux user all shares belong to; its home dir backs the "home" share. */
  shareOwner: string
}

const HOME_SHARE = 'home'

function emptyShare(): Share {
  return {
    name: '',
    path: '',
    comment: '',
    writable: true,
    public: false,
    browseable: true,
    enabled: true,
  }
}

function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="ui-toggle">
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} />
      <span className="ui-toggle-track" />
    </label>
  )
}

function ShareEditor({
  share,
  onChange,
  onDelete,
  onPickFolder,
}: {
  share: Share
  onChange: (s: Share) => void
  onDelete: () => void
  onPickFolder: () => void
}) {
  const [expanded, setExpanded] = useState(!share.name)

  const set = <K extends keyof Share>(k: K, v: Share[K]) => onChange({ ...share, [k]: v })

  return (
    <div className={`share-card${share.enabled ? '' : ' disabled'}`}>
      <div className="share-card-header" onClick={() => setExpanded(e => !e)}>
        <span style={{ fontSize: 16 }}>{expanded ? '▾' : '▸'}</span>
        <span className="share-card-name">{share.name || <em className="text-muted">unnamed</em>}</span>
        <span className="share-card-path">{share.path}</span>
        <span className={`share-badge ${share.enabled ? 'enabled' : 'disabled'}`}>
          {share.enabled ? 'on' : 'off'}
        </span>
      </div>

      {expanded && (
        <div className="share-card-body">
          {/* Name */}
          <div className="field">
            <label className="field-label">Share name</label>
            <input
              className="input input-sm"
              value={share.name}
              placeholder="e.g. media"
              onChange={e => set('name', e.target.value.replace(/\s+/g, '-').toLowerCase())}
            />
          </div>

          {/* Path */}
          <div className="field">
            <label className="field-label">Path</label>
            <div className="row">
              <input
                className="input input-sm"
                value={share.path}
                readOnly
                placeholder="/opt/…"
                style={{ flex: 1 }}
              />
              <button className="btn btn-ghost btn-sm" onClick={onPickFolder} title="Browse /opt">
                📂
              </button>
            </div>
          </div>

          {/* Comment */}
          <div className="field" style={{ gridColumn: '1 / -1' }}>
            <label className="field-label">Comment <span className="text-muted">(optional)</span></label>
            <input
              className="input input-sm"
              value={share.comment ?? ''}
              placeholder="Human-readable description"
              onChange={e => set('comment', e.target.value)}
            />
          </div>

          {/* Toggles */}
          <div className="col" style={{ gap: 2 }}>
            <div className="toggle-row">
              <div>
                <div className="toggle-label">Enabled</div>
                <div className="toggle-hint">Include in smb.conf</div>
              </div>
              <Toggle checked={share.enabled} onChange={v => set('enabled', v)} />
            </div>
            <div className="toggle-row">
              <div>
                <div className="toggle-label">Writable</div>
                <div className="toggle-hint">Allow write access</div>
              </div>
              <Toggle checked={share.writable} onChange={v => set('writable', v)} />
            </div>
          </div>

          <div className="col" style={{ gap: 2 }}>
            <div className="toggle-row">
              <div>
                <div className="toggle-label">Browseable</div>
                <div className="toggle-hint">Visible in network browser</div>
              </div>
              <Toggle checked={share.browseable} onChange={v => set('browseable', v)} />
            </div>
            <div className="toggle-row">
              <div>
                <div className="toggle-label">Guest OK</div>
                <div className="toggle-hint">Allow anonymous access</div>
              </div>
              <Toggle checked={share.public} onChange={v => set('public', v)} />
            </div>
          </div>

          {/* Actions */}
          <div className="share-actions">
            <button className="btn btn-danger btn-sm" onClick={onDelete}>
              🗑 Remove share
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

export function SharesPage({ shares, onChange, shareOwner }: Props) {
  const [findingHome, setFindingHome] = useState(false)

  // The share owner's home directory lives outside /opt, where the folder
  // picker cannot go, so the server resolves it. An existing "home" share is
  // re-pointed rather than duplicated, keeping its other settings.
  const pickFor = async (i: number) => {
    const folder = await pickFolder()
    if (!folder) return
    const share = shares[i]
    // Default the name from the folder if share has no name yet.
    update(i, { ...share, path: folder.path, name: share.name || folder.name })
  }

  const shareHome = async () => {
    const owner = shareOwner.trim()
    if (!owner) {
      showToast('Set the share owner first.', 'notice')
      return
    }
    setFindingHome(true)
    try {
      const { path } = await api.home(owner)
      const at = shares.findIndex(s => s.name === HOME_SHARE)
      if (at >= 0) {
        update(at, { ...shares[at], path })
        showToast(`Share "${HOME_SHARE}" now points at ${path}. Save to apply.`, 'success')
      } else {
        onChange([...shares, {
          ...emptyShare(),
          name: HOME_SHARE,
          path,
          comment: `${owner}'s home directory`,
        }])
        showToast(`Added share "${HOME_SHARE}" for ${path}. Save to apply.`, 'success')
      }
    } catch {
      showToast(`Couldn't find a home directory for user "${owner}".`, 'error')
    } finally {
      setFindingHome(false)
    }
  }

  const update = (i: number, s: Share) => {
    const next = [...shares]
    next[i] = s
    onChange(next)
  }

  const remove = (i: number) => {
    onChange(shares.filter((_, idx) => idx !== i))
  }

  const add = () => {
    onChange([...shares, emptyShare()])
  }

  return (
    <div>
      <div className="page-header">
        <div className="row-between">
          <div>
            <div className="page-title">Shares</div>
            <div className="page-subtitle">
              Editable list of Samba share definitions. All shares are owned by the configured user.
            </div>
          </div>
          <div className="row">
            <button
              className="btn btn-ghost"
              onClick={shareHome}
              disabled={findingHome}
              title={`Share ${shareOwner || 'the share owner'}'s home directory as "${HOME_SHARE}"`}
            >
              {findingHome ? '⟳ Finding home…' : '🏠 Share home'}
            </button>
            <button className="btn btn-primary" onClick={add}>+ Add share</button>
          </div>
        </div>
      </div>

      {shares.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon">🗂</div>
          <div className="empty-state-text">No shares defined</div>
          <div className="empty-state-sub">Click "Add share" to pick a folder from /opt</div>
        </div>
      ) : (
        <div className="share-list">
          {shares.map((s, i) => (
            <ShareEditor
              key={i}
              share={s}
              onChange={updated => update(i, updated)}
              onDelete={() => remove(i)}
              onPickFolder={() => void pickFor(i)}
            />
          ))}
        </div>
      )}

    </div>
  )
}
