import type { Deadline } from './status'
import type { MemoryCategory } from './types'

export const deadlineKindLabel = { deadline: '截止', appointment: '预约', recurring: '固定安排' } as const

/** The nine parts a handover note is written in. */
const handoverTitles = ['他是谁和现在的处境', '怎么跟他配合', '现在手上的事', '时间和节奏', '资源和限制', '口味和标准', '重要的人', '他的叫法', '他看重什么']

export interface HandoverSection {
  /** Empty for text that comes before any heading. */
  title: string
  text: string
}

/** A line's heading, if it is one: a Markdown or numbered or bracketed title, or one of the nine on a line of its own. */
function heading(line: string): { title: string; rest: string } | undefined {
  const trimmed = line.trim()
  const marked = /^(#{1,6}\s+|\*\*|【|[0-9一二三四五六七八九]+[.、．)）]\s*)/.test(trimmed)
  const bare = trimmed
    .replace(/^#{1,6}\s+/, '')
    .replace(/^[0-9一二三四五六七八九]+[.、．)）]\s*/, '')
    .replace(/^(\*\*|【)/, '')
  const known = handoverTitles.find((t) => bare.startsWith(t))
  if (known) {
    const rest = bare.slice(known.length).replace(/^(\*\*|】)/, '').replace(/^[:：]\s*/, '').replace(/\*\*$/, '')
    return { title: known, rest: rest.trim() }
  }
  if (!marked) return undefined
  const title = bare.replace(/(\*\*|】)\s*[:：]?$/, '').replace(/[:：]$/, '').trim()
  return title && title.length <= 20 ? { title, rest: '' } : undefined
}

/** A handover note cut into its parts. A note with no headings comes back as one part. */
export function handoverSections(body: string): HandoverSection[] {
  const out: HandoverSection[] = []
  let current: HandoverSection = { title: '', text: '' }
  for (const line of body.split('\n')) {
    const h = heading(line)
    if (h) {
      if (current.title || current.text.trim()) out.push(current)
      current = { title: h.title, text: h.rest }
    } else {
      current.text += (current.text ? '\n' : '') + line
    }
  }
  if (current.title || current.text.trim()) out.push(current)
  return out.map((s) => ({ title: s.title, text: s.text.trim() }))
}

export type DeadlineGroup = 'upcoming' | 'overdue' | 'recurring' | 'unclear'

/** The order the dates are shown in, and what each part is called. */
export const deadlineGroups: { group: DeadlineGroup; label: string }[] = [
  { group: 'upcoming', label: '还没到的' },
  { group: 'overdue', label: '已过期，不知是否完成' },
  { group: 'recurring', label: '固定安排' },
  { group: 'unclear', label: '日期没说清的' },
]

export function deadlineGroupOf(d: Deadline, now: number): DeadlineGroup {
  if (d.kind === 'recurring') return 'recurring'
  if (!d.at || Number.isNaN(new Date(d.at).getTime())) return 'unclear'
  return new Date(d.at).getTime() < now ? 'overdue' : 'upcoming'
}

/** The dates by part: the nearest first among those to come, the latest first among those gone by. */
export function groupDeadlines(items: Deadline[], now: number): Record<DeadlineGroup, Deadline[]> {
  const out: Record<DeadlineGroup, Deadline[]> = { upcoming: [], overdue: [], recurring: [], unclear: [] }
  for (const d of items) out[deadlineGroupOf(d, now)].push(d)
  const time = (d: Deadline) => new Date(d.at!).getTime()
  out.upcoming.sort((a, b) => time(a) - time(b))
  out.overdue.sort((a, b) => time(b) - time(a))
  return out
}

/** The kinds of memory about the user themselves that the library can be narrowed to. */
export const selfCategories: Extract<MemoryCategory, 'identity' | 'goal' | 'taste' | 'rule'>[] = ['identity', 'goal', 'taste', 'rule']

/**
 * Where a link to one of the former cards leads: the library narrowed to the
 * same memories. `isGroup` says whether an id is a project, topic or area;
 * anything else is a person.
 */
export function cardFilter(key: string, isGroup: (entityId: string) => boolean): { category?: string; group?: string; entity?: string } {
  if (key.startsWith('self:')) {
    const category = selfCategories.find((c) => c === key.slice(5))
    return category ? { category } : {}
  }
  if (!key.startsWith('entity:') || key.length === 7) return {}
  const id = key.slice(7)
  return isGroup(id) ? { group: id } : { entity: id }
}
