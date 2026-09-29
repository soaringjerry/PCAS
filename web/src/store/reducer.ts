import { guessCapture } from '../domain/capture'
import { newId } from '../domain/ids'
import { ahead, nowIso } from '../domain/time'
import type {
  Agent,
  CandidateKind,
  Handoff,
  Idea,
  Memory,
  MemoryKind,
  Project,
  SampleState,
  Settings,
  SourceRef,
  State,
  Task,
  TaskStatus,
} from '../domain/types'
import { taskStatusLabel } from '../domain/labels'
import { createSeed } from './seed'

export type Action =
  | { type: 'capture'; text: string }
  | {
      type: 'acceptCandidate'
      id: string
      kind: CandidateKind
      text: string
      memoryKind?: MemoryKind
      projectId?: string
      due?: string
    }
  | { type: 'ignoreCandidate'; id: string }
  | { type: 'restoreCandidate'; id: string }
  | { type: 'mergeCandidate'; id: string; targetId: string }
  | { type: 'addTask'; title: string; projectId?: string }
  | { type: 'updateTask'; id: string; patch: Partial<Task>; summary: string }
  | { type: 'setTaskStatus'; id: string; status: TaskStatus }
  | { type: 'toggleTrigger'; taskId: string; triggerId: string }
  | { type: 'ideaContinue'; id: string }
  | { type: 'ideaPromote'; id: string }
  | { type: 'ideaSnooze'; id: string; days: number }
  | { type: 'ideaStopReminders'; id: string }
  | { type: 'ideaShelve'; id: string; reason: string; condition: string }
  | { type: 'ideaDrop'; id: string }
  | { type: 'ideaNote'; id: string; note: string }
  | { type: 'editMemory'; id: string; text: string; reason: string }
  | { type: 'confirmMemory'; id: string }
  | { type: 'deleteMemory'; id: string }
  | { type: 'setMemoryVisibility'; id: string; agentIds: string[] }
  | { type: 'createHandoff'; handoff: Handoff }
  | { type: 'updateHandoff'; id: string; patch: Partial<Handoff> }
  | { type: 'sendHandoff'; id: string }
  | { type: 'recordResult'; id: string; text: string }
  | { type: 'adoptResult'; id: string; userEdit?: string }
  | { type: 'setSampleState'; id: string; state: SampleState }
  | { type: 'updateAgent'; id: string; patch: Partial<Agent> }
  | { type: 'retryJob'; id: string }
  | { type: 'renameThing'; id: string; title: string }
  | { type: 'setNotes'; id: string; text: string }
  | { type: 'moveThing'; id: string; projectId?: string }
  | { type: 'addProject'; id: string; name: string }
  | { type: 'updateProject'; id: string; patch: Partial<Project> }
  | { type: 'addIdea'; id: string; title: string; projectId?: string }
  | { type: 'addCondition'; ideaId: string; description: string }
  | { type: 'removeCondition'; ideaId: string; conditionId: string }
  | { type: 'deferTask'; id: string; days: number }
  | { type: 'updateSettings'; patch: Partial<Settings> }
  | { type: 'demoImport'; step: 'start' | 'extract' | 'finish' }
  | { type: 'reset' }

const userCapture = (text: string): SourceRef => ({
  sourceId: 'src_capture',
  label: '快速记录',
  excerpt: text,
  at: nowIso(),
})

function mapById<T extends { id: string }>(items: T[], id: string, fn: (item: T) => T): T[] {
  return items.map((item) => (item.id === id ? fn(item) : item))
}

function newTask(title: string, fields: Partial<Task> = {}): Task {
  const at = nowIso()
  return {
    id: newId('t'),
    title,
    status: 'todo',
    dependsOn: [],
    triggers: [],
    sources: [],
    history: [{ at, by: 'user', summary: '创建' }],
    createdAt: at,
    updatedAt: at,
    ...fields,
  }
}

