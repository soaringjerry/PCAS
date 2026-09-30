import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { Link } from 'react-router'
import { ArrowUp, Check, ChevronDown, ChevronRight, FileText, MoreHorizontal, Search, Sparkles, X } from 'lucide-react'
import { RecallSheet } from '../components/RecallSheet'
import { SourceSheet } from '../components/SourceSheet'
import { UnsureSheet } from '../components/UnsureSheet'
import {
  backgroundFeed,
  decisionQueue,
  ideaNote,
  ideaWall,
  looksLikeQuestion,
  looksLikeRequest,
  projectCards,
  todayColumn,
  type TodayRow,
} from '../domain/hall'
import { projectStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import { api } from '../store/api'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
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

interface DeskReply {
  answer: string
  agent: string
  used: { ref: { id: string; version: number }; text: string }[]
  searches: string[]
  links: string[]
}
/** One exchange on the answer card; follow-ups add turns until the card is closed. */
type Turn = { q: string; busy?: boolean; reply?: DeskReply; error?: string }
type Intent = 'ask' | 'record' | 'delegate'
type Receipt = { key: number; text: string; hint: string; to?: string }

/** Jev decides where an entry goes; without it (or when it fails) a local rule does. */
async function route(q: string): Promise<{ intent: Intent; sure: boolean }> {
  try {
    const r = await api<{ intent: Intent; confidence: number }>('/v1/desk/route', { text: q })
    // Starting the assistant spends budget and shares memories with it, so it needs a clearer call.
    return { intent: r.intent, sure: r.confidence >= (r.intent === 'delegate' ? 0.8 : 0.6) }
  } catch {
    return { intent: looksLikeRequest(q) ? 'delegate' : looksLikeQuestion(q) ? 'ask' : 'record', sure: false }
  }
}

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

function TurnBody({ turn, onSource }: { turn: Turn; onSource: (s: { id: string; version?: number }) => void }) {
  const { state } = useStore()
  const reply = turn.reply
  if (turn.busy) return <p className="hall-answer-body h-muted">正在翻记录、想怎么回答…</p>
  if (!reply) return <p className="hall-answer-body h-muted">{turn.error}</p>
  return (
    <>
      <p className="hall-answer-body">{reply.answer}</p>
      {reply.used.length > 0 && (
        <ul className="hall-answer-excerpts">
          {reply.used.map((u) => {
            // The record behind a memory, when there is one, opens as the source.
            const origin = state.memories.find((m) => m.id === u.ref.id)?.sources[0]
            return (
              <li key={u.ref.id}>
                {origin ? (
                  <button type="button" className="hall-cite" onClick={() => onSource({ id: origin.sourceId, version: origin.version })}>
                    {u.text}
                    <FileText size={12} />
                  </button>
                ) : (
                  u.text
                )}
              </li>
            )
          })}
        </ul>
      )}
      {reply.links.length > 0 && (
        <ul className="hall-answer-links">
          {reply.links.map((link) => (
            <li key={link}>
              <a href={link} target="_blank" rel="noopener noreferrer">
                {new URL(link).hostname}
              </a>
            </li>
          ))}
        </ul>
      )}
    </>
  )
}

function AnswerCard({ thread, onClose, onDeeper, onFile, onDelegate }: { thread: Turn[]; onClose: () => void; onDeeper: () => void; onFile: () => void; onDelegate: () => void }) {
  const [source, setSource] = useState<{ id: string; version?: number } | null>(null)
  const [more, setMore] = useState(false)
  const card = useRef<HTMLDivElement>(null)
  const last = thread[thread.length - 1]
  // On a phone the desk input sits at the bottom; bring the answer into view.
  useEffect(() => {
    card.current?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  }, [thread.length, last.busy])

  return (
    <div className="hall-answer" aria-live="polite" ref={card}>
      {source && <SourceSheet id={source.id} version={source.version} onClose={() => setSource(null)} />}
      <div className="hall-answer-head">
        <span>你问：{thread[0].q}</span>
        <button type="button" className="hall-icon-btn" aria-label="结束这次问答" onClick={onClose}>
          <X size={16} />
        </button>
      </div>
      {thread.map((turn, i) => (
        <div key={i} className="hall-turn">
          {i > 0 && <p className="hall-turn-q">{turn.q}</p>}
          <TurnBody turn={turn} onSource={setSource} />
        </div>
      ))}
      {!last.busy && (
        <div className="hall-answer-foot">
          {last.reply && (
            <span className="h-note">
              {last.reply.agent} 回答{last.reply.searches.length > 0 && ` · 联网查了「${last.reply.searches.join('」「')}」`}
            </span>
          )}
          <span className="grow" />
          {more ? (
            <>
              <button type="button" className="hall-link" onClick={onFile}>
                不是问题，记下来
              </button>
              <button type="button" className="hall-link" onClick={onDelegate}>
                <Sparkles size={13} />
                交给副手
              </button>
              <button type="button" className="hall-link" onClick={onDeeper}>
                <Search size={13} />
                翻完整历史
              </button>
            </>
          ) : (
            <button type="button" className="hall-icon-btn" aria-label="更多操作" onClick={() => setMore(true)}>
              <MoreHorizontal size={16} />
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function PickCard({ q, onPick, onCancel }: { q: string; onPick: (intent: Intent) => void; onCancel: () => void }) {
  return (
    <div className="hall-answer hall-pick" role="group" aria-label="这句要怎么处理">
      <div className="hall-answer-head">
        <span>请选择这句话要怎么处理：{q}</span>
        <button type="button" className="hall-icon-btn" aria-label="放回输入框" onClick={onCancel}>
          <X size={16} />
        </button>
      </div>
      <div className="hall-answer-foot">
        <button type="button" className="hall-chip" onClick={() => onPick('ask')}>
          <Search size={12} />
          问一下
        </button>
        <button type="button" className="hall-chip" onClick={() => onPick('record')}>
          <Check size={12} />
          记下来
        </button>
        <button type="button" className="hall-chip" onClick={() => onPick('delegate')}>
          <Sparkles size={12} />
          交给副手
        </button>
      </div>
    </div>
  )
}

function Desk() {
  const { dispatch } = useStore()
  const { agentFor } = useShell()
  const [text, setText] = useState('')
  const [routing, setRouting] = useState(false)
  const [receipt, setReceipt] = useState<Receipt | null>(null)
  const [thread, setThread] = useState<Turn[] | null>(null)
  const [pick, setPick] = useState<string | null>(null)
  const [deeper, setDeeper] = useState<string | null>(null)
  const asked = useRef(0)
  const input = useRef<HTMLInputElement>(null)
  // While an answer card is open, the next line continues that conversation.
  const following = thread !== null && !thread[thread.length - 1].busy

  const close = () => {
    asked.current++ // an answer still in flight must not reopen the card
    setThread(null)
  }

  const file = async (q: string) => {
    if (!(await dispatch({ type: 'capture', text: q }))) return false
    close()
    setReceipt({ key: Date.now(), text: '记下了，后台会整理', hint: '它会自己放进今天、项目或想法；拿不准的会出现在上面。' })
    return true
  }

  const ask = async (q: string, follow = false) => {
    const ticket = ++asked.current
    const before = follow && thread ? thread : []
    const history = before.flatMap((t) => (t.reply ? [{ q: t.q, a: '' }] : [])).slice(-6)
    setReceipt(null)
    setThread([...before, { q, busy: true }])
    const done = (turn: Turn) => {
      if (ticket !== asked.current) return
      setThread([...before, turn])
      input.current?.focus()
    }
    const agentId = agentFor('desk')
    if (agentId === 'manual') {
      done({ q, error: '没有能直接回答的副手。去设置里启用一个，或者翻完整历史自己找。' })
      return true
    }
    try {
      done({ q, reply: await api<DeskReply>('/v1/desk/answer', { question: q, agentId, history }) })
    } catch (e) {
      done({ q, error: e instanceof Error ? e.message : '没答上来，请稍后再问一次。' })
    }
    return true
  }

  // Creating an item does not itself authorize a paid model run.
  const delegate = async (q: string, prompt = q) => {
    const id = crypto.randomUUID()
    const title = q.length > 60 ? `${q.slice(0, 60)}…` : q
    if (!(await dispatch({ type: 'addTask', id, title, text: prompt }))) return false
    close()
    setReceipt({ key: Date.now(), text: '建好了事项', hint: '打开事项确认副手、上下文和费用后再开始。', to: `/t/${id}` })
    return true
  }

  const go = (intent: Intent, q: string) => {
    setPick(null)
    return { ask, record: file, delegate }[intent](q)
  }

  const submit = async () => {
    const q = text.trim()
    if (!q || routing) return
    if (following) {
      setText('')
      await ask(q, true)
      return
    }
    setRouting(true)
    const { intent, sure } = await route(q)
    setRouting(false)
    setText('')
    if (!sure || intent === 'delegate') {
      close()
      setReceipt(null)
      setPick(q)
      return
    }
    if (!(await go(intent, q))) setText(q)
  }

  return (
    <section className="hall-desk">
      {deeper !== null && <RecallSheet query={deeper} onClose={() => setDeeper(null)} />}
      <DecisionStrip />
      <form
        className="hall-input"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <label htmlFor="hall-desk-input">导办台</label>
        <div className="h-row">
          <input
            ref={input}
            id="hall-desk-input"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape' && thread) close()
            }}
            placeholder={following ? '接着问，或按 Esc 结束这次问答' : '记一件事、问一句话、让副手做点什么，都在这'}
            autoComplete="off"
            enterKeyHint="send"
          />
          <button type="submit" className="hall-send" aria-label={following ? '接着问' : '交给她'} disabled={!text.trim() || routing || (thread !== null && !following)}>
            <ArrowUp size={18} strokeWidth={2.5} />
          </button>
        </div>
      </form>
      {pick !== null && (
        <PickCard
          q={pick}
          onPick={(intent) => void go(intent, pick)}
          onCancel={() => {
            setText(pick)
            setPick(null)
          }}
        />
      )}
      {receipt && !thread && pick === null && (
        <p key={receipt.key} className="hall-receipt" role="status">
          <Check size={15} strokeWidth={2.6} />
          <span>{receipt.text}</span>
          <span className="h-hint">{receipt.hint}</span>
          {receipt.to && (
            <Link className="hall-link" to={receipt.to}>
              去看看
            </Link>
          )}
        </p>
      )}
      {thread && (
        <AnswerCard
          thread={thread}
          onClose={close}
          onDeeper={() => setDeeper(thread[0].q)}
          onFile={() => void file(thread[0].q)}
          onDelegate={() => void delegate(thread[0].q, thread.map((t) => `问：${t.q}`).join('\n'))}
        />
      )}
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
