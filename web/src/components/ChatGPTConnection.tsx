import { formatTimestamp } from '../domain/time'
import { useCallback, useEffect, useState } from 'react'
import { api } from '../store/api'
import { CircleAlert, Copy, ExternalLink, RotateCw } from 'lucide-react'
import { Button, Progress, Sheet, Tag } from './ui'
import { Select } from './controls'
import { useStore } from '../store/context'

interface Account { account: null | { type: string; email?: string; planType?: string } }
interface Login { verificationUrl: string; userCode: string; loginId: string }
interface Limits { rateLimits: { primary?: { usedPercent: number; resetsAt: number }; secondary?: { usedPercent: number; resetsAt: number } } }

function CodexConnection() {
  const { state } = useStore()
  const [enabled, setEnabled] = useState(false)
  const [account, setAccount] = useState<Account['account']>(null)
  const [login, setLogin] = useState<Login | null>(null)
  const [limits, setLimits] = useState<Limits | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const refresh = useCallback(async () => {
    try {
      const config = await api<{ chatgptEnabled: boolean }>('/v1/models')
      setEnabled(config.chatgptEnabled)
      if (!config.chatgptEnabled) return
      const result = await api<Account>('/v1/chatgpt/account'); setAccount(result.account)
      if (result.account?.type === 'chatgpt') {
        setLogin(null)
        try { setLimits(await api<Limits>('/v1/chatgpt/limits')) } catch { setLimits(null) }
      }
    } catch (e) { setError(e instanceof Error ? e.message : '连接失败') }
  }, [])
  // Hydrate from the external account service; refresh updates after awaiting I/O.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => {
    if (!login) return
    const timer = window.setInterval(() => void refresh(), 4000)
    return () => window.clearInterval(timer)
  }, [login, refresh])
  const connected = account?.type === 'chatgpt'
  return <section className="section"><h2 className="section-title">Codex App Server</h2><Sheet pad>
    <div className="stack-sm">
      <div className="spread">
        <div className="row-nowrap">
          <span className={`status-dot${connected ? ' on' : ''}`} aria-hidden />
          <span className="ink">{connected ? `已连接 ${account.email ?? 'ChatGPT'}` : '未连接'}</span>
          {connected && account.planType && <Tag tone="info">{account.planType}</Tag>}
        </div>
        <div className="row-nowrap">
          <Button variant="quiet" size="sm" icon={<RotateCw size={13} />} onClick={() => void refresh()}>刷新</Button>
          <Button size="sm" variant={connected ? 'default' : 'primary'} disabled={!enabled || busy} onClick={async () => {
            setBusy(true); setError('')
            try {
              if (connected) { await api('/v1/chatgpt/logout', {}); setAccount(null); setLimits(null) }
              else setLogin(await api<Login>('/v1/chatgpt/login', {}))
            } catch (e) { setError(e instanceof Error ? e.message : '连接失败') }
            finally { setBusy(false) }
          }}>{busy ? '连接中…' : connected ? '退出 ChatGPT' : login ? '重新获取验证码' : '登录 ChatGPT'}</Button>
        </div>
      </div>
      {!connected && <p className="small muted">使用 ChatGPT 订阅额度运行副手。通过官方 Codex 登录。</p>}
      {!enabled && <p className="callout">服务端尚未启用订阅入口。按部署文档配置 Codex 后，此处即可登录。</p>}
      {limits?.rateLimits.primary && <div className="stack-sm" style={{ gap: 6 }}>
        <div className="spread small"><span className="muted">当前额度窗口</span><span>已用 {limits.rateLimits.primary.usedPercent}%</span></div>
        <Progress value={limits.rateLimits.primary.usedPercent / 100} />
        <span className="tiny muted">预计 {formatTimestamp(new Date(limits.rateLimits.primary.resetsAt * 1000).toISOString(), state.settings.timezone ?? 'UTC')} 重置</span>
      </div>}
      {limits?.rateLimits.secondary && <div className="stack-sm" style={{ gap: 6 }}>
        <div className="spread small"><span className="muted">较长额度窗口</span><span>已用 {limits.rateLimits.secondary.usedPercent}%</span></div>
        <Progress value={limits.rateLimits.secondary.usedPercent / 100} />
      </div>}
      {connected && !limits && <p className="small muted">暂时无法读取剩余额度。</p>}
      {login && <div className="device-login" role="status">
        <p>打开 <a href={login.verificationUrl} target="_blank" rel="noreferrer">官方验证页面 <ExternalLink size={12} /></a>，输入验证码：</p>
        <div className="device-code-row">
          <strong className="device-code">{login.userCode}</strong>
          <Button size="sm" variant="quiet" icon={<Copy size={13} />} onClick={() => void navigator.clipboard?.writeText(login.userCode)}>复制</Button>
        </div>
        <p className="tiny muted">验证完成后这里会自动更新。若无法使用设备登录，请先在 ChatGPT 安全设置中启用。</p>
      </div>}
      {error && <p className="form-error" role="alert"><CircleAlert size={14} />{error}</p>}
      <p className="tiny muted">订阅额度与 API 费用分别计算，具体可用模型和限额以账户为准。语义索引使用单独配置的向量服务。</p>
    </div>
  </Sheet></section>
}

