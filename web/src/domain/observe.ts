import type { Tone } from './labels'
import observeLabels from './observe-labels.json'

export type ActivityEventKind = 'call' | 'usage' | 'action' | 'undo' | 'run' | 'stage' | 'decision'

export interface ActivityEventEvidence {
  provider?: string
  model?: string
  prompt?: string
  usageId?: string
  accounting?: string
  inputTokens?: number
  outputTokens?: number
  durationMs?: number
  costEstimated?: boolean
  heldReservation?: number
  retryOf?: string
  cause?: string
  attempt?: number
  attemptLimit?: number
  recoveryState?: string
  recoveryReason?: string
  applicationOutcome?: string
  applicationReason?: string
  unsupportedFallback?: unknown
  changes?: number
  undoOf?: string
  undoneAt?: string
  expiredAt?: string
  turn?: string
  thing?: string
  items?: string[]
  actionAt?: string
  agent?: string
  count?: number
  claim?: string
  claimVersion?: number
  class?: string
  item?: string
  action?: string
}

export interface ActivityEvent {
  at: string
  kind: ActivityEventKind
  eventId: string
  executionId: string | null
  rootExecutionId: string | null
  function: string
  outcome: string
  summary: string
  reason: string
  cost: number | null
  costKnown: boolean
  evidence: ActivityEventEvidence
}

export interface ActivityFunctionCost {
  function: string
  invocations: number
  returned: number
  settled: number
  unknownCost: number
  inputTokens: number
  outputTokens: number
}

export interface ActivityCost {
  settled: number
  invocations: number
  estimatedCostInvocations: number
  unknownCostInvocations: number
  heldReservations: number
  byFunction: ActivityFunctionCost[]
}

export interface ActivityDaySummary {
  failures: number
  deferrals: number
  overflows: number
}

export interface DayActivity {
  date: string
  timezone: string
  eventCount: number
  omittedEvents: number
  summary?: ActivityDaySummary
  kinds: Partial<Record<ActivityEventKind, number>>
  events: ActivityEvent[]
  cost: ActivityCost
  gaps: string[]
}

export interface UsageDurationStat {
  measuredCalls?: number
  median: number | null
  max: number | null
}

export interface UsagePurpose {
  purpose: string
  calls: number
  inputTokens: number
  outputTokens: number
  cost: number
  estimatedTokenCalls: number
  estimatedCostCalls: number
  durationMs: UsageDurationStat
}

export interface UsageTiming {
  kind: string
  tier: string
  executions: number
  prepareMs: UsageDurationStat
  answerMs: UsageDurationStat
  selfcheckMs: UsageDurationStat
  writebackMs: UsageDurationStat
  modelMs: UsageDurationStat
  totalMs: UsageDurationStat
  otherMs: UsageDurationStat
}

export interface UsageDay {
  date: string
  purposes: UsagePurpose[]
  timings: UsageTiming[]
}

export interface UsageSummary {
  days: UsageDay[]
  purposes?: UsagePurpose[]
  timings?: UsageTiming[]
}

export interface UsageCallRef {
  id: string
  version: number
  kind: 'claim' | 'source' | string
  text: string
  deleted: boolean
}

export interface UsageCall {
  id: string
  at: string
  purpose: string
  agentId: string | null
  model: string
  inputTokens: number
  outputTokens: number
  cost: number
  turnId: string | null
  runId: string | null
  jobId: string | null
  refs: UsageCallRef[]
  tier: string
  plan?: unknown
  inputEstimated: boolean
  outputEstimated: boolean
  costEstimated: boolean
  durationMs: number | null
  executionTiming?: {
    kind: string
    tier: string
    prepareMs: number
    answerMs: number
    selfcheckMs: number
    writebackMs: number
    modelMs: number
    totalMs: number
    otherMs: number
  }
}

export interface UsageCallsPage {
  items: UsageCall[]
  next?: string
}

export interface BackgroundStageHealth {
  stage: string
  hourlyBudget: number
  success: number
  deferred: number
  failure: number
  overflow: number
  deferredBy: Record<string, number>
  lastFailure: string
  withdrawn: {
    deadlines: number
    requirements: number
  }
  calls: number
}

