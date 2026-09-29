import type {
  CandidateKind,
  Epistemic,
  RunStatus,
  IdeaStatus,
  JobStatus,
  MemoryKind,
  ProjectStatus,
  SampleState,
  SourceStatus,
  TaskStatus,
} from './types'

export type Tone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info'

interface Label {
  text: string
  tone: Tone
}

export const epistemicLabel: Record<Epistemic, Label> = {
  confirmed: { text: '已确认', tone: 'success' },
  inferred: { text: 'AI 推测', tone: 'warning' },
  planned: { text: '计划', tone: 'info' },
}

export const taskStatusLabel: Record<TaskStatus, Label> = {
  doing: { text: '正在做', tone: 'accent' },
  todo: { text: '待办', tone: 'neutral' },
  waiting: { text: '等待', tone: 'warning' },
  done: { text: '完成', tone: 'success' },
  cancelled: { text: '取消', tone: 'neutral' },
}

export const taskStatusOrder: TaskStatus[] = ['doing', 'todo', 'waiting', 'done', 'cancelled']

export const ideaStatusLabel: Record<IdeaStatus, Label> = {
  awakened: { text: '已唤醒', tone: 'accent' },
  active: { text: '进行中', tone: 'info' },
  shelved: { text: '搁置', tone: 'neutral' },
  promoted: { text: '已转任务', tone: 'success' },
  dropped: { text: '放弃', tone: 'neutral' },
}

export const projectStatusLabel: Record<ProjectStatus, Label> = {
  active: { text: '进行中', tone: 'accent' },
  paused: { text: '暂停', tone: 'neutral' },
  done: { text: '完成', tone: 'success' },
}

export const memoryKindLabel: Record<MemoryKind, string> = {
  fact: '事实',
  preference: '偏好',
  decision: '决定',
  intention: '意向',
  plan: '计划',
}

export const candidateKindLabel: Record<CandidateKind, string> = {
  unknown: '待分类',
  task: '待办',
  idea: 'IDEA',
  memory: '记忆',
}

export const sourceStatusLabel: Record<SourceStatus, Label> = {
  syncing: { text: '处理中', tone: 'info' },
  connected: { text: '已连接', tone: 'success' },
  manual: { text: '手动导入', tone: 'info' },
  unverified: { text: '待验证', tone: 'warning' },
  failed: { text: '出错', tone: 'danger' },
}

export const jobStatusLabel: Record<JobStatus, Label> = {
  queued: { text: '排队中', tone: 'neutral' },
  running: { text: '处理中', tone: 'accent' },
  waiting: { text: '等待', tone: 'warning' },
  failed: { text: '失败', tone: 'danger' },
  done: { text: '完成', tone: 'success' },
}

export const runStatusLabel: Record<RunStatus, Label> = {
  running: { text: '进行中', tone: 'accent' },
  waiting: { text: '等你贴回结果', tone: 'warning' },
  done: { text: '完成', tone: 'success' },
  failed: { text: '失败', tone: 'danger' },
}

export const sampleStateLabel: Record<SampleState, Label> = {
  candidate: { text: '候选', tone: 'warning' },
  included: { text: '纳入', tone: 'success' },
  excluded: { text: '排除', tone: 'neutral' },
}

export const triggerLabel = {
  event: '事件驱动',
  time: '时间驱动',
  manual: '手动',
} as const
