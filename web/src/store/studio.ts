import { useCallback, useEffect, useRef, useState } from 'react'
import type { DocDiff, DocVersion, Handover } from '../domain/studio'
import { api } from './api'

// The studio's reads (phase 3 contract §3). Every path is in this file.

const enc = encodeURIComponent

/** The project's handover. One that has not been written yet comes back with empty parts and no time. */
export async function readHandover(projectId: string): Promise<Handover> {
  const h = await api<Partial<Handover>>(`/v1/workspace/projects/${enc(projectId)}/handover`)
  return { projectId, writtenAt: h.writtenAt ?? null, stale: Boolean(h.stale), conclusion: h.conclusion ?? [], blockers: h.blockers ?? [], nextSteps: h.nextSteps ?? [] }
}

/** Every version of a document, newest first. */
export async function readVersions(docId: string): Promise<DocVersion[]> {
  const { items } = await api<{ items?: DocVersion[] }>(`/v1/workspace/documents/${enc(docId)}/versions`)
  return [...(items ?? [])].sort((a, b) => b.version - a.version)
}

export async function readVersionBody(docId: string, version: number): Promise<string> {
  return (await api<{ body?: string }>(`/v1/workspace/documents/${enc(docId)}/versions/${version}`)).body ?? ''
}

export async function readDiff(docId: string, from: number, to: number): Promise<DocDiff> {
  const diff = await api<Partial<DocDiff>>(`/v1/workspace/documents/${enc(docId)}/diff?from=${from}&to=${to}`)
  return { documentId: docId, fromVersion: from, toVersion: to, changes: diff.changes ?? [] }
}

export type Read<T> = { phase: 'loading' } | { phase: 'ready'; value: T } | { phase: 'failed'; problem: string }

/**
 * Reads once per `key` and again whenever `revision` moves. A re-read keeps
 * what is on screen until the new answer arrives, and a failed re-read keeps
 * it too: only a first read that fails has nothing to show.
 */
export function useRead<T>(key: string, revision: number, read: () => Promise<T>): [Read<T>, () => void] {
  const [state, setState] = useState<{ key: string; read: Read<T> }>({ key, read: { phase: 'loading' } })
  const [again, setAgain] = useState(0)
  const reader = useRef(read)
  useEffect(() => {
    reader.current = read
  })
  useEffect(() => {
    let alive = true
    reader.current().then(
      (value) => alive && setState({ key, read: { phase: 'ready', value } }),
      (e: unknown) =>
        alive && setState((s) => (s.key === key && s.read.phase === 'ready' ? s : { key, read: { phase: 'failed', problem: e instanceof Error ? e.message : '网络连接中断' } })),
    )
    return () => {
      alive = false
    }
  }, [key, revision, again])
  const retry = useCallback(() => setAgain((n) => n + 1), [])
  return [state.key === key ? state.read : { phase: 'loading' }, retry]
}
