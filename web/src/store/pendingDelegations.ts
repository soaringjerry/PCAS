import type { Action } from './actions'

type DelegationAction = Extract<Action, { type: 'delegateTask' }>
type Envelope = DelegationAction & { requestId: string; expectedRevision: number }
export interface PendingDelegation {
  question: string
  action: DelegationAction
  envelope?: Envelope
}

const KEY = 'pcas.pending-delegations.v1'

export function pendingDelegations(): PendingDelegation[] {
  const raw = sessionStorage.getItem(KEY)
  if (!raw) return []
  const entries = JSON.parse(raw) as PendingDelegation[]
  if (!Array.isArray(entries) || entries.some(entry => !entry.action?.id || entry.action.type !== 'delegateTask' || typeof entry.question !== 'string')) {
    throw new Error('之前提交的编号无法读取，请保留当前页面并检查浏览器存储。')
  }
  return entries
}

function write(entries: PendingDelegation[]) {
  // Persistence is required before a paid request, never best-effort.
  try { sessionStorage.setItem(KEY, JSON.stringify(entries)) }
  catch { throw new Error('浏览器无法保存提交状态，请允许浏览器存储后重试。') }
}

export function rememberDelegation(entry: PendingDelegation) {
  const entries = pendingDelegations()
  const index = entries.findIndex(previous => previous.action.id === entry.action.id)
  if (index < 0) entries.push(entry)
  else entries[index] = entry
  write(entries)
}

export function resolveDelegation(id: string) {
  write(pendingDelegations().filter(entry => entry.action.id !== id))
}

export function delegationEnvelope(action: DelegationAction, revision: number): Envelope {
  const entry = pendingDelegations().find(entry => entry.action.id === action.id)
  if (!entry || JSON.stringify(entry.action) !== JSON.stringify(action)) {
    throw new Error('提交编号与内容不一致，尚未开始执行。请重试之前的提交。')
  }
  if (entry.envelope) return entry.envelope
  const envelope = { ...action, requestId: crypto.randomUUID(), expectedRevision: revision }
  rememberDelegation({ ...entry, envelope })
  return envelope
}

export function resetDelegationEnvelope(id: string) {
  const entry = pendingDelegations().find(entry => entry.action.id === id)
  if (entry) rememberDelegation({ question: entry.question, action: entry.action })
}
