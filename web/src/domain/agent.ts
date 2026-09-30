import type { Agent, Memory, State } from './types'

// Memory visibility for settings. The server independently enforces grants
// and assembles the authoritative versioned brief at execution time.

export function memoriesFor(state: State, agent: Agent, projectId?: string): Memory[] {
  return state.memories.filter(
    (m) =>
      m.visibleTo.includes(agent.id) &&
      agent.memoryKinds.includes(m.kind) &&
      (agent.includeInferred || m.epistemic !== 'inferred') &&
      (!projectId || !m.projectId || m.projectId === projectId),
  )
}

export function parseChecklist(output: string): string[] {
  return output
    .split('\n')
    .map((l) => l.match(/^\s*- \[[ x]\]\s+(.+)$/)?.[1]?.trim())
    .filter((l): l is string => Boolean(l))
}
