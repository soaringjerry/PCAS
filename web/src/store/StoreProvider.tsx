import { useCallback, useEffect, useMemo, useReducer, useRef, type ReactNode } from 'react'
import { buildBrief, contextFor, mockOutput } from '../domain/agent'
import { newId } from '../domain/ids'
import { findThing } from '../domain/things'
import { nowIso } from '../domain/time'
import type { State } from '../domain/types'
import { StoreContext, type RunRequest } from './context'
import { reducer } from './reducer'
import { createSeed, STATE_VERSION } from './seed'

// The prototype keeps everything in the browser and simulates agents. The
// backend replaces this module; pages only talk to the store through actions.
const STORAGE_KEY = 'pcas.prototype.state'

function load(): State {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const parsed = JSON.parse(raw) as State
      if (parsed.version === STATE_VERSION) return parsed
    }
  } catch {
    // Storage can be unavailable (private mode) or hold stale data.
  }
  return createSeed()
}

export function StoreProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(reducer, undefined, load)
  const stateRef = useRef(state)
  const timers = useRef<number[]>([])

  useEffect(() => {
    stateRef.current = state
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(state))
    } catch {
      // Ignore: the prototype still works without persistence.
    }
  }, [state])

  useEffect(() => () => timers.current.forEach((t) => window.clearTimeout(t)), [])

  const runDemoImport = useCallback(() => {
    dispatch({ type: 'demoImport', step: 'start' })
    timers.current.push(
      window.setTimeout(() => dispatch({ type: 'demoImport', step: 'extract' }), 1200),
      window.setTimeout(() => dispatch({ type: 'demoImport', step: 'finish' }), 2600),
    )
  }, [])

  const runAgent = useCallback(({ thingId, agentId, kind, prompt }: RunRequest) => {
    const current = stateRef.current
    const thing = findThing(current, thingId)
    const agent = current.agents.find((a) => a.id === agentId)
    if (!thing || !agent) return undefined
    const memories = contextFor(current, thing, agent)
      .filter((c) => c.included)
      .map((c) => c.memory)
    const id = newId('r')
    const manual = agent.channel === 'manual'
    dispatch({
      type: 'startRun',
      run: {
        id,
        thingId,
        agentId,
        kind,
        prompt,
        brief: buildBrief(current, thing, prompt, memories),
        contextMemoryIds: memories.map((m) => m.id),
        status: manual ? 'waiting' : 'running',
        staleContext: false,
        createdAt: nowIso(),
      },
    })
    if (!manual) {
      const output = mockOutput(current, thingId, kind, prompt, memories)
      timers.current.push(window.setTimeout(() => dispatch({ type: 'finishRun', id, output }), 1400))
    }
    return id
  }, [])

  const value = useMemo(() => ({ state, dispatch, runDemoImport, runAgent }), [state, runDemoImport, runAgent])
  return <StoreContext.Provider value={value}>{children}</StoreContext.Provider>
}
