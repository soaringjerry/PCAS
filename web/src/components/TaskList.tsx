import { useCallback } from 'react'
import { useNavigate } from 'react-router'
import { taskStatusLabel } from '../domain/labels'
import { dayOffset, formatWhen } from '../domain/time'
import type { TaskGroup } from '../domain/views'
import type { Task, TaskStatus } from '../domain/types'
import { useStore } from '../store/context'
import { useListNav } from '../store/useListNav'
import { useToast } from '../store/toast'
import { useWorkspace } from '../store/workspace'
import { TaskBox } from './Marks'
import { Button, Empty, Tag } from './ui'

const cycle: Record<TaskStatus, TaskStatus> = { todo: 'doing', doing: 'waiting', waiting: 'todo', done: 'todo', cancelled: 'todo' }

function when(task: Task): { text: string; late: boolean } {
  const iso = task.due ?? task.scheduled
  if (!iso) return { text: '', late: false }
  const off = dayOffset(iso)
  const late = !!task.due && new Date(task.due).getTime() < Date.now()
  const text = off === 0 ? formatWhen(iso).replace('今天 ', '') : off === 1 ? '明天' : off === -1 ? '昨天' : formatWhen(iso)
  return { text: task.due ? text : `排 ${text}`, late }
}

export function TaskRow({ task, cursor, picked, onPick, onOpen, selectable = true }: {
  task: Task
  cursor: boolean
  picked: boolean
  onPick: () => void
  onOpen: () => void
  selectable?: boolean
}) {
  const { state } = useStore()
  const project = state.projects.find((p) => p.id === task.projectId)
  const w = when(task)
  const checks = task.checklist.length
  const done = task.checklist.filter((c) => c.done).length
  const finished = task.status === 'done' || task.status === 'cancelled'
  return (
    <div className={`lrow${cursor ? ' cursor' : ''}${picked ? ' picked' : ''}`} onClick={onOpen} role="row" aria-selected={cursor}>
      {selectable && <input type="checkbox" className="pick" checked={picked} onChange={onPick} onClick={(e) => e.stopPropagation()} aria-label="选中" />}
      <TaskBox task={task} />
      <span className={`title${finished ? ' done' : ''}`}>{task.title}</span>
      {task.status === 'waiting' && task.waitingFor && <span className="sub">等 {task.waitingFor}</span>}
      {checks > 0 && (
        <span className="when">
          {done}/{checks}
        </span>
      )}
      {(task.status === 'doing' || task.status === 'waiting') && <Tag tone={taskStatusLabel[task.status].tone}>{taskStatusLabel[task.status].text}</Tag>}
      {project && <span className="sub">{project.name}</span>}
      {w.text && <span className={`when${w.late && !finished ? ' late' : ''}`}>{w.text}</span>}
    </div>
  )
}

export function TaskList({ groups, empty }: { groups: TaskGroup[]; empty: string }) {
  const { state, dispatch } = useStore()
  const { setSelected } = useWorkspace()
  const navigate = useNavigate()
  const toast = useToast()
  const ids = groups.flatMap((g) => g.tasks.map((t) => t.id))

  const onKey = useCallback(
    (key: string, targets: string[]) => {
      if (key === 'c') {
        dispatch({ type: 'bulkStatus', ids: targets, status: 'done' })
        toast.show(`完成 ${targets.length} 件`)
        return true
      }
      if (key === 't') {
        dispatch({ type: 'bulkDefer', ids: targets, days: 1 })
        toast.show(`${targets.length} 件推到明天`)
        return true
      }
      if (key === 's') {
        for (const id of targets) {
          const t = state.tasks.find((x) => x.id === id)
          if (t) dispatch({ type: 'setTaskStatus', id, status: cycle[t.status] })
        }
        return true
      }
      return false
    },
    [dispatch, state.tasks, toast],
  )
  const onOpen = useCallback((id: string) => navigate(`/t/${id}`), [navigate])
  const nav = useListNav(ids, { onOpen, onKey, onCursor: setSelected })

  if (ids.length === 0) return <Empty>{empty}</Empty>

  return (
    <div className={nav.picked.length ? 'picking' : undefined}>
      {nav.picked.length > 0 && (
        <div className="bulkbar">
          <span className="grow">已选 {nav.picked.length} 件</span>
          <Button size="sm" onClick={() => (onKey('c', nav.picked), nav.clear())}>
            完成 <kbd>C</kbd>
          </Button>
          <Button size="sm" onClick={() => (onKey('t', nav.picked), nav.clear())}>
            推到明天 <kbd>T</kbd>
          </Button>
          <select
            aria-label="改状态"
            value=""
            onChange={(e) => {
              dispatch({ type: 'bulkStatus', ids: nav.picked, status: e.target.value as TaskStatus })
              nav.clear()
            }}
          >
            <option value="">改状态…</option>
            {(['doing', 'todo', 'waiting', 'cancelled'] as TaskStatus[]).map((s) => (
              <option key={s} value={s}>
                {taskStatusLabel[s].text}
              </option>
            ))}
          </select>
          <select
            aria-label="移到项目"
            value=""
            onChange={(e) => {
              dispatch({ type: 'bulkMove', ids: nav.picked, projectId: e.target.value === '-' ? undefined : e.target.value })
              nav.clear()
            }}
          >
            <option value="">移到项目…</option>
            {state.projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
            <option value="-">不属于项目</option>
          </select>
          <Button size="sm" onClick={nav.clear}>
            取消 <kbd>Esc</kbd>
          </Button>
        </div>
      )}
      {groups.map((g) => (
        <div key={g.key} className="group">
          <div className={`group-head${g.tone === 'danger' ? ' danger' : ''}`}>
            {g.label}
            <span className="n">{g.tasks.length}</span>
          </div>
          {g.tasks.map((t) => (
            <TaskRow
              key={t.id}
              task={t}
              cursor={nav.cursor === t.id}
              picked={nav.picked.includes(t.id)}
              onPick={() => nav.toggle(t.id)}
              onOpen={() => {
                nav.setCursor(t.id)
                navigate(`/t/${t.id}`)
              }}
            />
          ))}
        </div>
      ))}
      <div className="keys">
        <span>
          <kbd>J</kbd>
          <kbd>K</kbd> 移动
        </span>
        <span>
          <kbd>↵</kbd> 打开
        </span>
        <span>
          <kbd>X</kbd> 多选
        </span>
        <span>
          <kbd>C</kbd> 完成
        </span>
        <span>
          <kbd>T</kbd> 推到明天
        </span>
        <span>
          <kbd>S</kbd> 切换状态
        </span>
      </div>
    </div>
  )
}
