import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { State } from '../domain/types'
import { StoreContext, type RunRequest, type UndoOutcome } from './context'
import { CONFLICT_RESENDS, sameTargets } from './conflict'
import type { Action } from './actions'
import { api, APIError, readWorkspace } from './api'
import { ToastContext, type ToastApi, type ToastOptions } from './toast'
import { CircleAlert, KeyRound, RotateCw } from 'lucide-react'
import { Toast, type ToastEntry } from '../components/Shell'
import { Spinner } from '../components/ui'

type Outcome = { ok: true } | { ok: false; error: unknown }

function Logo() {
  return <img className="gate-logo" src="/favicon.svg" alt="" width={44} height={44} />
}

export function StoreProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<State | null>(null)
  const [loading, setLoading] = useState(true)
  const [login, setLogin] = useState(false)
  const [token, setToken] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [signingIn, setSigningIn] = useState(false)
  const [toast, setToast] = useState<ToastEntry | null>(null)
  const [writes, setWrites] = useState(0)
  const closeToast = useCallback(() => setToast(null), [])
  const stateRef = useRef<State | null>(null)
  const etag = useRef<string | undefined>(undefined)
  const queue = useRef<Promise<unknown>>(Promise.resolve())
  const pending = useRef(0)
  const alive = useRef(true)
  const accept = useCallback((next: State) => { if (alive.current && (!stateRef.current || next.revision >= stateRef.current.revision)) { stateRef.current = next; setState(next); setLogin(false) } }, [])
  const fail = useCallback((e: unknown) => {
    if (!alive.current) return
    // A lapsed session shows the sign-in form; that is not an error to report.
    if (e instanceof APIError && e.status === 401) { setLogin(true); setState(null); stateRef.current = null; etag.current = undefined; setError(''); return }
    setError(e instanceof Error ? e.message : '网络连接中断，请重试')
  }, [])
  const refresh = useCallback(async () => {
    try {
      // Only a poll over a state already on screen may be answered with "unchanged".
      const read = await readWorkspace<State>(stateRef.current ? etag.current : undefined)
      if (read) { etag.current = read.etag; accept(read.state) }
    } catch (e) { fail(e) }
    finally { if (alive.current) setLoading(false) }
  }, [accept, fail])
  useEffect(() => {
    alive.current = true
    // Initial hydration only updates state after the network response.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void refresh()
    const timer = window.setInterval(() => { if (pending.current === 0 && stateRef.current && !document.hidden) void refresh() }, 3000)
    return () => { alive.current = false; window.clearInterval(timer) }
  }, [refresh])

  /**
   * Sends one command in order. `quiet` leaves reporting the failure to the caller.
   * The server turns a toggle or a bulk change down when the workspace moved on
   * since it was read. The background moves it on all the time, so the command
   * is sent again when what it acts on is the same as it was on screen.
   */
  const command = useCallback((action: object, requestId: string, quiet = false): Promise<Outcome> => {
    pending.current++; setSaving(true)
    const work = queue.current.then(async (): Promise<Outcome> => {
      let seen = stateRef.current
      if (!seen) return { ok: false, error: new Error('还没有连上 PCAS') }
      for (let resent = 0; ; resent++) {
        const body = { ...action, requestId, expectedRevision: seen.revision }
        try {
          let next: State
          try { next = await api<State>('/v1/workspace/commands', body) }
          catch (e) {
            // Retrying the identical request is safe because the server deduplicates it.
            if (e instanceof APIError) throw e
            next = await api<State>('/v1/workspace/commands', body)
          }
          accept(next); setError('')
          if (alive.current) setWrites((n) => n + 1)
          return { ok: true }
        } catch (e) {
          if (e instanceof APIError && e.status === 409) {
            let fresh: State | undefined
            try { fresh = await api<State>('/v1/workspace'); accept(fresh) } catch (failure) { fail(failure) }
            if (fresh && e.code === 'version_conflict' && resent < CONFLICT_RESENDS && fresh.revision !== seen.revision && sameTargets(action, seen, fresh)) { seen = fresh; continue }
          }
          if (!quiet || e instanceof APIError && e.status === 401) fail(e)
          return { ok: false, error: e }
        }
      }
    }).finally(() => { pending.current--; if (alive.current) setSaving(pending.current > 0) })
    queue.current = work.catch(() => undefined)
    return work
  }, [accept, fail])
  const dispatch = useCallback(async (action: Action) => (await command(action, crypto.randomUUID())).ok, [command])
  const showToast = useCallback((text: string, options?: ToastOptions) => setToast({ text, ...options, key: Date.now() }), [])
  const tryUndo = useCallback(async (actionId: string): Promise<UndoOutcome> => {
    const attempt = () => command({ type: 'undoAction', id: actionId }, crypto.randomUUID(), true)
    let outcome = await attempt()
    // A stale revision says nothing about the action itself; the refreshed state makes a second try valid.
    if (!outcome.ok && outcome.error instanceof APIError && outcome.error.code === 'version_conflict') outcome = await attempt()
    if (outcome.ok) return { ok: true }
    const e = outcome.error
    return { ok: false, error: e instanceof Error ? e.message : '没撤销成功，请重试。', code: e instanceof APIError ? e.code : undefined }
  }, [command])
  const undo = useCallback(async (actionId: string) => {
    const outcome = await tryUndo(actionId)
    if (!outcome.ok) showToast(outcome.error)
    return outcome.ok
  }, [tryUndo, showToast])
  const dispatchUndoable = useCallback(async (action: Action, label: string) => {
    // The command's requestId doubles as its action ID in the server's log.
    const requestId = crypto.randomUUID()
    const { ok } = await command(action, requestId)
    if (ok) showToast(label, { undo: async () => { if (await undo(requestId)) showToast('撤销了') } })
    return ok
  }, [command, showToast, undo])
  const runAgent = useCallback(async (request: RunRequest) => {
    const id = crypto.randomUUID()
    return (await command({ type: 'requestRun', id, ...request }, crypto.randomUUID())).ok ? id : undefined
  }, [command])
  const importText = useCallback(async (title: string, text: string) => {
    try {
      await api('/v1/memory/sources', { connector: 'file-import', external_id: crypto.randomUUID(), external_version: '1', title, text, media_type: 'text/plain' })
      await refresh(); setError(''); return true
    } catch (e) { fail(e); return false }
  }, [fail, refresh])
  const importAttachment = useCallback(async (file: File) => {
    const form = new FormData(); form.append('file', file); form.append('external_id', crypto.randomUUID())
    try {
      const response = await fetch('/v1/memory/attachments', { method: 'POST', body: form, credentials: 'same-origin' })
      if (!response.ok) throw new APIError(response.status, response.status === 401 ? '请先登录 PCAS' : '附件没有保存成功，请检查格式与大小。')
      await refresh(); setError(''); return true
    } catch (e) { fail(e); return false }
  }, [fail, refresh])
  const value = useMemo(() => state ? ({ state, writes, dispatch, dispatchUndoable, undo, tryUndo, applyState: accept, runAgent, importText, importAttachment, refresh }) : null, [state, writes, dispatch, dispatchUndoable, undo, tryUndo, accept, runAgent, importText, importAttachment, refresh])
  const toastApi = useMemo<ToastApi>(() => ({ show: showToast }), [showToast])

  if (loading) return <main className="gate"><div className="gate-card gate-loading" role="status"><Logo /><Spinner size={16} /><span className="muted">正在连接 PCAS…</span></div></main>
  if (login || state === null) return <main className="gate">
    <form className="gate-card" onSubmit={async (e) => {
      e.preventDefault(); setError(''); setSigningIn(true)
      try { await api('/v1/session', { token }); setToken(''); await refresh() }
      catch (error) { if (error instanceof APIError && error.status === 401) setError('访问令牌不正确'); else fail(error) }
      finally { if (alive.current) setSigningIn(false) }
    }}>
      <Logo />
      <h1>连接 PCAS</h1>
      <p className="muted">输入服务端配置的访问令牌。登录后由服务端会话保持连接。</p>
      <label className="input-icon">
        <KeyRound size={16} />
        <input className="input" type="password" autoComplete="current-password" autoFocus placeholder="访问令牌" aria-label="访问令牌" value={token} onChange={(e) => setToken(e.target.value)} />
      </label>
      {error && <p className="form-error" role="alert"><CircleAlert size={14} />{error}</p>}
      <button className="btn btn-primary btn-lg" type="submit" disabled={!token || signingIn}>{signingIn ? <Spinner /> : null}登录</button>
      <button className="btn btn-quiet" type="button" onClick={() => void refresh()}><RotateCw size={14} />重新连接</button>
    </form>
  </main>
  return <StoreContext.Provider value={value}>
    <ToastContext.Provider value={toastApi}>
      {error && <div className="connection-banner" role="alert"><CircleAlert size={16} /><span className="grow">{error}</span><button type="button" className="btn btn-quiet btn-sm" onClick={() => setError('')}>知道了</button></div>}
      {saving && <div className="save-status" role="status"><Spinner size={12} />正在保存</div>}
      {children}
      {toast && <Toast key={toast.key} toast={toast} onClose={closeToast} />}
    </ToastContext.Provider>
  </StoreContext.Provider>
}
