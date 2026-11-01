import {
  forwardRef,
  isValidElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type CSSProperties,
  type InputHTMLAttributes,
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  type TextareaHTMLAttributes,
} from 'react'
import { createPortal } from 'react-dom'
import clsx from 'clsx'
import { AlertCircle, Check, CheckCircle2, ChevronDown, Loader2, X } from 'lucide-react'
import { statusTone } from '../format'

export function Button({
  variant = 'default',
  size = 'md',
  loading,
  icon,
  className,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'default' | 'primary' | 'ghost' | 'subtle' | 'danger'
  size?: 'sm' | 'md' | 'lg'
  loading?: boolean
  icon?: ReactNode
}) {
  return (
    <button
      {...props}
      disabled={props.disabled || loading}
      className={clsx('btn', `btn-${variant}`, `btn-${size}`, className)}
    >
      {loading ? <Loader2 className="spin" size={15} /> : icon}
      {children && <span>{children}</span>}
    </button>
  )
}

export function IconButton({
  label,
  className,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { label: string }) {
  return (
    <button {...props} aria-label={label} title={label} className={clsx('icon-btn', className)}>
      {children}
    </button>
  )
}

export function Badge({ children, tone, dot, className }: { children?: ReactNode; tone?: string; dot?: boolean; className?: string }) {
  return (
    <span className={clsx('badge', tone && `badge-${tone}`, className)}>
      {dot && <span className="badge-dot" />}
      {children}
    </span>
  )
}

export function StatusBadge({ status, className }: { status?: string; className?: string }) {
  const label = (status || 'idle').replace(/_/g, ' ')
  return (
    <Badge tone={statusTone(status)} dot className={className}>
      {label}
    </Badge>
  )
}

export function Spinner({ size = 18 }: { size?: number }) {
  return <Loader2 className="spin muted" size={size} />
}

export function Kbd({ children }: { children: ReactNode }) {
  return <kbd className="kbd">{children}</kbd>
}

export function PageHeader({
  eyebrow,
  title,
  description,
  actions,
  back,
}: {
  eyebrow?: ReactNode
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  back?: ReactNode
}) {
  return (
    <header className="page-header">
      <div className="page-heading">
        {back}
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <h1>{title}</h1>
        {description && <p className="page-description">{description}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  )
}

export function EmptyState({
  icon,
  title,
  description,
  action,
  compact,
}: {
  icon?: ReactNode
  title: string
  description?: ReactNode
  action?: ReactNode
  compact?: boolean
}) {
  return (
    <div className={clsx('empty-state', compact && 'compact')}>
      {icon && <div className="empty-icon">{icon}</div>}
      <h3>{title}</h3>
      {description && <p>{description}</p>}
      {action && <div className="empty-action">{action}</div>}
    </div>
  )
}

export function Modal({
  open,
  onClose,
  title,
  subtitle,
  children,
  footer,
  width = 620,
  className,
}: {
  open: boolean
  onClose: () => void
  title?: ReactNode
  subtitle?: ReactNode
  children: ReactNode
  footer?: ReactNode
  width?: number
  className?: string
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    const prev = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = prev
    }
  }, [open, onClose])
  if (!open) return null
  return createPortal(
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={clsx('modal', className)} style={{ maxWidth: width }} role="dialog" aria-modal="true">
        {(title || subtitle) && (
          <div className="modal-head">
            <div>
              {title && <h2>{title}</h2>}
              {subtitle && <p>{subtitle}</p>}
            </div>
            <IconButton label="Close" onClick={onClose}>
              <X size={17} />
            </IconButton>
          </div>
        )}
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>,
    document.body,
  )
}

