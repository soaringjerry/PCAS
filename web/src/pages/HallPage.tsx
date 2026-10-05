import { useEffect, useState, type CSSProperties } from 'react'
import { Link } from 'react-router'
import { BellRing, ChevronDown, ChevronRight, X } from 'lucide-react'
import { TimezoneHint } from '../components/TimezoneHint'
import { Secretary } from '../components/Secretary'
import { backgroundFeed, decisionQueue, ideaNote, ideaWall, projectCards, todayColumn, type NoticeRow as NoticeRowData, type TodayRow } from '../domain/hall'
import { projectStatusLabel } from '../domain/labels'
import { clockTime, formatAgo, formatWhen } from '../domain/time'
import type { State } from '../domain/types'
import { api } from '../store/api'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'

const SEEN_KEY = 'pcas.hall.seen'

function readSeen(): number {
  try {
    return Number(localStorage.getItem(SEEN_KEY)) || Date.now() - 24 * 60 * 60 * 1000
  } catch {
    return Date.now() - 24 * 60 * 60 * 1000
  }
}

/* ---------- Today ---------- */

function TaskRow({ row, withTime }: { row: TodayRow; withTime?: boolean }) {
  const { dispatchUndoable } = useStore()
  return (
    <div className={`hall-task${row.past ? ' past' : ''}`}>
      {withTime && <span className="hall-time">{row.time}</span>}
      <button
        type="button"
        className="hall-check"
        aria-label={`做完了：${row.task.title}`}
        onClick={() => dispatchUndoable({ type: 'setTaskStatus', id: row.task.id, status: 'done' }, '做完了')}
      >
        <span className="ring" />
      </button>
      <Link to={`/t/${row.task.id}`} className="hall-task-body">
        <span className="h-title">{row.task.title}</span>
        <span className="h-note">{row.note}</span>
      </Link>
    </div>
  )
}

/** A reminder that went off: finish it with the circle, or close it with ×. */
function NoticeRow({ row }: { row: NoticeRowData }) {
  const { state, dispatchUndoable, applyState } = useStore()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const { notice, task } = row
  // A result says how the work ended; only a reminder has a time it came due.
  const note = notice.result
    ? `${notice.reason.split('\n')[0].replace(/：$/, '')} · ${formatWhen(notice.dueAt, state.settings.timezone ?? 'UTC')}`
    : [notice.reason && notice.reason !== notice.title ? notice.reason : '', `${formatWhen(notice.dueAt, state.settings.timezone ?? 'UTC')} 到点`].filter(Boolean).join(' · ')
  return (
    <div className="hall-task hall-rang">
      {task ? (
        <button
          type="button"
          className="hall-check"
          aria-label={`做完了：${task.title}`}
          onClick={() => dispatchUndoable({ type: 'setTaskStatus', id: task.id, status: 'done' }, '做完了')}
        >
          <span className="ring" />
        </button>
      ) : (
        <span className="hall-check" aria-hidden="true">
          <BellRing size={15} />
        </span>
      )}
      <Link to={`/t/${notice.thingId}`} className="hall-task-body">
        <span className="h-title">{notice.title}</span>
        <span className="h-note">{note}</span>
      </Link>
      <button
        type="button"
        className="hall-dismiss"
        aria-label={`关掉提醒：${notice.title}`}
        title="关掉这条提醒"
        disabled={busy}
        onClick={async () => {
          // Closing a reminder is not deleting anything, so it needs no confirmation.
          setBusy(true)
          try {
            applyState(await api<State>(`/v1/notify/notices/${encodeURIComponent(notice.id)}/dismiss`, undefined, 'POST'))
          } catch (e) {
            toast.show(e instanceof Error ? e.message : '没关掉，请重试')
          } finally {
            setBusy(false)
          }
        }}
      >
        <X size={15} />
      </button>
    </div>
  )
}

function NowLine({ timezone }: { timezone: string }) {
  const [now, setNow] = useState(() => new Date().toISOString())
  useEffect(() => {
    const t = window.setInterval(() => setNow(new Date().toISOString()), 60_000)
    return () => window.clearInterval(t)
  }, [])
  return (
    <div className="hall-now" role="separator" aria-label={`现在 ${clockTime(now, timezone)}`}>
      <span>现在</span>
      <i />
      <span>{clockTime(now, timezone)}</span>
    </div>
  )
}

const COMPACT_ROWS = 5

/**
 * 今天. On a phone it is the short version: what rang, who is waiting and
 * today's timeline, at most five rows; the rest is one tap away.
 */
