import { useState } from 'react'
import { api } from '../store/api'
import { SideSheet } from './Overlay'
import { SourceSheet } from './SourceSheet'
import { Button } from './ui'

interface Ref { id: string; version: number; kind: string }
interface Recall { summary: string; memories: Ref[]; evidence: { id: string; source: Ref }[]; coverage: { complete: boolean; gaps: string[]; next_cursor?: string }; follow_ups: string[] }
export function RecallSheet({ query: initial, onClose }: { query: string; onClose: () => void }) {
  const [query, setQuery] = useState(initial)
  const [mode, setMode] = useState('remember')
  const [results, setResults] = useState<Recall[]>([])
  const [source, setSource] = useState<Ref | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [searched, setSearched] = useState({ query: '', mode: '' })
  const search = async (more = false) => {
    setBusy(true); setError('')
    const target = more ? searched : { query, mode }
    try {
      const result = await api<Recall>('/v1/memory/recall', { ...target, context: { text: '', objects: [] }, budget: { candidates: 15, tokens: 4000, edges: 15, hops: 1 }, cursor: more ? results.at(-1)?.coverage.next_cursor : undefined })
      setSearched(target); setResults((prior) => more ? [...prior, result] : [result])
    } catch (e) { setError(e instanceof Error ? e.message : '检索失败') }
    finally { setBusy(false) }
  }
  return <SideSheet title="找回记忆" onClose={onClose}><div className="stack">
    <form className="stack-sm" onSubmit={(e) => { e.preventDefault(); void search() }}>
      <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="模糊描述、对象或原文关键词" aria-label="回忆线索" />
      <select value={mode} onChange={(e) => setMode(e.target.value)} aria-label="检索范围"><option value="continue">续接当前话题</option><option value="remember">模糊回忆</option><option value="history">完整历史</option></select>
      <Button type="submit" disabled={busy} variant="primary">{busy ? '查找中…' : '查找'}</Button>
    </form>
    {error && <p role="alert">{error}</p>}
    {results.map((result, i) => <section key={i} className="stack-sm">
      <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{result.summary || '当前范围没有找到。'}</pre>
      <div className="row">{[...result.memories.filter((r) => r.kind === 'source'), ...result.evidence.map((e) => e.source)].filter((r, index, all) => all.findIndex((v) => v.id === r.id && v.version === r.version) === index).map((ref) => <Button key={`${ref.id}:${ref.version}`} size="sm" onClick={() => setSource(ref)}>展开来源 · v{ref.version}</Button>)}</div>
      {result.coverage.gaps.map((gap) => <p key={gap} className="small muted">{gap}</p>)}
      {result.follow_ups.map((hint) => <p key={hint} className="small muted">{hint}</p>)}
    </section>)}
    {results.at(-1)?.coverage.next_cursor && <Button disabled={busy} onClick={() => void search(true)}>继续扩大历史覆盖</Button>}
    {source && <SourceSheet id={source.id} version={source.version} onClose={() => setSource(null)} />}
  </div></SideSheet>
}
