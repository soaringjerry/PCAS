import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { Check, ChevronLeft, Copy, Ellipsis, FileText, Lightbulb, Pencil, Plus, Sparkles, Trash2, X } from 'lucide-react'
import { Popover } from '../components/controls'
import { ConfirmModal } from '../components/Overlay'
import { SaveMark, type SaveState } from '../components/ui'
import { Markdown } from '../components/Markdown'
import { Secretary } from '../components/Secretary'
import { parseChecklist } from '../domain/agent'
import { newId } from '../domain/ids'
import { projectStatusLabel, taskStatusLabel } from '../domain/labels'
import { ongoingLine, urgentLine, type LineItem } from '../domain/lines'
import { findThing, isOpenTask, thingProjectId, thingTitle, type Thing } from '../domain/things'
import { clockTime, dayOffset, formatAgo, formatShortWhen } from '../domain/time'
import type { Doc, Run, Task } from '../domain/types'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { useToast } from '../store/toast'
import { api } from '../store/api'
import { NotFound } from './NotFound'
import '../styles/thing.css'

// A thing page keeps three kinds of buttons (docs/design/principles.md): the
// done circle, undo / change, and a confirmation before anything leaves or
// costs money. The title and notes are edited in place and say whether they
// saved; each fact under the title starts the sentence that changes it, said
// to the secretary at the bottom.

/* ---------- Header ---------- */

/** When the reminder set by the secretary goes off (contracts §3), if it will. */
function reminderAt(task: Task): string | undefined {
  const t = task.triggers.find((t) => t.id === 'due-reminder')
  return t?.active && t.nextAt ? t.nextAt : undefined
}

const ideaStatusText = { active: '想法', awakened: '刚被唤醒', shelved: '放着', promoted: '已转成待办', dropped: '不做了' } as const

type Fact = {
  text: string
  tone?: 'late' | 'owed'
  /** The sentence this fact starts in the secretary's input; without one it is only read. */
  say?: string
  /** Something the thing does not have yet, offered as a next step. */
  add?: boolean
  /** Where the fact is decided, when that is not this page. */
  to?: string
}

/** What is known about a thing, and for an open task what could still be set. */
function facts(state: ReturnType<typeof useStore>['state'], thing: Thing): Fact[] {
  const timezone = state.settings.timezone ?? 'UTC'
  const project = state.projects.find((p) => p.id === thingProjectId(thing))?.name
  const inProject: Fact[] = project ? [{ text: project, say: '把这件事挪到项目：' }] : []
  if (thing.kind === 'task') {
    const t = thing.item
    const open = isOpenTask(t)
    const remind = state.settings.followUps ? reminderAt(t) : undefined
    const late = t.due && open && new Date(t.due).getTime() < Date.now()
    return [
      { text: t.status === 'waiting' && t.waitingFor ? `在等${t.waitingFor}` : taskStatusLabel[t.status].text, say: '把状态改成：' },
      ...(t.due ? [{ text: `${formatShortWhen(t.due, timezone)} 截止`, tone: late ? ('late' as const) : undefined, say: '把截止时间改到：' }] : []),
      ...inProject,
      ...(remind ? [{ text: `${t.due && dayOffset(remind, timezone) === dayOffset(t.due, timezone) ? clockTime(remind, timezone) : formatShortWhen(remind, timezone)} 提醒`, say: '把提醒改到：' }] : []),
      ...(t.owedTo ? [{ text: `${t.owedTo.who}在等你`, tone: 'owed' as const }] : []),
      ...(open && t.urgent ? [{ text: '尽快', tone: 'owed' as const, say: '这件事不急了' }] : []),
      ...(open && !t.due ? [{ text: '定个截止时间', add: true, say: '截止时间定在：' }] : []),
      ...(open && !remind && state.settings.followUps ? [{ text: '加个提醒', add: true, say: '到这个时间提醒我：' }] : []),
      ...(open && !state.settings.followUps && reminderAt(t) ? [{ text: '提醒已在设置里停了', to: '/settings' }] : []),
      ...(open && !project && state.projects.length > 0 ? [{ text: '归到项目', add: true, say: '把这件事归到项目：' }] : []),
      ...(open && !t.urgent ? [{ text: '标成尽快', add: true, say: '这件事要尽快' }] : []),
    ]
  }
  if (thing.kind === 'idea') return [{ text: ideaStatusText[thing.item.status] }, ...inProject]
  const p = thing.item
  const open = state.tasks.filter((t) => t.projectId === p.id && isOpenTask(t)).length
  const last = p.progress.split('\n').filter(Boolean).at(-1)
  return [{ text: projectStatusLabel[p.status].text, say: '把这个项目的状态改成：' }, ...(last ? [{ text: last, say: '更新一下进度：' }] : []), ...(open ? [{ text: `${open} 件没做完` }] : [])]
}

