import type { State } from '../domain/types'

/**
 * How many times a command turned down for a moved-on workspace is sent again.
 * Each resend follows a fresh read, so more than one is needed only when the
 * background writes twice within a round trip; after that the refusal is shown.
 */
export const CONFLICT_RESENDS = 2

const idFields = ['id', 'taskId', 'ideaId', 'thingId', 'targetId'] as const
const collections = ['tasks', 'ideas', 'projects', 'memories', 'candidates', 'docs', 'runs', 'samples', 'agents', 'jobs'] as const

/** One record as the user saw it. How faded a memory is drifts with the clock and is left out. */
function seen(state: State, id: string): string {
  for (const name of collections) {
    const found = (state[name] as { id: string }[] | undefined)?.find((r) => r.id === id)
    if (found) return JSON.stringify(found, (field, value) => (field === 'exposure' || field === 'lastUsedAt' ? undefined : value))
  }
  // Not in either snapshot: it was not touched lately, or the command is about to make it.
  return ''
}

/** Whether everything a command acts on reads the same in both snapshots. */
export function sameTargets(action: object, before: State, after: State): boolean {
  const fields = action as Record<string, unknown>
  const ids = new Set<string>()
  for (const name of idFields) if (typeof fields[name] === 'string') ids.add(fields[name])
  if (Array.isArray(fields.ids)) for (const id of fields.ids) if (typeof id === 'string') ids.add(id)
  if (fields.type === 'updateSettings' && JSON.stringify(before.settings) !== JSON.stringify(after.settings)) return false
  for (const id of ids) if (seen(before, id) !== seen(after, id)) return false
  return true
}
