import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, Search } from 'lucide-react'
import { memoryKindLabel } from '../domain/labels'
import type { Memory } from '../domain/types'
import { api } from '../store/api'
import { useMemoryList, type MemoryFilter } from '../store/memories'
import { Stepper } from './controls'
import { Button, Spinner, Tag } from './ui'

const baseFilter: Omit<MemoryFilter, 'q'> = {
  entity: '',
  group: '',
  nature: '',
  within: '',
  epistemic: '',
  trust: '',
  retired: '',
}

const SEARCH_DEBOUNCE_MS = 250

/**
 * Tunes how long one memory keeps its everyday recall priority.
 * Searches across all memories on the server with pagination rather than only recent snapshot entries.
 */
export function MemoryActivitySettings() {
  const [query, setQuery] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')
  const [selected, setSelected] = useState<Memory | null>(null)
  const [days, setDays] = useState(30)
  const [limit, setLimit] = useState(8)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState<{ text: string; failed?: boolean } | null>(null)

  useEffect(() => {
    const trimmed = query.trim()
    const timer = window.setTimeout(() => setDebouncedQuery(trimmed), trimmed ? SEARCH_DEBOUNCE_MS : 0)
    return () => window.clearTimeout(timer)
  }, [query])

  const list = useMemoryList({ ...baseFilter, q: debouncedQuery }, true)

  const pick = (m: Memory) => {
    setSelected(m)
    setDays(m.halfLifeDays ?? 30)
    setLimit(m.reinforcementLimit ?? 8)
    setMessage(null)
  }

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!selected) return
    setBusy(true)
    setMessage(null)
    try {
      await api('/v1/memory/activity', {
        ref: { id: selected.id, version: selected.recordVersion, kind: 'claim' },
        half_life_days: days,
        reinforcement_limit: limit,
        pinned: selected.pinned,
      })
      setSelected({ ...selected, halfLifeDays: days, reinforcementLimit: limit })
      setMessage({ text: '已保存。它不会被删除，点名去找时照样找得到。' })
    } catch (err) {
      setMessage({ text: (err as Error).message, failed: true })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="stack-sm">
      <p className="small muted">只影响日常交流里它排得多靠前，不会删除或隐藏它。你提到、确认或采用它时，它会重新靠前。</p>
      <label className="input-icon">
        <Search size={15} />
        <input
          type="search"
          className="input"
          aria-label="搜索要调节权重的记忆"
          placeholder="搜索任意一条记忆…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
      </label>

      {list.phase === 'failed' && (
        <div className="obs-problem" role="alert">
          <span>{list.problem}</span>
          <Button size="sm" onClick={list.retry}>
            重试
          </Button>
        </div>
      )}

      {list.phase === 'loading' && list.items.length === 0 && (
        <p className="row muted small">
          <Spinner size={12} /> 正在查找记忆…
        </p>
      )}

      {list.phase === 'ready' && list.items.length === 0 && (
        <p className="small muted">{query.trim() ? '没有找到匹配的记忆。' : '还没有记忆。'}</p>
      )}

      {list.items.length > 0 && (
        <div className="stack-sm" style={{ gap: 6 }}>
          <div className="tiny muted">
            共 {list.total} 条，当前显示 {list.items.length} 条
          </div>
          <div className="obs-picker-list" role="listbox" aria-label="记忆列表">
            {list.items.map((m) => {
              const on = selected?.id === m.id
              return (
                <button
                  key={m.id}
                  type="button"
                  role="option"
                  aria-selected={on}
                  className={`obs-picker-item${on ? ' on' : ''}`}
                  onClick={() => pick(m)}
                >
                  <span className="grow">{m.text}</span>
                  <Tag>{memoryKindLabel[m.kind]}</Tag>
                </button>
              )
            })}
          </div>
          {list.hasMore && (
            <div className="row">
              <Button size="sm" variant="quiet" disabled={list.more === 'loading'} onClick={list.loadMore}>
                {list.more === 'loading' ? '正在加载…' : '再看 50 条'}
              </Button>
              {list.more === 'failed' && (
                <span className="small warn-text" role="alert">
                  {list.moreProblem}
                </span>
              )}
            </div>
          )}
        </div>
      )}

      {selected && (
        <form className="stack-sm" style={{ paddingTop: 8, borderTop: '1px solid var(--sep)' }} onSubmit={(e) => void save(e)}>
          <div className="spread">
            <span className="small ink">已选：{selected.text}</span>
            <Link className="obs-link-pill" to={`/library?m=${encodeURIComponent(selected.id)}`}>
              在资料库查看
              <ArrowUpRight size={12} />
            </Link>
          </div>
          <label className="field">
            <span className="field-label">多少天没用到，优先程度减半</span>
            <Stepper label="减半天数" min={1} max={36500} value={days} onChange={setDays} />
          </label>
          <label className="field">
            <span className="field-label">经常用到时，这个天数最多延长到几倍</span>
            <Stepper label="最多延长倍数" min={1} max={100} value={limit} onChange={setLimit} />
          </label>
          {selected.pinned && <p className="small muted">这条记忆已固定保留，现在不会减弱。</p>}
          <div className="row">
            <Button type="submit" variant="primary" size="sm" disabled={busy}>
              {busy ? '正在保存…' : '保存权重设置'}
            </Button>
          </div>
        </form>
      )}

      {message && (
        <p role={message.failed ? 'alert' : 'status'} className={`small${message.failed ? ' warn-text' : ''}`}>
          {message.text}
        </p>
      )}
    </div>
  )
}
