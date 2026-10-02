import { useCallback, useEffect, useState } from 'react'

// Bringing a chat history in: what the file holds (nothing stored yet), then
// each import with how far it has got (docs/tasks/phase2/batch4/README.md §3, §6).

export interface ArchivePreview {
  name: string
  conversations: number
  messages: number
  fromUser: number
  earliest?: string
  latest?: string
  /** Messages of this file that are already here. */
  alreadyImported: number
  /** Messages beyond what one import takes; the newest are kept. */
  leftOut: number
  /** Messages the user deleted before and asked never to import again; in neither of the two above. `gaps` says so in words. */
  blocked: number
  gaps: string[]
}

export interface ImportBatch {
  id: string
  archiveId: string
  archiveVersion: number
  name: string
  state: 'importing' | 'paused' | 'done' | 'failed'
  total: number
  /** Kept as they were said; these can be asked about already. */
  stored: number
  /** Of those, how many have been gone through for memories. */
  organized: number
  leftOut: number
  earliest?: string
  latest?: string
  errorCode: string
  error: string
  createdAt: string
  updatedAt: string
}

/** A failure with the sentence to show. The server's own `message` is used as it is. */
export class ImportProblem extends Error {
  status: number
  code?: string
  constructor(status: number, body: { error?: unknown; message?: unknown }, doing: string) {
    const code = typeof body.error === 'string' ? body.error : undefined
    super(
      typeof body.message === 'string' && body.message
        ? body.message
        : status === 401
          ? '登录已过期，请重新登录后再试。'
          : status === 413
            ? '文件太大，服务器没有收下。'
            : status === 0
              ? `网络连接中断，${doing}没有完成。请检查网络后重试。`
              : `${doing}没有完成：服务器出错了（错误 ${status}${code ? `，${code}` : ''}）。请稍后重试；一直这样请查看服务日志。`,
    )
    this.status = status
    this.code = code
  }
}

/** A ChatGPT export is a zip; some people unpack it and pick the conversations file. Anything else is imported as before. */
export const isChatExport = (file: File) => /\.zip$/i.test(file.name) || file.type === 'application/zip' || file.name.toLowerCase() === 'conversations.json'

const parse = (text: string): Record<string, unknown> => {
  try {
    const value: unknown = JSON.parse(text)
    return value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  } catch {
    return {}
  }
}

/** Sends one file, reporting how much has gone up; `signal` gives up on it. */
export function upload<T>(path: string, file: File, doing: string, onProgress: (sent: number, total: number) => void, signal?: AbortSignal): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('POST', path)
    xhr.withCredentials = true
    xhr.upload.onprogress = (e) => { if (e.lengthComputable) onProgress(e.loaded, e.total) }
    xhr.onload = () => {
      const body = parse(xhr.responseText)
      if (xhr.status >= 200 && xhr.status < 300) resolve(body as T)
      else reject(new ImportProblem(xhr.status, body, doing))
    }
    xhr.onerror = () => reject(new ImportProblem(0, {}, doing))
    xhr.onabort = () => reject(new DOMException('aborted', 'AbortError'))
    signal?.addEventListener('abort', () => xhr.abort())
    const form = new FormData()
    form.append('file', file)
    xhr.send(form)
  })
}

async function call<T>(path: string, doing: string, body?: unknown): Promise<T> {
  let response: Response
  try {
    response = await fetch(path, { method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', headers: body === undefined ? undefined : { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })
  } catch {
    throw new ImportProblem(0, {}, doing)
  }
  const value = parse(await response.text())
  if (!response.ok) throw new ImportProblem(response.status, value, doing)
  return value as T
}

/** How soon to look again: often while messages are being stored, now and then otherwise. */
function pace(items: ImportBatch[]): number {
  if (items.some((b) => b.state === 'importing')) return 2500
  if (items.some((b) => b.state === 'done' && b.organized < b.total)) return 10000
  return 30000
}

export interface Imports {
  /** Newest first; undefined until the first answer. */
  items?: ImportBatch[]
  problem: string
  reload: () => void
  pause: (batch: ImportBatch) => Promise<void>
  resume: (batch: ImportBatch) => Promise<void>
  /** Deletes the import: every message it brought in and the memories drawn from them. */
  remove: (batch: ImportBatch) => Promise<void>
}

/** The imports and their progress, kept current for as long as the caller is on screen. */
export function useImports(): Imports {
  const [items, setItems] = useState<ImportBatch[]>()
  const [problem, setProblem] = useState('')
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let alive = true
    let timer: number | undefined
    const load = () => {
      call<{ items?: ImportBatch[] }>('/v1/connectors/imports', '读取导入进度')
        .then((v) => {
          if (!alive) return
          const next = v.items ?? []
          setItems(next)
          setProblem('')
          timer = window.setTimeout(load, pace(next))
        })
        .catch((e: unknown) => {
          if (!alive) return
          // A server from before imports had progress has no such list; that is nothing to report.
          if (e instanceof ImportProblem && e.status === 404) return setItems([])
          setProblem(e instanceof Error ? e.message : '读取导入进度没有完成。')
          timer = window.setTimeout(load, 15000)
        })
    }
    load()
    return () => {
      alive = false
      window.clearTimeout(timer)
    }
  }, [attempt])

  const reload = useCallback(() => setAttempt((n) => n + 1), [])
  const put = useCallback((batch: Partial<ImportBatch>) => {
    if (batch.id) setItems((prev) => prev?.map((b) => (b.id === batch.id ? { ...b, ...batch } : b)))
    reload()
  }, [reload])
  const pause = useCallback(async (batch: ImportBatch) => put(await call<Partial<ImportBatch>>(`/v1/connectors/imports/${encodeURIComponent(batch.id)}/pause`, '暂停', {})), [put])
  const resume = useCallback(async (batch: ImportBatch) => put(await call<Partial<ImportBatch>>(`/v1/connectors/imports/${encodeURIComponent(batch.id)}/resume`, '继续', {})), [put])
  const remove = useCallback(async (batch: ImportBatch) => {
    await call('/v1/memory/delete', '删除', { targets: [{ id: batch.archiveId, version: batch.archiveVersion, kind: 'source' }], include_sources: true })
    setItems((prev) => prev?.filter((b) => b.id !== batch.id))
    reload()
  }, [reload])

  return { items, problem, reload, pause, resume, remove }
}
