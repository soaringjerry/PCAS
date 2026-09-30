import type { State, TaskStatus } from './types'

// The secretary's wire format (docs/tasks/phase1/contracts.md §2).

export interface SourcesCard {
  kind: 'sources'
  items: { memoryId: string; version: number; text: string; sourceId?: string | null; sourceVersion?: number | null; at?: string | null }[]
}

export interface LinksCard {
  kind: 'links'
  items: { url: string; host: string }[]
}

export interface TimelineCard {
  kind: 'timeline'
  title?: string
  items: { at?: string | null; text: string; status: 'open' | 'done' | 'dropped'; memoryId?: string | null; thingId?: string | null }[]
}

export interface TasksCard {
  kind: 'tasks'
  items: { thingId: string; title: string; due?: string | null; project?: string | null; status: TaskStatus }[]
}

/** Kinds this build does not know are kept as-is and skipped when drawn. */
export type DeskCard = SourcesCard | LinksCard | TimelineCard | TasksCard | { kind: string; items?: unknown }

export type ReceiptOp = 'create_task' | 'update' | 'create_idea' | 'create_project' | 'add_steps' | 'remember' | 'delegate' | 'capture'

export interface Receipt {
  actionId: string | null
  op: ReceiptOp
  text: string
  thingId: string | null
  undoable: boolean
  /** Whether the action has been undone since; filled in when turns are read back. */
  undone?: boolean
  status: 'done' | 'skipped'
  reason?: string
}

export interface DeskAsk {
  question: string
  options: string[]
}

export interface DeskTurn {
  id: string
  text: string
  reply: string
  cards: DeskCard[]
  receipts: Receipt[]
  ask: DeskAsk | null
  agent: string
  createdAt: string
}

export interface DeskTurnRequest {
  requestId: string
  /** Made by the client when a conversation starts, so its lines can go out together. */
  conversationId: string
  thingId: string | null
  text: string
  agentId: string
}

export interface DeskTurnResponse {
  conversationId: string
  turn: DeskTurn
  state: State
}

export interface DeskTurnsResponse {
  conversationId: string
  turns: DeskTurn[]
}

/* ---------- What survives a reload ---------- */

/** Lines are stored with their exact request before sending; a retry repeats it byte for byte. */
export interface Conversation {
  conversationId: string | null
  unanswered: DeskTurnRequest[]
}

const storageKey = (key: string) => `pcas.secretary.${key}`

export function loadConversation(key: string): Conversation {
  try {
    const raw = localStorage.getItem(storageKey(key))
    if (raw) {
      const saved = JSON.parse(raw) as Partial<Conversation>
      return {
        conversationId: typeof saved.conversationId === 'string' ? saved.conversationId : null,
        unanswered: Array.isArray(saved.unanswered) ? saved.unanswered.filter((r) => typeof r?.requestId === 'string' && typeof r.conversationId === 'string' && typeof r.text === 'string') : [],
      }
    }
  } catch {
    // A fresh conversation is fine.
  }
  return { conversationId: null, unanswered: [] }
}

export function updateConversation(key: string, change: (c: Conversation) => Conversation): Conversation {
  const next = change(loadConversation(key))
  try {
    if (next.conversationId || next.unanswered.length) localStorage.setItem(storageKey(key), JSON.stringify(next))
    else localStorage.removeItem(storageKey(key))
  } catch {
    // Without storage the conversation still works; it just will not survive a reload.
  }
  return next
}

/** Where a receipt's thing title sits inside its text, so it can become a link. */
export function splitTitle(text: string, title: string | undefined): [string, string, string] | null {
  if (!title) return null
  const at = text.indexOf(title)
  return at < 0 ? null : [text.slice(0, at), title, text.slice(at + title.length)]
}