export interface BackgroundOverflow {
  stage: string
  reason: string
  count: number
}

export interface BackgroundHealth {
  stages: BackgroundStageHealth[]
  overflow: BackgroundOverflow[]
}

export const activityKindOrder: ActivityEventKind[] = ['action', 'call', 'run', 'stage', 'decision', 'undo', 'usage']

export const activityKindLabel: Record<ActivityEventKind, string> = {
  call: '模型调用',
  usage: '历史用量',
  action: '执行动作',
  undo: '撤销记录',
  run: '副手任务',
  stage: '后台阶段',
  decision: '日期复查',
}

const functionLabels: Record<string, string> = observeLabels.functions
const reasonLabels: Record<string, string> = observeLabels.reasons
const reasonPrefixes: Record<string, string> = observeLabels.reasonPrefixes

export function functionLabel(name: string): string {
  if (!name) return '未命名环节'
  if (functionLabels[name]) return functionLabels[name]
  const base = name.split(':')[0]
  if (functionLabels[base]) return functionLabels[base]
  return name
}

const outcomeLabels: Record<string, { text: string; tone: Tone }> = {
  returned: { text: '已返回', tone: 'success' },
  recorded: { text: '已记录', tone: 'neutral' },
  executed: { text: '已执行', tone: 'success' },
  undone: { text: '已撤销', tone: 'neutral' },
  done: { text: '已完成', tone: 'success' },
  success: { text: '成功', tone: 'success' },
  deferred: { text: '延后等待', tone: 'warning' },
  waiting: { text: '等待中', tone: 'warning' },
  running: { text: '进行中', tone: 'accent' },
  prepared: { text: '已准备', tone: 'info' },
  started: { text: '调用中', tone: 'accent' },
  failed: { text: '失败', tone: 'danger' },
  failure: { text: '失败', tone: 'danger' },
  overflow: { text: '预算裁剪', tone: 'neutral' },
  unknown: { text: '结果未知', tone: 'warning' },
  kept: { text: '保持不变', tone: 'neutral' },
  keep: { text: '保持不变', tone: 'neutral' },
  drop: { text: '已关闭日期', tone: 'info' },
  task: { text: '转为待办', tone: 'success' },
  idea: { text: '转为想法', tone: 'accent' },
  closed: { text: '已关闭日期', tone: 'info' },
  converted: { text: '已转为事项', tone: 'success' },
  skipped: { text: '已跳过', tone: 'neutral' },
}

export function outcomeBadge(outcome: string): { text: string; tone: Tone } {
  return outcomeLabels[outcome] ?? { text: outcome || '未知', tone: 'neutral' }
}

export function reasonLabel(reason: string): string {
  if (!reason) return ''
  if (reasonLabels[reason]) return reasonLabels[reason]
  for (const [prefix, label] of Object.entries(reasonPrefixes)) {
    if (reason.startsWith(prefix)) {
      const rest = reason.slice(prefix.length)
      if (!rest) return label.replace(/：$/, '')
      return `${label}${reasonLabels[rest] ?? rest}`
    }
  }
  return reason
}

const accountingLabels: Record<string, string> = {
  settled: '已结算',
  not_reserved: '未预占（未发往模型）',
  reserved: '已预占',
  pending: '待结算',
  held: '保留预占（实际花费未知）',
  failed: '记账失败',
}

export function accountingLabel(state: string): string {
  return accountingLabels[state] ?? state
}

export function recoveryStateLabel(state: string): string {
  switch (state) {
    case 'exhausted':
      return '自动恢复耗尽（已暂停）'
    case 'budget_exhausted':
      return '恢复预算不足（已暂停）'
    case 'replaced':
      return '已重试替换'
    case 'active':
      return '正常'
    default:
      return state
  }
}

export function isTerminalRecovery(state?: string): boolean {
  return state === 'exhausted' || state === 'budget_exhausted'
}

const gapLabels: Record<string, string> = {
  'an action has no recorded link to the model call that proposed it; both carry the execution identity when the action came from a secretary turn':
    '执行动作本身未直接存模型调用 ID；来自秘书轮次的动作与调用共享同一个执行 ID，可用于关联查看。',
  'a background action and a date review decision carry no execution identity':
    '后台动作与日期复查决策当前未记录执行 ID，不会按相近时间强行猜测关联。',
  'usage written before the call journal has no outcome or reason':
    '统一调用流水上线前的历史用量缺少状态与原因字段。',
}

