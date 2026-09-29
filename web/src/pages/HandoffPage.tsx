import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { AlertTriangle, Check, ChevronRight, Copy, RefreshCw } from 'lucide-react'
import { TrustTag } from '../components/Marks'
import { Button, Sheet, Tag } from '../components/ui'
import { draftSections, memoriesFor, renderHandoff, sectionTitles } from '../domain/handoff'
import { handoffStatusLabel, memoryKindLabel } from '../domain/labels'
import { findThing, thingTitle } from '../domain/things'
import { formatAgo } from '../domain/time'
import type { Handoff, HandoffSections } from '../domain/types'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'
import { NotFound } from './NotFound'

function StepHead({ n, title, active, hint }: { n: number; title: string; active: boolean; hint?: string }) {
  return (
    <div className="row-nowrap" style={{ marginBottom: 10 }}>
      <span className={`step-no${active ? '' : ' idle'}`}>{n}</span>
      <h2 style={{ fontSize: 18 }}>{title}</h2>
      {hint && <span className="small muted">{hint}</span>}
    </div>
  )
}

function Result({ handoff, backTo }: { handoff: Handoff; backTo?: string }) {
  const { dispatch } = useStore()
  const toast = useToast()
  const [pasted, setPasted] = useState('')
  const [edit, setEdit] = useState(handoff.result?.userEdit ?? handoff.result?.text ?? '')

  if (handoff.status === 'draft') return <p className="small muted">发出去之后，把 AI 的回复贴回这里。</p>
  if (handoff.status === 'sent') {
    return (
      <div className="stack-sm">
        <textarea className="lined" value={pasted} onChange={(e) => setPasted(e.target.value)} placeholder="把 AI 的回复贴在这里…" aria-label="AI 的回复" />
        <div>
          <Button variant="primary" size="sm" disabled={!pasted.trim()} onClick={() => dispatch({ type: 'recordResult', id: handoff.id, text: pasted.trim() })}>
            收回结果
          </Button>
        </div>
      </div>
    )
  }
  if (handoff.status === 'returned') {
    return (
      <div className="stack-sm">
        <p className="small muted">{handoff.result && `${formatAgo(handoff.result.at)}回来的。`}可以先改再采纳，你的修改会记成一次纠正。</p>
        <textarea className="lined" style={{ minHeight: 168 }} value={edit} onChange={(e) => setEdit(e.target.value)} aria-label="结果" />
        <div>
          <Button
            variant="primary"
            size="sm"
            icon={<Check size={14} />}
            onClick={() => {
              dispatch({ type: 'adoptResult', id: handoff.id, userEdit: edit })
              toast.show('采纳了，已经写回原来的事情', backTo ? { to: backTo, label: '回去看看' } : undefined)
            }}
          >
            采纳，写回去
          </Button>
        </div>
      </div>
    )
  }
  return (
    <div className="stack-sm">
      <div className="letter" style={{ fontFamily: 'var(--hand)', fontSize: 15 }}>
        {handoff.result?.userEdit ?? handoff.result?.text}
      </div>
      <p className="small muted">
        已写回{backTo ? <Link to={backTo}>原来的事情</Link> : '项目'}，记成一条记忆，也成了一条训练候选{handoff.result?.userEdit ? '（带着你的修改）' : ''}。
        <Link to="/library?tab=training"> 看训练数据</Link>
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
  const origin = findThing(state, handoff.taskId ?? handoff.ideaId ?? handoff.projectId ?? '')
  const backTo = origin ? `/t/${origin.id}` : undefined
  const step = handoff.status === 'draft' ? 1 : handoff.status === 'sent' || handoff.status === 'returned' ? 3 : 4

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
      <nav className="crumbs" aria-label="位置">
        {origin ? <Link to={backTo!}>{thingTitle(origin)}</Link> : <Link to="/things">事情</Link>}
        <ChevronRight size={13} />
        <span>交接</span>
      </nav>

      <div className="page-head">
        <div className="grow">
          <div className="row" style={{ marginBottom: 6 }}>
            <Tag tone={handoffStatusLabel[handoff.status].tone}>{handoffStatusLabel[handoff.status].text}</Tag>
          </div>
          <input
            className="doc-title"
            value={handoff.title}
            disabled={!editable}
            onChange={(e) => dispatch({ type: 'updateHandoff', id: handoff.id, patch: { title: e.target.value } })}
            aria-label="标题"
          />
        </div>
        <div className="row-nowrap">
          <span className="muted">交给</span>
          <select className="inline-select" value={handoff.agentId} disabled={!editable} onChange={(e) => regenerate(e.target.value)} aria-label="交给哪个 AI">
            {state.agents
              .filter((a) => a.enabled)
              .map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
          </select>
        </div>
      </div>

      {handoff.stale && editable && (
        <div className="note-warn" style={{ marginBottom: 20 }}>
          <AlertTriangle size={16} />
          <div className="grow">写好之后，用到的记忆被改过或删掉了，内容可能过时。</div>
          <Button size="sm" icon={<RefreshCw size={13} />} onClick={() => regenerate()}>
            按最新的重写
          </Button>
        </div>
      )}

      <div className="steps">
        <div className="stack">
          <div>
            <StepHead n={1} title="要交代的" active={step === 1} hint="系统先起了个草稿" />
            <Sheet pad>
              {sectionTitles.map(([key, title]) => (
                <label key={key} className="section-field">
                  <span>{title}</span>
                  <textarea value={handoff.sections[key]} disabled={!editable} onChange={(e) => setSection(key, e.target.value)} rows={1} />
                </label>
              ))}
            </Sheet>
          </div>

          <Sheet title={<h3>附带的记忆</h3>} aside={<span className="tiny muted">{agent.name} 只看得到你授权的部分</span>}>
            <div className="list">
              {related.map((m) => {
                const permitted = allowedIds.has(m.id)
                const checked = handoff.memoryIds.includes(m.id)
                const kindBlocked = !agent.memoryKinds.includes(m.kind)
                const unconfirmed = !kindBlocked && m.epistemic === 'inferred' && !agent.includeInferred
                return (
                  <label key={m.id} className="item check" style={{ opacity: kindBlocked ? 0.5 : 1, alignItems: 'flex-start' }}>
                    <input
                      type="checkbox"
                      style={{ marginTop: 4 }}
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
                      <div className={`ink${m.epistemic === 'inferred' ? ' guess' : ''}`}>{m.text}</div>
                      <div className="meta">
                        <span>{memoryKindLabel[m.kind]}</span>
                        <TrustTag value={m.epistemic} />
                        {kindBlocked && <span>{agent.name} 不能看{memoryKindLabel[m.kind]}</span>}
                        {unconfirmed && <span>还没确认，默认不给</span>}
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
                        没错，带上
                      </Button>
                    )}
                  </label>
                )
              })}
            </div>
          </Sheet>
        </div>

        <div className="stack">
          <div>
            <StepHead n={2} title="发出去" active={step === 1} hint={`${agent.name} 收到的就是这些`} />
            <Sheet pad>
              <div className="stack-sm">
                <div className="letter">{text}</div>
                <div className="row">
                  {handoff.status === 'draft' && (
                    <Button variant="primary" onClick={() => dispatch({ type: 'sendHandoff', id: handoff.id })}>
                      {agent.channel === 'manual' ? '我复制好了' : `发给 ${agent.name}`}
                    </Button>
                  )}
                  <Button
                    icon={copied ? <Check size={14} /> : <Copy size={14} />}
                    onClick={() => {
                      navigator.clipboard?.writeText(text).catch(() => undefined)
                      setCopied(true)
                      window.setTimeout(() => setCopied(false), 1500)
                    }}
                  >
                    {copied ? '复制了' : '复制'}
                  </Button>
                </div>
              </div>
            </Sheet>
          </div>

          <div>
            <StepHead n={3} title="收回来" active={step === 3} />
            <Sheet pad>
              <Result key={handoff.status} handoff={handoff} backTo={backTo} />
            </Sheet>
          </div>
        </div>
      </div>
    </main>
  )
}
