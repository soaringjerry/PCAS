import { useRef, useState } from 'react'
import { Popover } from './controls'
import { timezoneOptions } from '../domain/timezone'
import { useStore } from '../store/context'

export function TimezoneSettings() {
  const { state, dispatch } = useStore()
  const timezone = state.settings.timezone ?? 'UTC'
  const anchor = useRef<HTMLButtonElement>(null)
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const [busy, setBusy] = useState(false)
  const options = timezoneOptions(timezone).filter((o) => o.label.toLowerCase().includes(search.trim().toLowerCase()))
  return <div className="setting">
    <div className="setting-text"><div className="ink">时区</div><div className="small muted">秘书、提醒和首页「今天」都按这个时区，夏令时会自动调整。</div></div>
    <button ref={anchor} className="btn btn-quiet btn-sm" type="button" aria-label="时区" aria-haspopup="listbox" aria-expanded={open}
      onClick={() => { setSearch(''); setOpen(!open) }}>{timezone}</button>
    {open && <Popover anchor={anchor} onClose={() => setOpen(false)} className="timezone-picker">
      <input autoFocus type="search" className="input" aria-label="搜索时区" placeholder="搜索城市，比如 上海、Melbourne" value={search} onChange={(e) => setSearch(e.target.value)} />
      <div role="listbox" aria-label="时区选项" className="timezone-options">
        {options.map((o) => <button key={o.value} role="option" aria-selected={o.value === timezone} disabled={busy}
          className="btn btn-quiet btn-sm" type="button" onClick={async () => {
            setBusy(true)
            if (await dispatch({ type: 'updateSettings', patch: { timezone: o.value } })) {
              setOpen(false)
              anchor.current?.focus()
            }
            setBusy(false)
          }}>{o.label}</button>)}
        {options.length === 0 && <p className="small muted">没有找到。试试城市的英文名，比如 Melbourne。</p>}
      </div>
    </Popover>}
  </div>
}
