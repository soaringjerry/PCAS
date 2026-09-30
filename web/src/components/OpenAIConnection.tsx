import { useCallback, useEffect, useState } from 'react'
import { api } from '../store/api'
import { useStore } from '../store/context'
import { Button, Sheet, Switch, Tag } from './ui'

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
  return <Sheet pad>
    <form className="stack-sm" onSubmit={e => { e.preventDefault(); void save() }}>
      <div className="spread"><h3>{embedding ? 'OpenAI 向量模型' : 'OpenAI 兼容文本 API'}</h3>
        <Tag tone={draft.key_configured ? 'info' : undefined}>{draft.key_configured ? '已配置密钥' : '待配置密钥'}</Tag>
      </div>
      <p className="small muted">{embedding ? '用于资料索引与语义检索。更换模型后补建现有向量，原文检索可继续使用。' : '填写服务的 /v1 地址，通过 Chat Completions 协议运行副手。'}</p>
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
      {!embedding && <div className="spread small"><span>作为默认副手与后台抽取入口</span><Switch label="API 作为默认入口" checked={draft.default} onChange={v => setDraft({ ...draft, default: v })} /></div>}
      <p className="tiny muted">单价用于每日预算估算，请按供应商实际价格填写。密钥仅保存在服务端；更换地址时需重新填写密钥。</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      {notice && <p className="callout" role="status">{notice}</p>}
      {!editable && <p className="callout">服务端需配置 PCAS_MODEL_SETTINGS_FILE 后才能在此保存接入信息。</p>}
      <div className="row">
        <Button type="submit" variant="primary" size="sm" disabled={!editable || busy}>{busy ? '处理中…' : `保存${label}接入`}</Button>
        {embedding && <Button type="button" size="sm" disabled={!draft.key_configured || busy} onClick={() => void rebuild()}>补建现有资料向量</Button>}
      </div>
    </form>
  </Sheet>
}

/** Jev only sorts desk entries; its key lives with the other keys on the server. */
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
      setNotice(saved.working ? '已保存，试分流成功，导办台现在由 Jev 判断去向。' : '已保存，但试分流没成功：检查密钥是否正确、是否已开通 Jev。导办台暂时按本地规则分流。')
      await onSaved()
    } catch (e) { setError(e instanceof Error ? e.message : '保存失败') }
    finally { setBusy(false) }
  }
  async function remove() {
    setBusy(true); setError(''); setNotice('')
    try { await api('/v1/models/decision', undefined, 'DELETE'); setNotice('已移除，导办台按本地规则分流。'); await onSaved() }
    catch (e) { setError(e instanceof Error ? e.message : '移除失败') }
    finally { setBusy(false) }
  }
  return <Sheet pad>
    <form className="stack-sm" onSubmit={e => { e.preventDefault(); void save() }}>
      <div className="spread"><h3>导办台分流 · Jev</h3>
        <Tag tone={value.key_configured ? 'info' : undefined}>{value.saved ? '已配置密钥' : value.key_configured ? '使用服务端环境变量' : '待配置密钥'}</Tag>
      </div>
      <p className="small muted">TypeSafe 的 Jev 判断导办台上每句话是要问、要记，还是交给副手。它只返回选择，不生成文字。没有密钥时按本地规则分流。</p>
      <label className="stack-sm small">Jev API Key
        <input className="input" aria-label="Jev API Key" value={key} onChange={e => setKey(e.target.value)} type="password" autoComplete="new-password" placeholder={value.saved ? '填写新密钥以替换' : '在 console.typesafe.ai 创建'} required disabled={!editable || busy} />
      </label>
      <p className="tiny muted">配置后，导办台上的每句原文都会发给 TypeSafe 判断去向。密钥仅保存在服务端。</p>
      {error && <p className="form-error" role="alert">{error}</p>}
      {notice && <p className="callout" role="status">{notice}</p>}
      <div className="row">
        <Button type="submit" variant="primary" size="sm" disabled={!editable || busy || !key.trim()}>{busy ? '处理中…' : '保存并试分流'}</Button>
        {value.saved && <Button type="button" size="sm" disabled={busy} onClick={() => void remove()}>移除密钥</Button>}
      </div>
    </form>
  </Sheet>
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
  return <section className="section"><h2 className="section-title">API 与向量接入</h2>
    <div className="stack-sm">
      {error && <p className="form-error" role="alert">{error}</p>}
      {configuration && <>
        <ConnectionForm role="text" value={configuration.text} editable={configuration.editable} onSaved={refresh} />
        <ConnectionForm role="embedding" value={configuration.embedding} editable={configuration.editable} onSaved={refresh} />
        <DecisionForm value={configuration.decision} editable={configuration.editable} onSaved={refresh} />
      </>}
    </div>
  </section>
}
