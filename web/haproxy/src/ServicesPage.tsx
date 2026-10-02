import { useEffect, useState } from 'react'
import { showToast } from '@shared'
import type { CertRow, CoverageResult, Model, Service } from './api'
import { coverageBadge } from './certview'
import type { GroupedIssues } from './issues'
import { IssueList } from './IssueList'
import { Toggle } from './Toggle'
import {
  addFqdn, addPort, addService, extraToText, groupByPort, portDefaultOptions, removeFqdn, removePort,
  removeService, setFqdn, setPortDefault, setServiceEnabled, setUpstreamHost, textToExtra,
  updateDefaultService, updateService, DEFAULT_UPSTREAM_HOST, newModelId,
} from './modelForm'

interface Props {
  model: Model
  certs: CertRow[]
  issues: GroupedIssues
  coverage: CoverageResult[]
  onPickCert: (svc: Service) => void
  onChange: (m: Model) => void
}

/** Extra directives: the raw text is local so blank lines can be typed. */
function ExtraField({ extra, onChange }: { extra: string[]; onChange: (e: string[]) => void }) {
  const [text, setText] = useState(extraToText(extra))
  useEffect(() => {
    if (textToExtra(text).join('\n') !== extra.join('\n')) setText(extraToText(extra))
  }, [extra]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <textarea
      className="input extra"
      rows={3}
      value={text}
      placeholder="one directive per line, e.g. timeout server 2h"
      onChange={e => { setText(e.target.value); onChange(textToExtra(e.target.value)) }}
    />
  )
}

