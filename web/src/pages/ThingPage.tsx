import { useState } from 'react'
import { Link, useParams } from 'react-router'
import { Bell, BellOff, Check, ChevronRight, Plus, Send, X } from 'lucide-react'
import { Fade, KindLabel, TaskBox, Timeline, TrustTag } from '../components/Marks'
import { Modal } from '../components/Overlay'
import { Button, Empty, Field, Seg, Sheet, Switch, Tag } from '../components/ui'
import { handoffStatusLabel, projectStatusLabel, taskStatusLabel, taskStatusOrder } from '../domain/labels'
import { findThing, isOpenTask, thingProjectId, thingStatus, thingTitle, timelineFor, type Thing } from '../domain/things'
import { formatAgo, formatWhen, fromLocalInput, toLocalInput } from '../domain/time'
import type { Idea, Project, ProjectStatus, Task } from '../domain/types'
import { useStore } from '../store/context'
import { useCreateHandoff } from '../store/hooks'
import { useToast } from '../store/toast'
import { NotFound } from './NotFound'

const triggerKindText = { time: '到时间', event: '有事件', 'event+delay': '事件之后' } as const

function ProjectSelect({ id, projectId }: { id: string; projectId?: string }) {
  const { state, dispatch } = useStore()
  return (
    <select
      className="inline-select"
      value={projectId ?? ''}
      aria-label="属于项目"
      onChange={(e) => dispatch({ type: 'moveThing', id, projectId: e.target.value || undefined })}
    >
      <option value="">不属于项目</option>
      {state.projects.map((p) => (
        <option key={p.id} value={p.id}>
          {p.name}
        </option>
      ))}
    </select>
  )
}

function TaskBody({ task }: { task: Task }) {
  const { state, dispatch } = useStore()
  const idea = state.ideas.find((i) => i.id === task.ideaId)
  const unlocks = state.tasks.filter((t) => t.dependsOn.includes(task.id))

  const setDate = (key: 'due' | 'scheduled', value: string) =>
    dispatch({
      type: 'updateTask',
      id: task.id,
      patch: { [key]: fromLocalInput(value) },
      summary: value
        ? `${key === 'due' ? '截止' : '安排'}改到 ${formatWhen(new Date(value).toISOString())}`
        : `去掉了${key === 'due' ? '截止时间' : '安排时间'}`,
    })

  return (
    <>
      <div className="doc-block" style={{ borderTop: 0, marginTop: 14, paddingTop: 0 }}>
        <Seg
          label="状态"
          value={task.status}
          onChange={(status) => dispatch({ type: 'setTaskStatus', id: task.id, status })}
          items={taskStatusOrder.map((s) => ({ value: s, label: taskStatusLabel[s].text }))}
        />
      </div>

      <div className="doc-block">
        <dl className="props">
          <dt>截止</dt>
          <dd>
            <input type="datetime-local" className="inline-select" value={toLocalInput(task.due)} onChange={(e) => setDate('due', e.target.value)} aria-label="截止" />
          </dd>
          <dt>安排在</dt>
          <dd>
            <input type="datetime-local" className="inline-select" value={toLocalInput(task.scheduled)} onChange={(e) => setDate('scheduled', e.target.value)} aria-label="安排在" />
          </dd>
          <dt>属于</dt>
          <dd>
            <ProjectSelect id={task.id} projectId={task.projectId} />
          </dd>
          {task.status === 'waiting' && (
            <>
              <dt>在等</dt>
              <dd>
                <input
                  className="inline-select"
                  style={{ width: '100%' }}
                  defaultValue={task.waitingFor}
                  placeholder="对方回复、前置任务完成…"
                  onBlur={(e) =>
                    e.target.value !== (task.waitingFor ?? '') &&
                    dispatch({ type: 'updateTask', id: task.id, patch: { waitingFor: e.target.value }, summary: `在等：${e.target.value}` })
                  }
                />
              </dd>
            </>
          )}
          {task.dependsOn.length > 0 && (
            <>
              <dt>先完成</dt>
              <dd className="stack-sm" style={{ gap: 4 }}>
                {task.dependsOn.map((id) => {
                  const dep = state.tasks.find((t) => t.id === id)
                  return (
                    dep && (
                      <span key={id} className="row-nowrap">
                        <TaskBox task={dep} />
                        <Link to={`/t/${dep.id}`}>{dep.title}</Link>
                      </span>
                    )
                  )
                })}
              </dd>
            </>
          )}
          {unlocks.length > 0 && (
            <>
              <dt>做完解锁</dt>
              <dd className="stack-sm" style={{ gap: 4 }}>
                {unlocks.map((t) => (
                  <Link key={t.id} to={`/t/${t.id}`}>
                    {t.title}
                  </Link>
                ))}
              </dd>
            </>
          )}
          {idea && (
            <>
              <dt>来自想法</dt>
              <dd>
                <Link to={`/t/${idea.id}`}>{idea.title}</Link>
              </dd>
            </>
          )}
        </dl>
      </div>

      {task.triggers.length > 0 && (
        <div className="doc-block">
          <h3>提醒</h3>
          {task.triggers.map((tr) => (
            <div key={tr.id} className="spread">
              <div>
                <div className="row-nowrap ink">
                  {tr.active ? <Bell size={14} /> : <BellOff size={14} className="faint" />}
                  {tr.description}
                  <Tag>{triggerKindText[tr.kind]}</Tag>
                </div>
                {tr.guard && <div className="tiny muted">{tr.guard}</div>}
                {tr.active && tr.nextAt && <div className="tiny muted">下一次：{formatWhen(tr.nextAt)}</div>}
              </div>
              <Switch label="提醒开关" checked={tr.active} onChange={() => dispatch({ type: 'toggleTrigger', taskId: task.id, triggerId: tr.id })} />
            </div>
          ))}
        </div>
      )}

      <div className="doc-block">
        <h3>笔记</h3>
        <textarea
          className="lined"
          defaultValue={task.notes}
          placeholder="想到什么就写在这里…"
          onBlur={(e) => e.target.value !== (task.notes ?? '') && dispatch({ type: 'setNotes', id: task.id, text: e.target.value })}
          aria-label="笔记"
        />
      </div>
    </>
  )
}

