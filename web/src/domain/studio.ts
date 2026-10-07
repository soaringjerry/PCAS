// What a studio (a project's page) reads on top of the workspace snapshot:
// the project's handover, shown as the status block, and a document's versions.
// The shapes are the phase 3 backend's (the skeleton PR freezes them);
// `store/studio.ts` is the only place that knows the paths.

import type { ID, Task, TaskStatus } from './types'

/**
 * What one sentence of the handover rests on, by id. For a memory `version` is
 * the memory's version; for `documentVersion` the id is the document's and
 * `version` its version number.
 */
export interface Evidence {
  kind: 'memory' | 'item' | 'documentVersion' | 'run'
  id: ID
  version?: number
}

export interface Sentence {
  text: string
  evidence: Evidence[]
}

/** A project's handover: written by the model, never edited here. */
export interface Handover {
  projectId: ID
  /** Absent while none has been written; the three parts are then empty. */
  writtenAt: string | null
  /** Something it was written from has changed; a new one is on its way. */
  stale: boolean
  conclusion: Sentence[]
  blockers: Sentence[]
  nextSteps: Sentence[]
}

/** Whether there is a handover to show at all. */
export function isWritten(handover: Handover): boolean {
  return Boolean(handover.writtenAt) || handover.conclusion.length + handover.blockers.length + handover.nextSteps.length > 0
}

export type VersionAuthor = 'user' | 'deputy' | 'secretary'

export interface DocVersion {
  documentId: ID
  version: number
  writtenAt: string
  author: VersionAuthor
  basedOn: number | null
  /** The assistant work that wrote it, when one did. */
  runId: ID | null
}

/** One changed paragraph, in the document's order. The side a paragraph is missing from is empty. */
export interface ParagraphChange {
  kind: 'add' | 'delete' | 'change'
  beforeIndex: number | null
  afterIndex: number | null
  before: string
  after: string
}

/** Only what changed between two versions; paragraphs that stayed are not listed. */
export interface DocDiff {
  documentId: ID
  fromVersion: number
  toVersion: number
  changes: ParagraphChange[]
}

export const authorText: Record<VersionAuthor, string> = { user: '你', deputy: '副手', secretary: '秘书' }

/** A run of saves shown as one line; `versions` is newest first and never empty. */
export interface VersionGroup {
  versions: DocVersion[]
}

/** One sitting of editing: saving on blur writes many versions in a row. */
export const FOLD_WINDOW = 15 * 60 * 1000

/**
 * Folds what one author wrote in a row into one line. A group spans at most
 * 15 minutes from its first version, and a piece of assistant work never
 * shares a line with another one. Returned newest first, like the list.
 */
export function foldVersions(versions: DocVersion[]): VersionGroup[] {
  const oldestFirst = [...versions].sort((a, b) => a.version - b.version)
  const groups: DocVersion[][] = []
  for (const v of oldestFirst) {
    const group = groups.at(-1)
    const first = group?.[0]
    if (group && first && first.author === v.author && (first.runId ?? '') === (v.runId ?? '') && new Date(v.writtenAt).getTime() - new Date(first.writtenAt).getTime() <= FOLD_WINDOW) group.push(v)
    else groups.push([v])
  }
  return groups.reverse().map((g) => ({ versions: g.reverse() }))
}

/** The two versions compared when nothing was picked: the latest against the one before it. The latest counts as touched last, so the next pick is compared with it. */
export function defaultPair(versions: DocVersion[]): number[] {
  return [...versions].sort((a, b) => a.version - b.version).slice(-2).map((v) => v.version)
}

/** Picking keeps the last two versions touched; touching a picked one lets it go. */
export function pick(picked: number[], version: number): number[] {
  if (picked.includes(version)) return picked.filter((v) => v !== version)
  return [...picked, version].slice(-2)
}

/* ---------- Files ---------- */

/** A file kept with the project. It is a source like any other; `sourceId` names it. */
export interface ProjectFile {
  sourceId: ID
  projectId: ID
  name: string
  mediaType: string
  size: number
  createdAt: string
  status: 'stored' | 'extracted' | 'failed'
  failureReason: string
  /** The processing job, for trying a failed one again. */
  jobId: ID | null
  openUrl: string
}

/** The server's limit for one attachment. */
export const FILE_LIMIT = 20 * 1024 * 1024

/** Why a file cannot be sent, or nothing when it can. */
export function checkFile(file: { name: string; size: number }): string {
  if (!file.size) return `「${file.name}」是空的`
  if (file.size > FILE_LIMIT) return `「${file.name}」超过 20 MB`
  return ''
}

