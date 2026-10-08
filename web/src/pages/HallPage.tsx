import { useEffect, useState, type CSSProperties, type ReactNode } from 'react'
import { Link } from 'react-router'
import { BellRing, CalendarClock, ChevronDown, ChevronRight, CircleAlert, CircleHelp, Repeat, X } from 'lucide-react'
import { SideSheet } from '../components/Overlay'
import { TimezoneHint } from '../components/TimezoneHint'
import { Secretary } from '../components/Secretary'
import { backgroundFeed, decisionQueue, firstRows, ideaNote, ideaWall, isDateRow, leadOf, projectCards, splitLate, todayColumn, todoOrder, type DateRow, type FeedItem, type NoticeRow as NoticeRowData, type TimeRow, type TodayColumn, type TodayRow } from '../domain/hall'
import { projectStatusLabel } from '../domain/labels'
import { entryKindLabel, type Schedule, type ScheduleEntry } from '../domain/schedule'
import { isOpenTask } from '../domain/things'
import { clockTime, formatAgo, formatCivil, formatDateTime, formatWhen } from '../domain/time'
import type { State } from '../domain/types'
import { api } from '../store/api'
import { useStore } from '../store/context'
import type { Read } from '../store/now'
import { useInProgress, useSchedule } from '../store/schedule'
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
        title="做完了"
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

const entryIcon = { deadline: CalendarClock, appointment: CalendarClock, recurring: Repeat, task: CalendarClock } as const

/** The circle on a deadline that went by: it was met. That is marked on the memory under it; no to-do is made. */
function FinishDate({ entry, children, onDone }: { entry: ScheduleEntry; children?: ReactNode; onDone?: () => void }) {
  const { dispatchUndoable } = useStore()
  return (
    <button
      type="button"
      className={children ? 'hall-finish' : 'hall-check'}
      aria-label={`做完了：${entry.title}`}
      title="做完了"
      onClick={async () => {
        if (await dispatchUndoable({ type: 'completeDeadline', id: entry.source.deadlineId ?? entry.id, memoryId: entry.source.memoryId }, '做完了')) onDone?.()
      }}
    >
      <span className="ring" />
      {children}
    </button>
  )
}

/**
 * 不要了, at the end of a date from the table of deadlines. The circle says it
 * was done; this says it is not wanted here, and the row says so in words when
 * pointed at. It is marked on the memory under it, like 做完了, so every day it
 * would fall on goes with it; the memory itself stays, and the toast takes it back.
 */
function DropDate({ entry, onDone }: { entry: ScheduleEntry; onDone?: () => void }) {
  const { dispatchUndoable } = useStore()
  if (entry.source.kind !== 'deadline' || !(entry.source.deadlineId ?? entry.id)) return null
  return (
    <button
      type="button"
      className="hall-drop"
      aria-label={`不要了：${entry.title}`}
      title="不要了，别再显示"
      onClick={async () => {
        if (await dispatchUndoable({ type: 'completeDeadline', id: entry.source.deadlineId ?? entry.id, memoryId: entry.source.memoryId, reason: 'dropped' }, '不再显示了')) onDone?.()
      }}
    >
      <span className="h-word">不要了</span>
      <X size={14} />
    </button>
  )
}

/** A date from the table of deadlines. It opens onto what was said; a to-do opens its own page. */
function DateRowView({ row, withTime, unclear, onOpen }: { row: DateRow; withTime?: boolean; unclear?: boolean; onOpen: (row: DateRow) => void }) {
  const { entry } = row
  const Icon = unclear ? CircleHelp : entryIcon[entry.kind]
  const body = (
    <>
      <span className="h-title">{entry.title}</span>
      <span className="h-note">{row.note}</span>
    </>
  )
  return (
    <div className={`hall-task${row.past ? ' past' : ''}`}>
      {withTime && <span className="hall-time">{row.time}</span>}
      {row.canFinish ? (
        <FinishDate entry={entry} />
      ) : (
        <span className="hall-check hall-mark" aria-hidden="true">
          <Icon size={15} />
        </span>
      )}
      {entry.source.kind === 'task' && entry.source.itemId ? (
        <Link to={`/t/${entry.source.itemId}`} className="hall-task-body">
          {body}
        </Link>
      ) : (
        <button type="button" className="hall-task-body" onClick={() => onOpen(row)}>
          {body}
        </button>
      )}
      <DropDate entry={entry} />
    </div>
  )
}

