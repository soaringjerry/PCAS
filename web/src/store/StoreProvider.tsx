import { useCallback, useEffect, useMemo, useReducer, useRef, type ReactNode } from 'react'
import type { State } from '../domain/types'
import { StoreContext } from './context'
import { reducer } from './reducer'
import { createSeed, STATE_VERSION } from './seed'

// The prototype keeps everything in the browser. The backend replaces this
// module; pages only talk to the store through actions.
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
  const timers = useRef<number[]>([])

  useEffect(() => {
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

  const value = useMemo(() => ({ state, dispatch, runDemoImport }), [state, runDemoImport])
  return <StoreContext.Provider value={value}>{children}</StoreContext.Provider>
}
