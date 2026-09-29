import { createContext, useContext, type Dispatch } from 'react'
import type { State } from '../domain/types'
import type { Action } from './reducer'

export interface Store {
  state: State
  dispatch: Dispatch<Action>
  runDemoImport: () => void
}

export const StoreContext = createContext<Store | null>(null)

export function useStore(): Store {
  const store = useContext(StoreContext)
  if (!store) throw new Error('useStore must be used inside StoreProvider')
  return store
}