/** What a date rests on: when it is, what was said, and the memory it was read from. */
function DateSheet({ row, onClose }: { row: DateRow; onClose: () => void }) {
  const timezone = useStore().state.settings.timezone ?? 'UTC'
  const { entry } = row
  const when = entry.at && !entry.dateOnly ? formatDateTime(entry.at, timezone) : entry.date ? formatCivil(entry.date, timezone) : '日期没说清'
  return (
    <SideSheet title={entry.title} onClose={onClose} top={<span className="tiny muted">{entryKindLabel[entry.kind]} · {when}</span>}>
      <div className="stack">
        {entry.timeNote && <p className="note">{entry.timeNote}</p>}
        {entry.originalText && (
          <div className="stack-sm">
            <span className="tiny muted">原话</span>
            <pre className="source-text">{entry.originalText}</pre>
          </div>
        )}
        {row.canFinish && (
          <FinishDate entry={entry} onDone={onClose}>
            <span>做完了</span>
          </FinishDate>
        )}
        {entry.source.memoryId && (
          <Link to={`/library?m=${encodeURIComponent(entry.source.memoryId)}`} onClick={onClose}>
            看这条记忆和它的来源
          </Link>
        )}
      </div>
    </SideSheet>
  )
}

/** Rows of one kind that can run long: the first few, and the rest one click away. */
function Capped<T>({ rows, children }: { rows: T[]; children: (row: T) => ReactNode }) {
  const [all, setAll] = useState(false)
  return (
    <>
      {(all ? rows : rows.slice(0, ROWS_SHOWN)).map(children)}
      {rows.length > ROWS_SHOWN && (
        <button type="button" className="link-btn hall-rest" aria-expanded={all} onClick={() => setAll((v) => !v)}>
          {all ? '收起' : `还有 ${rows.length - ROWS_SHOWN} 条`}
        </button>
      )}
    </>
  )
}