export function Drawer({
  open,
  onClose,
  title,
  subtitle,
  children,
  footer,
  width = 480,
}: {
  open: boolean
  onClose: () => void
  title?: ReactNode
  subtitle?: ReactNode
  children: ReactNode
  footer?: ReactNode
  width?: number
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [open, onClose])
  if (!open) return null
  return createPortal(
    <div className="drawer-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <aside className="drawer" style={{ width }} role="dialog" aria-modal="true">
        <div className="drawer-head">
          <div>
            {title && <h2>{title}</h2>}
            {subtitle && <p>{subtitle}</p>}
          </div>
          <IconButton label="Close" onClick={onClose}>
            <X size={17} />
          </IconButton>
        </div>
        <div className="drawer-body">{children}</div>
        {footer && <div className="drawer-footer">{footer}</div>}
      </aside>
    </div>,
    document.body,
  )
}

export function Tabs<T extends string>({
  tabs,
  value,
  onChange,
  className,
}: {
  tabs: { id: T; label: ReactNode; count?: number }[]
  value: T
  onChange: (id: T) => void
  className?: string
}) {
  return (
    <div className={clsx('tabs', className)} role="tablist">
      {tabs.map((t) => (
        <button
          key={t.id}
          role="tab"
          aria-selected={value === t.id}
          className={clsx('tab', value === t.id && 'active')}
          onClick={() => onChange(t.id)}
        >
          {t.label}
          {t.count !== undefined && <span className="tab-count">{t.count}</span>}
        </button>
      ))}
    </div>
  )
}

export function Field({
  label,
  hint,
  error,
  children,
  required,
}: {
  label?: ReactNode
  hint?: ReactNode
  error?: ReactNode
  children: ReactNode
  required?: boolean
}) {
  return (
    <label className="field">
      {label && (
        <span className="field-label">
          {label} {required && <span className="required">*</span>}
        </span>
      )}
      {children}
      {hint && !error && <span className="field-hint">{hint}</span>}
      {error && <span className="field-error">{error}</span>}
    </label>
  )
}

export function TextInput({ className, ...props }: InputHTMLAttributes<HTMLInputElement>) {
  return <input {...props} className={clsx('input', className)} />
}

export const TextArea = forwardRef<HTMLTextAreaElement, TextareaHTMLAttributes<HTMLTextAreaElement>>(
  function TextArea({ className, ...props }, ref) {
    return <textarea ref={ref} {...props} className={clsx('textarea', className)} />
  },
)

export interface SelectOption {
  value: string
  label: ReactNode
  disabled?: boolean
}

function optionText(node: ReactNode): string {
  if (typeof node === 'string' || typeof node === 'number') return String(node)
  if (Array.isArray(node)) return node.map(optionText).join('')
  if (isValidElement<{ children?: ReactNode }>(node)) return optionText(node.props.children)
  return ''
}

function firstEnabled(options: SelectOption[]): number {
  return options.findIndex((option) => !option.disabled)
}

function nearbyEnabled(options: SelectOption[], start: number, step: 1 | -1): number {
  if (options.length === 0) return -1
  let index = Math.max(0, Math.min(start, options.length - 1))
  for (let seen = 0; seen < options.length; seen += 1) {
    if (!options[index].disabled) return index
    index = (index + step + options.length) % options.length
  }
  return -1
}

