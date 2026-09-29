import { createContext, useContext, type Dispatch } from 'react'
import type { RunKind, State } from '../domain/types'
import type { Action } from './reducer'

export interface RunRequest {
  thingId: string
  agentId: string
  kind: RunKind
  prompt: string
}

export interface Store {
  state: State
  dispatch: Dispatch<Action>
  runDemoImport: () => void
  /** Start an AI run on a thing; returns the run id. */
  runAgent: (request: RunRequest) => string | undefined
}

export const StoreContext = createContext<Store | null>(null)

export function useStore(): Store {
  const store = useContext(StoreContext)
  if (!store) throw new Error('useStore must be used inside StoreProvider')
  return store
}
