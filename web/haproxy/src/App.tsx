import { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import { showToast } from '@shared'
import { api, ApiError, Issue, ImportReport, Model, Status, CertsResponse, Changes, CoverageResult, CertMachineCert, Service } from './api'
import { initHamburger } from './main'
import { finalizeForSave, isDirty, normalizeModel, updateService } from './modelForm'
import { groupIssues, issueCounts, issuesSummary } from './issues'
import { IssueList } from './IssueList'
import { StatsPage } from './StatsPage'
import { PendingBar } from './PendingBar'
import { CertsPage } from './CertsPage'
import { CertPicker } from './CertPicker'
import { BackupsPage } from './BackupsPage'
import { LogPage } from './LogPage'
import { RawPage } from './RawPage'
import { runApplyLike } from './applyAction'
import { POLL_MS, firstRunState, shouldPoll } from './livefeed'
import { ServicesPage } from './ServicesPage'
import { GlobalsPage } from './GlobalsPage'
import { SettingsPage } from './SettingsPage'
import './styles.css'

type Tab = 'services' | 'globals' | 'certs' | 'backups' | 'settings' | 'raw' | 'log' | 'stats'

const TABS: { id: Tab; label: string }[] = [
  { id: 'services', label: 'Services' },
  { id: 'globals', label: 'Globals' },
  { id: 'certs', label: 'Certificates' },
  { id: 'backups', label: 'Backups' },
  { id: 'settings', label: 'Settings' },
  { id: 'raw', label: 'Raw' },
  { id: 'log', label: 'Log' },
  { id: 'stats', label: 'Stats' },
]

const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))

function reportLines(r: ImportReport): string[] {
  const lines: string[] = []
  const list = (k: string, label: string) => {
    const v = r[k]
    if (Array.isArray(v)) for (const x of v) lines.push(`${label}: ${String(x)}`)
  }
  list('rawKept', 'Kept verbatim')
  list('legacyCrtArgs', 'Legacy crt argument')
  list('unmapped', 'Not modelled')
  return lines
}

