import { useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, Check } from 'lucide-react'
import { isKnownCard, type DeskCard, type LinksCard, type SourcesCard, type TasksCard, type TimelineCard } from '../domain/desk'
import { formatShortDate, formatDateTime, isOverdue } from '../domain/time'
import { useStore } from '../store/context'
import { SourceSheet } from './SourceSheet'

type Quote = SourcesCard['items'][number]

/** A line kept as it was said, rather than a memory drawn from one. */
const isSaid = (item: Quote) => item.kind === 'source'

function Sources({ items }: { items: Quote[] }) {
  const { state } = useStore()
  const [open, setOpen] = useState<{ id: string; version?: number } | null>(null)
  return (
    <ul className="sec-sources" aria-label="依据">
      {open && <SourceSheet id={open.id} version={open.version} onClose={() => setOpen(null)} />}
      {items.map((item) => {
        const said = isSaid(item)
        const when = item.at ? formatShortDate(item.at, state.settings.timezone ?? 'UTC') : ''
        // The user's own words come in quotation marks and are named as such; a memory stays a plain line.
        const body = said ? (
          <>
            <q className="s-text">{item.text}</q>
            <span className="s-when">
              原话
              {when && ' · '}
              {when && <time>{when}</time>}
            </span>
          </>
        ) : (
          <>
            <span className="s-text">{item.text}</span>
            {item.at && <time className="s-when">{when}</time>}
          </>
        )
        return (
          <li key={`${item.memoryId}-${item.version}`} className={said ? 'said' : undefined}>
            {item.sourceId ? (
              // An excerpt can run long; the row shows its start and the sheet has all of it.
              <button type="button" title={said ? '看原文' : item.text} onClick={() => setOpen({ id: item.sourceId!, version: item.sourceVersion ?? undefined })}>
                {body}
              </button>
            ) : (
              <div title={said ? undefined : item.text}>{body}</div>
            )}
          </li>
        )
      })}
    </ul>
  )
}

function Links({ card }: { card: LinksCard }) {
  return (
    <ul className="sec-links" aria-label="链接">
      {card.items.map((link) => (
        <li key={link.url}>
          <a href={link.url} target="_blank" rel="noopener noreferrer" title={link.url}>
            {link.host}
            <ArrowUpRight size={12} />
          </a>
        </li>
      ))}
    </ul>
  )
}

type Moment = TimelineCard['items'][number] & { source?: Quote }

function Timeline({ title, items }: { title?: string; items: Moment[] }) {
  const { state } = useStore()
  const [open, setOpen] = useState<{ id: string; version?: number } | null>(null)
  return (
    <figure className="sec-timeline">
      {open && <SourceSheet id={open.id} version={open.version} onClose={() => setOpen(null)} />}
      {title && <figcaption>{title}</figcaption>}
      <ol>
        {items.map((item, i) => (
          <li key={i} className={item.status}>
            <time>{item.at ? formatShortDate(item.at, state.settings.timezone ?? 'UTC') : ''}</time>
            <span className="t-mark" aria-label={item.status === 'done' ? '已完成' : item.status === 'dropped' ? '放弃了' : undefined}>
              {item.status === 'done' && <Check size={9} strokeWidth={3.5} />}
            </span>
            {item.source?.sourceId ? (
              // Each moment opens the record it came from.
              <button type="button" className="t-text t-source" title="看原文" onClick={() => setOpen({ id: item.source!.sourceId!, version: item.source!.sourceVersion ?? undefined })}>
                {item.text}
              </button>
            ) : item.thingId ? (
              <Link className="t-text" to={`/t/${item.thingId}`}>
                {item.text}
              </Link>
            ) : (
              <span className="t-text">{item.text}</span>
            )}
          </li>
        ))}
      </ol>
    </figure>
  )
}

/**
 * With a timeline, the quotes live on it rather than in a second list: each
 * moment carries its source, and quotes without a moment join it in time order.
 * Lines as they were said are not moments; they keep their own list.
 */
function withQuotes(timeline: TimelineCard, sources: SourcesCard | undefined): Moment[] {
  const quotes = sources?.items.filter((q) => !isSaid(q)) ?? []
  if (!quotes.length) return timeline.items
  const byMemory = new Map(quotes.map((q) => [q.memoryId, q]))
  const placed = new Set<string>()
  const moments: Moment[] = timeline.items.map((item) => {
    const source = item.memoryId ? byMemory.get(item.memoryId) : undefined
    if (source) placed.add(source.memoryId)
    return { ...item, source }
  })
  const rest = quotes.filter((q) => !placed.has(q.memoryId)).map((q): Moment => ({ at: q.at, text: q.text, status: 'open', memoryId: q.memoryId, thingId: null, source: q }))
  // Keep the timeline in time order; moments without a time go last, in the order given.
  const when = (m: Moment) => (m.at ? new Date(m.at).getTime() : Infinity)
  return [...moments, ...rest].sort((a, b) => when(a) - when(b))
}

function Tasks({ card }: { card: TasksCard }) {
  const { state, dispatchUndoable } = useStore()
  return (
    <ul className="sec-tasks" aria-label="事项">
      {card.items.map((item) => {
        // The card is a snapshot; the store knows whether it has been done since.
        const live = state.tasks.find((t) => t.id === item.thingId)
        const status = live?.status ?? item.status
        const due = live ? live.due : item.due
        const done = status === 'done' || status === 'cancelled'
        const meta = [due && formatDateTime(due, state.settings.timezone ?? 'UTC'), item.project].filter(Boolean).join(' · ')
        return (
          <li key={item.thingId} className={done ? 'done' : undefined}>
            <button
              type="button"
              className="k-check"
              aria-label={`做完了：${item.title}`}
              aria-pressed={done}
              disabled={done || !live}
              onClick={() => void dispatchUndoable({ type: 'setTaskStatus', id: item.thingId, status: 'done' }, '做完了')}
            >
              {done && <Check size={11} strokeWidth={3} />}
            </button>
            <Link className="k-title" to={`/t/${item.thingId}`}>
              {live?.title ?? item.title}
            </Link>
            {meta && <span className={`k-meta${due && !done && isOverdue(due) ? ' late' : ''}`}>{meta}</span>}
          </li>
        )
      })}
    </ul>
  )
}

/** Visual answers: quotes with their source, links, a timeline, a few task rows. Unknown kinds are skipped. */
export function SecretaryCards({ cards }: { cards: DeskCard[] }) {
  const known = cards.filter(isKnownCard)
  const sources = known.find((c): c is SourcesCard => c.kind === 'sources')
  const timeline = known.some((c) => c.kind === 'timeline')
  return (
    <>
      {known.map((card, i) => {
        switch (card.kind) {
          case 'sources': {
            // Said once: a timeline already carries the memories, so only the lines as they were said are left to list.
            const items = timeline ? card.items.filter(isSaid) : card.items
            return items.length > 0 ? <Sources key={i} items={items} /> : null
          }
          case 'links':
            return <Links key={i} card={card} />
          case 'timeline':
            return <Timeline key={i} title={card.title} items={withQuotes(card, sources)} />
          case 'tasks':
            return <Tasks key={i} card={card} />
        }
      })}
    </>
  )
}
