import { useState, useEffect, useCallback } from 'react'
import { api, AppConfig, Share, GlobalEntry } from './api'
import { ThemeProvider } from './theme'
import { ToastProvider, useToast } from './Toast'
import { SharesPage } from './SharesPage'
import { GlobalsPage } from './GlobalsPage'
import { SettingsPage } from './SettingsPage'
import { PreviewPage } from './PreviewPage'
import { LogsPage } from './LogsPage'
import './styles.css'

type Page = 'shares' | 'globals' | 'preview' | 'logs'

const NAV: { id: Page; label: string; icon: string }[] = [
  { id: 'shares',   label: 'Shares',   icon: '🗂' },
  { id: 'globals',  label: 'Globals',  icon: '⚙️' },
  { id: 'preview',  label: 'Preview',  icon: '📄' },
  { id: 'logs',     label: 'Logs',     icon: '📜' },
]

// warnAutoDisabled toasts a warning when the server has flipped a share to
// disabled because its path no longer exists on disk.
function warnAutoDisabled(before: Share[], after: Share[], toast: ReturnType<typeof useToast>['toast']) {
  const names = after
    .filter((s, i) => before[i]?.enabled && !s.enabled)
    .map(s => s.name || '(unnamed)')
  if (names.length > 0) {
    toast(
      'Share(s) auto-disabled',
      `Path no longer exists for: ${names.join(', ')}`,
      'warning'
    )
  }
}

