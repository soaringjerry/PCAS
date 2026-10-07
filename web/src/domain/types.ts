// Domain model for the interactive prototype. It mirrors the PRD's concepts and
// is deliberately close to what the backend will need, but it is not the final
// schema: the backend data model is designed separately.

import type { Handover } from './studio'

export type ID = string

/** How much a piece of knowledge can be trusted (PRD §2). */
export type Epistemic = 'confirmed' | 'sourced' | 'inferred' | 'planned'

export type Actor = 'user' | 'ai' | 'import' | 'system'

/** Points back to the raw material a record came from. */
export interface SourceRef {
  version?: number
  sourceId: ID
  label: string
  excerpt?: string
  at: string
}

export interface Revision {
  at: string
  by: 'user' | 'secretary' | 'assistant' | 'system'
  summary: string
}

export interface ChecklistItem {
  id: ID
  text: string
  done: boolean
}

export type TaskStatus = 'todo' | 'doing' | 'waiting' | 'done' | 'cancelled'

/** A combined event and time trigger (PRD §4). */
export interface Trigger {
  id: ID
  kind: 'time' | 'event' | 'event+delay'
  description: string
  /** Checked against the latest state right before firing. */
  guard?: string
  nextAt?: string
  active: boolean
  /** For the `due-reminder` trigger: `-30m`, `-2h`, `at` or `HH:MM` relative to the due time. */
  offset?: string
}

export interface Task {
  hasRetainedWriting?: boolean
  id: ID
  title: string
  notes?: string
  status: TaskStatus
  projectId?: ID
  ideaId?: ID
  due?: string
  scheduled?: string
  waitingFor?: string
  /** Someone is waiting on you for this; counts as urgent. */
  owedTo?: { who: string; since: string }
  /** The user said this cannot wait; while it has no time it leads today's timeline. */
  urgent?: boolean
  /** How much work it is thought to be, in hours; the user's own figure is never overwritten. */
  estimatedHours?: number | null
  /** The day to start by, worked back from the due time; the server computes it. */
  startDate?: string | null
  effortReason?: string
  effortSource?: 'model' | 'user'
  dependsOn: ID[]
  checklist: ChecklistItem[]
  triggers: Trigger[]
  sources: SourceRef[]
  history: Revision[]
  createdAt: string
  updatedAt: string
}

export type IdeaStatus = 'active' | 'shelved' | 'awakened' | 'promoted' | 'dropped'

export interface RevisitCondition {
  id: ID
  kind: 'time' | 'info' | 'event'
  description: string
  dueAt?: string
  met: boolean
  metAt?: string
  metBy?: SourceRef
}

export interface Wake {
  at: string
  reason: string
  conditionId?: ID
  snoozedUntil?: string
}

export interface Idea {
  hasRetainedWriting?: boolean
  id: ID
  title: string
  body: string
  status: IdeaStatus
  projectId?: ID
  shelvedReason?: string
  conditions: RevisitCondition[]
  wake?: Wake
  /** Follow-up reminders stop when the user turns them off. */
  remindersOn: boolean
  evolution: Revision[]
  sources: SourceRef[]
  createdAt: string
  updatedAt: string
}

export type ProjectStatus = 'active' | 'paused' | 'done'

export interface Project {
  hasRetainedWriting?: boolean
  id: ID
  name: string
  goal: string
  status: ProjectStatus
  /** The project's handover, as the studio's status block shows it. */
  projectHandover?: Handover
  /** @deprecated Retired in phase 3; the handover says where a project stands. Still sent, no longer read. */
  progress?: string
  /** @deprecated Retired in phase 3, like `progress`. */
  nextSteps?: string[]
  updatedAt: string
}

export type MemoryKind = 'fact' | 'preference' | 'decision' | 'intention' | 'plan'
export type MemoryCategory = 'identity' | 'taste' | 'rule' | 'goal' | 'progress' | 'event' | 'opinion' | 'other_person' | 'unknown'

export type MemoryTrust = 'stated' | 'repeated' | 'tentative' | 'reported' | 'inferred'
export type MemoryRetired = 'superseded' | 'duplicate'

export interface MemoryGroup {
  entityId: ID
  name: string
  type: 'project' | 'topic' | 'area'
}

