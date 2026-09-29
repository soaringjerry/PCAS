import { Check, CornerDownRight, X } from 'lucide-react'
import { epistemicLabel, taskStatusLabel } from '../domain/labels'
import { formatAgo, formatWhen, isOverdue } from '../domain/time'
import type { Epistemic, Revision, SourceRef, Task } from '../domain/types'
import { useStore } from '../store/context'
import { Badge } from './ui'

export function EpistemicBadge({ value }: { value: Epistemic }) {
  const label = epistemicLabel[value]
  return <Badge tone={label.tone}>{label.text}</Badge>
}

export function ProjectName({ id }: { id?: string }) {
  const { state } = useStore()
  const project = state.projects.find((p) => p.id === id)
  if (!project) return null
  return <span className="chip">{project.name}</span>
}

export function SourceLine({ source }: { source: SourceRef }) {
  return (
    <div className="stack-sm" style={{ gap: 4 }}>
      <div className="chip">
        <CornerDownRight size={13} />
        <span>{source.label}</span>
        <span>· {formatAgo(source.at)}</span>
      </div>
      {source.excerpt && <div className="quote">“{source.excerpt}”</div>}
    </div>
  )
}

const actorText: Record<Revision['by'], string> = {
  user: '你',
  ai: 'AI',
  import: '导入',
  system: '系统',
}

export function Timeline({ items }: { items: Revision[] }) {
  return (
    <ul className="timeline">
      {items.map((item, index) => (
        <li key={`${item.at}-${index}`} className={index === items.length - 1 ? 'current' : undefined}>
          <div className="ink">{item.summary}</div>
          <div className="small muted">
            {actorText[item.by]} · {formatAgo(item.at)}
          </div>
        </li>
      ))}
    </ul>
  )
}

export function StatusDot({ task }: { task: Task }) {
  const { dispatch } = useStore()
  const next = task.status === 'done' ? 'todo' : 'done'
  return (
    <button
      type="button"
      className={`status-dot ${task.status}`}
      aria-label={task.status === 'done' ? '标记为未完成' : '标记为完成'}
      title={taskStatusLabel[task.status].text}
      onClick={(e) => {
        e.stopPropagation()
        if (task.status !== 'cancelled') dispatch({ type: 'setTaskStatus', id: task.id, status: next })
      }}
    >
      {task.status === 'done' && <Check size={12} strokeWidth={3} />}
      {task.status === 'cancelled' && <X size={11} strokeWidth={3} />}
    </button>
  )
}

export function TaskMeta({ task, showProject = true }: { task: Task; showProject?: boolean }) {
  const { state } = useStore()
  const open = task.status !== 'done' && task.status !== 'cancelled'
  const blockers = task.dependsOn
    .map((id) => state.tasks.find((t) => t.id === id))
    .filter((t) => t && t.status !== 'done' && t.status !== 'cancelled')
  return (
    <div className="meta">
      {task.status !== 'todo' && <Badge tone={taskStatusLabel[task.status].tone}>{taskStatusLabel[task.status].text}</Badge>}
      {task.due && (
        <span className={open && isOverdue(task.due) ? 'overdue' : undefined}>截止 {formatWhen(task.due)}</span>
      )}
      {task.scheduled && <span>安排在 {formatWhen(task.scheduled)}</span>}
      {task.waitingFor && task.status === 'waiting' && <span>等待：{task.waitingFor}</span>}
      {open && blockers.length > 0 && <span>依赖 {blockers.length} 项未完成</span>}
      {task.triggers.some((t) => t.active) && <span>有提醒</span>}
      {showProject && <ProjectName id={task.projectId} />}
    </div>
  )
}

export function TaskRow({ task, onOpen, showProject }: { task: Task; onOpen: () => void; showProject?: boolean }) {
  const finished = task.status === 'done' || task.status === 'cancelled'
  return (
    <div className="list-item clickable" onClick={onOpen}>
      <StatusDot task={task} />
      <div className="grow">
        <div className={`item-title${finished ? ' done' : ''}`}>{task.title}</div>
        <TaskMeta task={task} showProject={showProject} />
      </div>
    </div>
  )
}
