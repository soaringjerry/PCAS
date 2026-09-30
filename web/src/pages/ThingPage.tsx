import { useRef, useState } from 'react'
import { Link, useParams } from 'react-router'
import { Check, ChevronLeft, Copy, Ellipsis, FileText, Lightbulb, Plus, Sparkles, X } from 'lucide-react'
import { Popover } from '../components/controls'
import { Markdown } from '../components/Markdown'
import { Secretary } from '../components/Secretary'
import { parseChecklist } from '../domain/agent'
import { newId } from '../domain/ids'
import { projectStatusLabel, taskStatusLabel } from '../domain/labels'
import { ongoingLine, urgentLine, type LineItem } from '../domain/lines'
import { findThing, isOpenTask, thingProjectId, thingTitle, type Thing } from '../domain/things'
import { dayOffset, formatAgo } from '../domain/time'
import type { Doc, Run, Task } from '../domain/types'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { useToast } from '../store/toast'
import { api } from '../store/api'
import { NotFound } from './NotFound'
import '../styles/thing.css'

// A thing page keeps three kinds of buttons (docs/design/principles.md): the
// done circle, undo / change, and a confirmation before anything leaves or
// costs money. Everything else is said to the secretary at the bottom.

/* ---------- Header ---------- */

const weekdays = ['日', '一', '二', '三', '四', '五', '六']
const pad = (n: number) => String(n).padStart(2, '0')
const clock = (d: Date) => `${pad(d.getHours())}:${pad(d.getMinutes())}`

/** "今天 15:00", "明天 09:00", "周五 15:00" within the week, otherwise "10月12日 15:00". */
function shortWhen(iso: string): string {
  const d = new Date(iso)
  const days = dayOffset(iso)
  if (days === 0) return `今天 ${clock(d)}`
  if (days === 1) return `明天 ${clock(d)}`
  if (days === -1) return `昨天 ${clock(d)}`
  if (days > 1 && days < 7) return `周${weekdays[d.getDay()]} ${clock(d)}`
  return `${d.getMonth() + 1}月${d.getDate()}日 ${clock(d)}`
}

/** When the reminder set by the secretary goes off (contracts §3), if it will. */
function reminderAt(task: Task): string | undefined {
  const t = task.triggers.find((t) => t.id === 'due-reminder')
  return t?.active && t.nextAt ? t.nextAt : undefined
}

const ideaStatusText = { active: '想法', awakened: '刚被唤醒', shelved: '放着', promoted: '已转成待办', dropped: '不做了' } as const

/** The read-only facts about a thing, in one line; the parts it lacks are left out. */
function infoParts(state: ReturnType<typeof useStore>['state'], thing: Thing): { text: string; tone?: 'late' | 'owed' }[] {
  const project = state.projects.find((p) => p.id === thingProjectId(thing))?.name
  if (thing.kind === 'task') {
    const t = thing.item
    const remind = state.settings.followUps ? reminderAt(t) : undefined
    const late = t.due && isOpenTask(t) && new Date(t.due).getTime() < Date.now()
    return [
      { text: t.status === 'waiting' && t.waitingFor ? `在等${t.waitingFor}` : taskStatusLabel[t.status].text },
      ...(t.due ? [{ text: `${shortWhen(t.due)} 截止`, tone: late ? ('late' as const) : undefined }] : []),
      ...(project ? [{ text: project }] : []),
      ...(remind ? [{ text: `${t.due && dayOffset(remind) === dayOffset(t.due) ? clock(new Date(remind)) : shortWhen(remind)} 提醒` }] : []),
      ...(t.owedTo ? [{ text: `${t.owedTo.who}在等你`, tone: 'owed' as const }] : []),
    ]
  }
  if (thing.kind === 'idea') return [{ text: ideaStatusText[thing.item.status] }, ...(project ? [{ text: project }] : [])]
  const p = thing.item
  const open = state.tasks.filter((t) => t.projectId === p.id && isOpenTask(t)).length
  const last = p.progress.split('\n').filter(Boolean).at(-1)
  return [{ text: projectStatusLabel[p.status].text }, ...(last ? [{ text: last }] : []), ...(open ? [{ text: `${open} 件没做完` }] : [])]
}

