import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { AlertTriangle, ArrowLeft, Check, Copy, RefreshCw } from 'lucide-react'
import { EpistemicBadge } from '../components/bits'
import { Badge, Button, Card, Field } from '../components/ui'
import { draftSections, memoriesFor, renderHandoff, sectionTitles } from '../domain/handoff'
import { handoffStatusLabel, memoryKindLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import type { Handoff, HandoffSections } from '../domain/types'
import { useStore } from '../store/context'
import { NotFound } from './NotFound'

function ResultPanel({ handoff }: { handoff: Handoff }) {
  const { dispatch } = useStore()
  const [pasted, setPasted] = useState('')
  const [edit, setEdit] = useState(handoff.result?.userEdit ?? handoff.result?.text ?? '')

  if (handoff.status === 'draft') {
    return <p className="muted small">发出之后，可以把 AI 的回复贴回这里。</p>
  }
  if (handoff.status === 'sent') {
    return (
      <div className="stack-sm">
        <Field label="把 AI 的回复贴到这里">
          <textarea className="textarea" rows={6} value={pasted} onChange={(e) => setPasted(e.target.value)} />
        </Field>
        <div>
          <Button variant="primary" size="sm" disabled={!pasted.trim()} onClick={() => dispatch({ type: 'recordResult', id: handoff.id, text: pasted.trim() })}>
            记录结果
          </Button>
        </div>
      </div>
    )
  }
  if (handoff.status === 'returned') {
    return (
      <div className="stack-sm">
        <div className="small muted">{handoff.result && `${formatAgo(handoff.result.at)}返回。`}可以先修改再采纳，修改会作为纠正记录。</div>
        <textarea className="textarea" rows={8} value={edit} onChange={(e) => setEdit(e.target.value)} aria-label="结果" />
        <div>
          <Button variant="primary" size="sm" icon={<Check size={14} />} onClick={() => dispatch({ type: 'adoptResult', id: handoff.id, userEdit: edit })}>
            采纳并写回
          </Button>
        </div>
      </div>
    )
  }
  return (
    <div className="stack-sm">
      <div className="code">{handoff.result?.userEdit ?? handoff.result?.text}</div>
      <p className="small muted">
        已写回项目进度，记为一条记忆，并生成一条训练候选{handoff.result?.userEdit ? '（包含你的修改）' : ''}。
        <Link to="/training"> 查看训练数据</Link>
      </p>
    </div>
  )
}

export function HandoffPage() {
  const { id } = useParams()
  const { state, dispatch } = useStore()
  const [copied, setCopied] = useState(false)
  const handoff = state.handoffs.find((h) => h.id === id)
  if (!handoff) return <NotFound />

  const agent = state.agents.find((a) => a.id === handoff.agentId) ?? state.agents[0]
  const allowed = memoriesFor(state, agent, handoff.projectId)
  const allowedIds = new Set(allowed.map((m) => m.id))
  const related = state.memories.filter((m) => !handoff.projectId || !m.projectId || m.projectId === handoff.projectId)
  const text = renderHandoff(handoff, allowed)
  const editable = handoff.status === 'draft' || handoff.status === 'sent'

  const setSection = (key: keyof HandoffSections, value: string) =>
    dispatch({ type: 'updateHandoff', id: handoff.id, patch: { sections: { ...handoff.sections, [key]: value } } })

  const regenerate = (agentId = handoff.agentId) => {
    const nextAgent = state.agents.find((a) => a.id === agentId) ?? agent
    const memories = memoriesFor(state, nextAgent, handoff.projectId)
    dispatch({
      type: 'updateHandoff',
      id: handoff.id,
      patch: {
        agentId,
        sections: draftSections(
          state,
          {
            projectId: handoff.projectId,
            task: state.tasks.find((t) => t.id === handoff.taskId),
            idea: state.ideas.find((i) => i.id === handoff.ideaId),
          },
          memories,
        ),
        memoryIds: memories.map((m) => m.id),
        stale: false,
      },
    })
  }

  return (
    <main className="page">
      <Link to="/handoffs" className="chip" style={{ marginBottom: 10 }}>
        <ArrowLeft size={14} /> 全部交接
      </Link>
      <div className="page-head">
        <div style={{ flex: 1, minWidth: 240 }}>
          <div className="row" style={{ marginBottom: 6 }}>
            <Badge tone={handoffStatusLabel[handoff.status].tone}>{handoffStatusLabel[handoff.status].text}</Badge>
            <span className="chip">{state.projects.find((p) => p.id === handoff.projectId)?.name}</span>
          </div>
          <input
            className="input"
            style={{ fontSize: 20, fontWeight: 600, border: 0, padding: 0, background: 'transparent', boxShadow: 'none' }}
            value={handoff.title}
            disabled={!editable}
            onChange={(e) => dispatch({ type: 'updateHandoff', id: handoff.id, patch: { title: e.target.value } })}
            aria-label="标题"
          />
        </div>
        <Field label="交给">
          <select
            className="select"
            value={handoff.agentId}
            disabled={!editable}
            onChange={(e) => regenerate(e.target.value)}
          >
            {state.agents
              .filter((a) => a.enabled)
              .map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
          </select>
        </Field>
      </div>

      <div className="stack">
        {handoff.stale && editable && (
          <div className="stale-note">
            <AlertTriangle size={16} />
            <div className="grow">用到的记忆在生成后被修改或删除了，内容可能已经过时。</div>
            <Button size="sm" icon={<RefreshCw size={14} />} onClick={() => regenerate()}>
              按最新记忆重新生成
            </Button>
          </div>
        )}

        <div className="grid-2" style={{ alignItems: 'start' }}>
          <div className="stack">
            <Card title="内容" hint="自动生成的初稿，发出前可以随意修改" pad>
              <div className="stack-sm">
                {sectionTitles.map(([key, title]) => (
                  <Field key={key} label={title}>
                    <textarea
                      className="textarea"
                      rows={key === 'background' || key === 'progress' ? 4 : 2}
                      value={handoff.sections[key]}
                      disabled={!editable}
                      onChange={(e) => setSection(key, e.target.value)}
                    />
                  </Field>
                ))}
              </div>
            </Card>

            <Card title="附带的记忆" hint={`${agent.name} 只能看到授权范围内的记忆`}>
              <div className="list">
                {related.map((m) => {
                  const permitted = allowedIds.has(m.id)
                  const checked = handoff.memoryIds.includes(m.id)
                  const kindBlocked = !agent.memoryKinds.includes(m.kind)
                  const unconfirmed = !kindBlocked && m.epistemic === 'inferred' && !agent.includeInferred
                  return (
                    <label key={m.id} className="list-item check" style={{ opacity: kindBlocked ? 0.5 : 1, cursor: permitted && editable ? 'pointer' : 'not-allowed' }}>
                      <input
                        type="checkbox"
                        checked={permitted && checked}
                        disabled={!permitted || !editable}
                        onChange={() =>
                          dispatch({
                            type: 'updateHandoff',
                            id: handoff.id,
                            patch: { memoryIds: checked ? handoff.memoryIds.filter((x) => x !== m.id) : [...handoff.memoryIds, m.id] },
                          })
                        }
                      />
                      <div className="grow">
                        <div className="ink">{m.text}</div>
                        <div className="meta">
                          <EpistemicBadge value={m.epistemic} />
                          <span>{memoryKindLabel[m.kind]}</span>
                          {kindBlocked && <span>{agent.name} 无权查看{memoryKindLabel[m.kind]}</span>}
                          {unconfirmed && <span>未确认，默认不提供给 {agent.name}</span>}
                        </div>
                      </div>
                      {unconfirmed && editable && (
                        <Button
                          size="sm"
                          onClick={(e) => {
                            e.preventDefault()
                            dispatch({ type: 'confirmMemory', id: m.id })
                            dispatch({ type: 'updateHandoff', id: handoff.id, patch: { memoryIds: [...handoff.memoryIds, m.id] } })
                          }}
                        >
                          确认并附带
                        </Button>
                      )}
                    </label>
                  )
                })}
              </div>
            </Card>
          </div>

          <div className="stack">
            <Card
              title="预览"
              hint={`${agent.name} 实际收到的内容`}
              action={
                <Button
                  size="sm"
                  icon={copied ? <Check size={14} /> : <Copy size={14} />}
                  onClick={() => {
                    navigator.clipboard?.writeText(text).catch(() => undefined)
                    setCopied(true)
                    window.setTimeout(() => setCopied(false), 1500)
                  }}
                >
                  {copied ? '已复制' : '复制'}
                </Button>
              }
              pad
            >
              <div className="stack-sm">
                <div className="code">{text}</div>
                {handoff.status === 'draft' && (
                  <div>
                    <Button variant="primary" onClick={() => dispatch({ type: 'sendHandoff', id: handoff.id })}>
                      {agent.channel === 'manual' ? '已复制并发出' : `发给 ${agent.name}`}
                    </Button>
                  </div>
                )}
              </div>
            </Card>

            <Card title="返回结果" pad>
              <ResultPanel key={handoff.status} handoff={handoff} />
            </Card>
          </div>
        </div>
      </div>
    </main>
  )
}
