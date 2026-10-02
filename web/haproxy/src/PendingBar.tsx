import { useState } from 'react'
import { showToast, confirmDialog } from '@shared'
import { api, Changes, Issue, Status } from './api'
import { classifyDiff, pendingView, serviceActionNeedsConfirm, diagnosticNotices } from './pending'
import { IssueList } from './IssueList'

const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e))

function DiffView({ title, diff }: { title: string; diff: string }) {
  if (!diff) return null
  return (
    <div className="stack">
      <div className="field-label">{title}</div>
      <pre className="diff">
        {classifyDiff(diff).map((l, n) => <div key={n} className={`diff-${l.kind}`}>{l.text || ' '}</div>)}
      </pre>
    </div>
  )
}

interface Props {
  status: Status | null
  statusError: string | null
  changes: Changes | null
  changesError: string | null
  blockers: Issue[]
  busy: boolean
  dirty: boolean
  onApply: () => void
  onRefresh: () => void
}

/** Always-visible bar: pending state, Review/Apply/Check, service actions, status panel (D4, D5). */
export function PendingBar({ status, statusError, changes, changesError, blockers, busy, dirty, onApply, onRefresh }: Props) {
  const [review, setReview] = useState(false)
  const [acting, setActing] = useState(false)
  const v = pendingView(changes, busy)
  const active = status?.service.active ?? false

  const check = async () => {
    setActing(true)
    try {
      const r = await api.check()
      showToast(r.ok ? `HAProxy accepts the candidate configuration. ${r.message}`.trim() : `HAProxy rejected the candidate configuration: ${r.message}`, r.ok ? 'success' : 'error')
    } catch (e) {
      showToast(`Check failed: ${errMsg(e)}`, 'error')
    } finally {
      setActing(false)
    }
  }

  const service = async (action: 'reload' | 'restart' | 'start') => {
    if (serviceActionNeedsConfirm(action, active)) {
      const msg = action === 'restart'
        ? 'Restart HAProxy? Active connections will be dropped.'
        : 'HAProxy is not running. Start it now?'
      if (!(await confirmDialog(msg, { confirmLabel: action === 'restart' ? 'Restart' : 'Start' }))) {
        showToast(`${action === 'restart' ? 'Restart' : 'Start'} cancelled.`, 'notice')
        return
      }
    }
    setActing(true)
    try {
      await api[action]()
      showToast(action === 'reload' ? 'HAProxy reloaded.' : action === 'restart' ? 'HAProxy restarted.' : 'HAProxy started.', 'success')
    } catch (e) {
      showToast(`${action} failed: ${errMsg(e)}`, 'error')
    } finally {
      setActing(false)
      onRefresh()
    }
  }

  const last = status?.lastApply
  return (
    <section className="pendingbar">
      <div className="row">
        <span className={`pending-label ${changes?.hasChanges ? 'unsaved' : 'muted'}`}>{v.label}</span>
        {v.summary && <span className="muted">{v.summary}</span>}
        {dirty && <span className="muted">Unsaved edits are not included until you Save.</span>}
        <span className="grow" />
        <button className="btn btn-ghost btn-sm" disabled={!v.canReview} onClick={() => setReview(r => !r)}>{review ? 'Hide changes' : 'Review changes'}</button>
        <button className="btn btn-ghost btn-sm" disabled={acting} onClick={check}>Check</button>
        <button className="btn btn-primary btn-sm" disabled={v.applyDisabled || acting} onClick={onApply}>{v.applyLabel}</button>
      </div>
      {changesError && <p className="error">Could not read pending changes: {changesError}</p>}
      <IssueList issues={blockers} />
      {review && changes && (
        <div className="stack review">
          <DiffView title="haproxy.cfg" diff={changes.configDiff} />
          <DiffView title="crt-list" diff={changes.crtListDiff} />
        </div>
      )}
      {diagnosticNotices(status?.diagnostics).map((n, i) => <p key={i} className="diag-notice" role="note">{n.message}</p>)}
      <div className="row statuspanel">
        {status ? (
          <>
            <span className={`badge ${status.service.active ? 'badge-ok' : 'badge-error'}`}>{status.service.active ? 'active' : 'inactive'}</span>
            {status.service.detail && <span className="muted">{status.service.detail}</span>}
            <span className="muted">HAProxy {status.version || 'version unknown'}</span>
            <span className="muted">{last ? `last apply: ${last.outcome} at ${new Date(last.time).toLocaleString()}` : 'never applied'}</span>
            {last && last.message && <span className="muted" title={last.message}>{last.message.slice(0, 80)}</span>}
          </>
        ) : <span className="muted">{statusError ? `Status unavailable: ${statusError}` : 'Loading status…'}</span>}
        <span className="grow" />
        <button className="btn btn-ghost btn-sm" disabled={acting} onClick={() => service('reload')}>Reload</button>
        <button className="btn btn-ghost btn-sm" disabled={acting} onClick={() => service('restart')}>Restart</button>
        <button className="btn btn-ghost btn-sm" disabled={acting} onClick={() => service('start')}>Start</button>
      </div>
    </section>
  )
}
