import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { memoriesFor } from '../domain/agent'
import type { Agent, Epistemic, Memory, MemoryFacets, MemoryKind, MemoryPage, MemoryTrust, State } from '../domain/types'
import { api, APIError } from './api'
import { useStore } from './context'

// The workspace snapshot carries only the 200 most recently updated memories.
// The full list, one memory by id, and the people and places come from here.

export interface MemoryFilter {
  /** Words the memory contains. */
  q: string
  /** A person or place: only memories that mention it. */
  entity: string
  /** A project, topic or area of life: only memories filed under it. */
  group: string
  nature: MemoryKind | ''
  /** How far it can be trusted; the server knows three values. */
  epistemic: Exclude<Epistemic, 'planned'> | ''
  /** How far it can be relied on, by where it came from. */
  trust: MemoryTrust | ''
  /** `1`: the memories that were replaced or merged away, instead of the current ones. */
  retired: '1' | ''
}

/** Everything the list can be narrowed by; `project` and `agent` are for the places that ask about one of them. */
type MemoryQuery = Partial<MemoryFilter> & { project?: string; agent?: string; retiredBy?: string }

const PAGE = 50

/** Whether the snapshot holds every memory there is; then it can answer without asking the server. */
const snapshotIsComplete = (state: State) => (state.memoryTotal ?? state.memories.length) <= state.memories.length

/** Why a read failed, in words the user can act on. */
export function readProblem(e: unknown): string {
  if (!(e instanceof APIError)) return '网络连接中断，请检查网络后重试。'
  if (e.status === 401) return '登录已过期，请重新登录。'
  return `服务器出错了（错误 ${e.status}${e.code ? `，${e.code}` : ''}）。请稍后重试；一直这样请查看服务日志。`
}

function listPath(filter: MemoryQuery, cursor?: string, limit = PAGE): string {
  const query = new URLSearchParams({ limit: String(limit) })
  for (const name of ['q', 'entity', 'group', 'nature', 'epistemic', 'trust', 'retired', 'retiredBy', 'project', 'agent'] as const) {
    const value = filter[name]
    if (value) query.set(name, value)
  }
  if (cursor) query.set('cursor', cursor)
  return `/v1/workspace/memories?${query}`
}

interface Loaded {
  /** The filter these items answer; anything else on screen is still being read. */
  key: string
  phase: 'ready' | 'failed'
  problem: string
  items: Memory[]
  next: string
  total: number
  /** Pages read so far. */
  pages: number
  more: 'idle' | 'loading' | 'failed'
  moreProblem: string
}

const nothing: Loaded = { key: '', phase: 'ready', problem: '', items: [], next: '', total: 0, pages: 0, more: 'idle', moreProblem: '' }

/** A fresh first page replaces a list that is one page long, and is laid over a longer one so the place scrolled to is kept. */
function refreshed(prev: Loaded, key: string, page: Partial<MemoryPage>): Loaded {
  const items = page.items ?? []
  const total = page.total ?? items.length
  if (prev.key !== key || prev.phase !== 'ready' || prev.pages <= 1) {
    return { ...nothing, key, items, next: page.next ?? '', total, pages: 1 }
  }
  const seen = new Set(items.map((m) => m.id))
  return { ...prev, items: [...items, ...prev.items.filter((m) => !seen.has(m.id))], total }
}

export interface MemoryList {
  phase: 'loading' | 'ready' | 'failed'
  problem: string
  items: Memory[]
  total: number
  hasMore: boolean
  more: Loaded['more']
  moreProblem: string
  loadMore: () => void
  retry: () => void
  /** Takes a memory the user just deleted out of the list without waiting for the next read. */
  remove: (id: string) => void
}

