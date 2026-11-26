import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight } from 'lucide-react'
import { memoryKindLabel } from '../../domain/labels'
import type { Agent, MemoryKind } from '../../domain/types'
import { useStore } from '../../store/context'
import { useMemoryCount } from '../../store/memories'
import { Chip } from '../controls'
import { MemoryActivitySettings } from '../MemoryActivitySettings'
import { Empty, SaveMark, Sheet, Switch, Tag, type SaveState } from '../ui'

function useSaving(): [SaveState, (work: () => Promise<boolean>) => Promise<void>] {
  const [state, setState] = useState<SaveState>('idle')
  const timer = useRef<number>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return [
    state,
    async (work) => {
      window.clearTimeout(timer.current)
      setState('saving')
      const ok = await work()
      setState(ok ? 'saved' : 'failed')
      if (ok) timer.current = window.setTimeout(() => setState('idle'), 2500)
    },
  ]
}

function AgentAccessRow({ agent }: { agent: Agent }) {
  const { dispatch } = useStore()
  const [mark, run] = useSaving()
  const seen = useMemoryCount(agent)
  const update = (patch: Partial<Agent>) => run(() => dispatch({ type: 'updateAgent', id: agent.id, patch }))

  return (
    <div className="item">
      <div className="grow stack-sm" style={{ gap: 8 }}>
        <div className="spread">
          <div className="row" style={{ gap: 6 }}>
            <span className="item-title">{agent.name}</span>
            <Tag tone={agent.enabled ? 'success' : 'neutral'}>{agent.enabled ? '已启用' : '已停用'}</Tag>
            <SaveMark state={mark} />
          </div>
          <span className="tiny muted">按当前范围最多可用 {seen} 条记忆</span>
        </div>
        <div className="row" style={{ gap: 6 }} role="group" aria-label={`${agent.name} 可读取的记忆类型`}>
          {(Object.keys(memoryKindLabel) as MemoryKind[]).map((kind) => {
            const on = agent.memoryKinds.includes(kind)
            return (
              <Chip
                key={kind}
                on={on}
                disabled={!agent.enabled || mark === 'saving'}
                onToggle={() =>
                  void update({ memoryKinds: on ? agent.memoryKinds.filter((k) => k !== kind) : [...agent.memoryKinds, kind] })
                }
              >
                {memoryKindLabel[kind]}
              </Chip>
            )
          })}
        </div>
        <div className="spread">
          <span className="small muted">
            {agent.includeInferred ? '当前包含未确认的 AI 推测' : '当前仅使用你确认或亲口说过的记忆'}
          </span>
          <Switch
            label={`${agent.name} 包含推测`}
            checked={agent.includeInferred}
            disabled={!agent.enabled || mark === 'saving'}
            onChange={(v) => void update({ includeInferred: v })}
          />
        </div>
      </div>
    </div>
  )
}

export function ObserveMemoryTab() {
  const { state } = useStore()
  const pending = state.candidates.filter((c) => c.state === 'pending').length
  const totalMemories = state.memoryTotal ?? state.memories.length
  const excludedThings = Object.keys(state.excludedMemories).filter((id) => (state.excludedMemories[id]?.length ?? 0) > 0).length

  return (
    <div className="stack">
      <div className="obs-overview" role="group" aria-label="记忆与权限概览">
        <div className="obs-tile">
          <span className="k">当前记忆总数</span>
          <span className="v">{totalMemories} 条</span>
          <span className="d">日常工作自动共享，无需逐条审批</span>
        </div>
        <div className={`obs-tile${pending > 0 ? ' warn' : ''}`}>
          <span className="k">待确认内容</span>
          <span className="v">{pending} 条</span>
          <span className="d">{pending > 0 ? '拿不准的待办、想法或记忆' : '目前没有待确认条目'}</span>
        </div>
        <div className="obs-tile">
          <span className="k">整理进度</span>
          <span className="v">{state.organize ? `${state.organize.done}/${state.organize.total}` : '—'}</span>
          <span className="d">{state.organize ? `规则 v${state.organize.version}` : '暂无统计'}</span>
        </div>
        <div className="obs-tile">
          <span className="k">按事项排除记忆</span>
          <span className="v">{excludedThings} 个事项</span>
          <span className="d">在对应事项页单独控制</span>
        </div>
      </div>

      <Sheet
        title={<h3>各模型记忆访问权限</h3>}
        aside={
          <Link className="obs-link-pill" to="/settings#settings-models">
            模型连接与开关
            <ArrowUpRight size={12} />
          </Link>
        }
      >
        {state.agents.length === 0 ? (
          <Empty>还没有配置模型。前往设置连接一个模型后，可在这里查看它的记忆范围。</Empty>
        ) : (
          <div className="list">
            {state.agents.map((agent) => (
              <AgentAccessRow key={agent.id} agent={agent} />
            ))}
          </div>
        )}
      </Sheet>

      <Sheet
        title={<h3>某条记忆多久后少被想起</h3>}
        aside={
          <Link className="obs-link-pill" to={pending > 0 ? '/library?pending=1' : '/library'}>
            {pending > 0 ? `处理 ${pending} 条待确认` : '前往资料库管理记忆'}
            <ArrowUpRight size={12} />
          </Link>
        }
        pad
      >
        <MemoryActivitySettings />
      </Sheet>
    </div>
  )
}
