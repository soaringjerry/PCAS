import { dayOffset } from './time'
import type { EventPrecision, MemoryMention, State, TaskStatus } from './types'

// The secretary's wire format (docs/tasks/phase1/contracts.md §2).

export interface SourcesCard {
  kind: 'sources'
  /** `source` is a line as it was said, with `memoryId` naming that record; `claim`, or no kind, is a memory. */
  items: { kind?: 'claim' | 'source'; memoryId: string; version: number; text: string; sourceId?: string | null; sourceVersion?: number | null; at?: string | null }[]
}

export interface LinksCard {
  kind: 'links'
  items: { url: string; host: string }[]
}

export interface TimelineCard {
  kind: 'timeline'
  title?: string
  items: { at?: string | null; text: string; status: 'open' | 'done' | 'dropped' | 'changed'; eventFrom?: string; eventTo?: string; eventPrecision?: EventPrecision; mentions?: MemoryMention[]; memoryId?: string | null; thingId?: string | null }[]
}

export interface TasksCard {
  kind: 'tasks'
  items: { thingId: string; title: string; due?: string | null; project?: string | null; status: TaskStatus }[]
}

/** Kinds this build does not know are kept as-is and skipped when drawn. */
export type DeskCard = SourcesCard | LinksCard | TimelineCard | TasksCard | { kind: string; items?: unknown }

/** A card this build can draw, with something in it. */
export function isKnownCard(card: DeskCard): card is SourcesCard | LinksCard | TimelineCard | TasksCard {
  return ['sources', 'links', 'timeline', 'tasks'].includes(card.kind) && Array.isArray(card.items) && card.items.length > 0
}

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
  /** What this answer drew on has been changed since; the turn itself is shown as it was. */
  outdated?: boolean
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

/* ---------- The two times on a timeline ---------- */

type Moment = TimelineCard['items'][number]

/** The calendar day an instant falls on in the workspace zone. */
function dayOf(at: Date, timeZone: string) {
  const parts = new Intl.DateTimeFormat('zh-CN', { timeZone, year: 'numeric', month: 'numeric', day: 'numeric', weekday: 'short' }).formatToParts(at)
  const value = (type: string) => parts.find((p) => p.type === type)!.value
  return { year: value('year'), month: value('month'), day: value('day'), weekday: value('weekday') }
}

/** The day something was said: "今天", "10月3日", and the year apart when it is not this one. Nothing for a missing or broken time. */
export function saidDay(at: string | null | undefined, timeZone: string): { day: string; year?: string } | null {
  if (!at || Number.isNaN(new Date(at).getTime())) return null
  const offset = dayOffset(at, timeZone)
  if (offset === 0) return { day: '今天' }
  if (offset === -1) return { day: '昨天' }
  if (offset === 1) return { day: '明天' }
  const d = dayOf(new Date(at), timeZone)
  return { day: `${d.month}月${d.day}日`, year: d.year === dayOf(new Date(), timeZone).year ? undefined : `${d.year}年` }
}

/**
 * When the thing a moment talks about happens, as exact as the words were:
 * "10月9日 周五", "2025年3月", "2025年", "9月14日至20日". Empty when it is not
 * known, or when it is the very day the words were said.
 */
export function eventText(item: Pick<Moment, 'at' | 'eventFrom' | 'eventTo' | 'eventPrecision'>, timeZone: string): string {
  const { eventFrom, eventTo, eventPrecision: precision } = item
  if (!eventFrom || !precision || precision === 'unknown') return ''
  const start = new Date(eventFrom)
  if (Number.isNaN(start.getTime())) return ''
  const a = dayOf(start, timeZone)
  if (precision === 'year') return `${a.year}年`
  if (precision === 'month') return `${a.year}年${a.month}月`
  // The year is left out when it is the one the margin already shows: the year it was said, or this year.
  const said = item.at && !Number.isNaN(new Date(item.at).getTime()) ? dayOf(new Date(item.at), timeZone) : null
  const day = `${a.year === (said ?? dayOf(new Date(), timeZone)).year ? '' : `${a.year}年`}${a.month}月${a.day}日`
  // The interval excludes its end, so the last day it covers is the one before.
  const end = eventTo ? new Date(new Date(eventTo).getTime() - 1) : start
  const b = Number.isNaN(end.getTime()) || end < start ? a : dayOf(end, timeZone)
  if (precision === 'day' || (a.year === b.year && a.month === b.month && a.day === b.day)) {
    return said && said.year === a.year && said.month === a.month && said.day === a.day ? '' : `${day} ${a.weekday}`
  }
  if (a.year !== b.year) return `${a.year}年${a.month}月${a.day}日至${b.year}年${b.month}月${b.day}日`
  return a.month === b.month ? `${day}至${b.day}日` : `${day}至${b.month}月${b.day}日`
}

/** Where a receipt's thing title sits inside its text, so it can become a link. */
export function splitTitle(text: string, title: string | undefined): [string, string, string] | null {
  if (!title) return null
  const at = text.indexOf(title)
  return at < 0 ? null : [text.slice(0, at), title, text.slice(at + title.length)]
}
