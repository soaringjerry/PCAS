import { createContext, useContext } from 'react'

export interface ShellApi {
  openPalette: () => void
  closeDrawer: () => void
  /** Unsent instruction per thing, so leaving and coming back keeps it. */
  draft: (thingId: string) => string
  setDraft: (thingId: string, text: string) => void
  agentFor: (thingId: string) => string
  setAgentFor: (thingId: string, agentId: string) => void
}

export const ShellContext = createContext<ShellApi | null>(null)

export function useShell(): ShellApi {
  const api = useContext(ShellContext)
  if (!api) throw new Error('useShell must be used inside Shell')
  return api
}
