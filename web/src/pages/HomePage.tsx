import { useState } from 'react'
import { Link } from 'react-router'
import { ChevronDown, ChevronRight, Plus, Sparkles } from 'lucide-react'
import { LineRow } from '../components/LineRow'
import { UnsureSheet } from '../components/UnsureSheet'
import { ongoingLine, spentToday, unsure, urgentLine } from '../domain/lines'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { useToast } from '../store/toast'

function BackgroundNote() {
  const { state, runDemoImport } = useStore()
  const today = new Date().toDateString()
  const jobs = state.jobs.filter((j) => new Date(j.createdAt).toDateString() === today)
  const filed = jobs.filter((j) => j.title === '整理新记录').reduce((n, j) => n + Number(j.detail.match(/自动收下 (\d+)/)?.[1] ?? 0), 0)
  const woken = state.ideas.filter((i) => i.wake && new Date(i.wake.at).toDateString() === today).length
  const failed = state.jobs.filter((j) => j.status === 'failed').length
  const parts = [filed && `整理了 ${filed} 条记录`, woken && `唤醒了 ${woken} 个想法`].filter(Boolean)
  const demoRunning = state.jobs.some((j) => j.id === 'j_demo' && j.status === 'running')

  return (
    <p className="quiet-foot">
      {parts.length > 0 && <>后台今天{parts.join('，')}。</>}
      {failed > 0 && (
        <>
          {' '}
          <Link to="/library?tab=sources">{failed} 项导入没成功</Link>。
        </>
      )}
      {!state.demo.costReportImported && (
        <>
          {' '}
          <button type="button" className="link-btn" style={{ fontSize: 'inherit', color: 'var(--blue)' }} onClick={runDemoImport}>
            试试导入一份成本测算
          </button>
        </>
      )}
      {demoRunning && ' 正在导入…'}
    </p>
  )
}

export function HomePage() {
  const { state, dispatch, runAgent } = useStore()
  const { agentFor } = useShell()
  const toast = useToast()
  const [text, setText] = useState('')
  const [showUnsure, setShowUnsure] = useState(false)
  const [showParked, setShowParked] = useState(false)

  const urgent = urgentLine(state)
  const { active, parked } = ongoingLine(state)
  const { candidates, guesses } = unsure(state)
  const toConfirm = candidates.length + guesses.length
  const ready = active.filter((i) => i.next)
  const allCost = ready.reduce((n, i) => n + (i.next?.cost ?? 0), 0)
  const canAll = ready.length > 0 && spentToday(state) + allCost <= state.settings.dailyBudget
  const date = new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })

  return (
    <div className="home">
      <div className="head-row">
        <div>
          <h1 className="large-title">今天</h1>
          <p className="subtitle">{date}</p>
        </div>
        {toConfirm > 0 && (
          <button type="button" className="pill-btn" onClick={() => setShowUnsure(true)}>
            {toConfirm} 条需要你确认 <ChevronRight size={14} />
          </button>
        )}
      </div>

      <form
        className="capture"
        onSubmit={(e) => {
          e.preventDefault()
          if (!text.trim()) return
          dispatch({ type: 'capture', text: text.trim() })
          setText('')
          toast.show('记下了，后台会整理')
        }}
      >
        <Plus size={18} />
        <input value={text} onChange={(e) => setText(e.target.value)} placeholder="记点什么" aria-label="记点什么" />
      </form>

      <div className="lines">
        <section>
          <div className="line-head">
            <h2>紧迫</h2>
            <span className="n">{urgent.length}</span>
          </div>
          <div className="group">
            {urgent.length ? urgent.map((i) => <LineRow key={i.thing.id} item={i} />) : <div className="empty-line">没有紧迫的事</div>}
          </div>
        </section>

        <section>
          <div className="line-head">
            <h2>在推进</h2>
            <span className="n">{active.length}</span>
            {ready.length > 1 && (
              <span className="end">
                <button
                  type="button"
                  className="ai-btn"
                  disabled={!canAll}
                  title={canAll ? `让副手把 ${ready.length} 件事各推进一步` : '超过今天的额度'}
                  onClick={() => {
                    for (const i of ready) runAgent({ thingId: i.thing.id, agentId: agentFor(i.thing.id), kind: i.next!.kind, prompt: i.next!.prompt })
                    toast.show(`副手开始推进 ${ready.length} 件事`)
                  }}
                >
                  <Sparkles size={12} />
                  全部推进一步
                  <span className="cost">¥{allCost.toFixed(2)}</span>
                </button>
              </span>
            )}
          </div>
          <div className="group">
            {active.length ? active.map((i) => <LineRow key={i.thing.id} item={i} />) : <div className="empty-line">没有在推进的事</div>}
            {parked.length > 0 && (
              <>
                <button type="button" className="disclosure" onClick={() => setShowParked((v) => !v)} aria-expanded={showParked}>
                  {showParked ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
                  {parked.length} 个想法放着，条件满足会自己回来
                </button>
                {showParked && parked.map((i) => <LineRow key={i.thing.id} item={i} />)}
              </>
            )}
          </div>
        </section>
      </div>

      <BackgroundNote />
      {showUnsure && <UnsureSheet onClose={() => setShowUnsure(false)} />}
    </div>
  )
}
