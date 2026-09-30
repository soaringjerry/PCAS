import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { ArrowUp, Check, ChevronDown, ChevronLeft, ChevronRight, Copy, FileText, Folder, Lightbulb, ListPlus, Plus, Sparkles, X } from 'lucide-react'
import { Checkbox, DateTimePicker, Select } from '../components/controls'
import { ConfirmModal } from '../components/Overlay'
import { Markdown } from '../components/Markdown'
import { LineRow } from '../components/LineRow'
import { Timeline } from '../components/Marks'
import { buildBrief, contextFor, parseChecklist, quickActions } from '../domain/agent'
import { newId } from '../domain/ids'
import { projectStatusLabel, taskStatusLabel, taskStatusOrder, type Tone } from '../domain/labels'
import { estimateCost, ongoingLine, spentToday, urgentLine } from '../domain/lines'
import { findThing, thingProjectId, thingTitle, timelineFor, type Thing } from '../domain/things'
import { formatAgo } from '../domain/time'
import type { Doc, Run, RunKind, Task } from '../domain/types'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { useToast } from '../store/toast'
import { api } from '../store/api'
import { NotFound } from './NotFound'

function Header({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  const project = state.projects.find((p) => p.id === thingProjectId(thing))
  const notes = thing.kind === 'project' ? thing.item.goal : thing.kind === 'idea' ? thing.item.body : (thing.item.notes ?? '')

  return (
    <>
      <Link to={project ? `/t/${project.id}` : '/'} className="back">
        <ChevronLeft size={16} />
        {project ? project.name : '大厅'}
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
        onBlur={async (e) => {
          const title = e.target.value.trim()
          if (title && title !== thingTitle(thing)) if (!(await dispatch({ type: 'renameThing', id: thing.id, title }))) return
        }}
      />
      <Meta thing={thing} />
      {thing.item.hasRetainedWriting && <RetainedWriting id={thing.id} />}
      <textarea
        key={`notes-${thing.id}`}
        className="notes"
        defaultValue={notes}
        placeholder={thing.kind === 'project' ? '这个项目想做成什么' : '补充点什么'}
        aria-label="说明"
        onBlur={async (e) => {
          if (e.target.value === notes) return
          if (thing.kind === 'project') if (!(await dispatch({ type: 'updateProject', id: thing.id, patch: { goal: e.target.value } }))) return
          else if (!(await dispatch({ type: 'setNotes', id: thing.id, text: e.target.value }))) return
        }}
      />
    </>
  )
}

function RetainedWriting({ id }: { id: string }) {
  const [writing, setWriting] = useState<{ field: string; text: string; reason: string }[] | null>(null)
  const [error, setError] = useState('')
  return <div>
    <p>旧版文字的来源无法分开，已保留供你检查。副手不会使用这些文字。</p>
    <button className="btn btn-quiet" type="button" onClick={async () => {
      try { setWriting(await api(`/v1/workspace/items/${id}/retained-writing`)); setError('') }
      catch (e) { setError(e instanceof Error ? e.message : '文字暂时无法读取') }
    }}>查看保留的文字</button>
    {error && <p role="alert">{error}</p>}
    {writing?.map((part) => <div key={part.field}><p>{part.reason}</p><pre style={{ whiteSpace: 'pre-wrap' }}>{part.text}</pre></div>)}
  </div>
}

function ProjectPicker({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  if (thing.kind === 'project') return null
  return (
    <Select
      variant="chip"
      label="项目"
      icon={<Folder size={13} />}
      value={thing.item.projectId ?? ''}
      onChange={(v) => dispatch({ type: 'moveThing', id: thing.id, projectId: v || undefined })}
      options={[{ value: '', label: '不属于项目' }, ...state.projects.map((p) => ({ value: p.id, label: p.name }))]}
    />
  )
}

function DueControl({ task }: { task: Task }) {
  const { dispatch } = useStore()
  return (
    <DateTimePicker
      label="截止"
      placeholder="没有截止"
      clearLabel="去掉截止"
      value={task.due}
      onChange={(due) => dispatch({ type: 'updateTask', id: task.id, patch: { due }, summary: due ? '改了截止时间' : '去掉截止时间' })}
    />
  )
}

const dot = (tone: Tone) => <span className={`dot dot-${tone}`} />

function Meta({ thing }: { thing: Thing }) {
  const { dispatch } = useStore()

  if (thing.kind === 'task') {
    const t = thing.item
    return (
      <div className="meta-line">
        <Select
          variant="chip"
          label="状态"
          value={t.status}
          onChange={(status) => dispatch({ type: 'setTaskStatus', id: t.id, status })}
          options={taskStatusOrder.map((s) => ({ value: s, label: taskStatusLabel[s].text, icon: dot(taskStatusLabel[s].tone) }))}
        />
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
    const label = { active: '想法', awakened: '刚被唤醒', shelved: '放着', promoted: '已转成待办', dropped: '不做了' }[i.status]
    return (
      <div className="meta-line">
        <span className="meta-text">
          <Lightbulb size={13} />
          {label}
        </span>
        <ProjectPicker thing={thing} />
        {i.status === 'active' && (
          <button type="button" className="select select-chip" onClick={() => dispatch({ type: 'ideaPromote', id: i.id })}>
            <ListPlus size={13} />
            转成待办
          </button>
        )}
      </div>
    )
  }

  const p = thing.item
  return (
    <div className="meta-line">
      <Select
        variant="chip"
        label="项目状态"
        value={p.status}
        onChange={(status) => dispatch({ type: 'updateProject', id: p.id, patch: { status } })}
        options={(['active', 'paused', 'done'] as const).map((s) => ({ value: s, label: projectStatusLabel[s].text, icon: dot(projectStatusLabel[s].tone) }))}
      />
      {p.progress && <span className="meta-text ellipsis">{p.progress.split('\n').at(-1)}</span>}
    </div>
  )
}

function IdeaBanner({ thing }: { thing: Extract<Thing, { kind: 'idea' }> }) {
  const { state, dispatch } = useStore()
  const i = thing.item
  const [cond, setCond] = useState('')
  const [conditionDue, setConditionDue] = useState<string | undefined>()

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
          onSubmit={async (e) => {
            e.preventDefault()
            if (!cond.trim()) return
            if (!(await dispatch({ type: 'addCondition', ideaId: i.id, description: cond.trim(), due: conditionDue }))) return
            setCond('')
            setConditionDue(undefined)
          }}
        >
          <span className="plus">
            <Plus size={16} />
          </span>
          <input className="add" value={cond} onChange={(e) => setCond(e.target.value)} placeholder="再加一个条件" aria-label="再加一个条件" />
          <DateTimePicker label="条件到期时间（可选）" placeholder="到期时间" defaultHour={9} value={conditionDue} onChange={setConditionDue} />
          <button type="submit" className="btn btn-sm" disabled={!cond.trim()}>
            添加
          </button>
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
          onSubmit={async (e) => {
            e.preventDefault()
            if (!text.trim()) return
            if (!(await dispatch({ type: 'addCheck', taskId: task.id, text: text.trim() }))) return
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
          onSubmit={async (e) => {
            e.preventDefault()
            if (!text.trim()) return
            if (!(await dispatch({ type: 'addTask', title: text.trim(), projectId }))) return
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
          {run.staleContext && <p className="small muted">记忆或授权已经变化，请重新生成交接内容。</p>}
          <p className="small muted">这个副手要你手动转交：复制下面的内容发给它，再把回答贴回来。</p>
          <div className="row">
            <button
              type="button"
              className="btn btn-sm"
              disabled={run.staleContext}
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
          <textarea className="textarea" value={pasted} onChange={(e) => setPasted(e.target.value)} placeholder="把它的回答贴在这里" aria-label="贴回回答" style={{ minHeight: 120 }} />
          <div className="row">
            <button type="button" className="btn btn-primary btn-sm" disabled={!pasted.trim() || run.staleContext} onClick={() => dispatch({ type: 'pasteRunResult', id: run.id, output: pasted.trim() })}>
              放回来
            </button>
            <button type="button" className="btn btn-quiet btn-sm" onClick={() => dispatch({ type: 'discardRun', id: run.id })}>
              算了
            </button>
          </div>
        </div>
      )}

      {run.status === 'failed' && (
        <div className="card-body small muted">没做成。{run.error ?? run.output}</div>
      )}

      {run.status === 'done' && run.output && (
        <>
          <div className="card-body">
            {editing ? (
              <textarea className="textarea" value={text} onChange={(e) => setText(e.target.value)} aria-label="修改结果" autoFocus />
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
              disabled={run.staleContext}
              onClick={async () => {
                if (!(await dispatch({ type: 'adoptRun', id: run.id, as: choice.as, text: editing ? text : run.output! }))) return
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
  const [deleting, setDeleting] = useState(false)

  return (
    <div className="card">
      {deleting && (
        <ConfirmModal
          title={`删掉文档「${doc.title}」？`}
          onClose={() => setDeleting(false)}
          onConfirm={async () => {
            if (await dispatch({ type: 'deleteDoc', id: doc.id })) setDeleting(false)
          }}
        >
          <p className="muted">文档会从这件事的工作记录里移除。</p>
        </ConfirmModal>
      )}
      <div className="card-head">
        <FileText size={14} className="faint" />
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
          <textarea className="textarea" value={body} onChange={(e) => setBody(e.target.value)} aria-label="文档内容" autoFocus />
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
              onClick={async () => {
                if (!(await dispatch({ type: 'updateDoc', id: doc.id, patch: { body, title: body.split('\n')[0].replace(/^#+\s*/, '').slice(0, 40) || doc.title } }))) return
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
          onClick={() => setDeleting(true)}
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
          onClick={async () => {
            const at = new Date().toISOString()
            if (!(await dispatch({ type: 'createDoc', doc: { id: newId(), thingId: thing.id, title: '新文档', body: '', by: 'user', createdAt: at, updatedAt: at } }))) return
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
  const [showContext, setShowContext] = useState(false)
  const agentId = agentFor(thing.id)
  const agent = state.agents.find((a) => a.id === agentId) ?? state.agents[0]
  const context = contextFor(state, thing, agent)
  const included = context.filter((c) => c.included)
  const text = draft(thing.id)
  const manual = agent.channel === 'manual'
  const left = state.settings.dailyBudget - spentToday(state)
  const costOf = (prompt: string) => (manual ? 0 : estimateCost(buildBrief(state, thing, prompt, included.map((c) => c.memory)), agent))

  const send = async (kind: RunKind, prompt: string) => {
    if (await runAgent({ thingId: thing.id, agentId: agent.id, kind, prompt })) return true
    return false
  }

  const submit = async () => {
    const prompt = text.trim()
    if (!prompt) return
    if (await send('ask', prompt)) setDraft(thing.id, '')
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
        {agent.protocol === 'codex' && <span className="tiny muted">使用订阅额度</span>}
        <span className="grow" />
        <button type="button" className="link-btn small" style={{ color: 'var(--label-2)' }} onClick={() => setShowContext((v) => !v)} aria-expanded={showContext}>
          带上 {included.length} 条记忆
        </button>
      </div>
      {showContext && (
        <div className="group" style={{ marginBottom: 10, maxHeight: 240, overflowY: 'auto' }}>
          {context.length === 0 && <div className="empty-line">没有相关的记忆</div>}
          {context.map(({ memory, allowed, included: on, kindBlocked, unconfirmed }) => (
            <div key={memory.id} className="check-row">
              <Checkbox
                className="grow"
                checked={on}
                disabled={!allowed}
                onChange={() => dispatch({ type: 'toggleContextMemory', thingId: thing.id, memoryId: memory.id })}
              >
                <span className={allowed ? undefined : 'faint'}>{memory.text}</span>
              </Checkbox>
              {kindBlocked && <span className="tiny faint">{agent.name}看不到这类</span>}
              {unconfirmed && (
                <button
                  type="button"
                  className="link-btn tiny"
                  title="这是从资料里推测的；确认后就会带上"
                  onClick={async (e) => {
                    e.preventDefault()
                    if (!(await dispatch({ type: 'confirmMemory', id: memory.id }))) return
                  }}
                >
                  确认后带上
                </button>
              )}
            </div>
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
        <Select
          variant="ghost"
          label="交给谁"
          value={agent.id}
          onChange={(v) => setAgentFor(thing.id, v)}
          options={state.agents.filter((a) => a.enabled).map((a) => ({ value: a.id, label: a.name, hint: a.protocol === 'codex' ? '订阅' : a.channel === 'manual' ? '复制粘贴' : undefined }))}
        />
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