/** Changing status, due time or project is said to the secretary: clicking the line starts that sentence. */
function InfoLine({ thing }: { thing: Thing }) {
  const { state } = useStore()
  const { prefill } = useShell()
  const parts = infoParts(state, thing)
  return (
    <button type="button" className="info-line" title="要改，跟秘书说一句" onClick={() => prefill(thing.id, '改一下这件事：')}>
      {parts.map((p, i) => (
        <span key={i} className={p.tone}>
          {p.text}
        </span>
      ))}
    </button>
  )
}

function DoneCircle({ task }: { task: Task }) {
  const { dispatchUndoable } = useStore()
  const done = task.status === 'done'
  return (
    <button
      type="button"
      className={`circle title-check${done ? ' on' : ''}`}
      aria-label={done ? '改回没做完' : '做完了'}
      aria-pressed={done}
      onClick={() => dispatchUndoable({ type: 'setTaskStatus', id: task.id, status: done ? 'todo' : 'done' }, done ? '改回没做完了' : '做完了')}
    >
      {done && <Check size={14} strokeWidth={3} />}
    </button>
  )
}

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
      <div className="title-row">
        {thing.kind === 'task' && <DoneCircle task={thing.item} />}
        <textarea
          key={`title-${thing.id}`}
          className={`doc-title${thing.kind === 'task' && thing.item.status === 'done' ? ' done' : ''}`}
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
            if (title && title !== thingTitle(thing)) {
              if (!(await dispatch({ type: 'renameThing', id: thing.id, title }))) return
            }
          }}
        />
      </div>
      <InfoLine thing={thing} />
      <textarea
        key={`notes-${thing.id}`}
        className="notes"
        defaultValue={notes}
        placeholder={thing.kind === 'project' ? '这个项目想做成什么' : '补充点什么'}
        aria-label="说明"
        onBlur={async (e) => {
          if (e.target.value === notes) return
          if (thing.kind === 'project') {
            if (!(await dispatch({ type: 'updateProject', id: thing.id, patch: { goal: e.target.value } }))) return
          } else {
            if (!(await dispatch({ type: 'setNotes', id: thing.id, text: e.target.value }))) return
          }
        }}
      />
    </>
  )
}

/* ---------- What the thing holds ---------- */

