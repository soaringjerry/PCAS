import { useEffect, useState, type CSSProperties } from 'react'
import { Link } from 'react-router'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { Secretary } from '../components/Secretary'
import { UnsureSheet } from '../components/UnsureSheet'
import { backgroundFeed, decisionQueue, ideaNote, ideaWall, projectCards, todayColumn, type TodayRow } from '../domain/hall'
import { projectStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
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

function clock(iso: string): string {
  return new Date(iso).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
}

/* ---------- Today ---------- */

function TaskRow({ row, withTime }: { row: TodayRow; withTime?: boolean }) {
  const { dispatch } = useStore()
  const toast = useToast()
  return (
    <div className={`hall-task${row.past ? ' past' : ''}`}>
      {withTime && <span className="hall-time">{row.time}</span>}
      <button
        type="button"
        className="hall-check"
        aria-label={`做完了：${row.task.title}`}
        onClick={async () => {
          if (await dispatch({ type: 'setTaskStatus', id: row.task.id, status: 'done' })) toast.show('做完了')
        }}
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

function NowLine() {
  const [now, setNow] = useState(() => new Date().toISOString())
  useEffect(() => {
    const t = window.setInterval(() => setNow(new Date().toISOString()), 60_000)
    return () => window.clearInterval(t)
  }, [])
  return (
    <div className="hall-now" role="separator" aria-label={`现在 ${clock(now)}`}>
      <span>现在</span>
      <i />
      <span>{clock(now)}</span>
    </div>
  )
}

function TodayWall() {
  const { state } = useStore()
  const { waiting, timeline, soon } = todayColumn(state)
  const before = timeline.filter((r) => r.past)
  const after = timeline.filter((r) => !r.past)
  const count = waiting.length + timeline.length
  const date = new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })

  return (
    <section className="hall-panel hall-today" aria-labelledby="hall-today-title">
      <header className="hall-head">
        <h1 id="hall-today-title">今天</h1>
        <span>{date}</span>
      </header>
      <div className="hall-scroll">
        {waiting.length > 0 && (
          <div className="hall-group">
            <h2 className="hall-sub warn">在等你，或已经晚了</h2>
            {waiting.map((r) => (
              <TaskRow key={r.task.id} row={r} />
            ))}
          </div>
        )}
        <div className="hall-group">
          <h2 className="hall-sub">按时间</h2>
          {before.map((r) => (
            <TaskRow key={r.task.id} row={r} withTime />
          ))}
          <NowLine />
          {after.map((r) => (
            <TaskRow key={r.task.id} row={r} withTime />
          ))}
          {timeline.length === 0 && <p className="hall-empty">今天没有定了时间的事。</p>}
        </div>
        {soon.length > 0 && (
          <div className="hall-group">
            <h2 className="hall-sub">这几天</h2>
            {soon.map((r) => (
              <TaskRow key={r.task.id} row={r} />
            ))}
          </div>
        )}
        <p className="hall-foot">{count === 0 && soon.length === 0 ? '今天没有要你动手的事。' : '更远的事临近时会自己进来。'}</p>
      </div>
    </section>
  )
}

/* ---------- Desk ---------- */

function DecisionStrip() {
  const { state } = useStore()
  const { items, working } = decisionQueue(state)
  const [open, setOpen] = useState(false)
  const [unsure, setUnsure] = useState(false)
  const n = items.length

  return (
    <div className="hall-queue">
      {unsure && <UnsureSheet onClose={() => setUnsure(false)} />}
      <button type="button" className="hall-queue-bar" onClick={() => setOpen((v) => !v)} aria-expanded={open} disabled={n === 0}>
        <span className={`hall-badge${n ? ' on' : ''}`}>{n}</span>
        <span className="h-label">{n ? `${n} 件等你拍板` : '没有要你拍板的事'}</span>
        {working > 0 && <span className="h-working">副手在做 {working} 件</span>}
        {n > 0 && <span className="h-toggle">{open ? <ChevronDown size={16} /> : <ChevronRight size={16} />}</span>}
      </button>
      {open && n > 0 && (
        <ul className="hall-queue-list">
          {items.map((item) => (
            <li key={item.key}>
              <span className={`hall-tag ${item.kind}`}>{{ result: '草稿', handoff: '要转交', unsure: '拿不准' }[item.kind]}</span>
              <div className="grow">
                <div className="h-title">{item.title}</div>
                <div className="h-detail">{item.detail}</div>
              </div>
              {item.to ? (
                <Link className="hall-btn" to={item.to}>
                  去看看
                </Link>
              ) : (
                <button type="button" className="hall-btn" onClick={() => setUnsure(true)}>
                  逐条确认
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function Desk() {
  return (
    <section className="hall-desk">
      <DecisionStrip />
      <Secretary />
    </section>
  )
}

/* ---------- Feed ---------- */

function Feed() {
  const { state } = useStore()
  const [seen] = useState(readSeen)
  useEffect(() => {
    try {
      localStorage.setItem(SEEN_KEY, String(Date.now()))
    } catch {
      // Only affects which lines are marked new.
    }
  }, [])
  const items = backgroundFeed(state)

  return (
    <section className="hall-panel hall-feed" aria-labelledby="hall-feed-title">
      <header className="hall-head small">
        <h2 id="hall-feed-title">你不在的时候，我做了这些</h2>
        <Link to="/library?tab=sources">导入资料</Link>
      </header>
      <div className="hall-scroll">
        {items.length === 0 ? (
          <p className="hall-empty">最近两天后台没什么动静。接入更多资料后，这里会记下它整理、唤醒和做好的事。</p>
        ) : (
          <ol className="hall-feed-list">
            {items.map((f) => {
              const fresh = new Date(f.at).getTime() > seen
              const body = (
                <>
                  <span className="h-when">{formatAgo(f.at)}</span>
                  <span className={`h-text${f.failed ? ' failed' : ''}`}>{f.text}</span>
                  {fresh && <span className="h-new" aria-label="新的" />}
                </>
              )
              return <li key={f.key}>{f.to ? <Link to={f.to}>{body}</Link> : <div>{body}</div>}</li>
            })}
          </ol>
        )}
      </div>
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
  const { state, dispatch } = useStore()
  const toast = useToast()
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
            <button type="button" className="hall-btn solid" onClick={() => dispatch({ type: 'ideaPromote', id: idea.id })}>
              转成待办
            </button>
            <button
              type="button"
              className="hall-btn quiet"
              onClick={async () => {
                if (await dispatch({ type: 'ideaSnooze', id: idea.id, days: 7 })) toast.show('好，再放一周')
              }}
            >
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
                    {ideaNote(idea)} · {formatAgo(idea.updatedAt)}
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

export function HallPage() {
  return (
    <div className="hall">
      <TodayWall />
      <Desk />
      <Feed />
      <div className="hall-right">
        <ProjectWall />
        <IdeaWall />
      </div>
    </div>
  )
}
