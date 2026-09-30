import { isOpenTask } from './things'
import { dayOffset, formatWhen } from './time'
import type { Idea, Project, Run, State, Task } from './types'

// The home screen is a service hall (docs/design/principles.md): today on the
// left, projects and ideas on the right, one desk in the middle. These
// selectors decide what each wall shows; none of them spend money.

const HOUR = 60 * 60 * 1000
const DAY = 24 * HOUR

function hhmm(iso: string): string {
  return new Date(iso).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}

function daysSince(iso: string): number {
  return Math.max(1, Math.floor((Date.now() - new Date(iso).getTime()) / DAY))
}

export interface TodayRow {
  task: Task
  /** Why it is here, in the user's words. */
  note: string
  /** Clock time for the timeline, when it has one today. */
  time?: string
  at?: string
  past?: boolean
}

export interface TodayColumn {
  /** Someone is waiting, a follow-up is due, or it is already late. */
  waiting: TodayRow[]
  /** Due or scheduled today, in clock order. */
  timeline: TodayRow[]
  /** Due within the next three days. Anything later stays out until it is close. */
  soon: TodayRow[]
}

function followUpAt(task: Task): string | undefined {
  return task.triggers.find((t) => t.active && t.nextAt)?.nextAt
}

export function todayColumn(state: State): TodayColumn {
  const waiting: TodayRow[] = []
  const timeline: TodayRow[] = []
  const soon: TodayRow[] = []
  const now = Date.now()

  for (const task of state.tasks) {
    if (!isOpenTask(task)) continue
    const due = task.due
    const follow = task.status === 'waiting' ? followUpAt(task) : undefined

    if (follow && dayOffset(follow) <= 0) {
      waiting.push({ task, note: `该跟进了：${task.waitingFor ?? '对方还没回'}` })
      continue
    }
    if (task.status === 'waiting') continue
    if (due && new Date(due).getTime() < now) {
      waiting.push({ task, note: `已过截止 · ${formatWhen(due)}` })
      continue
    }
    if (task.owedTo) {
      const when = due && dayOffset(due) === 0 ? ` · 今天 ${hhmm(due)} 前` : ''
      waiting.push({ task, note: `${task.owedTo.who}在等你 · ${daysSince(task.owedTo.since)} 天${when}` })
      continue
    }
    const at = due && dayOffset(due) === 0 ? due : task.scheduled && dayOffset(task.scheduled) === 0 ? task.scheduled : undefined
    if (at) {
      const note = at === due ? '截止' : task.notes?.split('\n')[0] || '安排在今天'
      timeline.push({ task, note, time: hhmm(at), at, past: new Date(at).getTime() < now })
      continue
    }
    if (due && dayOffset(due) <= 3) soon.push({ task, note: `${formatWhen(due).replace(/ \d\d:\d\d$/, '')}截止` })
  }

  timeline.sort((a, b) => (a.at ?? '').localeCompare(b.at ?? ''))
  soon.sort((a, b) => (a.task.due ?? '').localeCompare(b.task.due ?? ''))
  return { waiting, timeline, soon }
}

export interface QueueItem {
  key: string
  kind: 'result' | 'handoff' | 'unsure'
  title: string
  detail: string
  /** Where to act on it. */
  to?: string
}

function thingName(state: State, id: string): string {
  return (
    state.tasks.find((t) => t.id === id)?.title ??
    state.ideas.find((i) => i.id === id)?.title ??
    state.projects.find((p) => p.id === id)?.name ??
    '一件事'
  )
}

/** Things only the user can decide: finished drafts, manual handoffs, captures the background could not sort. */
export function decisionQueue(state: State): { items: QueueItem[]; working: number } {
  const items: QueueItem[] = []
  for (const run of state.runs) {
    if (run.status === 'done' && run.output && !run.adopted) {
      items.push({ key: run.id, kind: 'result', title: thingName(state, run.thingId), detail: `副手做好了「${run.prompt}」${run.staleContext ? '，但它用到的记忆后来改过' : ''}`, to: `/t/${run.thingId}` })
    } else if (run.status === 'waiting') {
      items.push({ key: run.id, kind: 'handoff', title: thingName(state, run.thingId), detail: '要你手动转交给外部 AI，再把回答贴回来', to: `/t/${run.thingId}` })
    }
  }
  const candidates = state.candidates.filter((c) => c.state === 'pending').length
  const guesses = state.memories.filter((m) => m.epistemic === 'inferred' && Date.now() - new Date(m.versions[0].at).getTime() < 7 * DAY).length
  if (candidates + guesses > 0) {
    const parts = [candidates && `${candidates} 条记录不知道该放哪`, guesses && `${guesses} 条从资料里读到的内容等你确认`].filter(Boolean)
    items.push({ key: 'unsure', kind: 'unsure', title: '拿不准的', detail: parts.join('，') })
  }
  const working = state.runs.filter((r) => r.status === 'running').length
  return { items, working }
}