function IdeaBanner({ thing }: { thing: Extract<Thing, { kind: 'idea' }> }) {
  const { state, dispatchUndoable } = useStore()
  const i = thing.item

  if (i.status === 'awakened') {
    return (
      <div className="banner">
        <Sparkles size={16} />
        <div className="grow">
          <p>{i.wake?.reason ?? '它等的条件满足了。'}</p>
          <div className="quick-replies">
            <button type="button" onClick={() => dispatchUndoable({ type: 'ideaPromote', id: i.id }, '转成待办了')}>
              转成待办
            </button>
            <button type="button" onClick={() => dispatchUndoable({ type: 'ideaSnooze', id: i.id, days: 7 }, '好，再放一周')}>
              再放放
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

  if (i.status !== 'shelved' || i.conditions.length === 0) return null
  // Conditions are added by saying them to the secretary; here they can only be seen and removed.
  return (
    <section className="section">
      <div className="section-label">满足这些条件时，它会自己回来</div>
      <div className="group">
        {i.conditions.map((c) => (
          <div key={c.id} className="check-row">
            <span className={`cond-mark${c.met ? ' met' : ''}`} aria-hidden>
              {c.met && <Check size={11} strokeWidth={3} />}
            </span>
            <span className={`text${c.met ? ' done' : ''}`}>{c.description}</span>
            <button
              type="button"
              className="btn btn-quiet btn-icon btn-sm del"
              aria-label={`删掉条件：${c.description}`}
              onClick={() => dispatchUndoable({ type: 'removeCondition', ideaId: i.id, conditionId: c.id }, '删掉了这个条件')}
            >
              <X size={14} />
            </button>
          </div>
        ))}
      </div>
    </section>
  )
}

function Checklist({ task }: { task: Task }) {
  const { dispatch, dispatchUndoable } = useStore()
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
            <button
              type="button"
              className="btn btn-quiet btn-icon btn-sm del"
              aria-label={`删掉：${c.text}`}
              onClick={() => dispatchUndoable({ type: 'removeCheck', taskId: task.id, itemId: c.id }, '删掉了这一步')}
            >
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

/** One thing inside a project: the done circle, and the rest opens it. */
function ItemRow({ item }: { item: LineItem }) {
  const { dispatchUndoable } = useStore()
  const { thing } = item
  return (
    <div className="lrow item-row">
      {thing.kind === 'task' ? (
        <button
          type="button"
          className={`circle${thing.item.status === 'doing' ? ' doing' : ''}${thing.item.status === 'waiting' ? ' waiting' : ''}`}
          aria-label={`做完了：${thing.item.title}`}
          onClick={() => dispatchUndoable({ type: 'setTaskStatus', id: thing.id, status: 'done' }, '做完了')}
        />
      ) : (
        <span className="glyph-idea" aria-hidden>
          <Lightbulb size={17} />
        </span>
      )}
      <Link to={`/t/${thing.id}`} className="body">
        <span className="title">{thingTitle(thing)}</span>
        <span className={`reason ${item.tone}`}>{item.reason}</span>
      </Link>
    </div>
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
          <ItemRow key={i.thing.id} item={i} />
        ))}
        {shelved.length > 0 && (
          <>
            <button type="button" className="disclosure" onClick={() => setShowParked((v) => !v)} aria-expanded={showParked}>
              {showParked ? '收起放着的想法' : `还有 ${shelved.length} 个想法放着`}
            </button>
            {showParked && shelved.map((i) => <ItemRow key={i.thing.id} item={i} />)}
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

/* ---------- Documents ---------- */

/** The row already shows the title; drop a leading heading that repeats it. */
function withoutTitle(doc: Doc): string {
  const [first, ...rest] = doc.body.split('\n')
  return first.replace(/^#+\s*/, '').trim() === doc.title.trim() ? rest.join('\n').trimStart() : doc.body
}

/** A heading on the first line names the document; a fresh one takes its first line. */
function titleFor(doc: Doc, body: string): string {
  const first = body.split('\n')[0].trim()
  if (/^#+\s/.test(first)) return first.replace(/^#+\s*/, '').slice(0, 40) || doc.title
  if (doc.title === '新文档' && first) return first.slice(0, 40)
  return doc.title
}

function DocRow({ doc, fresh }: { doc: Doc; fresh: boolean }) {
  const { dispatch, dispatchUndoable } = useStore()
  const [open, setOpen] = useState(fresh)
  const [editing, setEditing] = useState(fresh)
  const [menu, setMenu] = useState(false)
  const more = useRef<HTMLButtonElement>(null)

  const save = async (body: string) => {
    setEditing(false)
    if (body === doc.body) return
    await dispatch({ type: 'updateDoc', id: doc.id, patch: { body, title: titleFor(doc, body) } })
  }

  return (
    <div className={`doc-row${open ? ' open' : ''}`}>
      <div className="doc-head">
        <button type="button" className="doc-open" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          <FileText size={15} />
          <span className="doc-name">{doc.title}</span>
          <span className="doc-meta">
            {doc.by === 'ai' ? '副手写的' : '你写的'} · {formatAgo(doc.updatedAt)}
          </span>
        </button>
        <button ref={more} type="button" className="doc-more" aria-label={`文档「${doc.title}」的更多操作`} aria-expanded={menu} onClick={() => setMenu((v) => !v)}>
          <Ellipsis size={16} />
        </button>
        {menu && (
          <Popover anchor={more} onClose={() => setMenu(false)}>
            <div className="menu" role="menu">
              <button
                type="button"
                role="menuitem"
                className="menu-item danger"
                onClick={() => {
                  setMenu(false)
                  void dispatchUndoable({ type: 'deleteDoc', id: doc.id }, `删掉了「${doc.title}」`)
                }}
              >
                删除
              </button>
            </div>
          </Popover>
        )}
      </div>
      {open &&
        (editing ? (
          <textarea
            className="doc-editor"
            defaultValue={doc.body}
            aria-label={`${doc.title} 的内容`}
            placeholder="写点什么…"
            autoFocus
            onBlur={(e) => void save(e.target.value)}
          />
        ) : (
          <div className="doc-body" role="textbox" tabIndex={0} aria-label={`${doc.title} 的内容`} onClick={() => setEditing(true)} onFocus={() => setEditing(true)}>
            <Markdown text={withoutTitle(doc) || '（空）'} />
          </div>
        ))}
    </div>
  )
}

/** The documents a thing produced, each one line until opened. */
function Docs({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  const [fresh, setFresh] = useState<string>()
  const docs = state.docs.filter((d) => d.thingId === thing.id).sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
  const write = (
    <button
      type="button"
      className="doc-new"
      onClick={async () => {
        const at = new Date().toISOString()
        const id = newId()
        if (await dispatch({ type: 'createDoc', doc: { id, thingId: thing.id, title: '新文档', body: '', by: 'user', createdAt: at, updatedAt: at } })) setFresh(id)
      }}
    >
      写点什么…
    </button>
  )
  // With no documents yet, the block is just the faint line that starts one.
  if (docs.length === 0) return <div className="docs-empty">{write}</div>

  return (
    <section className="section">
      <div className="section-label">文档</div>
      <div className="group docs">
        {docs.map((d) => (
          <DocRow key={d.id} doc={d} fresh={d.id === fresh} />
        ))}
        {write}
      </div>
    </section>
  )
}

/* ---------- 动态: everything that happened, newest first ---------- */

/** What adopting a result does, decided from the result itself. */
function adoptAs(thing: Thing, run: Run): { as: 'doc' | 'subtasks' | 'progress'; done: string } {
  const n = parseChecklist(run.output ?? '').length
  if (n > 0) return { as: 'subtasks', done: thing.kind === 'task' ? `已加入 ${n} 个子任务` : `建成了 ${n} 件待办` }
  if (run.kind === 'summary') return { as: 'progress', done: '写进了进度' }
  return { as: 'doc', done: '存成了文档' }
}

function adoptedLine(thing: Thing, run: Run): string {
  if (run.adopted?.as === 'subtasks') {
    const n = parseChecklist(run.output ?? '').length
    return n ? (thing.kind === 'task' ? `已加入 ${n} 个子任务` : `建成了 ${n} 件待办`) : '加成了待办'
  }
  return run.adopted?.as === 'progress' ? '写进了进度' : '存成了文档'
}

function failureText(run: Run): string {
  const error = (run.error ?? run.output ?? '').toLowerCase()
  if (error.includes('budget')) return '超过今天的额度'
  if (error.includes('timeout') || error.includes('deadline')) return '等太久没回应'
  if (error.includes('unavailable') || error.includes('not configured')) return '这个副手现在连不上'
  return '出了点问题'
}

/** Runs a click's work once: the button stays disabled until it settles, so a paid request is never sent twice. */
function useBusy(): [boolean, (work: () => Promise<unknown>) => Promise<void>] {
  const [busy, setBusy] = useState(false)
  const lock = useRef(false)
  return [
    busy,
    async (work) => {
      if (lock.current) return
      lock.current = true
      setBusy(true)
      try {
        await work()
      } finally {
        lock.current = false
        setBusy(false)
      }
    },
  ]
}

function Handoff({ run }: { run: Run }) {
  const { dispatch } = useStore()
  const toast = useToast()
  const [pasted, setPasted] = useState('')
  const [brief, setBrief] = useState(false)
  const [busy, guard] = useBusy()
  return (
    <div className="act-detail stack-sm">
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
        <button type="button" className="link-btn" onClick={() => setBrief((v) => !v)}>
          {brief ? '收起' : '看一眼'}
        </button>
      </div>
      {brief && <pre className="act-brief">{run.brief}</pre>}
      <textarea className="textarea" value={pasted} onChange={(e) => setPasted(e.target.value)} placeholder="把它的回答贴在这里" aria-label="贴回回答" style={{ minHeight: 120 }} />
      <div className="row">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          disabled={!pasted.trim() || run.staleContext || busy}
          onClick={() => guard(() => dispatch({ type: 'pasteRunResult', id: run.id, output: pasted.trim() }))}
        >
          放回来
        </button>
      </div>
    </div>
  )
}

function RunRow({ thing, run }: { thing: Thing; run: Run }) {
  const { state, dispatchUndoable, undo, runAgent } = useStore()
  const { agentFor } = useShell()
  const toast = useToast()
  const [busy, guard] = useBusy()
  const [shown, setShown] = useState(false)
  const agent = state.agents.find((a) => a.id === run.agentId)
  // Which model did it is a detail; it stays in the tooltip.
  const who = (
    <span className="act-who" title={agent?.name}>
      副手
    </span>
  )
  const when = <span className="act-when">{formatAgo(run.finishedAt ?? run.createdAt)}</span>
  const look = run.output && (
    <button type="button" className="act-btn" aria-expanded={shown} onClick={() => setShown((v) => !v)}>
      {shown ? '收起' : '看看'}
    </button>
  )
  const output = shown && run.output && (
    <div className="act-detail">
      <Markdown text={run.output} />
    </div>
  )

  if (run.adopted) {
    const actionId = run.adopted.actionId
    return (
      <li className="card act-run">
        <div className="act-line">
          {who}
          <span className="act-text">{adoptedLine(thing, run)}</span>
          {actionId && (
            <button type="button" className="act-btn" disabled={busy} onClick={() => guard(() => undo(actionId))}>
              撤销
            </button>
          )}
          {look}
          {when}
        </div>
        {output}
      </li>
    )
  }

  if (run.status === 'running') {
    return (
      <li className="card act-run">
        <div className="act-line">
          {who}
          <span className="act-text working">正在做：{run.prompt}</span>
          {when}
        </div>
      </li>
    )
  }

  if (run.status === 'waiting') {
    return (
      <li className="card act-run">
        <div className="act-line">
          {who}
          <span className="act-text">等你转交：{run.prompt}</span>
          <button
            type="button"
            className="act-btn icon"
            aria-label="不转交了"
            title="不转交了"
            onClick={() => dispatchUndoable({ type: 'discardRun', id: run.id }, '不转交了')}
          >
            <X size={14} />
          </button>
          {when}
        </div>
        <Handoff run={run} />
      </li>
    )
  }

  if (run.status === 'failed') {
    const retryAgent = state.agents.find((a) => a.id === agentFor(thing.id) && a.enabled && a.id !== run.agentId)
    const retry = (agentId: string) => guard(() => runAgent({ thingId: run.thingId, agentId, kind: run.kind, prompt: run.prompt }))
    return (
      <li className="card act-run">
        <div className="act-line">
          {who}
          <span className="act-text failed" title={run.error ?? run.output}>
            没做成：{failureText(run)}
          </span>
          <button type="button" className="act-btn" disabled={busy} onClick={() => retry(run.agentId)}>
            重试
          </button>
          {retryAgent && (
            <button type="button" className="act-btn" disabled={busy} onClick={() => retry(retryAgent.id)}>
              换 {retryAgent.name} 重试
            </button>
          )}
          {when}
        </div>
      </li>
    )
  }

  if (run.status !== 'done' || !run.output) return null
  // Finished but not in place: it was undone, or what it relied on has changed since.
  const choice = adoptAs(thing, run)
  return (
    <li className="card act-run">
      <div className="act-line">
        {who}
        <span className="act-text">{run.prompt}</span>
        {run.staleContext ? (
          <>
            <span className="act-note">依据变了</span>
            <button
              type="button"
              className="act-btn"
              disabled={busy}
              onClick={() =>
                guard(async () => {
                  if (await runAgent({ thingId: run.thingId, agentId: run.agentId, kind: run.kind, prompt: run.prompt })) toast.show('让副手重做了')
                })
              }
            >
              重做
            </button>
          </>
        ) : (
          <button
            type="button"
            className="act-btn"
            disabled={busy}
            onClick={() => guard(() => dispatchUndoable({ type: 'adoptRun', id: run.id, as: choice.as, text: run.output! }, `放回去了，${choice.done}`))}
          >
            放回去
          </button>
        )}
        {look}
        {when}
      </div>
      {output}
    </li>
  )
}

const actorText: Record<string, string> = { user: '你', secretary: '秘书', assistant: '副手', system: '系统', ai: '副手', import: '导入' }

function RetainedWriting({ id }: { id: string }) {
  const [writing, setWriting] = useState<{ field: string; text: string; reason: string }[] | null>(null)
  const [error, setError] = useState('')
  return (
    <li className="act-row retained">
      <div className="act-line">
        <span className="act-who">旧版</span>
        <span className="act-text">旧版文字的来源无法分开，已保留供你检查。副手不会使用这些文字。</span>
        {!writing && (
          <button
            type="button"
            className="act-btn"
            onClick={async () => {
              try {
                setWriting(await api(`/v1/workspace/items/${id}/retained-writing`))
                setError('')
              } catch (e) {
                setError(e instanceof Error ? e.message : '文字暂时无法读取')
              }
            }}
          >
            查看
          </button>
        )}
      </div>
      {error && <p role="alert">{error}</p>}
      {writing?.map((part) => (
        <div key={part.field} className="act-detail">
          <p className="small muted">{part.reason}</p>
          <pre className="act-brief">{part.text}</pre>
        </div>
      ))}
    </li>
  )
}

const FOLD = 10

/** The one place that says what happened: edits, the secretary's and assistants' work, and how an idea evolved. */
function Activity({ thing }: { thing: Thing }) {
  const { state } = useStore()
  const [all, setAll] = useState(false)
  const history = thing.kind === 'task' ? thing.item.history : thing.kind === 'idea' ? thing.item.evolution : []
  type Entry = { key: string; at: string; run?: Run; by?: string; summary?: string }
  const entries: Entry[] = [
    ...state.runs.filter((r) => r.thingId === thing.id).map((run) => ({ key: run.id, at: run.finishedAt ?? run.createdAt, run })),
    // An assistant's own edits are its adopted result, which the run's line already shows with 【撤销】.
    ...history.filter((h) => (h.by as string) !== 'assistant').map((h, i) => ({ key: `h-${i}`, at: h.at, by: h.by as string, summary: h.summary })),
  ].sort((a, b) => b.at.localeCompare(a.at))
  const hidden = all ? 0 : Math.max(0, entries.length - FOLD)
  const retained = thing.item.hasRetainedWriting

  return (
    <section className="section">
      <div className="section-label">动态</div>
      {entries.length === 0 && !retained ? (
        <p className="act-empty">对秘书说一句，结果会出现在这里</p>
      ) : (
        <ol className="record activity">
          {entries.slice(0, entries.length - hidden).map((e) =>
            e.run ? (
              <RunRow key={e.key} thing={thing} run={e.run} />
            ) : (
              <li key={e.key} className="act-row">
                <div className="act-line">
                  <span className="act-who">{actorText[e.by ?? 'user'] ?? '你'}</span>
                  <span className="act-text">{e.summary}</span>
                  <span className="act-when">{formatAgo(e.at)}</span>
                </div>
              </li>
            ),
          )}
          {hidden > 0 && (
            <li className="act-row">
              <button type="button" className="act-more" onClick={() => setAll(true)}>
                更早的 {hidden} 条
              </button>
            </li>
          )}
          {retained && (all || hidden === 0) && <RetainedWriting id={thing.id} />}
        </ol>
      )}
    </section>
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
      <Docs thing={thing} />
      <Activity thing={thing} />
      <Secretary thingId={thing.id} variant="latest" />
    </div>
  )
}
