import { useCallback, useEffect, useState } from 'react'
import { api } from '../store/api'
import { CircleAlert, Copy, ExternalLink, RotateCw } from 'lucide-react'
import { Button, Progress, Sheet, Tag } from './ui'

interface Account { account: null | { type: string; email?: string; planType?: string } }
interface Login { verificationUrl: string; userCode: string; loginId: string }
interface Limits { rateLimits: { primary?: { usedPercent: number; resetsAt: number }; secondary?: { usedPercent: number; resetsAt: number } } }

export function ChatGPTConnection() {
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
  return <section className="section"><h2 className="section-title">ChatGPT 订阅</h2><Sheet pad>
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
        <span className="tiny muted">预计 {new Date(limits.rateLimits.primary.resetsAt * 1000).toLocaleString()} 重置</span>
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