export function gapLabel(gap: string): string {
  return gapLabels[gap] ?? gap
}

/** Formats currency without rounding small sub-cent costs down to zero or losing fractional cents. */
export function formatCost(amount: number | null | undefined, known = true): string {
  if (!known || amount === null || amount === undefined || Number.isNaN(amount)) return '未知'
  if (amount === 0) return '¥0'
  const abs = Math.abs(amount)
  if (abs < 0.01) return `¥${amount.toFixed(4)}`
  if (abs < 0.1 && Math.abs(abs * 100 - Math.round(abs * 100)) > 1e-6) return `¥${amount.toFixed(4)}`
  return `¥${amount.toFixed(2)}`
}

export function formatTokens(n: number | null | undefined): string {
  if (!n) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`
  if (n >= 10_000) return `${(n / 1_000).toFixed(1)}k`
  return n.toLocaleString('zh-CN')
}

export function formatDurationMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || Number.isNaN(ms)) return '—'
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`
  return `${(ms / 60_000).toFixed(1)} min`
}

/** Shifts a YYYY-MM-DD civil date by `deltaDays` without local timezone drift. */
export function shiftCivilDate(date: string, deltaDays: number): string {
  const [y, m, d] = date.split('-').map(Number)
  const utc = Date.UTC(y, m - 1, d + deltaDays)
  return new Date(utc).toISOString().slice(0, 10)
}

/**
 * Genuine unresolved failure, unknown call outcome, or terminal recovery pause.
 * Normal budget truncation (overflow), superseded transient retries (replaced),
 * and compatibility fallbacks are intentionally NOT treated as anomalies.
 */
export function isEventProblem(e: ActivityEvent): boolean {
  if (e.kind === 'call' && e.evidence.recoveryState === 'replaced') return false
  if (e.kind === 'call' && isTerminalRecovery(e.evidence.recoveryState)) return true
  return e.outcome === 'failed' || e.outcome === 'failure' || e.outcome === 'unknown'
}

export function isEventDeferred(e: ActivityEvent): boolean {
  return e.outcome === 'deferred' || e.outcome === 'waiting'
}

export function isEventOverflow(e: ActivityEvent): boolean {
  return e.outcome === 'overflow'
}

/** Whether an action row is eligible to offer the undo button (server still enforces all invariants). */
export function canOfferUndo(e: ActivityEvent): boolean {
  if (e.kind !== 'action' || e.outcome !== 'executed') return false
  if (e.evidence.undoneAt || e.evidence.expiredAt || e.evidence.undoOf) return false
  const ageMs = Date.now() - Date.parse(e.at)
  return Number.isFinite(ageMs) && ageMs < 30 * 24 * 60 * 60 * 1000
}

export interface TraceCall {
  id: string
  executionId: string
  rootExecutionId: string
  causationId?: string
  retryOfId?: string
  attempt: number
  attemptLimit: number
  recoveryState: string
  recoveryReason?: string
  function: string
  stage: string
  provider: string
  model: string
  promptName?: string
  instructionHash?: string
  schemaName?: string
  schemaHash?: string
  contextBuilderVersion?: string
  requiredCapabilities?: string[]
  actualMode?: Record<string, unknown>
  inputManifest?: Record<string, unknown>
  outcome: string
  errorCode?: string
  accountingState: string
  usageId?: string
  cost?: number | null
  costKnown: boolean
  costEstimated?: boolean
  heldReservation?: number
  inputTokens?: number
  outputTokens?: number
  durationMs?: number
  createdAt: string
  startedAt?: string
  finishedAt?: string
}

export interface TraceEvent {
  id: string
  type: string
  stage: string
  objectId: string
  objectVersion: number
  triggerSource: 'job' | 'schedule_or_request' | string
  jobId: string
  rootId: string
  causeId?: string
  chainDepth: number
  occurredAt: string
  jobState?: string
  jobErrorCode?: string
  jobAttempts?: number
}

