import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { ArrowUp, Check, ChevronDown, ChevronLeft, ChevronRight, Copy, Plus, Sparkles, X } from 'lucide-react'
import { Markdown } from '../components/Markdown'
import { LineRow } from '../components/LineRow'
import { Timeline } from '../components/Marks'
import { buildBrief, contextFor, parseChecklist, quickActions } from '../domain/agent'
import { newId } from '../domain/ids'
import { taskStatusLabel, taskStatusOrder } from '../domain/labels'
import { estimateCost, ongoingLine, spentToday, urgentLine } from '../domain/lines'
import { findThing, thingProjectId, thingTitle, timelineFor, type Thing } from '../domain/things'
import { formatAgo, fromLocalInput, toLocalInput } from '../domain/time'
import type { Doc, Run, RunKind, Task } from '../domain/types'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { useToast } from '../store/toast'
import { NotFound } from './NotFound'

function Header({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  const project = state.projects.find((p) => p.id === thingProjectId(thing))
  const notes = thing.kind === 'project' ? thing.item.goal : thing.kind === 'idea' ? thing.item.body : (thing.item.notes ?? '')

  return (
    <>
      <Link to={project ? `/t/${project.id}` : '/'} className="back">
        <ChevronLeft size={16} />
        {project ? project.name : '首页'}
      </Link>
      <textarea
        key={`title-${thing.id}`}
        className="doc-title"
        rows={1}
        defaultValue={thingTitle(thing)}
        aria-label="标题"
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
            e.preventDefault()
            e.currentTarget.blur()
          }
        }}
        onBlur={(e) => {
          const title = e.target.value.trim()
          if (title && title !== thingTitle(thing)) dispatch({ type: 'renameThing', id: thing.id, title })
        }}
      />
      <Meta thing={thing} />
      <textarea
        key={`notes-${thing.id}`}
        className="notes"
        defaultValue={notes}
        placeholder={thing.kind === 'project' ? '这个项目想做成什么' : '补充点什么'}
        aria-label="说明"
        onBlur={(e) => {
          if (e.target.value === notes) return
          if (thing.kind === 'project') dispatch({ type: 'updateProject', id: thing.id, patch: { goal: e.target.value } })
          else dispatch({ type: 'setNotes', id: thing.id, text: e.target.value })
        }}
      />
    </>
  )
}

