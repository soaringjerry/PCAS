import { entryKindLabel, type Schedule, type ScheduleEntry } from './schedule'
import { isOpenTask } from './things'
import { civilOffset, clockTime, dayOffset, formatCivil, formatWhen } from './time'
import type { Creation, Idea, Notice, Project, Run, State, Task } from './types'

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

/** A row that comes from the table of deadlines rather than from a to-do. */
export interface DateRow {
  entry: ScheduleEntry
  note: string
  time?: string
  at?: string
  past?: boolean
  /** A deadline whose time has gone by with nobody saying it was met; the circle says so. */
  canFinish?: boolean
}

export type TimeRow = TodayRow | DateRow

export function isDateRow(row: TimeRow): row is DateRow {
  return 'entry' in row
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
  /** Deadlines from an earlier day that nobody has said were met, the latest first. */
  late: DateRow[]
  /** Said to be urgent and given no time yet; these lead the timeline, oldest first. */
  urgent: TodayRow[]
  /** Due or scheduled today, in clock order; the ones already past are marked. */
  timeline: TimeRow[]
  /** Due or booked within the next three days. Anything later stays out until it is close. */
  soon: TimeRow[]
  /** Dates that were never pinned down, each with what was said. */
  unclear: DateRow[]
}

function dated(entry: ScheduleEntry, timezone: string): string {
  const day = entry.date ? formatCivil(entry.date, timezone) : ''
  return entry.at && !entry.dateOnly ? formatWhen(entry.at, timezone) : day
}