export interface FeedItem {
  key: string
  at: string
  text: string
  to?: string
  failed?: boolean
}

/** What the background did recently, newest first. */
export function backgroundFeed(state: State, since = Date.now() - 2 * DAY): FeedItem[] {
  const items: FeedItem[] = []
  const recent = (iso?: string) => Boolean(iso) && new Date(iso!).getTime() >= since
  // Processing, reminders and daily reviews arrive already folded and worded by the server.
  for (const a of state.activity ?? []) {
    if (recent(a.at)) items.push({ key: `a-${a.id}`, at: a.at, text: a.text, to: a.to, failed: a.failed })
  }
  for (const idea of state.ideas) {
    if (idea.wake && recent(idea.wake.at)) items.push({ key: `w-${idea.id}`, at: idea.wake.at, text: `把「${idea.title}」带回来了：${idea.wake.reason}`, to: `/t/${idea.id}` })
  }
  for (const run of state.runs) {
    if (run.status === 'done' && recent(run.finishedAt)) items.push({ key: `r-${run.id}`, at: run.finishedAt!, text: `做好了「${run.prompt}」· ${thingName(state, run.thingId)}`, to: `/t/${run.thingId}` })
  }
  return items.sort((a, b) => b.at.localeCompare(a.at)).slice(0, 20)
}

export interface ProjectCard {
  project: Project
  last: string
  next?: string
  open: number
  fresh: boolean
}

function hasFreshResult(runs: Run[], ids: Set<string>): boolean {
  return runs.some((r) => ids.has(r.thingId) && r.status === 'done' && !r.adopted)
}

/** Projects that are still alive; ones with something new come first. */
export function projectCards(state: State): ProjectCard[] {
  return state.projects
    .filter((p) => p.status !== 'done')
    .map((project) => {
      const tasks = state.tasks.filter((t) => t.projectId === project.id && isOpenTask(t))
      const ids = new Set([project.id, ...tasks.map((t) => t.id)])
      const fresh = hasFreshResult(state.runs, ids) || Date.now() - new Date(project.updatedAt).getTime() < DAY
      return {
        project,
        last: project.progress.split('\n').filter(Boolean).at(-1) ?? project.goal ?? '',
        next: project.nextSteps[0] ?? tasks[0]?.title,
        open: tasks.length,
        fresh,
      }
    })
    .sort((a, b) => Number(b.fresh) - Number(a.fresh) || Number(a.project.status === 'paused') - Number(b.project.status === 'paused') || b.project.updatedAt.localeCompare(a.project.updatedAt))
}

export interface IdeaWall {
  awake: Idea[]
  /** What drifts past: the most recent ideas, plus a few old ones for chance encounters. */
  drifting: Idea[]
}

export function ideaWall(state: State, recent = 8, old = 4): IdeaWall {
  const awake = state.ideas.filter((i) => i.status === 'awakened')
  const rest = state.ideas.filter((i) => i.status === 'active' || i.status === 'shelved').sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
  const drifting = rest.length <= recent + old ? rest : [...rest.slice(0, recent), ...rest.slice(-old)]
  return { awake, drifting }
}

export function ideaNote(idea: Idea): string {
  if (idea.status === 'shelved') {
    const waiting = idea.conditions.find((c) => !c.met)
    return waiting ? `等：${waiting.description}` : '放着'
  }
  return idea.body.split('\n')[0] || '还没展开'
}

/**
 * Questions go to recall; everything else is captured and sorted in the
 * background. Deliberately conservative, and the answer card can still file a
 * statement that was mistaken for a question.
 */
export function looksLikeQuestion(text: string): boolean {
  const t = text.trim()
  return /[?？]$|[吗呢]$|(怎么样|是什么|在哪|多少)$/.test(t) || /^(问一下|查一下|找一下|我之前|我上次)/.test(t)
}

/** Asking the assistant to produce something; the fallback when Jev is not configured. */
export function ambiguousDelegation(text: string): boolean {
  return /是否|要不要|应不应该|也许|可能|考虑一下|maybe|whether|should i/i.test(text)
}

export function looksLikeRequest(text: string): boolean {
  const t = text.trim()
  if (ambiguousDelegation(t)) return false
  return /^(请你?|麻烦你?)?(帮我|帮忙|替我|给我|你来)(写|起草|列|整理|做|总结|规划|拆|生成|修改|翻译|设计)\S/.test(t)
    || /^(请|麻烦)(写|起草|列|整理|做|总结|规划|拆|生成|修改|翻译|设计)\S/.test(t)
    || /^(please |can you |could you )(write|draft|make|create|summarize|translate|revise|plan)\s+\S/i.test(t)
    || /^(把|将).+(整理|改写|翻译|总结|生成|修改)/.test(t)
}