export interface MemoryVersion {
  at: string
  by: Actor
  text: string
  reason?: string
}

export type EventPrecision = 'unknown' | 'day' | 'month' | 'year' | 'range'

export interface MemoryMention {
  entityId: ID
  name: string
  role: 'person' | 'place' | 'organization' | 'thing'
}

export interface Memory {
  /** On a card of what is asked of the assistant: the kind of task this one applies to; absent when it always applies. */
  appliesTo?: string
	trust?: MemoryTrust
	retired?: MemoryRetired
	retiredBy?: ID
	mergedFrom?: number
  category?: MemoryCategory
  durable?: boolean
  groups?: MemoryGroup[]
  contextDependent?: boolean
  expressedAt?: string
  eventFrom?: string
  eventTo?: string
  eventPrecision?: EventPrecision
  mentions?: MemoryMention[]
	confirmation?: 'unknown' | 'candidate' | 'adopted' | 'confirmed' | 'disputed'
	acquisition?: 'direct' | 'reported' | 'inferred' | 'execution'
  halfLifeDays?: number
  reinforcementLimit?: number
  recordVersion: number
  pinned: boolean
  id: ID
  kind: MemoryKind
  text: string
  epistemic: Epistemic
  projectId?: ID
  sources: SourceRef[]
  versions: MemoryVersion[]
  /** Agents this memory may be shared with. */
  visibleTo: ID[]
  /** 0..1. Lowers when a memory goes unused; never removes it. */
  exposure: number
  lastUsedAt: string
}

/** One page of `GET /v1/workspace/memories`, most recently updated first. */
export interface MemoryPage {
  items: Memory[]
  /** Cursor for the page after this one; absent on the last page. */
  next?: string
  total: number
}

/** A person or place with the number of memories that mention it. */
export interface MemoryFacet {
  entityId: ID
  name: string
  count: number
}

export interface MemoryGroupFacet extends MemoryGroup {
  count: number
}

export interface MemoryFacets {
  groups?: MemoryGroupFacet[]
  people: MemoryFacet[]
  places: MemoryFacet[]
}

/** One entry of the directory of groups: a person, a project, a topic, an area of life, or a kind of memory about the user. */
export interface MemoryGroupEntry {
  /** Names the group to the server; means nothing here. */
  key: string
  kind: 'person' | 'project' | 'topic' | 'area' | 'self'
  name: string
  /** How many current memories are under it. */
  count: number
}

/** Something the user asks of the assistant, after the ones saying the same were merged. */
export interface AssistantRequirement {
  memoryId: ID
  text: string
  /** It holds in every turn; otherwise `scope` says when it does. */
  unrestricted: boolean
  scope: string
}

export type CandidateKind = 'task' | 'idea' | 'memory' | 'unknown'
export type CandidateState = 'pending' | 'accepted' | 'ignored' | 'merged'

export interface Candidate {
  id: ID
  kind: CandidateKind
  text: string
  memoryKind?: MemoryKind
  projectId?: ID
  due?: string
  confidence: number
  source: SourceRef
  state: CandidateState
  resolvedInto?: ID
  createdAt: string
}

export type SourceStatus = 'connected' | 'manual' | 'unverified' | 'failed' | 'syncing'

/** One entry in the library's list of where material came from. */
export interface Source {
  /** The original's id when `single`; otherwise the key its originals are listed under. */
  id: ID
  name: string
  kind: 'said' | 'note' | 'capture' | 'telegram' | 'import' | 'file'
  /** One stored original, opened directly; otherwise many, opened as a list. */
  single: boolean
  status: SourceStatus
  note: string
  itemCount: number
  lastSyncAt?: string
}

export type JobStatus = 'queued' | 'running' | 'waiting' | 'failed' | 'done'

export interface Job {
  id: ID
  title: string
  trigger: 'event' | 'time' | 'manual'
  status: JobStatus
  progress?: number
  detail: string
  recovery?: string
  createdAt: string
  nextRunAt?: string
}

export interface Agent {
  default?: boolean
  protocol?: string
  available: boolean
  inputPrice: number
  outputPrice: number
  maxOutput: number
  id: ID
  name: string
  channel: 'mcp' | 'api' | 'manual'
  note: string
  enabled: boolean
  /** Memory kinds this agent may read. */
  memoryKinds: MemoryKind[]
  /** Only confirmed memories unless the user widens it. */
  includeInferred: boolean
}