export default function App() {
  const [status, setStatus] = useState<Status | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saved, setSaved] = useState<Model | null>(null)
  const [draft, setDraft] = useState<Model | null>(null)
  const [imported, setImported] = useState(true)
  const [issues, setIssues] = useState<Issue[]>([])
  const [certsResp, setCertsResp] = useState<CertsResponse | null>(null)
  const [changes, setChanges] = useState<Changes | null>(null)
  const [changesError, setChangesError] = useState<string | null>(null)
  const [blockers, setBlockers] = useState<Issue[]>([])
  const [applyBusy, setApplyBusy] = useState(false)
  const [coverage, setCoverage] = useState<CoverageResult[]>([])
  const [refreshKey, setRefreshKey] = useState(0)
  const [picker, setPicker] = useState<{ svc: Service | null } | null>(null)
  const [importStatus, setImportStatus] = useState<number | undefined>(undefined)
  const inFlight = useRef(false)
  const certs = certsResp?.certs ?? []
  const [tab, setTab] = useState<Tab>('services')
  const [busy, setBusy] = useState(false)
  const [preview, setPreview] = useState<{ model: Model; report: ImportReport } | null>(null)

  useEffect(() => { initHamburger() }, [])

  // quiet = the calm background poll: the failure shows inline, not as a toast every few seconds.
  const loadStatus = useCallback(async (quiet = false) => {
    try {
      setStatus(await api.status())
      setStatusError(null)
    } catch (e) {
      setStatusError(errMsg(e))
      if (!quiet) showToast(`Could not load status: ${errMsg(e)}`, 'error')
    }
  }, [])

  const loadChanges = useCallback(async (quiet = false) => {
    try {
      setChanges(await api.changes())
      setChangesError(null)
    } catch (e) {
      setChangesError(errMsg(e))
      if (!quiet) showToast(`Could not read pending changes: ${errMsg(e)}`, 'error')
    }
  }, [])

  const loadIssues = useCallback(async (announce: boolean) => {
    try {
      const r = await api.checkModel()
      setIssues(r.issues ?? [])
      if (announce) {
        const { errors } = issueCounts(r.issues ?? [])
        showToast(issuesSummary(r.issues ?? []), errors > 0 ? 'error' : (r.issues ?? []).length > 0 ? 'notice' : 'success')
      }
    } catch (e) {
      showToast(`Could not check the model: ${errMsg(e)}`, 'error')
    }
  }, [])

  const loadCerts = useCallback(async () => {
    try {
      setCertsResp(await api.certs())
    } catch (e) {
      showToast(`Could not load certificates: ${errMsg(e)}`, 'error')
    }
  }, [])

  const loadModel = useCallback(async () => {
    try {
      const r = await api.getModel()
      const m = normalizeModel(r)
      setSaved(m)
      setDraft(m)
      setImported(r.imported)
      setLoadError(null)
      if (r.imported) loadIssues(false)
    } catch (e) {
      setLoadError(errMsg(e))
      showToast(`Could not load the model: ${errMsg(e)}`, 'error')
    }
  }, [loadIssues])

  const loadCoverage = useCallback(async () => {
    try {
      setCoverage((await api.coverage()) ?? [])
    } catch (e) {
      setCoverage([])
      showToast(`Could not check certificate coverage: ${errMsg(e)}`, 'error')
    }
  }, [])

  // After Save, Apply, Restore and cert actions: everything that can have changed.
  const refreshAll = useCallback(() => {
    loadStatus()
    loadChanges()
    loadCerts()
    loadCoverage()
    setRefreshKey(k => k + 1)
  }, [loadStatus, loadChanges, loadCerts, loadCoverage])

  useEffect(() => { loadStatus(); loadChanges(); loadModel(); loadCerts(); loadCoverage() }, [loadStatus, loadChanges, loadModel, loadCerts, loadCoverage])

  // Calm poll of status + pending changes; paused while the tab is hidden.
  useEffect(() => {
    const tick = async () => {
      if (!shouldPoll(document.hidden, inFlight.current)) return
      inFlight.current = true
      try { await Promise.all([loadStatus(true), loadChanges(true)]) } finally { inFlight.current = false }
    }
    const id = setInterval(tick, POLL_MS)
    const onVisible = () => { if (!document.hidden) tick() }
    document.addEventListener('visibilitychange', onVisible)
    return () => { clearInterval(id); document.removeEventListener('visibilitychange', onVisible) }
  }, [loadStatus, loadChanges])

  const dirty = saved !== null && draft !== null && isDirty(saved, draft)

  // The only place the model is written: the Save button.
  const save = useCallback(async () => {
    if (!draft) return
    const body = finalizeForSave(draft)
    setBusy(true)
    try {
      await api.saveModel(body)
      const m = normalizeModel(body)
      setSaved(m)
      setDraft(m)
      showToast('Model saved.', 'success')
      setBlockers([])
      await loadIssues(true)
      refreshAll()
    } catch (e) {
      showToast(`Save failed: ${errMsg(e)}`, 'error')
      if (e instanceof ApiError && e.issues.length > 0) setIssues(e.issues)
    } finally {
      setBusy(false)
    }
  }, [draft, loadIssues, refreshAll])

  const discard = () => {
    if (!saved) return
    setDraft(saved)
    showToast('Unsaved changes discarded.', 'notice')
  }

  const importPreview = async () => {
    setBusy(true)
    try {
      const r = await api.importLive(false)
      setPreview({ model: normalizeModel(r.model), report: r.report })
      const n = reportLines(r.report).length
      showToast(`Import report ready: ${n} item${n === 1 ? '' : 's'} need your attention. Nothing is stored yet.`, 'notice')
    } catch (e) {
      if (e instanceof ApiError) setImportStatus(e.status)
      showToast(e instanceof ApiError && e.status === 404
        ? 'No live HAProxy configuration was found. You can start with defaults instead.'
        : `Import failed: ${errMsg(e)}`, 'error')
    } finally {
      setBusy(false)
    }
  }

  const importCommit = async () => {
    setBusy(true)
    try {
      const r = await api.importLive(true)
      const m = normalizeModel(r.model)
      setSaved(m)
      setDraft(m)
      setImported(true)
      setPreview(null)
      showToast('Live configuration imported and stored.', 'success')
      await loadIssues(true)
      refreshAll()
    } catch (e) {
      showToast(`Import failed: ${errMsg(e)}`, 'error')
    } finally {
      setBusy(false)
    }
  }

  // First run on a machine with no live config: store the default model so the editor is never blocked behind an import.
  const startWithDefaults = async () => {
    setBusy(true)
    try {
      const body = finalizeForSave(normalizeModel(await api.getModel()))
      await api.saveModel(body)
      const m = normalizeModel(body)
      setSaved(m)
      setDraft(m)
      setImported(true)
      showToast('Started with the default configuration. Review it, then Apply.', 'success')
      await loadIssues(true)
      refreshAll()
    } catch (e) {
      showToast(`Could not start with defaults: ${errMsg(e)}`, 'error')
    } finally {
      setBusy(false)
    }
  }

  const apply = async () => {
    setApplyBusy(true)
    try {
      setBlockers(await runApplyLike(api.apply))
    } finally {
      setApplyBusy(false)
      refreshAll()
      loadIssues(false)
    }
  }

  const restore = async (name: string) => {
    setApplyBusy(true)
    try {
      setBlockers(await runApplyLike(() => api.restoreBackup(name)))
    } finally {
      setApplyBusy(false)
      refreshAll()
    }
  }

  const pulled = (cert: CertMachineCert) => {
    const target = picker?.svc
    setPicker(null)
    if (target && draft) setDraft(updateService(draft, target.id, { cert: { fqdn: cert.fqdn } }))
    refreshAll()
  }
  const grouped = useMemo(() => groupIssues(issues), [issues])
  const counts = issueCounts(issues)

  const editor = () => {
    if (loadError) return <p className="error">{loadError}</p>
    if (!draft || !saved) return <p>Loading…</p>
    if (!imported) {
      const first = firstRunState({ imported, importStatus })
      return (
        <div className="card stack">
          <div className="card-title">Import the live HAProxy configuration</div>
          <p className="muted">No model has been stored yet. Import reads the running configuration and shows what it could and could not map; nothing is stored until you confirm.</p>
          {preview ? (
            <>
              {reportLines(preview.report).length === 0
                ? <p>The whole configuration maps cleanly.</p>
                : <ul className="issues">{reportLines(preview.report).map((l, n) => <li key={n} className="issue issue-warning">{l}</li>)}</ul>}
              <p className="muted">{preview.model.services.length} service(s) found.</p>
              <div className="row">
                <button className="btn btn-primary" disabled={busy} onClick={importCommit}>Confirm import</button>
                <button className="btn btn-ghost" disabled={busy} onClick={() => { setPreview(null); showToast('Import cancelled; nothing stored.', 'notice') }}>Cancel</button>
              </div>
            </>
          ) : (
            <>
            <div className="row">
              <button className="btn btn-primary" disabled={busy} onClick={importPreview}>Import live config</button>
              {first === 'defaults' && <button className="btn btn-ghost" disabled={busy} onClick={startWithDefaults}>Start with defaults</button>}
            </div>
            {first === 'defaults' && <p className="muted">No live HAProxy configuration exists on this machine. Start with defaults stores the default model so you can add services.</p>}
            </>
          )}
        </div>
      )
    }
    return (
      <>
        <div className="savebar">
          <span className={dirty ? 'unsaved' : 'muted'}>{dirty ? 'Unsaved changes' : 'All changes saved'}</span>
          {counts.errors + counts.warnings > 0 && (
            <span className="muted">{counts.errors} error(s), {counts.warnings} warning(s)</span>
          )}
          <span className="grow" />
          <button className="btn btn-ghost" disabled={!dirty || busy} onClick={discard}>Discard</button>
          <button className="btn btn-primary" disabled={!dirty || busy} onClick={save}>Save</button>
        </div>
        <IssueList issues={grouped.other} />
        {tab === 'services' && (
          <ServicesPage
            model={draft}
            certs={certs}
            issues={grouped}
            coverage={coverage}
            onPickCert={svc => setPicker({ svc })}
            onChange={setDraft}
          />
        )}
        {tab === 'globals' && (
          <GlobalsPage
            global={draft.global}
            defaults={draft.defaults}
            onGlobal={rows => setDraft({ ...draft, global: rows })}
            onDefaults={rows => setDraft({ ...draft, defaults: rows })}
          />
        )}
      </>
    )
  }

  return (
    <>
      <header className="topbar">
        <h1>HAProxy editor</h1>
        <button id="hamburger-trigger" className="hamburger-trigger" aria-label="Settings">☰</button>
      </header>
      <PendingBar
        status={status}
        statusError={statusError}
        changes={changes}
        changesError={changesError}
        blockers={blockers}
        busy={applyBusy}
        dirty={dirty}
        onApply={apply}
        onRefresh={refreshAll}
      />
      <nav className="tabs">
        {TABS.map(t => (
          <button key={t.id} className={`tab${tab === t.id ? ' active' : ''}`} onClick={() => setTab(t.id)}>{t.label}</button>
        ))}
      </nav>
      <main className="content">
        {(tab === 'services' || tab === 'globals') && editor()}
        {tab === 'certs' && (
          <CertsPage
            data={certsResp}
            configured={status?.certmachineConfigured ?? false}
            refreshKey={refreshKey}
            onChanged={refreshAll}
            onPick={() => setPicker({ svc: null })}
          />
        )}
        {tab === 'backups' && <BackupsPage refreshKey={refreshKey} onRestore={restore} />}
        {tab === 'settings' && <SettingsPage onSaved={refreshAll} />}
        {tab === 'raw' && <RawPage refreshKey={refreshKey} />}
        {tab === 'log' && <LogPage />}
        {tab === 'stats' && <StatsPage />}
      </main>
      {picker && (
        <CertPicker
          configured={status?.certmachineConfigured ?? false}
          forFqdn={picker.svc?.fqdns.find(f => f.trim() !== '')?.trim() ?? ''}
          onClose={() => setPicker(null)}
          onPulled={pulled}
        />
      )}
    </>
  )
}