function IdeaBody({ idea }: { idea: Idea }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [condition, setCondition] = useState('')
  const [shelving, setShelving] = useState(false)
  const [reason, setReason] = useState('')
  const [shelveCondition, setShelveCondition] = useState('')
  const task = state.tasks.find((t) => t.ideaId === idea.id)

  return (
    <>
      {idea.status === 'awakened' && idea.wake && (
        <div className="doc-block" style={{ borderTop: 0, marginTop: 18, paddingTop: 0 }}>
          <div className="wake-note">
            <div className="hand" style={{ fontSize: 16, fontWeight: 400, marginBottom: 4 }}>
              ✦ 它回来了
            </div>
            <p>{idea.wake.reason}</p>
            <div className="item-actions">
              <Button size="sm" variant="primary" onClick={() => dispatch({ type: 'ideaPromote', id: idea.id })}>
                转成待办
              </Button>
              <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
                继续想
              </Button>
              <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ideaSnooze', id: idea.id, days: 7 })}>
                一周后再说
              </Button>
              <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ideaStopReminders', id: idea.id })}>
                别再提醒
              </Button>
            </div>
          </div>
        </div>
      )}

      <div className="doc-block" style={idea.status === 'awakened' ? undefined : { borderTop: 0, marginTop: 14, paddingTop: 0 }}>
        <textarea
          className="lined"
          defaultValue={idea.body}
          placeholder="这个想法具体是什么…"
          onBlur={(e) => e.target.value !== idea.body && dispatch({ type: 'setNotes', id: idea.id, text: e.target.value })}
          aria-label="想法内容"
        />
        <dl className="props">
          <dt>属于</dt>
          <dd>
            <ProjectSelect id={idea.id} projectId={idea.projectId} />
          </dd>
        </dl>
        {idea.status === 'promoted' && task && (
          <p>
            已经转成待办：<Link to={`/t/${task.id}`}>{task.title}</Link>
          </p>
        )}
        <div className="row">
          {idea.status === 'active' && (
            <>
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
              <Button size="sm" onClick={() => setShelving(true)}>
                先放一放
              </Button>
            </>
          )}
          {idea.status === 'shelved' && (
            <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
              现在就继续
            </Button>
          )}
          {(idea.status === 'active' || idea.status === 'shelved') && (
            <Button size="sm" variant="danger" onClick={() => dispatch({ type: 'ideaDrop', id: idea.id })}>
              放弃
            </Button>
          )}
          {idea.status === 'dropped' && (
            <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
              重新拾起
            </Button>
          )}
        </div>
      </div>

      {(idea.status === 'shelved' || idea.status === 'awakened' || idea.conditions.length > 0) && (
        <div className="doc-block">
          <h3>什么时候再看它</h3>
          {idea.shelvedReason && <p className="muted">放下的原因：{idea.shelvedReason}</p>}
          {!idea.remindersOn && idea.status !== 'dropped' && <p className="small muted">提醒已关闭。想法还在，随时能在“事情”里找回。</p>}
          <ul className="conds">
            {idea.conditions.map((c) => (
              <li key={c.id}>
                <span className={`cond-mark${c.met ? ' met' : ''}`}>{c.met && <Check size={11} strokeWidth={3} />}</span>
                <div className="grow">
                  <div className={c.met ? 'ink' : undefined}>{c.description}</div>
                  <div className="tiny muted">
                    {c.met && c.metAt ? `${formatAgo(c.metAt)}满足` : c.dueAt ? `到 ${formatWhen(c.dueAt)}` : '等新的信息'}
                    {c.metBy && ` · ${c.metBy.label}`}
                  </div>
                </div>
                {!c.met && (
                  <Button size="sm" variant="quiet" icon={<X size={13} />} aria-label="去掉这个条件" onClick={() => dispatch({ type: 'removeCondition', ideaId: idea.id, conditionId: c.id })} />
                )}
              </li>
            ))}
          </ul>
          <form
            className="row-nowrap"
            onSubmit={(e) => {
              e.preventDefault()
              if (!condition.trim()) return
              dispatch({ type: 'addCondition', ideaId: idea.id, description: condition.trim() })
              setCondition('')
            }}
          >
            <input className="inline-select" style={{ flex: 1 }} value={condition} onChange={(e) => setCondition(e.target.value)} placeholder="再加一个条件，比如“拿到报价”…" />
            <Button size="sm" type="submit" icon={<Plus size={13} />} disabled={!condition.trim()}>
              加
            </Button>
          </form>
        </div>
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
                  dispatch({ type: 'ideaShelve', id: idea.id, reason: reason.trim(), condition: shelveCondition.trim() })
                  setShelving(false)
                  toast.show('放下了。条件满足时它会回来')
                }}
              >
                放下
              </Button>
            </>
          }
        >
          <p className="small muted">写下为什么先不做，以及什么情况下值得再看。条件满足时它会自己回来，并告诉你为什么。</p>
          <Field label="为什么先放下">
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} autoFocus />
          </Field>
          <Field label="什么时候再看（可选）">
            <input className="input" value={shelveCondition} onChange={(e) => setShelveCondition(e.target.value)} placeholder="拿到成本数据、下个月初…" />
          </Field>
        </Modal>
      )}
    </>
  )
}

