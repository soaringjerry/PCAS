import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { ArrowUp, Check, Minus, Undo2, X } from 'lucide-react'
import {
  loadConversation,
  splitTitle,
  updateConversation,
  type DeskTurn,
  type DeskTurnRequest,
  type DeskTurnResponse,
  type DeskTurnsResponse,
  type Receipt,
} from '../domain/desk'
import { findThing, thingTitle } from '../domain/things'
import { api, APIError } from '../store/api'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { SecretaryCards } from './SecretaryCards'
import '../styles/secretary.css'

const MAX_LENGTH = 4000

/* ---------- Delivery, independent of whichever page is showing ---------- */

// Requests outlive the component that sent them: leaving the page must not
// turn an answer into a "retry", and coming back must not send it twice.
const inflight = new Map<string, Promise<DeskTurnResponse>>()
// The first line of a new conversation gets its ID from the server; lines
// sent meanwhile wait for it so they all land in the same conversation.
const founding = new Map<string, Promise<string>>()
// Lines whose conversation was ended while they were in flight.
const abandoned = new Set<string>()

async function transmit(key: string, request: DeskTurnRequest): Promise<DeskTurnResponse> {
  let body = request
  const saved = loadConversation(key).unanswered.find((u) => u.request.requestId === request.requestId)
  if (saved?.sent) {
    body = saved.request // A retry repeats exactly what the server may have seen.
  } else {
    for (;;) {
      const conversationId = loadConversation(key).conversationId ?? body.conversationId
      if (conversationId) {
        body = { ...body, conversationId }
        break
      }
      const pending = founding.get(key)
      if (!pending) break
      await pending.catch(() => undefined)
      if (founding.get(key) === pending) founding.delete(key)
    }
  }
  updateConversation(key, (c) => ({
    ...c,
    unanswered: c.unanswered.some((u) => u.request.requestId === body.requestId)
      ? c.unanswered.map((u) => (u.request.requestId === body.requestId ? { request: body, sent: true } : u))
      : [...c.unanswered, { request: body, sent: true }],
  }))
  const response = api<DeskTurnResponse>('/v1/desk/turn', body)
  if (!body.conversationId) {
    const id = response.then((r) => r.conversationId)
    founding.set(key, id)
    id.catch(() => undefined).finally(() => founding.get(key) === id && founding.delete(key))
  }
  const r = await response
  const keep = !abandoned.delete(body.requestId)
  updateConversation(key, (c) => ({
    conversationId: c.conversationId ?? (keep ? r.conversationId : null),
    unanswered: c.unanswered.filter((u) => u.request.requestId !== body.requestId),
  }))
  return r
}

function deliver(key: string, request: DeskTurnRequest): Promise<DeskTurnResponse> {
  const existing = inflight.get(request.requestId)
  if (existing) return existing
  const p = transmit(key, request).finally(() => inflight.delete(request.requestId))
  inflight.set(request.requestId, p)
  return p
}

/* ---------- One conversation ---------- */

type Line =
  | { key: string; kind: 'turn'; turn: DeskTurn }
  | { key: string; kind: 'waiting'; request: DeskTurnRequest }
  | { key: string; kind: 'failed'; request: DeskTurnRequest; error: string; retry: boolean }

const turnLine = (turn: DeskTurn): Line => ({ key: turn.id, kind: 'turn', turn })

function failure(e: unknown): { error: string; retry: boolean } {
  if (!(e instanceof APIError)) return { error: '网络断了，没发出去', retry: true }
  // The server refused this exact request; repeating it cannot help.
  if (e.status === 400 || e.status === 409) return { error: e.message, retry: false }
  return { error: e.message, retry: true }
}

function ReceiptRow({ receipt, onEdit }: { receipt: Receipt; onEdit: (title: string) => void }) {
  const { state, tryUndo } = useStore()
  const [undo, setUndo] = useState<{ busy?: boolean; done?: boolean; error?: string }>({})
  const thing = receipt.thingId ? findThing(state, receipt.thingId) : undefined
  const title = thing && thingTitle(thing)
  const skipped = receipt.status === 'skipped'
  const parts = !undo.done && thing ? splitTitle(receipt.text, title) : null
  const text = parts ? (
    <>
      {parts[0]}
      <Link to={`/t/${thing!.id}`}>{parts[1]}</Link>
      {parts[2]}
    </>
  ) : !undo.done && thing ? (
    <Link to={`/t/${thing.id}`}>{receipt.text}</Link>
  ) : (
    receipt.text
  )

  return (
    <li className={`sec-receipt${skipped ? ' skipped' : ''}${undo.done ? ' undone' : ''}`}>
      <span className="r-icon" aria-hidden="true">
        {undo.done ? <Undo2 size={13} /> : skipped ? <Minus size={13} /> : <Check size={13} strokeWidth={3} />}
      </span>
      <span className="r-text" title={receipt.reason ? `${receipt.text} · ${receipt.reason}` : receipt.text}>
        <span className="r-what">{text}</span>
        {skipped && receipt.reason && <span className="r-note"> · {receipt.reason}</span>}
      </span>
      {undo.done && <span className="r-note">已撤销</span>}
      {undo.error && (
        <span className="r-note r-error" role="alert">
          {undo.error}
        </span>
      )}
      {!skipped && !undo.done && (
        <span className="r-actions">
          {thing && title && (
            <button type="button" onClick={() => onEdit(title)}>
              改
            </button>
          )}
          {receipt.undoable && receipt.actionId && (
            <button
              type="button"
              disabled={undo.busy}
              onClick={async () => {
                setUndo({ busy: true })
                const outcome = await tryUndo(receipt.actionId!)
                if (outcome.ok || outcome.code === 'already_undone') setUndo({ done: true })
                else setUndo({ error: outcome.message })
              }}
            >
              撤销
            </button>
          )}
        </span>
      )}
    </li>
  )
}

