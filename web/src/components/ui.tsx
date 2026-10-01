import { useId, useState, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { Check, ChevronDown, CircleAlert } from 'lucide-react'
import type { Tone } from '../domain/labels'

export function Tag({ tone = 'neutral', children }: { tone?: Tone; children: ReactNode }) {
  return <span className={`tag${tone === 'neutral' ? '' : ` tag-${tone}`}`}>{children}</span>
}

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'default' | 'primary' | 'quiet' | 'danger'
  size?: 'md' | 'sm'
  icon?: ReactNode
}

export function Button({ variant = 'default', size = 'md', icon, className = '', children, ...rest }: ButtonProps) {
  const classes = [
    'btn',
    variant === 'primary' && 'btn-primary',
    (variant === 'quiet' || variant === 'danger') && 'btn-quiet',
    variant === 'danger' && 'btn-danger',
    size === 'sm' && 'btn-sm',
    !children && 'btn-icon',
    className,
  ]
    .filter(Boolean)
    .join(' ')
  return (
    <button type="button" className={classes} {...rest}>
      {icon}
      {children}
    </button>
  )
}

export function Sheet({ title, aside, children, pad = false }: {
  title?: ReactNode
  aside?: ReactNode
  children: ReactNode
  pad?: boolean
}) {
  return (
    <section className="sheet">
      {(title || aside) && (
        <div className="sheet-head">
          {typeof title === 'string' ? <h3>{title}</h3> : title}
          {aside}
        </div>
      )}
      {pad ? <div className="sheet-pad">{children}</div> : children}
    </section>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>
}

export function Field({ label, hint, children, as = 'label' }: { label: string; hint?: ReactNode; children: ReactNode; as?: 'label' | 'div' }) {
  const Tag = as
  return (
    <Tag className="field">
      <span className="field-label">{label}</span>
      {children}
      {hint && <span className="field-hint">{hint}</span>}
    </Tag>
  )
}

export function Spinner({ size = 14 }: { size?: number }) {
  return <span className="spinner" style={{ width: size, height: size }} aria-hidden />
}

export function Switch({ checked, onChange, label, disabled }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={label} className="switch" disabled={disabled} onClick={() => onChange(!checked)} />
  )
}

/** A row that says its current state while closed and opens in place for the details. */
export function Fold({ title, summary, tone, defaultOpen = false, children }: {
  title: ReactNode
  /** Shown while closed too, so a failure or a missing step is never hidden. */
  summary?: ReactNode
  tone?: 'ok' | 'warn' | 'danger'
  defaultOpen?: boolean
  children: ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  const id = useId()
  return (
    <div className={`fold${open ? ' open' : ''}`}>
      <button type="button" className="fold-head" aria-expanded={open} aria-controls={id} onClick={() => setOpen((v) => !v)}>
        <span className="fold-title">{title}</span>
        {summary && <span className={`fold-summary${tone ? ` ${tone}` : ''}`}>{summary}</span>}
        <ChevronDown size={16} className="fold-chevron" aria-hidden />
      </button>
      {open && (
        <div className="fold-body" id={id}>
          {children}
        </div>
      )}
    </div>
  )
}

export type SaveState = 'idle' | 'saving' | 'saved' | 'failed'

/** What happened to the change just made, shown beside the control that made it. */
export function SaveMark({ state, failed = '没保存上，还是原来的设置' }: { state: SaveState; failed?: string }) {
  if (state === 'idle') return null
  if (state === 'failed') {
    return (
      <span className="save-mark failed" role="alert">
        <CircleAlert size={12} />
        {failed}
      </span>
    )
  }
  return (
    <span className={`save-mark ${state}`} role="status">
      {state === 'saving' ? <Spinner size={11} /> : <Check size={12} strokeWidth={3} />}
      {state === 'saving' ? '正在保存' : '已保存'}
    </span>
  )
}

export interface SegItem<T extends string> {
  value: T
  label: string
  count?: number
}

export function Seg<T extends string>({ items, value, onChange, label }: {
  items: SegItem<T>[]
  value: T
  onChange: (v: T) => void
  label: string
}) {
  return (
    <div className="seg" role="radiogroup" aria-label={label}>
      {items.map((item) => (
        <button
          key={item.value}
          type="button"
          role="radio"
          aria-checked={item.value === value}
          className={item.value === value ? 'on' : undefined}
          onClick={() => onChange(item.value)}
        >
          {item.label}
          {item.count !== undefined && <span className="n">{item.count}</span>}
        </button>
      ))}
    </div>
  )
}

export function Progress({ value }: { value: number }) {
  return (
    <div className="progress" role="progressbar" aria-valuenow={Math.round(value * 100)} aria-valuemin={0} aria-valuemax={100}>
      <div style={{ width: `${Math.round(value * 100)}%` }} />
    </div>
  )
}
