import type { Tone } from './labels'

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

export interface DayActivity {
  date: string
  timezone: string
  eventCount: number
  omittedEvents: number
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

const functionLabels: Record<string, string> = {
  secretary: '秘书回答',
  deputy: '副手工作',
  desk: '秘书动作',
  command: '手动命令',
  worker: '后台执行',
  background_extraction: '后台抽取',
  background_topic: '自动主题项目',
  extraction: '记忆抽取',
  organize: '记忆整理',
  compare: '陈述对比',
  entity_candidates: '别名扫描',
  entity_compare: '实体合并',
  handover: '全局交接',
  project_handover: '项目交接',
  date_tidy: '日期复查',
  effort: '工时估算',
  topic_project: '自动主题项目',
  embed: '向量索引',
  embedding: '向量索引',
  query_embed: '检索向量化',
  vision: '模型读图',
  transcription: '语音转写',
  reader: '记忆细读',
  selfcheck: '输出自查',
  revise: '文档修订',
  'source.parse': '附件解析',
  'source.chunk': '原文切分',
  'source.extract': '记忆抽取',
  'source.tokenize': '原文分词索引',
  'source.embed': '原文向量索引',
  'memory.index': '记忆检索索引',
  'memory.embed': '记忆向量索引',
  'memory.organize': '记忆整理',
  'memory.compare': '陈述对比',
  'memory.entity_candidates': '别名扫描',
  'memory.entity_compare': '实体合并',
  'memory.summary': '情景摘要',
  'memory.card': '旧卡片槽位清理',
  'memory.handover': '全局交接',
  'memory.date_tidy': '日期复查',
  'memory.project_handover': '项目交接',
  'memory.effort': '工时估算',
  'memory.topic_project': '自动主题项目',
}

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
  overflow: { text: '超出上限', tone: 'danger' },
  unknown: { text: '结果未知', tone: 'warning' },
  kept: { text: '保持不变', tone: 'neutral' },
  closed: { text: '已关闭日期', tone: 'info' },
  converted: { text: '已转为事项', tone: 'success' },
  skipped: { text: '已跳过', tone: 'neutral' },
}

export function outcomeBadge(outcome: string): { text: string; tone: Tone } {
  return outcomeLabels[outcome] ?? { text: outcome || '未知', tone: 'neutral' }
}

const reasonLabels: Record<string, string> = {
  no_call_record: '网关上线前的历史用量，无调用流水',
  budget_deferred: '今日额度已满，延后至次日继续',
  topic_project_hourly_limit: '达到自动主题项目每小时限额，延后等待',
  organize_hourly_limit: '达到记忆整理每小时限额，延后等待',
  compare_hourly_limit: '达到陈述对比每小时限额，延后等待',
  entity_candidates_hourly_limit: '达到别名扫描每小时限额，延后等待',
  entity_compare_hourly_limit: '达到实体合并每小时限额，延后等待',
  date_tidy_hourly_limit: '达到日期复查每小时限额，延后等待',
  effort_hourly_limit: '达到工时估算每小时限额，延后等待',
  project_handover_hourly_limit: '达到项目交接每小时限额，延后等待',
  'memory.handover_hourly_limit': '达到全局交接每小时限额，延后等待',
  provider_outcome_unknown: '模型响应中断，结果未知',
  provider_recovery_exhausted: '自动恢复次数已用尽，已暂停',
  provider_recovery_budget_exhausted: '恢复预算不足，已暂停',
  chain_limit: '超过因果链深度上限（200），已停止自动继续',
  deputy_execution_interrupted: '副手运行中途中断',
  model_call_failed: '模型调用失败',
  background_write_busy: '等待其他写入提交，延后至下次调度',
}

export function reasonLabel(reason: string): string {
  if (!reason) return ''
  return reasonLabels[reason] ?? reason
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

/** Whether an event represents a failure, unknown state, or overflow worth highlighting. */
export function isEventProblem(e: ActivityEvent): boolean {
  return (
    e.outcome === 'failed' ||
    e.outcome === 'failure' ||
    e.outcome === 'overflow' ||
    e.outcome === 'unknown' ||
    Boolean(e.evidence.recoveryState) ||
    Boolean(e.evidence.unsupportedFallback)
  )
}

export function isEventDeferred(e: ActivityEvent): boolean {
  return e.outcome === 'deferred' || e.outcome === 'waiting'
}

/** Whether an action row is eligible to offer the undo button (server still enforces all invariants). */
export function canOfferUndo(e: ActivityEvent): boolean {
  if (e.kind !== 'action' || e.outcome !== 'executed') return false
  if (e.evidence.undoneAt || e.evidence.expiredAt || e.evidence.undoOf) return false
  const ageMs = Date.now() - Date.parse(e.at)
  return Number.isFinite(ageMs) && ageMs < 30 * 24 * 60 * 60 * 1000
}