function ProjectPicker({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  if (thing.kind === 'project') return null
  return (
    <label className="meta-ctl">
      <select value={thing.item.projectId ?? ''} onChange={(e) => dispatch({ type: 'moveThing', id: thing.id, projectId: e.target.value || undefined })} aria-label="项目">
        <option value="">不属于项目</option>
        {state.projects.map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
    </label>
  )
}

function DueControl({ task }: { task: Task }) {
  const { dispatch } = useStore()
  const [picking, setPicking] = useState(false)
  if (!task.due && !picking) {
    return (
      <button type="button" className="link-btn meta-ctl" style={{ fontSize: 13 }} onClick={() => setPicking(true)}>
        没有截止
      </button>
    )
  }
  return (
    <label className="meta-ctl">
      <input
        type="datetime-local"
        value={toLocalInput(task.due)}
        aria-label="截止"
        title="截止时间；清空就是没有截止"
        autoFocus={picking}
        onBlur={() => setPicking(false)}
        onChange={(e) =>
          dispatch({ type: 'updateTask', id: task.id, patch: { due: fromLocalInput(e.target.value) }, summary: e.target.value ? '改了截止时间' : '去掉截止时间' })
        }
      />
    </label>
  )
}

function Meta({ thing }: { thing: Thing }) {
  const { dispatch } = useStore()

  if (thing.kind === 'task') {
    const t = thing.item
    return (
      <div className="meta-line">
        <label className="meta-ctl">
          <select value={t.status} onChange={(e) => dispatch({ type: 'setTaskStatus', id: t.id, status: e.target.value as Task['status'] })} aria-label="状态">
            {taskStatusOrder.map((s) => (
              <option key={s} value={s}>
                {taskStatusLabel[s].text}
              </option>
            ))}
          </select>
        </label>
        <DueControl task={t} />
        <ProjectPicker thing={thing} />
        {t.owedTo && (
          <button
            type="button"
            className="link-btn meta-flag"
            title="对方已经不用等了"
            onClick={() => dispatch({ type: 'updateTask', id: t.id, patch: { owedTo: undefined }, summary: `${t.owedTo?.who}不用再等了` })}
          >
            {t.owedTo.who}在等你 ×
          </button>
        )}
        {t.status === 'waiting' && t.waitingFor && <span>在等{t.waitingFor}</span>}
      </div>
    )
  }

  if (thing.kind === 'idea') {
    const i = thing.item
    const label = { active: '想法', awakened: '想法 · 刚被唤醒', shelved: '想法 · 放着', promoted: '想法 · 已转成待办', dropped: '想法 · 不做了' }[i.status]
    return (
      <div className="meta-line">
        <span>{label}</span>
        <ProjectPicker thing={thing} />
        {i.status === 'active' && (
          <button type="button" className="link-btn" onClick={() => dispatch({ type: 'ideaPromote', id: i.id })}>
            转成待办
          </button>
        )}
      </div>
    )
  }

  const p = thing.item
  return (
    <div className="meta-line">
      <label className="meta-ctl">
        <select value={p.status} onChange={(e) => dispatch({ type: 'updateProject', id: p.id, patch: { status: e.target.value as typeof p.status } })} aria-label="项目状态">
          <option value="active">进行中</option>
          <option value="paused">暂停</option>
          <option value="done">完成</option>
        </select>
      </label>
      {p.progress && <span className="ellipsis">{p.progress.split('\n').at(-1)}</span>}
    </div>
  )
}

function IdeaBanner({ thing }: { thing: Extract<Thing, { kind: 'idea' }> }) {
  const { state, dispatch } = useStore()
  const i = thing.item
  const [cond, setCond] = useState('')

  if (i.status === 'awakened') {
    return (
      <div className="banner">
        <Sparkles size={16} />
        <div className="grow">
          <p>{i.wake?.reason ?? '它等的条件满足了。'}</p>
          <div className="row" style={{ marginTop: 10 }}>
            <button type="button" className="btn btn-primary btn-sm" onClick={() => dispatch({ type: 'ideaPromote', id: i.id })}>
              转成待办
            </button>
            <button type="button" className="btn btn-quiet btn-sm" onClick={() => dispatch({ type: 'ideaSnooze', id: i.id, days: 7 })}>
              先放着
            </button>
          </div>
        </div>
      </div>
    )
  }

  if (i.status === 'promoted') {
    const task = state.tasks.find((t) => t.ideaId === i.id)
    return task ? (
      <div className="banner">
        <Check size={16} />
        <p>
          已经转成待办：<Link to={`/t/${task.id}`}>{task.title}</Link>
        </p>
      </div>
    ) : null
  }

  if (i.status !== 'shelved') return null
  return (
    <section className="section">
      <div className="section-label">满足这些条件时，它会自己回来</div>
      <div className="group">
        {i.conditions.map((c) => (
          <div key={c.id} className="check-row">
            <span className={`circle${c.met ? ' on' : ''}`} aria-hidden>
              {c.met && <Check size={12} strokeWidth={3} />}
            </span>
            <span className={`text${c.met ? ' done' : ''}`}>{c.description}</span>
            <button type="button" className="btn btn-quiet btn-icon btn-sm del" aria-label="删掉条件" onClick={() => dispatch({ type: 'removeCondition', ideaId: i.id, conditionId: c.id })}>
              <X size={14} />
            </button>
          </div>
        ))}
        <form
          className="check-row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!cond.trim()) return
            dispatch({ type: 'addCondition', ideaId: i.id, description: cond.trim() })
            setCond('')
          }}
        >
          <span className="plus">
            <Plus size={16} />
          </span>
          <input className="add" value={cond} onChange={(e) => setCond(e.target.value)} placeholder="再加一个条件" aria-label="再加一个条件" />
        </form>
      </div>
    </section>
  )
}

