import { useState } from 'react'
import { AppConfig } from './api'
import { useTheme, ThemeMode } from './theme'

interface Props {
  config: AppConfig
  onChange: (patch: Partial<AppConfig>) => void
  importing: boolean
  onImport: (path: string) => void
}

export function SettingsPage({ config, onChange, importing, onImport }: Props) {
  const { mode, setMode } = useTheme()
  const [importPath, setImportPath] = useState(config.smb_conf_path)

  const themes: { label: string; value: ThemeMode; icon: string }[] = [
    { label: 'Light', value: 'light', icon: '☀️' },
    { label: 'Dark', value: 'dark', icon: '🌑' },
    { label: 'System', value: 'system', icon: '💻' },
  ]

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Settings</div>
        <div className="page-subtitle">
          Server configuration and appearance. Saved to <code>state.json</code>.
        </div>
      </div>

      {/* ── Appearance ────────────────────────────────────────────────── */}
      <div className="card">
        <div className="card-title">🎨 Appearance</div>

        <div className="field">
          <label className="field-label">Theme</label>
          <div className="theme-switcher">
            {themes.map(t => (
              <button
                key={t.value}
                className={`theme-btn${mode === t.value ? ' active' : ''}`}
                onClick={() => {
                  setMode(t.value)
                  onChange({ theme: t.value })
                }}
              >
                {t.icon} {t.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {/* ── Server ───────────────────────────────────────────────────── */}
      <div className="card">
        <div className="card-title">⚙️ Server</div>

        <div className="field">
          <label className="field-label">smb.conf output path</label>
          <input
            className="input"
            value={config.smb_conf_path}
            placeholder="/etc/samba/smb.conf"
            onChange={e => onChange({ smb_conf_path: e.target.value })}
          />
        </div>

        <div className="field">
          <label className="field-label">Samba log path <span className="text-muted">(tailed on the Logs page)</span></label>
          <input
            className="input"
            value={config.samba_log_path}
            placeholder="/var/log/samba/log.smbd"
            onChange={e => onChange({ samba_log_path: e.target.value })}
          />
        </div>
      </div>

      {/* ── Import ───────────────────────────────────────────────────── */}
      <div className="card">
        <div className="card-title">📥 Import existing configuration</div>
        <div className="page-subtitle" style={{ marginBottom: 14 }}>
          Read an existing smb.conf and load its <code>[global]</code> settings and shares
          into the editor. This replaces the Globals and Shares shown here — review them,
          then click Save to persist. Nothing is written to disk until you save.
        </div>

        <div className="field">
          <label className="field-label">smb.conf path to import</label>
          <input
            className="input"
            value={importPath}
            placeholder="/etc/samba/smb.conf"
            onChange={e => setImportPath(e.target.value)}
          />
        </div>

        <button
          className="btn btn-primary"
          style={{ width: '100%', padding: '10px 14px', fontSize: 12 }}
          disabled={importing || !importPath.trim()}
          onClick={() => {
            if (window.confirm(
              `Import ${importPath}? This will replace the Globals and Shares currently shown in the editor.`
            )) {
              onImport(importPath.trim())
            }
          }}
        >
          {importing ? '⟳ Importing…' : '📥 Import'}
        </button>
      </div>

      {/* ── Share ownership ───────────────────────────────────────────── */}
      <div className="card">
        <div className="card-title">👤 Share ownership</div>
        <div className="page-subtitle" style={{ marginBottom: 14 }}>
          All shares use this Linux user as <code>valid users</code> and <code>force user</code>.
        </div>

        <div className="field">
          <label className="field-label">Share owner</label>
          <input
            className="input"
            value={config.share_owner}
            placeholder="nobody"
            onChange={e => onChange({ share_owner: e.target.value })}
          />
        </div>
      </div>
    </div>
  )
}
