import { useRef, useState } from 'react'
import { api } from '../store/api'
import { useStore } from '../store/context'
import { formatTimestamp } from '../domain/time'
import { Button, Spinner, Tag } from './ui'

interface Message { id: string; version: number; text: string; role: string; expressed_at?: string; recorded_at: string; anchor: boolean }
interface Window { messages: Message[]; before?: string; after?: string; gaps: string[] }
const roleName: Record<string, string> = { user: '你', assistant: 'AI', system: '系统', tool: '工具' }

export function ConversationContext({ id, version }: { id: string; version: number }) {
  const { state } = useStore()
  const [data, setData] = useState<Window | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const pending = useRef(false)
  async function load(direction?: 'before' | 'after') {
    if (pending.current) return
    pending.current = true; setBusy(true); setError('')
    const params = new URLSearchParams({ version: String(version) })
    if (direction && data?.[direction]) params.set(direction, data[direction])
    try {
      const page = await api<Window>(`/v1/memory/sources/${encodeURIComponent(id)}/conversation?${params}`)
      setData((prior) => {
        if (!prior || !direction) return page
        const combined = direction === 'before' ? [...page.messages, ...prior.messages] : [...prior.messages, ...page.messages]
        const messages = combined.filter((m, i) => combined.findIndex((other) => other.id === m.id && other.version === m.version) === i)
        return { messages, before: direction === 'before' ? page.before : prior.before, after: direction === 'after' ? page.after : prior.after, gaps: [...new Set([...prior.gaps, ...page.gaps])] }
      })
    } catch {
      setError('当时的对话暂时无法读取，请重试；引用原话仍可在上方核对。')
    } finally { pending.current = false; setBusy(false) }
  }
  return <details className="conversation-context" onToggle={(event) => { if (event.currentTarget.open && !data) void load() }}>
    <summary>看当时的对话</summary>
    <div className="stack-sm">
      <p className="tiny muted">按当时的顺序阅读；AI 的回复用于理解上下文，观点和决定请以你的原话为准。</p>
      <p className="tiny muted">先显示引用原话附近的消息，可继续展开更早、更晚的记录。</p>
      {error && <div role="alert"><p className="form-error">{error}</p><Button size="sm" onClick={() => void load()}>重试</Button></div>}
      {data?.before && <Button size="sm" disabled={busy} onClick={() => void load('before')}>更早的消息</Button>}
      {data?.messages.map((message) => <section key={`${message.id}:${message.version}`} className={message.anchor ? 'callout conversation-message' : 'conversation-message'} aria-label={message.anchor ? '引用原话' : `${roleName[message.role] ?? '其他说话人'}的消息`}>
        <div className="row"><Tag>{roleName[message.role] ?? '其他说话人'}</Tag>{message.anchor && <Tag tone="info">引用原话</Tag>}<span className="tiny muted">{message.expressed_at ? '说于' : '记录于'} {formatTimestamp(message.expressed_at ?? message.recorded_at, state.settings.timezone ?? 'UTC')}</span></div>
        <pre className="source-text">{message.text || '这条消息没有文字内容。'}</pre>
      </section>)}
      {data?.gaps.map((gap) => <p className="callout" key={gap}>{gap}</p>)}
      {data?.after && <Button size="sm" disabled={busy} onClick={() => void load('after')}>更晚的消息</Button>}
      {busy && <p className="row tiny muted" role="status"><Spinner />读取当时的对话…</p>}
    </div>
  </details>
}
