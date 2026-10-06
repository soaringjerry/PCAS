import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { memoriesFor } from '../domain/agent'
import type { Agent, AssistantRequirement, Epistemic, Memory, MemoryFacets, MemoryGroupEntry, MemoryKind, MemoryPage, MemoryTrust, State } from '../domain/types'
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
  /**
   * A group from the directory, by its key: the memories under it, as the
   * server counts them there. The server takes nothing else alongside it.
   */
  within: string
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

// The workspace's revision moves with every background write, most of which
// change nothing a list shows. What the lists follow instead is the snapshot's
// memories: a memory that changes is among the most recently updated, so it
// shows up there, and one that is added or goes away changes the total.
const changes = new WeakMap<Memory[], string>()

function memoryChange(state: State): string {
  let mark = changes.get(state.memories)
  if (mark === undefined) {
    // How faded a memory is drifts with the clock; that alone is no reason to read again.
    const text = JSON.stringify(state.memories, (name, value) => (name === 'exposure' || name === 'lastUsedAt' ? undefined : value))
    let hash = 2166136261
    for (let i = 0; i < text.length; i++) hash = Math.imul(hash ^ text.charCodeAt(i), 16777619)
    mark = `${text.length}.${hash >>> 0}`
    changes.set(state.memories, mark)
  }
  return `${state.memoryTotal ?? state.memories.length}.${mark}`
}

/** Changes when a memory was added, changed or taken away, and not when the background wrote something else. */
export function useMemoryChange(): string {
  const { state, writes } = useStore()
  // What the user just did here is always read back, whatever it touched.
  return `${writes}.${memoryChange(state)}`
}

/** Why a read failed, in words the user can act on. */
export function readProblem(e: unknown): string {
  if (!(e instanceof APIError)) return '网络连接中断，请检查网络后重试。'
  if (e.status === 401) return '登录已过期，请重新登录。'
  return `服务器出错了（错误 ${e.status}${e.code ? `，${e.code}` : ''}）。请稍后重试；一直这样请查看服务日志。`
}

function listPath(filter: MemoryQuery, cursor?: string, limit = PAGE): string {
  const query = new URLSearchParams({ limit: String(limit) })
  if (filter.within) {
    if (cursor) query.set('cursor', cursor)
    return `/v1/workspace/memory-groups/${encodeURIComponent(filter.within)}/memories?${query}`
  }
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

/**
 * The memory list for one filter, read a page at a time and re-read from the
 * top whenever a memory changes. Reads nothing while not `wanted`.
 */
export function useMemoryList(filter: MemoryFilter, wanted = true): MemoryList {
  const { state } = useStore()
  const changed = useMemoryChange()
  const { q, entity, group, nature, within, epistemic, trust, retired } = filter
  const key = `${q}\n${entity}\n${group}\n${nature}\n${within}\n${epistemic}\n${trust}\n${retired}`
  const [data, setData] = useState(nothing)
  const [attempt, setAttempt] = useState(0)
  const reading = useRef('')

  useEffect(() => {
    if (!wanted) return
    let alive = true
    api<Partial<MemoryPage>>(listPath({ q, entity, group, nature, within, epistemic, trust, retired }))
      .then((page) => { if (alive) setData((prev) => refreshed(prev, key, page)) })
      // A failed re-read keeps what is already shown; only a first read has nothing to fall back on.
      .catch((e: unknown) => { if (alive) setData((prev) => prev.key === key && prev.phase === 'ready' ? prev : { ...nothing, key, phase: 'failed', problem: readProblem(e) }) })
    return () => { alive = false }
  }, [wanted, key, q, entity, group, nature, within, epistemic, trust, retired, changed, attempt])

  const current = wanted && data.key === key ? data : undefined
  const cursor = current?.phase === 'ready' ? current.next : ''
  const loadMore = useCallback(() => {
    const token = `${key}\n${cursor}`
    if (!cursor || reading.current === token) return
    reading.current = token
    setData((prev) => prev.key === key ? { ...prev, more: 'loading', moreProblem: '' } : prev)
    api<Partial<MemoryPage>>(listPath({ q, entity, group, nature, within, epistemic, trust, retired }, cursor))
      .then((page) => setData((prev) => {
        // The list moved on while this page was on its way.
        if (prev.key !== key || prev.next !== cursor) return prev
        const seen = new Set(prev.items.map((m) => m.id))
        return { ...prev, items: [...prev.items, ...(page.items ?? []).filter((m) => !seen.has(m.id))], next: page.next ?? '', total: page.total ?? prev.total, pages: prev.pages + 1, more: 'idle' }
      }))
      .catch((e: unknown) => setData((prev) => prev.key === key && prev.next === cursor ? { ...prev, more: 'failed', moreProblem: readProblem(e) } : prev))
      .finally(() => { if (reading.current === token) reading.current = '' })
  }, [key, q, entity, group, nature, within, epistemic, trust, retired, cursor])

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
  const changed = useMemoryChange()
  const [facets, setFacets] = useState<MemoryFacets>()
  const [problem, setProblem] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let alive = true
    api<Partial<MemoryFacets>>('/v1/workspace/memory-facets')
      .then((v) => { if (alive) { setFacets({ groups: v.groups ?? [], people: v.people ?? [], places: v.places ?? [] }); setProblem('') } })
      .catch((e: unknown) => { if (alive) setProblem(readProblem(e)) })
    return () => { alive = false }
  }, [changed, attempt])
  const retry = useCallback(() => { setProblem(''); setAttempt((n) => n + 1) }, [])
  // An earlier answer stays usable when a later read fails.
  return { facets, problem: facets ? '' : problem, retry }
}

