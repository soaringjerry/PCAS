import { useEffect, useRef, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { Button } from './ui'

function useEscape(onClose: () => void) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
}

export function Drawer({ title, badges, onClose, children }: {
  title: ReactNode
  badges?: ReactNode
  onClose: () => void
  children: ReactNode
}) {
  useEscape(onClose)
  const panel = useRef<HTMLDivElement>(null)
  useEffect(() => panel.current?.focus(), [])
  return (
    <>
      <div className="overlay" onClick={onClose} />
      <aside className="drawer" role="dialog" aria-modal="true" tabIndex={-1} ref={panel}>
        <div className="drawer-head">
          <div className="grow">
            {badges && <div className="row" style={{ marginBottom: 6 }}>{badges}</div>}
            <h2>{title}</h2>
          </div>
          <Button variant="ghost" icon={<X size={18} />} onClick={onClose} aria-label="关闭" />
        </div>
        <div className="drawer-body">{children}</div>
      </aside>
    </>
  )
}

export function Modal({ title, onClose, children, actions }: {
  title: string
  onClose: () => void
  children: ReactNode
  actions: ReactNode
}) {
  useEscape(onClose)
  return (
    <>
      <div className="overlay modal-overlay" onClick={onClose} />
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        <h2 style={{ marginBottom: 10 }}>{title}</h2>
        <div className="stack-sm">{children}</div>
        <div className="row" style={{ justifyContent: 'flex-end', marginTop: 18 }}>
          {actions}
        </div>
      </div>
    </>
  )
}