function TodayWall({ compact }: { compact: boolean }) {
  const { state } = useStore()
  const timezone = state.settings.timezone ?? 'UTC'
  const full = todayColumn(state)
  const [all, setAll] = useState(false)
  const short = compact && !all
  const date = new Date().toLocaleDateString('zh-CN', { timeZone: timezone, month: 'long', day: 'numeric', weekday: 'long' })

  // Short, rows are kept in this order: what rang, who waits, what cannot wait, what is still ahead today, then what already passed.
  const cap = short ? COMPACT_ROWS : Infinity
  const rang = full.rang.slice(0, cap)
  const waiting = full.waiting.slice(0, cap - rang.length)
  const urgent = full.urgent.slice(0, cap - rang.length - waiting.length)
  const ahead = full.timeline.filter((r) => !r.past).slice(0, cap - rang.length - waiting.length - urgent.length)
  const passed = full.timeline.filter((r) => r.past).slice(0, cap - rang.length - waiting.length - urgent.length - ahead.length)
  const soon = short ? [] : full.soon
  const hidden = full.rang.length + full.waiting.length + full.urgent.length + full.timeline.length + full.soon.length - (rang.length + waiting.length + urgent.length + ahead.length + passed.length + soon.length)
  const nothing = full.rang.length + full.waiting.length + full.urgent.length + full.timeline.length + full.soon.length === 0

  return (
    <section className={`hall-panel hall-today${compact ? ' compact' : ''}`} aria-labelledby="hall-today-title">
      <header className="hall-head">
        <h1 id="hall-today-title">今天</h1>
        <span>{date}</span>
      </header>
      <div className="hall-scroll">
        {rang.length > 0 && (
          <div className="hall-group hall-rang-group">
            <h2 className="hall-sub rang">{rang.every((r) => r.notice.result) ? '做完了，等你看' : '到点了'}</h2>
            {rang.map((r) => (
              <NoticeRow key={r.notice.id} row={r} />
            ))}
          </div>
        )}
        {waiting.length > 0 && (
          <div className="hall-group">
            <h2 className="hall-sub warn">在等你，或已经晚了</h2>
            {waiting.map((r) => (
              <TaskRow key={r.task.id} row={r} />
            ))}
          </div>
        )}
        {(!short || urgent.length + ahead.length + passed.length > 0 || full.timeline.length + full.urgent.length === 0) && (
          <div className="hall-group">
            <h2 className="hall-sub">按时间</h2>
            {urgent.map((r) => (
              <TaskRow key={r.task.id} row={r} withTime />
            ))}
            {passed.map((r) => (
              <TaskRow key={r.task.id} row={r} withTime />
            ))}
            <NowLine timezone={timezone} />
            {ahead.map((r) => (
              <TaskRow key={r.task.id} row={r} withTime />
            ))}
            {full.timeline.length + full.urgent.length === 0 && <p className="hall-empty">今天没有定了时间的事。</p>}
          </div>
        )}
        {soon.length > 0 && (
          <div className="hall-group">
            <h2 className="hall-sub">这几天</h2>
            {soon.map((r) => (
              <TaskRow key={r.task.id} row={r} />
            ))}
          </div>
        )}
        {hidden > 0 ? (
          <button type="button" className="hall-more" onClick={() => setAll(true)}>
            还有 {hidden} 件
          </button>
        ) : (
          !compact && <p className="hall-foot">{nothing ? '今天没有要你动手的事。' : '更远的事临近时会自己进来。'}</p>
        )}
      </div>
    </section>
  )
}

/* ---------- Desk ---------- */

