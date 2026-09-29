import { Select, Stepper } from './controls'
import { useState } from 'react'
import { useStore } from '../store/context'
import { api } from '../store/api'
import { Button, Sheet } from './ui'
export function MemoryActivitySettings() {
  const { state } = useStore()
  const [id, setId] = useState('')
  const [days, setDays] = useState(30)
  const [limit, setLimit] = useState(8)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const selected = state.memories.find(m => m.id === id)
  return <section className="section"><h2 className="section-title">记忆曝光</h2><Sheet pad><form className="stack-sm" onSubmit={async e => { e.preventDefault(); if (!selected) return; setBusy(true); setMessage(''); try { await api('/v1/memory/activity', { ref: { id: selected.id, version: selected.recordVersion, kind: 'claim' }, half_life_days: days, reinforcement_limit: limit, pinned: selected.pinned }); setMessage('已保存。明确回忆时仍能找回，提醒和执行状态不受影响。') } catch (e) { setMessage((e as Error).message) } finally { setBusy(false) } }}>
    <p className="small muted">调整一条记忆在日常交流中多久逐渐减少曝光。用户实际采用会适度强化；系统自行检索不会强化。</p>
    <label className="field"><span className="field-label">选择记忆</span><Select label="选择记忆" value={id} onChange={value => { const m = state.memories.find(v => v.id === value); setId(value); setDays(m?.halfLifeDays ?? 30); setLimit(m?.reinforcementLimit ?? 8); setMessage('') }} options={[{ value: '', label: '请选择' }, ...state.memories.map(m => ({ value: m.id, label: m.text.slice(0, 60) }))]} /></label>
    {selected && <><label className="field"><span className="field-label">基础曝光减半时间（天）</span><Stepper label="基础曝光减半时间" min={1} max={36500} value={days} onChange={setDays} /></label><label className="field"><span className="field-label">采用后的最大强化倍数</span><Stepper label="最大强化倍数" min={1} max={100} value={limit} onChange={setLimit} /></label>{selected.pinned && <p className="small muted">此记忆已固定保留，当前不会减少曝光。</p>}<div><Button type="submit" disabled={busy}>保存曝光设置</Button></div></>}
    {message && <p role="status" className="small">{message}</p>}
  </form></Sheet></section>
}
