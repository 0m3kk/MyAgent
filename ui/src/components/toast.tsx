import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { AlertTriangle, CheckCircle2, Info, X } from 'lucide-react'

export type ToastTone = 'info' | 'success' | 'error'

interface ToastItem {
  id: number
  title: string
  message?: string
  tone: ToastTone
}

interface ToastContextValue {
  push: (title: string, message?: string, tone?: ToastTone) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)
let nextId = 1

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const push = useCallback((title: string, message?: string, tone: ToastTone = 'info') => {
    const id = nextId++
    setItems((prev) => [...prev.slice(-4), { id, title, message, tone }])
    window.setTimeout(() => setItems((prev) => prev.filter((t) => t.id !== id)), tone === 'error' ? 8000 : 4800)
  }, [])
  const remove = (id: number) => setItems((prev) => prev.filter((t) => t.id !== id))
  const value = useMemo(() => ({ push }), [push])
  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toast-stack" role="status" aria-live="polite">
        {items.map((t) => (
          <div key={t.id} className={`toast toast-${t.tone}`}>
            <span className="toast-icon">
              {t.tone === 'success' ? <CheckCircle2 size={16} /> : t.tone === 'error' ? <AlertTriangle size={16} /> : <Info size={16} />}
            </span>
            <div className="toast-body">
              <div className="toast-title">{t.title}</div>
              {t.message && <div className="toast-message">{t.message}</div>}
            </div>
            <button className="icon-btn tiny" onClick={() => remove(t.id)} aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used inside ToastProvider')
  return ctx.push
}