/** A working document attached to a thing: plans, notes, specs, AI drafts. */
export interface Doc {
  id: ID
  thingId: ID
  title: string
  body: string
  by: 'user' | 'ai'
  runId?: ID
  /** The number of the version shown, and the one it was written on. */
  version?: number
  basedOn?: number | null
  createdAt: string
  updatedAt: string
}

export type RunKind = 'plan' | 'breakdown' | 'summary' | 'draft' | 'ask' | 'revise'
export type RunStatus = 'running' | 'waiting' | 'done' | 'failed'

/**
 * One piece of work an AI does on a thing (PRD §3 多 AI 接入). The brief is
 * exactly what the agent received; results come back onto the same thing.
 */
export interface Run {
  error?: string
  id: ID
  thingId: ID
  agentId: ID
  kind: RunKind
  prompt: string
  brief: string
  contextMemoryIds: ID[]
  status: RunStatus
  output?: string
  /** What the user did with the output. */
  adopted?: {
    as: 'doc' | 'subtasks' | 'progress'
    at: string
    edited: boolean
    /** The recorded action behind the adoption; undoing it puts the result back. */
    actionId?: string
    /** Adopted by the worker as soon as the run finished, not by the user. */
    auto?: boolean
  }
  /** For `revise`: the document it rewrites and the version it starts from. */
  documentId?: ID
  baseVersion?: number
  /** The document versions it was actually given. */
  documentVersions?: { documentId: ID; version: number }[]
  /** When the project handover it read was written; absent when it read none. */
  projectHandoverWrittenAt?: string | null
  /** A memory it used changed or was deleted afterwards. */
  staleContext: boolean
  /** Estimated cost in CNY, charged against the daily budget. */
  cost: number
  createdAt: string
  finishedAt?: string
}

export type SampleState = 'candidate' | 'included' | 'excluded'

export interface TrainingSample {
  id: ID
  kind: 'correction' | 'adopted-result' | 'conversation'
  prompt: string
  response: string
  origin: { label: string; runId?: ID; memoryId?: ID }
  version: number
  state: SampleState
  epistemic: Epistemic
  stale: boolean
  createdAt: string
}

/** How far the system may act on its own (PRD §5 主动但可控). */
export interface Settings {
  /** IANA zone shared by the secretary, reminders and the hall. */
  timezone?: string
  /** Accept high-confidence captures without asking. */
  autoAccept: boolean
  /** Bring shelved ideas back when their conditions are met. */
  wakeIdeas: boolean
  /** Run follow-up reminders created from events. */
  followUps: boolean
  /** Local time of the daily review, "HH:MM". */
  dailyReviewAt: string
  /** Daily spend limit for button-triggered AI work, in CNY. */
  dailyBudget: number
  /** Where the owner usually is; the desk uses it for weather and "nearby". */
  city?: string
}

/** One line of what the background did, already in the user's words. */
export interface Activity {
  id: ID
  at: string
  text: string
  to?: string
  failed?: boolean
}

export interface Notice {
  id: ID
  thingId: ID
  title: string
  reason: string
  /** How handed-off work ended, rather than a reminder that came due. */
  result?: boolean
  dueAt: string
  createdAt: string
  dismissedAt?: string
}

export interface Organize {
  done: number
  total: number
  version: number
}

export interface State {
  organize?: Organize
  /** All memories there are; `memories` holds only the 200 most recently updated. */
  memoryTotal?: number
  notices: Notice[]
  budgetUsage: number
  revision: number
  /** Moves only when a memory, its groups or its names change. Absent on an older server. */
  memoryRevision?: number
  version: number
  settings: Settings
  tasks: Task[]
  ideas: Idea[]
  projects: Project[]
  memories: Memory[]
  candidates: Candidate[]
  sources: Source[]
  jobs: Job[]
  activity: Activity[]
  agents: Agent[]
  docs: Doc[]
  runs: Run[]
  /** Memories the user left out of a thing's AI context. */
  excludedMemories: Record<ID, ID[]>
  samples: TrainingSample[]
}
