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
  /** Which part of the table it belongs in, as the server judged it when read. */
  dateStatus: 'upcoming' | 'expired_unknown' | 'recurring' | 'unclear'
  /** What was actually said, for a date that could not be pinned down. */
  originalText: string
}
