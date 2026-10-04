/** A modal spinner for the few seconds Check, Apply and the service actions take. */
export function BusyDialog({ message }: { message: string }) {
  return (
    <div className="ui-modal-overlay" role="alertdialog" aria-modal="true" aria-busy="true" aria-label={message}>
      <div className="ui-modal-panel busy-panel">
        <span className="spinner" aria-hidden="true" />
        <span>{message}</span>
      </div>
    </div>
  )
}