interface DirectAccount { client_id: string; email?: string; connected: boolean; plan_enabled: boolean; verified: boolean; paused: boolean; model?: string }
interface DirectStatus { accounts: DirectAccount[]; active_client_id: string; pending: boolean; error?: string; default_ready: boolean }
interface DirectModel { slug: string; display_name: string }

function DirectConnection() {
  const { refresh: refreshWorkspace } = useStore()
  const [enabled, setEnabled] = useState(false)
  const [status, setStatus] = useState<DirectStatus | null>(null)
  const [models, setModels] = useState<DirectModel[]>([])
  const [authorization, setAuthorization] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const active = status?.accounts.find(a => a.client_id === status.active_client_id)
  const refresh = useCallback(async () => {
    try {
      const config = await api<{ chatgptDirectEnabled: boolean }>('/v1/models')
      setEnabled(config.chatgptDirectEnabled)
      if (!config.chatgptDirectEnabled) return
      const next = await api<DirectStatus>('/v1/chatgpt/direct/account')
      setStatus(next)
      const selected = next.accounts.find(a => a.client_id === next.active_client_id)
      if (selected?.plan_enabled && !selected.paused) {
        setModels((await api<{ models: DirectModel[] }>('/v1/chatgpt/direct/models')).models)
      } else setModels([])
      if (!next.pending) setAuthorization('')
    } catch (e) { setError(e instanceof Error ? e.message : '连接失败') }
  }, [])
  // Account state is hydrated only after awaiting the external service.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void refresh() }, [refresh])
  useEffect(() => {
    if (!status?.pending) return
    const timer = window.setInterval(() => { void refresh(); void refreshWorkspace() }, 3000)
    return () => window.clearInterval(timer)
  }, [status?.pending, refresh, refreshWorkspace])
  async function action(work: () => Promise<void>) {
    setBusy(true); setError(''); setNotice('')
    try { await work(); await refresh(); await refreshWorkspace() }
    catch (e) { setError(e instanceof Error ? e.message : '操作未完成') }
    finally { setBusy(false) }
  }
  function login(client = '', consent = false) {
    const popup = window.open('about:blank', '_blank')
    if (popup) popup.opener = null
    void action(async () => {
      try {
        const result = await api<{ authorization_url: string }>('/v1/chatgpt/direct/login', { client_id: client, consent })
        const url = new URL(result.authorization_url)
        if (url.origin !== 'https://auth.openai.com') throw new Error('授权地址无效')
        setAuthorization(result.authorization_url)
        if (popup) popup.location.href = result.authorization_url
      } catch (e) { popup?.close(); throw e }
    })
  }
  if (!enabled) return null
  return <section className="section"><h2 className="section-title">ChatGPT 订阅</h2><Sheet pad><div className="stack-sm">
    <div className="spread">
      <div className="row-nowrap">
        <span className={`status-dot${active?.connected ? ' on' : ''}`} aria-hidden />
        <span className="ink">{active?.connected ? `已连接 ${active.email ?? active.client_id}` : '未连接'}</span>
        {status?.default_ready && <Tag tone="info">默认套餐入口</Tag>}
      </div>
      <Button size="sm" variant="quiet" icon={<RotateCw size={13} />} onClick={() => void refresh()}>刷新连接</Button>
    </div>
    <p className="small muted">通过 OpenAI 官方授权直接运行副手和资料整理，消耗你的 ChatGPT 套餐及账户允许的额度。</p>
    <a href="https://chatgpt.com/settings/usage" target="_blank" rel="noreferrer">管理 ChatGPT 用量与应用权限 <ExternalLink size={12} /></a>
    {status && status.accounts.length > 0 && <Select label="连接的 ChatGPT 账户" value={status.active_client_id}
      options={status.accounts.map(a => ({ value: a.client_id, label: `${a.email ?? 'ChatGPT'} · ${a.client_id}`, hint: a.connected ? '已连接' : '已退出' }))}
      onChange={client => void action(async () => { await api('/v1/chatgpt/direct/select', { client_id: client }) })} disabled={busy} />}
    {models.length > 0 && active && <Select label="ChatGPT 可用模型" value={active.model || models[0].slug}
      options={models.map(m => ({ value: m.slug, label: m.display_name || m.slug }))}
      onChange={model => void action(async () => { await api('/v1/chatgpt/direct/select', { client_id: active.client_id, model }) })} disabled={busy} />}
    {active?.connected && !active.plan_enabled && <p className="callout">账户已连接，尚未允许 PCAS 使用套餐。请开启套餐授权，或选择 API Key 通道。</p>}
    {active?.paused && <p className="callout">此账户的套餐请求已暂停。请先在 ChatGPT 用量设置中检查限制，再恢复请求。</p>}
    {active?.connected && !active.verified && <p className="small muted">完整授权流程尚未验收，暂未设为默认。你可以在副手中选择「ChatGPT · 套餐授权」。</p>}
    <div className="row-nowrap" style={{ flexWrap: 'wrap' }}>
      <Button variant="primary" disabled={!enabled || busy || status?.pending} onClick={() => login(active?.client_id)}>Continue with ChatGPT</Button>
      {!!status?.accounts.length && <Button disabled={!enabled || busy || status?.pending} onClick={() => login()}>添加账户或工作区</Button>}
      {active?.connected && !active.plan_enabled && <Button disabled={busy || status?.pending} onClick={() => login(active.client_id, true)}>开启套餐授权</Button>}
      {active?.paused && <Button disabled={busy} onClick={() => void action(async () => { await api('/v1/chatgpt/direct/select', { client_id: active.client_id, resume: true }) })}>恢复套餐请求</Button>}
      {active?.connected && <Button disabled={busy} onClick={() => void action(async () => {
        const result = await api<{ remote_revocation_confirmed: boolean }>('/v1/chatgpt/direct/logout', { client_id: active.client_id })
        if (!result.remote_revocation_confirmed) setNotice('已清除本地凭据，远端撤销未确认。请到 ChatGPT 设置中断开 PCAS。')
      })}>退出并撤销授权</Button>}
    </div>
    {authorization && status?.pending && <p role="status">请在本机浏览器完成授权。<a href={authorization} target="_blank" rel="noreferrer">打开授权页面</a></p>}
    <p className="tiny muted">本机 Docker 可直接登录；个人远程 Docker／VM 请在浏览器所在本机授权，再通过 SSH 转移受保护凭据，按部署文档操作。</p>
    {(error || status?.error) && <p className="form-error" role="alert"><CircleAlert size={14} />{error || status?.error}</p>}
    {notice && <p className="callout" role="status">{notice}</p>}
  </div></Sheet></section>
}

export function ChatGPTConnection() {
  return <><CodexConnection /><DirectConnection /></>
}
