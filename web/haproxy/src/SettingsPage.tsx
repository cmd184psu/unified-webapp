import { useCallback, useEffect, useState } from 'react'
import { showToast } from '@shared'
import { api, SettingsResponse } from './api'
import { Toggle } from './Toggle'
import { FieldErrors, FormState, TextKey, apiKeyLabel, buildPayload, fromEffective, hasErrors, isDirty, validate } from './settingsform'

const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))

interface Props {
  /** Called after settings were saved so status, certs and the rest reload. */
  onSaved: () => void
}

// The Settings tab: nothing is written until the explicit Save button.
export function SettingsPage({ onSaved }: Props) {
  const [loaded, setLoaded] = useState<SettingsResponse | null>(null)
  const [saved, setSaved] = useState<FormState | null>(null)
  const [draft, setDraft] = useState<FormState | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({})
  const [serverError, setServerError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const r = await api.settings()
      const f = fromEffective(r.effective)
      setLoaded(r)
      setSaved(f)
      setDraft(f)
      setLoadError(null)
    } catch (e) {
      setLoadError(errMsg(e))
      showToast(`Could not load settings: ${errMsg(e)}`, 'error')
    }
  }, [])
  useEffect(() => { load() }, [load])

  if (loadError) return <p className="error">{loadError}</p>
  if (!loaded || !saved || !draft) return <p>Loading…</p>

  const dirty = isDirty(saved, draft)
  const edit = (k: TextKey, v: string) => {
    setDraft({ ...draft, values: { ...draft.values, [k]: v } })
    setFieldErrors({ ...fieldErrors, [k]: undefined })
    setServerError(null)
  }
  const def = (k: string) => loaded.defaults[k] ?? ''

  const field = (k: TextKey, label: string, opts: { placeholder?: string; hint?: string } = {}) => (
    <label className="field" key={k}>
      <span className="field-label">{label}</span>
      <input
        className="input settings-input"
        value={draft.values[k]}
        placeholder={opts.placeholder}
        onChange={e => edit(k, e.target.value)}
        spellCheck={false}
        autoComplete="off"
        data-lpignore="true"
        data-1p-ignore
        data-form-type="other"
      />
      {opts.hint && <span className="muted">{opts.hint}</span>}
      {fieldErrors[k] && <span className="error">{fieldErrors[k]}</span>}
    </label>
  )
  const pathField = (k: TextKey, label: string) => field(k, label, { placeholder: def(k) })

  const submit = async () => {
    const errs = validate(draft)
    setFieldErrors(errs)
    if (hasErrors(errs)) {
      showToast('Fix the highlighted settings first; nothing was saved.', 'error')
      return
    }
    setBusy(true)
    try {
      const r = await api.saveSettings(buildPayload(draft))
      setServerError(null)
      setNotice(r.needsRestart ? r.message : null)
      showToast(r.needsRestart ? r.message : 'Settings saved and applied.', r.needsRestart ? 'notice' : 'success')
      await load()
      onSaved()
    } catch (e) {
      setServerError(errMsg(e))
      showToast(`Save failed: ${errMsg(e)}`, 'error')
    } finally {
      setBusy(false)
    }
  }

  const discard = () => {
    setDraft(saved)
    setFieldErrors({})
    setServerError(null)
    showToast('Unsaved changes discarded.', 'notice')
  }

  const testConnection = async () => {
    setBusy(true)
    try {
      const r = await api.testConnection({
        certmachineUrl: draft.values.certmachineUrl.trim(),
        certmachineCaFile: draft.values.certmachineCaFile.trim(),
        certmachineInsecure: draft.values.certmachineInsecure,
        apiKey: draft.clearApiKey ? '' : draft.apiKey,
      })
      showToast(r.message, r.ok ? 'success' : 'error')
    } catch (e) {
      showToast(`Test connection failed: ${errMsg(e)}`, 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="stack">
      <div className="savebar">
        <span className={dirty ? 'unsaved' : 'muted'}>{dirty ? 'Unsaved changes' : 'All changes saved'}</span>
        <span className="grow" />
        <button className="btn btn-ghost" disabled={!dirty || busy} onClick={discard}>Discard</button>
        <button className="btn btn-primary" disabled={!dirty || busy} onClick={submit}>Save settings</button>
      </div>
      {serverError && <p className="error">{serverError}</p>}
      {notice && <p className="muted">{notice}</p>}
      {loaded.unavailable && <p className="error">The module cannot drive HAProxy right now: {loaded.unavailable}</p>}

      <div className="card stack">
        <div className="card-title">CertMachine</div>
        {field('certmachineUrl', 'CertMachine URL', { placeholder: 'https://certmachine.example.com', hint: 'Empty = no CertMachine' })}
        <div className="field">
          <span className="field-label">API key</span>
          <div className="row">
            <input
              type="text"
              className="input settings-input grow ui-secret"
              autoComplete="off"
              data-lpignore="true"
              data-1p-ignore
              data-form-type="other"
              spellCheck={false}
              value={draft.apiKey}
              placeholder={loaded.apiKeySet ? 'Type a new key to replace the stored one' : 'Paste the API key'}
              onChange={e => { setDraft({ ...draft, apiKey: e.target.value, clearApiKey: false }); setServerError(null) }}
            />
            <span className="badge">{apiKeyLabel(loaded.apiKeySet, draft)}</span>
            <button
              className="btn btn-ghost btn-sm"
              disabled={!loaded.apiKeySet || draft.clearApiKey}
              onClick={() => setDraft({ ...draft, apiKey: '', clearApiKey: true })}
            >Clear</button>
          </div>
          <span className="muted">Write-only; stored on this machine.</span>
          {fieldErrors.apiKey && <span className="error">{fieldErrors.apiKey}</span>}
        </div>
        {field('certmachineCaFile', 'CA file', { placeholder: 'Optional: path to a CA certificate to trust' })}
        <Toggle
          checked={draft.values.certmachineInsecure}
          onChange={v => setDraft({ ...draft, values: { ...draft.values, certmachineInsecure: v } })}
          label="Skip certificate check"
        />
        <div className="row">
          <button className="btn" disabled={busy} onClick={testConnection}>Test connection</button>
        </div>
      </div>

      <div className="card stack">
        <div className="card-title">Files and service</div>
        {pathField('configPath', 'Config path')}
        {pathField('certsDir', 'Certs directory')}
        {pathField('crtListPath', 'crt-list path')}
        {pathField('statsSocketPath', 'Stats socket path')}
        {pathField('backupDir', 'Backup directory')}
        {pathField('serviceName', 'Service name')}
        <span className="muted">Empty = default.</span>
      </div>

      <div className="card stack">
        <div className="card-title">Behaviour</div>
        <label className="field">
          <span className="field-label">Operating system</span>
          <select className="input settings-input" value={draft.values.os} onChange={e => edit('os', e.target.value)}>
            <option value="auto">Detect automatically</option>
            <option value="ubuntu">Ubuntu</option>
            <option value="rocky">Rocky / RHEL</option>
            <option value="macos">macOS (Apple silicon)</option>
          </select>
        </label>
        {field('backupKeep', 'Backups to keep (1-100)')}
        {field('expiryWarnDays', 'Warn this many days before a certificate expires (1-365)')}
      </div>
    </div>
  )
}
