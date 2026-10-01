import { Select, Stepper } from './controls'
import { useState } from 'react'
import { useStore } from '../store/context'
import { api } from '../store/api'
import { Button, Fold } from './ui'

/** How long one memory keeps its extra weight in everyday recall. It ranks; it never deletes or hides. */
export function MemoryActivitySettings() {
  const { state } = useStore()
  const [id, setId] = useState('')
  const [days, setDays] = useState(30)
  const [limit, setLimit] = useState(8)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<{ text: string; failed?: boolean } | null>(null)
  const selected = state.memories.find(m => m.id === id)
  return <Fold title="某条记忆多久后少被想起" summary="一般不用动">
    <form className="stack-sm" onSubmit={async e => { e.preventDefault(); if (!selected) return; setBusy(true); setMessage(null); try { await api('/v1/memory/activity', { ref: { id: selected.id, version: selected.recordVersion, kind: 'claim' }, half_life_days: days, reinforcement_limit: limit, pinned: selected.pinned }); setMessage({ text: '已保存。它不会被删除，点名去找时照样找得到。' }) } catch (e) { setMessage({ text: (e as Error).message, failed: true }) } finally { setBusy(false) } }}>
      <p className="small muted">只影响日常交流里它排得多靠前，不会删除或隐藏它。你提到、确认或采用它时，它会重新靠前。</p>
      <label className="field"><span className="field-label">选择记忆</span><Select label="选择记忆" value={id} onChange={value => { const m = state.memories.find(v => v.id === value); setId(value); setDays(m?.halfLifeDays ?? 30); setLimit(m?.reinforcementLimit ?? 8); setMessage(null) }} options={[{ value: '', label: state.memories.length ? '请选择' : '还没有记忆' }, ...state.memories.map(m => ({ value: m.id, label: m.text.slice(0, 60) }))]} /></label>
      {selected && <><label className="field"><span className="field-label">多少天没用到，优先程度减半</span><Stepper label="减半天数" min={1} max={36500} value={days} onChange={setDays} /></label><label className="field"><span className="field-label">经常用到时，这个天数最多延长到几倍</span><Stepper label="最多延长倍数" min={1} max={100} value={limit} onChange={setLimit} /></label>{selected.pinned && <p className="small muted">这条记忆已固定保留，现在不会减弱。</p>}<div><Button type="submit" disabled={busy}>{busy ? '正在保存…' : '保存'}</Button></div></>}
      {message && <p role={message.failed ? 'alert' : 'status'} className={`small${message.failed ? ' warn-text' : ''}`}>{message.text}</p>}
    </form>
  </Fold>
}
