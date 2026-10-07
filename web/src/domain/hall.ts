import { isOpenTask } from './things'
import { clockTime, dayOffset, formatWhen } from './time'
import type { Idea, Notice, Project, Run, State, Task } from './types'

// The home screen is a service hall (docs/design/principles.md): today on the
// left, projects and ideas on the right, one desk in the middle. These
// selectors decide what each wall shows; none of them spend money.

const HOUR = 60 * 60 * 1000
const DAY = 24 * HOUR

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

export interface NoticeRow {
  notice: Notice
  /** The thing it is about, when that is a task the circle can finish. */
  task?: Task
}

export interface TodayColumn {
  /** Reminders that went off and have not been closed; pinned on top. */
  rang: NoticeRow[]
  /** Someone is waiting, a follow-up is due, or it went late on an earlier day. */
  waiting: TodayRow[]
  /** Said to be urgent and given no time yet; these lead the timeline, oldest first. */
  urgent: TodayRow[]
  /** Due or scheduled today, in clock order; the ones already past are marked. */
  timeline: TodayRow[]
  /** Due within the next three days. Anything later stays out until it is close. */
  soon: TodayRow[]
}

function followUpAt(task: Task): string | undefined {
  return task.triggers.find((t) => t.active && t.nextAt)?.nextAt
}

/** Reminders still ringing: not closed, and about something not yet done or dropped. */
function ringing(state: State): NoticeRow[] {
  const rows: NoticeRow[] = []
  for (const notice of state.notices ?? []) {
    if (notice.dismissedAt) continue
    const task = state.tasks.find((t) => t.id === notice.thingId)
    if (task && !isOpenTask(task)) continue
    const idea = state.ideas.find((i) => i.id === notice.thingId)
    if (idea && (idea.status === 'promoted' || idea.status === 'dropped')) continue
    if (!task && !idea && !state.projects.some((p) => p.id === notice.thingId)) continue
    rows.push({ notice, task })
  }
  return rows.sort((a, b) => a.notice.dueAt.localeCompare(b.notice.dueAt))
}

export function todayColumn(state: State): TodayColumn {
  const rang = ringing(state)
  // A task whose reminder is pinned on top is not listed a second time below.
  const pinned = new Set(rang.map((r) => r.notice.thingId))
  const waiting: TodayRow[] = []
  const urgent: TodayRow[] = []
  const timeline: TodayRow[] = []
  const soon: TodayRow[] = []
  const now = Date.now()
  const timezone = state.settings.timezone ?? 'UTC'

  for (const task of state.tasks) {
    if (!isOpenTask(task) || pinned.has(task.id)) continue
    const due = task.due
    const follow = task.status === 'waiting' ? followUpAt(task) : undefined

    if (follow && dayOffset(follow, timezone) <= 0) {
      waiting.push({ task, note: `该跟进了：${task.waitingFor ?? '对方还没回'}` })
      continue
    }
    if (task.status === 'waiting') continue
    // Anything due or scheduled today belongs on today's timeline, even once its time has passed.
    const at = due && dayOffset(due, timezone) === 0 ? due : task.scheduled && dayOffset(task.scheduled, timezone) === 0 ? task.scheduled : undefined
    if (at) {
      const past = new Date(at).getTime() < now
      const note = task.owedTo ? `${task.owedTo.who}在等你` : at === due ? (past ? '过了截止时间' : '截止') : task.notes?.split('\n')[0] || '安排在今天'
      timeline.push({ task, note, time: clockTime(at, timezone), at, past })
      continue
    }
    if (due && new Date(due).getTime() < now) {
      waiting.push({ task, note: `已过截止 · ${formatWhen(due, timezone)}` })
      continue
    }
    if (task.owedTo) {
      waiting.push({ task, note: `${task.owedTo.who}在等你 · ${daysSince(task.owedTo.since)} 天` })
      continue
    }
    // No time at all, but the user said it cannot wait: it stays on the timeline until it is done or gets a time.
    if (task.urgent && !due && !task.scheduled) {
      urgent.push({ task, note: task.notes?.split('\n')[0] || '你说过要尽快', time: '尽快' })
      continue
    }
    if (due && dayOffset(due, timezone) <= 3) soon.push({ task, note: `${formatWhen(due, timezone).replace(/ \d\d:\d\d$/, '')}截止` })
  }

  urgent.sort((a, b) => a.task.createdAt.localeCompare(b.task.createdAt))
  timeline.sort((a, b) => (a.at ?? '').localeCompare(b.at ?? ''))
  soon.sort((a, b) => (a.task.due ?? '').localeCompare(b.task.due ?? ''))
  return { rang, waiting, urgent, timeline, soon }
}

export interface QueueItem {
  key: string
  kind: 'redo' | 'handoff'
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

/**
 * What only the user can move forward: results whose basis changed and need
 * redoing, and manual handoffs. Finished results put themselves in place, and
 * captures the background could not sort go to the observatory, not here.
 */
export function decisionQueue(state: State): { items: QueueItem[]; working: number } {
  const items: QueueItem[] = []
  /** A thing that is finished, dropped or gone asks nothing more of the user. */
  const open = (id: string) => {
    const task = state.tasks.find((t) => t.id === id)
    if (task) return isOpenTask(task)
    const idea = state.ideas.find((i) => i.id === id)
    if (idea) return idea.status !== 'promoted' && idea.status !== 'dropped'
    const project = state.projects.find((p) => p.id === id)
    return !!project && project.status !== 'done'
  }
  /** Only the newest result for a thing can need redoing; an older one was already superseded. */
  const newest = new Map<string, string>()
  for (const run of state.runs) {
    const seen = newest.get(run.thingId)
    if (!seen || run.createdAt > seen) newest.set(run.thingId, run.createdAt)
  }
  for (const run of state.runs) {
    if (!open(run.thingId)) continue
    if (run.status === 'done' && run.output && !run.adopted && run.staleContext) {
      if (newest.get(run.thingId) !== run.createdAt) continue
      items.push({ key: run.id, kind: 'redo', title: thingName(state, run.thingId), detail: `「${run.prompt}」用到的记忆后来改过，要重做`, to: `/t/${run.thingId}` })
    } else if (run.status === 'waiting') {
      items.push({ key: run.id, kind: 'handoff', title: thingName(state, run.thingId), detail: '要你手动转交给外部 AI，再把回答贴回来', to: `/t/${run.thingId}` })
    }
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
  /** The first sentence of the handover's 下一步. */
  next?: string
  /** Shown when there is no handover yet. */
  goal: string
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
        next: project.handoverNext || undefined,
        goal: project.goal ?? '',
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
