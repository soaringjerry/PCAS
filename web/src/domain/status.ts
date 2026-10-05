import type { ID, Memory } from './types'

export interface Handover {
  body: string
  builtAt: string
  stale: boolean
}

export type StatusCardKind = 'project' | 'topic' | 'area' | 'person' | 'self'
export type StatusCardFieldKind = 'status' | 'deadline' | 'decided' | 'blocker' | 'next' | 'preference' | 'people'

export interface StatusCardRef {
  key: string
  kind: StatusCardKind
  name: string
  count: number
  builtAt: string
  stale: boolean
}

export interface StatusCardField {
  field: StatusCardFieldKind
  items: Memory[]
}

export interface StatusCard extends StatusCardRef {
  fields: StatusCardField[]
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

export interface About {
  handover: Handover
  cards: StatusCard[]
  deadlines: Deadline[]
  building: { done: number; total: number }
}