export interface TraceAction {
  id: string
  source: string
  summary: string
  outcome: 'executed' | 'undone' | string
  createdAt: string
  undoneAt?: string
  expiredAt?: string
  undoOf?: string
  turnId?: string
  thingId?: string
  changes: number
  items?: string[]
}

export interface TraceRun {
  id: string
  thingId: string
  agentId: string
  status: string
  kind?: string
  prompt?: string
  error?: string
  staleContext: boolean
  cost: number
  createdAt: string
  finishedAt?: string
}

export interface TraceStage {
  id: string
  at: string
  stage: string
  outcome: string
  reason?: string
  count: number
  jobId: string
}

export interface ExecutionTrace {
  anchorId: string
  calls: TraceCall[]
  events: TraceEvent[]
  actions: TraceAction[]
  runs: TraceRun[]
  stages: TraceStage[]
  gaps: string[]
}

export interface PausedJob {
  jobId: string
  stage: string
  baseStage: string
  recordId: string
  recordVersion: number
  state: 'failed' | 'blocked' | string
  errorCode?: string
  attempts: number
  chainDepth: number
  rootJobId?: string
  causeJobId?: string
  createdAt: string
  updatedAt: string
  recoveryState?: string
  recoveryReason?: string
  attempt?: number
  attemptLimit?: number
  requiresOwnerRecovery: boolean
}

export interface TopicDecision {
  topicId: string
  topicName?: string
  topicKind: string
  page: number
  decision: string
  reason?: string
  proposedName?: string
  projectId?: string
  actionId?: string
  createdProject: boolean
  undoneAt?: string
  linkedAt?: string
}

export interface DateDecision {
  claimId: string
  claimVersion: number
  class: 'unclear' | 'habit' | 'past' | string
  decision: 'keep' | 'drop' | 'task' | 'idea' | string
  reason?: string
  entryTitle?: string
  title?: string
  itemId?: string
  actionId?: string
  createdAt: string
}

export interface ActivityExplanations {
  pausedJobs: PausedJob[]
  topicDecisions: TopicDecision[]
  dateDecisions: DateDecision[]
}

const traceGapLabels: Record<string, string> = {
  'this action has no recorded execution link to a model call':
    '该动作未记录对应的模型调用执行 ID，不会按相近时间强行关联。',
  'input_manifest stores reference identities and coverage counts only; private prompt text stays outside the call journal':
    '输入清单仅记录引用 ID 与覆盖/省略统计，不保存私密提示词原文。',
}

export function traceGapLabel(gap: string): string {
  return traceGapLabels[gap] ?? gapLabel(gap)
}

export function topicDecisionBadge(decision: string): { text: string; tone: Tone } {
  switch (decision) {
    case 'skip':
      return { text: '跳过未建项目', tone: 'neutral' }
    case 'new':
    case 'create':
      return { text: '新建项目', tone: 'success' }
    case 'match':
    case 'attach':
      return { text: '归入已有项目', tone: 'info' }
    case 'uncertain':
      return { text: '拿不准（未操作）', tone: 'warning' }
    default:
      return { text: decision || '已记录', tone: 'neutral' }
  }
}

export function dateDecisionBadge(decision: string): { text: string; tone: Tone } {
  switch (decision) {
    case 'keep':
      return { text: '保持不变', tone: 'neutral' }
    case 'drop':
      return { text: '已关闭日期', tone: 'info' }
    case 'task':
      return { text: '转为待办', tone: 'success' }
    case 'idea':
      return { text: '转为想法', tone: 'accent' }
    default:
      return { text: decision || '已记录', tone: 'neutral' }
  }
}

export function dateClassLabel(cls: string): string {
  switch (cls) {
    case 'past':
      return '已过日期'
    case 'unclear':
      return '日期不明'
    case 'habit':
      return '习惯安排'
    default:
      return cls
  }
}

export function triggerSourceLabel(src: string): string {
  return src === 'job' ? '上游任务触发' : '调度或用户请求'
}

export function canOfferTraceActionUndo(a: TraceAction): boolean {
  if (a.outcome !== 'executed' || a.undoneAt || a.expiredAt || a.undoOf) return false
  const ageMs = Date.now() - Date.parse(a.createdAt)
  return Number.isFinite(ageMs) && ageMs < 30 * 24 * 60 * 60 * 1000
}

