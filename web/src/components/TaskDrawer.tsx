import { Bell, BellOff, Send } from 'lucide-react'
import { taskStatusLabel, taskStatusOrder } from '../domain/labels'
import { formatWhen, fromLocalInput, toLocalInput } from '../domain/time'
import type { Task } from '../domain/types'
import { useStore } from '../store/context'
import { useCreateHandoff, useDetail } from '../store/hooks'
import { ProjectName, SourceLine, StatusDot, Timeline } from './bits'
import { Drawer } from './Overlay'
import { Badge, Button, Field, Switch } from './ui'

const triggerKindText: Record<Task['triggers'][number]['kind'], string> = {
  time: '时间驱动',
  event: '事件驱动',
  'event+delay': '事件 + 延时',
}

export function TaskDrawer({ task }: { task: Task }) {
  const { state, dispatch } = useStore()
  const { close, openTask, openIdea } = useDetail()
  const createHandoff = useCreateHandoff()
  const idea = state.ideas.find((i) => i.id === task.ideaId)
  const dependents = state.tasks.filter((t) => t.dependsOn.includes(task.id))

  const setDate = (key: 'due' | 'scheduled', value: string) =>
    dispatch({
      type: 'updateTask',
      id: task.id,
      patch: { [key]: fromLocalInput(value) },
      summary: value ? `${key === 'due' ? '截止时间' : '安排时间'}改为 ${formatWhen(new Date(value).toISOString())}` : `清除${key === 'due' ? '截止时间' : '安排时间'}`,
    })

  return (
    <Drawer
      title={task.title}
      onClose={close}
      badges={
        <>
          <Badge tone={taskStatusLabel[task.status].tone}>{taskStatusLabel[task.status].text}</Badge>
          <ProjectName id={task.projectId} />
        </>
      }
    >
      <div className="drawer-section">
        <h3>状态</h3>
        <div className="tabs">
          {taskStatusOrder.map((status) => (
            <button
              key={status}
              type="button"
              className={`tab${task.status === status ? ' active' : ''}`}
              onClick={() => status !== task.status && dispatch({ type: 'setTaskStatus', id: task.id, status })}
            >
              {taskStatusLabel[status].text}
            </button>
          ))}
        </div>
        {task.status === 'waiting' && (
          <Field label="在等什么">
            <input
              className="input"
              defaultValue={task.waitingFor}
              placeholder="例如：对方回复、前置任务完成"
              onBlur={(e) =>
                e.target.value !== (task.waitingFor ?? '') &&
                dispatch({ type: 'updateTask', id: task.id, patch: { waitingFor: e.target.value }, summary: `等待：${e.target.value}` })
              }
            />
          </Field>
        )}
        {task.notes && <p className="pre">{task.notes}</p>}
      </div>

      <div className="drawer-section">
        <h3>时间</h3>
        <div className="grid-2">
          <Field label="截止">
            <input type="datetime-local" className="input" value={toLocalInput(task.due)} onChange={(e) => setDate('due', e.target.value)} />
          </Field>
          <Field label="安排在">
            <input
              type="datetime-local"
              className="input"
              value={toLocalInput(task.scheduled)}
              onChange={(e) => setDate('scheduled', e.target.value)}
            />
          </Field>
        </div>
      </div>

      <div className="drawer-section">
        <h3>提醒与触发</h3>
        {task.triggers.length === 0 && <p className="muted small">没有设置提醒。</p>}
        {task.triggers.map((trigger) => (
          <div key={trigger.id} className="row-between">
            <div>
              <div className="row">
                {trigger.active ? <Bell size={14} /> : <BellOff size={14} className="muted" />}
                <span className="ink">{trigger.description}</span>
                <Badge>{triggerKindText[trigger.kind]}</Badge>
              </div>
              {trigger.guard && <div className="small muted">{trigger.guard}</div>}
              {trigger.active && trigger.nextAt && <div className="small muted">下一次：{formatWhen(trigger.nextAt)}</div>}
            </div>
            <Switch
              label="启用提醒"
              checked={trigger.active}
              onChange={() => dispatch({ type: 'toggleTrigger', taskId: task.id, triggerId: trigger.id })}
            />
          </div>
        ))}
      </div>

      {(task.dependsOn.length > 0 || dependents.length > 0) && (
        <div className="drawer-section">
          <h3>依赖</h3>
          {task.dependsOn.map((id) => {
            const dep = state.tasks.find((t) => t.id === id)
            return (
              dep && (
                <div key={id} className="row">
                  <StatusDot task={dep} />
                  <span className="small muted">先完成</span>
                  <a href="#" onClick={(e) => (e.preventDefault(), openTask(dep.id))}>
                    {dep.title}
                  </a>
                </div>
              )
            )
          })}
          {dependents.map((dep) => (
            <div key={dep.id} className="row">
              <StatusDot task={dep} />
              <span className="small muted">完成后解锁</span>
              <a href="#" onClick={(e) => (e.preventDefault(), openTask(dep.id))}>
                {dep.title}
              </a>
            </div>
          ))}
        </div>
      )}

      {(task.sources.length > 0 || idea) && (
        <div className="drawer-section">
          <h3>来源</h3>
          {idea && (
            <div className="small">
              由 IDEA{' '}
              <a href="#" onClick={(e) => (e.preventDefault(), openIdea(idea.id))}>
                {idea.title}
              </a>{' '}
              转成
            </div>
          )}
          {task.sources.map((s, i) => (
            <SourceLine key={i} source={s} />
          ))}
        </div>
      )}

      <div className="drawer-section">
        <h3>历史</h3>
        <Timeline items={task.history} />
      </div>

      <div className="drawer-section">
        <Button icon={<Send size={15} />} onClick={() => createHandoff({ taskId: task.id })}>
          交给外部 AI
        </Button>
      </div>
    </Drawer>
  )
}
