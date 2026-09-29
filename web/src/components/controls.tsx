import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type DragEvent,
  type InputHTMLAttributes,
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import { CalendarDays, Check, ChevronDown, ChevronLeft, ChevronRight, ChevronsUpDown, FileText, Minus, Plus, Upload, X } from 'lucide-react'
import { formatDateTime } from '../domain/time'
import { useEscape } from './useEscape'

// Form controls drawn by the app instead of the browser, so menus, dates and
// checkboxes look the same on every platform and in dark mode.

/* ---------------- Popover ---------------- */

/** Floats next to `anchor`, flipping above it when there is no room below. */
export function Popover({ anchor, onClose, children, className = '', matchWidth = false }: {
  anchor: RefObject<HTMLElement | null>
  onClose: () => void
  children: ReactNode
  className?: string
  matchWidth?: boolean
}) {
  const panel = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ top: number; left: number; up: boolean } | null>(null)
  useEscape(onClose)

  useLayoutEffect(() => {
    const place = () => {
      const a = anchor.current?.getBoundingClientRect()
      const el = panel.current
      if (!a || !el) return
      if (matchWidth) el.style.minWidth = `${a.width}px`
      const gap = 6
      const edge = 8
      const h = el.offsetHeight
      const w = el.offsetWidth
      const below = window.innerHeight - a.bottom - gap - edge
      const above = a.top - gap - edge
      const up = h > below && above > below
      const top = up ? Math.max(edge, a.top - gap - h) : Math.min(a.bottom + gap, window.innerHeight - edge - h)
      const left = Math.min(Math.max(edge, a.left), window.innerWidth - edge - w)
      setPos({ top, left, up })
    }
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => {
      window.removeEventListener('resize', place)
      window.removeEventListener('scroll', place, true)
    }
  }, [anchor, matchWidth])

  useEffect(() => {
    const onDown = (e: PointerEvent) => {
      const target = e.target as Node
      if (panel.current?.contains(target) || anchor.current?.contains(target)) return
      onClose()
    }
    document.addEventListener('pointerdown', onDown, true)
    return () => document.removeEventListener('pointerdown', onDown, true)
  }, [anchor, onClose])

  return createPortal(
    <div
      ref={panel}
      className={`popover${pos?.up ? ' up' : ''} ${className}`}
      // Unplaced it is transparent rather than hidden, so its contents can take focus.
      style={pos ? { top: pos.top, left: pos.left } : { top: 0, left: 0, opacity: 0, pointerEvents: 'none' }}
    >
      {children}
    </div>,
    document.body,
  )
}

/* ---------------- Select ---------------- */

export interface Option<T extends string> {
  value: T
  label: string
  icon?: ReactNode
  hint?: string
}

/**
 * A listbox in a popover. `field` looks like an input, `chip` like quiet
 * metadata text, `ghost` like a bare label (used inside the composer).
 */