function newIdea(title: string, fields: Partial<Idea> = {}): Idea {
  const at = nowIso()
  return {
    id: newId('i'),
    title,
    body: '',
    status: 'active',
    conditions: [],
    remindersOn: true,
    evolution: [{ at, by: 'user', summary: '提出' }],
    sources: [],
    createdAt: at,
    updatedAt: at,
    ...fields,
  }
}

function newMemory(text: string, kind: MemoryKind, fields: Partial<Memory> = {}): Memory {
  const at = nowIso()
  return {
    id: newId('m'),
    kind,
    text,
    epistemic: 'confirmed',
    sources: [],
    versions: [{ at, by: 'user', text }],
    visibleTo: ['a_claude', 'a_chatgpt', 'a_codex'],
    exposure: 1,
    lastUsedAt: at,
    ...fields,
  }
}

function updateIdea(state: State, id: string, fn: (idea: Idea) => Idea): State {
  return { ...state, ideas: mapById(state.ideas, id, (idea) => ({ ...fn(idea), updatedAt: nowIso() })) }
}

/** A memory changed: anything built from it must be rebuilt (PRD §6). */
function invalidateDerived(state: State, memoryId: string, removed: boolean): State {
  return {
    ...state,
    handoffs: state.handoffs.map((h) =>
      h.memoryIds.includes(memoryId)
        ? {
            ...h,
            stale: h.status === 'draft' || h.status === 'sent' ? true : h.stale,
            memoryIds: removed ? h.memoryIds.filter((id) => id !== memoryId) : h.memoryIds,
          }
        : h,
    ),
    samples: state.samples.map((s) =>
      s.origin.memoryId === memoryId
        ? { ...s, stale: true, state: removed ? 'excluded' : s.state }
        : s,
    ),
  }
}

