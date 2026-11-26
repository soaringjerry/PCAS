import { useCallback, useEffect, useRef, useState } from 'react'
import { civilDate } from '../domain/time'
import type { BackgroundHealth, DayActivity, UsageCall, UsageCallsPage, UsageSummary } from '../domain/observe'
import { api } from './api'
import { readProblem } from './memories'

const DAY_PAGE_LIMIT = 500
const CALLS_PAGE_LIMIT = 50

export interface AsyncData<T> {
  phase: 'loading' | 'ready' | 'failed'
  data: T | null
  problem: string
  reload: () => void
}

export interface DayActivityState extends AsyncData<DayActivity> {
  more: 'idle' | 'loading' | 'failed'
  moreProblem: string
  loadMore: () => void
}

export function useDayActivity(date: string, enabled: boolean): DayActivityState {
  const [state, setState] = useState<{
    key: string
    phase: 'loading' | 'ready' | 'failed'
    data: DayActivity | null
    problem: string
    more: 'idle' | 'loading' | 'failed'
    moreProblem: string
  }>({ key: '', phase: 'loading', data: null, problem: '', more: 'idle', moreProblem: '' })
  const [tick, setTick] = useState(0)
  const loadingMore = useRef(false)

  const reload = useCallback(() => setTick((n) => n + 1), [])

  useEffect(() => {
    if (!enabled || !date) return
    let alive = true
    const path = `/v1/workspace/activity/day?date=${encodeURIComponent(date)}&limit=${DAY_PAGE_LIMIT}&offset=0`
    api<DayActivity>(path)
      .then((data) => {
        if (alive) setState({ key: date, phase: 'ready', data, problem: '', more: 'idle', moreProblem: '' })
      })
      .catch((e: unknown) => {
        if (alive) setState((prev) => ({ key: date, phase: 'failed', data: prev.key === date ? prev.data : null, problem: readProblem(e), more: 'idle', moreProblem: '' }))
      })
    return () => { alive = false }
  }, [date, enabled, tick])

  const loadMore = useCallback(() => {
    const current = state.key === date ? state.data : null
    if (!current || current.omittedEvents <= 0 || loadingMore.current) return
    const offset = current.events.length
    loadingMore.current = true
    setState((prev) => (prev.key === date ? { ...prev, more: 'loading', moreProblem: '' } : prev))
    const path = `/v1/workspace/activity/day?date=${encodeURIComponent(date)}&limit=${DAY_PAGE_LIMIT}&offset=${offset}`
    api<DayActivity>(path)
      .then((page) => {
        setState((prev) => {
          if (prev.key !== date || !prev.data) return prev
          const seen = new Set(prev.data.events.map((e) => `${e.kind}:${e.eventId}`))
          const merged = [...prev.data.events, ...page.events.filter((e) => !seen.has(`${e.kind}:${e.eventId}`))]
          return {
            ...prev,
            data: { ...page, events: merged },
            more: 'idle',
            moreProblem: '',
          }
        })
      })
      .catch((e: unknown) => {
        setState((prev) => (prev.key === date ? { ...prev, more: 'failed', moreProblem: readProblem(e) } : prev))
      })
      .finally(() => { loadingMore.current = false })
  }, [date, state.data, state.key])

  const current = state.key === date ? state : undefined
  return {
    phase: current ? current.phase : 'loading',
    data: current?.data ?? null,
    problem: current?.problem ?? '',
    more: current?.more ?? 'idle',
    moreProblem: current?.moreProblem ?? '',
    loadMore,
    reload,
  }
}

export function useUsageSummary(days: number, timezone: string, enabled: boolean): AsyncData<UsageSummary> {
  const to = civilDate(0, timezone)
  const from = civilDate(-(days - 1), timezone)
  const key = `${from}:${to}`
  const [state, setState] = useState<{ key: string; phase: 'loading' | 'ready' | 'failed'; data: UsageSummary | null; problem: string }>({
    key: '',
    phase: 'loading',
    data: null,
    problem: '',
  })
  const [tick, setTick] = useState(0)
  const reload = useCallback(() => setTick((n) => n + 1), [])

  useEffect(() => {
    if (!enabled) return
    let alive = true
    api<UsageSummary>(`/v1/workspace/usage?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}`)
      .then((data) => {
        if (alive) setState({ key, phase: 'ready', data, problem: '' })
      })
      .catch((e: unknown) => {
        if (alive) setState({ key, phase: 'failed', data: null, problem: readProblem(e) })
      })
    return () => { alive = false }
  }, [enabled, from, to, key, tick])

  const current = state.key === key ? state : undefined
  return {
    phase: current ? current.phase : 'loading',
    data: current?.data ?? null,
    problem: current?.problem ?? '',
    reload,
  }
}

