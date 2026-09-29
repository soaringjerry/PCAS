import { useCallback, useMemo, useState } from 'react'
import { Check, X } from 'lucide-react'
import { FromLine } from '../components/Marks'
import { Button, Empty } from '../components/ui'
import { attentionFor, type Attention } from '../domain/attention'
import { memoryKindLabel } from '../domain/labels'
import { isLiveIdea, isOpenTask } from '../domain/things'
import { formatAgo, formatWhen } from '../domain/time'
import type { Candidate, CandidateKind, MemoryKind } from '../domain/types'
import { useStore } from '../store/context'
import { useListNav } from '../store/useListNav'
import { useToast } from '../store/toast'
import { DemoBanner } from './TodayView'

const kindWords: Record<CandidateKind, string> = { task: '待办', idea: '想法', memory: '记忆' }

type Row = Extract<Attention, { kind: 'candidate' } | { kind: 'confirm' }>

function Adjust({ candidate, onDone }: { candidate: Candidate; onDone: () => void }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [kind, setKind] = useState<CandidateKind>(candidate.kind)
  const [memoryKind, setMemoryKind] = useState<MemoryKind>(candidate.memoryKind ?? 'fact')
  const [projectId, setProjectId] = useState(candidate.projectId ?? '')
  const [text, setText] = useState(candidate.text)
  const [target, setTarget] = useState('')
  const targets =
    kind === 'task'
      ? state.tasks.filter(isOpenTask).map((t) => ({ id: t.id, label: t.title }))
      : kind === 'idea'
        ? state.ideas.filter(isLiveIdea).map((i) => ({ id: i.id, label: i.title }))
        : state.memories.map((m) => ({ id: m.id, label: m.text }))

  return (
    <div className="stack-sm" style={{ margin: '4px 10px 10px 34px', padding: 10, border: '1px solid var(--line)', borderRadius: 8, background: 'var(--panel-2)' }}>
      <input className="input" value={text} onChange={(e) => setText(e.target.value)} aria-label="内容" autoFocus />
      <div className="row">
        <span className="small muted">其实是</span>
        <select className="inline-select" value={kind} onChange={(e) => setKind(e.target.value as CandidateKind)} aria-label="类型">
          {(Object.keys(kindWords) as CandidateKind[]).map((k) => (
            <option key={k} value={k}>
              {kindWords[k]}
            </option>
          ))}
        </select>
        {kind === 'memory' && (
          <select className="inline-select" value={memoryKind} onChange={(e) => setMemoryKind(e.target.value as MemoryKind)} aria-label="记忆类型">
            {(Object.keys(memoryKindLabel) as MemoryKind[]).map((k) => (
              <option key={k} value={k}>
                {memoryKindLabel[k]}
              </option>
            ))}
          </select>
        )}
        <span className="small muted">属于</span>
        <select className="inline-select" value={projectId} onChange={(e) => setProjectId(e.target.value)} aria-label="项目">
          <option value="">不属于项目</option>
          {state.projects.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
        <Button
          size="sm"
          variant="primary"
          icon={<Check size={13} />}
          onClick={() => {
            dispatch({
              type: 'acceptCandidate',
              id: candidate.id,
              kind,
              text: text.trim() || candidate.text,
              memoryKind: kind === 'memory' ? memoryKind : undefined,
              projectId: projectId || undefined,
              due: kind === 'task' ? candidate.due : undefined,
            })
            toast.show('收下了')
            onDone()
          }}
        >
          收下
        </Button>
      </div>
      {candidate.source.excerpt && candidate.source.sourceId !== 'src_capture' && <FromLine source={candidate.source} />}
      <div className="row">
        <span className="small muted">或者并进已有的</span>
        <select className="inline-select" style={{ maxWidth: 300 }} value={target} onChange={(e) => setTarget(e.target.value)} aria-label="合并到">
          <option value="">选一条…</option>
          {targets.map((t) => (
            <option key={t.id} value={t.id}>
              {t.label}
            </option>
          ))}
        </select>
        <Button
          size="sm"
          disabled={!target}
          onClick={() => {
            dispatch({ type: 'mergeCandidate', id: candidate.id, targetId: target })
            toast.show('合并好了')
            onDone()
          }}
        >
          合并
        </Button>
      </div>
    </div>
  )
}

export function InboxView() {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [open, setOpen] = useState<string | null>(null)
  const rows = useMemo(() => attentionFor(state).filter((a): a is Row => a.kind === 'candidate' || a.kind === 'confirm'), [state])
  const ids = useMemo(() => rows.map((r) => r.key), [rows])
  const byKey = useMemo(() => new Map(rows.map((r) => [r.key, r])), [rows])

  const accept = useCallback(
    (keys: string[]) => {
      const cands = keys.map((k) => byKey.get(k)).filter((r): r is Extract<Row, { kind: 'candidate' }> => r?.kind === 'candidate')
      const mems = keys.map((k) => byKey.get(k)).filter((r): r is Extract<Row, { kind: 'confirm' }> => r?.kind === 'confirm')
      if (cands.length) dispatch({ type: 'bulkAccept', ids: cands.map((c) => c.candidate.id) })
      for (const m of mems) dispatch({ type: 'confirmMemory', id: m.memory.id })
      toast.show(`收下 ${keys.length} 条`)
    },
    [byKey, dispatch, toast],
  )
  const reject = useCallback(
    (keys: string[]) => {
      for (const k of keys) {
        const r = byKey.get(k)
        if (r?.kind === 'candidate') dispatch({ type: 'ignoreCandidate', id: r.candidate.id })
        if (r?.kind === 'confirm') dispatch({ type: 'deleteMemory', id: r.memory.id })
      }
    },
    [byKey, dispatch],
  )
  const onKey = useCallback(
    (key: string, keys: string[]) => {
      if (key === 'a') return accept(keys), true
      if (key === 'Backspace' || key === 'Delete') return reject(keys), true
      if (key === 'e' && keys.length === 1 && byKey.get(keys[0])?.kind === 'candidate') return setOpen(keys[0]), true
      return false
    },
    [accept, reject, byKey],
  )
  const onOpen = useCallback((key: string) => setOpen((o) => (o === key ? null : key)), [])
  const nav = useListNav(ids, { onOpen, onKey })
  const done = state.candidates.filter((c) => c.state !== 'pending').length

  return (
    <div className="view">
      <div className="view-head">
        <div>
          <h1>收件</h1>
          <p>从对话、资料和随手记里提取出来的东西。虚线下划线是 AI 猜的，收下之前不算数。</p>
        </div>
      </div>
      <DemoBanner />
      {rows.length === 0 ? (
        <Empty>收件清空了。{done > 0 && `之前处理过 ${done} 条。`}</Empty>
      ) : (
        <div className={nav.picked.length ? 'picking' : undefined}>
          {nav.picked.length > 0 && (
            <div className="bulkbar">
              <span className="grow">已选 {nav.picked.length} 条</span>
              <Button size="sm" onClick={() => (accept(nav.picked), nav.clear())}>
                全部收下 <kbd>A</kbd>
              </Button>
              <Button size="sm" onClick={() => (reject(nav.picked), nav.clear())}>
                都不要 <kbd>⌫</kbd>
              </Button>
              <Button size="sm" onClick={nav.clear}>
                取消 <kbd>Esc</kbd>
              </Button>
            </div>
          )}
          <div className="group">
            <div className="group-head">
              待确认 <span className="n">{rows.length}</span>
              <span className="grow" />
              <Button size="sm" variant="quiet" onClick={() => accept(ids)}>
                全部按猜的收下
              </Button>
            </div>
            {rows.map((r) => {
              const cursor = nav.cursor === r.key
              const picked = nav.picked.includes(r.key)
              const text = r.kind === 'candidate' ? r.candidate.text : r.memory.text
              const project = state.projects.find((p) => p.id === (r.kind === 'candidate' ? r.candidate.projectId : r.memory.projectId))
              const guess =
                r.kind === 'candidate'
                  ? [r.candidate.kind === 'memory' ? memoryKindLabel[r.candidate.memoryKind ?? 'fact'] : kindWords[r.candidate.kind], r.candidate.due ? `截止 ${formatWhen(r.candidate.due)}` : '']
                      .filter(Boolean)
                      .join(' · ')
                  : `${memoryKindLabel[r.memory.kind]} · AI 从资料里读出的`
              const from = r.kind === 'candidate' ? r.candidate.source.label : r.memory.sources[0]?.label
              const at = r.kind === 'candidate' ? r.candidate.createdAt : r.memory.versions[0].at
              return (
                <div key={r.key}>
                  <div className={`lrow${cursor ? ' cursor' : ''}${picked ? ' picked' : ''}`} onClick={() => (nav.setCursor(r.key), onOpen(r.key))}>
                    <input type="checkbox" className="pick" checked={picked} onChange={() => nav.toggle(r.key)} onClick={(e) => e.stopPropagation()} aria-label="选中" />
                    <span className="title guess" style={{ flex: '0 1 auto' }}>
                      {text}
                    </span>
                    <span className="sub grow" style={{ maxWidth: 'none' }}>
                      {guess}
                      {project && ` · ${project.name}`}
                      {from && ` · 来自${from}`}
                    </span>
                    <span className="when">{formatAgo(at)}</span>
                    <span className="acts">
                      <Button size="sm" variant="primary" icon={<Check size={12} />} aria-label="收下" title="收下（A）" onClick={(e) => (e.stopPropagation(), accept([r.key]))} />
                      <Button size="sm" variant="quiet" icon={<X size={12} />} aria-label="不要" title="不要（⌫）" onClick={(e) => (e.stopPropagation(), reject([r.key]))} />
                    </span>
                  </div>
                  {open === r.key && r.kind === 'candidate' && <Adjust candidate={r.candidate} onDone={() => setOpen(null)} />}
                </div>
              )
            })}
          </div>
          <div className="keys">
            <span>
              <kbd>J</kbd>
              <kbd>K</kbd> 移动
            </span>
            <span>
              <kbd>A</kbd> 收下
            </span>
            <span>
              <kbd>E</kbd> 调整
            </span>
            <span>
              <kbd>⌫</kbd> 不要
            </span>
            <span>
              <kbd>X</kbd> 多选
            </span>
          </div>
        </div>
      )}
    </div>
  )
}
