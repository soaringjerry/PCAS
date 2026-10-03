import { useStore } from '../store/context'
import { formatTimestamp } from '../domain/time'
import { MemorySummary } from './MemorySummary'
import { ConversationContext } from './ConversationContext'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { CircleAlert, Download, FileText } from 'lucide-react'
import { api } from '../store/api'
import { SideSheet } from './Overlay'
import { Spinner, Tag } from './ui'

// How a text that was read out of an original came to be, in plain words.
const READ_BY: Record<string, string> = { vision: '模型读图', transcript: '录音转写', ocr: '图片识别', extracted: '文件提取' }

/** Where the cited passage sits in the text; nothing when it is not there word for word, or is all there is. */
function locate(text: string, excerpt?: string): [string, string, string] | null {
  const passage = excerpt?.replace(/^[…\s]+|[…\s]+$/g, '')
  if (!passage) return null
  const at = text.indexOf(passage)
  if (at < 0 || passage.length >= text.trim().length) return null
  return [text.slice(0, at), passage, text.slice(at + passage.length)]
}

/** The text itself, scrolling inside the sheet and opened at the cited passage. */
function Words({ text, excerpt }: { text: string; excerpt?: string }) {
  const box = useRef<HTMLPreElement>(null)
  const parts = locate(text, excerpt)
  useLayoutEffect(() => {
    const el = box.current
    const mark = el?.querySelector('mark')
    // A couple of lines stay above the passage, so it reads in context.
    if (el && mark) el.scrollTop += mark.getBoundingClientRect().top - el.getBoundingClientRect().top - 48
  }, [text, excerpt])
  return <pre ref={box} className="source-text source-words" tabIndex={0}>{parts ? <>{parts[0]}<mark>{parts[1]}</mark>{parts[2]}</> : text}</pre>
}

interface SourceResult { context?: { conversation?: string }; derived: { id: string; version: number }[]; source: { id: string; version: number; title: string; text: string; recorded_at: string; has_attachment: boolean; attachment_missing: boolean; representation: string }; processing: { id: string; stage: string; state: string; error_code?: string; method?: string }[] }
/**
 * The library opens a record with its version, processing state and summary.
 * Opened from a conversation (`conversation` given), it shows what was said and
 * little else: the text first, at the passage the card quoted when it has one.
 */
export function SourceSheet({ id, version, onClose, conversation }: { id: string; version?: number; onClose: () => void; conversation?: { excerpt?: string } }) {
  const { state } = useStore()
  const [data, setData] = useState<SourceResult | null>(null)
  const [error, setError] = useState('')
  const [derived, setDerived] = useState<{ id: string; version: number } | null>(null)
  useEffect(() => {
    let alive = true
    api<SourceResult>(`/v1/memory/sources/${encodeURIComponent(id)}${version ? `?version=${version}` : ''}`)
      .then((data) => { if (alive) setData(data) }).catch((e: Error) => { if (alive) setError(e.message) })
    return () => { alive = false }
  }, [id, version])
  const methods = [...new Set(data?.processing.map((job) => job.method).filter((method): method is string => !!method) ?? [])]
  const pending = data?.processing.filter((job) => job.state !== 'done') ?? []
  if (conversation) {
    const readBy = data && data.source.representation !== 'original' ? READ_BY[data.source.representation] : undefined
    return <SideSheet title={data?.source.title || '原话'} onClose={onClose} top={data && <span className="tiny muted">记录于 {formatTimestamp(data.source.recorded_at, state.settings.timezone ?? 'UTC')}</span>}>
      {error && <p className="form-error" role="alert"><CircleAlert size={14} />{error}</p>}
      {!data && !error && <p className="row muted"><Spinner />读取中…</p>}
      {data && <div className="source-read">
        {data.source.text ? <Words text={data.source.text} excerpt={conversation.excerpt} /> : <p className="tiny muted">这份原件里没有能直接显示的文字。</p>}
        {data.context?.conversation && <ConversationContext key={`${id}:${data.source.version}`} id={id} version={data.source.version} />}
        {data.source.attachment_missing && <p className="callout callout-danger" role="alert"><CircleAlert size={15} />原件现在打不开，这段文字代替不了原件；请检查附件存放的地方。</p>}
        {data.source.representation !== 'original' && <p className="callout">这段文字是从原件里读出来的{readBy ? `（${readBy}）` : ''}，可能有出入，请对照原件。</p>}
        {(data.source.has_attachment && !data.source.attachment_missing || data.derived.length > 0) && <div className="row">
          {data.source.has_attachment && !data.source.attachment_missing && <a className="btn btn-sm" href={`/v1/memory/sources/${id}/attachment?version=${data.source.version}`}><Download size={14} />下载原件</a>}
          {data.derived.map((ref) => <button type="button" className="btn btn-sm" key={ref.id} onClick={() => setDerived(ref)}><FileText size={14} />看从原件里读出来的文字</button>)}
        </div>}
      </div>}
      {derived && <SourceSheet id={derived.id} version={derived.version} conversation={conversation} onClose={() => setDerived(null)} />}
    </SideSheet>
  }
  return <SideSheet title={data?.source.title ?? '来源原文'} onClose={onClose} top={data && <><Tag>第 {data.source.version} 版</Tag><span className="tiny muted">记录于 {formatTimestamp(data.source.recorded_at, state.settings.timezone ?? 'UTC')}</span></>}>
    {error && <p className="form-error" role="alert"><CircleAlert size={14} />{error}</p>}
    {!data && !error && <p className="row muted"><Spinner />读取中…</p>}
    {data && <div className="stack">
      {methods.length > 0 && <p className="note">读取方式：{methods.map((method) => READ_BY[method] ?? method).join('、')}</p>}
      {(pending.length > 0 || data.source.attachment_missing || data.source.representation !== 'original') && <div className="stack-sm">
        {pending.length > 0 && <div className="row">{pending.map((job) => <Tag key={job.id} tone={job.error_code ? 'danger' : 'info'}>{job.stage}：{job.state}{job.error_code ? `（${job.error_code}）` : ''}</Tag>)}</div>}
        {data.source.attachment_missing && <p className="callout callout-danger" role="alert"><CircleAlert size={15} />原件当前不可用，解析文本不能代替原件；请核对附件存储。</p>}
        {data.source.representation !== 'original' && <p className="callout">这是解析文本（{data.source.representation}），请结合原件核对。</p>}
      </div>}
      {(data.source.has_attachment && !data.source.attachment_missing || data.derived.length > 0) && <div className="row">
        {data.source.has_attachment && !data.source.attachment_missing && <a className="btn btn-sm" href={`/v1/memory/sources/${id}/attachment?version=${data.source.version}`}><Download size={14} />下载原件</a>}
        {data.derived.map((ref) => <button type="button" className="btn btn-sm" key={ref.id} onClick={() => setDerived(ref)}><FileText size={14} />展开解析文本</button>)}
      </div>}
      <MemorySummary id={data.source.id} version={data.source.version} />
      {data.context?.conversation && <ConversationContext key={`${id}:${data.source.version}`} id={id} version={data.source.version} />}
      <details><summary>展开原文</summary><pre className="source-text">{data.source.text}</pre></details></div>}
    {derived && <SourceSheet id={derived.id} version={derived.version} onClose={() => setDerived(null)} />}
  </SideSheet>
}