export function Select({
  value,
  options,
  onChange,
  className,
  disabled,
  placeholder = 'Select…',
  'aria-label': ariaLabel,
}: {
  value: string
  options: SelectOption[]
  onChange: (value: string) => void
  className?: string
  disabled?: boolean
  placeholder?: string
  'aria-label'?: string
}) {
  const listId = useId()
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const typeaheadRef = useRef({ query: '', timer: 0 })
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(-1)
  const [menuStyle, setMenuStyle] = useState<CSSProperties | null>(null)
  const selected = options.find((option) => option.value === value)

  const positionMenu = useCallback(() => {
    const trigger = triggerRef.current
    const menu = menuRef.current
    if (!trigger || !menu) return
    const anchor = trigger.getBoundingClientRect()
    const margin = 8
    const gap = 5
    const viewportWidth = window.innerWidth
    const viewportHeight = window.innerHeight
    const width = Math.min(Math.max(anchor.width, 190), viewportWidth - margin * 2)
    const menuHeight = menu.getBoundingClientRect().height
    const roomBelow = viewportHeight - anchor.bottom - gap - margin
    const roomAbove = anchor.top - gap - margin
    const flip = menuHeight > roomBelow && roomAbove > roomBelow
    const maxHeight = Math.max(120, Math.min(flip ? roomAbove : roomBelow, 320))
    const visibleHeight = Math.min(menuHeight, maxHeight)
    setMenuStyle({
      left: Math.max(margin, Math.min(anchor.left, viewportWidth - width - margin)),
      top: flip ? Math.max(margin, anchor.top - gap - visibleHeight) : anchor.bottom + gap,
      width,
      maxHeight,
    })
  }, [])

  useLayoutEffect(() => {
    if (open) positionMenu()
  }, [open, options.length, positionMenu])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node
      if (triggerRef.current?.contains(target) || menuRef.current?.contains(target)) return
      setOpen(false)
      setMenuStyle(null)
    }
    const onScroll = (event: Event) => {
      if (menuRef.current?.contains(event.target as Node)) return
      setOpen(false)
      setMenuStyle(null)
    }
    const onResize = () => positionMenu()
    document.addEventListener('pointerdown', onPointerDown, true)
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('resize', onResize)
    return () => {
      document.removeEventListener('pointerdown', onPointerDown, true)
      window.removeEventListener('scroll', onScroll, true)
      window.removeEventListener('resize', onResize)
    }
  }, [open, positionMenu])

  useEffect(() => {
    if (!open || active < 0) return
    menuRef.current
      ?.querySelector<HTMLElement>(`[data-index="${active}"]`)
      ?.scrollIntoView({ block: 'nearest' })
  }, [open, active])

  useEffect(() => () => window.clearTimeout(typeaheadRef.current.timer), [])

  function closeMenu(refocus = false) {
    setOpen(false)
    setMenuStyle(null)
    if (refocus) triggerRef.current?.focus()
  }

  function openMenu(initial: 'selected' | 'first' | 'last' = 'selected') {
    if (disabled) return
    if (initial === 'last') {
      setActive(nearbyEnabled(options, options.length - 1, -1))
    } else if (initial === 'first') {
      setActive(firstEnabled(options))
    } else {
      const selectedIndex = options.findIndex((option) => option.value === value)
      setActive(selectedIndex >= 0 ? nearbyEnabled(options, selectedIndex, 1) : firstEnabled(options))
    }
    setOpen(true)
  }

  function choose(option?: SelectOption) {
    if (!option || option.disabled) return
    if (option.value !== value) onChange(option.value)
    closeMenu(true)
  }

  function typeahead(key: string) {
    const now = typeaheadRef.current
    window.clearTimeout(now.timer)
    now.query += key.toLowerCase()
    now.timer = window.setTimeout(() => {
      typeaheadRef.current.query = ''
    }, 600)
    const start = active >= 0 ? active + 1 : 0
    const ordered = [...options.slice(start), ...options.slice(0, start)]
    const match = ordered.find((option) => !option.disabled && optionText(option.label).toLowerCase().startsWith(now.query))
    if (match) setActive(options.indexOf(match))
  }

  function onKeyDown(event: ReactKeyboardEvent<HTMLButtonElement>) {
    if (disabled) return
    if (!open) {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp' || event.key === 'Enter' || event.key === ' ') {
        event.preventDefault()
        openMenu(event.key === 'ArrowUp' ? 'last' : 'selected')
      }
      return
    }
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      closeMenu(true)
      return
    }
    if (event.key === 'Tab') {
      closeMenu()
      return
    }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      event.stopPropagation()
      setActive((current) => nearbyEnabled(options, current + (event.key === 'ArrowDown' ? 1 : -1), event.key === 'ArrowDown' ? 1 : -1))
      return
    }
    if (event.key === 'Home') {
      event.preventDefault()
      setActive(firstEnabled(options))
      return
    }
    if (event.key === 'End') {
      event.preventDefault()
      setActive(nearbyEnabled(options, options.length - 1, -1))
      return
    }
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      event.stopPropagation()
      choose(options[active])
      return
    }
    if (event.key.length === 1 && !event.metaKey && !event.ctrlKey && !event.altKey) {
      event.preventDefault()
      typeahead(event.key)
    }
  }

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        role="combobox"
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? listId : undefined}
        aria-activedescendant={open && active >= 0 ? `${listId}-option-${active}` : undefined}
        className={clsx('select', 'select-trigger', className)}
        disabled={disabled}
        onClick={() => (open ? closeMenu() : openMenu())}
        onKeyDown={onKeyDown}
      >
        <span className={clsx('select-value', !selected && 'select-placeholder')}>{selected?.label ?? placeholder}</span>
        <ChevronDown className="select-chevron" size={14} aria-hidden />
      </button>
      {open &&
        createPortal(
          <div
            ref={menuRef}
            id={listId}
            role="listbox"
            aria-label={ariaLabel}
            className="select-menu"
            style={menuStyle ?? { visibility: 'hidden' }}
          >
            {options.length === 0 && <div className="select-empty">No options</div>}
            {options.map((option, index) => (
              <div
                key={option.value}
                id={`${listId}-option-${index}`}
                data-index={index}
                role="option"
                aria-selected={option.value === value}
                aria-disabled={option.disabled || undefined}
                className={clsx(
                  'select-option',
                  index === active && 'active',
                  option.value === value && 'selected',
                  option.disabled && 'disabled',
                )}
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => {
                  if (!option.disabled) setActive(index)
                }}
                onClick={() => choose(option)}
              >
                <span className="select-option-label">{option.label}</span>
                {option.value === value && <Check className="select-check" size={13} aria-hidden />}
              </div>
            ))}
          </div>,
          document.body,
        )}
    </>
  )
}

