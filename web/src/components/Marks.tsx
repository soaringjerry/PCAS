import { SourceSheet } from './SourceSheet'
import { useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, Box, Building2, CalendarDays, Check, CornerDownRight, FileText, Flag, Folder, Hash, LayoutGrid, MapPin, PenLine, Send, Sparkles, UserRound } from 'lucide-react'
import { kindText, type Thing, type TimelineEvent } from '../domain/things'
import { dayOffset, formatAgo, formatDateTime } from '../domain/time'
import type { Epistemic, EventPrecision, MemoryGroup, MemoryMention, SourceRef } from '../domain/types'
import { useStore } from '../store/context'
import { Tag } from './ui'

/** Source-backed wording is distinct from both user review and AI inference. */
export function TrustTag({ value }: { value: Epistemic }) {
  if (value === 'sourced') return <Tag tone="info">原话有据</Tag>
  if (value === 'inferred') return <Tag tone="warning">推测</Tag>
  if (value === 'planned') return <Tag tone="info">计划</Tag>
  return null
}

export function KindLabel({ kind, bare = false }: { kind: Thing['kind']; bare?: boolean }) {
  return (
    <span className={`kind kind-${kind}`} title={kindText[kind]}>
      {bare ? null : kindText[kind]}
    </span>
  )
}

export function ProjectLink({ id }: { id?: string }) {
  const { state } = useStore()
  const project = state.projects.find((p) => p.id === id)
  if (!project) return null
  return (
    <Link to={`/t/${project.id}`} className="from" onClick={(e) => e.stopPropagation()}>
      {project.name}
    </Link>
  )
}

export function FromLine({ source, quote = true }: { source: SourceRef; quote?: boolean }) {
  const { state } = useStore()
  const [open, setOpen] = useState(false)
  return (
    <div className="stack-sm" style={{ gap: 4 }}>
      <button type="button" className="from link-btn" onClick={() => setOpen(true)}>
        <CornerDownRight size={12} />
        {source.label} · {formatAgo(source.at, state.settings.timezone ?? 'UTC')}
      </button>
      {open && <SourceSheet id={source.sourceId} version={source.version} conversation={{ excerpt: source.excerpt, requireConversation: true }} onClose={() => setOpen(false)} />}
      {quote && source.excerpt && <div className="quote">“{source.excerpt}”</div>}
    </div>
  )
}

/** Exposure: how present a memory is. Fades with disuse, never deletes. */
export function Fade({ value }: { value: number }) {
  const bars = Math.max(1, Math.round(value * 5))
  return (
    <span className="fade" title={`曝光度 ${Math.round(value * 100)}%：长期不用会变淡，但不会被删除`}>
      {[0, 1, 2, 3, 4].map((i) => (
        <i key={i} className={i < bars ? 'on' : undefined} />
      ))}
    </span>
  )
}

const mentionIcon = {
  person: <UserRound size={12} />,
  place: <MapPin size={12} />,
  organization: <Building2 size={12} />,
  thing: <Box size={12} />,
}
const mentionOrder: MemoryMention['role'][] = ['person', 'place', 'organization', 'thing']

/**
 * Who and where a memory is about, people first. With `onPick` each one is a
 * button that narrows a list to the memories mentioning it; `active` is the one
 * the list is already narrowed to.
 */
export function Mentions({ mentions, active, onPick, limit }: {
  mentions?: MemoryMention[]
  active?: string
  onPick?: (mention: MemoryMention) => void
  /** Show this many and count the rest. */
  limit?: number
}) {
  if (!mentions?.length) return null
  const ordered = [...mentions].sort((a, b) => mentionOrder.indexOf(a.role) - mentionOrder.indexOf(b.role))
  const shown = limit && ordered.length > limit ? ordered.slice(0, limit) : ordered
  return (
    <>
      {shown.map((m) =>
        onPick ? (
          <button
            key={`${m.role}:${m.entityId}`}
            type="button"
            className={`mention${m.entityId === active ? ' on' : ''}`}
            aria-pressed={m.entityId === active}
            title={m.entityId === active ? '不再只看提到它的记忆' : `只看提到「${m.name}」的记忆`}
            onClick={(e) => {
              e.stopPropagation()
              onPick(m)
            }}
          >
            {mentionIcon[m.role]}
            <span className="mention-name">{m.name}</span>
          </button>
        ) : (
          <span key={`${m.role}:${m.entityId}`} className="mention">
            {mentionIcon[m.role]}
            <span className="mention-name">{m.name}</span>
          </span>
        ),
      )}
      {ordered.length > shown.length && <span className="mention-rest">+{ordered.length - shown.length}</span>}
    </>
  )
}

const groupIcon = {
  project: <Folder size={12} />,
  topic: <Hash size={12} />,
  area: <LayoutGrid size={12} />,
}
const groupOrder: MemoryGroup['type'][] = ['project', 'topic', 'area']

/**
 * The project, topics and area of life a memory is filed under. Each one is a
 * button that narrows a list to the memories under it; `active` is the one the
 * list is already narrowed to.
 */
