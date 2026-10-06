import { useCallback, useEffect, useState } from 'react'
import type { Now } from '../domain/status'
import { api } from './api'
import { readProblem, useMemoryChange } from './memories'

// What matters right now: the handover note and the dates to keep. Both are
// worked out in the background from the memories.

const blank: Now = { handover: { body: '', builtAt: '', stale: false }, deadlines: [] }

/** Fills whatever the server left out, so the page never meets a missing list. */
function whole(v: Partial<Now>): Now {
  return { handover: { ...blank.handover, ...v.handover }, deadlines: v.deadlines ?? [] }
}

/** The note is rewritten in the background at most once an hour; this is how soon a new one shows without a reload. */
const RECHECK = 5 * 60_000

export interface NowRead {
  now?: Now
  /** When `now` was read, in milliseconds. */
  readAt: number
  phase: 'loading' | 'ready' | 'failed'
  problem: string
  retry: () => void
}

/**
 * Read again when a memory changes, and every few minutes while the page is in
 * sight. An earlier answer stays up when a later read fails.
 */
export function useNow(): NowRead {
  const changed = useMemoryChange()
  const [read, setRead] = useState<{ now: Now; at: number }>()
  const [problem, setProblem] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let alive = true
    // The former page's address still answers; the new reads replace it once the backend has them.
    api<Partial<Now>>('/v1/workspace/about')
      .then((v) => { if (alive) { setRead({ now: whole(v), at: Date.now() }); setProblem('') } })
      .catch((e: unknown) => { if (alive) setProblem(readProblem(e)) })
    return () => { alive = false }
  }, [changed, attempt])
  useEffect(() => {
    const timer = window.setInterval(() => { if (!document.hidden) setAttempt((n) => n + 1) }, RECHECK)
    return () => window.clearInterval(timer)
  }, [])
  const retry = useCallback(() => { setProblem(''); setAttempt((n) => n + 1) }, [])
  if (read) return { now: read.now, readAt: read.at, phase: 'ready', problem: '', retry }
  return { readAt: 0, phase: problem ? 'failed' : 'loading', problem, retry }
}