/** What the browser shows by itself; anything else is downloaded. */
export function opensInPage(file: Pick<ProjectFile, 'mediaType'>): 'image' | 'frame' | undefined {
  if (file.mediaType.startsWith('image/')) return 'image'
  if (file.mediaType === 'application/pdf' || file.mediaType.startsWith('text/')) return 'frame'
  return undefined
}

export function fileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1).replace(/\.0$/, '')} MB`
}

/* ---------- Plan timeline ---------- */

export interface TimelineItem {
  id: ID
  title: string
  /** `YYYY-MM-DD` in the workspace zone; absent while there is no estimate. */
  startDate: string | null
  due: string
  status: TaskStatus
  estimatedHours: number | null
  overdue: boolean
}

export interface ProjectTimeline {
  projectId: ID
  items: TimelineItem[]
  withoutDue: Task[]
  dailyHours: number
}

const DAY = 24 * 60 * 60 * 1000

/** A calendar day as a count of days, so days can be subtracted. */
export function dayNumber(ymd: string): number {
  const [y, m, d] = ymd.split('-').map(Number)
  return Math.round(Date.UTC(y, m - 1, d) / DAY)
}

/** The calendar day an instant falls on in the workspace zone, as `YYYY-MM-DD`. */
export function dayOf(iso: string | number, timeZone?: string): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone, year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date(iso))
}

/** How far through its day an instant is in the workspace zone, 0..1. */
function dayFraction(at: number, timeZone?: string): number {
  const parts = new Intl.DateTimeFormat('en-GB', { timeZone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(at))
  const value = (type: string) => Number(parts.find((p) => p.type === type)?.value ?? 0)
  return (value('hour') * 60 + value('minute')) / (24 * 60)
}

export interface TimelineBar {
  item: TimelineItem
  /** Left edge and width as fractions of the axis. Without a start day the bar is only its due day. */
  left: number
  width: number
  started: boolean
  tone: 'done' | 'cancelled' | 'late' | 'open'
}

export interface TimelineTick {
  at: number
  label: string
  weekend: boolean
}

export interface TimelineLayout {
  days: number
  /** Where now is, as a fraction of the axis. */
  now: number
  bars: TimelineBar[]
  ticks: TimelineTick[]
}

/**
 * Lays the dated items on one axis of whole days in the workspace zone. The
 * axis runs from a day before the earliest start (or today) to two days after
 * the latest due day (or today), so now is always on it.
 */
export function layoutTimeline(items: TimelineItem[], now: number, timeZone?: string): TimelineLayout {
  const today = dayNumber(dayOf(now, timeZone))
  const spans = items.map((item) => {
    const end = dayNumber(dayOf(item.due, timeZone))
    const start = item.startDate ? Math.min(dayNumber(item.startDate), end) : end
    return { item, start, end }
  })
  const first = Math.min(today, ...spans.map((s) => s.start)) - 1
  const last = Math.max(today, ...spans.map((s) => s.end)) + 2
  const days = last - first + 1
  const bars = spans
    .sort((a, b) => a.start - b.start || a.end - b.end)
    .map(({ item, start, end }): TimelineBar => ({
      item,
      left: (start - first) / days,
      width: (end - start + 1) / days,
      started: Boolean(item.startDate),
      tone: item.status === 'done' ? 'done' : item.status === 'cancelled' ? 'cancelled' : item.overdue ? 'late' : 'open',
    }))
  const ticks: TimelineTick[] = []
  for (let n = first; n <= last; n++) {
    const date = new Date(n * DAY)
    const weekday = date.getUTCDay()
    // A short plan names every day; a long one names Mondays and the first of each month.
    const full = n === first || date.getUTCDate() === 1 || days > 16
    const named = full ? days <= 16 || weekday === 1 || date.getUTCDate() === 1 || n === first : true
    // A full date is wider than its day, so the day after it goes unnamed.
    const crowded = Boolean(ticks.at(-1)?.label.includes('月'))
    ticks.push({ at: (n - first) / days, weekend: weekday === 0 || weekday === 6, label: !named || crowded ? '' : full ? `${date.getUTCMonth() + 1}月${date.getUTCDate()}日` : `${date.getUTCDate()}` })
  }
  return { days, now: (today - first + dayFraction(now, timeZone)) / days, bars, ticks }
}

/** "约 8 小时", "约 1.5 小时". */
export function effortText(hours: number): string {
  return `约 ${Number(hours.toFixed(1))} 小时`
}