function TurnView({ turn, last, onSend, onEdit }: { turn: DeskTurn; last: boolean; onSend: (text: string) => void; onEdit: (title: string) => void }) {
  return (
    <>
      {turn.reply && <p className="sec-reply">{turn.reply}</p>}
      <SecretaryCards cards={turn.cards ?? []} />
      {turn.receipts?.length > 0 && (
        <ul className="sec-receipts">
          {turn.receipts.map((r, i) => (
            <ReceiptRow key={r.actionId ?? i} receipt={r} onEdit={onEdit} />
          ))}
        </ul>
      )}
      {turn.ask && (
        <div className="sec-ask">
          <p>{turn.ask.question}</p>
          {last && turn.ask.options.length > 0 && (
            <div className="a-options">
              {turn.ask.options.map((option) => (
                <button key={option} type="button" onClick={() => onSend(option)}>
                  {option}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </>
  )
}

/**
 * The front-desk secretary: one continuous conversation. Say something and
 * it gets done; each result comes back as a one-line receipt with 改 and 撤销.
 * The home page uses <Secretary />; a thing page passes its thing.
 */
export function Secretary({ thingId }: { thingId?: string }) {
  const key = thingId ?? 'desk'
  const { applyState, refresh } = useStore()
  const { draft, setDraft, prefill, onPrefill, agentFor } = useShell()
  const text = draft(key)
  const input = useRef<HTMLTextAreaElement>(null)
  const thread = useRef<HTMLOListElement>(null)
  const [reveal, setReveal] = useState(0)
  const [restored, setRestored] = useState(0)
  const [lines, setLines] = useState<Line[]>(() =>
    loadConversation(key).unanswered.map((u): Line =>
      inflight.has(u.request.requestId)
        ? { key: u.request.requestId, kind: 'waiting', request: u.request }
        : { key: u.request.requestId, kind: 'failed', request: u.request, error: '上次没等到回复，原话还在', retry: true },
    ),
  )

  const follow = (requestId: string, p: Promise<DeskTurnResponse>, alive: { current: boolean }) => {
    p.then(
      (r) => {
        applyState(r.state)
        if (!alive.current) return
        setLines((ls) => {
          // A retried line whose answer was already restored appears once.
          if (ls.some((l) => l.key === r.turn.id)) return ls.filter((l) => l.key !== requestId)
          return ls.map((l) => (l.key === requestId ? turnLine(r.turn) : l))
        })
        setReveal((n) => n + 1)
      },
      (e: unknown) => {
        if (e instanceof APIError && e.status === 401) void refresh()
        if (!alive.current) return
        setLines((ls) => ls.map((l) => (l.key === requestId && l.kind !== 'turn' ? { key: requestId, kind: 'failed', request: l.request, ...failure(e) } : l)))
      },
    )
  }

  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  // Restore: pick up lines still in flight, and the conversation so far.
  useEffect(() => {
    const saved = loadConversation(key)
    for (const u of saved.unanswered) {
      const p = inflight.get(u.request.requestId)
      if (p) follow(u.request.requestId, p, alive)
    }
    if (!saved.conversationId) return
    const conversationId = saved.conversationId
    api<DeskTurnsResponse>(`/v1/desk/turns?conversationId=${encodeURIComponent(conversationId)}`).then(
      (r) => {
        if (!alive.current || loadConversation(key).conversationId !== conversationId) return
        setLines((ls) => [...r.turns.filter((t) => !ls.some((l) => l.key === t.id)).map(turnLine), ...ls])
        setRestored((n) => n + 1)
      },
      (e: unknown) => {
        if (e instanceof APIError && e.status === 404) updateConversation(key, (c) => ({ ...c, conversationId: null }))
      },
    )
    // Runs once per conversation key; `follow` only closes over stable store callbacks.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key])

  useEffect(
    () =>
      onPrefill(key, () => {
        // Wait for the prefilled draft to render, then put the caret at its end.
        requestAnimationFrame(() => {
          const el = input.current
          if (!el) return
          el.focus()
          el.setSelectionRange(el.value.length, el.value.length)
        })
      }),
    [key, onPrefill],
  )

  // A restored conversation opens at its latest turn, without moving the page.
  useEffect(() => {
    const list = thread.current
    if (restored && list) list.scrollTop = list.scrollHeight
  }, [restored])

  // Bring the newest line into view after sending or when an answer lands.
  useEffect(() => {
    if (reveal) thread.current?.lastElementChild?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  }, [reveal])

  const send = (said: string) => {
    const words = said.trim()
    if (!words || words.length > MAX_LENGTH) return
    const request: DeskTurnRequest = {
      requestId: crypto.randomUUID(),
      conversationId: loadConversation(key).conversationId,
      thingId: thingId ?? null,
      text: words,
      agentId: agentFor('desk'),
    }
    // Keep the words before anything goes out, so a lost connection loses nothing.
    updateConversation(key, (c) => ({ ...c, unanswered: [...c.unanswered, { request, sent: false }] }))
    setLines((ls) => [...ls, { key: request.requestId, kind: 'waiting', request }])
    setReveal((n) => n + 1)
    follow(request.requestId, deliver(key, request), alive)
  }

  const submit = () => {
    if (!text.trim() || text.trim().length > MAX_LENGTH) return
    send(text)
    setDraft(key, '')
  }

  const retry = (line: Extract<Line, { kind: 'failed' }>) => {
    setLines((ls) => ls.map((l) => (l.key === line.key ? { key: l.key, kind: 'waiting', request: line.request } : l)))
    follow(line.key, deliver(key, line.request), alive)
  }

  /** Takes an unanswered line back into the input to reword it. */
  const reword = (line: Extract<Line, { kind: 'failed' }>) => {
    updateConversation(key, (c) => ({ ...c, unanswered: c.unanswered.filter((u) => u.request.requestId !== line.key) }))
    setLines((ls) => ls.filter((l) => l.key !== line.key))
    prefill(key, line.request.text)
  }

  const end = () => {
    updateConversation(key, (c) => ({ ...c, conversationId: null }))
    founding.delete(key)
    for (const l of lines) if (l.kind === 'waiting') abandoned.add(l.key)
    // Answered turns go; anything still unanswered stays so no words are lost.
    setLines((ls) => ls.filter((l) => l.kind !== 'turn'))
    input.current?.focus()
  }

  const edit = (title: string) => prefill(key, `改一下：${title}，`)
  const lastTurn = [...lines].reverse().find((l) => l.kind === 'turn')?.key
  const tooLong = text.trim().length > MAX_LENGTH

  return (
    <section className={`sec ${thingId ? 'sec-thing' : 'sec-hall'}`} aria-label="秘书">
      {lines.length > 0 && (
        <div className="sec-thread">
          <button type="button" className="sec-close" aria-label="结束这次对话" title="结束这次对话（Esc）" onClick={end}>
            <X size={15} />
          </button>
          <ol ref={thread} aria-live="polite">
            {lines.map((line) => (
              <li key={line.key} className={`sec-turn ${line.kind}`}>
                <p className="sec-said" title={line.kind === 'turn' ? line.turn.text : line.request.text}>
                  {line.kind === 'turn' ? line.turn.text : line.request.text}
                </p>
                {line.kind === 'waiting' && <p className="sec-status">正在想…</p>}
                {line.kind === 'failed' && (
                  <p className="sec-status failed">
                    <span>{line.error}</span>
                    {line.retry && (
                      <button type="button" onClick={() => retry(line)}>
                        重试
                      </button>
                    )}
                    <button type="button" onClick={() => reword(line)}>
                      改
                    </button>
                  </p>
                )}
                {line.kind === 'turn' && <TurnView turn={line.turn} last={line.key === lastTurn} onSend={send} onEdit={edit} />}
              </li>
            ))}
          </ol>
        </div>
      )}
      <form
        className="sec-input"
        onSubmit={(e) => {
          e.preventDefault()
          submit()
        }}
      >
        <textarea
          ref={input}
          rows={1}
          value={text}
          onChange={(e) => setDraft(key, e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing && e.keyCode !== 229) {
              e.preventDefault()
              submit()
            } else if (e.key === 'Escape' && lines.some((l) => l.kind === 'turn')) {
              e.preventDefault()
              end()
            }
          }}
          placeholder={thingId ? '关于这件事，说一句' : '说一句：记事、问事、改安排都行'}
          aria-label="跟秘书说"
          aria-invalid={tooLong || undefined}
          autoComplete="off"
          enterKeyHint="send"
        />
        <button type="submit" className="sec-send" aria-label="发送" disabled={!text.trim() || tooLong}>
          <ArrowUp size={18} strokeWidth={2.5} />
        </button>
      </form>
      {tooLong && <p className="sec-hint">太长了，一次最多 {MAX_LENGTH} 字</p>}
    </section>
  )
}
