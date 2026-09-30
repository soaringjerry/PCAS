import { useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, Check } from 'lucide-react'
import type { DeskCard, LinksCard, SourcesCard, TasksCard, TimelineCard } from '../domain/desk'
import { dayOffset, formatDateTime, isOverdue } from '../domain/time'
import { useStore } from '../store/context'
import { SourceSheet } from './SourceSheet'

type Known = SourcesCard | LinksCard | TimelineCard | TasksCard

function known(card: DeskCard): card is Known {
  return ['sources', 'links', 'timeline', 'tasks'].includes(card.kind) && Array.isArray(card.items) && card.items.length > 0
}

/** "今天", "3月12日", "2025年3月" — short enough for a margin. */
function shortDate(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const days = dayOffset(iso)
  if (days === 0) return '今天'
  if (days === -1) return '昨天'
  if (days === 1) return '明天'
  if (d.getFullYear() === new Date().getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`
  return `${d.getFullYear()}年${d.getMonth() + 1}月`
}

function Sources({ card }: { card: SourcesCard }) {
  const [open, setOpen] = useState<{ id: string; version?: number } | null>(null)
  return (
    <ul className="sec-sources" aria-label="依据">
      {open && <SourceSheet id={open.id} version={open.version} onClose={() => setOpen(null)} />}
      {card.items.map((item) => {
        const body = (
          <>
            <span className="s-text">{item.text}</span>
            {item.at && <time className="s-when">{shortDate(item.at)}</time>}
          </>
        )
        return (
          <li key={`${item.memoryId}-${item.version}`}>
            {item.sourceId ? (
              <button type="button" title={item.text} onClick={() => setOpen({ id: item.sourceId!, version: item.sourceVersion ?? undefined })}>
                {body}
              </button>
            ) : (
              <div title={item.text}>{body}</div>
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

function Timeline({ card }: { card: TimelineCard }) {
  return (
    <figure className="sec-timeline">
      {card.title && <figcaption>{card.title}</figcaption>}
      <ol>
        {card.items.map((item, i) => (
          <li key={i} className={item.status}>
            <time>{item.at ? shortDate(item.at) : ''}</time>
            <span className="t-mark" aria-label={item.status === 'done' ? '已完成' : item.status === 'dropped' ? '放弃了' : undefined}>
              {item.status === 'done' && <Check size={9} strokeWidth={3.5} />}
            </span>
            {item.thingId ? (
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
        const meta = [due && formatDateTime(due), item.project].filter(Boolean).join(' · ')
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
  return (
    <>
      {cards.filter(known).map((card, i) => {
        switch (card.kind) {
          case 'sources':
            return <Sources key={i} card={card} />
          case 'links':
            return <Links key={i} card={card} />
          case 'timeline':
            return <Timeline key={i} card={card} />
          case 'tasks':
            return <Tasks key={i} card={card} />
        }
      })}
    </>
  )
}
