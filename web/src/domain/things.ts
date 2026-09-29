import { ideaStatusLabel, projectStatusLabel, taskStatusLabel } from './labels'
import type { Idea, Project, SourceRef, State, Task } from './types'

// The UI treats projects, ideas and tasks as one thing ("事情") whose kind can
// change: an idea becomes a task, tasks gather under a project.

export type Thing =
  | { kind: 'project'; id: string; item: Project }
  | { kind: 'idea'; id: string; item: Idea }
  | { kind: 'task'; id: string; item: Task }

export const kindText = { project: '项目', idea: '想法', task: '待办' } as const

export function findThing(state: State, id: string): Thing | undefined {
  const task = state.tasks.find((t) => t.id === id)
  if (task) return { kind: 'task', id, item: task }
  const idea = state.ideas.find((i) => i.id === id)
  if (idea) return { kind: 'idea', id, item: idea }
  const project = state.projects.find((p) => p.id === id)
  if (project) return { kind: 'project', id, item: project }
  return undefined
}

export function thingTitle(thing: Thing): string {
  return thing.kind === 'project' ? thing.item.name : thing.item.title
}

export function thingStatus(thing: Thing) {
  if (thing.kind === 'project') return projectStatusLabel[thing.item.status]
  if (thing.kind === 'idea') return ideaStatusLabel[thing.item.status]
  return taskStatusLabel[thing.item.status]
}

export function thingProjectId(thing: Thing): string | undefined {
  return thing.kind === 'project' ? undefined : thing.item.projectId
}

export function isOpenTask(t: Task): boolean {
  return t.status !== 'done' && t.status !== 'cancelled'
}

export function isLiveIdea(i: Idea): boolean {
  return i.status === 'active' || i.status === 'awakened' || i.status === 'shelved'
}

export function allThings(state: State): Thing[] {
  return [
    ...state.projects.map((item) => ({ kind: 'project' as const, id: item.id, item })),
    ...state.ideas.map((item) => ({ kind: 'idea' as const, id: item.id, item })),
    ...state.tasks.map((item) => ({ kind: 'task' as const, id: item.id, item })),
  ]
}

export function thingUpdatedAt(thing: Thing): string {
  return thing.item.updatedAt
}

export interface TimelineEvent {
  at: string
  kind: 'note' | 'source' | 'wake' | 'handoff' | 'done' | 'decision'
  text: string
  by?: string
  source?: SourceRef
  link?: string
}

const actor = { user: '你', ai: 'AI', import: '导入', system: '系统' } as const

/** Everything that happened to a thing, oldest first: its 来龙去脉. */
export function timelineFor(state: State, thing: Thing): TimelineEvent[] {
  const events: TimelineEvent[] = []
  const agentName = (id: string) => state.agents.find((a) => a.id === id)?.name ?? 'AI'

  for (const r of state.runs.filter((r) => r.thingId === thing.id)) {
    events.push({ at: r.createdAt, kind: 'handoff', text: `交给 ${agentName(r.agentId)}：${r.prompt}` })
    if (r.finishedAt) events.push({ at: r.finishedAt, kind: 'handoff', text: `${agentName(r.agentId)} 的结果${r.adopted ? '已采纳' : '回来了'}` })
  }
  for (const d of state.docs.filter((d) => d.thingId === thing.id)) {
    events.push({ at: d.createdAt, kind: 'note', text: `${d.by === 'ai' ? 'AI 写了' : '新建'}文档「${d.title}」` })
  }

  if (thing.kind === 'task') {
    for (const r of thing.item.history) {
      events.push({ at: r.at, kind: r.summary.startsWith('状态改为完成') ? 'done' : 'note', text: r.summary, by: actor[r.by] })
    }
    for (const s of thing.item.sources) events.push({ at: s.at, kind: 'source', text: s.label, source: s })
  }

  if (thing.kind === 'idea') {
    for (const r of thing.item.evolution) {
      events.push({ at: r.at, kind: r.by === 'system' ? 'wake' : 'note', text: r.summary, by: actor[r.by] })
    }
    for (const s of thing.item.sources) events.push({ at: s.at, kind: 'source', text: s.label, source: s })
    for (const c of thing.item.conditions) {
      if (c.metBy) events.push({ at: c.metBy.at, kind: 'source', text: `这份资料满足了条件：${c.description}`, source: c.metBy })
    }
  }

  if (thing.kind === 'project') {
    for (const m of state.memories.filter((m) => m.projectId === thing.id && m.kind === 'decision')) {
      m.versions.forEach((v, i) =>
        events.push({
          at: v.at,
          kind: 'decision',
          text: i === 0 ? `${v.by === 'ai' ? 'AI 提议' : '定下'}：${v.text}` : `改成：${v.text}${v.reason ? `（${v.reason}）` : ''}`,
          by: actor[v.by],
        }),
      )
    }
    for (const t of state.tasks.filter((t) => t.projectId === thing.id)) {
      events.push({ at: t.createdAt, kind: 'note', text: `新增待办：${t.title}`, link: `/t/${t.id}` })
      if (t.status === 'done') events.push({ at: t.updatedAt, kind: 'done', text: `完成：${t.title}`, link: `/t/${t.id}` })
    }
  }

  return events.sort((a, b) => a.at.localeCompare(b.at))
}