function ProjectBody({ project }: { project: Project }) {
  const { state, dispatch } = useStore()
  const [step, setStep] = useState('')
  const [task, setTask] = useState('')
  const tasks = state.tasks.filter((t) => t.projectId === project.id && isOpenTask(t))
  const ideas = state.ideas.filter((i) => i.projectId === project.id && (i.status === 'active' || i.status === 'awakened' || i.status === 'shelved'))
  const decisions = state.memories.filter((m) => m.projectId === project.id && m.kind === 'decision')

  return (
    <>
      <div className="doc-block" style={{ borderTop: 0, marginTop: 14, paddingTop: 0 }}>
        <Seg
          label="项目状态"
          value={project.status}
          onChange={(status: ProjectStatus) => dispatch({ type: 'updateProject', id: project.id, patch: { status } })}
          items={(['active', 'paused', 'done'] as ProjectStatus[]).map((s) => ({ value: s, label: projectStatusLabel[s].text }))}
        />
        <input
          className="bare"
          style={{ fontSize: 16, color: 'var(--ink-2)' }}
          defaultValue={project.goal}
          placeholder="这个项目想达成什么？"
          onBlur={(e) => e.target.value !== project.goal && dispatch({ type: 'updateProject', id: project.id, patch: { goal: e.target.value } })}
          aria-label="目标"
        />
      </div>

      <div className="doc-block">
        <h3>现在到哪儿了</h3>
        <textarea
          className="lined"
          defaultValue={project.progress}
          placeholder="写下当前进度，下次回来一眼就能接上…"
          onBlur={(e) => e.target.value !== project.progress && dispatch({ type: 'updateProject', id: project.id, patch: { progress: e.target.value } })}
          aria-label="进度"
        />
      </div>

      <div className="doc-block">
        <h3>下一步</h3>
        <ul className="conds">
          {project.nextSteps.map((s, i) => (
            <li key={`${s}-${i}`}>
              <button
                type="button"
                className="box"
                aria-label="完成这一步"
                title="完成这一步"
                onClick={() => dispatch({ type: 'updateProject', id: project.id, patch: { nextSteps: project.nextSteps.filter((_, j) => j !== i) } })}
              />
              <span className="grow ink">{s}</span>
            </li>
          ))}
        </ul>
        <form
          className="row-nowrap"
          onSubmit={(e) => {
            e.preventDefault()
            if (!step.trim()) return
            dispatch({ type: 'updateProject', id: project.id, patch: { nextSteps: [...project.nextSteps, step.trim()] } })
            setStep('')
          }}
        >
          <input className="inline-select" style={{ flex: 1 }} value={step} onChange={(e) => setStep(e.target.value)} placeholder="加一步…" />
          <Button size="sm" type="submit" icon={<Plus size={13} />} disabled={!step.trim()}>
            加
          </Button>
        </form>
      </div>

      <div className="doc-block">
        <h3>里面的事</h3>
        {tasks.length + ideas.length === 0 && <p className="muted small">还没有待办或想法。</p>}
        <div className="stack-sm" style={{ gap: 2 }}>
          {tasks.map((t) => (
            <Link key={t.id} to={`/t/${t.id}`} className="motion-line">
              <TaskBox task={t} />
              <span className="grow">{t.title}</span>
              <span className="when">{t.status === 'waiting' ? `等 ${t.waitingFor ?? ''}` : t.due ? formatWhen(t.due) : ''}</span>
            </Link>
          ))}
          {ideas.map((i) => (
            <Link key={i.id} to={`/t/${i.id}`} className="motion-line">
              <span className="kind kind-idea" style={{ width: 18, justifyContent: 'center' }} />
              <span className="grow">{i.title}</span>
              <span className="when">{i.status === 'shelved' ? '放着' : i.status === 'awakened' ? '刚回来' : '想法'}</span>
            </Link>
          ))}
        </div>
        <form
          className="row-nowrap"
          onSubmit={(e) => {
            e.preventDefault()
            if (!task.trim()) return
            dispatch({ type: 'addTask', title: task.trim(), projectId: project.id })
            setTask('')
          }}
        >
          <input className="inline-select" style={{ flex: 1 }} value={task} onChange={(e) => setTask(e.target.value)} placeholder="加一件待办…" />
          <Button size="sm" type="submit" icon={<Plus size={13} />} disabled={!task.trim()}>
            加
          </Button>
        </form>
      </div>

      <div className="doc-block">
        <h3>已经定下的</h3>
        {decisions.length === 0 && <p className="muted small">还没有记录决定。</p>}
        {decisions.map((m) => (
          <div key={m.id} className="row-nowrap" style={{ alignItems: 'flex-start' }}>
            <span className="hand" style={{ color: 'var(--accent)', fontWeight: 400 }}>
              ›
            </span>
            <span className={`grow${m.epistemic === 'inferred' ? ' guess' : ''}`}>{m.text}</span>
            <TrustTag value={m.epistemic} />
          </div>
        ))}
      </div>
    </>
  )
}

