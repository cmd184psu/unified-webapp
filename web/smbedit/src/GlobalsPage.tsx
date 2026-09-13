import { useState } from 'react'
import { GlobalEntry } from './api'

interface Props {
  globals: GlobalEntry[]
  onChange: (g: GlobalEntry[]) => void
}

export function GlobalsPage({ globals, onChange }: Props) {
  const [newKey, setNewKey] = useState('')
  const [newVal, setNewVal] = useState('')

  const update = (i: number, field: 'key' | 'value', v: string) => {
    const next = [...globals]
    next[i] = { ...next[i], [field]: v }
    onChange(next)
  }

  const remove = (i: number) => onChange(globals.filter((_, idx) => idx !== i))

  const add = () => {
    const k = newKey.trim()
    const v = newVal.trim()
    if (!k) return
    onChange([...globals, { key: k, value: v }])
    setNewKey('')
    setNewVal('')
  }

  return (
    <div>
      <div className="page-header">
        <div className="page-title">Global settings</div>
        <div className="page-subtitle">
          Key/value pairs written to the <code>[global]</code> section of smb.conf.
        </div>
      </div>

      <div className="card">
        <table className="globals-table">
          <thead>
            <tr>
              <th style={{ width: '38%' }}>Key</th>
              <th>Value</th>
              <th style={{ width: 60 }} />
            </tr>
          </thead>
          <tbody>
            {globals.map((g, i) => (
              <tr key={i}>
                <td>
                  <input
                    className="input input-sm"
                    value={g.key}
                    onChange={e => update(i, 'key', e.target.value)}
                  />
                </td>
                <td>
                  <input
                    className="input input-sm"
                    value={g.value}
                    onChange={e => update(i, 'value', e.target.value)}
                  />
                </td>
                <td>
                  <div className="globals-row-actions">
                    <button className="btn btn-danger btn-sm" onClick={() => remove(i)} title="Delete row">
                      ✕
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        <div className="divider" />

        <div className="row" style={{ gap: 8 }}>
          <input
            className="input input-sm"
            placeholder="key"
            value={newKey}
            style={{ width: '38%' }}
            onChange={e => setNewKey(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') add() }}
          />
          <input
            className="input input-sm"
            placeholder="value"
            value={newVal}
            style={{ flex: 1 }}
            onChange={e => setNewVal(e.target.value)}
            onKeyDown={e => { if (e.key === 'Enter') add() }}
          />
          <button className="btn btn-primary btn-sm" onClick={add}>+ Add</button>
        </div>
      </div>
    </div>
  )
}