/** The memory list for one filter, read a page at a time and re-read from the top whenever the workspace changes. */
export function useMemoryList(filter: MemoryFilter): MemoryList {
  const { state } = useStore()
  const { q, entity, group, nature, epistemic, trust, retired } = filter
  const key = `${q}\n${entity}\n${group}\n${nature}\n${epistemic}\n${trust}\n${retired}`
  const [data, setData] = useState(nothing)
  const [attempt, setAttempt] = useState(0)
  const reading = useRef('')

  useEffect(() => {
    let alive = true
    api<Partial<MemoryPage>>(listPath({ q, entity, group, nature, epistemic, trust, retired }))
      .then((page) => { if (alive) setData((prev) => refreshed(prev, key, page)) })
      // A failed re-read keeps what is already shown; only a first read has nothing to fall back on.
      .catch((e: unknown) => { if (alive) setData((prev) => prev.key === key && prev.phase === 'ready' ? prev : { ...nothing, key, phase: 'failed', problem: readProblem(e) }) })
    return () => { alive = false }
  }, [key, q, entity, group, nature, epistemic, trust, retired, state.revision, attempt])

  const current = data.key === key ? data : undefined
  const cursor = current?.phase === 'ready' ? current.next : ''
  const loadMore = useCallback(() => {
    const token = `${key}\n${cursor}`
    if (!cursor || reading.current === token) return
    reading.current = token
    setData((prev) => prev.key === key ? { ...prev, more: 'loading', moreProblem: '' } : prev)
    api<Partial<MemoryPage>>(listPath({ q, entity, group, nature, epistemic, trust, retired }, cursor))
      .then((page) => setData((prev) => {
        // The list moved on while this page was on its way.
        if (prev.key !== key || prev.next !== cursor) return prev
        const seen = new Set(prev.items.map((m) => m.id))
        return { ...prev, items: [...prev.items, ...(page.items ?? []).filter((m) => !seen.has(m.id))], next: page.next ?? '', total: page.total ?? prev.total, pages: prev.pages + 1, more: 'idle' }
      }))
      .catch((e: unknown) => setData((prev) => prev.key === key && prev.next === cursor ? { ...prev, more: 'failed', moreProblem: readProblem(e) } : prev))
      .finally(() => { if (reading.current === token) reading.current = '' })
  }, [key, q, entity, group, nature, epistemic, trust, retired, cursor])

  const retry = useCallback(() => { setData(nothing); setAttempt((n) => n + 1) }, [])
  const remove = useCallback((id: string) => setData((prev) => prev.items.some((m) => m.id === id) ? { ...prev, items: prev.items.filter((m) => m.id !== id), total: Math.max(0, prev.total - 1) } : prev), [])

  // The snapshot is re-read after every change, so its copy of a memory is the newer one.
  const recent = useMemo(() => new Map(state.memories.map((m) => [m.id, m])), [state.memories])
  const items = useMemo(() => (current?.items ?? []).map((m) => recent.get(m.id) ?? m), [current, recent])

  return {
    phase: current?.phase ?? 'loading',
    problem: current?.problem ?? '',
    items,
    total: current?.total ?? 0,
    hasMore: Boolean(cursor),
    more: current?.more ?? 'idle',
    moreProblem: current?.moreProblem ?? '',
    loadMore,
    retry,
    remove,
  }
}

/** The people and places memories mention and the groups they are filed under, most used first. */
export function useMemoryFacets(): { facets?: MemoryFacets; problem: string; retry: () => void } {
  const { state } = useStore()
  const [facets, setFacets] = useState<MemoryFacets>()
  const [problem, setProblem] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let alive = true
    api<Partial<MemoryFacets>>('/v1/workspace/memory-facets')
      .then((v) => { if (alive) { setFacets({ groups: v.groups ?? [], people: v.people ?? [], places: v.places ?? [] }); setProblem('') } })
      .catch((e: unknown) => { if (alive) setProblem(readProblem(e)) })
    return () => { alive = false }
  }, [state.revision, attempt])
  const retry = useCallback(() => { setProblem(''); setAttempt((n) => n + 1) }, [])
  // An earlier answer stays usable when a later read fails.
  return { facets, problem: facets ? '' : problem, retry }
}

/** Pages of retired memories looked through for the ones merged into a memory before giving up. */
const MERGED_PAGES = 20

/**
 * The memories merged into `id`, read from the retired list a page at a time
 * until all `expected` of them are found: the server lists every retired memory
 * and has no way to ask for one memory's alone.
 */
async function mergedInto(id: string, expected: number): Promise<Memory[]> {
  const found: Memory[] = []
  let cursor: string | undefined
  for (let page = 0; page < MERGED_PAGES; page++) {
    const read = await api<Partial<MemoryPage>>(listPath({ retired: '1', retiredBy: id }, cursor, 100))
    found.push(...(read.items ?? []).filter((m) => m.retired === 'duplicate' && m.retiredBy === id))
    cursor = read.next || undefined
    if (!cursor || found.length >= expected) break
  }
  return found
}

/** The memories that were merged into one, each still carrying its own sources. */
export function useMergedInto(id: string, expected: number): { items: Memory[]; phase: 'loading' | 'ready' | 'failed'; problem: string; retry: () => void } {
  const { state } = useStore()
  const [read, setRead] = useState<{ id: string; items: Memory[]; problem: string }>()
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (!id) return
    let alive = true
    mergedInto(id, expected)
      .then((items) => { if (alive) setRead({ id, items, problem: '' }) })
      // An earlier answer stays usable when a later read fails.
      .catch((e: unknown) => { if (alive) setRead((prev) => prev?.id === id && !prev.problem ? prev : { id, items: [], problem: readProblem(e) }) })
    return () => { alive = false }
  }, [id, expected, state.revision, attempt])
  const retry = useCallback(() => { setRead(undefined); setAttempt((n) => n + 1) }, [])
  if (!id || read?.id !== id) return { items: [], phase: 'loading', problem: '', retry }
  return { items: read.items, phase: read.problem ? 'failed' : 'ready', problem: read.problem, retry }
}

