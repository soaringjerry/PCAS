import { Link, useMatch, useNavigate } from 'react-router'
import { ArrowUpRight, Info } from 'lucide-react'
import { contextFor, sourcesFor } from '../domain/agent'
import { runStatusLabel } from '../domain/labels'
import { findThing, thingTitle, timelineFor, type Thing } from '../domain/things'
import { formatAgo } from '../domain/time'
import { useStore } from '../store/context'
import { useWorkspace } from '../store/workspace'
import { FromLine, KindLabel, Timeline, TrustTag } from './Marks'
import { Button, Tag } from './ui'

function ThingContext({ thing, preview }: { thing: Thing; preview: boolean }) {
  const { state, dispatch } = useStore()
  const { ws } = useWorkspace()
  const navigate = useNavigate()
  const agent = state.agents.find((a) => a.id === (ws.agentFor[thing.id] ?? 'a_claude')) ?? state.agents[0]
  const context = contextFor(state, thing, agent)
  const included = context.filter((c) => c.included).length
  const sources = sourcesFor(state, thing)

  return (
    <>
      {preview && (
        <div className="ctx-section">
          <div className="spread">
            <div className="row-nowrap grow">
              <KindLabel kind={thing.kind} bare />
              <span className="ink ellipsis" style={{ fontWeight: 600 }}>
                {thingTitle(thing)}
              </span>
            </div>
            <Button size="sm" icon={<ArrowUpRight size={12} />} onClick={() => navigate(`/t/${thing.id}`)}>
              打开
            </Button>
          </div>
        </div>
      )}

      <div className="ctx-section">
        <h3>
          <span>{agent.name} 会看到的记忆</span>
          <span className="faint">
            {included}/{context.length}
          </span>
        </h3>
        {context.length === 0 && <p className="small muted">没有相关的记忆。</p>}
        {context.map(({ memory, allowed, included: on, kindBlocked, unconfirmed }) => (
          <label key={memory.id} className={`ctx-mem${allowed ? '' : ' blocked'}`}>
            <input
              type="checkbox"
              checked={on}
              disabled={!allowed}
              onChange={() => dispatch({ type: 'toggleContextMemory', thingId: thing.id, memoryId: memory.id })}
            />
            <span className="grow">
              <span className={memory.epistemic === 'inferred' ? 'guess ink' : 'ink'}>{memory.text}</span>
              <span className="row" style={{ gap: 6, marginTop: 2 }}>
                <TrustTag value={memory.epistemic} />
                {kindBlocked && <span className="tiny faint">{agent.name} 无权看这类记忆</span>}
                {unconfirmed && (
                  <button
                    type="button"
                    className="btn btn-sm"
                    style={{ height: 20 }}
                    onClick={(e) => {
                      e.preventDefault()
                      dispatch({ type: 'confirmMemory', id: memory.id })
                    }}
                  >
                    确认后才会带上
                  </button>
                )}
              </span>
            </span>
          </label>
        ))}
        <p className="tiny faint" style={{ marginTop: 6 }}>
          取消勾选的记忆不会发给 AI。范围在“设置 → AI 接入”里调。
        </p>
      </div>

      {sources.length > 0 && (
        <div className="ctx-section">
          <h3>来源</h3>
          <div className="stack-sm" style={{ gap: 10 }}>
            {sources.map((s, i) => (
              <FromLine key={i} source={s} />
            ))}
          </div>
        </div>
      )}

      <div className="ctx-section">
        <h3>来龙去脉</h3>
        <Timeline key={thing.id} events={timelineFor(state, thing)} limit={6} />
      </div>
    </>
  )
}

function Idle() {
  const { state } = useStore()
  const recent = [...state.runs].sort((a, b) => b.createdAt.localeCompare(a.createdAt)).slice(0, 6)
  return (
    <>
      <div className="ctx-section">
        <h3>最近 AI 的工作</h3>
        {recent.length === 0 && <p className="small muted">还没有。打开一件事，在下方让 AI 动手。</p>}
        {recent.map((r) => {
          const thing = findThing(state, r.thingId)
          return (
            <Link key={r.id} to={`/t/${r.thingId}`} className="ctx-mem" style={{ color: 'inherit', textDecoration: 'none' }}>
              <span className="grow">
                <span className="ink ellipsis" style={{ display: 'block' }}>
                  {r.prompt}
                </span>
                <span className="tiny muted">
                  {state.agents.find((a) => a.id === r.agentId)?.name} · {thing ? thingTitle(thing) : '已删除'} · {formatAgo(r.createdAt)}
                </span>
              </span>
              <Tag tone={r.adopted ? 'success' : runStatusLabel[r.status].tone}>{r.adopted ? '已采纳' : runStatusLabel[r.status].text}</Tag>
            </Link>
          )
        })}
      </div>
      <div className="ctx-section">
        <h3>快捷键</h3>
        <div className="stack-sm small muted">
          <span>
            <kbd>⌘K</kbd> 搜索、新建、跳转
          </span>
          <span>
            <kbd>J</kbd> <kbd>K</kbd> 在列表里移动，<kbd>↵</kbd> 打开
          </span>
          <span>
            <kbd>X</kbd> 多选，<kbd>C</kbd> 完成，<kbd>T</kbd> 推到明天
          </span>
          <span>
            <kbd>⌘\</kbd> 侧栏，<kbd>⌘.</kbd> 这一栏
          </span>
          <span>
            <kbd>Alt</kbd>+<kbd>W</kbd> 关标签，<kbd>Alt</kbd>+<kbd>[</kbd> <kbd>]</kbd> 切标签
          </span>
        </div>
      </div>
    </>
  )
}

export function ContextPane() {
  const { state } = useStore()
  const { ws } = useWorkspace()
  const match = useMatch('/t/:id')
  const openId = match?.params.id
  const thing = findThing(state, openId ?? ws.selected ?? '')

  return (
    <aside className={`context${ws.drawer === 'context' ? ' open' : ''}`} aria-label="上下文">
      <div className="ctx-head">
        <Info size={14} className="muted" />
        上下文
      </div>
      <div className="ctx-scroll">{thing ? <ThingContext thing={thing} preview={!openId} /> : <Idle />}</div>
    </aside>
  )
}
