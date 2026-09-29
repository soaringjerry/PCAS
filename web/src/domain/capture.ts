import type { CandidateKind, MemoryKind } from './types'

// Stand-in for model-based extraction: a few keyword rules decide what a quick
// capture most likely is. The user always confirms the guess in the inbox.

const taskHints = ['要', '需要', '记得', '明天', '今天', '周', '截止', '之前', '回复', '提交', '发给', '跟进']
const ideaHints = ['想法', '或许', '也许', '可以试试', '要不要', '如果能', '会不会', 'idea', 'IDEA']
const preferenceHints = ['喜欢', '偏好', '习惯', '不喜欢', '倾向']
const decisionHints = ['决定', '定了', '不做', '先不', '改用', '就用']

export interface Guess {
  kind: CandidateKind
  memoryKind?: MemoryKind
  confidence: number
}

function hits(text: string, words: string[]): number {
  return words.filter((w) => text.includes(w)).length
}

export function guessCapture(text: string): Guess {
  const idea = hits(text, ideaHints)
  const task = hits(text, taskHints)
  if (idea > 0 && idea >= task) return { kind: 'idea', confidence: 0.6 + Math.min(idea, 3) * 0.1 }
  if (task > 0) return { kind: 'task', confidence: 0.55 + Math.min(task, 3) * 0.1 }
  if (hits(text, decisionHints) > 0) return { kind: 'memory', memoryKind: 'decision', confidence: 0.7 }
  if (hits(text, preferenceHints) > 0) return { kind: 'memory', memoryKind: 'preference', confidence: 0.7 }
  return { kind: 'memory', memoryKind: 'fact', confidence: 0.5 }
}
