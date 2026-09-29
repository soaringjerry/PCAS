import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { State } from '../domain/types'
import { StoreContext, type RunRequest } from './context'
import type { Action } from './actions'
import { api, APIError } from './api'
import { CircleAlert, KeyRound, RotateCw } from 'lucide-react'
import { Spinner } from '../components/ui'

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
  const stateRef = useRef<State | null>(null)
  const queue = useRef<Promise<unknown>>(Promise.resolve())
  const pending = useRef(0)
  const alive = useRef(true)
  const accept = useCallback((next: State) => { if (alive.current && (!stateRef.current || next.revision >= stateRef.current.revision)) { stateRef.current = next; setState(next); setLogin(false) } }, [])
  const fail = useCallback((e: unknown) => {
    if (!alive.current) return
    // A lapsed session shows the sign-in form; that is not an error to report.
    if (e instanceof APIError && e.status === 401) { setLogin(true); setState(null); stateRef.current = null; setError(''); return }
    setError(e instanceof Error ? e.message : '网络连接中断，请重试')
  }, [])
  const refresh = useCallback(async () => {
    try { accept(await api<State>('/v1/workspace')) } catch (e) { fail(e) }
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

  const command = useCallback((action: object): Promise<boolean> => {
    pending.current++; setSaving(true)
    const work = queue.current.then(async () => {
      if (!stateRef.current) return false
      const body = { ...action, requestId: crypto.randomUUID(), expectedRevision: stateRef.current.revision }
      try {
        let next: State
        try { next = await api<State>('/v1/workspace/commands', body) }
        catch (e) {
          // Retrying the identical request is safe because the server deduplicates it.
          if (e instanceof APIError) throw e
          next = await api<State>('/v1/workspace/commands', body)
        }
        accept(next); setError(''); return true
      } catch (e) { fail(e); if (e instanceof APIError && e.status === 409) await refresh(); return false }
    }).finally(() => { pending.current--; if (alive.current) setSaving(pending.current > 0) })
    queue.current = work.catch(() => undefined)
    return work
  }, [accept, fail, refresh])
  const dispatch = useCallback((action: Action) => command(action), [command])
  const runAgent = useCallback(async (request: RunRequest) => {
    const id = crypto.randomUUID()
    return await command({ type: 'requestRun', id, ...request }) ? id : undefined
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
  const value = useMemo(() => state ? ({ state, dispatch, runAgent, importText, importAttachment, refresh }) : null, [state, dispatch, runAgent, importText, importAttachment, refresh])

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
    {error && <div className="connection-banner" role="alert"><CircleAlert size={16} /><span className="grow">{error}</span><button type="button" className="btn btn-quiet btn-sm" onClick={() => setError('')}>知道了</button></div>}
    {saving && <div className="save-status" role="status"><Spinner size={12} />正在保存</div>}
    {children}
  </StoreContext.Provider>
}
