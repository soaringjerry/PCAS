import { createContext, useContext } from 'react'

/** Layout state of the workbench, kept per browser and separate from data. */
export interface WorkspaceState {
  /** Open tabs, as route paths. */
  tabs: string[]
  sidebar: boolean
  context: boolean
  /** Projects expanded in the sidebar tree. */
  expanded: Record<string, boolean>
  /** Unsent AI instructions per thing, so switching tabs keeps them. */
  drafts: Record<string, string>
  /** Agent picked per thing, shared by the composer and the context pane. */
  agentFor: Record<string, string>
  /** Thing previewed in the context pane from a list view. */
  selected?: string
  /** Phone layout: which drawer is open. */
  drawer: 'sidebar' | 'context' | null
}

export interface WorkspaceApi {
  ws: WorkspaceState
  closeTab: (path: string) => void
  toggleSidebar: () => void
  toggleContext: () => void
  toggleExpanded: (projectId: string) => void
  setDraft: (thingId: string, text: string) => void
  setAgentFor: (thingId: string, agentId: string) => void
  setSelected: (thingId?: string) => void
  setDrawer: (drawer: WorkspaceState['drawer']) => void
  openPalette: () => void
}

export const WorkspaceContext = createContext<WorkspaceApi | null>(null)

export function useWorkspace(): WorkspaceApi {
  const api = useContext(WorkspaceContext)
  if (!api) throw new Error('useWorkspace must be used inside the workbench')
  return api
}