/** The most the server gives of a group at a time. */
const GROUP_PAGE = 100

export interface WholeGroups {
  phase: 'loading' | 'ready' | 'failed'
  problem: string
  /** The memories under every one of the groups, once all of them have been read. */
  items: Memory[]
  /** How far the reading has got, for saying so while it lasts. */
  read: number
  total: number
  retry: () => void
}

/**
 * Everything under each of `keys`, read to the end a page at a time, and what
 * they have in common. The server lists one group and takes no other filter
 * with it, so narrowing a group further is done here, over all of it: nothing
 * is left out for being on a later page. Reads nothing while not `wanted`.
 */
export function useWholeGroups(keys: string[], wanted: boolean): WholeGroups {
  const changed = useMemoryChange()
  const [attempt, setAttempt] = useState(0)
  const token = `${changed}\n${attempt}`
  const joined = keys.join('\n')
  // A group already read to the end is not read again until a memory changes.
  const cache = useRef({ token: '', groups: new Map<string, Memory[]>(), done: new Set<string>() })
  const [state, setState] = useState({ id: '', phase: 'loading' as WholeGroups['phase'], problem: '', items: [] as Memory[], read: 0, total: 0 })
  const id = `${token}\n${joined}`
  useEffect(() => {
    if (!wanted) return
    let alive = true
    if (cache.current.token !== token) cache.current = { token, groups: new Map(), done: new Set() }
    const { groups, done } = cache.current
    const wantedKeys = joined ? joined.split('\n') : []
    const totals = new Map<string, number>()
    const progress = () => {
      let read = 0, total = 0
      for (const key of wantedKeys) {
        read += groups.get(key)?.length ?? 0
        total += totals.get(key) ?? groups.get(key)?.length ?? 0
      }
      return { read, total }
    }
    const run = async () => {
      for (const key of wantedKeys) {
        if (done.has(key)) continue
        const items: Memory[] = []
        groups.set(key, items)
        for (let cursor = ''; ;) {
          const page = await api<Partial<MemoryPage>>(listPath({ within: key }, cursor, GROUP_PAGE))
          if (!alive) return
          items.push(...(page.items ?? []))
          totals.set(key, page.total ?? items.length)
          cursor = page.next ?? ''
          setState({ id, phase: 'loading', problem: '', items: [], ...progress() })
          if (!cursor) break
        }
        done.add(key)
      }
      const [first = [], ...rest] = wantedKeys.map((key) => groups.get(key) ?? [])
      const others = rest.map((items) => new Set(items.map((m) => m.id)))
      const common = first.filter((m) => others.every((ids) => ids.has(m.id)))
      setState({ id, phase: 'ready', problem: '', items: common, ...progress() })
    }
    run().catch((e: unknown) => {
      if (!alive) return
      // A group read part of the way is not kept as if it were whole.
      for (const key of wantedKeys) if (!done.has(key)) groups.delete(key)
      setState({ id, phase: 'failed', problem: readProblem(e), items: [], read: 0, total: 0 })
    })
    return () => {
      alive = false
      for (const key of wantedKeys) if (!done.has(key)) groups.delete(key)
    }
  }, [wanted, id, token, joined])
  const retry = useCallback(() => setAttempt((n) => n + 1), [])
  if (!wanted || state.id !== id) return { phase: 'loading', problem: '', items: [], read: 0, total: 0, retry }
  return { ...state, retry }
}

