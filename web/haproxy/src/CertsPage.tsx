import { Fragment, useCallback, useEffect, useState } from 'react'
import { showToast, confirmDialog } from '@shared'
import { api, CertRow, CertsResponse, Freshness, ImportableCert } from './api'
import { certDetailsView, expiryBadge, freshnessBadge, freshnessFor, removeButtonState } from './certview'
import { Toggle } from './Toggle'

const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))

interface Props {
  data: CertsResponse | null
  configured: boolean
  refreshKey: number
  onChanged: () => void
  onPick: () => void
}

export function CertsPage({ data, configured, refreshKey, onChanged, onPick }: Props) {
  const [fresh, setFresh] = useState<Freshness[]>([])
  const [freshError, setFreshError] = useState<string | null>(null)
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)

  const loadFresh = useCallback(async () => {
    if (!configured) { setFresh([]); return }
    try {
      setFresh((await api.freshness()) ?? [])
      setFreshError(null)
    } catch (e) {
      setFreshError(errMsg(e))
      showToast(`Could not check certificate freshness: ${errMsg(e)}`, 'error')
    }
  }, [configured])
  useEffect(() => { loadFresh() }, [loadFresh, refreshKey])

  const [found, setFound] = useState<ImportableCert[]>([])
  const [picked, setPicked] = useState<Set<string>>(new Set())
  useEffect(() => {
    api.importableCerts().then(list => {
      setFound(list ?? [])
      setPicked(new Set((list ?? []).map(c => c.name)))
    }).catch(() => setFound([]))
  }, [refreshKey])

  const importPicked = async () => {
    setBusy(true)
    try {
      const r = await api.importCerts([...picked])
      const n = r.imported?.length ?? 0
      showToast(`${n} certificate${n === 1 ? '' : 's'} imported. They stay exactly where they are. Run Check to test them.`, 'success')
    } catch (e) {
      showToast(`Could not import: ${errMsg(e)}`, 'error')
    } finally {
      setBusy(false)
      onChanged()
    }
  }

  const toggleOpen = (n: string) => setOpen(p => { const x = new Set(p); if (x.has(n)) x.delete(n); else x.add(n); return x })

  const setEnabled = async (c: CertRow, enabled: boolean) => {
    try {
      await api.setCertEnabled(c.name, enabled)
      showToast(`${c.name} ${enabled ? 'enabled' : 'disabled'}. The change is pending until you Apply.`, 'success')
    } catch (e) {
      showToast(`Could not ${enabled ? 'enable' : 'disable'} ${c.name}: ${errMsg(e)}`, 'error')
    } finally {
      onChanged()
    }
  }

  const remove = async (c: CertRow) => {
    if (!(await confirmDialog(`Remove ${c.name}? The file is deleted.`, { confirmLabel: 'Remove' }))) {
      showToast('Remove canceled.', 'notice')
      return
    }
    try {
      await api.deleteCert(c.name)
      showToast(`${c.name} removed.`, 'success')
    } catch (e) {
      showToast(`Could not remove ${c.name}: ${errMsg(e)}`, 'error')
    } finally {
      onChanged()
    }
  }

  const update = async (c: CertRow) => {
    setBusy(true)
    try {
      const r = await api.certMachineCerts(c.certmachine.fqdn)
      const active = (r.certs ?? []).find(x => x.fqdn === c.certmachine.fqdn && (!x.status || x.status === 'active'))
      if (!active) {
        showToast(`CertMachine has no active certificate for ${c.certmachine.fqdn}.`, 'error')
        return
      }
      await api.pullCert(active.id, c.note)
      showToast(`${c.name} updated from CertMachine. The change is pending until you Apply.`, 'success')
    } catch (e) {
      showToast(`Could not update ${c.name}: ${errMsg(e)}`, 'error')
    } finally {
      setBusy(false)
      onChanged()
    }
  }

  return (
    <div className="stack">
      <div className="row">
        <span className="muted">
          {data ? `${data.certs.length} certificate${data.certs.length === 1 ? '' : 's'}` : 'Loading…'}
        </span>
        <span className="grow" />
        <button className="btn btn-primary btn-sm" onClick={onPick}>+ Add from CertMachine</button>
      </div>
      {found.length > 0 && (
        <div className="card stack">
          <div className="card-title">Already on this server</div>
          <p className="muted">In the certs folder, not tracked yet. Files are left as they are.</p>
          {found.map(c => (
            <Toggle key={c.name} checked={picked.has(c.name)} label={`${c.fqdn}  ·  ${c.name}`}
              onChange={v => setPicked(p => { const x = new Set(p); if (v) x.add(c.name); else x.delete(c.name); return x })} />
          ))}
          <div className="row">
            <button className="btn btn-primary btn-sm" disabled={busy || picked.size === 0} onClick={importPicked}>Import {picked.size}</button>
          </div>
        </div>
      )}
      {!configured && <p className="muted">CertMachine isn't set up.</p>}
      {freshError && <p className="error">Freshness unavailable: {freshError}</p>}
      {data && data.certs.length === 0 && found.length === 0 && <p className="muted">No certificates yet.</p>}
      {data && data.certs.map(c => {
        const details = certDetailsView(c)
        const exp = expiryBadge(c.details)
        const rs = removeButtonState(c)
        const fb = freshnessBadge(configured ? freshnessFor(fresh, c.name) : undefined)
        return (
          <div className={`card${c.enabled ? '' : ' svc-off'}`} key={c.name}>
            <div className="row">
              <span className="svc-name grow">{c.name}</span>
              <Toggle checked={c.enabled} onChange={v => setEnabled(c, v)} label="Enabled" />
              {c.missing && <span className="badge badge-error" title="The certificate file is not on disk">missing</span>}
              {c.superseded && <span className="badge">superseded</span>}
              {exp && <span className={`badge badge-${exp.tone}`}>{exp.label}</span>}
              {configured && <span className={`badge badge-${fb.tone}`} title={fb.title}>{fb.label}</span>}
              {fb.canUpdate && <button className="btn btn-primary btn-sm" disabled={busy} onClick={() => update(c)}>Update</button>}
              <button className="btn btn-ghost btn-sm" onClick={() => toggleOpen(c.name)}>{open.has(c.name) ? 'Hide details' : 'Details'}</button>
              <button className="btn btn-danger btn-sm" disabled={rs.disabled} title={rs.title} onClick={() => remove(c)}>Remove</button>
            </div>
            {rs.disabled && <div className="muted small">{rs.title}</div>}
            <div className="muted">{c.certmachine.fqdn} (CertMachine id {c.certmachine.id}){c.note ? ` - ${c.note}` : ''}</div>
            {open.has(c.name) && (
              details.available ? (
                <dl className="status">
                  {details.fields.map(f => <Fragment key={f.label}><dt>{f.label}</dt><dd>{f.value}</dd></Fragment>)}
                </dl>
              ) : <p className="muted">{details.message}</p>
            )}
          </div>
        )
      })}
    </div>
  )
}
