import { useState, useCallback, createContext, useContext, ReactNode } from 'react'

export type ToastKind = 'info' | 'success' | 'error' | 'warning'

interface Toast {
  id: number
  kind: ToastKind
  title: string
  message?: string
}

interface ToastCtx {
  toast: (title: string, message?: string, kind?: ToastKind) => void
}

const Ctx = createContext<ToastCtx>({ toast: () => {} })

let seq = 0

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const toast = useCallback((title: string, message?: string, kind: ToastKind = 'info') => {
    const id = ++seq
    setToasts(p => [...p, { id, kind, title, message }])
    const duration = kind === 'error' || kind === 'warning' ? 12000 : 4000
    setTimeout(() => setToasts(p => p.filter(t => t.id !== id)), duration)
  }, [])

  return (
    <Ctx.Provider value={{ toast }}>
      {children}
      <div className="toast-stack">
        {toasts.map(t => (
          <div key={t.id} className={`toast ${t.kind}`}>
            <div className="toast-body">
              <div className="toast-title">{t.title}</div>
              {t.message && <div className="toast-msg">{t.message}</div>}
            </div>
            <button
              className="btn-icon btn"
              style={{ padding: '2px 5px', fontSize: 13 }}
              onClick={() => setToasts(p => p.filter(x => x.id !== t.id))}
            >×</button>
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}

export const useToast = () => useContext(Ctx)
