import { useCallback, useEffect, useState } from 'react'
import { api } from '../store/api'
import { useStore } from '../store/context'
import { Button, Fold, Switch, Tag } from './ui'

interface Connection {
  base_url: string
  model: string
  input_cny_per_million: number
  output_cny_per_million: number
  default: boolean
  key_configured: boolean
}
interface Decision { key_configured: boolean; saved: boolean; working?: boolean }
interface Configuration { editable: boolean; text: Connection; embedding: Connection; decision: Decision }

function ConnectionForm({ role, value, editable, onSaved }: {
  role: 'text' | 'embedding'; value: Connection; editable: boolean; onSaved: () => Promise<void>
}) {
  const { refresh: refreshWorkspace } = useStore()
  const [draft, setDraft] = useState(value)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const embedding = role === 'embedding'
  const label = embedding ? '向量' : '文本 API'
  async function save() {
    setBusy(true); setError(''); setNotice('')
    try {
      const saved = await api<Connection>(`/v1/models/openai/${role}`, {
        base_url: draft.base_url, model: draft.model, api_key: key,
        input_cny_per_million: draft.input_cny_per_million,
        output_cny_per_million: draft.output_cny_per_million, default: draft.default,
      })
      setDraft(saved); setKey('')
      setNotice(embedding ? '向量接入已保存，可补建现有资料的向量。' : 'API 接入已保存，可在副手中使用。')
      await onSaved(); await refreshWorkspace()
    } catch (e) { setError(e instanceof Error ? e.message : '保存失败') }
    finally { setBusy(false) }
  }
  async function rebuild() {
    setBusy(true); setError(''); setNotice('')
    try {
      const result = await api<{ queued: number }>('/v1/models/embeddings/rebuild', {})
      setNotice(`已排队 ${result.queued} 项资料，后台按每日预算补建缺失向量。`)
    } catch (e) { setError(e instanceof Error ? e.message : '重建失败') }
    finally { setBusy(false) }
  }
  return <form className="stack-sm conn-form" onSubmit={e => { e.preventDefault(); void save() }}>
      <div className="spread"><h4>{embedding ? '向量模型：按意思搜索资料用' : '对话模型：秘书和副手用'}</h4>
        <Tag tone={draft.key_configured ? 'info' : undefined}>{draft.key_configured ? '已填密钥' : '还没填密钥'}</Tag>
      </div>
      <p className="small muted">{embedding ? 'OpenAI 兼容的向量接口。没有它，资料仍能按原文搜到；换模型后需补建现有资料的向量。' : 'OpenAI 兼容接口（Chat Completions），填服务的 /v1 地址。保存时不会试连，能不能用要到第一次调用才知道。'}</p>
      <label className="stack-sm small">{label} Base URL
        <input className="input" aria-label={`${label} Base URL`} value={draft.base_url} onChange={e => setDraft({ ...draft, base_url: e.target.value })} type="url" required disabled={!editable || busy} />
      </label>
      <label className="stack-sm small">{label} API Key
        <input className="input" aria-label={`${label} API Key`} value={key} onChange={e => setKey(e.target.value)} type="password" autoComplete="new-password" placeholder={draft.key_configured ? '留空保留当前密钥' : '填写 API Key'} required={!draft.key_configured} disabled={!editable || busy} />
      </label>
      <label className="stack-sm small">{label}模型
        <input className="input" aria-label={`${label}模型`} value={draft.model} onChange={e => setDraft({ ...draft, model: e.target.value })} required disabled={!editable || busy} />
      </label>
      <div className="row">
        <label className="stack-sm small">输入预算单价（¥ / 百万 token）
          <input className="input" aria-label={`${label}输入单价`} type="number" min="0.000001" step="any" required value={draft.input_cny_per_million} onChange={e => setDraft({ ...draft, input_cny_per_million: Number(e.target.value) })} disabled={!editable || busy} />
        </label>
        {!embedding && <label className="stack-sm small">输出预算单价（¥ / 百万 token）
          <input className="input" aria-label="文本 API输出单价" type="number" min="0" step="any" required value={draft.output_cny_per_million} onChange={e => setDraft({ ...draft, output_cny_per_million: Number(e.target.value) })} disabled={!editable || busy} />
        </label>}
      </div>
      {!embedding && <div className="spread small"><span>默认用它来做副手的活和读新资料<span className="muted aside">点「保存」后才生效</span></span><Switch label="API 作为默认入口" checked={draft.default} onChange={v => setDraft({ ...draft, default: v })} /></div>}
      <p className="tiny muted">单价只用来估算每日花费，请按供应商实际价格填。密钥只存在服务端；换地址要重新填密钥。</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      {notice && <p className="callout" role="status">{notice}</p>}
      {!editable && <p className="callout">服务端需配置 PCAS_MODEL_SETTINGS_FILE 后才能在此保存接入信息。</p>}
      <div className="row">
        <Button type="submit" variant="primary" size="sm" disabled={!editable || busy}>{busy ? '处理中…' : `保存${label}接入`}</Button>
        {embedding && <Button type="button" size="sm" disabled={!draft.key_configured || busy} onClick={() => void rebuild()}>补建现有资料向量</Button>}
      </div>
    </form>
}