export function Select<T extends string>({ value, options, onChange, label, variant = 'field', icon, placeholder = '请选择', disabled, className = '' }: {
  value: T
  options: Option<T>[]
  onChange: (value: T) => void
  label: string
  variant?: 'field' | 'chip' | 'ghost'
  icon?: ReactNode
  placeholder?: string
  disabled?: boolean
  className?: string
}) {
  const trigger = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLDivElement>(null)
  const id = useId()
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const current = options.find((o) => o.value === value)

  const show = () => {
    setActive(Math.max(0, options.findIndex((o) => o.value === value)))
    setOpen(true)
  }
  const close = (refocus = true) => {
    setOpen(false)
    if (refocus) trigger.current?.focus()
  }
  const pick = (v: T) => {
    close()
    if (v !== value) onChange(v)
  }

  useEffect(() => {
    if (open) list.current?.focus({ preventScroll: true })
  }, [open])
  useEffect(() => {
    if (open) list.current?.querySelector(`[data-i="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [open, active])

  const onListKey = (e: KeyboardEvent) => {
    const last = options.length - 1
    if (e.key === 'ArrowDown') setActive((i) => Math.min(i + 1, last))
    else if (e.key === 'ArrowUp') setActive((i) => Math.max(i - 1, 0))
    else if (e.key === 'Home') setActive(0)
    else if (e.key === 'End') setActive(last)
    else if ((e.key === 'Enter' || e.key === ' ') && options[active]) pick(options[active].value)
    else if (e.key === 'Tab') return close(false)
    else return
    e.preventDefault()
  }

  const Chevron = variant === 'field' ? ChevronsUpDown : ChevronDown
  return (
    <>
      <button
        ref={trigger}
        type="button"
        className={`select select-${variant}${open ? ' open' : ''} ${className}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={`${label}：${current?.label ?? placeholder}`}
        disabled={disabled}
        onClick={() => (open ? close() : show())}
        onKeyDown={(e) => {
          if (!open && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
            e.preventDefault()
            show()
          }
        }}
      >
        {icon ?? current?.icon}
        <span className={`select-value${current ? '' : ' placeholder'}`}>{current?.label ?? placeholder}</span>
        <Chevron size={variant === 'field' ? 14 : 12} className="select-chevron" />
      </button>
      {open && (
        <Popover anchor={trigger} onClose={() => close(false)} matchWidth={variant === 'field'}>
          <div
            ref={list}
            role="listbox"
            aria-label={label}
            tabIndex={-1}
            className="menu"
            aria-activedescendant={`${id}-${active}`}
            onKeyDown={onListKey}
          >
            {options.map((o, i) => (
              <div
                key={o.value}
                id={`${id}-${i}`}
                data-i={i}
                role="option"
                aria-selected={o.value === value}
                className={`menu-item${i === active ? ' active' : ''}`}
                onPointerMove={() => setActive(i)}
                onClick={() => pick(o.value)}
              >
                {o.icon}
                <span className="grow">{o.label}</span>
                {o.hint && <span className="menu-hint">{o.hint}</span>}
                <Check size={14} className="menu-check" />
              </div>
            ))}
          </div>
        </Popover>
      )}
    </>
  )
}

/* ---------------- Date & time ---------------- */

const weekdays = ['一', '二', '三', '四', '五', '六', '日']
const pad = (n: number) => String(n).padStart(2, '0')
const sameDay = (a: Date, b: Date) => a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()

function at(base: Date, days: number, hour: number, minute = 0): Date {
  const d = new Date(base.getFullYear(), base.getMonth(), base.getDate() + days)
  d.setHours(hour, minute, 0, 0)
  return d
}

function Calendar({ value, onPick }: { value?: Date; onPick: (d: Date) => void }) {
  const today = new Date()
  const [month, setMonth] = useState(() => new Date((value ?? today).getFullYear(), (value ?? today).getMonth(), 1))
  const lead = (month.getDay() + 6) % 7
  const count = new Date(month.getFullYear(), month.getMonth() + 1, 0).getDate()
  const cells = Array.from({ length: Math.ceil((lead + count) / 7) * 7 }, (_, i) => new Date(month.getFullYear(), month.getMonth(), i - lead + 1))
  const shift = (n: number) => setMonth(new Date(month.getFullYear(), month.getMonth() + n, 1))

  return (
    <div className="cal">
      <div className="cal-head">
        <span className="cal-month">
          {month.getFullYear()}年{month.getMonth() + 1}月
        </span>
        <button type="button" className="btn btn-quiet btn-icon btn-sm" aria-label="上个月" onClick={() => shift(-1)}>
          <ChevronLeft size={16} />
        </button>
        <button type="button" className="btn btn-quiet btn-icon btn-sm" aria-label="下个月" onClick={() => shift(1)}>
          <ChevronRight size={16} />
        </button>
      </div>
      <div className="cal-grid" role="grid">
        {weekdays.map((w) => (
          <span key={w} className="cal-dow">
            {w}
          </span>
        ))}
        {cells.map((d) => (
          <button
            key={d.toISOString()}
            type="button"
            className={[
              'cal-day',
              d.getMonth() !== month.getMonth() && 'outside',
              sameDay(d, today) && 'today',
              value && sameDay(d, value) && 'on',
            ]
              .filter(Boolean)
              .join(' ')}
            aria-label={`${d.getMonth() + 1}月${d.getDate()}日`}
            aria-pressed={!!value && sameDay(d, value)}
            onClick={() => onPick(d)}
          >
            {d.getDate()}
          </button>
        ))}
      </div>
    </div>
  )
}

function TimeField({ value, onChange }: { value: Date; onChange: (h: number, m: number) => void }) {
  const [h, setH] = useState(pad(value.getHours()))
  const [m, setM] = useState(pad(value.getMinutes()))
  const [prev, setPrev] = useState(value)
  if (prev !== value) {
    setPrev(value)
    setH(pad(value.getHours()))
    setM(pad(value.getMinutes()))
  }
  const commit = (hh: string, mm: string) => {
    const hour = Math.min(23, Math.max(0, Number(hh) || 0))
    const minute = Math.min(59, Math.max(0, Number(mm) || 0))
    onChange(hour, minute)
  }
  const digits = (set: (v: string) => void) => (e: { target: { value: string } }) => set(e.target.value.replace(/\D/g, '').slice(0, 2))
  return (
    <div className="time-field">
      <input inputMode="numeric" aria-label="时" value={h} onChange={digits(setH)} onBlur={() => commit(h, m)} onFocus={(e) => e.target.select()} />
      <span>:</span>
      <input inputMode="numeric" aria-label="分" value={m} onChange={digits(setM)} onBlur={() => commit(h, m)} onFocus={(e) => e.target.select()} />
    </div>
  )
}

/**
 * Picks a moment: quick presets, a month grid and a 24-hour time. Changes are
 * kept as a draft until 完成, so Escape or a click outside discards them.
 */
export function DateTimePicker({ value, onChange, label, placeholder = '设定时间', variant = 'chip', defaultHour = 18, clearLabel = '清除' }: {
  value?: string
  onChange: (iso: string | undefined) => void
  label: string
  placeholder?: string
  variant?: 'chip' | 'field'
  defaultHour?: number
  clearLabel?: string
}) {
  const trigger = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<Date | undefined>()
  const now = new Date()
  const nextMonday = (8 - now.getDay()) % 7 || 7
  const presets = [
    { label: '今天', at: at(now, 0, Math.max(defaultHour, now.getHours() + 1)) },
    { label: '明天', at: at(now, 1, 9) },
    { label: '下周一', at: at(now, nextMonday, 9) },
  ].filter((p) => p.at.getDate() === at(now, 0, 0).getDate() || p.label !== '今天')

  const commit = (d: Date | undefined) => {
    setOpen(false)
    trigger.current?.focus()
    if ((d?.getTime() ?? 0) !== (value ? new Date(value).getTime() : 0)) onChange(d?.toISOString())
  }

  return (
    <>
      <button
        ref={trigger}
        type="button"
        className={`select select-${variant}${open ? ' open' : ''}${value ? '' : ' empty'}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`${label}：${value ? formatDateTime(value) : placeholder}`}
        onClick={() => {
          setDraft(value ? new Date(value) : undefined)
          setOpen((v) => !v)
        }}
      >
        <CalendarDays size={13} />
        <span className={`select-value${value ? '' : ' placeholder'}`}>{value ? formatDateTime(value) : placeholder}</span>
      </button>
      {open && (
        <Popover anchor={trigger} onClose={() => setOpen(false)}>
          <div className="picker" role="dialog" aria-label={label}>
            <div className="picker-presets">
              {presets.map((p) => (
                <button key={p.label} type="button" className="chip" onClick={() => commit(p.at)}>
                  {p.label}
                  <span className="faint">{`${pad(p.at.getHours())}:${pad(p.at.getMinutes())}`}</span>
                </button>
              ))}
            </div>
            <Calendar
              value={draft}
              onPick={(d) => setDraft(at(d, 0, draft?.getHours() ?? defaultHour, draft?.getMinutes() ?? 0))}
            />
            <div className="picker-foot">
              {draft ? (
                <TimeField value={draft} onChange={(h, m) => setDraft(at(draft, 0, h, m))} />
              ) : (
                <span className="small faint">先选一天</span>
              )}
              <span className="grow" />
              {value && (
                <button type="button" className="btn btn-quiet btn-sm btn-danger" onClick={() => commit(undefined)}>
                  {clearLabel}
                </button>
              )}
              <button type="button" className="btn btn-primary btn-sm" disabled={!draft} onClick={() => commit(draft)}>
                完成
              </button>
            </div>
          </div>
        </Popover>
      )}
    </>
  )
}

/* ---------------- Small inputs ---------------- */

/** A number with − / + buttons. Typing commits on blur or Enter. */
export function Stepper({ value, onChange, min = 0, max = Infinity, step = 1, prefix, label }: {
  value: number
  onChange: (value: number) => void
  min?: number
  max?: number
  step?: number
  prefix?: string
  label: string
}) {
  const [text, setText] = useState(String(value))
  const [prev, setPrev] = useState(value)
  if (prev !== value) {
    setPrev(value)
    setText(String(value))
  }
  const set = (n: number) => {
    const next = Math.min(max, Math.max(min, n))
    setText(String(next))
    if (next !== value) onChange(next)
  }
  return (
    <div className="stepper" role="group" aria-label={label}>
      <button type="button" aria-label="减少" disabled={value <= min} onClick={() => set(value - step)}>
        <Minus size={13} />
      </button>
      <label>
        {prefix && <span className="faint">{prefix}</span>}
        <input
          inputMode="decimal"
          aria-label={label}
          value={text}
          onChange={(e) => setText(e.target.value.replace(/[^\d.]/g, ''))}
          onBlur={() => set(Number(text) || 0)}
          onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
          onFocus={(e) => e.target.select()}
        />
      </label>
      <button type="button" aria-label="增加" disabled={value >= max} onClick={() => set(value + step)}>
        <Plus size={13} />
      </button>
    </div>
  )
}

/** A checkbox with a drawn box; the real input stays for keyboard and forms. */
export function Checkbox({ children, className = '', ...rest }: InputHTMLAttributes<HTMLInputElement> & { children?: ReactNode }) {
  return (
    <label className={`checkbox ${className}`}>
      <input type="checkbox" {...rest} />
      <span className="checkbox-box" aria-hidden>
        <Check size={11} strokeWidth={3.5} />
      </span>
      {children && <span className="checkbox-label">{children}</span>}
    </label>
  )
}

/** An on/off pill for picking several things from a short list. */
export function Chip({ on, onToggle, disabled, children }: { on: boolean; onToggle: () => void; disabled?: boolean; children: ReactNode }) {
  return (
    <button type="button" className={`chip chip-toggle${on ? ' on' : ''}`} aria-pressed={on} disabled={disabled} onClick={onToggle}>
      {on && <Check size={12} strokeWidth={3} />}
      {children}
    </button>
  )
}

function size(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

/** Click or drop a file. Shows the chosen file with a way to clear it. */
export function FileDrop({ file, accept, hint, onFile, onClear }: {
  file: File | null
  accept: string
  hint: string
  onFile: (file: File) => void
  onClear: () => void
}) {
  const [over, setOver] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const drop = (e: DragEvent) => {
    e.preventDefault()
    setOver(false)
    const f = e.dataTransfer.files[0]
    if (f) onFile(f)
  }
  if (file) {
    return (
      <div className="file-chip">
        <span className="file-icon">
          <FileText size={16} />
        </span>
        <div className="grow">
          <div className="ellipsis ink">{file.name}</div>
          <div className="tiny muted">{size(file.size)}</div>
        </div>
        <button
          type="button"
          className="btn btn-quiet btn-icon btn-sm"
          aria-label="移除文件"
          onClick={() => {
            if (input.current) input.current.value = ''
            onClear()
          }}
        >
          <X size={14} />
        </button>
      </div>
    )
  }
  return (
    <label
      className={`dropzone${over ? ' over' : ''}`}
      onDragOver={(e) => {
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={() => setOver(false)}
      onDrop={drop}
    >
      <input
        ref={input}
        type="file"
        className="visually-hidden"
        accept={accept}
        aria-label="选择文件"
        onChange={(e) => {
          const f = e.target.files?.[0]
          if (f) onFile(f)
        }}
      />
      <span className="dropzone-icon">
        <Upload size={18} />
      </span>
      <span className="ink">
        拖到这里，或<span className="link">选择文件</span>
      </span>
      <span className="tiny muted">{hint}</span>
    </label>
  )
}
