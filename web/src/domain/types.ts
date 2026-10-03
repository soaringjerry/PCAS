// Domain model for the interactive prototype. It mirrors the PRD's concepts and
// is deliberately close to what the backend will need, but it is not the final
// schema: the backend data model is designed separately.

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
  progress: string
  nextSteps: string[]
  updatedAt: string
}

export type MemoryKind = 'fact' | 'preference' | 'decision' | 'intention' | 'plan'

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

export interface MemoryFacets {
  people: MemoryFacet[]
  places: MemoryFacet[]
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

export interface Source {
  id: ID
  name: string
  method: string
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
  createdAt: string
  updatedAt: string
}

export type RunKind = 'plan' | 'breakdown' | 'summary' | 'draft' | 'ask'
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
  dueAt: string
  createdAt: string
  dismissedAt?: string
}

export interface State {
  /** All memories there are; `memories` holds only the 200 most recently updated. */
  memoryTotal?: number
  notices: Notice[]
  budgetUsage: number
  revision: number
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
