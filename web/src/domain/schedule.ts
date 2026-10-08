import type { ID, Task } from './types'

// The hall's dates (phase 3.5, A and D): what the table of deadlines and the
// to-dos put on each day, and the to-dos that have no time at all.

export interface ScheduleSource {
  kind: 'deadline' | 'task'
  /** A row of the table points at the memory it was read from. */
  memoryId?: ID
  deadlineId?: ID
  /** A to-do points at itself. */
  itemId?: ID
}

export interface ScheduleEntry {
  /** One occurrence: for a fixed arrangement, the row and the day it falls on. */
  id: ID
  title: string
  kind: 'deadline' | 'appointment' | 'recurring' | 'task'
  /** YYYY-MM-DD in the workspace zone. */
  date?: string
  at: string | null
  /** Only the day was said; `at` is not a time to show. */
  dateOnly: boolean
  timeNote: string
  originalText: string
  source: ScheduleSource
}

export interface Schedule {
  from: string
  to: string
  timezone: string
  days: { date: string; items: ScheduleEntry[] }[]
  /** Dates that could not be pinned down. */
  unclear: ScheduleEntry[]
  /** Deadlines from before `from` that nobody has said were met. */
  overdue: ScheduleEntry[]
}

export interface InProgress {
  /** To-dos with no time, the ones that moved most recently first. */
  items: Task[]
  total: number
  /** How many more there are than `items` shows. */
  remaining: number
}

export const entryKindLabel: Record<ScheduleEntry['kind'], string> = { deadline: '截止', appointment: '预约', recurring: '固定安排', task: '待办' }

// Whatever the server left out is filled in, so the page never meets a missing field.
function entry(v: Partial<ScheduleEntry>): ScheduleEntry {
  return { id: '', title: '', kind: 'deadline', at: null, dateOnly: false, timeNote: '', originalText: '', ...v, source: { kind: 'deadline', ...v.source } }
}

export function shapeSchedule(v: unknown): Schedule {
  const s = (v ?? {}) as Partial<Schedule>
  return {
    from: s.from ?? '',
    to: s.to ?? '',
    timezone: s.timezone ?? '',
    days: (s.days ?? []).map((d) => ({ date: d.date, items: (d.items ?? []).map((e) => entry({ date: d.date, ...e })) })),
    unclear: (s.unclear ?? []).map(entry),
    overdue: (s.overdue ?? []).map(entry),
  }
}

export function shapeInProgress(v: unknown): InProgress {
  const p = (v ?? {}) as Partial<InProgress>
  const items = p.items ?? []
  return { items, total: p.total ?? items.length, remaining: p.remaining ?? 0 }
}