/** Status, due time, reminder and project are changed by saying so: each one starts its own sentence. */
function InfoLine({ thing }: { thing: Thing }) {
  const { state } = useStore()
  const { prefill } = useShell()
  return (
    <div className="info-line" role="group" aria-label="这件事的情况，点一项就跟秘书说怎么改">
      {facts(state, thing).map((f, i) =>
        f.to ? (
          <Link key={i} to={f.to} className="fact off">
            {f.text}
          </Link>
        ) : f.say ? (
          <button key={i} type="button" className={`fact${f.add ? ' add' : ''}${f.tone ? ` ${f.tone}` : ''}`} title="点一下，跟秘书说怎么改" onClick={() => prefill(thing.id, f.say!)}>
            {f.text}
          </button>
        ) : (
          <span key={i} className={`fact plain${f.tone ? ` ${f.tone}` : ''}`}>
            {f.text}
          </span>
        ),
      )}
    </div>
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

/** Saves a field when it is left, and keeps what was typed when the save is refused. */
function useFieldSave(saved: string, save: (text: string) => Promise<boolean>) {
  const [state, setState] = useState<SaveState>('idle')
  const timer = useRef<number>(undefined)
  /** What the server has or is being sent: the last text dispatched, else the saved one. */
  const sent = useRef(saved)
  const current = useRef(saved)
  const flying = useRef(0)
  const turn = useRef(0)
  useEffect(() => {
    current.current = saved
    if (!flying.current) sent.current = saved
  }, [saved])
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const commit = async (text: string) => {
    if (text === sent.current) {
      // Typed back to what is saved: there is nothing left to warn about.
      if (!flying.current) setState((s) => (s === 'failed' ? 'idle' : s))
      return
    }
    window.clearTimeout(timer.current)
    const mine = ++turn.current
    sent.current = text
    flying.current++
    setState('saving')
    const ok = await save(text)
    flying.current--
    // An edit made while this one was on its way owns the state now.
    if (mine !== turn.current) return
    if (!ok) sent.current = current.current
    setState(ok ? 'saved' : 'failed')
    if (ok) timer.current = window.setTimeout(() => setState('idle'), 2500)
  }
  return { state, commit, reset: () => setState('idle') }
}

/** The field's box and a way to put text in it. It shows a change made elsewhere (the secretary, an undo, another window) unless it holds the user's own text. */
function useFollowSaved(saved: string, state: SaveState) {
  const box = useRef<HTMLTextAreaElement>(null)
  const put = (text: string) => {
    if (box.current) box.current.value = text
  }
  useEffect(() => {
    const field = box.current
    if (!field || field.value === saved) return
    if (document.activeElement === field || state === 'saving' || state === 'failed') return
    field.value = saved
  }, [saved, state])
  return [box, put] as const
}

/** Under a field whose save was refused: what is shown is the draft, with the way back. */
function Unsaved({ what, onRetry, onRevert }: { what: string; onRetry: () => void; onRevert: () => void }) {
  return (
    <p className="unsaved" role="alert">
      <span>{what}没保存上，上面是你刚写的。</span>
      <button type="button" className="act-btn" onClick={onRetry}>
        再存一次
      </button>
      <button type="button" className="act-btn" onClick={onRevert}>
        改回原来的
      </button>
    </p>
  )
}

function Header({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  const project = state.projects.find((p) => p.id === thingProjectId(thing))
  const saved = thingTitle(thing)
  const notes = thing.kind === 'project' ? thing.item.goal : thing.kind === 'idea' ? thing.item.body : (thing.item.notes ?? '')
  const title = useFieldSave(saved, (text) => dispatch({ type: 'renameThing', id: thing.id, title: text }))
  const note = useFieldSave(notes, (text) =>
    thing.kind === 'project' ? dispatch({ type: 'updateProject', id: thing.id, patch: { goal: text } }) : dispatch({ type: 'setNotes', id: thing.id, text }),
  )
  const [titleBox, putTitle] = useFollowSaved(saved, title.state)
  const [notesBox, putNotes] = useFollowSaved(notes, note.state)
  const navigate = useNavigate()
  const [deleting, setDeleting] = useState(false)
  const busy = title.state === 'saving' || note.state === 'saving'
  const done = title.state === 'saved' || note.state === 'saved'
  /** What each box held when the caret went in, to tell an edit from a visit. */
  const entered = useRef({ title: saved, notes })
  /** A title cannot be empty: leaving it blank puts the saved one back. */
  const commitTitle = () => {
    const text = titleBox.current?.value.trim()
    if (text === undefined) return
    if (!text) putTitle(saved)
    void title.commit(text || saved)
  }
  // Leaving a box nothing was typed in saves nothing: the screen may be behind a change made
  // elsewhere, and sending it would undo that change. A refused draft stays as it is.
  const leaveTitle = () => {
    if (titleBox.current?.value !== entered.current.title) return commitTitle()
    if (title.state !== 'failed') putTitle(saved)
  }
  const leaveNotes = () => {
    const text = notesBox.current?.value
    if (text === undefined) return
    if (text !== entered.current.notes) return void note.commit(text)
    if (note.state !== 'failed') putNotes(notes)
  }

  return (
    <>
      <div className="head-row">
        <Link to={project ? `/t/${project.id}` : '/'} className="back">
          <ChevronLeft size={16} />
          {project ? project.name : '大厅'}
        </Link>
        <SaveMark state={busy ? 'saving' : done ? 'saved' : 'idle'} />
      </div>
      <div className="title-row">
        {thing.kind === 'task' && <DoneCircle task={thing.item} />}
        <textarea
          ref={titleBox}
          key={`title-${thing.id}`}
          className={`doc-title editable${thing.kind === 'task' && thing.item.status === 'done' ? ' done' : ''}${title.state === 'failed' ? ' unsaved-field' : ''}`}
          rows={1}
          defaultValue={saved}
          aria-label="标题"
          title="点一下直接改，离开就保存"
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
              e.preventDefault()
              e.currentTarget.blur()
            }
          }}
          onFocus={(e) => (entered.current.title = e.target.value)}
          onBlur={leaveTitle}
        />
        {/* Only a pointer target that shows the title can be edited; the field itself is what keyboards and readers reach. */}
        <button type="button" className="edit-hint" tabIndex={-1} aria-hidden onClick={() => titleBox.current?.focus()}>
          <Pencil size={14} />
        </button>
        <button type="button" className="edit-hint thing-delete" aria-label={`删除：${saved}`} title="删除" onClick={() => setDeleting(true)}>
          <Trash2 size={14} />
        </button>
      </div>
      {deleting && (
        <ConfirmModal
          title={`删除「${saved}」？`}
          onClose={() => setDeleting(false)}
          onConfirm={async () => {
            if (await dispatch({ type: 'deleteThing', id: thing.id })) navigate('/')
            else setDeleting(false)
          }}
        >
          <p>它的子任务、文档、提醒和副手的工作记录会一起删掉，删了找不回来。不想做了但想留个记录的话，改成「已取消」就行。</p>
        </ConfirmModal>
      )}
      {title.state === 'failed' && (
        <Unsaved
          what="标题"
          onRetry={commitTitle}
          onRevert={() => {
            putTitle(saved)
            title.reset()
          }}
        />
      )}
      <InfoLine thing={thing} />
      <textarea
        ref={notesBox}
        key={`notes-${thing.id}`}
        className={`notes editable${note.state === 'failed' ? ' unsaved-field' : ''}`}
        defaultValue={notes}
        placeholder={thing.kind === 'project' ? '这个项目想做成什么（点这里直接写）' : '加点说明（点这里直接写）'}
        aria-label="说明"
        title="点一下直接改，离开就保存"
        onFocus={(e) => (entered.current.notes = e.target.value)}
        onBlur={leaveNotes}
      />
      {note.state === 'failed' && (
        <Unsaved
          what="说明"
          onRetry={() => void note.commit(notesBox.current?.value ?? notes)}
          onRevert={() => {
            putNotes(notes)
            note.reset()
          }}
        />
      )}
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
  const { state, dispatch, dispatchUndoable } = useStore()
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
            {doc.by === 'ai' ? '副手写的' : '你写的'} · {formatAgo(doc.updatedAt, state.settings.timezone ?? 'UTC')}
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
            placeholder="在这里写正文，离开就保存"
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
      title="给这件事写一份文档，比如草稿、方案、清单"
      onClick={async () => {
        const at = new Date().toISOString()
        const id = newId()
        if (await dispatch({ type: 'createDoc', doc: { id, thingId: thing.id, title: '新文档', body: '', by: 'user', createdAt: at, updatedAt: at } })) setFresh(id)
      }}
    >
      <Plus size={14} aria-hidden />
      新建文档
    </button>
  )
  // With no documents yet, the block is just the line that starts one.
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

/** Where a summary is written: a project's progress is replaced, a task's notes and an idea's body are added to. */
const summaryInto = { project: { label: '更新项目进度', done: '已更新项目进度' }, task: { label: '追加到说明', done: '已追加到说明' }, idea: { label: '追加到想法', done: '已追加到想法' } } as const

/** What putting a result into the thing does, decided from the result itself; the button says it. */
function adoptAs(thing: Thing, run: Run): { as: 'doc' | 'subtasks' | 'progress'; label: string; done: string } {
  const n = parseChecklist(run.output ?? '').length
  if (n > 0) return thing.kind === 'task' ? { as: 'subtasks', label: `加入 ${n} 个子任务`, done: `已加入 ${n} 个子任务` } : { as: 'subtasks', label: `创建 ${n} 件待办`, done: `已创建 ${n} 件待办` }
  if (run.kind === 'summary') return { as: 'progress', ...summaryInto[thing.kind] }
  return { as: 'doc', label: '保存为文档', done: '已保存为文档' }
}

function adoptedLine(thing: Thing, run: Run): string {
  if (run.adopted?.as === 'subtasks') {
    const n = parseChecklist(run.output ?? '').length
    return n ? (thing.kind === 'task' ? `已加入 ${n} 个子任务` : `已创建 ${n} 件待办`) : '已创建待办'
  }
  return run.adopted?.as === 'progress' ? summaryInto[thing.kind].done : '已保存为文档'
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
      <p className="small muted">这个副手要你手动转交：复制下面的内容发给它，再把回答贴回来。保存后，回答会自动放进这件事（子任务、文档或说明）。</p>
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
          保存并加入这件事
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
  const when = <span className="act-when">{formatAgo(run.finishedAt ?? run.createdAt, state.settings.timezone ?? 'UTC')}</span>
  const look = run.output && (
    <button type="button" className="act-btn" aria-expanded={shown} onClick={() => setShown((v) => !v)}>
      {shown ? '收起' : '看结果'}
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
  // Finished but not in the thing: it was undone, or what it relied on has changed since.
  const choice = adoptAs(thing, run)
  return (
    <li className="card act-run">
      <div className="act-line">
        {who}
        <span className="act-text">{run.prompt}</span>
        {run.staleContext ? (
          <>
            <span className="act-note">依据变了，结果可能过时</span>
            <button
              type="button"
              className="act-btn"
              disabled={busy}
              onClick={() =>
                guard(async () => {
                  if (await runAgent({ thingId: run.thingId, agentId: run.agentId, kind: run.kind, prompt: run.prompt })) toast.show('已让副手重新生成')
                })
              }
            >
              重新生成
            </button>
          </>
        ) : (
          <button
            type="button"
            className="act-btn"
            disabled={busy}
            onClick={() => guard(() => dispatchUndoable({ type: 'adoptRun', id: run.id, as: choice.as, text: run.output! }, choice.done))}
          >
            {choice.label}
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
                  <span className="act-when">{formatAgo(e.at, state.settings.timezone ?? 'UTC')}</span>
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