export function reducer(state: State, action: Action): State {
  const at = nowIso()
  switch (action.type) {
    case 'capture': {
      const guess = guessCapture(action.text)
      const id = newId('cd')
      const next: State = {
        ...state,
        candidates: [
          {
            id,
            kind: guess.kind,
            memoryKind: guess.memoryKind,
            text: action.text,
            confidence: guess.confidence,
            source: userCapture(action.text),
            state: 'pending',
            createdAt: at,
          },
          ...state.candidates,
        ],
      }
      // Only confident guesses skip the inbox, and only when the user allows it.
      if (state.settings.autoAccept && guess.confidence >= 0.8) {
        return reducer(next, { type: 'acceptCandidate', id, kind: guess.kind, text: action.text, memoryKind: guess.memoryKind })
      }
      return next
    }

    case 'acceptCandidate': {
      const candidate = state.candidates.find((c) => c.id === action.id)
      if (!candidate) return state
      const sources = [candidate.source]
      let next: State
      let createdId: string
      if (action.kind === 'task') {
        const task = newTask(action.text, {
          projectId: action.projectId,
          due: action.due,
          sources,
          history: [{ at, by: 'ai', summary: `从「${candidate.source.label}」提取，已由你采纳` }],
        })
        createdId = task.id
        next = { ...state, tasks: [task, ...state.tasks] }
      } else if (action.kind === 'idea') {
        const idea = newIdea(action.text, { projectId: action.projectId, sources })
        createdId = idea.id
        next = { ...state, ideas: [idea, ...state.ideas] }
      } else {
        const memory = newMemory(action.text, action.memoryKind ?? 'fact', {
          projectId: action.projectId,
          sources,
        })
        createdId = memory.id
        next = { ...state, memories: [memory, ...state.memories] }
      }
      return {
        ...next,
        candidates: mapById(next.candidates, action.id, (c) => ({ ...c, state: 'accepted', resolvedInto: createdId })),
      }
    }

    case 'ignoreCandidate':
      return { ...state, candidates: mapById(state.candidates, action.id, (c) => ({ ...c, state: 'ignored' })) }

    case 'restoreCandidate':
      return {
        ...state,
        candidates: mapById(state.candidates, action.id, (c) => ({ ...c, state: 'pending', resolvedInto: undefined })),
      }

    case 'mergeCandidate': {
      const candidate = state.candidates.find((c) => c.id === action.id)
      if (!candidate) return state
      const note = `合并：${candidate.text}`
      const next: State = {
        ...state,
        tasks: mapById(state.tasks, action.targetId, (t) => ({
          ...t,
          sources: [...t.sources, candidate.source],
          history: [...t.history, { at, by: 'user', summary: note }],
          updatedAt: at,
        })),
        ideas: mapById(state.ideas, action.targetId, (i) => ({
          ...i,
          sources: [...i.sources, candidate.source],
          evolution: [...i.evolution, { at, by: 'user', summary: note }],
          updatedAt: at,
        })),
        memories: mapById(state.memories, action.targetId, (m) => ({
          ...m,
          sources: [...m.sources, candidate.source],
        })),
      }
      return {
        ...next,
        candidates: mapById(next.candidates, action.id, (c) => ({ ...c, state: 'merged', resolvedInto: action.targetId })),
      }
    }

    case 'addTask':
      return { ...state, tasks: [newTask(action.title, { projectId: action.projectId }), ...state.tasks] }

    case 'updateTask':
      return {
        ...state,
        tasks: mapById(state.tasks, action.id, (t) => ({
          ...t,
          ...action.patch,
          history: [...t.history, { at, by: 'user', summary: action.summary }],
          updatedAt: at,
        })),
      }

    case 'setTaskStatus': {
      const next = {
        ...state,
        tasks: mapById(state.tasks, action.id, (t) => ({
          ...t,
          status: action.status,
          // A finished task no longer needs its reminders (PRD §4).
          triggers:
            action.status === 'done' || action.status === 'cancelled'
              ? t.triggers.map((tr) => ({ ...tr, active: false }))
              : t.triggers,
          history: [...t.history, { at, by: 'user' as const, summary: `状态改为${taskStatusLabel[action.status].text}` }],
          updatedAt: at,
        })),
      }
      return next
    }

    case 'toggleTrigger':
      return {
        ...state,
        tasks: mapById(state.tasks, action.taskId, (t) => ({
          ...t,
          triggers: t.triggers.map((tr) => (tr.id === action.triggerId ? { ...tr, active: !tr.active } : tr)),
        })),
      }

    case 'ideaContinue':
      return updateIdea(state, action.id, (i) => ({
        ...i,
        // The last wake is kept as context for handoffs; only status decides what shows as awakened.
        status: 'active',
        evolution: [...i.evolution, { at, by: 'user', summary: '决定继续推进' }],
      }))

    case 'ideaPromote': {
      const idea = state.ideas.find((i) => i.id === action.id)
      if (!idea) return state
      const task = newTask(idea.title, {
        projectId: idea.projectId,
        ideaId: idea.id,
        notes: idea.body,
        sources: idea.sources,
        history: [{ at, by: 'user', summary: '由 IDEA 转成任务' }],
      })
      const next = updateIdea(state, action.id, (i) => ({
        ...i,
        status: 'promoted',
        evolution: [...i.evolution, { at, by: 'user', summary: '转成任务' }],
      }))
      return {
        ...next,
        tasks: [task, ...next.tasks],
        projects: idea.projectId
          ? mapById(next.projects, idea.projectId, (p) => ({ ...p, status: 'active', updatedAt: at }))
          : next.projects,
      }
    }

    case 'ideaSnooze':
      return updateIdea(state, action.id, (i) => ({
        ...i,
        status: 'shelved',
        wake: i.wake ? { ...i.wake, snoozedUntil: ahead(action.days) } : undefined,
        conditions: [
          ...i.conditions,
          { id: newId('c'), kind: 'time', description: `${action.days} 天后再看`, dueAt: ahead(action.days), met: false },
        ],
        evolution: [...i.evolution, { at, by: 'user', summary: `延期 ${action.days} 天` }],
      }))

    case 'ideaStopReminders':
      return updateIdea(state, action.id, (i) => ({
        ...i,
        status: i.status === 'awakened' ? 'shelved' : i.status,
        wake: undefined,
        remindersOn: false,
        evolution: [...i.evolution, { at, by: 'user', summary: '停止提醒；想法保留，可随时找回' }],
      }))

    case 'ideaShelve':
      return updateIdea(state, action.id, (i) => ({
        ...i,
        status: 'shelved',
        wake: undefined,
        shelvedReason: action.reason,
        conditions: action.condition
          ? [...i.conditions, { id: newId('c'), kind: 'info', description: action.condition, met: false }]
          : i.conditions,
        evolution: [...i.evolution, { at, by: 'user', summary: `搁置：${action.reason}` }],
      }))

    case 'ideaDrop':
      return updateIdea(state, action.id, (i) => ({
        ...i,
        status: 'dropped',
        wake: undefined,
        remindersOn: false,
        evolution: [...i.evolution, { at, by: 'user', summary: '放弃' }],
      }))

    case 'ideaNote':
      return updateIdea(state, action.id, (i) => ({
        ...i,
        evolution: [...i.evolution, { at, by: 'user', summary: action.note }],
      }))

    case 'editMemory': {
      const next = {
        ...state,
        memories: mapById(state.memories, action.id, (m) => ({
          ...m,
          text: action.text,
          epistemic: 'confirmed' as const,
          versions: [...m.versions, { at, by: 'user' as const, text: action.text, reason: action.reason || undefined }],
          lastUsedAt: at,
        })),
      }
      return invalidateDerived(next, action.id, false)
    }

    case 'confirmMemory':
      return {
        ...state,
        memories: mapById(state.memories, action.id, (m) => ({
          ...m,
          epistemic: 'confirmed',
          versions: [...m.versions, { at, by: 'user', text: m.text, reason: '确认' }],
        })),
      }

    case 'deleteMemory':
      return invalidateDerived(
        { ...state, memories: state.memories.filter((m) => m.id !== action.id) },
        action.id,
        true,
      )

    case 'setMemoryVisibility':
      return { ...state, memories: mapById(state.memories, action.id, (m) => ({ ...m, visibleTo: action.agentIds })) }

    case 'createHandoff':
      return { ...state, handoffs: [action.handoff, ...state.handoffs] }

    case 'updateHandoff':
      return {
        ...state,
        handoffs: mapById(state.handoffs, action.id, (h) => ({ ...h, ...action.patch, updatedAt: at })),
      }

    case 'sendHandoff':
      return {
        ...state,
        handoffs: mapById(state.handoffs, action.id, (h) => ({ ...h, status: 'sent', stale: false, updatedAt: at })),
      }

    case 'recordResult':
      return {
        ...state,
        handoffs: mapById(state.handoffs, action.id, (h) => ({
          ...h,
          status: 'returned',
          result: { at, text: action.text },
          updatedAt: at,
        })),
      }

    case 'adoptResult': {
      const handoff = state.handoffs.find((h) => h.id === action.id)
      if (!handoff?.result) return state
      const finalText = action.userEdit?.trim() || handoff.result.text
      const edited = Boolean(action.userEdit?.trim()) && action.userEdit?.trim() !== handoff.result.text
      const summary = `采纳交接结果：${handoff.title}${edited ? '（有修改）' : ''}`
      const memory = newMemory(`「${handoff.title}」的结果已采纳：${finalText.split('\n').find((l) => l.trim())?.replace(/^[#\-\s]+/, '') ?? ''}`, 'fact', {
        projectId: handoff.projectId,
        sources: [{ sourceId: 'src_handoff', label: `交接：${handoff.title}`, at }],
      })
      return {
        ...state,
        handoffs: mapById(state.handoffs, action.id, (h) => ({
          ...h,
          status: 'adopted',
          result: h.result && { ...h.result, userEdit: edited ? finalText : undefined },
          updatedAt: at,
        })),
        projects: handoff.projectId
          ? mapById(state.projects, handoff.projectId, (p) => ({
              ...p,
              progress: `${p.progress}\n${summary}。`,
              updatedAt: at,
            }))
          : state.projects,
        tasks: handoff.taskId
          ? mapById(state.tasks, handoff.taskId, (t) => ({ ...t, history: [...t.history, { at, by: 'user', summary }] }))
          : state.tasks,
        ideas: handoff.ideaId
          ? mapById(state.ideas, handoff.ideaId, (i) => ({ ...i, evolution: [...i.evolution, { at, by: 'user', summary }] }))
          : state.ideas,
        memories: [memory, ...state.memories],
        samples: [
          {
            id: newId('s'),
            kind: edited ? 'correction' : 'adopted-result',
            prompt: handoff.sections.goal,
            response: finalText,
            origin: { label: `交接：${handoff.title}`, handoffId: handoff.id },
            version: 1,
            state: 'candidate',
            epistemic: 'confirmed',
            stale: false,
            createdAt: at,
          },
          ...state.samples,
        ],
      }
    }

    case 'setSampleState':
      return { ...state, samples: mapById(state.samples, action.id, (s) => ({ ...s, state: action.state })) }

    case 'updateAgent':
      return { ...state, agents: mapById(state.agents, action.id, (a) => ({ ...a, ...action.patch })) }

    case 'retryJob':
      return {
        ...state,
        jobs: mapById(state.jobs, action.id, (j) => ({
          ...j,
          status: 'queued',
          detail: '已重新排队，等待下一次运行',
          recovery: undefined,
          createdAt: at,
        })),
      }

    case 'renameThing':
      return {
        ...state,
        tasks: mapById(state.tasks, action.id, (t) => ({ ...t, title: action.title, updatedAt: at })),
        ideas: mapById(state.ideas, action.id, (i) => ({ ...i, title: action.title, updatedAt: at })),
        projects: mapById(state.projects, action.id, (p) => ({ ...p, name: action.title, updatedAt: at })),
      }

    case 'setNotes':
      return {
        ...state,
        tasks: mapById(state.tasks, action.id, (t) => ({ ...t, notes: action.text, updatedAt: at })),
        ideas: mapById(state.ideas, action.id, (i) => ({ ...i, body: action.text, updatedAt: at })),
      }

    case 'moveThing': {
      const name = state.projects.find((p) => p.id === action.projectId)?.name
      const summary = name ? `归到「${name}」` : '不再属于项目'
      return {
        ...state,
        tasks: mapById(state.tasks, action.id, (t) => ({
          ...t,
          projectId: action.projectId,
          history: [...t.history, { at, by: 'user', summary }],
          updatedAt: at,
        })),
        ideas: mapById(state.ideas, action.id, (i) => ({
          ...i,
          projectId: action.projectId,
          evolution: [...i.evolution, { at, by: 'user', summary }],
          updatedAt: at,
        })),
      }
    }

    case 'addProject':
      return {
        ...state,
        projects: [
          { id: action.id, name: action.name, goal: '', status: 'active', progress: '', nextSteps: [], updatedAt: at },
          ...state.projects,
        ],
      }

    case 'updateProject':
      return {
        ...state,
        projects: mapById(state.projects, action.id, (p) => ({ ...p, ...action.patch, updatedAt: at })),
      }

    case 'addIdea':
      return { ...state, ideas: [newIdea(action.title, { id: action.id, projectId: action.projectId }), ...state.ideas] }

    case 'addCondition':
      return updateIdea(state, action.ideaId, (i) => ({
        ...i,
        conditions: [...i.conditions, { id: newId('c'), kind: 'info', description: action.description, met: false }],
        evolution: [...i.evolution, { at, by: 'user', summary: `加了一个再看的条件：${action.description}` }],
      }))

    case 'removeCondition':
      return updateIdea(state, action.ideaId, (i) => ({
        ...i,
        conditions: i.conditions.filter((c) => c.id !== action.conditionId),
      }))

    case 'deferTask':
      return {
        ...state,
        tasks: mapById(state.tasks, action.id, (t) => {
          const base = t.due ? new Date(t.due) : new Date()
          const due = new Date(Math.max(base.getTime(), Date.now()) + action.days * 86400000)
          due.setHours(base.getHours() || 18, base.getMinutes(), 0, 0)
          return {
            ...t,
            due: due.toISOString(),
            history: [...t.history, { at, by: 'user' as const, summary: `推迟 ${action.days} 天` }],
            updatedAt: at,
          }
        }),
      }

    case 'updateSettings':
      return { ...state, settings: { ...state.settings, ...action.patch } }

    case 'demoImport':
      return demoImport(state, action.step)

    case 'reset':
      return createSeed()
  }
}

