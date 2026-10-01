import { useRef, useState } from 'react'
import { Link } from 'react-router'
import { findThing } from '../domain/things'
import type { Candidate } from '../domain/types'
import type { Action } from '../store/actions'
import { useStore } from '../store/context'
import { SideSheet } from './Overlay'
import { Button, Empty } from './ui'
import '../styles/settings.css'

// Each button names what it leaves behind. None of these can be undone from
// here, so nothing on this sheet says it can.

/** What taking a candidate in creates, by the kind chosen. */
const outcomes = [
  { kind: 'task', label: '创建待办', done: '已创建待办', guess: '看起来是一件待办' },
  { kind: 'idea', label: '保存为想法', done: '已保存为想法', guess: '看起来是一个想法' },
  { kind: 'memory', label: '保存为记忆', done: '已保存为记忆', guess: '看起来是一条关于你的记忆' },
] as const

type Handled = { id: string; text: string; said: string; memory?: boolean }

/** Sends one action once; a refusal leaves the item where it was and says so. */
function useAct(onDone: (said: string) => void) {
  const { dispatch } = useStore()
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  const lock = useRef(false)
  const act = async (said: string, action: Action) => {
    if (lock.current) return
    lock.current = true
    setBusy(true)
    setFailed(false)
    const ok = await dispatch(action)
    lock.current = false
    setBusy(false)
    if (ok) onDone(said)
    else setFailed(true)
  }
  return { busy, failed, act }
}

function CandidateItem({ c, onDone }: { c: Candidate; onDone: (h: Handled) => void }) {
  const { busy, failed, act } = useAct((said) => onDone({ id: c.id, text: c.text, said, memory: said === '已保存为记忆' }))
  const guess = outcomes.find((o) => o.kind === c.kind)
  return (
    <div className="item">
      <div className="grow">
        <div className="item-title">{c.text}</div>
        <div className="meta">
          <span>来自{c.source.label}</span>
          <span>{guess ? guess.guess : '没看出这是什么，你来定'}</span>
        </div>
        <div className="row unsure-actions">
          {outcomes.map((o) => (
            <Button
              key={o.kind}
              size="sm"
              variant={o.kind === c.kind ? 'primary' : 'default'}
              disabled={busy}
              onClick={() => void act(o.done, { type: 'acceptCandidate', id: c.id, kind: o.kind, text: c.text, memoryKind: c.memoryKind, projectId: c.projectId, due: c.due })}
            >
              {o.label}
            </Button>
          ))}
          <Button size="sm" variant="quiet" disabled={busy} onClick={() => void act('已忽略，原资料还在', { type: 'ignoreCandidate', id: c.id })}>
            忽略这条
          </Button>
        </div>
        {failed && (
          <p className="small warn-text" role="alert">
            没保存上，这条还在这里。
          </p>
        )}
      </div>
    </div>
  )
}

/** Things read from material that the background could not place on its own. Guessed memories are handled in the library's memory list. */
export function UnsureSheet({ onClose }: { onClose: () => void }) {
  const { state } = useStore()
  const candidates = state.candidates.filter((c) => c.state === 'pending')
  const [handled, setHandled] = useState<Handled[]>([])
  const done = (h: Handled) => setHandled((list) => [h, ...list].slice(0, 3))
  /** The task or idea a candidate became, by the id the server recorded for it. */
  const made = (id: string) => {
    const into = state.candidates.find((c) => c.id === id)?.resolvedInto
    return into && findThing(state, into) ? into : undefined
  }
  return (
    <SideSheet title="待确认内容" onClose={onClose}>
      <p className="small muted" style={{ marginBottom: 14 }}>
        从你的资料里读到、但拿不准该怎么放的。处理一条，它就离开这里；这里的操作不能撤销。
      </p>
      {handled.length > 0 && (
        <div className="unsure-done" role="status">
          {handled.map((h) => {
            const to = made(h.id)
            return (
              <p key={h.id}>
                <span className="said">{h.said}</span>
                <span className="what">{h.text}</span>
                {to && (
                  <Link to={`/t/${to}`} onClick={onClose}>
                    打开
                  </Link>
                )}
                {/* A memory has no id to open here, so the link goes to where memories are listed. */}
                {h.memory && (
                  <Link to="/library" onClick={onClose}>
                    去记忆列表查看
                  </Link>
                )}
              </p>
            )
          })}
        </div>
      )}
      {candidates.length === 0 && <Empty>都处理完了。</Empty>}
      {candidates.length > 0 && (
        <div className="sheet">
          <div className="list">
            {candidates.map((c) => (
              <CandidateItem key={c.id} c={c} onDone={done} />
            ))}
          </div>
        </div>
      )}
    </SideSheet>
  )
}