function Checklist({ task }: { task: Task }) {
  const { dispatch } = useStore()
  const [text, setText] = useState('')
  const done = task.checklist.filter((c) => c.done).length

  return (
    <section className="section">
      <div className="section-label">
        子任务{task.checklist.length > 0 && <span className="faint"> · {done}/{task.checklist.length}</span>}
      </div>
      <div className="group">
        {task.checklist.map((c) => (
          <div key={c.id} className="check-row">
            <button
              type="button"
              className={`circle${c.done ? ' on' : ''}`}
              aria-label={c.done ? '标为未完成' : '完成'}
              onClick={() => dispatch({ type: 'toggleCheck', taskId: task.id, itemId: c.id })}
            >
              {c.done && <Check size={12} strokeWidth={3} />}
            </button>
            <span className={`text${c.done ? ' done' : ''}`}>{c.text}</span>
            <button type="button" className="btn btn-quiet btn-icon btn-sm del" aria-label="删掉" onClick={() => dispatch({ type: 'removeCheck', taskId: task.id, itemId: c.id })}>
              <X size={14} />
            </button>
          </div>
        ))}
        <form
          className="check-row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!text.trim()) return
            dispatch({ type: 'addCheck', taskId: task.id, text: text.trim() })
            setText('')
          }}
        >
          <span className="plus">
            <Plus size={16} />
          </span>
          <input className="add" value={text} onChange={(e) => setText(e.target.value)} placeholder="加一步" aria-label="加一步" />
        </form>
      </div>
    </section>
  )
}

