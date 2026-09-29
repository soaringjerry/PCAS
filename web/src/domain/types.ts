// Domain model for the interactive prototype. It mirrors the PRD's concepts and
// is deliberately close to what the backend will need, but it is not the final
// schema: the backend data model is designed separately.

export type ID = string

/** How much a piece of knowledge can be trusted (PRD §2). */
export type Epistemic = 'confirmed' | 'inferred' | 'planned'

export type Actor = 'user' | 'ai' | 'import' | 'system'

/** Points back to the raw material a record came from. */
export interface SourceRef {
  sourceId: ID
  label: string
  excerpt?: string
  at: string
}

export interface Revision {
  at: string
  by: Actor
  summary: string
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
}

export interface Task {
  id: ID
  title: string
  notes?: string
  status: TaskStatus
  projectId?: ID
  ideaId?: ID
  due?: string
  scheduled?: string
  waitingFor?: string
  dependsOn: ID[]
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
  id: ID
  name: string
  goal: string
  status: ProjectStatus
  progress: string
  nextSteps: string[]
  updatedAt: string
}

export type MemoryKind = 'fact' | 'preference' | 'decision'

export interface MemoryVersion {
  at: string
  by: Actor
  text: string
  reason?: string
}

export interface Memory {
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

export type CandidateKind = 'task' | 'idea' | 'memory'
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

export type SourceStatus = 'connected' | 'manual' | 'unverified' | 'failed'

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

export interface HandoffSections {
  goal: string
  background: string
  progress: string
  decisions: string
  constraints: string
  expectedOutput: string
}

export type HandoffStatus = 'draft' | 'sent' | 'returned' | 'adopted'

export interface HandoffResult {
  at: string
  text: string
  userEdit?: string
}

export interface Handoff {
  id: ID
  title: string
  agentId: ID
  projectId?: ID
  taskId?: ID
  ideaId?: ID
  sections: HandoffSections
  memoryIds: ID[]
  status: HandoffStatus
  result?: HandoffResult
  /** Set when a memory it was built from changed or was deleted. */
  stale: boolean
  createdAt: string
  updatedAt: string
}

export type SampleState = 'candidate' | 'included' | 'excluded'

export interface TrainingSample {
  id: ID
  kind: 'correction' | 'adopted-result' | 'conversation'
  prompt: string
  response: string
  origin: { label: string; handoffId?: ID; memoryId?: ID }
  version: number
  state: SampleState
  epistemic: Epistemic
  stale: boolean
  createdAt: string
}

/** How far the system may act on its own (PRD §5 主动但可控). */
export interface Settings {
  /** Accept high-confidence captures without asking. */
  autoAccept: boolean
  /** Bring shelved ideas back when their conditions are met. */
  wakeIdeas: boolean
  /** Run follow-up reminders created from events. */
  followUps: boolean
  /** Local time of the daily review, "HH:MM". */
  dailyReviewAt: string
}

export interface State {
  version: number
  settings: Settings
  tasks: Task[]
  ideas: Idea[]
  projects: Project[]
  memories: Memory[]
  candidates: Candidate[]
  sources: Source[]
  jobs: Job[]
  agents: Agent[]
  handoffs: Handoff[]
  samples: TrainingSample[]
  /** Guided demo of the PRD §7 scenario. */
  demo: { costReportImported: boolean }
}
