import type { Agent, CandidateKind, Doc, MemoryKind, Project, SampleState, Settings, Task, TaskStatus } from '../domain/types'

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
  | { type: 'deleteMemory'; id: string; includeSources?: boolean }
  | { type: 'pinMemory'; id: string }
  | { type: 'setMemoryVisibility'; id: string; agentIds: string[] }
  | { type: 'pasteRunResult'; id: string; output: string }
  | { type: 'adoptRun'; id: string; as: 'doc' | 'subtasks' | 'progress'; text: string }
  | { type: 'discardRun'; id: string }
  | { type: 'createDoc'; doc: Doc }
  | { type: 'updateDoc'; id: string; patch: Partial<Pick<Doc, 'title' | 'body'>> }
  | { type: 'deleteDoc'; id: string }
  | { type: 'addCheck'; taskId: string; text: string }
  | { type: 'toggleCheck'; taskId: string; itemId: string }
  | { type: 'removeCheck'; taskId: string; itemId: string }
  | { type: 'toggleContextMemory'; thingId: string; memoryId: string }
  | { type: 'bulkStatus'; ids: string[]; status: TaskStatus }
  | { type: 'bulkDefer'; ids: string[]; days: number }
  | { type: 'bulkMove'; ids: string[]; projectId?: string }
  | { type: 'bulkAccept'; ids: string[] }
  | { type: 'bulkIgnore'; ids: string[] }
  | { type: 'setSampleState'; id: string; state: SampleState }
  | { type: 'updateAgent'; id: string; patch: Partial<Agent> }
  | { type: 'retryJob'; id: string }
  | { type: 'renameThing'; id: string; title: string }
  | { type: 'setNotes'; id: string; text: string }
  | { type: 'moveThing'; id: string; projectId?: string }
  | { type: 'addProject'; id: string; name: string }
  | { type: 'updateProject'; id: string; patch: Partial<Project> }
  | { type: 'addIdea'; id: string; title: string; projectId?: string }
  | { type: 'addCondition'; ideaId: string; description: string; due?: string }
  | { type: 'removeCondition'; ideaId: string; conditionId: string }
  | { type: 'deferTask'; id: string; days: number }
  | { type: 'updateSettings'; patch: Partial<Settings> }