function ProjectItems({ projectId }: { projectId: string }) {
  const { state, dispatch } = useStore()
  const [text, setText] = useState('')
  const [showParked, setShowParked] = useState(false)
  const mine = (i: { thing: Thing }) => thingProjectId(i.thing) === projectId
  const urgent = urgentLine(state).filter(mine)
  const { active, parked } = ongoingLine(state)
  const items = [...urgent, ...active.filter(mine)]
  const shelved = parked.filter(mine)
  const done = state.tasks.filter((t) => t.projectId === projectId && t.status === 'done').length

  return (
    <section className="section">
      <div className="section-label">
        里面的事{done > 0 && <span className="faint"> · 已完成 {done} 件</span>}
      </div>
      <div className="group">
        {items.map((i) => (
          <LineRow key={i.thing.id} item={i} />
        ))}
        {shelved.length > 0 && (
          <>
            <button type="button" className="disclosure" onClick={() => setShowParked((v) => !v)} aria-expanded={showParked}>
              {showParked ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
              {shelved.length} 个想法放着
            </button>
            {showParked && shelved.map((i) => <LineRow key={i.thing.id} item={i} />)}
          </>
        )}
        <form
          className="check-row"
          onSubmit={(e) => {
            e.preventDefault()
            if (!text.trim()) return
            dispatch({ type: 'addTask', title: text.trim(), projectId })
            setText('')
          }}
        >
          <span className="plus">
            <Plus size={16} />
          </span>
          <input className="add" value={text} onChange={(e) => setText(e.target.value)} placeholder="加一件事" aria-label="加一件事" />
        </form>
      </div>
    </section>
  )
}

/** What adopting a result does, decided from the result itself. */
function adoptAs(thing: Thing, run: Run, text: string): { as: 'doc' | 'subtasks' | 'progress'; label: string } {
  const n = parseChecklist(text).length
  if (n > 0) return { as: 'subtasks', label: thing.kind === 'task' ? `加进子任务（${n}）` : `建成 ${n} 件待办` }
  if (run.kind === 'summary') return { as: 'progress', label: '写进进度' }
  return { as: 'doc', label: '存成文档' }
}

const adoptedText = { doc: '存成了文档', subtasks: '加成了待办', progress: '写进了进度' } as const

function RunCard({ thing, run }: { thing: Thing; run: Run }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const agent = state.agents.find((a) => a.id === run.agentId)
  const [editing, setEditing] = useState(false)
  const [text, setText] = useState(run.output ?? '')
  const [pasted, setPasted] = useState('')
  const [expanded, setExpanded] = useState(false)
  const [basis, setBasis] = useState(false)

  if (run.adopted) {
    return (
      <div className="card">
        <div className="card-head" style={{ paddingBottom: 12 }}>
          <span className="who">{agent?.name ?? 'AI'}</span>
          <span className="grow ellipsis">{run.prompt}</span>
          <span className="faint">
            已采纳，{adoptedText[run.adopted.as]}
            {run.adopted.edited ? '（改过）' : ''}
          </span>
        </div>
      </div>
    )
  }

  const choice = adoptAs(thing, run, editing ? text : (run.output ?? ''))

  return (
    <div className="card">
      <div className="card-head">
        <span className="who">{agent?.name ?? 'AI'}</span>
        <span className="grow ellipsis">{run.prompt}</span>
        <span className="faint">{formatAgo(run.createdAt)}</span>
      </div>

      {run.status === 'running' && (
        <div className="card-body">
          <div className="shimmer" aria-label="正在做">
            <i style={{ width: '82%' }} />
            <i style={{ width: '64%' }} />
            <i style={{ width: '71%' }} />
          </div>
        </div>
      )}

      {run.status === 'waiting' && (
        <div className="card-body stack-sm">
          <p className="small muted">这个副手要你手动转交：复制下面的内容发给它，再把回答贴回来。</p>
          <div className="row">
            <button
              type="button"
              className="btn btn-sm"
              onClick={() => {
                void navigator.clipboard?.writeText(run.brief).then(
                  () => toast.show('复制好了'),
                  () => toast.show('复制失败，可以展开手动选'),
                )
              }}
            >
              <Copy size={14} />
              复制给它的内容
            </button>
            <button type="button" className="link-btn" onClick={() => setBasis((v) => !v)}>
              {basis ? '收起' : '看一眼'}
            </button>
          </div>
          {basis && (
            <div className="basis" style={{ padding: 0, borderTop: 0 }}>
              <pre>{run.brief}</pre>
            </div>
          )}
          <textarea value={pasted} onChange={(e) => setPasted(e.target.value)} placeholder="把它的回答贴在这里" aria-label="贴回回答" style={{ minHeight: 120 }} />
          <div className="row">
            <button type="button" className="btn btn-primary btn-sm" disabled={!pasted.trim()} onClick={() => dispatch({ type: 'pasteRunResult', id: run.id, output: pasted.trim() })}>
              放回来
            </button>
            <button type="button" className="btn btn-quiet btn-sm" onClick={() => dispatch({ type: 'discardRun', id: run.id })}>
              算了
            </button>
          </div>
        </div>
      )}

      {run.status === 'failed' && (
        <div className="card-body small muted">没做成。{run.output}</div>
      )}

      {run.status === 'done' && run.output && (
        <>
          <div className="card-body">
            {editing ? (
              <textarea value={text} onChange={(e) => setText(e.target.value)} aria-label="修改结果" autoFocus />
            ) : (
              <div className={expanded ? '' : 'clamp'} onClick={() => setExpanded(true)} style={{ cursor: expanded ? undefined : 'pointer' }}>
                <Markdown text={run.output} />
              </div>
            )}
          </div>
          {run.staleContext && <p className="card-body small" style={{ paddingTop: 0, color: 'var(--orange)' }}>它用到的记忆后来改过，结果可能过时了。</p>}
          <div className="card-foot">
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => {
                dispatch({ type: 'adoptRun', id: run.id, as: choice.as, text: editing ? text : run.output! })
                toast.show(`采纳了，${adoptedText[choice.as]}`)
              }}
            >
              {choice.label}
            </button>
            <button
              type="button"
              className="btn btn-quiet btn-sm"
              onClick={() => {
                setText(run.output ?? '')
                setEditing((v) => !v)
              }}
            >
              {editing ? '不改了' : '改一下'}
            </button>
            <button type="button" className="btn btn-quiet btn-sm" onClick={() => dispatch({ type: 'discardRun', id: run.id })}>
              不要
            </button>
            <span className="grow" />
            <button type="button" className="link-btn small" style={{ color: 'var(--label-2)' }} onClick={() => setBasis((v) => !v)} aria-expanded={basis}>
              依据 {run.contextMemoryIds.length} 条记忆
            </button>
          </div>
          {basis && (
            <div className="basis">
              副手收到的全部内容：
              <pre>{run.brief}</pre>
            </div>
          )}
        </>
      )}
    </div>
  )
}

