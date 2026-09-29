import { createContext, useContext } from 'react'
import type { RunKind, State } from '../domain/types'
import type { Action } from './actions'

export interface RunRequest {
  thingId: string
  agentId: string
  kind: RunKind
  prompt: string
}

export interface Store {
  state: State
  dispatch: (action: Action) => Promise<boolean>
  importText: (title: string, text: string) => Promise<boolean>
  importAttachment: (file: File) => Promise<boolean>
  refresh: () => Promise<void>
  /** Start an AI run on a thing; returns the run id. */
  runAgent: (request: RunRequest) => Promise<string | undefined>
}

export const StoreContext = createContext<Store | null>(null)

export function useStore(): Store {
  const store = useContext(StoreContext)
  if (!store) throw new Error('useStore must be used inside StoreProvider')
  return store
}
