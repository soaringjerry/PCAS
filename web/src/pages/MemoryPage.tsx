import { useState } from 'react'
import { Search, Trash2 } from 'lucide-react'
import { EpistemicBadge, ProjectName, SourceLine } from '../components/bits'
import { Drawer, Modal } from '../components/Overlay'
import { Badge, Button, Card, Empty, Field, Tabs } from '../components/ui'
import { memoryKindLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import type { Epistemic, Memory, MemoryKind } from '../domain/types'
import { useStore } from '../store/context'

function Exposure({ value }: { value: number }) {
  return (
    <span className="exposure" title="曝光度：长期不用会降低，只影响排序，不会删除">
      <span className="exposure-bar">
        <div style={{ width: `${Math.round(value * 100)}%` }} />
      </span>
      曝光 {Math.round(value * 100)}%
    </span>
  )
}

const actorText = { user: '你', ai: 'AI', import: '导入', system: '系统' } as const

function MemoryDrawer({ memory, onClose }: { memory: Memory; onClose: () => void }) {
  const { state, dispatch } = useStore()
  const [text, setText] = useState(memory.text)
  const [reason, setReason] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const handoffs = state.handoffs.filter((h) => h.memoryIds.includes(memory.id))
  const samples = state.samples.filter((s) => s.origin.memoryId === memory.id)
  const changed = text.trim() !== memory.text

  return (
    <Drawer
      title={memoryKindLabel[memory.kind]}
      onClose={onClose}
      badges={
        <>
          <EpistemicBadge value={memory.epistemic} />
          <ProjectName id={memory.projectId} />
        </>
      }
    >
      <div className="drawer-section">
        <Field label="内容">
          <textarea className="textarea" value={text} onChange={(e) => setText(e.target.value)} />
        </Field>
        {changed && (
          <Field label="为什么修改（会成为纠正记录）">
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="例如：之前理解错了" />
          </Field>
        )}
        <div className="row">
          <Button
            variant="primary"
            size="sm"
            disabled={!changed || !text.trim()}
            onClick={() => {
              dispatch({ type: 'editMemory', id: memory.id, text: text.trim(), reason: reason.trim() })
              setReason('')
            }}
          >
            保存为新版本
          </Button>
          {memory.epistemic !== 'confirmed' && !changed && (
            <Button size="sm" onClick={() => dispatch({ type: 'confirmMemory', id: memory.id })}>
              确认无误
            </Button>
          )}
        </div>
        <Exposure value={memory.exposure} />
        <div className="small muted">最近一次被用到：{formatAgo(memory.lastUsedAt)}</div>
      </div>

      <div className="drawer-section">
        <h3>哪些 AI 可以看到</h3>
        <div className="row">
          {state.agents.map((agent) => {
            const checked = memory.visibleTo.includes(agent.id)
            return (
              <label key={agent.id} className="check">
                <input
                  type="checkbox"
                  checked={checked}
                  onChange={() =>
                    dispatch({
                      type: 'setMemoryVisibility',
                      id: memory.id,
                      agentIds: checked ? memory.visibleTo.filter((a) => a !== agent.id) : [...memory.visibleTo, agent.id],
                    })
                  }
                />
                {agent.name}
              </label>
            )
          })}
        </div>
      </div>

      <div className="drawer-section">
        <h3>版本历史</h3>
        <ul className="timeline">
          {memory.versions.map((v, i) => (
            <li key={`${v.at}-${i}`} className={i === memory.versions.length - 1 ? 'current' : undefined}>
              <div className={i === memory.versions.length - 1 ? 'ink' : 'muted'}>{v.text}</div>
              <div className="small muted">
                {actorText[v.by]} · {formatAgo(v.at)}
                {v.reason && ` · ${v.reason}`}
              </div>
            </li>
          ))}
        </ul>
      </div>

      {memory.sources.length > 0 && (
        <div className="drawer-section">
          <h3>来源</h3>
          {memory.sources.map((s, i) => (
            <SourceLine key={i} source={s} />
          ))}
        </div>
      )}

      <div className="drawer-section">
        <h3>被谁用到</h3>
        <p className="small muted">
          {handoffs.length} 份交接 · {samples.length} 条训练样本
        </p>
        <Button size="sm" variant="ghost" className="btn-danger" icon={<Trash2 size={14} />} onClick={() => setConfirmDelete(true)}>
          删除这条记忆
        </Button>
      </div>

      {confirmDelete && (
        <Modal
          title="删除这条记忆？"
          onClose={() => setConfirmDelete(false)}
          actions={
            <>
              <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
                取消
              </Button>
              <Button
                variant="primary"
                onClick={() => {
                  dispatch({ type: 'deleteMemory', id: memory.id })
                  onClose()
                }}
              >
                删除
              </Button>
            </>
          }
        >
          <p>删除后，之后的检索、摘要、交接和导出都不会再用到它。</p>
          {(handoffs.length > 0 || samples.length > 0) && (
            <p className="small muted">
              {handoffs.length > 0 && `${handoffs.length} 份用到它的交接会标记为需要重新生成。`}
              {samples.length > 0 && `${samples.length} 条由它产生的训练样本会被排除。`}
            </p>
          )}
        </Modal>
      )}
    </Drawer>
  )
}

export function MemoryPage() {
  const { state } = useStore()
  const [kind, setKind] = useState<MemoryKind | 'all'>('all')
  const [epistemic, setEpistemic] = useState<Epistemic | ''>('')
  const [query, setQuery] = useState('')
  const [openId, setOpenId] = useState<string | null>(null)
  const open = state.memories.find((m) => m.id === openId)

  const memories = state.memories
    .filter((m) => kind === 'all' || m.kind === kind)
    .filter((m) => !epistemic || m.epistemic === epistemic)
    .filter((m) => !query.trim() || m.text.includes(query.trim()))
    .sort((a, b) => b.exposure - a.exposure)

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>记忆</h1>
          <p>事实、偏好和决定。都可以查看、修改和删除；长期不用只会降低曝光，不会消失。</p>
        </div>
        <Tabs
          value={kind}
          onChange={setKind}
          items={[
            { value: 'all', label: '全部', count: state.memories.length },
            ...(Object.keys(memoryKindLabel) as MemoryKind[]).map((k) => ({
              value: k,
              label: memoryKindLabel[k],
              count: state.memories.filter((m) => m.kind === k).length,
            })),
          ]}
        />
      </div>

      <div className="stack">
        <div className="row" style={{ flexWrap: 'nowrap' }}>
          <div className="capture" style={{ maxWidth: 'none', boxShadow: 'none' }}>
            <Search size={16} />
            <input placeholder="搜索记忆…" value={query} onChange={(e) => setQuery(e.target.value)} aria-label="搜索记忆" />
          </div>
          <select className="select" style={{ width: 'auto' }} value={epistemic} onChange={(e) => setEpistemic(e.target.value as Epistemic | '')} aria-label="可信度">
            <option value="">全部可信度</option>
            <option value="confirmed">已确认</option>
            <option value="inferred">AI 推测</option>
            <option value="planned">计划</option>
          </select>
        </div>

        <Card>
          {memories.length === 0 ? (
            <Empty>没有符合条件的记忆。</Empty>
          ) : (
            <div className="list">
              {memories.map((m) => (
                <div key={m.id} className="list-item clickable" onClick={() => setOpenId(m.id)} style={{ opacity: 0.55 + m.exposure * 0.45 }}>
                  <div className="grow">
                    <div className="item-title">{m.text}</div>
                    <div className="meta">
                      <EpistemicBadge value={m.epistemic} />
                      <Badge>{memoryKindLabel[m.kind]}</Badge>
                      <Exposure value={m.exposure} />
                      {m.versions.length > 1 && <span>{m.versions.length} 个版本</span>}
                      <span>{m.visibleTo.length ? `${m.visibleTo.length} 个 AI 可见` : '不共享'}</span>
                      <ProjectName id={m.projectId} />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>

      {open && <MemoryDrawer key={open.id} memory={open} onClose={() => setOpenId(null)} />}
    </main>
  )
}