/** The card already shows the title; drop a leading heading that repeats it. */
function withoutTitle(doc: Doc): string {
  const [first, ...rest] = doc.body.split('\n')
  return first.replace(/^#+\s*/, '').trim() === doc.title.trim() ? rest.join('\n').trimStart() : doc.body
}

function DocCard({ doc }: { doc: Doc }) {
  const { dispatch } = useStore()
  const [editing, setEditing] = useState(false)
  const [body, setBody] = useState(doc.body)
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="card">
      <div className="card-head">
        <span className="grow ellipsis" style={{ color: 'var(--label)', fontWeight: 600, fontSize: 14 }}>
          {doc.title}
        </span>
        <span className="faint">
          {doc.by === 'ai' ? '副手写的 · ' : ''}
          {formatAgo(doc.updatedAt)}
        </span>
      </div>
      <div className="card-body">
        {editing ? (
          <textarea value={body} onChange={(e) => setBody(e.target.value)} aria-label="文档内容" autoFocus />
        ) : (
          <div className={expanded ? '' : 'clamp'} onClick={() => setExpanded(true)} style={{ cursor: expanded ? undefined : 'pointer' }}>
            <Markdown text={withoutTitle(doc) || '（空）'} />
          </div>
        )}
      </div>
      <div className="card-foot">
        {editing ? (
          <>
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => {
                dispatch({ type: 'updateDoc', id: doc.id, patch: { body, title: body.split('\n')[0].replace(/^#+\s*/, '').slice(0, 40) || doc.title } })
                setEditing(false)
              }}
            >
              存好
            </button>
            <button type="button" className="btn btn-quiet btn-sm" onClick={() => setEditing(false)}>
              取消
            </button>
          </>
        ) : (
          <button
            type="button"
            className="btn btn-quiet btn-sm"
            onClick={() => {
              setBody(doc.body)
              setEditing(true)
            }}
          >
            编辑
          </button>
        )}
        <span className="grow" />
        <button
          type="button"
          className="btn btn-quiet btn-sm"
          style={{ color: 'var(--label-2)' }}
          onClick={() => {
            if (window.confirm(`删掉文档「${doc.title}」？`)) dispatch({ type: 'deleteDoc', id: doc.id })
          }}
        >
          删除
        </button>
      </div>
    </div>
  )
}

function Record({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  const runs = state.runs.filter((r) => r.thingId === thing.id)
  const docs = state.docs.filter((d) => d.thingId === thing.id)
  const entries: ({ at: string; run: Run; doc?: never } | { at: string; doc: Doc; run?: never })[] = [
    ...runs.map((run) => ({ at: run.createdAt, run })),
    ...docs.map((doc) => ({ at: doc.createdAt, doc })),
  ].sort((a, b) => a.at.localeCompare(b.at))

  return (
    <section className="section">
      <div className="section-label spread">
        <span>工作记录</span>
        <button
          type="button"
          className="link-btn"
          onClick={() => {
            const at = new Date().toISOString()
            dispatch({ type: 'createDoc', doc: { id: newId('d'), thingId: thing.id, title: '新文档', body: '', by: 'user', createdAt: at, updatedAt: at } })
          }}
        >
          写文档
        </button>
      </div>
      {entries.length === 0 ? (
        <p className="empty-line" style={{ padding: '4px 4px' }}>
          还没有记录。让副手做点什么，结果会出现在这里。
        </p>
      ) : (
        <div className="record">{entries.map((e) => (e.run ? <RunCard key={e.run.id} thing={thing} run={e.run} /> : <DocCard key={e.doc.id} doc={e.doc} />))}</div>
      )}
    </section>
  )
}

/** 来龙去脉: where the thing came from and what happened to it, folded away. */
function History({ thing }: { thing: Thing }) {
  const { state } = useStore()
  const [open, setOpen] = useState(false)
  const events = timelineFor(state, thing)
  if (events.length === 0) return null
  return (
    <section className="section">
      <button type="button" className="disclosure" style={{ padding: '0 4px' }} onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        来龙去脉 · {events.length} 条
      </button>
      {open && (
        <div style={{ padding: '12px 4px 0' }}>
          <Timeline events={events} />
        </div>
      )}
    </section>
  )
}

function Composer({ thing }: { thing: Thing }) {
  const { state, dispatch, runAgent } = useStore()
  const { draft, setDraft, agentFor, setAgentFor } = useShell()
  const toast = useToast()
  const [showContext, setShowContext] = useState(false)
  const agentId = agentFor(thing.id)
  const agent = state.agents.find((a) => a.id === agentId) ?? state.agents[0]
  const context = contextFor(state, thing, agent)
  const included = context.filter((c) => c.included)
  const text = draft(thing.id)
  const manual = agent.channel === 'manual'
  const left = state.settings.dailyBudget - spentToday(state)
  const costOf = (prompt: string) => (manual ? 0 : estimateCost(buildBrief(state, thing, prompt, included.map((c) => c.memory)).length))

  const send = (kind: RunKind, prompt: string) => {
    if (runAgent({ thingId: thing.id, agentId: agent.id, kind, prompt })) return true
    toast.show('今天的额度用完了，可以在设置里调', { to: '/settings', label: '去设置' })
    return false
  }

  const submit = () => {
    const prompt = text.trim()
    if (!prompt) return
    if (send('ask', prompt)) setDraft(thing.id, '')
  }

  return (
    <div className="composer-wrap">
      <div className="suggest">
        {quickActions[thing.kind].map((a) => {
          const cost = costOf(a.prompt)
          return (
            <button key={a.label} type="button" className="ai-btn" disabled={cost > left} title={cost > left ? '超过今天的额度' : a.prompt} onClick={() => send(a.kind, a.prompt)}>
              <Sparkles size={12} />
              {a.label}
              {cost > 0 && <span className="cost">¥{cost.toFixed(2)}</span>}
            </button>
          )
        })}
        <span className="grow" />
        <button type="button" className="link-btn small" style={{ color: 'var(--label-2)' }} onClick={() => setShowContext((v) => !v)} aria-expanded={showContext}>
          带上 {included.length} 条记忆
        </button>
      </div>
      {showContext && (
        <div className="group" style={{ marginBottom: 10, maxHeight: 240, overflowY: 'auto' }}>
          {context.length === 0 && <div className="empty-line">没有相关的记忆</div>}
          {context.map(({ memory, allowed, included: on, kindBlocked, unconfirmed }) => (
            <label key={memory.id} className="check-row" style={{ cursor: allowed ? 'pointer' : 'default' }}>
              <input
                type="checkbox"
                checked={on}
                disabled={!allowed}
                onChange={() => dispatch({ type: 'toggleContextMemory', thingId: thing.id, memoryId: memory.id })}
                aria-label={memory.text}
              />
              <span className={`text${allowed ? '' : ' done'}`} style={{ fontSize: 13.5 }}>
                {memory.text}
              </span>
              {kindBlocked && <span className="tiny faint">{agent.name}看不到这类</span>}
              {unconfirmed && (
                <button
                  type="button"
                  className="link-btn tiny"
                  title="这是从资料里推测的；确认后就会带上"
                  onClick={(e) => {
                    e.preventDefault()
                    dispatch({ type: 'confirmMemory', id: memory.id })
                  }}
                >
                  确认后带上
                </button>
              )}
            </label>
          ))}
        </div>
      )}
      <form
        className="composer"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <textarea
          rows={1}
          value={text}
          onChange={(e) => setDraft(thing.id, e.target.value)}
          placeholder={`让${agent.name}做点什么`}
          aria-label="交给副手"
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              submit()
            }
          }}
        />
        <select value={agent.id} onChange={(e) => setAgentFor(thing.id, e.target.value)} aria-label="交给谁">
          {state.agents
            .filter((a) => a.enabled)
            .map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
        </select>
        <button type="submit" className="send" disabled={!text.trim()} aria-label="发送">
          <ArrowUp size={16} strokeWidth={2.5} />
        </button>
      </form>
    </div>
  )
}

export function ThingPage() {
  const { id = '' } = useParams()
  const { state } = useStore()
  const thing = findThing(state, id)
  if (!thing) return <NotFound />

  return (
    <div className="doc-page" key={thing.id}>
      <Header thing={thing} />
      {thing.kind === 'idea' && <IdeaBanner thing={thing} />}
      {thing.kind === 'task' && <Checklist task={thing.item} />}
      {thing.kind === 'project' && <ProjectItems projectId={thing.id} />}
      <Record thing={thing} />
      <History thing={thing} />
      <Composer thing={thing} />
    </div>
  )
}
