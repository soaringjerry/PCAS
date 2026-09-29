import { useState } from 'react'
import { CircleAlert, FileText, Search } from 'lucide-react'
import { api } from '../store/api'
import { SideSheet } from './Overlay'
import { SourceSheet } from './SourceSheet'
import { Button, Seg, Spinner } from './ui'

type Mode = 'continue' | 'remember' | 'history'
interface Ref { id: string; version: number; kind: string }
interface Recall { summary: string; memories: Ref[]; evidence: { id: string; source: Ref }[]; coverage: { complete: boolean; gaps: string[]; next_cursor?: string }; follow_ups: string[] }

const modes: { value: Mode; label: string }[] = [
  { value: 'continue', label: '续接话题' },
  { value: 'remember', label: '模糊回忆' },
  { value: 'history', label: '完整历史' },
]

export function RecallSheet({ query: initial, onClose }: { query: string; onClose: () => void }) {
  const [query, setQuery] = useState(initial)
  const [mode, setMode] = useState<Mode>('remember')
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
      <label className="input-icon">
        <Search size={16} />
        <input className="input" autoFocus value={query} onChange={(e) => setQuery(e.target.value)} placeholder="模糊描述、对象或原文关键词" aria-label="回忆线索" />
      </label>
      <div className="spread">
        <Seg label="检索范围" value={mode} onChange={setMode} items={modes} />
        <Button type="submit" disabled={busy} variant="primary">{busy && <Spinner />}{busy ? '查找中…' : '查找'}</Button>
      </div>
    </form>
    {error && <p className="form-error" role="alert"><CircleAlert size={14} />{error}</p>}
    {results.map((result, i) => {
      const sources = [...result.memories.filter((r) => r.kind === 'source'), ...result.evidence.map((e) => e.source)]
        .filter((r, index, all) => all.findIndex((v) => v.id === r.id && v.version === r.version) === index)
      return <section key={i} className="recall-result">
        <div className={`recall-summary${result.summary ? '' : ' faint'}`}>{result.summary || '当前范围没有找到。'}</div>
        {sources.length > 0 && <div className="row">{sources.map((ref) => <button type="button" className="chip" key={`${ref.id}:${ref.version}`} onClick={() => setSource(ref)}><FileText size={12} />来源 · v{ref.version}</button>)}</div>}
        {[...result.coverage.gaps, ...result.follow_ups].length > 0 && <ul className="recall-notes">
          {result.coverage.gaps.map((gap) => <li key={gap}>{gap}</li>)}
          {result.follow_ups.map((hint) => <li key={hint}>{hint}</li>)}
        </ul>}
      </section>
    })}
    {results.at(-1)?.coverage.next_cursor && <Button disabled={busy} onClick={() => void search(true)}>继续扩大历史覆盖</Button>}
    {source && <SourceSheet id={source.id} version={source.version} onClose={() => setSource(null)} />}
  </div></SideSheet>
}
