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

export function SideSheet({ title, top, onClose, children }: {
  title: ReactNode
  top?: ReactNode
  onClose: () => void
  children: ReactNode
}) {
  useEscape(onClose)
  const panel = useRef<HTMLDivElement>(null)
  useEffect(() => panel.current?.focus(), [])
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <aside className="side-sheet" role="dialog" aria-modal="true" tabIndex={-1} ref={panel}>
        <div className="side-sheet-head">
          <div className="grow">
            {top && <div className="row" style={{ marginBottom: 6 }}>{top}</div>}
            <h2 style={{ fontSize: 20 }}>{title}</h2>
          </div>
          <Button variant="quiet" icon={<X size={18} />} onClick={onClose} aria-label="关闭" />
        </div>
        <div className="side-sheet-body">{children}</div>
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
      <div className="scrim modal-scrim" onClick={onClose} />
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        <h2 style={{ fontSize: 20, marginBottom: 10 }}>{title}</h2>
        <div className="stack-sm">{children}</div>
        <div className="row" style={{ justifyContent: 'flex-end', marginTop: 18 }}>
          {actions}
        </div>
      </div>
    </>
  )
}
