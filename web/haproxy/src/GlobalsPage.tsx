import { useState } from 'react'
import { Toggle } from './Toggle'
import type { Directive } from './api'
import {
  COMMON_KEYS, addDirective, updateDirective, removeDirective, moveDirective, isOsManaged,
} from './globalsForm'

interface GroupProps {
  title: string
  hint: string
  rows: Directive[]
  onChange: (rows: Directive[]) => void
  marksManaged: boolean
}

function DirectiveGroup({ title, hint, rows, onChange, marksManaged }: GroupProps) {
  const [newKey, setNewKey] = useState('')
  const [newVal, setNewVal] = useState('')
  const listId = `keys-${title}`

  const add = () => {
    if (!newKey.trim()) return
    onChange(addDirective(rows, newKey, newVal))
    setNewKey('')
    setNewVal('')
  }

  return (
    <div className="card">
      <div className="card-title">{title}</div>
      <p className="muted">{hint}</p>
      <div className="dir-rows">
        {rows.map((d, i) => (
          <div className="dir-row" key={i}>
            <input
              className="input dir-key"
              list={listId}
              value={d.key}
              onChange={e => onChange(updateDirective(rows, i, { key: e.target.value }))}
              aria-label="Key"
            />
            <input
              className="input dir-value"
              value={d.value}
              onChange={e => onChange(updateDirective(rows, i, { value: e.target.value }))}
              aria-label="Value"
            />
            {marksManaged && isOsManaged(d.key) && (
              <span className="badge" title="From the platform driver; a row here overrides it">OS-managed</span>
            )}
            <button className="btn btn-ghost btn-sm" disabled={i === 0} onClick={() => onChange(moveDirective(rows, i, -1))} title="Move up">↑</button>
            <button className="btn btn-ghost btn-sm" disabled={i === rows.length - 1} onClick={() => onChange(moveDirective(rows, i, 1))} title="Move down">↓</button>
            <button className="btn btn-danger btn-sm" onClick={() => onChange(removeDirective(rows, i))} title="Remove row">✕</button>
          </div>
        ))}
        {rows.length === 0 && <p className="muted">No rows.</p>}
      </div>

      <datalist id={listId}>
        {[...new Set(COMMON_KEYS.map(k => k.key))].map(k => <option key={k} value={k} />)}
      </datalist>

      <div className="divider" />
      <div className="dir-row">
        <input
          className="input dir-key"
          list={listId}
          placeholder="key"
          value={newKey}
          onChange={e => setNewKey(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') add() }}
        />
        <input
          className="input dir-value"
          placeholder="value"
          value={newVal}
          onChange={e => setNewVal(e.target.value)}
          onKeyDown={e => { if (e.key === 'Enter') add() }}
        />
        <button className="btn btn-primary btn-sm" onClick={add}>+ Add</button>
      </div>
      <div className="dir-row" style={{ marginTop: 8 }}>
        <select
          className="input"
          value=""
          aria-label="Add a common setting"
          onChange={e => {
            const c = COMMON_KEYS[Number(e.target.value)]
            if (c) onChange(addDirective(rows, c.key, c.value))
          }}
        >
          <option value="">Add a common setting…</option>
          {COMMON_KEYS.map((c, i) => (
            <option key={i} value={i}>{c.key} {c.key === 'timeout' ? c.value.split(' ')[0] : ''}: {c.hint}</option>
          ))}
        </select>
      </div>
    </div>
  )
}

interface Props {
  global: Directive[]
  defaults: Directive[]
  serverClose: boolean
  onServerClose: (v: boolean) => void
  onGlobal: (rows: Directive[]) => void
  onDefaults: (rows: Directive[]) => void
}

export function GlobalsPage({ global, defaults, serverClose, onServerClose, onGlobal, onDefaults }: Props) {
  return (
    <div>
      <div className="card">
        <Toggle checked={serverClose} onChange={onServerClose} label="Close backend connections after each response" />
      </div>
      <DirectiveGroup
        title="global"
        hint="Process-wide, in order."
        rows={global}
        onChange={onGlobal}
        marksManaged
      />
      <DirectiveGroup
        title="defaults"
        hint="Inherited by every frontend and backend."
        rows={defaults}
        onChange={onDefaults}
        marksManaged={false}
      />
    </div>
  )
}
