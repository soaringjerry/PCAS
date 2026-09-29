import { useEffect, useState } from 'react'
import { api } from '../store/api'

type Summary = { text: string; coverage: { gaps: string[] }; dependencies: { id: string; version: number }[] }
export function MemorySummary({ id, version }: { id: string; version: number }) {
  const [data, setData] = useState<Summary | null>(null)
  const [error, setError] = useState('')
  useEffect(() => { let alive = true; void api<Summary>('/v1/memory/summary', { id, version, tokens: 1000 }).then(v => { if (alive) setData(v) }).catch((e: Error) => { if (alive) setError(e.message) }); return () => { alive = false } }, [id, version])
  if (error) return <p className="tiny muted">当前摘要未生成，可直接核对下面的原文。</p>
  if (!data) return <p className="tiny muted">正在整理来源摘要…</p>
  return <details open><summary>来源摘要与当前理解</summary><pre className="source-text">{data.text || '暂无可用文字，请展开原件或解析结果。'}</pre>{data.coverage.gaps.map((g, i) => <p key={i} className="tiny muted">{g}</p>)}<p className="tiny muted">依据 {data.dependencies.length} 条材料的有效版本生成；历史原话与当前理解分别保留。</p></details>
}
