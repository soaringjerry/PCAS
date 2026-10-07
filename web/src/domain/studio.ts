// What a studio (a project's page) reads on top of the workspace snapshot:
// the project's handover, shown as the status block, and a document's versions.
// The shapes are the phase 3 backend's (the skeleton PR freezes them);
// `store/studio.ts` is the only place that knows the paths.

import type { ID } from './types'

/**
 * What one sentence of the handover rests on, by id. For a memory `version` is
 * the memory's version; for `documentVersion` the id is the document's and
 * `version` its version number.
 */
export interface Evidence {
  kind: 'memory' | 'item' | 'documentVersion' | 'run'
  id: ID
  version?: number
}

export interface Sentence {
  text: string
  evidence: Evidence[]
}

/** A project's handover: written by the model, never edited here. */
export interface Handover {
  projectId: ID
  /** Absent while none has been written; the three parts are then empty. */
  writtenAt: string | null
  /** Something it was written from has changed; a new one is on its way. */
  stale: boolean
  conclusion: Sentence[]
  blockers: Sentence[]
  nextSteps: Sentence[]
}

/** Whether there is a handover to show at all. */
export function isWritten(handover: Handover): boolean {
  return Boolean(handover.writtenAt) || handover.conclusion.length + handover.blockers.length + handover.nextSteps.length > 0
}

export type VersionAuthor = 'user' | 'deputy' | 'secretary'

export interface DocVersion {
  documentId: ID
  version: number
  writtenAt: string
  author: VersionAuthor
  basedOn: number | null
  /** The assistant work that wrote it, when one did. */
  runId: ID | null
}

/** One changed paragraph, in the document's order. The side a paragraph is missing from is empty. */
export interface ParagraphChange {
  kind: 'add' | 'delete' | 'change'
  beforeIndex: number | null
  afterIndex: number | null
  before: string
  after: string
}

/** Only what changed between two versions; paragraphs that stayed are not listed. */
export interface DocDiff {
  documentId: ID
  fromVersion: number
  toVersion: number
  changes: ParagraphChange[]
}

export const authorText: Record<VersionAuthor, string> = { user: '你', deputy: '副手', secretary: '秘书' }

/** A run of saves shown as one line; `versions` is newest first and never empty. */
export interface VersionGroup {
  versions: DocVersion[]
}

/** One sitting of editing: saving on blur writes many versions in a row. */
export const FOLD_WINDOW = 15 * 60 * 1000

/**
 * Folds what one author wrote in a row into one line. A group spans at most
 * 15 minutes from its first version, and a piece of assistant work never
 * shares a line with another one. Returned newest first, like the list.
 */
export function foldVersions(versions: DocVersion[]): VersionGroup[] {
  const oldestFirst = [...versions].sort((a, b) => a.version - b.version)
  const groups: DocVersion[][] = []
  for (const v of oldestFirst) {
    const group = groups.at(-1)
    const first = group?.[0]
    if (group && first && first.author === v.author && (first.runId ?? '') === (v.runId ?? '') && new Date(v.writtenAt).getTime() - new Date(first.writtenAt).getTime() <= FOLD_WINDOW) group.push(v)
    else groups.push([v])
  }
  return groups.reverse().map((g) => ({ versions: g.reverse() }))
}

/** The two versions compared when nothing was picked: the latest against the one before it. The latest counts as touched last, so the next pick is compared with it. */
export function defaultPair(versions: DocVersion[]): number[] {
  return [...versions].sort((a, b) => a.version - b.version).slice(-2).map((v) => v.version)
}

/** Picking keeps the last two versions touched; touching a picked one lets it go. */
export function pick(picked: number[], version: number): number[] {
  if (picked.includes(version)) return picked.filter((v) => v !== version)
  return [...picked, version].slice(-2)
}
