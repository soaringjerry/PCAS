import { useCallback, useEffect, useState } from 'react'
import type { Deadline, Handover } from '../domain/status'
import { api } from './api'
import { readProblem, useMemoryChange } from './memories'

// What matters right now: the handover note and the dates to keep. Both are
// worked out in the background from the memories, and each is read on its own,
// so one that cannot be read does not take the other with it.

/** The note is rewritten in the background at most once an hour; this is how soon a new one shows without a reload. */
const RECHECK = 5 * 60_000

export interface Read<T> {
  value?: T
  phase: 'loading' | 'ready' | 'failed'
  problem: string
  retry: () => void
}

/**
 * Read again when a memory changes (or `moved` does), and every few minutes
 * while the page is in sight. An earlier answer stays up when a later read fails.
 */
export function useRead<T>(path: string, shape: (answer: unknown) => T, moved: string | number = ''): Read<T> {
  const changed = `${useMemoryChange()}.${moved}`
  const [value, setValue] = useState<T>()
  const [problem, setProblem] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let alive = true
    api<unknown>(path)
      .then((v) => { if (alive) { setValue(shape(v)); setProblem('') } })
      .catch((e: unknown) => { if (alive) setProblem(readProblem(e)) })
    return () => { alive = false }
  }, [path, shape, changed, attempt])
  useEffect(() => {
    const timer = window.setInterval(() => { if (!document.hidden) setAttempt((n) => n + 1) }, RECHECK)
    return () => window.clearInterval(timer)
  }, [])
  const retry = useCallback(() => { setProblem(''); setAttempt((n) => n + 1) }, [])
  if (value !== undefined) return { value, phase: 'ready', problem: '', retry }
  return { phase: problem ? 'failed' : 'loading', problem, retry }
}

// Whatever the server left out is filled in, so the page never meets a missing field.
const handover = (v: unknown): Handover => ({ body: '', builtAt: '', stale: false, ...(v as Partial<Handover> | null) })
const deadlines = (v: unknown): Deadline[] => (v as { items?: Deadline[] } | null)?.items ?? []

export function useHandover(): Read<Handover> {
  return useRead('/v1/workspace/handover', handover)
}

/** Every date there is: the server sends them all, the ones gone by included. */
export function useDeadlines(): Read<Deadline[]> {
  return useRead('/v1/workspace/deadlines', deadlines)
}
