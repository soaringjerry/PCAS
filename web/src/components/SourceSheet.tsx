import { MemorySummary } from './MemorySummary'
import { useEffect, useState } from 'react'
import { CircleAlert, Download, FileText } from 'lucide-react'
import { api } from '../store/api'
import { SideSheet } from './Overlay'
import { Spinner, Tag } from './ui'

interface SourceResult { derived: { id: string; version: number }[]; source: { id: string; version: number; title: string; text: string; recorded_at: string; has_attachment: boolean; attachment_missing: boolean; representation: string }; processing: { id: string; stage: string; state: string; error_code?: string }[] }
export function SourceSheet({ id, version, onClose }: { id: string; version?: number; onClose: () => void }) {
  const [data, setData] = useState<SourceResult | null>(null)
  const [error, setError] = useState('')
  const [derived, setDerived] = useState<{ id: string; version: number } | null>(null)
  useEffect(() => {
    let alive = true
    api<SourceResult>(`/v1/memory/sources/${encodeURIComponent(id)}${version ? `?version=${version}` : ''}`)
      .then((data) => { if (alive) setData(data) }).catch((e: Error) => { if (alive) setError(e.message) })
    return () => { alive = false }
  }, [id, version])
  const pending = data?.processing.filter((job) => job.state !== 'done') ?? []
  return <SideSheet title={data?.source.title ?? '来源原文'} onClose={onClose} top={data && <><Tag>第 {data.source.version} 版</Tag><span className="tiny muted">记录于 {new Date(data.source.recorded_at).toLocaleString()}</span></>}>
    {error && <p className="form-error" role="alert"><CircleAlert size={14} />{error}</p>}
    {!data && !error && <p className="row muted"><Spinner />读取中…</p>}
    {data && <div className="stack">
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
      <details><summary>展开原文</summary><pre className="source-text">{data.source.text}</pre></details></div>}
    {derived && <SourceSheet id={derived.id} version={derived.version} onClose={() => setDerived(null)} />}
  </SideSheet>
}
