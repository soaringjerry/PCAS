import { attentionFor, isDecision } from './attention'
import { findThing, isOpenTask, thingTitle, type Thing } from './things'
import { dayOffset } from './time'
import type { Idea, State, Task } from './types'

export const viewPaths = ['/today', '/inbox', '/upcoming', '/ideas', '/library', '/settings'] as const

export const viewTitles: Record<string, string> = {
  '/today': '今天',
  '/inbox': '收件',
  '/upcoming': '接下来',
  '/ideas': '想法',
  '/library': '资料库',
  '/settings': '设置',
}

export interface TabInfo {
  title: string
  kind?: Thing['kind']
}

/** Title for a tab path, or undefined when it no longer points at anything. */
export function tabInfo(state: State, path: string): TabInfo | undefined {
  if (viewTitles[path]) return { title: viewTitles[path] }
  const m = path.match(/^\/t\/(.+)$/)
  if (m) {
    const thing = findThing(state, m[1])
    return thing ? { title: thingTitle(thing), kind: thing.kind } : undefined
  }
  return undefined
}

function dueOffset(t: Task): number | undefined {
  const d = t.due ?? t.scheduled
  return d ? dayOffset(d) : undefined
}

export interface TaskGroup {
  key: string
  label: string
  tone?: 'danger'
  tasks: Task[]
}

const byWhen = (a: Task, b: Task) => (a.due ?? a.scheduled ?? '9').localeCompare(b.due ?? b.scheduled ?? '9')

export function todayGroups(state: State): TaskGroup[] {
  const open = state.tasks.filter(isOpenTask)
  const overdue = open.filter((t) => t.status !== 'waiting' && t.due && dayOffset(t.due) < 0)
  const today = open.filter((t) => t.status !== 'waiting' && !overdue.includes(t) && (dueOffset(t) ?? 1) <= 0)
  const doing = open.filter((t) => t.status === 'doing' && !overdue.includes(t) && !today.includes(t))
  const waiting = open.filter((t) => t.status === 'waiting')
  return [
    { key: 'overdue', label: '逾期', tone: 'danger' as const, tasks: overdue.sort(byWhen) },
    { key: 'today', label: '今天', tasks: today.sort(byWhen) },
    { key: 'doing', label: '正在做', tasks: doing },
    { key: 'waiting', label: '等待中', tasks: waiting },
  ].filter((g) => g.tasks.length)
}

export function upcomingGroups(state: State): TaskGroup[] {
  const open = state.tasks.filter((t) => isOpenTask(t) && t.status !== 'waiting')
  const dated = open.filter((t) => (dueOffset(t) ?? 0) > 0).sort(byWhen)
  const groups = new Map<string, Task[]>()
  for (const t of dated) {
    const d = new Date(t.due ?? t.scheduled!)
    const off = dayOffset(d.toISOString())
    const label =
      off === 1 ? '明天' : `${d.getMonth() + 1}月${d.getDate()}日 ${d.toLocaleDateString('zh-CN', { weekday: 'short' })}`
    groups.set(label, [...(groups.get(label) ?? []), t])
  }
  const undated = open.filter((t) => !t.due && !t.scheduled && t.status !== 'doing')
  return [
    ...[...groups.entries()].map(([label, tasks]) => ({ key: label, label, tasks })),
    ...(undated.length ? [{ key: 'undated', label: '还没排期', tasks: undated }] : []),
  ]
}

export interface IdeaGroup {
  key: string
  label: string
  ideas: Idea[]
}

export function ideaGroups(state: State): IdeaGroup[] {
  const by = (status: Idea['status']) => state.ideas.filter((i) => i.status === status)
  return [
    { key: 'awakened', label: '刚回来', ideas: by('awakened') },
    { key: 'active', label: '进行中', ideas: by('active') },
    { key: 'shelved', label: '放着的', ideas: by('shelved') },
    { key: 'closed', label: '已转成待办或放弃', ideas: [...by('promoted'), ...by('dropped')] },
  ].filter((g) => g.ideas.length)
}

export function inboxCount(state: State): number {
  return attentionFor(state).filter((a) => !isDecision(a)).length
}

export function todayCount(state: State): number {
  const decisions = attentionFor(state).filter((a) => isDecision(a) && a.kind !== 'due').length
  return decisions + todayGroups(state).filter((g) => g.key !== 'waiting').reduce((n, g) => n + g.tasks.length, 0)
}