export interface UsageCallsState {
  phase: 'loading' | 'ready' | 'failed'
  items: UsageCall[]
  hasMore: boolean
  problem: string
  more: 'idle' | 'loading' | 'failed'
  moreProblem: string
  loadMore: () => void
  reload: () => void
}

export function useUsageCalls(enabled: boolean): UsageCallsState {
  const [state, setState] = useState<{
    loaded: boolean
    phase: 'loading' | 'ready' | 'failed'
    items: UsageCall[]
    next: string
    problem: string
    more: 'idle' | 'loading' | 'failed'
    moreProblem: string
  }>({ loaded: false, phase: 'loading', items: [], next: '', problem: '', more: 'idle', moreProblem: '' })
  const [tick, setTick] = useState(0)
  const loadingMore = useRef(false)
  const reload = useCallback(() => setTick((n) => n + 1), [])

  useEffect(() => {
    if (!enabled) return
    let alive = true
    api<UsageCallsPage>(`/v1/workspace/usage/calls?limit=${CALLS_PAGE_LIMIT}`)
      .then((page) => {
        if (alive) setState({ loaded: true, phase: 'ready', items: page.items ?? [], next: page.next ?? '', problem: '', more: 'idle', moreProblem: '' })
      })
      .catch((e: unknown) => {
        if (alive) setState((prev) => ({ ...prev, loaded: true, phase: 'failed', problem: readProblem(e), more: 'idle', moreProblem: '' }))
      })
    return () => { alive = false }
  }, [enabled, tick])

  const loadMore = useCallback(() => {
    if (!state.next || loadingMore.current) return
    loadingMore.current = true
    setState((prev) => ({ ...prev, more: 'loading', moreProblem: '' }))
    api<UsageCallsPage>(`/v1/workspace/usage/calls?limit=${CALLS_PAGE_LIMIT}&before=${encodeURIComponent(state.next)}`)
      .then((page) => {
        setState((prev) => {
          const seen = new Set(prev.items.map((item) => item.id))
          return {
            ...prev,
            items: [...prev.items, ...(page.items ?? []).filter((item) => !seen.has(item.id))],
            next: page.next ?? '',
            more: 'idle',
            moreProblem: '',
          }
        })
      })
      .catch((e: unknown) => {
        setState((prev) => ({ ...prev, more: 'failed', moreProblem: readProblem(e) }))
      })
      .finally(() => { loadingMore.current = false })
  }, [state.next])

  return {
    phase: state.loaded ? state.phase : 'loading',
    items: state.items,
    hasMore: Boolean(state.next),
    problem: state.problem,
    more: state.more,
    moreProblem: state.moreProblem,
    loadMore,
    reload,
  }
}

export function useBackgroundHealth(enabled: boolean): AsyncData<BackgroundHealth> {
  const [state, setState] = useState<{ loaded: boolean; phase: 'loading' | 'ready' | 'failed'; data: BackgroundHealth | null; problem: string }>({
    loaded: false,
    phase: 'loading',
    data: null,
    problem: '',
  })
  const [tick, setTick] = useState(0)
  const reload = useCallback(() => setTick((n) => n + 1), [])

  useEffect(() => {
    if (!enabled) return
    let alive = true
    api<BackgroundHealth>('/v1/workspace/background')
      .then((data) => {
        if (alive) setState({ loaded: true, phase: 'ready', data, problem: '' })
      })
      .catch((e: unknown) => {
        if (alive) setState({ loaded: true, phase: 'failed', data: null, problem: readProblem(e) })
      })
    return () => { alive = false }
  }, [enabled, tick])

  return {
    phase: state.loaded ? state.phase : 'loading',
    data: state.data,
    problem: state.problem,
    reload,
  }
}