function Rail({ thing }: { thing: Thing }) {
  const { state } = useStore()
  const createHandoff = useCreateHandoff()
  const [agentId, setAgentId] = useState('a_claude')
  const projectId = thing.kind === 'project' ? thing.id : thingProjectId(thing)
  const memories = state.memories
    .filter((m) => (projectId ? m.projectId === projectId : !m.projectId))
    .sort((a, b) => b.exposure - a.exposure)
    .slice(0, 6)
  const handoffs = state.handoffs.filter((h) =>
    thing.kind === 'project' ? h.projectId === thing.id : thing.kind === 'task' ? h.taskId === thing.id : h.ideaId === thing.id,
  )
  const target = thing.kind === 'project' ? { projectId: thing.id } : thing.kind === 'task' ? { taskId: thing.id } : { ideaId: thing.id }

  return (
    <aside className="rail">
      <Sheet pad>
        <h3>交给 AI</h3>
        <p className="small muted" style={{ marginBottom: 10 }}>
          自动整理目标、背景、进度和决定，发出前你可以改。结果回来会写回这里。
        </p>
        <div className="row-nowrap">
          <select className="inline-select" value={agentId} onChange={(e) => setAgentId(e.target.value)} aria-label="交给哪个 AI">
            {state.agents
              .filter((a) => a.enabled)
              .map((a) => (
                <option key={a.id} value={a.id}>
                  {a.name}
                </option>
              ))}
          </select>
          <Button size="sm" variant="primary" icon={<Send size={13} />} onClick={() => createHandoff(target, agentId)}>
            写交接
          </Button>
        </div>
        {handoffs.length > 0 && (
          <div className="stack-sm" style={{ marginTop: 12 }}>
            {handoffs.map((h) => (
              <Link key={h.id} to={`/handoff/${h.id}`} className="spread small">
                <span className="grow" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {state.agents.find((a) => a.id === h.agentId)?.name} · {h.title}
                </span>
                <Tag tone={handoffStatusLabel[h.status].tone}>{handoffStatusLabel[h.status].text}</Tag>
              </Link>
            ))}
          </div>
        )}
      </Sheet>

      <Sheet pad>
        <h3>相关的记忆</h3>
        {memories.length === 0 ? (
          <p className="small muted">还没有。</p>
        ) : (
          memories.map((m) => (
            <Link key={m.id} to={`/library?m=${m.id}`} className="mem-line row-nowrap" style={{ color: 'inherit', alignItems: 'flex-start' }}>
              <span className={`grow${m.epistemic === 'inferred' ? ' guess' : ''}`}>{m.text}</span>
              <Fade value={m.exposure} />
            </Link>
          ))
        )}
      </Sheet>
    </aside>
  )
}

