// Toggle.tsx — the one boolean control: the shared `ui-toggle` markup (a
// visually hidden checkbox driving a switch track). Boolean settings are
// toggle switches, never bare checkboxes (owner rule); this is the only file
// in the module allowed to render the underlying input.

interface Props {
  checked: boolean
  onChange: (v: boolean) => void
  label?: string
  disabled?: boolean
}

export function Toggle({ checked, onChange, label, disabled }: Props) {
  return (
    <label className="ui-toggle">
      <input
        type="checkbox"
        className="ui-toggle-input"
        checked={checked}
        disabled={disabled}
        onChange={e => onChange(e.target.checked)}
      />
      <span className="ui-toggle-track" aria-hidden="true" />
      {label && <span className="ui-toggle-label">{label}</span>}
    </label>
  )
}
