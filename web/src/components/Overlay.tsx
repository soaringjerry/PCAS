import { useEffect, useRef, type FormEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'
import { Button } from './ui'
import { useEscape } from './useEscape'

export function SideSheet({ title, top, onClose, children }: {
  title: ReactNode
  top?: ReactNode
  onClose: () => void
  children: ReactNode
}) {
  useEscape(onClose)
  const panel = useRef<HTMLDivElement>(null)
  useEffect(() => panel.current?.focus(), [])
  return createPortal(
    <>
      <div className="scrim" onClick={onClose} />
      <aside className="side-sheet" role="dialog" aria-modal="true" tabIndex={-1} ref={panel}>
        <div className="side-sheet-head">
          <div className="grow">
            {top && <div className="row" style={{ marginBottom: 6 }}>{top}</div>}
            <h2>{title}</h2>
          </div>
          <Button variant="quiet" className="btn-close" icon={<X size={18} />} onClick={onClose} aria-label="关闭" />
        </div>
        <div className="side-sheet-body">{children}</div>
      </aside>
    </>,
    document.body,
  )
}

/** A centred dialog. With `onSubmit` the body is a form, so Enter confirms. */
export function Modal({ title, onClose, onSubmit, children, actions }: {
  title: string
  onClose: () => void
  onSubmit?: () => void
  children: ReactNode
  actions: ReactNode
}) {
  useEscape(onClose)
  const body = (
    <>
      <h2 className="modal-title">{title}</h2>
      <div className="stack-sm">{children}</div>
      <div className="modal-actions">{actions}</div>
    </>
  )
  return createPortal(
    <>
      <div className="scrim modal-scrim" onClick={onClose} />
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        {onSubmit ? (
          <form
            onSubmit={(e: FormEvent) => {
              e.preventDefault()
              onSubmit()
            }}
          >
            {body}
          </form>
        ) : (
          body
        )}
      </div>
    </>,
    document.body,
  )
}

/** Replaces window.confirm: a yes/no question with a destructive default. */
export function ConfirmModal({ title, children, confirm = '删掉', cancel = '留着', onConfirm, onClose }: {
  title: string
  children?: ReactNode
  confirm?: string
  cancel?: string
  onConfirm: () => void | Promise<void>
  onClose: () => void
}) {
  return (
    <Modal
      title={title}
      onClose={onClose}
      actions={
        <>
          <Button variant="quiet" onClick={onClose}>
            {cancel}
          </Button>
          <Button variant="primary" className="btn-destructive" onClick={() => void onConfirm()} autoFocus>
            {confirm}
          </Button>
        </>
      }
    >
      {children}
    </Modal>
  )
}