/** 叫号条: only what the user must move forward by hand. Hidden while there is none. */
function DecisionStrip() {
  const { state } = useStore()
  const { items, working } = decisionQueue(state)
  const [open, setOpen] = useState(false)
  const n = items.length
  if (n === 0) return null

  return (
    <div className="hall-queue">
      <button type="button" className="hall-queue-bar" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <span className="hall-badge on">{n}</span>
        <span className="h-label">{n} 件要你动手</span>
        {working > 0 && <span className="h-working">副手在做 {working} 件</span>}
        <span className="h-toggle">{open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}</span>
      </button>
      {open && (
        <ul className="hall-queue-list">
          {items.map((item) => (
            <li key={item.key}>
              <span className={`hall-tag ${item.kind}`}>{{ redo: '依据变了', handoff: '要转交' }[item.kind]}</span>
              <div className="grow">
                <div className="h-title">{item.title}</div>
                <div className="h-detail">{item.detail}</div>
              </div>
              {item.to && (
                <Link className="hall-btn" to={item.to}>
                  去看看
                </Link>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

/** 你不在的时候: one line under the conversation, opened on demand. Hidden while there is nothing. */
function AwayLine() {
  const { state } = useStore()
  const [seen] = useState(readSeen)
  const [open, setOpen] = useState(false)
  useEffect(() => {
    try {
      localStorage.setItem(SEEN_KEY, String(Date.now()))
    } catch {
      // Only affects which lines are marked new.
    }
  }, [])
  const items = backgroundFeed(state)
  if (items.length === 0) return null
  const fresh = items.some((f) => new Date(f.at).getTime() > seen)

  return (
    <section className={`hall-away${open ? ' open' : ''}`} aria-label="你不在的时候">
      <button type="button" className="hall-away-bar" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <span>你不在的时候：{items.length} 件</span>
        {fresh && <span className="h-new" aria-label="有新的" />}
        <span className="h-toggle">{open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}</span>
      </button>
      {open && (
        <ol className="hall-feed-list">
          {items.map((f) => {
            const body = (
              <>
                <span className="h-when">{formatAgo(f.at, state.settings.timezone ?? 'UTC')}</span>
                <span className={`h-text${f.failed ? ' failed' : ''}`}>{f.text}</span>
                {new Date(f.at).getTime() > seen && <span className="h-new" aria-label="新的" />}
              </>
            )
            return <li key={f.key}>{f.to ? <Link to={f.to}>{body}</Link> : <div>{body}</div>}</li>
          })}
        </ol>
      )}
    </section>
  )
}

function Desk({ compact }: { compact: boolean }) {
  return (
    <section className="hall-desk">
      <DecisionStrip />
      {/* On a wide screen the conversation fills the column in full; stacked, only its latest turn shows. */}
      <Secretary variant={compact ? 'latest' : undefined} />
      <AwayLine />
      <Link to="/about" className="hall-about">
        关于你
        <ChevronRight size={14} />
      </Link>
    </section>
  )
}

/* ---------- Projects and ideas ---------- */

function ProjectWall() {
  const { state } = useStore()
  const cards = projectCards(state)
  return (
    <section className="hall-panel hall-projects" aria-labelledby="hall-projects-title">
      <header className="hall-head small">
        <h2 id="hall-projects-title">项目</h2>
        <span>{cards.length > 2 ? '有新进展的在前，横划看更多' : ''}</span>
      </header>
      {cards.length === 0 ? (
        <p className="hall-empty">还没有项目。几件事聚在一起时，后台会提议合成一个。</p>
      ) : (
        <div className="hall-cards" role="list">
          {cards.map((c) => (
            <Link key={c.project.id} to={`/t/${c.project.id}`} className={`hall-card${c.fresh ? ' fresh' : ''}`} role="listitem">
              <span className="h-name">
                {c.project.name}
                {c.fresh && <span className="h-dot" aria-label="有新进展" />}
              </span>
              <span className="h-meta">
                {projectStatusLabel[c.project.status].text}
                {c.open > 0 && ` · ${c.open} 件在做`}
              </span>
              <span className="h-last">{c.last || '还没有进展记录'}</span>
              {c.next && <span className="h-next">下一步：{c.next}</span>}
            </Link>
          ))}
        </div>
      )}
    </section>
  )
}

function IdeaWall() {
  const { state, dispatchUndoable } = useStore()
  const { awake, drifting } = ideaWall(state)
  const flows = drifting.length >= 5
  const loop = flows ? [...drifting, ...drifting] : drifting
  const style = { '--flow-duration': `${Math.max(36, drifting.length * 7)}s` } as CSSProperties

  return (
    <section className="hall-panel hall-ideas" aria-labelledby="hall-ideas-title">
      <header className="hall-head small">
        <h2 id="hall-ideas-title">想法</h2>
        <span>{flows ? '闲时慢慢流过，指着它就停' : ''}</span>
      </header>
      {awake.slice(0, 2).map((idea) => (
        <div key={idea.id} className="hall-awake">
          <div className="h-top">
            <span className="h-flag">被唤醒</span>
            <Link to={`/t/${idea.id}`}>{idea.title}</Link>
          </div>
          <p>{idea.wake?.reason ?? '它等的条件满足了。'}</p>
          <div className="h-row">
            <button type="button" className="hall-btn solid" onClick={() => dispatchUndoable({ type: 'ideaPromote', id: idea.id }, '转成待办了')}>
              转成待办
            </button>
            <button type="button" className="hall-btn quiet" onClick={() => dispatchUndoable({ type: 'ideaSnooze', id: idea.id, days: 7 }, '好，再放一周')}>
              再放放
            </button>
          </div>
        </div>
      ))}
      {drifting.length === 0 && awake.length === 0 ? (
        <p className="hall-empty">还没有想法。在导办台随口说一句「要不……」就会记到这里。</p>
      ) : (
        <div className={`hall-flow${flows ? ' moving' : ''}`}>
          <ul className="h-track" style={style}>
            {loop.map((idea, i) => (
              <li key={`${idea.id}-${i}`} aria-hidden={i >= drifting.length || undefined}>
                <Link to={`/t/${idea.id}`} tabIndex={i >= drifting.length ? -1 : undefined}>
                  <span className="h-text">{idea.title}</span>
                  <span className="h-note">
                    {ideaNote(idea)} · {formatAgo(idea.updatedAt, state.settings.timezone ?? 'UTC')}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}

/** Below this width the hall stacks into one column (hall.css). */
const STACKED = '(max-width: 1180px)'

function useStacked(): boolean {
  const [stacked, setStacked] = useState(() => window.matchMedia(STACKED).matches)
  useEffect(() => {
    const query = window.matchMedia(STACKED)
    const onChange = () => setStacked(query.matches)
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [])
  return stacked
}

export function HallPage() {
  const stacked = useStacked()
  return (
    <div className="hall">
      <TimezoneHint />
      <TodayWall compact={stacked} />
      <Desk compact={stacked} />
      <div className="hall-right">
        <ProjectWall />
        <IdeaWall />
      </div>
    </div>
  )
}
