import { memoryKindLabel } from './labels'
import { isOpenTask, thingTitle, type Thing } from './things'
import type { Agent, Memory, RunKind, State } from './types'

// Client preview of context. The server independently enforces grants and
// assembles the authoritative versioned brief at execution time.

export const quickActions: Record<Thing['kind'], { kind: RunKind; label: string; prompt: string }[]> = {
  task: [
    { kind: 'plan', label: '写方案', prompt: '为这件事写一份可执行的方案' },
    { kind: 'breakdown', label: '拆成子任务', prompt: '把这件事拆成具体的子任务' },
    { kind: 'draft', label: '起草', prompt: '起草这件事需要的文字（邮件、说明或文档）' },
    { kind: 'summary', label: '总结进度', prompt: '总结这件事目前的进度和卡点' },
  ],
  idea: [
    { kind: 'plan', label: '评估可行性', prompt: '评估这个想法现在是否值得做，给出方案' },
    { kind: 'breakdown', label: '拆成步骤', prompt: '如果要做，拆成几个待办' },
  ],
  project: [
    { kind: 'summary', label: '总结进度', prompt: '总结项目目前的进度、卡点和下一步' },
    { kind: 'breakdown', label: '规划下一步', prompt: '根据现状规划接下来要做的几件事' },
    { kind: 'plan', label: '写方案', prompt: '为项目接下来的阶段写一份方案' },
  ],
}

export function memoriesFor(state: State, agent: Agent, projectId?: string): Memory[] {
  return state.memories.filter(
    (m) =>
      m.visibleTo.includes(agent.id) &&
      agent.memoryKinds.includes(m.kind) &&
      (agent.includeInferred || m.epistemic !== 'inferred') &&
      (!projectId || !m.projectId || m.projectId === projectId),
  )
}

function projectOf(thing: Thing): string | undefined {
  return thing.kind === 'project' ? thing.id : thing.item.projectId
}

/** Memories relevant to a thing, each marked with whether the agent may see it. */
export function contextFor(state: State, thing: Thing, agent: Agent) {
  const projectId = projectOf(thing)
  const allowed = new Set(memoriesFor(state, agent, projectId).map((m) => m.id))
  const excluded = new Set(state.excludedMemories[thing.id] ?? [])
  const relevant = state.memories
    .filter((m) => (projectId ? m.projectId === projectId || !m.projectId : !m.projectId))
    .sort((a, b) => b.exposure - a.exposure)
  return relevant.map((m) => {
    const kindBlocked = !agent.memoryKinds.includes(m.kind)
    const unconfirmed = !kindBlocked && m.epistemic === 'inferred' && !agent.includeInferred
    return { memory: m, allowed: allowed.has(m.id), included: allowed.has(m.id) && !excluded.has(m.id), kindBlocked, unconfirmed }
  })
}

export function sourcesFor(state: State, thing: Thing) {
  const seen = new Set<string>()
  return collectSources(state, thing).filter((s) => {
    const key = `${s.label}|${s.at}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function collectSources(state: State, thing: Thing) {
  if (thing.kind === 'project') {
    return state.tasks.filter((t) => t.projectId === thing.id).flatMap((t) => t.sources)
  }
  const own = thing.item.sources
  if (thing.kind === 'task' && thing.item.ideaId) {
    const idea = state.ideas.find((i) => i.id === (thing.item as { ideaId?: string }).ideaId)
    const met = idea?.conditions.flatMap((c) => (c.metBy ? [c.metBy] : [])) ?? []
    return [...own, ...(idea?.sources ?? []), ...met]
  }
  if (thing.kind === 'idea') return [...own, ...thing.item.conditions.flatMap((c) => (c.metBy ? [c.metBy] : []))]
  return own
}

function background(state: State, thing: Thing): string[] {
  const project = state.projects.find((p) => p.id === projectOf(thing))
  const lines: string[] = []
  if (project && thing.kind !== 'project') lines.push(`所属项目「${project.name}」：${project.goal}`)
  if (thing.kind === 'project') lines.push(thing.item.goal)
  if (thing.kind === 'task') {
    if (thing.item.notes) lines.push(thing.item.notes)
    const idea = state.ideas.find((i) => i.id === thing.item.ideaId)
    if (idea?.shelvedReason) lines.push(`它来自一个曾被搁置的想法，原因：${idea.shelvedReason}`)
    if (idea?.wake) lines.push(`重新拿起它的原因：${idea.wake.reason}`)
  }
  if (thing.kind === 'idea') {
    lines.push(thing.item.body)
    if (thing.item.shelvedReason) lines.push(`搁置原因：${thing.item.shelvedReason}`)
    if (thing.item.wake) lines.push(`重新唤醒的原因：${thing.item.wake.reason}`)
  }
  return lines.filter(Boolean)
}

function progress(state: State, thing: Thing): string[] {
  if (thing.kind === 'project') {
    const open = state.tasks.filter((t) => t.projectId === thing.id && isOpenTask(t))
    return [thing.item.progress, open.length ? `进行中的事：${open.map((t) => t.title).join('；')}` : ''].filter(Boolean)
  }
  if (thing.kind === 'task') {
    const done = thing.item.checklist.filter((c) => c.done).map((c) => c.text)
    const todo = thing.item.checklist.filter((c) => !c.done).map((c) => c.text)
    return [done.length ? `已完成：${done.join('；')}` : '', todo.length ? `未完成：${todo.join('；')}` : ''].filter(Boolean)
  }
  return []
}

/** The exact text the agent receives. */
export function buildBrief(state: State, thing: Thing, prompt: string, memories: Memory[]): string {
  const parts = [`# 任务\n${prompt}`, `## 这件事\n${thingTitle(thing)}`]
  const bg = background(state, thing)
  if (bg.length) parts.push(`## 背景\n${bg.join('\n')}`)
  const pr = progress(state, thing)
  if (pr.length) parts.push(`## 当前进度\n${pr.join('\n')}`)
  const decisions = memories.filter((m) => m.kind === 'decision' && m.epistemic === 'confirmed')
  if (decisions.length) parts.push(`## 已确定的决定\n${decisions.map((m) => `- ${m.text}`).join('\n')}`)
  const others = memories.filter((m) => !decisions.includes(m))
  if (others.length) parts.push(`## 相关记忆\n${others.map((m) => `- [${memoryKindLabel[m.kind]}] ${m.text}`).join('\n')}`)
  parts.push('## 要求\n只使用这里提供的信息；不确定的地方直接说明，不要猜测。')
  return parts.join('\n\n')
}

export function parseChecklist(output: string): string[] {
  return output
    .split('\n')
    .map((l) => l.match(/^\s*- \[[ x]\]\s+(.+)$/)?.[1]?.trim())
    .filter((l): l is string => Boolean(l))
}