export interface OneMemory {
  memory?: Memory
  /** `gone`: it was deleted, or the action that made it was undone. */
  phase: 'loading' | 'ready' | 'gone' | 'failed'
  problem: string
  retry: () => void
}

/**
 * One memory by id. The snapshot answers for the recent ones; an older one is
 * read on its own, with `known` (say, its row in a list) shown meanwhile.
 */
export function useMemory(id: string | null, known?: Memory): OneMemory {
  const { state } = useStore()
  const recent = id ? state.memories.find((m) => m.id === id) : undefined
  const [read, setRead] = useState<{ id: string; memory?: Memory; phase: 'ready' | 'gone' | 'failed'; problem: string }>()
  const [attempt, setAttempt] = useState(0)
  const wanted = id && !recent ? id : ''
  useEffect(() => {
    if (!wanted) return
    let alive = true
    api<Partial<Memory>>(`/v1/workspace/memories/${encodeURIComponent(wanted)}`)
      .then((memory) => { if (alive) setRead(memory.id ? { id: wanted, memory: memory as Memory, phase: 'ready', problem: '' } : { id: wanted, phase: 'gone', problem: '' }) })
      .catch((e: unknown) => {
        if (!alive) return
        // Not there, or (400) not something that could ever be a memory's id.
        if (e instanceof APIError && (e.status === 404 || e.status === 400)) setRead({ id: wanted, phase: 'gone', problem: '' })
        else setRead((prev) => prev?.id === wanted && prev.memory ? prev : { id: wanted, phase: 'failed', problem: readProblem(e) })
      })
    return () => { alive = false }
  }, [wanted, state.revision, attempt])
  const retry = useCallback(() => { setRead(undefined); setAttempt((n) => n + 1) }, [])

  if (!id) return { phase: 'gone', problem: '', retry }
  if (recent) return { memory: recent, phase: 'ready', problem: '', retry }
  if (read?.id === id && read.phase !== 'failed') return { memory: read.memory, phase: read.phase, problem: '', retry }
  if (known) return { memory: known, phase: 'ready', problem: '', retry }
  return read?.id === id ? { phase: 'failed', problem: read.problem, retry } : { phase: 'loading', problem: '', retry }
}

/**
 * The first few memories containing `q`, for a quick find. The snapshot answers
 * at once; when it does not hold every memory, the server's answer takes over.
 */
export function useMemorySearch(q: string, limit: number): Memory[] {
  const { state } = useStore()
  const wanted = q && !snapshotIsComplete(state) ? q : ''
  const [read, setRead] = useState<{ q: string; items: Memory[] }>()
  useEffect(() => {
    if (!wanted) return
    let alive = true
    // A pause in typing, not every keystroke.
    const timer = window.setTimeout(() => {
      api<Partial<MemoryPage>>(listPath({ q: wanted }, undefined, limit))
        .then((page) => { if (alive) setRead({ q: wanted, items: page.items ?? [] }) })
        // The snapshot's matches stay; a quick find has nowhere to put an error.
        .catch(() => undefined)
    }, 200)
    return () => {
      alive = false
      window.clearTimeout(timer)
    }
  }, [wanted, limit])
  return useMemo(() => {
    if (!q) return []
    if (wanted && read?.q === wanted) return read.items
    return state.memories.filter((m) => m.text.includes(q)).slice(0, limit)
  }, [q, wanted, read, limit, state.memories])
}

/**
 * How many memories an agent can be given under its current scope: open to it,
 * of a kind it reads, and not a guess unless it takes guesses. Counted from the
 * snapshot when that is every memory there is, and by the server otherwise.
 */
export function useMemoryCount(agent: Agent): number {
  const { state } = useStore()
  const kinds = agent.memoryKinds.join(',')
  const { id, includeInferred } = agent
  const key = snapshotIsComplete(state) ? '' : `${id}\n${kinds}\n${includeInferred}`
  const [read, setRead] = useState<{ key: string; total: number }>()
  useEffect(() => {
    if (!key) return
    let alive = true
    // The list takes one kind and one level of trust at a time, so the scope is counted piece by piece.
    const pieces = (kinds ? kinds.split(',') : []).flatMap((nature) =>
      includeInferred ? [{ agent: id, nature }] : [{ agent: id, nature, epistemic: 'confirmed' }, { agent: id, nature, epistemic: 'sourced' }],
    ) as MemoryQuery[]
    Promise.all(pieces.map((piece) => api<Partial<MemoryPage>>(listPath(piece, undefined, 1))))
      .then((pages) => { if (alive) setRead({ key, total: pages.reduce((n, page) => n + (page.total ?? 0), 0) }) })
      // The snapshot's count stays; it is what this showed before.
      .catch(() => undefined)
    return () => { alive = false }
  }, [key, id, kinds, includeInferred, state.revision])
  return key && read?.key === key ? read.total : memoriesFor(state, agent).length
}