const DEMO_JOB = 'j_demo'
const DEMO_FILE = '本地 Qwen3-4B 推理成本测算.pdf'

/** PRD §7: new cost information arrives and wakes the shelved idea. */
function demoImport(state: State, step: 'start' | 'extract' | 'finish'): State {
  const at = nowIso()
  if (step === 'start') {
    return {
      ...state,
      demo: { costReportImported: true },
      jobs: [
        {
          id: DEMO_JOB,
          title: `导入资料：${DEMO_FILE}`,
          trigger: 'event',
          status: 'running',
          progress: 0.25,
          detail: '读取文件，保留原文',
          createdAt: at,
        },
        ...state.jobs.filter((j) => j.id !== DEMO_JOB),
      ],
    }
  }
  if (step === 'extract') {
    return {
      ...state,
      jobs: mapById(state.jobs, DEMO_JOB, (j) => ({
        ...j,
        progress: 0.7,
        detail: '提取出 1 条事实，正在检查搁置 IDEA 的重新考虑条件',
      })),
    }
  }

  const source: SourceRef = {
    sourceId: 'src_email',
    label: `邮件附件：${DEMO_FILE}`,
    excerpt: '在 RTX 4090 上运行 Qwen3-4B，单次记忆提取平均 ¥0.004（含电费折算）。',
    at,
  }
  const memory = newMemory('本地 Qwen3-4B 做一次记忆提取约 ¥0.004，约为云端的 1/7。', 'fact', {
    id: 'm_localcost',
    epistemic: 'inferred',
    projectId: 'p_model',
    sources: [source],
    versions: [{ at, by: 'ai', text: '本地 Qwen3-4B 做一次记忆提取约 ¥0.004，约为云端的 1/7。' }],
  })
  const wake = state.settings.wakeIdeas
  const next = updateIdea(state, 'i_small_model', (i) => ({
    ...i,
    status: wake && i.status === 'shelved' ? 'awakened' : i.status,
    conditions: i.conditions.map((c) => (c.id === 'c_cost' ? { ...c, met: true, metAt: at, metBy: source } : c)),
    wake: wake
      ? {
          at,
          reason: '新导入的成本测算显示：本地提取一次约 ¥0.004，是云端（¥0.03）的约 1/7，满足你设定的“低于 1/5”这一条件。',
          conditionId: 'c_cost',
        }
      : i.wake,
    evolution: [
      ...i.evolution,
      {
        at,
        by: 'system',
        summary: wake ? `条件满足，重新唤醒（来自「${DEMO_FILE}」）` : `条件满足（来自「${DEMO_FILE}」），自动唤醒已关闭`,
      },
    ],
  }))
  return {
    ...next,
    memories: [memory, ...next.memories.filter((m) => m.id !== memory.id)],
    sources: mapById(next.sources, 'src_email', (s) => ({ ...s, itemCount: s.itemCount + 1, lastSyncAt: at })),
    jobs: mapById(next.jobs, DEMO_JOB, (j) => ({
      ...j,
      status: 'done',
      progress: 1,
      detail: '新增 1 条事实；唤醒 1 个 IDEA',
    })),
  }
}
