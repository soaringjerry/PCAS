import type { ButtonHTMLAttributes, ReactNode } from 'react'
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

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label: string }) {
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={label} className="switch" onClick={() => onChange(!checked)} />
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
