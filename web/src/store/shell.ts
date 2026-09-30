import { createContext, useContext } from 'react'

export interface ShellApi {
  openPalette: () => void
  /** Unsent instruction per thing, so leaving and coming back keeps it. */
  draft: (thingId: string) => string
  setDraft: (thingId: string, text: string) => void
  /** Puts text into that secretary's draft and focuses its input. */
  prefill: (key: string, text: string) => void
  /** Secretary inputs listen here to take focus after a prefill of their key. */
  onPrefill: (listener: (key: string, text: string) => void) => () => void
  agentFor: (thingId: string) => string
  setAgentFor: (thingId: string, agentId: string) => void
}

export const ShellContext = createContext<ShellApi | null>(null)

export function useShell(): ShellApi {
  const api = useContext(ShellContext)
  if (!api) throw new Error('useShell must be used inside Shell')
  return api
}
