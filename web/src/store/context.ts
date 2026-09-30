import { createContext, useContext } from 'react'
import type { RunKind, State } from '../domain/types'
import type { Action } from './actions'

export interface RunRequest {
  thingId: string
  agentId: string
  kind: RunKind
  prompt: string
}

/** `error` is the text to show; `code` is the server's code, e.g. `already_undone`. */
export type UndoOutcome = { ok: true } | { ok: false; error: string; code?: string }

export interface Store {
  state: State
  dispatch: (action: Action) => Promise<boolean>
  /** Dispatches, then shows `label` with 【撤销】 for 8 seconds. */
  dispatchUndoable: (action: Action, label: string) => Promise<boolean>
  /** Undoes a recorded action; failures are shown as a toast. */
  undo: (actionId: string) => Promise<boolean>
  /** Like `undo`, but hands the reason back instead of showing it. */
  tryUndo: (actionId: string) => Promise<UndoOutcome>
  /** Takes a state the server returned elsewhere, unless it is older than ours. */
  applyState: (state: State) => void
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