/** The directory of groups, each with how many memories are under it. An earlier answer stays usable when a later read fails. */
export function useMemoryGroups(): { groups?: MemoryGroupEntry[]; problem: string; retry: () => void } {
  const changed = useMemoryChange()
  const [groups, setGroups] = useState<MemoryGroupEntry[]>()
  const [problem, setProblem] = useState('')
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    let alive = true
    api<{ items?: MemoryGroupEntry[] }>('/v1/workspace/memory-groups')
      .then((v) => { if (alive) { setGroups(v.items ?? []); setProblem('') } })
      .catch((e: unknown) => { if (alive) setProblem(readProblem(e)) })
    return () => { alive = false }
  }, [changed, attempt])
  const retry = useCallback(() => { setProblem(''); setAttempt((n) => n + 1) }, [])
  return { groups, problem: groups ? '' : problem, retry }
}

/**
 * What the user asks of the assistant, by the memory each one is. Read only
 * while `wanted`; a list shows without it when it cannot be read.
 */
export function useRequirements(wanted: boolean): Map<string, AssistantRequirement> {
  const changed = useMemoryChange()
  const [items, setItems] = useState<AssistantRequirement[]>()
  useEffect(() => {
    if (!wanted) return
    let alive = true
    api<{ items?: AssistantRequirement[] }>('/v1/workspace/assistant-requirements')
      .then((v) => { if (alive) setItems(v.items ?? []) })
      .catch(() => undefined)
    return () => { alive = false }
  }, [wanted, changed])
  return useMemo(() => new Map((wanted ? items ?? [] : []).map((r) => [r.memoryId, r])), [wanted, items])
}

/** The memories that were merged into one, each still carrying its own sources. */
export function useMergedInto(id: string): { items: Memory[]; phase: 'loading' | 'ready' | 'failed'; problem: string; retry: () => void } {
  const changed = useMemoryChange()
  const [read, setRead] = useState<{ id: string; items: Memory[]; problem: string }>()
  const [attempt, setAttempt] = useState(0)
  useEffect(() => {
    if (!id) return
    let alive = true
    // The server lists what retired because of this memory; the ones replaced by it, rather than merged in, are left out here.
    api<Partial<MemoryPage>>(listPath({ retired: '1', retiredBy: id }, undefined, 100))
      .then((page) => { if (alive) setRead({ id, items: (page.items ?? []).filter((m) => m.retired === 'duplicate' && m.retiredBy === id), problem: '' }) })
      // An earlier answer stays usable when a later read fails.
      .catch((e: unknown) => { if (alive) setRead((prev) => prev?.id === id && !prev.problem ? prev : { id, items: [], problem: readProblem(e) }) })
    return () => { alive = false }
  }, [id, changed, attempt])
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
  const changed = useMemoryChange()
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
  }, [wanted, changed, attempt])
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
  const changed = useMemoryChange()
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
  }, [key, id, kinds, includeInferred, changed])
  return key && read?.key === key ? read.total : memoriesFor(state, agent).length
}
