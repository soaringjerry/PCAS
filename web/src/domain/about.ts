import type { StatusCardFieldKind, StatusCardKind } from './status'

/** The order the cards are shown in, and what each kind is called. */
export const cardKinds: { kind: StatusCardKind; label: string }[] = [
  { kind: 'self', label: '关于你' },
  { kind: 'project', label: '项目' },
  { kind: 'person', label: '人' },
  { kind: 'topic', label: '主题' },
  { kind: 'area', label: '领域' },
]

export const cardFieldLabel: Record<StatusCardFieldKind, string> = {
  status: '现状',
  deadline: '期限',
  decided: '已经定的',
  blocker: '卡点',
  next: '下一步',
  preference: '偏好',
  people: '相关的人',
}

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
