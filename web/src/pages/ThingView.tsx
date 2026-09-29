import { useEffect, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router'
import { Bell, BellOff, CalendarClock, Check, ChevronRight, Clock, Folder, Hourglass, Plus, Sparkles, X } from 'lucide-react'
import { Composer } from '../components/Composer'
import { DocsBlock } from '../components/Docs'
import { KindLabel, TaskBox } from '../components/Marks'
import { Modal } from '../components/Overlay'
import { RunCard } from '../components/RunCard'
import { TaskRow } from '../components/TaskList'
import { Button, Field, Switch } from '../components/ui'
import { ideaStatusLabel, projectStatusLabel, taskStatusLabel, taskStatusOrder } from '../domain/labels'
import { findThing, isOpenTask, thingProjectId, thingTitle, type Thing } from '../domain/things'
import { formatAgo, formatWhen, fromLocalInput, toLocalInput } from '../domain/time'
import type { Idea, Project, ProjectStatus, Task } from '../domain/types'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'
import { NotFound } from './NotFound'

function ProjectProp({ id, projectId }: { id: string; projectId?: string }) {
  const { state, dispatch } = useStore()
  return (
    <label className="prop">
      <Folder size={12} />
      <select value={projectId ?? ''} onChange={(e) => dispatch({ type: 'moveThing', id, projectId: e.target.value || undefined })} aria-label="项目">
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

function TaskProps({ task }: { task: Task }) {
  const { dispatch } = useStore()
  const setDate = (key: 'due' | 'scheduled', value: string) =>
    dispatch({
      type: 'updateTask',
      id: task.id,
      patch: { [key]: fromLocalInput(value) },
      summary: value ? `${key === 'due' ? '截止' : '安排'}改到 ${formatWhen(new Date(value).toISOString())}` : `去掉${key === 'due' ? '截止' : '安排'}时间`,
    })
  return (
    <>
      <label className="prop">
        <select value={task.status} onChange={(e) => dispatch({ type: 'setTaskStatus', id: task.id, status: e.target.value as Task['status'] })} aria-label="状态">
          {taskStatusOrder.map((s) => (
            <option key={s} value={s}>
              {taskStatusLabel[s].text}
            </option>
          ))}
        </select>
      </label>
      <label className="prop" title="截止">
        <CalendarClock size={12} />
        <input type="datetime-local" value={toLocalInput(task.due)} onChange={(e) => setDate('due', e.target.value)} aria-label="截止" />
      </label>
      <label className="prop" title="安排在">
        <Clock size={12} />
        <input type="datetime-local" value={toLocalInput(task.scheduled)} onChange={(e) => setDate('scheduled', e.target.value)} aria-label="安排在" />
      </label>
      <ProjectProp id={task.id} projectId={task.projectId} />
      {task.status === 'waiting' && (
        <label className="prop">
          <Hourglass size={12} />
          <input
            defaultValue={task.waitingFor}
            placeholder="在等什么"
            onBlur={(e) =>
              e.target.value !== (task.waitingFor ?? '') &&
              dispatch({ type: 'updateTask', id: task.id, patch: { waitingFor: e.target.value }, summary: `在等：${e.target.value}` })
            }
            aria-label="在等什么"
          />
        </label>
      )}
    </>
  )
}

function Checklist({ task }: { task: Task }) {
  const { state, dispatch } = useStore()
  const [text, setText] = useState('')
  const done = task.checklist.filter((c) => c.done).length
  const blockers = task.dependsOn.map((id) => state.tasks.find((t) => t.id === id)).filter((t): t is Task => !!t)
  return (
    <section className="block">
      <div className="block-head">
        <h2>子任务</h2>
        <span className="n">
          {done}/{task.checklist.length}
        </span>
        {task.checklist.length > 0 && (
          <div className="meter">
            <div style={{ width: `${(done / task.checklist.length) * 100}%` }} />
          </div>
        )}
      </div>
      {blockers.map((b) => (
        <div key={b.id} className="check-row">
          <TaskBox task={b} />
          <span className="small muted">先完成</span>
          <Link to={`/t/${b.id}`}>{b.title}</Link>
        </div>
      ))}
      {task.checklist.map((c) => (
        <div key={c.id} className="check-row">
          <button
            type="button"
            className={`box${c.done ? ' done' : ''}`}
            aria-label={c.done ? '标记未完成' : '标记完成'}
            onClick={() => dispatch({ type: 'toggleCheck', taskId: task.id, itemId: c.id })}
          >
            {c.done && <Check size={11} strokeWidth={3} />}
          </button>
          <span className={`grow${c.done ? ' faint' : ' ink'}`} style={c.done ? { textDecoration: 'line-through' } : undefined}>
            {c.text}
          </span>
          <Button size="sm" variant="quiet" className="del" icon={<X size={12} />} aria-label="删除" onClick={() => dispatch({ type: 'removeCheck', taskId: task.id, itemId: c.id })} />
        </div>
      ))}
      <form
        className="add-line"
        onSubmit={(e) => {
          e.preventDefault()
          if (!text.trim()) return
          dispatch({ type: 'addCheck', taskId: task.id, text: text.trim() })
          setText('')
        }}
      >
        <Plus size={14} />
        <input value={text} onChange={(e) => setText(e.target.value)} placeholder="加一个子任务，回车确认" aria-label="新的子任务" />
      </form>
      {task.triggers.map((tr) => (
        <div key={tr.id} className="check-row small">
          {tr.active ? <Bell size={13} className="muted" /> : <BellOff size={13} className="faint" />}
          <span className="grow">
            <span className="ink">{tr.description}</span>
            {tr.guard && <span className="muted"> · {tr.guard}</span>}
            {tr.active && tr.nextAt && <span className="muted"> · 下次 {formatWhen(tr.nextAt)}</span>}
          </span>
          <Switch label="提醒开关" checked={tr.active} onChange={() => dispatch({ type: 'toggleTrigger', taskId: task.id, triggerId: tr.id })} />
        </div>
      ))}
    </section>
  )
}

function IdeaBlock({ idea }: { idea: Idea }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [condition, setCondition] = useState('')
  const [shelving, setShelving] = useState(false)
  const [reason, setReason] = useState('')
  const task = state.tasks.find((t) => t.ideaId === idea.id)

  return (
    <>
      {idea.status === 'awakened' && idea.wake && (
        <div className="wake-banner">
          <Sparkles size={15} style={{ color: 'var(--accent)', flex: 'none', marginTop: 2 }} />
          <div className="grow">
            <div style={{ fontWeight: 600 }}>它回来了</div>
            <p>{idea.wake.reason}</p>
          </div>
        </div>
      )}
      <div className="row" style={{ marginTop: 6 }}>
        {(idea.status === 'active' || idea.status === 'awakened') && (
          <Button
            size="sm"
            variant="primary"
            onClick={() => {
              dispatch({ type: 'ideaPromote', id: idea.id })
              toast.show('转成待办了')
            }}
          >
            转成待办
          </Button>
        )}
        {idea.status === 'awakened' && (
          <>
            <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
              继续想
            </Button>
            <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ideaSnooze', id: idea.id, days: 7 })}>
              一周后再说
            </Button>
          </>
        )}
        {idea.status === 'active' && (
          <Button size="sm" onClick={() => setShelving(true)}>
            先放一放
          </Button>
        )}
        {(idea.status === 'shelved' || idea.status === 'dropped') && (
          <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
            {idea.status === 'dropped' ? '重新拾起' : '现在就继续'}
          </Button>
        )}
        {idea.status !== 'dropped' && idea.status !== 'promoted' && (
          <Button size="sm" variant="danger" onClick={() => dispatch({ type: 'ideaDrop', id: idea.id })}>
            放弃
          </Button>
        )}
        {task && (
          <span className="small">
            已转成待办：<Link to={`/t/${task.id}`}>{task.title}</Link>
          </span>
        )}
      </div>

      {(idea.conditions.length > 0 || idea.status === 'shelved') && (
        <section className="block">
          <div className="block-head">
            <h2>什么时候再看</h2>
            {idea.shelvedReason && <span className="small muted ellipsis">放下的原因：{idea.shelvedReason}</span>}
          </div>
          {idea.conditions.map((c) => (
            <div key={c.id} className="check-row">
              <span className={`box${c.met ? ' done' : ''}`} style={{ borderRadius: '50%', cursor: 'default' }}>
                {c.met && <Check size={11} strokeWidth={3} />}
              </span>
              <span className="grow">
                <span className={c.met ? 'ink' : ''}>{c.description}</span>
                <span className="tiny muted">
                  {' · '}
                  {c.met && c.metAt ? `${formatAgo(c.metAt)}满足${c.metBy ? `（${c.metBy.label}）` : ''}` : c.dueAt ? `到 ${formatWhen(c.dueAt)}` : '等新的信息'}
                </span>
              </span>
              {!c.met && (
                <Button size="sm" variant="quiet" className="del" icon={<X size={12} />} aria-label="去掉" onClick={() => dispatch({ type: 'removeCondition', ideaId: idea.id, conditionId: c.id })} />
              )}
            </div>
          ))}
          <form
            className="add-line"
            onSubmit={(e) => {
              e.preventDefault()
              if (!condition.trim()) return
              dispatch({ type: 'addCondition', ideaId: idea.id, description: condition.trim() })
              setCondition('')
            }}
          >
            <Plus size={14} />
            <input value={condition} onChange={(e) => setCondition(e.target.value)} placeholder="再加一个条件，比如“拿到报价”" aria-label="新的条件" />
          </form>
        </section>
      )}

      {shelving && (
        <Modal
          title="先放一放"
          onClose={() => setShelving(false)}
          actions={
            <>
              <Button variant="quiet" onClick={() => setShelving(false)}>
                取消
              </Button>
              <Button
                variant="primary"
                disabled={!reason.trim()}
                onClick={() => {
                  dispatch({ type: 'ideaShelve', id: idea.id, reason: reason.trim(), condition: condition.trim() })
                  setShelving(false)
                  setCondition('')
                }}
              >
                放下
              </Button>
            </>
          }
        >
          <Field label="为什么先放下">
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} autoFocus />
          </Field>
          <Field label="什么时候再看（可选）">
            <input className="input" value={condition} onChange={(e) => setCondition(e.target.value)} placeholder="拿到成本数据、下个月初…" />
          </Field>
        </Modal>
      )}
    </>
  )
}

function ProjectBlock({ project }: { project: Project }) {
  const { state, dispatch } = useStore()
  const navigate = useNavigate()
  const [step, setStep] = useState('')
  const [task, setTask] = useState('')
  const tasks = state.tasks.filter((t) => t.projectId === project.id && isOpenTask(t))
  const decisions = state.memories.filter((m) => m.projectId === project.id && m.kind === 'decision' && m.epistemic !== 'inferred')

  return (
    <>
      <section className="block">
        <div className="block-head">
          <h2>进度</h2>
        </div>
        <textarea
          key={`progress-${project.id}-${project.progress.length}`}
          className="desc"
          defaultValue={project.progress}
          placeholder="写下现在到哪了，下次回来一眼接上…"
          onBlur={(e) => e.target.value !== project.progress && dispatch({ type: 'updateProject', id: project.id, patch: { progress: e.target.value } })}
          aria-label="进度"
        />
        <div className="stack-sm" style={{ marginTop: 6 }}>
          {project.nextSteps.map((s, i) => (
            <div key={`${s}-${i}`} className="check-row">
              <button
                type="button"
                className="box"
                aria-label="完成这一步"
                onClick={() => dispatch({ type: 'updateProject', id: project.id, patch: { nextSteps: project.nextSteps.filter((_, j) => j !== i) } })}
              />
              <span className="grow ink">下一步：{s}</span>
            </div>
          ))}
          <form
            className="add-line"
            onSubmit={(e) => {
              e.preventDefault()
              if (!step.trim()) return
              dispatch({ type: 'updateProject', id: project.id, patch: { nextSteps: [...project.nextSteps, step.trim()] } })
              setStep('')
            }}
          >
            <Plus size={14} />
            <input value={step} onChange={(e) => setStep(e.target.value)} placeholder="加一个下一步" aria-label="新的下一步" />
          </form>
        </div>
      </section>

      <section className="block">
        <div className="block-head">
          <h2>待办</h2>
          <span className="n">{tasks.length}</span>
        </div>
        {tasks.map((t) => (
          <TaskRow key={t.id} task={t} cursor={false} picked={false} selectable={false} onPick={() => undefined} onOpen={() => navigate(`/t/${t.id}`)} />
        ))}
        <form
          className="add-line"
          onSubmit={(e) => {
            e.preventDefault()
            if (!task.trim()) return
            dispatch({ type: 'addTask', title: task.trim(), projectId: project.id })
            setTask('')
          }}
        >
          <Plus size={14} />
          <input value={task} onChange={(e) => setTask(e.target.value)} placeholder="加一件待办，回车确认" aria-label="新的待办" />
        </form>
      </section>

      {decisions.length > 0 && (
        <section className="block">
          <div className="block-head">
            <h2>已经定下的</h2>
            <span className="n">{decisions.length}</span>
          </div>
          {decisions.map((m) => (
            <div key={m.id} className="check-row small">
              <span className="muted">›</span>
              <span className="grow ink">{m.text}</span>
              {m.epistemic === 'planned' && <span className="tag tag-info">计划</span>}
            </div>
          ))}
        </section>
      )}
    </>
  )
}

function Header({ thing }: { thing: Thing }) {
  const { state, dispatch } = useStore()
  const project = state.projects.find((p) => p.id === thingProjectId(thing))
  const body = thing.kind === 'task' ? (thing.item.notes ?? '') : thing.kind === 'idea' ? thing.item.body : thing.item.goal

  return (
    <>
      <nav className="crumbs" aria-label="位置">
        <KindLabel kind={thing.kind} />
        {project && (
          <>
            <ChevronRight size={12} />
            <Link to={`/t/${project.id}`}>{project.name}</Link>
          </>
        )}
        <span className="grow" />
        <span className="faint">{formatAgo(thing.item.updatedAt)}更新</span>
      </nav>
      <textarea
        key={`title-${thing.id}`}
        className="thing-title"
        rows={1}
        defaultValue={thingTitle(thing)}
        aria-label="标题"
        onKeyDown={(e) => e.key === 'Enter' && (e.preventDefault(), e.currentTarget.blur())}
        onBlur={(e) => {
          const t = e.target.value.trim()
          if (t && t !== thingTitle(thing)) dispatch({ type: 'renameThing', id: thing.id, title: t })
        }}
      />
      <div className="props">
        {thing.kind === 'task' && <TaskProps task={thing.item} />}
        {thing.kind === 'idea' && (
          <>
            <span className="prop">{ideaStatusLabel[thing.item.status].text}</span>
            <ProjectProp id={thing.id} projectId={thing.item.projectId} />
          </>
        )}
        {thing.kind === 'project' && (
          <label className="prop">
            <select
              value={thing.item.status}
              onChange={(e) => dispatch({ type: 'updateProject', id: thing.id, patch: { status: e.target.value as ProjectStatus } })}
              aria-label="项目状态"
            >
              {(['active', 'paused', 'done'] as ProjectStatus[]).map((s) => (
                <option key={s} value={s}>
                  {projectStatusLabel[s].text}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>
      <textarea
        key={`body-${thing.id}-${body.length}`}
        className="desc"
        defaultValue={body}
        placeholder={thing.kind === 'project' ? '这个项目想达成什么？' : '补充说明…'}
        aria-label="说明"
        onBlur={(e) => {
          if (e.target.value === body) return
          if (thing.kind === 'project') dispatch({ type: 'updateProject', id: thing.id, patch: { goal: e.target.value } })
          else dispatch({ type: 'setNotes', id: thing.id, text: e.target.value })
        }}
      />
    </>
  )
}

export function ThingView() {
  const { id = '' } = useParams()
  const { hash } = useLocation()
  const { state } = useStore()
  const thing = findThing(state, id)
  const runs = state.runs.filter((r) => r.thingId === id)
  const focusRun = hash.startsWith('#run-') ? hash.slice(5) : undefined

  useEffect(() => {
    if (focusRun) document.getElementById(`run-${focusRun}`)?.scrollIntoView({ block: 'center' })
  }, [focusRun])

  if (!thing) return <NotFound />

  return (
    <div className="thing">
      <Header thing={thing} />
      {thing.kind === 'idea' && <IdeaBlock idea={thing.item} />}
      {thing.kind === 'task' && <Checklist task={thing.item} />}
      {thing.kind === 'project' && <ProjectBlock project={thing.item} />}
      <DocsBlock thingId={thing.id} />
      <section className="block">
        <div className="block-head">
          <h2>AI 工作</h2>
          <span className="n">{runs.length}</span>
        </div>
        {runs.length === 0 && <p className="small muted" style={{ padding: '4px 4px 8px' }}>在下面告诉 AI 要做什么，或者点一个快捷动作。它会带着这件事的背景和你允许的记忆开工。</p>}
        {runs.map((r) => (
          <RunCard key={r.id} run={r} focus={r.id === focusRun} />
        ))}
        <Composer key={thing.id} thing={thing} />
      </section>
    </div>
  )
}