export function Toggle({
  checked,
  onChange,
  label,
  description,
  disabled,
}: {
  checked: boolean
  onChange: (next: boolean) => void
  label?: ReactNode
  description?: ReactNode
  disabled?: boolean
}) {
  return (
    <label className={clsx('toggle-row', disabled && 'disabled')}>
      {(label || description) && (
        <span className="toggle-copy">
          {label && <span className="toggle-label">{label}</span>}
          {description && <span className="toggle-desc">{description}</span>}
        </span>
      )}
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        disabled={disabled}
        className={clsx('toggle', checked && 'on')}
        onClick={() => onChange(!checked)}
      >
        <span className="toggle-thumb" />
      </button>
    </label>
  )
}

export function InlineNotice({
  tone = 'info',
  title,
  children,
}: {
  tone?: 'info' | 'warn' | 'bad' | 'ok'
  title?: ReactNode
  children?: ReactNode
}) {
  return (
    <div className={clsx('inline-notice', `notice-${tone}`)}>
      {tone === 'ok' ? <CheckCircle2 size={16} /> : <AlertCircle size={16} />}
      <div>
        {title && <strong>{title}</strong>}
        {children && <div>{children}</div>}
      </div>
    </div>
  )
}

export function CopyButton({ text, label = 'Copy' }: { text: string; label?: string }) {
  const timer = useRef<number | null>(null)
  useEffect(() => () => {
    if (timer.current) window.clearTimeout(timer.current)
  }, [])
  return (
    <button
      className="link-btn small"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
        } catch {
          // ignore
        }
      }}
      title={label}
    >
      {label}
    </button>
  )
}

export function SectionTitle({ children, hint, actions }: { children: ReactNode; hint?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="section-title">
      <div>
        <h2>{children}</h2>
        {hint && <p>{hint}</p>}
      </div>
      {actions}
    </div>
  )
}

export function FormError({ children }: { children?: ReactNode }) {
  if (!children) return null
  return <div className="form-error">{children}</div>
}

export function LabeledValue({ label, children, mono }: { label: ReactNode; children: ReactNode; mono?: boolean }) {
  return (
    <div className="labeled-value">
      <span>{label}</span>
      <div className={mono ? 'mono' : ''}>{children}</div>
    </div>
  )
}

export function useFieldId(prefix?: string) {
  const id = useId()
  return prefix ? `${prefix}-${id}` : id
}
