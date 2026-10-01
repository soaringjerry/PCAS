import { useState } from 'react'
import { X } from 'lucide-react'
import { browserTimezone, sameTimezone } from '../domain/timezone'
import { useStore } from '../store/context'

const DISMISSED = 'pcas.timezone-hint.dismissed'

export function TimezoneHint() {
  const { state, dispatch } = useStore()
  const [dismissed, setDismissed] = useState(() => {
    try { return localStorage.getItem(DISMISSED) === 'true' } catch { return false }
  })
  const [busy, setBusy] = useState(false)
  const zone = browserTimezone()
  if (dismissed || sameTimezone(state.settings.timezone ?? 'UTC', zone)) return null
  return <div className="timezone-hint" role="status">
    <span>你的时区好像是 {zone}，要改成 {zone} 吗？</span>
    <button type="button" className="btn btn-quiet btn-sm" disabled={busy} onClick={async () => {
      setBusy(true)
      await dispatch({ type: 'updateSettings', patch: { timezone: zone } })
      setBusy(false)
    }}>改成 {zone}</button>
    <button type="button" className="btn btn-quiet btn-icon btn-sm" aria-label="关闭时区提示" onClick={() => {
      try { localStorage.setItem(DISMISSED, 'true') } catch { /* Still dismiss for this page when storage is unavailable. */ }
      setDismissed(true)
    }}><X size={15} /></button>
  </div>
}
