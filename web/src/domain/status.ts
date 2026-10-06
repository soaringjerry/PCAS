import type { ID } from './types'

export interface Handover {
  body: string
  /** When this note was written; empty when there is none yet. */
  builtAt: string
  /** A newer one is on its way; this is the one before it. */
  stale: boolean
}

export interface Deadline {
  id: ID
  kind: 'deadline' | 'appointment' | 'recurring'
  at: string | null
  recurrence: string
  title: string
  timeNote: string
  memoryId: ID
}

/** What matters right now: the note a new helper would be handed, and the dates to keep. */
export interface Now {
  handover: Handover
  deadlines: Deadline[]
}