export function Groups({ groups, active, onPick }: { groups?: MemoryGroup[]; active?: string; onPick: (group: MemoryGroup) => void }) {
  if (!groups?.length) return null
  const ordered = [...groups].sort((a, b) => groupOrder.indexOf(a.type) - groupOrder.indexOf(b.type))
  return (
    <>
      {ordered.map((g) => (
        <button
          key={`${g.type}:${g.entityId}`}
          type="button"
          className={`mention${g.entityId === active ? ' on' : ''}`}
          aria-pressed={g.entityId === active}
          title={g.entityId === active ? '不再只看它下面的记忆' : `只看「${g.name}」下面的记忆`}
          onClick={(e) => {
            e.stopPropagation()
            onPick(g)
          }}
        >
          {groupIcon[g.type]}
          <span className="mention-name">{g.name}</span>
        </button>
      ))}
    </>
  )
}

/** The calendar day an instant falls on in the workspace zone. */
function dayOf(at: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('zh-CN', { timeZone, year: 'numeric', month: 'numeric', day: 'numeric', weekday: 'short' }).formatToParts(at)
  const value = (type: string) => parts.find((p) => p.type === type)!.value
  return { year: value('year'), month: value('month'), day: value('day'), weekday: value('weekday') }
}

/** "10月9日 周五", "2025年3月", "2025年", "9月14日至20日": as exact as the words were, and no more. */
function eventText(from: string | undefined, to: string | undefined, precision: EventPrecision | undefined, timeZone: string): string {
  if (!from || !precision || precision === 'unknown') return ''
  const start = new Date(from)
  if (Number.isNaN(start.getTime())) return ''
  const a = dayOf(start, timeZone)
  if (precision === 'year') return `${a.year}年`
  if (precision === 'month') return `${a.year}年${a.month}月`
  const day = `${a.year === dayOf(new Date(), timeZone).year ? '' : `${a.year}年`}${a.month}月${a.day}日`
  // The interval excludes its end, so the last day it covers is the one before.
  const end = to ? new Date(new Date(to).getTime() - 1) : start
  const b = Number.isNaN(end.getTime()) || end < start ? a : dayOf(end, timeZone)
  if (precision === 'day' || (a.year === b.year && a.month === b.month && a.day === b.day)) return `${day} ${a.weekday}`
  if (a.year !== b.year) return `${a.year}年${a.month}月${a.day}日至${b.year}年${b.month}月${b.day}日`
  return a.month === b.month ? `${day}至${b.day}日` : `${day}至${b.month}月${b.day}日`
}

/** When the thing a memory talks about happens. Not when it was said; that is `SaidAt`. */
export function EventTime({ from, to, precision, bare = false }: { from?: string; to?: string; precision?: EventPrecision; bare?: boolean }) {
  const { state } = useStore()
  const text = eventText(from, to, precision, state.settings.timezone ?? 'UTC')
  if (!text) return null
  if (bare) return <>{text}</>
  return (
    <span className="event-time" title="说的是这个时候的事">
      <CalendarDays size={12} />
      {text}
    </span>
  )
}

/** When something was said: "今天说的", "9月12日说的"; `full` adds the weekday and the time and drops the suffix. */
export function SaidAt({ at, full = false }: { at?: string; full?: boolean }) {
  const { state } = useStore()
  if (!at || Number.isNaN(new Date(at).getTime())) return null
  const timeZone = state.settings.timezone ?? 'UTC'
  if (full) return <>{formatDateTime(at, timeZone)}</>
  const days = dayOffset(at, timeZone)
  const d = dayOf(new Date(at), timeZone)
  const day = days === 0 ? '今天' : days === -1 ? '昨天' : `${d.year === dayOf(new Date(), timeZone).year ? '' : `${d.year}年`}${d.month}月${d.day}日`
  return <span title="什么时候说的">{day}说的</span>
}

const tlIcon = {
  note: <PenLine size={13} />,
  source: <FileText size={13} />,
  wake: <Sparkles size={13} />,
  handoff: <Send size={13} />,
  done: <Check size={13} />,
  decision: <Flag size={13} />,
}

export function Timeline({ events, limit = 8 }: { events: TimelineEvent[]; limit?: number }) {
  const { state } = useStore()
  const [all, setAll] = useState(false)
  const hidden = all ? 0 : Math.max(0, events.length - limit)
  return (
    <>
      {hidden > 0 && (
        <button type="button" className="btn btn-quiet btn-sm" style={{ marginBottom: 10 }} onClick={() => setAll(true)}>
          更早的 {hidden} 条
        </button>
      )}
      <ol className="timeline">
        {events.slice(hidden).map((e, i) => (
          <li key={`${e.at}-${i}`}>
            <span className={`tl-dot ${e.kind}`}>{tlIcon[e.kind]}</span>
            <div className="stack-sm" style={{ gap: 4 }}>
              <div className="ink">
                {e.link ? (
                  <Link to={e.link}>
                    {e.text} <ArrowUpRight size={12} />
                  </Link>
                ) : (
                  e.text
                )}
              </div>
              {e.source?.excerpt && <div className="quote">“{e.source.excerpt}”</div>}
              <div className="tl-when">
                {e.by ? `${e.by} · ` : ''}
                {formatAgo(e.at, state.settings.timezone ?? 'UTC')}
              </div>
            </div>
          </li>
        ))}
      </ol>
    </>
  )
}
