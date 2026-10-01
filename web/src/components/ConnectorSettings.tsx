import { useStore } from '../store/context'
import { formatTimestamp } from '../domain/time'
import { Select, Stepper, FileDrop } from './controls'
import { useEffect, useState, type ReactNode } from 'react'
import { api } from '../store/api'
import { Button, Fold, Tag } from './ui'

type Connection = { id: string; name: string; kind: 'webhook' | 'poll' | 'folder'; url?: string; token_env?: string; enabled: boolean; version: number; interval_seconds: number; status: string; imported: number; error?: string; gaps: string[]; folder?: string; last_sync?: string }
const statusText: Record<string, string> = { idle: '等待同步', syncing: '同步中', queued: '待同步', error: '同步失败' }

/** Bringing material in: a chat archive by hand, the rows passed in, then other apps that keep sending. */
export function ConnectorSettings({ children }: { children?: ReactNode }) {
  const { state } = useStore()
  const [connections, setConnections] = useState<Connection[]>([])
  const [name, setName] = useState('')
  const [kind, setKind] = useState<Connection['kind']>('webhook')
  const [url, setURL] = useState('')
  const [tokenEnv, setTokenEnv] = useState('')
  const [seconds, setSeconds] = useState(300)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const [credential, setCredential] = useState<{ id: string; token: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const refresh = () => api<Connection[]>('/v1/connectors').then(setConnections)
  useEffect(() => { let alive = true; const load = () => { void api<Connection[]>('/v1/connectors').then(v => { if (alive) setConnections(v) }).catch((e: Error) => { if (alive) setError(e.message) }) }; load(); const timer = setInterval(load, 5000); return () => { alive = false; clearInterval(timer) } }, [])
  const run = async (work: () => Promise<void>) => { setBusy(true); setError(''); setMessage(''); try { await work(); await refresh() } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
  const toggle = (c: Connection) => run(async () => { await api('/v1/connectors', { id: c.id, expected_version: c.version, name: c.name, kind: c.kind, url: c.url ?? '', token_env: c.token_env ?? '', interval_seconds: c.interval_seconds, enabled: !c.enabled }) })
  const failing = connections.filter(c => c.error).length
  return <>
    <div className="setting set-block">
      <label className="field"><span className="ink">导入聊天记录</span><FileDrop file={null} accept=".zip,.json,.jsonl,.txt,.md" hint="ChatGPT、Claude 或通用聊天归档 · ZIP、JSON、JSONL、TXT、Markdown · 20 MB" onClear={() => {}} onFile={file => { if (busy) return; void run(async () => { const form = new FormData(); form.append('file', file); const response = await fetch('/v1/connectors/archive', { method: 'POST', credentials: 'same-origin', body: form }); if (!response.ok) throw new Error('归档导入失败。支持 ZIP、JSON、JSONL、文本，文件最多 20 MB。'); const result = await response.json() as { gaps: string[] }; setMessage(result.gaps.join('；') || '已收到，原文已保存。') }) }} /><span className="tiny muted">原文先原样存下再整理；归档里的图片和附件会标出没解析的部分。</span></label>
      {error && <p role="alert" className="form-error">{error}</p>}{message && <p role="status" className="small">{message}</p>}
    </div>
    {children}
    {connections.map(c => <div className="setting set-block" key={c.id}>
      <div className="spread"><strong>{c.name}</strong><Tag tone={c.error ? 'danger' : 'neutral'}>{c.enabled ? (statusText[c.status] ?? c.status) : '已暂停'}</Tag></div>
      <p className="small muted">已导入 {c.imported} 条{c.last_sync ? ` · 最近同步 ${formatTimestamp(c.last_sync, state.settings.timezone ?? 'UTC')}` : ''}</p>
      {c.folder && <p className="small">收件目录：<code style={{ overflowWrap: 'anywhere' }}>PCAS_INBOX_DIR/{c.folder}</code></p>}
      {c.error && <p className="small" role="alert">{c.error === 'access_denied' ? '数据源凭据不可用，请检查服务器配置。' : c.error === 'source_version_conflict' ? '同一来源版本出现不同内容，请在数据源更新版本号。' : '同步未完成，请检查来源格式、连接状态和服务器配置。'}</p>}
      {c.gaps.map((g, i) => <p className="tiny muted" key={i}>{g}</p>)}
      <div className="row"><Button size="sm" disabled={busy} onClick={() => void toggle(c)}>{c.enabled ? '暂停接入' : '恢复接入'}</Button>{c.kind !== 'webhook' && c.enabled && <Button size="sm" disabled={busy} onClick={() => void run(async () => { await api(`/v1/connectors/${c.id}/sync`, { expected_version: c.version }) })}>立即同步</Button>}</div>
    </div>)}
    <Fold title="接入其他应用" summary={connections.length ? `已有 ${connections.length} 个${failing ? ` · ${failing} 个同步失败` : ''}` : '让笔记、聊天等应用持续把资料送进来'} tone={failing ? 'danger' : undefined}>
      <div className="stack-sm">
        <h3 className="fold-name">资料接入</h3>
        <p className="small muted">需要对方应用或服务器配合，一般由会配置的人来做。保存只是建好通道，不代表已经同步成功。</p>
        <form className="stack-sm" onSubmit={e => { e.preventDefault(); void run(async () => { const result = await api<{ connection: Connection; webhook_token?: string }>('/v1/connectors', { name, kind, url: kind === 'poll' ? url : '', token_env: kind === 'poll' ? tokenEnv : '', interval_seconds: seconds, enabled: true, expected_version: 0 }); if (result.webhook_token) setCredential({ id: result.connection.id, token: result.webhook_token }); setName(''); setMessage('接入已保存') }) }}>
          <label className="field"><span className="field-label">接入名称</span><input className="input" value={name} onChange={e => setName(e.target.value)} maxLength={200} required placeholder="例如：我的笔记" /></label>
          <label className="field"><span className="field-label">接入方式</span><Select label="接入方式" value={kind} onChange={setKind} options={[{ value: 'webhook', label: '应用推送（Webhook）', hint: '对方应用主动把资料发过来' }, { value: 'poll', label: '定时读取 HTTP 数据源', hint: '服务器定时去一个地址取' }, { value: 'folder', label: '服务器收件文件夹', hint: '服务器定时扫描自己的一个目录' }]} /></label>
          {kind === 'poll' && <><label className="field"><span className="field-label">数据源地址</span><input className="input" type="url" value={url} onChange={e => setURL(e.target.value)} required /></label><label className="field"><span className="field-label">凭据环境变量名（可选）</span><input className="input" value={tokenEnv} onChange={e => setTokenEnv(e.target.value)} placeholder="PCAS_CONNECTOR_NOTES_TOKEN" /><span className="tiny muted">密钥保存在服务器环境中，不在这里填写。</span></label></>}
          {kind !== 'webhook' && <label className="field"><span className="field-label">同步间隔（秒）</span><Stepper label="同步间隔" min={15} max={86400} value={seconds} onChange={setSeconds} /></label>}
          <div><Button type="submit" disabled={busy || !name.trim()}>添加接入</Button></div>
        </form>
        {credential && <div className="stack-sm callout"><p>此密钥只显示一次，用于向这个接入发送资料。请保存到发送应用。</p><code style={{ overflowWrap: 'anywhere' }}>{window.location.origin}/v1/connectors/{credential.id}/records</code><code style={{ overflowWrap: 'anywhere' }}>Bearer {credential.token}</code><Button size="sm" onClick={() => setCredential(null)}>已保存，隐藏密钥</Button></div>}
      </div>
    </Fold>
  </>
}