/** A reminder that went off: finish it with the circle, or close it with ×. */
function NoticeRow({ row }: { row: NoticeRowData }) {
  const { state, dispatchUndoable, applyState } = useStore()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const { notice, task } = row
  // A result says how the work ended; only a reminder has a time it came due.
  // The deputy handed something back; the to-do itself is not thereby done, so the row names what came back.
  const back = notice.reason.split('\n').slice(1).map((l) => l.replace(/^[#>*\s-]+/, '').replace(/\*+/g, '').trim()).find(Boolean)
  const note = notice.result
    ? `${back ? `副手交回：${back}` : '副手交回了结果'} · ${formatWhen(notice.dueAt, state.settings.timezone ?? 'UTC')}`
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
/** Dates gone by and dates never pinned down can pile up over the years; this many show before 「还有 N 条」, as in the library's 眼下. */
const ROWS_SHOWN = 5

/**
 * 今天. On a phone it is the short version: what rang, who is waiting and
 * today's timeline, at most five rows; the rest is one tap away.
 */
function TodayWall({ compact, schedule, full }: { compact: boolean; schedule: Read<Schedule>; full: TodayColumn }) {
  const { state } = useStore()
  const timezone = state.settings.timezone ?? 'UTC'
  const [all, setAll] = useState(false)
  const [old, setOld] = useState(false)
  const [open, setOpen] = useState<DateRow>()
  const short = compact && !all
  const date = new Date().toLocaleDateString('zh-CN', { timeZone: timezone, month: 'long', day: 'numeric', weekday: 'long' })

  const { rang, waiting, late, urgent, ahead, passed } = firstRows(full, short ? COMPACT_ROWS : Infinity)
  // A deadline from more than a week back is no longer news; it waits behind one line.
  const { recent, older } = splitLate(late, timezone)
  const soon = short ? [] : full.soon
  const unclear = short ? [] : full.unclear
  const total = full.rang.length + full.waiting.length + full.late.length + full.urgent.length + full.timeline.length + full.soon.length + full.unclear.length
  const hidden = total - (rang.length + waiting.length + late.length + urgent.length + ahead.length + passed.length + soon.length + unclear.length)
  const timed = (r: TimeRow) => (isDateRow(r) ? <DateRowView key={r.entry.id} row={r} withTime onOpen={setOpen} /> : <TaskRow key={r.task.id} row={r} withTime />)

  return (
    <section className={`hall-panel hall-today${compact ? ' compact' : ''}`} aria-labelledby="hall-today-title">
      <header className="hall-head">
        <h1 id="hall-today-title">今天</h1>
        <span>{date}</span>
      </header>
      <div className="hall-scroll">
        {rang.length > 0 && (
          <div className="hall-group hall-rang-group">
            <h2 className="hall-sub rang">{rang.every((r) => r.notice.result) ? '副手交回了，等你看' : '到点了'}</h2>
            {rang.map((r) => (
              <NoticeRow key={r.notice.id} row={r} />
            ))}
          </div>
        )}
        {waiting.length > 0 && (
          <div className="hall-group">
            <h2 className="hall-sub warn">在等你</h2>
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
            {passed.map(timed)}
            <NowLine timezone={timezone} />
            {ahead.map(timed)}
            {full.timeline.length + full.urgent.length === 0 && <p className="hall-empty">今天没有定了时间的事。</p>}
          </div>
        )}
        {soon.length + unclear.length > 0 && (
          <div className="hall-group">
            <h2 className="hall-sub">这几天</h2>
            {soon.map((r) => (isDateRow(r) ? <DateRowView key={r.entry.id} row={r} onOpen={setOpen} /> : <TaskRow key={r.task.id} row={r} />))}
            {unclear.length > 0 && (
              <div className="hall-unclear" role="group" aria-label="日期没说清的">
                <h3 className="hall-sub">日期没说清的</h3>
                <Capped rows={unclear}>{(r) => <DateRowView key={r.entry.id} row={r} unclear onOpen={setOpen} />}</Capped>
              </div>
            )}
          </div>
        )}
        {late.length > 0 && (
          <div className="hall-group hall-late">
            <h2 className="hall-sub">之前没了结的</h2>
            <Capped rows={recent}>{(r) => <DateRowView key={r.entry.id} row={r} onOpen={setOpen} />}</Capped>
            {older.length > 0 && (
              <button type="button" className="link-btn hall-rest" aria-expanded={old} onClick={() => setOld((v) => !v)}>
                {old ? '收起更早的' : `更早的 ${older.length} 条`}
              </button>
            )}
            {old && older.map((r) => <DateRowView key={r.entry.id} row={r} onOpen={setOpen} />)}
          </div>
        )}
        {schedule.phase === 'failed' && (
          <p className="hall-problem" role="alert">
            <CircleAlert size={14} />
            <span>期限和固定安排没读出来，上面只有待办：{schedule.problem}</span>
            <button type="button" className="link-btn" onClick={schedule.retry}>
              重试
            </button>
          </p>
        )}
        {hidden > 0 ? (
          <button type="button" className="hall-more" onClick={() => setAll(true)}>
            还有 {hidden} 件
          </button>
        ) : (
          !compact && <p className="hall-foot">{total === 0 ? '今天没有要你动手的事。' : '更远的事临近时会自己进来。'}</p>
        )}
      </div>
      {open && <DateSheet row={open} onClose={() => setOpen(undefined)} />}
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

function FeedRow({ item, fresh }: { item: FeedItem; fresh: boolean }) {
  const { state, undo } = useStore()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const when = <span className="h-when">{formatAgo(item.at, state.settings.timezone ?? 'UTC')}</span>
  const text = <span className={`h-text${item.failed ? ' failed' : ''}`}>{item.text}</span>
  const dot = fresh && <span className="h-new" aria-label="新的" />
  if (item.items?.length) return <FeedGroup item={item} when={when} dot={dot} />
  if (!item.actionId) {
    const body = (
      <>
        {when}
        {text}
        {dot}
      </>
    )
    return <li>{item.to ? <Link to={item.to}>{body}</Link> : <div>{body}</div>}</li>
  }
  const actionId = item.actionId
  return (
    <li>
      <div>
        {when}
        {item.to ? (
          <Link to={item.to} className="h-open">
            {text}
          </Link>
        ) : (
          text
        )}
        {dot}
        <button
          type="button"
          className="hall-btn quiet hall-undo"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            // Undone, the thing is gone from the workspace and this line goes with it.
            if (await undo(actionId)) toast.show('撤销了')
            else setBusy(false)
          }}
        >
          撤销
        </button>
      </div>
    </li>
  )
}

/** One line for several things done at once (the dates put away); it opens onto each, with why and its own undo. */
function FeedGroup({ item, when, dot }: { item: FeedItem; when: ReactNode; dot: ReactNode }) {
  const { undo } = useStore()
  const toast = useToast()
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState('')
  return (
    <li>
      <div>
        {when}
        <button type="button" className="h-open hall-group-toggle" aria-expanded={open} onClick={() => setOpen(!open)}>
          <span className="h-text">{item.text}</span>
          {open ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
        </button>
        {dot}
      </div>
      {open && (
        <ul className="hall-feed-items">
          {item.items!.map((it) => (
            <li key={it.actionId}>
              <span className="h-text">
                {it.text}
                {it.note && <span className="h-why"> · {it.note}</span>}
              </span>
              <button
                type="button"
                className="hall-btn quiet hall-undo"
                disabled={busy === it.actionId}
                aria-label={`放回去：${it.text}`}
                onClick={async () => {
                  setBusy(it.actionId)
                  if (await undo(it.actionId)) toast.show('放回去了')
                  else setBusy('')
                }}
              >
                放回去
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}

/** How many lines of 你不在的时候 show before it is opened. */
const AWAY_SHOWN = 3

/** 你不在的时候: right under the desk, its newest lines showing; the rest opens. Hidden while there is nothing. */
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
        {items.length > AWAY_SHOWN && <span className="h-toggle">{open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}</span>}
      </button>
      <ol className="hall-feed-list">
        {(open ? items : items.slice(0, AWAY_SHOWN)).map((f) => (
          <FeedRow key={f.key} item={f} fresh={new Date(f.at).getTime() > seen} />
        ))}
      </ol>
    </section>
  )
}

/** How long until an instant, in the words a person would use. */
function untilText(at: string, now: number): string {
  const minutes = Math.round((new Date(at).getTime() - now) / 60_000)
  if (minutes <= 0) return '到点了'
  if (minutes < 60) return `还有 ${minutes} 分钟`
  const hours = Math.floor(minutes / 60)
  return hours < 24 ? `还有 ${hours} 小时` : ''
}

/** 最要紧: the one thing to look at first, in the middle of the page above the input. */
function LeadCard({ full }: { full: TodayColumn }) {
  const timezone = useStore().state.settings.timezone ?? 'UTC'
  const [now, setNow] = useState(() => Date.now())
  const [open, setOpen] = useState<DateRow>()
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(t)
  }, [])
  const lead = leadOf(full)
  const date = new Date(now).toLocaleDateString('zh-CN', { timeZone: timezone, month: 'long', day: 'numeric', weekday: 'long' })
  if (!lead) {
    return (
      <section className="hall-lead quiet" aria-label="最要紧">
        <span className="h-day">{date}</span>
        <p className="h-title">今天没有赶时间的事。</p>
      </section>
    )
  }
  const body = (
    <>
      <span className="h-title">{lead.title}</span>
      <span className="h-note">{[lead.at && lead.kind === 'next' ? `今天 ${clockTime(lead.at, timezone)}` : '', lead.note, lead.at && lead.kind === 'next' ? untilText(lead.at, now) : ''].filter(Boolean).join(' · ')}</span>
    </>
  )
  return (
    <section className={`hall-lead ${lead.kind}`} aria-label="最要紧">
      <span className="h-day">{date} · 最要紧</span>
      {lead.to ? (
        <Link to={lead.to} className="h-body">
          {body}
        </Link>
      ) : (
        <button type="button" className="h-body" onClick={() => lead.row && setOpen(lead.row)}>
          {body}
        </button>
      )}
      {open && <DateSheet row={open} onClose={() => setOpen(undefined)} />}
    </section>
  )
}

function Desk({ compact, full }: { compact: boolean; full: TodayColumn }) {
  return (
    <section className="hall-desk">
      <LeadCard full={full} />
      <DecisionStrip />
      {/* On a wide screen the conversation shows in full above the input, up to a height; stacked, only its latest turn shows. */}
      <Secretary variant={compact ? 'latest' : undefined} />
      <AwayLine />
    </section>
  )
}

/* ---------- Projects and ideas ---------- */

/** Project cards that show before 「还有 N 个」; they wrap, nothing is scrolled sideways. */
const PROJECTS_SHOWN = 6

function ProjectWall() {
  const { state } = useStore()
  const cards = projectCards(state)
  const [all, setAll] = useState(false)
  return (
    <section className="hall-panel hall-projects" aria-labelledby="hall-projects-title">
      <header className="hall-head small">
        <h2 id="hall-projects-title">项目</h2>
        <span>{cards.length > 1 ? '有新进展的在前' : ''}</span>
      </header>
      {cards.length === 0 ? (
        <p className="hall-empty">还没有项目。几件事聚在一起时，后台会提议合成一个。</p>
      ) : (
        <div className="hall-cards" role="list">
          {(all ? cards : cards.slice(0, PROJECTS_SHOWN)).map((c) => (
            <Link key={c.project.id} to={`/t/${c.project.id}`} className={`hall-card${c.fresh ? ' fresh' : ''}`} role="listitem">
              <span className="h-name">
                {c.project.name}
                {c.fresh && <span className="h-dot" aria-label="有新进展" />}
              </span>
              <span className="h-meta">
                {projectStatusLabel[c.project.status].text}
                {c.open > 0 && ` · ${c.open} 件在做`}
              </span>
              {c.next ? <span className="h-last">下一步：{c.next}</span> : c.goal && <span className="h-last">{c.goal}</span>}
            </Link>
          ))}
          {cards.length > PROJECTS_SHOWN && (
            <button type="button" className="link-btn hall-rest" aria-expanded={all} onClick={() => setAll((v) => !v)}>
              {all ? '收起' : `还有 ${cards.length - PROJECTS_SHOWN} 个`}
            </button>
          )}
        </div>
      )}
    </section>
  )
}

/**
 * 待办: to-dos with no time; whatever has a time is under 今天. Nothing here
 * reminds. What the user said cannot wait is on top, what the background made
 * on its own is last and says so; how many are listed is the server's to say.
 */
function ProgressWall() {
  const { state, dispatchUndoable } = useStore()
  const read = useInProgress()
  const timezone = state.settings.timezone ?? 'UTC'
  if (read.value === undefined) {
    // Still being read, it takes no room; unread, it says why.
    if (read.phase !== 'failed') return null
    return (
      <section className="hall-panel hall-progress" aria-labelledby="hall-progress-title">
        <header className="hall-head small">
          <h2 id="hall-progress-title">待办</h2>
        </header>
        <p className="hall-problem" role="alert">
          <CircleAlert size={14} />
          <span>没读出来：{read.problem}</span>
          <button type="button" className="link-btn" onClick={read.retry}>
            重试
          </button>
        </p>
      </section>
    )
  }
  // One just finished here is gone at once, before the list is read again.
  const items = todoOrder(read.value.items.filter((t) => isOpenTask(state.tasks.find((s) => s.id === t.id) ?? t)))
  if (items.length === 0 && read.value.remaining === 0) return null
  return (
    <section className="hall-panel hall-progress" aria-labelledby="hall-progress-title">
      <header className="hall-head small">
        <h2 id="hall-progress-title">待办</h2>
        <span>没定时间的</span>
      </header>
      <div className="hall-scroll">
        {items.map((task) => {
          const project = state.projects.find((p) => p.id === task.projectId)?.name
          return (
            <div key={task.id} className="hall-task">
              <button
                type="button"
                className="hall-check"
                aria-label={`做完了：${task.title}`}
                title="做完了"
                onClick={() => dispatchUndoable({ type: 'setTaskStatus', id: task.id, status: 'done' }, '做完了')}
              >
                <span className="ring" />
              </button>
              <Link to={`/t/${task.id}`} className="hall-task-body">
                <span className="h-title">{task.title}</span>
                <span className="h-note">{[task.urgent ? '你说过要尽快' : '', project, task.creation && task.creation.by !== 'secretary' ? '后台建的' : '', formatAgo(task.updatedAt, timezone)].filter(Boolean).join(' · ')}</span>
              </Link>
            </div>
          )
        })}
        {read.value.remaining > 0 && <p className="hall-foot">还有 {read.value.remaining} 件没列出来，在各自的项目里，搜索也能找到。</p>}
      </div>
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

/** Below this width the three columns under the desk stack into one (hall.css). */
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
  const { state } = useStore()
  // Read once: the lead on top and the column of today stand on the same days.
  const schedule = useSchedule()
  const full = todayColumn(state, schedule.value)
  return (
    <div className="hall">
      <TimezoneHint />
      <Desk compact={stacked} full={full} />
      {/* Side by side on a wide screen; a part with nothing in it takes no column. */}
      <div className="hall-below">
        <TodayWall compact={stacked} schedule={schedule} full={full} />
        <ProgressWall />
        <div className="hall-right">
          <ProjectWall />
          <IdeaWall />
        </div>
      </div>
    </div>
  )
}
