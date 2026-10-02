import { useEffect, useState } from 'react'
import { showToast, confirmDialog } from '@shared'
import { api, ApiError, CertMachineCert } from './api'
import { pickerNeedsConfirm, pickerRows, pullErrorToast } from './certview'
import { Toggle } from './Toggle'

interface Props {
  configured: boolean
  /** The service FQDN the cert must cover; empty when picking from the Certificates tab. */
  forFqdn: string
  onClose: () => void
  /** Called after a successful pull with the chosen CertMachine cert. */
  onPulled: (cert: CertMachineCert) => void
}

/** Modal listing CertMachine certs: covering ones by default, everything (flagged) with Show all. */
export function CertPicker({ configured, forFqdn, onClose, onPulled }: Props) {
  const [showAll, setShowAll] = useState(false)
  const [certs, setCerts] = useState<CertMachineCert[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!configured) return
    let stale = false
    setCerts(null)
    api.certMachineCerts(forFqdn || undefined, showAll)
      .then(r => { if (!stale) { setCerts(r.certs ?? []); setError(null) } })
      .catch(e => {
        if (stale) return
        const m = e instanceof Error ? e.message : String(e)
        setError(m)
        showToast(`Could not list CertMachine certificates: ${m}`, 'error')
      })
    return () => { stale = true }
  }, [configured, forFqdn, showAll])

  const choose = async (cert: CertMachineCert, needsConfirm: boolean) => {
    if (needsConfirm && !(await confirmDialog(
      `${cert.fqdn} (id ${cert.id}) is flagged and may not work for ${forFqdn || 'this service'}. Install it anyway?`,
      { confirmLabel: 'Install anyway' },
    ))) {
      showToast('Install cancelled.', 'notice')
      return
    }
    setBusy(true)
    try {
      const r = await api.pullCert(cert.id, '')
      showToast(`Installed ${r.name}. The change is pending until you Apply.`, 'success')
      onPulled(cert)
    } catch (e) {
      const t = e instanceof ApiError ? pullErrorToast(e.status, e.message) : pullErrorToast(0, e instanceof Error ? e.message : String(e))
      showToast(t.message, t.tone)
    } finally {
      setBusy(false)
    }
  }

  const rows = certs ? pickerRows(certs, showAll, forFqdn !== '') : []
  return (
    <div className="ui-modal-overlay" role="dialog" aria-modal="true">
      <div className="ui-modal-panel picker">
        <div className="ui-modal-title">Pick a CertMachine certificate{forFqdn ? ` for ${forFqdn}` : ''}</div>
        {!configured ? (
          <p className="error">CertMachine is not configured. Set certmachine.url in the haproxy settings to pick certificates.</p>
        ) : (
          <div className="stack">
            <Toggle checked={showAll} onChange={setShowAll} label="Show all (including non-covering, archived, quarantined, expired)" />
            {error && <p className="error">{error}</p>}
            {!certs && !error && <p className="muted">Loading…</p>}
            {certs && rows.length === 0 && <p className="muted">{showAll ? 'CertMachine has no certificates.' : 'No active certificate covers this name. Use Show all to see the rest.'}</p>}
            {rows.map(r => (
              <div className="row pick-row" key={r.cert.id}>
                <span className="grow">{r.cert.fqdn} <span className="muted">(id {r.cert.id})</span></span>
                {r.flags.map(f => <span className="badge badge-warning" key={f}>{f}</span>)}
                <button className="btn btn-primary btn-sm" disabled={busy} onClick={() => choose(r.cert, pickerNeedsConfirm(r))}>Choose</button>
              </div>
            ))}
          </div>
        )}
        <div className="row"><span className="grow" /><button className="btn btn-ghost" onClick={onClose}>Close</button></div>
      </div>
    </div>
  )
}