function AppInner() {
  const { toast } = useToast()
  const [config, setConfig] = useState<AppConfig | null>(null)
  const [page, setPage] = useState<Page>('shares')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [importing, setImporting] = useState(false)
  const [restartOutput, setRestartOutput] = useState<{ success: boolean; output: string } | null>(null)
  const [version, setVersion] = useState('')

  // ── Load initial config ─────────────────────────────────────────────────────
  useEffect(() => {
    Promise.all([api.getConfig(), api.version()])
      .then(([cfg, ver]) => {
        setConfig(cfg)
        setVersion(ver.version)
      })
      .catch(e => toast('Failed to load config', String(e), 'error'))
  }, [toast])

  // ── Patch helpers ───────────────────────────────────────────────────────────
  const patchShares = useCallback((shares: Share[]) => {
    setConfig(c => c ? { ...c, shares } : c)
    setDirty(true)
  }, [])

  const patchGlobals = useCallback((globals: GlobalEntry[]) => {
    setConfig(c => c ? { ...c, globals } : c)
    setDirty(true)
  }, [])

  const patchConfig = useCallback((patch: Partial<AppConfig>) => {
    setConfig(c => c ? { ...c, ...patch } : c)
    setDirty(true)
  }, [])

  // ── Save (no restart) ────────────────────────────────────────────────────────
  const save = useCallback(async () => {
    if (!config || saving) return
    setSaving(true)
    try {
      const [savedShares] = await Promise.all([
        api.putShares(config.shares),
        api.putGlobals(config.globals),
        api.putConfig({
          smb_conf_path: config.smb_conf_path,
          share_owner: config.share_owner,
          theme: config.theme,
        }),
      ])
      warnAutoDisabled(config.shares, savedShares, toast)
      setConfig(c => c ? { ...c, shares: savedShares } : c)
      setDirty(false)
      toast('Saved', 'Configuration written to state.json', 'success')
    } catch (e) {
      toast('Save failed', String(e), 'error')
    } finally {
      setSaving(false)
    }
  }, [config, saving, toast])

  // ── Import existing smb.conf ─────────────────────────────────────────────────
  const importConf = useCallback(async (path: string) => {
    if (importing) return
    setImporting(true)
    try {
      const result = await api.importConf(path)
      setConfig(c => c ? {
        ...c,
        globals: result.globals,
        shares: result.shares,
        ...(result.share_owner ? { share_owner: result.share_owner } : {}),
      } : c)
      setDirty(true)
      toast(
        'Imported',
        `Loaded ${result.shares.length} share(s) and ${result.globals.length} global(s) from ${path}. Review, then click Save to persist.`,
        'success'
      )
    } catch (e) {
      toast('Import failed', String(e), 'error')
    } finally {
      setImporting(false)
    }
  }, [importing, toast])

  // ── Save + write smb.conf + restart Samba ────────────────────────────────────
  const saveAndRestart = useCallback(async () => {
    if (!config || restarting) return
    setRestarting(true)
    setRestartOutput(null)
    try {
      // Persist config first.
      const [savedShares] = await Promise.all([
        api.putShares(config.shares),
        api.putGlobals(config.globals),
        api.putConfig({
          smb_conf_path: config.smb_conf_path,
          share_owner: config.share_owner,
          theme: config.theme,
        }),
      ])
      warnAutoDisabled(config.shares, savedShares, toast)
      setConfig(c => c ? { ...c, shares: savedShares } : c)
      setDirty(false)
      // Then write smb.conf and restart.
      const result = await api.saveAndRestart()
      setRestartOutput(result.restart)
      if (result.restart.success) {
        toast('Saved & restarted', `smb.conf written to ${result.path}`, 'success')
      } else {
        toast('Samba restart failed', result.restart.output || 'Unknown error', 'warning')
      }
    } catch (e) {
      toast('Save & restart failed', String(e), 'error')
    } finally {
      setRestarting(false)
    }
  }, [config, restarting, toast])

  if (!config) {
    return (
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100vh', color: 'var(--text-muted)' }}>
        Loading smbed…
      </div>
    )
  }

  return (
    <ThemeProvider initial={config.theme} onChange={theme => patchConfig({ theme })}>
      <div className="layout">
        {/* ── Topbar ─────────────────────────────────────────────────── */}
        <header className="topbar">
          <span className="topbar-logo">smbed<span> — Samba Mini-editor</span></span>
          <span className="topbar-spacer" />
          <span className="topbar-status">
            <span className={`status-dot${saving || restarting ? ' saving' : ''}`} />
            {saving ? 'Saving…' : restarting ? 'Restarting Samba…' : dirty ? 'Unsaved changes' : 'Saved'}
          </span>
          {version && <span className="text-muted" style={{ fontSize: 10 }}>v{version}</span>}
          <button
            className="hamburger-btn"
            onClick={() => setSettingsOpen(true)}
            title="Settings"
            aria-label="Open settings"
          >
            ☰
          </button>
        </header>

        {/* ── Sidebar ────────────────────────────────────────────────── */}
        <nav className="sidebar">
          <div className="sidebar-section-label">Navigation</div>
          {NAV.map(n => (
            <div
              key={n.id}
              className={`nav-item${page === n.id ? ' active' : ''}`}
              onClick={() => setPage(n.id)}
            >
              <span className="nav-icon">{n.icon}</span>
              {n.label}
            </div>
          ))}

          <div className="divider" style={{ margin: '12px 16px' }} />
          <div className="sidebar-section-label">Share owner</div>
          <div style={{ padding: '4px 16px' }}>
            <input
              className="input input-sm"
              value={config.share_owner}
              onChange={e => patchConfig({ share_owner: e.target.value })}
              title="Linux user that owns all shares"
            />
          </div>
        </nav>

        {/* ── Main ───────────────────────────────────────────────────── */}
        <main className="main-content">
          {page === 'shares'   && <SharesPage shares={config.shares} onChange={patchShares} />}
          {page === 'globals'  && <GlobalsPage globals={config.globals} onChange={patchGlobals} />}
          {page === 'preview'  && <PreviewPage />}
          {page === 'logs'     && <LogsPage />}

          {/* ── Restart output ──────────────────────────────────────── */}
          {restartOutput && (
            <div className="card mt-16" style={{ borderColor: restartOutput.success ? 'var(--green)' : 'var(--red)' }}>
              <div className="card-title">
                {restartOutput.success ? '✅' : '❌'} Samba restart output
              </div>
              <div className="restart-output">
                {restartOutput.output || '(no output)'}
              </div>
            </div>
          )}
        </main>

        {/* ── Action bar (persistent footer) ────────────────────────── */}
        <footer className="action-bar">
          <span className="action-bar-hint">
            {dirty
              ? '⚠️ You have unsaved changes.'
              : '✓ All changes saved to state.json.'}
          </span>
          <button
            className="btn btn-ghost"
            onClick={save}
            disabled={!dirty || saving || restarting}
          >
            💾 Save
          </button>
          <button
            className="btn btn-success"
            onClick={saveAndRestart}
            disabled={saving || restarting}
          >
            {restarting ? '⟳ Restarting…' : '🚀 Save & Restart Samba'}
          </button>
        </footer>

        {/* ── Settings drawer ────────────────────────────────────────── */}
        <div
          className={`settings-backdrop${settingsOpen ? ' open' : ''}`}
          onClick={() => setSettingsOpen(false)}
        />
        <div className={`settings-drawer${settingsOpen ? ' open' : ''}`}>
          <div className="settings-drawer-header">
            <span className="settings-drawer-title">🔧 Settings</span>
            <button
              className="btn-icon"
              onClick={() => setSettingsOpen(false)}
              aria-label="Close settings"
            >
              ✕
            </button>
          </div>
          <div className="settings-drawer-body">
            <SettingsPage config={config} onChange={patchConfig} importing={importing} onImport={importConf} />
          </div>
        </div>
      </div>
    </ThemeProvider>
  )
}

export default function App() {
  return (
    <ToastProvider>
      <AppInner />
    </ToastProvider>
  )
}