/** The key for the old /v1/desk/route endpoint. The secretary and Telegram no longer call it. */
function DecisionForm({ value, editable, onSaved }: { value: Decision; editable: boolean; onSaved: () => Promise<void> }) {
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  async function save() {
    setBusy(true); setError(''); setNotice('')
    try {
      const saved = await api<Decision>('/v1/models/decision', { api_key: key })
      setKey('')
      setNotice(saved.working ? '已保存，试调用成功。' : '已保存，但试调用没成功：检查密钥是否正确、是否已开通 Jev。')
      await onSaved()
    } catch (e) { setError(e instanceof Error ? e.message : '保存失败') }
    finally { setBusy(false) }
  }
  async function remove() {
    setBusy(true); setError(''); setNotice('')
    try { await api('/v1/models/decision', undefined, 'DELETE'); setNotice('已移除这里保存的密钥。服务器环境里若另配了密钥，旧接口仍会用它。'); await onSaved() }
    catch (e) { setError(e instanceof Error ? e.message : '移除失败') }
    finally { setBusy(false) }
  }
  return <form className="stack-sm conn-form" onSubmit={e => { e.preventDefault(); void save() }}>
      <p className="small muted">旧的分流接口用 TypeSafe 的 Jev 判断一句话是要问、要记，还是交给副手。现在的秘书和 Telegram 都不经过它，只有仍在调用旧接口的外部程序会用到。</p>
      <label className="stack-sm small">Jev API Key
        <input className="input" aria-label="Jev API Key" value={key} onChange={e => setKey(e.target.value)} type="password" autoComplete="new-password" placeholder={value.saved ? '填写新密钥以替换' : '在 console.typesafe.ai 创建'} required disabled={!editable || busy} />
      </label>
      <p className="tiny muted">外部程序调用旧接口时，那句原文会发给 TypeSafe。密钥只存在服务端。</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      {notice && <p className="callout" role="status">{notice}</p>}
      <div className="row">
        <Button type="submit" size="sm" disabled={!editable || busy || !key.trim()}>{busy ? '处理中…' : '保存并试调用'}</Button>
        {value.saved && <Button type="button" size="sm" disabled={busy} onClick={() => void remove()}>移除密钥</Button>}
      </div>
    </form>
}

export function OpenAIConnection() {
  const [configuration, setConfiguration] = useState<Configuration | null>(null)
  const [error, setError] = useState('')
  const refresh = useCallback(async () => {
    try { setConfiguration(await api<Configuration>('/v1/models/openai')); setError('') }
    catch (e) { setError(e instanceof Error ? e.message : '读取 API 配置失败') }
  }, [])
  // Hydrate connection metadata after the server request completes.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void refresh() }, [refresh])
  const text = configuration?.text
  // A saved key is not a tested connection, so the summary only says what has been filled in.
  const summary = error ? '读取失败' : !configuration ? '正在读取…'
    : text?.key_configured ? `已填密钥 · ${text.model}${configuration.embedding.key_configured ? '' : ' · 向量还没填'}` : '还没填密钥'
  const decision = configuration?.decision
  return <Fold title="按量计费接口" summary={summary} tone={error ? 'danger' : undefined}>
    <div className="stack-sm">
      <h3 className="fold-name">API 与向量接入</h3>
      {error && <p className="form-error" role="alert">{error}</p>}
      {configuration && <>
        <ConnectionForm role="text" value={configuration.text} editable={configuration.editable} onSaved={refresh} />
        <ConnectionForm role="embedding" value={configuration.embedding} editable={configuration.editable} onSaved={refresh} />
        <Fold title="旧版分流接口的密钥" summary={`现在的秘书不使用 · ${decision?.saved ? '已保存密钥' : decision?.key_configured ? '使用服务端环境变量' : '未配置'}`}>
          <DecisionForm value={configuration.decision} editable={configuration.editable} onSaved={refresh} />
        </Fold>
      </>}
    </div>
  </Fold>
}
