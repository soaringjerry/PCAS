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
  const current = state.settings.timezone ?? 'UTC'
  if (dismissed || sameTimezone(current, zone)) return null
  // Each zone is named once: the one in use here, the device's on the button.
  return <div className="timezone-hint" role="status">
    <span>时间按 {current} 显示，和这台设备不同</span>
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
