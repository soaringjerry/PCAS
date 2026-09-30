import { isOpenTask, type Thing } from './things'
import { dayOffset, formatWhen } from './time'
import type { Run, State, Task } from './types'

// The two lines on the home screen (docs/design/principles.md):
// urgent things move by time, ongoing things move by events.

export interface LineItem {
  thing: Thing
  reason: string
  tone: 'late' | 'today' | 'owed' | 'soon' | 'follow' | 'ready' | 'working' | 'wake' | 'doing' | 'waiting' | 'idle' | 'parked'
  /** An AI result waiting to be looked at. */
  result?: Run
  rank: number
  at: string
}

const DAY = 24 * 60 * 60 * 1000
export function spentToday(state: State): number { return state.budgetUsage }

function daysSince(iso: string): number {
  return Math.max(1, Math.floor((Date.now() - new Date(iso).getTime()) / DAY))
}

function followUpAt(task: Task): string | undefined {
  return task.triggers.find((t) => t.active && t.nextAt)?.nextAt
}

function hhmm(iso: string): string {
  return new Date(iso).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}

/** Why a task is urgent, or undefined when it is not. */
function urgency(task: Task): Pick<LineItem, 'reason' | 'tone' | 'rank'> | undefined {
  if (!isOpenTask(task)) return undefined
  if (task.status === 'waiting') {
    const at = followUpAt(task)
    return at && dayOffset(at) <= 0 ? { reason: `该跟进了：${task.waitingFor ?? '对方还没回'}`, tone: 'follow', rank: 1.5 } : undefined
  }
  const due = task.due
  if (due && new Date(due).getTime() < Date.now()) return { reason: `已过截止 · ${formatWhen(due)}`, tone: 'late', rank: 0 }
  if (due && dayOffset(due) === 0) {
    return { reason: `今天 ${hhmm(due)} 截止${task.owedTo ? ` · ${task.owedTo.who}在等你` : ''}`, tone: 'today', rank: 1 }
  }
  if (task.owedTo) {
    const days = daysSince(task.owedTo.since)
    return { reason: `${task.owedTo.who}在等你 · ${days} 天了`, tone: 'owed', rank: 2 + 1 / (days + 1) }
  }
  if (due && dayOffset(due) <= 3) return { reason: `${formatWhen(due).replace(/ \d\d:\d\d$/, '')}截止`, tone: 'soon', rank: 3 + dayOffset(due) / 10 }
  return undefined
}

function latestResult(state: State, thingId: string): Run | undefined {
  return state.runs.find((r) => r.thingId === thingId && r.status === 'done' && !r.adopted)
}

function running(state: State, thingId: string): boolean {
  return state.runs.some((r) => r.thingId === thingId && r.status === 'running')
}

export function urgentLine(state: State): LineItem[] {
  const items: LineItem[] = []
  for (const task of state.tasks) {
    const u = urgency(task)
    if (!u) continue
    const result = latestResult(state, task.id)
    items.push({
      thing: { kind: 'task', id: task.id, item: task },
      ...u,
      result,
      at: task.updatedAt,
    })
  }
  return items.sort((a, b) => a.rank - b.rank)
}

function short(text: string, n = 36): string {
  const t = text.replace(/\s+/g, ' ').trim()
  return t.length > n ? `${t.slice(0, n)}…` : t
}

export function ongoingLine(state: State): { active: LineItem[]; parked: LineItem[] } {
  const active: LineItem[] = []
  const parked: LineItem[] = []

  const withProgress = (thing: Thing, base: Omit<LineItem, 'thing' | 'at'>, at: string): LineItem => {
    const result = latestResult(state, thing.id)
    if (result) return { thing, reason: `副手做好了：${short(result.prompt, 24)}，等你看`, tone: 'ready', result, rank: 0, at }
    if (running(state, thing.id)) return { thing, reason: '副手正在推进…', tone: 'working', rank: 0.5, at }
    return { thing, ...base, at }
  }

  for (const task of state.tasks) {
    if (!isOpenTask(task) || urgency(task)) continue
    const thing: Thing = { kind: 'task', id: task.id, item: task }
    const nextCheck = task.checklist.find((c) => !c.done)
    if (task.status === 'waiting') {
      const at = followUpAt(task)
      active.push(withProgress(thing, { reason: `等${task.waitingFor ?? '对方回复'}${at ? ` · ${formatWhen(at).replace(/ \d\d:\d\d$/, '')}跟进` : ''}`, tone: 'waiting', rank: 4 }, task.updatedAt))
    } else if (task.status === 'doing') {
      active.push(withProgress(thing, { reason: nextCheck ? `下一步：${nextCheck.text}` : '正在做', tone: 'doing', rank: 2 }, task.updatedAt))
    } else {
      active.push(
        withProgress(
          thing,
          { reason: nextCheck ? `下一步：${nextCheck.text}` : task.notes ? short(task.notes) : '还没开始', tone: 'idle', rank: 3 },
          task.updatedAt,
        ),
      )
    }
  }

  for (const idea of state.ideas) {
    const thing: Thing = { kind: 'idea', id: idea.id, item: idea }
    if (idea.status === 'awakened') {
      active.push(withProgress(thing, { reason: `✦ ${short(idea.wake?.reason ?? '条件满足了', 40)}`, tone: 'wake', rank: 1 }, idea.updatedAt))
    } else if (idea.status === 'active') {
      active.push(withProgress(thing, { reason: `想法 · ${short(idea.body || '还没展开')}`, tone: 'idle', rank: 3 }, idea.updatedAt))
    } else if (idea.status === 'shelved') {
      const waiting = idea.conditions.find((c) => !c.met)
      parked.push({ thing, reason: waiting ? `等：${waiting.description}` : (idea.shelvedReason ?? '放着'), tone: 'parked', rank: 9, at: idea.updatedAt })
    }
  }

  // Fresh progress first; within a rank, whatever moved most recently.
  active.sort((a, b) => a.rank - b.rank || b.at.localeCompare(a.at))
  return { active, parked }
}

/** Candidates and AI guesses the background could not decide on. */
export function unsure(state: State) {
  return {
    candidates: state.candidates.filter((c) => c.state === 'pending'),
    guesses: state.memories.filter((m) => m.epistemic === 'inferred' && Date.now() - new Date(m.versions[0].at).getTime() < 7 * DAY),
  }
}
