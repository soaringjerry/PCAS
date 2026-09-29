import { memoryKindLabel } from './labels'
import type { Agent, Handoff, HandoffSections, Idea, Memory, State, Task } from './types'

/** Memories an agent is allowed to see, relevant to a project. */
export function memoriesFor(state: State, agent: Agent, projectId?: string): Memory[] {
  return state.memories.filter(
    (m) =>
      agent.memoryKinds.includes(m.kind) &&
      (agent.includeInferred || m.epistemic !== 'inferred') &&
      (!projectId || !m.projectId || m.projectId === projectId),
  )
}

interface Target {
  projectId?: string
  task?: Task
  idea?: Idea
}

/** Assemble a first draft from the current state; the user edits it before sending. */
export function draftSections(state: State, target: Target, memories: Memory[]): HandoffSections {
  const project = state.projects.find((p) => p.id === target.projectId)
  const decisions = memories.filter((m) => m.kind === 'decision' && m.epistemic === 'confirmed')
  const openTasks = state.tasks.filter(
    (t) =>
      t.projectId === target.projectId && t.id !== target.task?.id && (t.status === 'doing' || t.status === 'todo'),
  )

  const goal = target.task
    ? target.task.title
    : target.idea
      ? `评估并推进这个想法：${target.idea.title}`
      : (project?.goal ?? '')

  const background = [
    project ? `项目「${project.name}」：${project.goal}` : '',
    target.idea ? `${target.idea.body}${target.idea.shelvedReason ? `\n此前搁置的原因：${target.idea.shelvedReason}` : ''}` : '',
    target.idea?.wake ? `重新唤醒的原因：${target.idea.wake.reason}` : '',
    target.task?.notes && target.task.notes !== target.idea?.body ? target.task.notes : '',
  ]
    .filter(Boolean)
    .join('\n')

  const progress = [
    project?.progress ?? '',
    openTasks.length ? `进行中的事项：${openTasks.map((t) => t.title).join('；')}` : '',
  ]
    .filter(Boolean)
    .join('\n')

  return {
    goal,
    background,
    progress,
    decisions: decisions.map((m) => `- ${m.text}`).join('\n'),
    constraints: '只使用这里提供的信息；不确定的地方直接说明，不要猜测。',
    expectedOutput: target.idea ? '一份可执行的方案：步骤、成本估算和主要风险。' : '完成目标所需的具体产出。',
  }
}

const sectionTitles: [keyof HandoffSections, string][] = [
  ['goal', '目标'],
  ['background', '背景'],
  ['progress', '当前进度'],
  ['decisions', '已确定的决定'],
  ['constraints', '约束'],
  ['expectedOutput', '预期输出'],
]

/** The text the target AI actually receives. */
export function renderHandoff(handoff: Handoff, memories: Memory[]): string {
  const parts = [`# ${handoff.title}`]
  for (const [key, title] of sectionTitles) {
    const body = handoff.sections[key].trim()
    if (body) parts.push(`## ${title}\n${body}`)
  }
  const shared = memories.filter((m) => handoff.memoryIds.includes(m.id))
  if (shared.length) {
    parts.push(`## 相关记忆\n${shared.map((m) => `- [${memoryKindLabel[m.kind]}] ${m.text}`).join('\n')}`)
  }
  return parts.join('\n\n')
}

export { sectionTitles }
