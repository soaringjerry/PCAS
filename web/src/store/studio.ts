import { useCallback, useEffect, useRef, useState } from 'react'
import type { DocDiff, DocVersion, Handover, ProjectFile, ProjectTimeline } from '../domain/studio'
import { api, APIError } from './api'

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

/** The files kept with the project this item belongs to, newest first. */
export async function readFiles(itemId: string): Promise<ProjectFile[]> {
  return (await api<{ items?: ProjectFile[] }>(`/v1/workspace/items/${enc(itemId)}/files`)).items ?? []
}

const uploadProblems: Record<string, string> = {
  unauthorized: '请先登录 PCAS',
  reimport_blocked: '这份文件删过，已经不再接收',
  invalid_input: '这种文件收不了',
  not_found: '这件事已经不在了',
}

/** Sends one file. The same content sent twice is kept once; `alreadyExists` says it was there. */
export async function uploadFile(itemId: string, file: File): Promise<{ file: ProjectFile; alreadyExists: boolean }> {
  const form = new FormData()
  form.append('file', file, file.name)
  const response = await fetch(`/v1/workspace/items/${enc(itemId)}/files`, { method: 'POST', credentials: 'same-origin', body: form })
  const value = await response.json().catch(() => ({}))
  if (!response.ok) throw new APIError(response.status, response.status === 413 ? '超过 20 MB' : (uploadProblems[value.error] ?? '服务暂时不可用，没有存上'), typeof value.error === 'string' ? value.error : undefined)
  return { file: value.file as ProjectFile, alreadyExists: Boolean(value.alreadyExists) }
}

/** Removes a file and everything read out of it. Only ever called after the user confirmed. */
export async function deleteFile(itemId: string, sourceId: string): Promise<void> {
  await api(`/v1/workspace/items/${enc(itemId)}/files/${enc(sourceId)}?confirmed=true`, undefined, 'DELETE')
}

export async function readTimeline(projectId: string): Promise<ProjectTimeline> {
  const t = await api<Partial<ProjectTimeline>>(`/v1/workspace/projects/${enc(projectId)}/timeline`)
  return { projectId, items: t.items ?? [], withoutDue: t.withoutDue ?? [], dailyHours: t.dailyHours ?? 0 }
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
