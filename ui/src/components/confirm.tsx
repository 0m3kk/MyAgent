import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'
import { AlertTriangle, CheckCircle2 } from 'lucide-react'
import { Button, Modal } from './ui'

export interface ConfirmOptions {
  title: string
  message: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  tone?: 'danger' | 'primary'
}

interface ConfirmState {
  options: ConfirmOptions
  resolve: (value: boolean) => void
}

const ConfirmContext = createContext<((options: ConfirmOptions) => Promise<boolean>) | null>(null)

export function ConfirmProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<ConfirmState | null>(null)
  const current = useRef<ConfirmState | null>(null)

  const confirm = useCallback((options: ConfirmOptions) => {
    return new Promise<boolean>((resolve) => {
      current.current?.resolve(false)
      const next = { options, resolve }
      current.current = next
      setState(next)
    })
  }, [])

  const close = useCallback((value: boolean) => {
    const active = current.current
    current.current = null
    active?.resolve(value)
    setState(null)
  }, [])

  const cancel = useCallback(() => close(false), [close])
  const value = useMemo(() => confirm, [confirm])
  const tone = state?.options.tone ?? 'danger'

  return (
    <ConfirmContext.Provider value={value}>
      {children}
      <Modal
        open={!!state}
        onClose={cancel}
        title={state?.options.title}
        width={480}
        className="confirm-dialog"
        footer={state ? (
          <>
            <Button variant="ghost" onClick={cancel} autoFocus={tone === 'danger'}>
              {state.options.cancelLabel || 'Cancel'}
            </Button>
            <Button
              variant={tone === 'danger' ? 'danger' : 'primary'}
              onClick={() => close(true)}
              autoFocus={tone !== 'danger'}
            >
              {state.options.confirmLabel || 'Confirm'}
            </Button>
          </>
        ) : undefined}
      >
        {state && (
          <div className="confirm-body">
            <span className={`confirm-icon ${tone === 'primary' ? 'primary' : ''}`}>
              {tone === 'primary' ? <CheckCircle2 size={18} /> : <AlertTriangle size={18} />}
            </span>
            <p className="confirm-message">{state.options.message}</p>
          </div>
        )}
      </Modal>
    </ConfirmContext.Provider>
  )
}

export function useConfirm() {
  const confirm = useContext(ConfirmContext)
  if (!confirm) throw new Error('useConfirm must be used inside ConfirmProvider')
  return confirm
}
