import { dayOffset, isOverdue } from './time'
import type { Candidate, Handoff, Idea, Job, Memory, State, Task } from './types'

// "需要你看": the few things that want a decision from the user right now.
// Everything else stays out of the way until it matters.

export type Attention =
  | { kind: 'wake'; key: string; idea: Idea }
  | { kind: 'due'; key: string; task: Task }
  | { kind: 'result'; key: string; handoff: Handoff }
  | { kind: 'candidate'; key: string; candidate: Candidate }
  | { kind: 'confirm'; key: string; memory: Memory }
  | { kind: 'job'; key: string; job: Job }

const WEEK = 7 * 24 * 60 * 60 * 1000

export function attentionFor(state: State): Attention[] {
  const items: Attention[] = []

  for (const idea of state.ideas) {
    if (idea.status === 'awakened') items.push({ kind: 'wake', key: `w-${idea.id}`, idea })
  }

  for (const task of state.tasks) {
    const open = task.status === 'todo' || task.status === 'doing'
    if (open && task.due && (isOverdue(task.due) || dayOffset(task.due) <= 0)) {
      items.push({ kind: 'due', key: `d-${task.id}`, task })
    }
  }

  for (const handoff of state.handoffs) {
    if (handoff.status === 'returned') items.push({ kind: 'result', key: `r-${handoff.id}`, handoff })
  }

  for (const candidate of state.candidates) {
    if (candidate.state === 'pending') items.push({ kind: 'candidate', key: `c-${candidate.id}`, candidate })
  }

  // Recent guesses the AI made on its own, worth a quick yes or no.
  for (const memory of state.memories) {
    const recent = Date.now() - new Date(memory.versions[0].at).getTime() < WEEK
    if (memory.epistemic === 'inferred' && recent) items.push({ kind: 'confirm', key: `m-${memory.id}`, memory })
  }

  for (const job of state.jobs) {
    if (job.status === 'failed') items.push({ kind: 'job', key: `j-${job.id}`, job })
  }

  return items
}

/** Things that want a decision now, as opposed to quick yes/no confirmations. */
export function isDecision(item: Attention): boolean {
  return item.kind !== 'candidate' && item.kind !== 'confirm'
}
