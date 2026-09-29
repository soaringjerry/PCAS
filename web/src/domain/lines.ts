import { buildBrief, contextFor } from './agent'
import { isOpenTask, type Thing } from './things'
import { dayOffset, formatWhen } from './time'
import type { Idea, Run, RunKind, State, Task } from './types'

// The two lines on the home screen (docs/design/principles.md):
// urgent things move by time, ongoing things move by events.

export interface NextStep {
  kind: RunKind
  label: string
  prompt: string
  cost: number
}

export interface LineItem {
  thing: Thing
  reason: string
  tone: 'late' | 'today' | 'owed' | 'soon' | 'follow' | 'ready' | 'working' | 'wake' | 'doing' | 'waiting' | 'idle' | 'parked'
  next?: NextStep
  /** An AI result waiting to be looked at. */
  result?: Run
  rank: number
  at: string
}

const DAY = 24 * 60 * 60 * 1000
const DEFAULT_AGENT = 'a_claude'

/** Rough CNY estimate: about one token per Chinese character plus the answer. */
export function estimateCost(briefChars: number): number {
  return Math.max(0.01, Math.round(((briefChars + 1200) / 1000) * 0.02 * 100) / 100)
}

export function spentToday(state: State): number {
  const today = new Date().toDateString()
  return state.runs.filter((r) => new Date(r.createdAt).toDateString() === today).reduce((sum, r) => sum + (r.cost ?? 0), 0)
}

function step(state: State, thing: Thing, kind: RunKind, label: string, prompt: string): NextStep {
  const agent = state.agents.find((a) => a.id === DEFAULT_AGENT) ?? state.agents[0]
  const memories = contextFor(state, thing, agent).filter((c) => c.included).map((c) => c.memory)
  return { kind, label, prompt, cost: estimateCost(buildBrief(state, thing, prompt, memories).length) }
}

function taskStep(state: State, task: Task): NextStep {
  const thing: Thing = { kind: 'task', id: task.id, item: task }
  if (task.owedTo || /回复|邮件|答复/.test(task.title)) return step(state, thing, 'draft', '起草回复', '起草这件事需要的回复')
  if (task.checklist.length === 0) return step(state, thing, 'breakdown', '拆步骤', '把这件事拆成具体的子任务')
  const hasPlan = state.docs.some((d) => d.thingId === task.id && d.by === 'ai')
  return hasPlan
    ? step(state, thing, 'summary', '整理进度', '总结这件事目前的进度和卡点')
    : step(state, thing, 'plan', '写方案', '为这件事写一份可执行的方案')
}

function ideaStep(state: State, idea: Idea): NextStep {
  return step(state, { kind: 'idea', id: idea.id, item: idea }, 'plan', '评估一下', '评估这个想法现在是否值得做，给出方案')
}

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
  return state.runs.filter((r) => r.thingId === thingId && r.status === 'done' && !r.adopted).at(-1)
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
      next: result || running(state, task.id) ? undefined : taskStep(state, task),
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
      active.push(withProgress(thing, { reason: nextCheck ? `下一步：${nextCheck.text}` : '正在做', tone: 'doing', rank: 2, next: taskStep(state, task) }, task.updatedAt))
    } else {
      active.push(
        withProgress(
          thing,
          { reason: nextCheck ? `下一步：${nextCheck.text}` : task.notes ? short(task.notes) : '还没开始', tone: 'idle', rank: 3, next: taskStep(state, task) },
          task.updatedAt,
        ),
      )
    }
  }

  for (const idea of state.ideas) {
    const thing: Thing = { kind: 'idea', id: idea.id, item: idea }
    if (idea.status === 'awakened') {
      active.push(withProgress(thing, { reason: `✦ ${short(idea.wake?.reason ?? '条件满足了', 40)}`, tone: 'wake', rank: 1, next: ideaStep(state, idea) }, idea.updatedAt))
    } else if (idea.status === 'active') {
      active.push(withProgress(thing, { reason: `想法 · ${short(idea.body || '还没展开')}`, tone: 'idle', rank: 3, next: ideaStep(state, idea) }, idea.updatedAt))
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