function statusSentence(thing: Thing): string {
  if (thing.kind === 'task') {
    const t = thing.item
    if (t.status === 'waiting') return `在等${t.waitingFor ? `：${t.waitingFor}` : ''}`
    if (t.status === 'done') return `${formatAgo(t.updatedAt)}完成`
    if (t.due) return `截止 ${formatWhen(t.due)}`
    return `${formatAgo(t.createdAt)}记下`
  }
  if (thing.kind === 'idea') {
    const i = thing.item
    const pending = i.conditions.filter((c) => !c.met).length
    if (i.status === 'shelved') return pending ? `放着，等 ${pending} 个条件` : '放着'
    return `${formatAgo(i.createdAt)}想到的`
  }
  return `${formatAgo(thing.item.updatedAt)}更新`
}

export function ThingPage() {
  const { id = '' } = useParams()
  const { state, dispatch } = useStore()
  const thing = findThing(state, id)
  if (!thing) return <NotFound />

  const project = state.projects.find((p) => p.id === thingProjectId(thing))
  const status = thingStatus(thing)
  const events = timelineFor(state, thing)

  return (
    <main className="page">
      <nav className="crumbs" aria-label="位置">
        <Link to="/things">事情</Link>
        {project && (
          <>
            <ChevronRight size={13} />
            <Link to={`/t/${project.id}`}>{project.name}</Link>
          </>
        )}
      </nav>

      <div className="doc-layout">
        <div className="stack">
          <article className="sheet doc">
            <div className="row" style={{ marginBottom: 8 }}>
              <KindLabel kind={thing.kind} />
              <Tag tone={status.tone}>{status.text}</Tag>
            </div>
            <textarea
              key={`title-${thing.id}`}
              className="doc-title"
              rows={1}
              defaultValue={thingTitle(thing)}
              aria-label="标题"
              onKeyDown={(e) => e.key === 'Enter' && (e.preventDefault(), e.currentTarget.blur())}
              onBlur={(e) => {
                const title = e.target.value.trim()
                if (title && title !== thingTitle(thing)) dispatch({ type: 'renameThing', id: thing.id, title })
              }}
            />
            <p className="doc-status">{statusSentence(thing)}</p>

            {thing.kind === 'task' && <TaskBody key={`body-${thing.id}`} task={thing.item} />}
            {thing.kind === 'idea' && <IdeaBody key={`body-${thing.id}`} idea={thing.item} />}
            {thing.kind === 'project' && <ProjectBody key={`body-${thing.id}`} project={thing.item} />}
          </article>

          <section className="section">
            <h2 className="section-title">
              <span className="squiggle">来龙去脉</span>
            </h2>
            <Sheet pad>{events.length ? <Timeline key={thing.id} events={events} /> : <Empty>还没有什么经过。</Empty>}</Sheet>
          </section>
        </div>

        <Rail thing={thing} />
      </div>
    </main>
  )
}