function FqdnList({ svc, model, onChange }: { svc: Service; model: Model; onChange: (m: Model) => void }) {
  const [draft, setDraft] = useState('')
  const add = () => {
    if (!draft.trim()) return
    onChange(addFqdn(model, svc.id, draft))
    setDraft('')
  }
  return (
    <div className="stack">
      {svc.fqdns.map((f, i) => (
        <div className="row" key={i}>
          <input className="input grow" value={f} onChange={e => onChange(setFqdn(model, svc.id, i, e.target.value))} aria-label="FQDN" />
          <button className="btn btn-danger btn-sm" onClick={() => onChange(removeFqdn(model, svc.id, i))} title="Remove FQDN">✕</button>
        </div>
      ))}
      <div className="row">
        <input
          className="input grow"
          placeholder="host.example.com"
          value={draft}
          onChange={e => setDraft(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') add() }}
          aria-label="New FQDN"
        />
        <button className="btn btn-primary btn-sm" onClick={add}>+ Add FQDN</button>
      </div>
    </div>
  )
}

function certOptions(certs: CertRow[], current: string) {
  const byFqdn = new Map<string, boolean>()
  for (const c of certs) {
    const f = c.certmachine.fqdn
    byFqdn.set(f, (byFqdn.get(f) ?? false) || c.enabled)
  }
  const opts = [...byFqdn.entries()].map(([fqdn, enabled]) => ({ fqdn, label: enabled ? fqdn : `${fqdn} (disabled)` }))
  if (current && !byFqdn.has(current)) opts.push({ fqdn: current, label: `${current} (not installed)` })
  return opts
}

interface EditorProps {
  svc: Service
  model: Model
  certs: CertRow[]
  issues: GroupedIssues
  coverage: CoverageResult | undefined
  onPickCert: (svc: Service) => void
  open: boolean
  onToggleOpen: () => void
  onChange: (m: Model) => void
}

function ServiceEditor({ svc, model, certs, issues, coverage, onPickCert, open, onToggleOpen, onChange }: EditorProps) {
  const mine = issues.byService[svc.id]
  const errors = (mine ?? []).filter(i => i.severity === 'error').length
  const ports = groupByPort(model).map(g => g.port)
  const patch = (p: Partial<Service>) => onChange(updateService(model, svc.id, p))
  const cov = coverageBadge(coverage)
  return (
    <div className={`svc${svc.enabled ? '' : ' svc-off'}`}>
      <div className="svc-head" onClick={onToggleOpen}>
        <span className="svc-name">{svc.name || '(unnamed)'}</span>
        <span className="muted svc-summary">{svc.fqdns.join(', ') || 'no FQDN'} → {svc.upstream.host || DEFAULT_UPSTREAM_HOST}:{svc.upstream.port}</span>
        {!svc.enabled && <span className="badge">disabled</span>}
        {cov && <span className={`badge badge-${cov.tone}`} title={cov.title}>{cov.label}</span>}
        {mine && mine.length > 0 && (
          <span className={`badge ${errors > 0 ? 'badge-error' : 'badge-warning'}`}>{errors > 0 ? 'error' : 'warning'}</span>
        )}
        <span className="svc-caret">{open ? '▾' : '▸'}</span>
      </div>
      <IssueList issues={mine} />
      {open && (
        <div className="svc-body">
          <div className="field">
            <label className="field-label">Name</label>
            <input className="input" value={svc.name} onChange={e => patch({ name: e.target.value })} />
          </div>
          <div className="field">
            <Toggle checked={svc.enabled} onChange={v => onChange(setServiceEnabled(model, svc.id, v))} label="Enabled" />
          </div>
          <div className="field">
            <label className="field-label">FQDNs</label>
            <FqdnList svc={svc} model={model} onChange={onChange} />
          </div>
          <div className="field">
            <label className="field-label">Exposed port</label>
            <select className="input" value={svc.exposedPort || 443} onChange={e => patch({ exposedPort: Number(e.target.value) })}>
              {ports.map(p => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
          <div className="field">
            <label className="field-label">Upstream (host and port)</label>
            <div className="row">
              <input
                className="input grow"
                value={svc.upstream.host}
                placeholder={DEFAULT_UPSTREAM_HOST}
                onChange={e => onChange(setUpstreamHost(model, svc.id, e.target.value))}
                aria-label="Upstream host"
              />
              <input
                className="input port"
                inputMode="numeric"
                value={svc.upstream.port || ''}
                onChange={e => patch({ upstream: { ...svc.upstream, port: Number(e.target.value.replace(/\D/g, '')) || 0 } })}
                aria-label="Upstream port"
              />
            </div>
          </div>
          <div className="field">
            <Toggle checked={svc.check} onChange={v => patch({ check: v })} label="Health check" />
          </div>
          <div className="field">
            <label className="field-label">Certificate</label>
            <div className="row">
              <select className="input grow" value={svc.cert.fqdn} onChange={e => patch({ cert: { fqdn: e.target.value } })}>
                <option value="">(none)</option>
                {certOptions(certs, svc.cert.fqdn).map(o => <option key={o.fqdn} value={o.fqdn}>{o.label}</option>)}
              </select>
              <button className="btn btn-ghost btn-sm" onClick={() => onPickCert(svc)}>Pick from CertMachine</button>
            </div>
          </div>
          <div className="field">
            <label className="field-label">Extra directives</label>
            <ExtraField extra={svc.extra} onChange={extra => patch({ extra })} />
          </div>
          <button className="btn btn-danger btn-sm" onClick={() => onChange(removeService(model, svc.id))}>Delete service</button>
        </div>
      )}
    </div>
  )
}

export function ServicesPage({ model, certs, issues, coverage, onPickCert, onChange }: Props) {
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [newPort, setNewPort] = useState('')

  const toggleOpen = (id: string) =>
    setOpen(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const add = (port: number) => {
    setOpen(prev => new Set(prev).add(newModelId(model)))
    onChange(addService(model, port))
  }

  const addNewPort = () => {
    const r = addPort(model, Number(newPort))
    if (r.error) {
      showToast(r.error, 'error')
      return
    }
    onChange(r.model)
    setNewPort('')
    showToast(`Port ${newPort} added. Add a service to it, then Save.`, 'notice')
  }

  const dropPort = (port: number) => {
    const r = removePort(model, port)
    if (r.error) showToast(r.error, 'error')
    else onChange(r.model)
  }

  const chooseDefault = (port: number, name: string) => {
    const r = setPortDefault(model, port, name)
    if (r.error) showToast(r.error, 'error')
    else onChange(r.model)
  }

  const ds = model.defaultService
  return (
    <div>
      <IssueList issues={issues.other} />
      {groupByPort(model).map(g => (
        <div className="card" key={g.port}>
          <div className="card-title">
            <span>Port {g.port}{g.isDefaultPort ? ' (default)' : ''}</span>
            <span className="grow" />
            {!g.isDefaultPort && (
              <button className="btn btn-ghost btn-sm" onClick={() => dropPort(g.port)} title="Remove this port (only while it has no services)">Remove port</button>
            )}
          </div>
          <IssueList issues={issues.byPort[g.port]} />

          {g.isDefaultPort ? (
            <div className="svc svc-default">
              <div className="svc-head">
                <span className="svc-name">{ds.name}</span>
                <span className="badge">default service</span>
                <span className="muted svc-summary">receives every :443 request that matches no host rule; cannot be deleted</span>
              </div>
              <div className="svc-body">
                <div className="field">
                  <label className="field-label">Upstream (host and port)</label>
                  <div className="row">
                    <input
                      className="input grow"
                      value={ds.upstream.host}
                      onChange={e => onChange(updateDefaultService(model, { upstream: { ...ds.upstream, host: e.target.value } }))}
                      aria-label="Default service upstream host"
                    />
                    <input
                      className="input port"
                      inputMode="numeric"
                      value={ds.upstream.port || ''}
                      onChange={e => onChange(updateDefaultService(model, { upstream: { ...ds.upstream, port: Number(e.target.value.replace(/\D/g, '')) || 0 } }))}
                      aria-label="Default service upstream port"
                    />
                  </div>
                </div>
                <div className="field">
                  <Toggle checked={ds.check} onChange={v => onChange(updateDefaultService(model, { check: v }))} label="Health check" />
                </div>
              </div>
            </div>
          ) : (
            <div className="field">
              <label className="field-label">Default service for this port</label>
              <select className="input" value={g.defaultServiceName} onChange={e => chooseDefault(g.port, e.target.value)}>
                <option value="">(none)</option>
                {portDefaultOptions(model, g.port).map(n => <option key={n} value={n}>{n}</option>)}
                {g.defaultServiceName && !portDefaultOptions(model, g.port).includes(g.defaultServiceName) && (
                  <option value={g.defaultServiceName}>{g.defaultServiceName} (not on this port)</option>
                )}
              </select>
            </div>
          )}

          {g.services.map(s => (
            <ServiceEditor
              key={s.id}
              svc={s}
              model={model}
              certs={certs}
              issues={issues}
              coverage={coverage.find(c => c.service === s.name)}
              onPickCert={onPickCert}
              open={open.has(s.id)}
              onToggleOpen={() => toggleOpen(s.id)}
              onChange={onChange}
            />
          ))}
          <button className="btn btn-primary btn-sm" onClick={() => add(g.port)}>+ Add service on port {g.port}</button>
        </div>
      ))}

      <div className="card">
        <div className="card-title">Add port</div>
        <div className="row">
          <input
            className="input port"
            inputMode="numeric"
            placeholder="8443"
            value={newPort}
            onChange={e => setNewPort(e.target.value.replace(/\D/g, ''))}
            onKeyDown={e => { if (e.key === 'Enter') addNewPort() }}
            aria-label="New port"
          />
          <button className="btn btn-primary btn-sm" onClick={addNewPort}>+ Add port</button>
        </div>
      </div>
    </div>
  )
}
