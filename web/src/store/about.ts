import { useCallback, useEffect, useState } from 'react'
import type { About, StatusCard } from '../domain/status'
import { api } from './api'
import { useStore } from './context'
import { readProblem } from './memories'

// What has been worked out about the user from their memories: the handover
// note, the deadlines, and the index of cards. One card's contents are read on
// their own when it is opened.

const blank: About = { handover: { body: '', builtAt: '', stale: false }, cards: [], deadlines: [], building: { done: 0, total: 0 } }

/** Fills whatever the server left out, so the page never meets a missing list. */
function whole(v: Partial<About>): About {
  return {
    handover: { ...blank.handover, ...v.handover },
    cards: (v.cards ?? []).map((c) => ({ ...c, fields: c.fields ?? [] })),
    deadlines: v.deadlines ?? [],
    building: { ...blank.building, ...v.building },
  }
}

export interface AboutRead {
  about?: About
  phase: 'loading' | 'ready' | 'failed'
  problem: string
  retry: () => void
}

/** The page's contents, re-read whenever the workspace changes. An earlier answer stays up when a later read fails. */
export function useAbout(): AboutRead {
  const { state } = useStore()
  const [about, setAbout] = useState<About>()
  const [problem, setProblem] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let alive = true
    api<Partial<About>>('/v1/workspace/about')
      .then((v) => { if (alive) { setAbout(whole(v)); setProblem('') } })
      .catch((e: unknown) => { if (alive) setProblem(readProblem(e)) })
    return () => { alive = false }
  }, [state.revision, attempt])
  const retry = useCallback(() => { setProblem(''); setAttempt((n) => n + 1) }, [])
  if (about) return { about, phase: 'ready', problem: '', retry }
  return { phase: problem ? 'failed' : 'loading', problem, retry }
}

export interface CardRead {
  card?: StatusCard
  /** `gone`: the server no longer has a card under this key. */
  phase: 'loading' | 'ready' | 'gone' | 'failed'
  problem: string
  retry: () => void
}

/** One card with the memories under each of its headings. */
export function useAboutCard(key: string): CardRead {
  const { state } = useStore()
  const [read, setRead] = useState<{ key: string; card?: StatusCard; problem: string }>()
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (!key) return
    let alive = true
    api<Partial<About>>(`/v1/workspace/about?key=${encodeURIComponent(key)}`)
      .then((v) => { if (alive) setRead({ key, card: whole(v).cards.find((c) => c.key === key), problem: '' }) })
      // A failed re-read keeps the card that is already open.
      .catch((e: unknown) => { if (alive) setRead((prev) => prev?.key === key && prev.card ? prev : { key, problem: readProblem(e) }) })
    return () => { alive = false }
  }, [key, state.revision, attempt])
  const retry = useCallback(() => { setRead(undefined); setAttempt((n) => n + 1) }, [])
  if (!key || read?.key !== key) return { phase: 'loading', problem: '', retry }
  if (read.problem) return { phase: 'failed', problem: read.problem, retry }
  return read.card ? { card: read.card, phase: 'ready', problem: '', retry } : { phase: 'gone', problem: '', retry }
}
