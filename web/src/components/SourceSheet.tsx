import { useEffect, useState } from 'react'
import { api } from '../store/api'
import { SideSheet } from './Overlay'

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
  return <SideSheet title={data?.source.title ?? '来源原文'} onClose={onClose}>
    {error && <p role="alert">{error}</p>}
    {!data && !error && <p>读取中…</p>}
    {data && <div className="stack"><p className="small muted">版本 {data.source.version} · 记录于 {new Date(data.source.recorded_at).toLocaleString()}</p>
      {data.processing.filter((job) => job.state !== 'done').map((job) => <p className="small muted" key={job.id}>{job.stage}：{job.state}{job.error_code ? `（${job.error_code}）` : ''}</p>)}
      {data.source.attachment_missing && <p role="alert">原件当前不可用，解析文本不能代替原件；请核对附件存储。</p>}
      {data.source.has_attachment && !data.source.attachment_missing && <a href={`/v1/memory/sources/${id}/attachment?version=${data.source.version}`}>下载原件</a>}
      {data.source.representation !== 'original' && <p className="small muted">这是解析文本（{data.source.representation}），请结合原件核对。</p>}
      {data.derived.map((ref) => <button className="btn btn-sm" key={ref.id} onClick={() => setDerived(ref)}>展开解析文本</button>)}
      <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{data.source.text}</pre></div>}
    {derived && <SourceSheet id={derived.id} version={derived.version} onClose={() => setDerived(null)} />}
  </SideSheet>
}
