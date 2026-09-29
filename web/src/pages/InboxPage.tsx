import { useState } from 'react'
import { Check, GitMerge, X } from 'lucide-react'
import { SourceLine } from '../components/bits'
import { Badge, Button, Card, Empty, Field, Tabs } from '../components/ui'
import { candidateKindLabel, memoryKindLabel } from '../domain/labels'
import { formatAgo, fromLocalInput, toLocalInput } from '../domain/time'
import type { Candidate, CandidateKind, MemoryKind } from '../domain/types'
import { useStore } from '../store/context'

function mergeTargets(kind: CandidateKind) {
  return (state: ReturnType<typeof useStore>['state']) => {
    if (kind === 'task')
      return state.tasks.filter((t) => t.status !== 'done' && t.status !== 'cancelled').map((t) => ({ id: t.id, label: t.title }))
    if (kind === 'idea') return state.ideas.filter((i) => i.status !== 'dropped').map((i) => ({ id: i.id, label: i.title }))
    return state.memories.map((m) => ({ id: m.id, label: m.text }))
  }
}

function CandidateCard({ candidate }: { candidate: Candidate }) {
  const { state, dispatch } = useStore()
  const [kind, setKind] = useState<CandidateKind>(candidate.kind)
  const [memoryKind, setMemoryKind] = useState<MemoryKind>(candidate.memoryKind ?? 'fact')
  const [text, setText] = useState(candidate.text)
  const [projectId, setProjectId] = useState(candidate.projectId ?? '')
  const [due, setDue] = useState(toLocalInput(candidate.due))
  const [merging, setMerging] = useState(false)
  const targets = mergeTargets(kind)(state)
  const [target, setTarget] = useState('')

  return (
    <div className="card card-pad stack-sm">
      <div className="row-between">
        <div className="row">
          <Badge tone="warning">AI 推测</Badge>
          <span className="chip">置信度 {Math.round(candidate.confidence * 100)}%</span>
        </div>
        <span className="small muted">{formatAgo(candidate.createdAt)}</span>
      </div>

      <textarea className="textarea" rows={2} value={text} onChange={(e) => setText(e.target.value)} aria-label="内容" />

      <div className="grid-3" style={{ gap: 10 }}>
        <Field label="类型">
          <select className="select" value={kind} onChange={(e) => setKind(e.target.value as CandidateKind)}>
            {(Object.keys(candidateKindLabel) as CandidateKind[]).map((k) => (
              <option key={k} value={k}>
                {candidateKindLabel[k]}
              </option>
            ))}
          </select>
        </Field>
        {kind === 'memory' && (
          <Field label="记忆类型">
            <select className="select" value={memoryKind} onChange={(e) => setMemoryKind(e.target.value as MemoryKind)}>
              {(Object.keys(memoryKindLabel) as MemoryKind[]).map((k) => (
                <option key={k} value={k}>
                  {memoryKindLabel[k]}
                </option>
              ))}
            </select>
          </Field>
        )}
        {kind === 'task' && (
          <Field label="截止">
            <input type="datetime-local" className="input" value={due} onChange={(e) => setDue(e.target.value)} />
          </Field>
        )}
        <Field label="项目">
          <select className="select" value={projectId} onChange={(e) => setProjectId(e.target.value)}>
            <option value="">不属于项目</option>
            {state.projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
        </Field>
      </div>

      <SourceLine source={candidate.source} />

      {merging ? (
        <div className="row" style={{ flexWrap: 'nowrap' }}>
          <select className="select" value={target} onChange={(e) => setTarget(e.target.value)} aria-label="合并到">
            <option value="">选择要合并到的{candidateKindLabel[kind]}…</option>
            {targets.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
              </option>
            ))}
          </select>
          <Button
            variant="primary"
            size="sm"
            disabled={!target}
            onClick={() => dispatch({ type: 'mergeCandidate', id: candidate.id, targetId: target })}
          >
            合并
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setMerging(false)}>
            取消
          </Button>
        </div>
      ) : (
        <div className="row">
          <Button
            variant="primary"
            size="sm"
            icon={<Check size={14} />}
            disabled={!text.trim()}
            onClick={() =>
              dispatch({
                type: 'acceptCandidate',
                id: candidate.id,
                kind,
                text: text.trim(),
                memoryKind: kind === 'memory' ? memoryKind : undefined,
                projectId: projectId || undefined,
                due: kind === 'task' ? fromLocalInput(due) : undefined,
              })
            }
          >
            采纳
          </Button>
          <Button size="sm" icon={<GitMerge size={14} />} onClick={() => setMerging(true)}>
            合并到已有
          </Button>
          <Button size="sm" variant="ghost" icon={<X size={14} />} onClick={() => dispatch({ type: 'ignoreCandidate', id: candidate.id })}>
            忽略
          </Button>
        </div>
      )}
    </div>
  )
}

const resolvedText = { accepted: '已采纳', merged: '已合并', ignored: '已忽略', pending: '' } as const

export function InboxPage() {
  const { state, dispatch } = useStore()
  const [tab, setTab] = useState<'pending' | 'done'>('pending')
  const pending = state.candidates.filter((c) => c.state === 'pending')
  const done = state.candidates.filter((c) => c.state !== 'pending')

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>收件箱</h1>
          <p>从对话、资料和快速记录里提取的候选。采纳前都只是 AI 的推测。</p>
        </div>
        <Tabs
          value={tab}
          onChange={setTab}
          items={[
            { value: 'pending', label: '待整理', count: pending.length },
            { value: 'done', label: '已处理', count: done.length },
          ]}
        />
      </div>

      {tab === 'pending' ? (
        pending.length === 0 ? (
          <Card pad>
            <Empty>都整理完了。用顶部的输入框随手记一句，它会出现在这里。</Empty>
          </Card>
        ) : (
          <div className="stack">
            {pending.map((c) => (
              <CandidateCard key={c.id} candidate={c} />
            ))}
          </div>
        )
      ) : (
        <Card>
          {done.length === 0 ? (
            <Empty>还没有处理过的候选。</Empty>
          ) : (
            <div className="list">
              {done.map((c) => (
                <div key={c.id} className="list-item">
                  <div className="grow">
                    <div className="item-title">{c.text}</div>
                    <div className="meta">
                      <Badge tone={c.state === 'ignored' ? 'neutral' : 'success'}>{resolvedText[c.state]}</Badge>
                      <span>{candidateKindLabel[c.kind]}</span>
                      <span>{c.source.label}</span>
                    </div>
                  </div>
                  {c.state === 'ignored' && (
                    <Button size="sm" variant="ghost" onClick={() => dispatch({ type: 'restoreCandidate', id: c.id })}>
                      撤销
                    </Button>
                  )}
                </div>
              ))}
            </div>
          )}
        </Card>
      )}
    </main>
  )
}