/** What the table of deadlines adds to the column. To-dos already in the workspace are placed by the workspace's own rules. */
function scheduleRows(state: State, schedule: Schedule, timezone: string, now: number) {
  const known = new Set(state.tasks.map((t) => t.id))
  const mine = (e: ScheduleEntry) => !(e.source.kind === 'task' && e.source.itemId && known.has(e.source.itemId))
  const withNote = (text: string, e: ScheduleEntry) => [text, e.timeNote].filter(Boolean).join(' · ')
  const timeline: DateRow[] = []
  const soon: DateRow[] = []
  for (const day of schedule.days) {
    const offset = civilOffset(day.date, timezone)
    for (const entry of day.items.filter(mine)) {
      const timed = entry.at && !entry.dateOnly ? entry.at : undefined
      if (offset === 0) {
        const past = !!timed && new Date(timed).getTime() < now
        const late = past && entry.kind === 'deadline'
        timeline.push({ entry, note: withNote(late ? '过了截止时间' : entryKindLabel[entry.kind], entry), time: timed ? clockTime(timed, timezone) : '今天', at: timed, past, canFinish: late })
      } else if (offset > 0 && (entry.kind === 'deadline' || entry.kind === 'appointment')) {
        // A fixed arrangement shows on its own day; listing every one ahead would bury the dates that are news.
        const when = entry.kind === 'deadline' ? `${formatCivil(day.date, timezone)}截止` : `${dated(entry, timezone)} 预约`
        soon.push({ entry, note: withNote(when, entry), at: timed })
      }
    }
  }
  const late: DateRow[] = schedule.overdue
    .filter(mine)
    .map((entry) => ({ entry, note: withNote(`已过截止 · ${dated(entry, timezone) || '日期没说清'}`, entry), at: entry.at ?? undefined, canFinish: entry.source.kind === 'deadline' }))
    .sort((a, b) => (b.at ?? b.entry.date ?? '').localeCompare(a.at ?? a.entry.date ?? ''))
  const unclear: DateRow[] = schedule.unclear.filter(mine).map((entry) => ({ entry, note: entry.originalText ? `原话：${entry.originalText}` : entry.timeNote || '日期没说清' }))
  return { timeline, soon, late, unclear }
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

export function todayColumn(state: State, schedule?: Schedule): TodayColumn {
  const rang = ringing(state)
  // A task whose reminder is pinned on top is not listed a second time below.
  const pinned = new Set(rang.map((r) => r.notice.thingId))
  const waiting: TodayRow[] = []
  const urgent: TodayRow[] = []
  const timeline: TimeRow[] = []
  const soon: TimeRow[] = []
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

  const dates = schedule ? scheduleRows(state, schedule, timezone, now) : { timeline: [], soon: [], late: [], unclear: [] }
  timeline.push(...dates.timeline)
  soon.push(...dates.soon)

  urgent.sort((a, b) => a.task.createdAt.localeCompare(b.task.createdAt))
  // Something said for the day with no hour comes before the hours.
  timeline.sort((a, b) => (a.at ?? '').localeCompare(b.at ?? ''))
  const day = (row: TimeRow) => (isDateRow(row) ? civilOffset(row.entry.date ?? '', timezone) : dayOffset(row.task.due!, timezone))
  const hour = (row: TimeRow) => (isDateRow(row) ? row.at : row.task.due) ?? ''
  soon.sort((a, b) => day(a) - day(b) || hour(a).localeCompare(hour(b)))
  return { rang, waiting, late: dates.late, urgent, timeline, soon, unclear: dates.unclear }
}

/**
 * The first `cap` rows of the column above 「这几天」, kept in this order: what
 * rang, who waits, what went by unmet, what cannot wait, what is still ahead
 * today, then what already passed.
 */
export function firstRows(full: TodayColumn, cap: number) {
  let left = cap
  const take = <T,>(rows: T[]): T[] => {
    const taken = rows.slice(0, left)
    left -= taken.length
    return taken
  }
  return {
    rang: take(full.rang),
    waiting: take(full.waiting),
    late: take(full.late),
    urgent: take(full.urgent),
    ahead: take(full.timeline.filter((r) => !r.past)),
    passed: take(full.timeline.filter((r) => r.past)),
  }
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
  /** The recorded action behind the line, when it can be taken back. */
  actionId?: string
}

const madeWhat = { task: '待办', idea: '想法', project: '项目' } as const

/** One line for a thing the background made on its own: who made it, and on what. */
function madeLine(kind: keyof typeof madeWhat, title: string, creation: Creation): string {
  if (creation.by === 'background_topic') {
    const n = creation.memoryIds?.length ?? 0
    return `后台看这个主题${n > 0 ? `攒了 ${n} 条记忆` : '聊得多了'}，建了${madeWhat[kind]}「${title}」`
  }
  const said = creation.source?.excerpt?.trim()
  const from = said ? `依据原话「${said}」` : creation.source?.label ? `依据「${creation.source.label}」` : creation.memoryIds?.length ? `依据 ${creation.memoryIds.length} 条记忆` : ''
  return [`后台整理时建了${madeWhat[kind]}「${title}」`, from].filter(Boolean).join(' · ')
}

/** What the background did recently, newest first. */
export function backgroundFeed(state: State, since = Date.now() - 2 * DAY): FeedItem[] {
  const items: FeedItem[] = []
  const recent = (iso?: string) => Boolean(iso) && new Date(iso!).getTime() >= since
  // Processing, reminders and daily reviews arrive already folded and worded by the server.
  for (const a of state.activity ?? []) {
    if (recent(a.at)) items.push({ key: `a-${a.id}`, at: a.at, text: a.text, to: a.to, failed: a.failed })
  }
  // What the background made outright: one line each, which can be taken back. What the secretary made has its receipt in the conversation.
  const made = (kind: keyof typeof madeWhat, id: string, title: string, at: string, creation?: Creation) => {
    if (!creation || creation.by === 'secretary' || !recent(at)) return
    items.push({ key: `c-${id}`, at, text: madeLine(kind, title, creation), to: `/t/${id}`, actionId: creation.actionId })
  }
  for (const task of state.tasks) made('task', task.id, task.title, task.createdAt, task.creation)
  for (const idea of state.ideas) made('idea', idea.id, idea.title, idea.createdAt, idea.creation)
  for (const project of state.projects) made('project', project.id, project.name, project.createdAt ?? project.updatedAt, project.creation)
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
        next: project.projectHandover?.nextSteps[0]?.text || undefined,
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
