import { RecallSheet } from '../components/RecallSheet'
import { SourceSheet } from '../components/SourceSheet'
import { ImportSheet } from '../components/ImportSheet'
import { api, downloadExport } from '../store/api'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router'
import { ChevronRight, CircleAlert, Download, History, Info, RotateCw, Search, Trash2, Upload } from 'lucide-react'
import { Checkbox, Chip } from '../components/controls'
import { EventTime, Fade, FromLine, Mentions, ProjectLink, SaidAt, TrustTag } from '../components/Marks'
import { ConfirmModal, SideSheet } from '../components/Overlay'
import { UnsureSheet } from '../components/UnsureSheet'
import { Button, Empty, Progress, Seg, Sheet, Spinner, Switch, Tag } from '../components/ui'
import { jobStatusLabel, memoryKindLabel, sampleStateLabel, sourceStatusLabel, triggerLabel } from '../domain/labels'
import { formatAgo, formatTimestamp, formatWhen } from '../domain/time'
import type { Epistemic, Memory, MemoryFacet, MemoryKind, MemoryMention, Source, TrainingSample } from '../domain/types'
import { useStore } from '../store/context'
import { useMemory, useMemoryFacets, useMemoryList } from '../store/memories'
import { useToast } from '../store/toast'

type Tab = 'memory' | 'sources' | 'training'

const actor = { user: '你', ai: 'AI', import: '导入', system: '系统' } as const

const hasEventTime = (m: Memory) => Boolean(m.eventFrom && m.eventPrecision && m.eventPrecision !== 'unknown')

function MemorySheet({ memory, entity, onClose, onPick, onDeleted }: {
  memory: Memory
  /** The person or place the list is narrowed to, if any. */
  entity: string
  onClose: () => void
  onPick: (mention: MemoryMention) => void
  onDeleted: () => void
}) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [text, setText] = useState(memory.text)
  const [reason, setReason] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [includeSources, setIncludeSources] = useState(false)
  const runs = state.runs.filter((r) => r.contextMemoryIds.includes(memory.id))
  const samples = state.samples.filter((s) => s.origin.memoryId === memory.id)
  const changed = text.trim() !== memory.text
  const mentions = memory.mentions ?? []
  const people = mentions.filter((m) => m.role === 'person')
  const places = mentions.filter((m) => m.role === 'place')
  const others = mentions.filter((m) => m.role !== 'person' && m.role !== 'place')

  return (
    <SideSheet
      title={memoryKindLabel[memory.kind]}
      onClose={onClose}
      top={
        <>
          <TrustTag value={memory.epistemic} />
          {memory.epistemic === 'confirmed' && <Tag tone="success">已确认</Tag>}
          <ProjectLink id={memory.projectId} />
        </>
      }
    >
      <div className="stack">
        <div className="stack-sm">
          <textarea className="textarea" style={{ minHeight: 84 }} value={text} onChange={(e) => setText(e.target.value)} aria-label="内容" />
          {changed && (
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} placeholder="为什么改？（会记成一次纠正）" />
          )}
          <div className="row">
            <Button
              size="sm"
              variant="primary"
              disabled={!changed || !text.trim()}
              onClick={async () => {
                if (!(await dispatch({ type: 'editMemory', id: memory.id, text: text.trim(), reason: reason.trim() }))) return
                setReason('')
                toast.show('改好了，旧版本也留着')
              }}
            >
              存为新版本
            </Button>
            {memory.epistemic !== 'confirmed' && !changed && (
              <Button size="sm" onClick={() => dispatch({ type: 'confirmMemory', id: memory.id })}>
                没错
              </Button>
            )}
          </div>
          <div className="row small muted">
            <Fade value={memory.exposure} />
            <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'pinMemory', id: memory.id })}>{memory.pinned ? '取消固定保留' : '固定保留'}</Button>
            <span>{memory.exposure < 0.4 ? '很久没用到，已经变淡，但还在' : `最近用到：${formatAgo(memory.lastUsedAt, state.settings.timezone ?? 'UTC')}`}</span>
          </div>
        </div>

        {(hasEventTime(memory) || memory.expressedAt || mentions.length > 0) && (
          <dl className="mem-facts">
            {hasEventTime(memory) && (
              <div>
                <dt>说的是哪天的事</dt>
                <dd><EventTime bare from={memory.eventFrom} to={memory.eventTo} precision={memory.eventPrecision} /></dd>
              </div>
            )}
            {memory.expressedAt && (
              <div>
                <dt>什么时候说的</dt>
                <dd><SaidAt full at={memory.expressedAt} /></dd>
              </div>
            )}
            {people.length > 0 && (
              <div>
                <dt>提到的人</dt>
                <dd className="mem-marks"><Mentions mentions={people} active={entity} onPick={onPick} /></dd>
              </div>
            )}
            {places.length > 0 && (
              <div>
                <dt>地点</dt>
                <dd className="mem-marks"><Mentions mentions={places} active={entity} onPick={onPick} /></dd>
              </div>
            )}
            {others.length > 0 && (
              <div>
                <dt>还提到</dt>
                <dd className="mem-marks"><Mentions mentions={others} active={entity} onPick={onPick} /></dd>
              </div>
            )}
          </dl>
        )}

        <div className="stack-sm">
          <h3 className="sheet-subtitle">谁能看到</h3>
          <div className="row" style={{ gap: 6 }}>
            {state.agents.map((agent) => {
              const on = memory.visibleTo.includes(agent.id)
              return (
                <Chip
                  key={agent.id}
                  on={on}
                  onToggle={() =>
                    dispatch({
                      type: 'setMemoryVisibility',
                      id: memory.id,
                      agentIds: on ? memory.visibleTo.filter((a) => a !== agent.id) : [...memory.visibleTo, agent.id],
                    })
                  }
                >
                  {agent.name}
                </Chip>
              )
            })}
          </div>
        </div>

        <div className="stack-sm">
          <h3 className="sheet-subtitle">改过的版本</h3>
          <ol className="timeline">
            {memory.versions.map((v, i) => (
              <li key={`${v.at}-${i}`}>
                <span className={`tl-dot${i === memory.versions.length - 1 ? ' done' : ''}`}>{i + 1}</span>
                <div>
                  <div className={i === memory.versions.length - 1 ? 'ink' : 'faint'} style={i === memory.versions.length - 1 ? undefined : { textDecoration: 'line-through' }}>
                    {v.text}
                  </div>
                  <div className="tl-when">
                    {actor[v.by]} · {formatAgo(v.at, state.settings.timezone ?? 'UTC')}
                    {v.reason && ` · “${v.reason}”`}
                  </div>
                </div>
              </li>
            ))}
          </ol>
        </div>

        {memory.sources.length > 0 && (
          <div className="stack-sm">
            <h3 className="sheet-subtitle">从哪来的</h3>
            {memory.sources.map((s, i) => (
              <FromLine key={i} source={s} />
            ))}
          </div>
        )}

        <div className="stack-sm">
          <p className="small muted">
            AI 用过它 {runs.length} 次，{samples.length} 条训练样本来自它。
          </p>
          <div>
            <Button size="sm" variant="danger" icon={<Trash2 size={14} />} onClick={() => setDeleting(true)}>
              删掉这条
            </Button>
          </div>
        </div>
      </div>

      {deleting && (
        <ConfirmModal
          title="删掉这条记忆？"
          onClose={() => setDeleting(false)}
          onConfirm={async () => {
            if (!(await dispatch({ type: 'deleteMemory', id: memory.id, includeSources }))) return
            onDeleted()
            onClose()
            toast.show('删掉了')
          }}
        >
          <p className="muted">删除这条结构化记忆及其派生内容。保留的原文仍可被检索。</p>
          <Checkbox checked={includeSources} onChange={(e) => setIncludeSources(e.target.checked)}>
            同时删除来源原文及从这些来源提取的其他记忆
          </Checkbox>
          {(runs.length > 0 || samples.length > 0) && (
            <p className="small muted">
              {runs.length > 0 && `${runs.length} 次相关 AI 结果及由其采纳的副本会删除。`}
              {samples.length > 0 && `${samples.length} 条由它来的训练样本会删除。`}
            </p>
          )}
        </ConfirmModal>
      )}
    </SideSheet>
  )
}

const natures = Object.keys(memoryKindLabel) as MemoryKind[]
type Trust = Exclude<Epistemic, 'planned'>
const trusts: { value: Trust; label: string }[] = [
  { value: 'confirmed', label: '已确认' },
  { value: 'sourced', label: '原话有据' },
  { value: 'inferred', label: '推测' },
]
/** How many people or places are offered before 「更多」. */
const FACETS_SHOWN = 6

/** The people or the places memories mention, as a row to pick one from. */
function FacetRow({ label, entries, active, onPick }: { label: string; entries: MemoryFacet[]; active: string; onPick: (entityId: string) => void }) {
  const [all, setAll] = useState(false)
  if (!entries.length) return null
  // The one in use stays in sight even when it is far down the list.
  const shown = all ? entries : entries.filter((e, i) => i < FACETS_SHOWN || e.entityId === active)
  return (
    <div className="mem-facet" role="group" aria-label={label}>
      <span className="mem-facet-label">{label}</span>
      <div className="mem-facet-chips">
        {shown.map((e) => (
          <button
            key={e.entityId}
            type="button"
            className={`chip chip-toggle${e.entityId === active ? ' on' : ''}`}
            aria-pressed={e.entityId === active}
            onClick={() => onPick(e.entityId)}
          >
            <span className="mem-facet-name">{e.name}</span>
            <span className="n">{e.count}</span>
          </button>
        ))}
        {entries.length > FACETS_SHOWN && (
          <button type="button" className="link-btn mem-facet-more" onClick={() => setAll((v) => !v)}>
            {all ? '收起' : `更多 ${entries.length - shown.length}`}
          </button>
        )}
      </div>
    </div>
  )
}

function MemoryTab() {
  const [params, setParams] = useSearchParams()
  // The filters live in the address, so a reload or the back button keeps them.
  const q = params.get('q') ?? ''
  const entity = params.get('entity') ?? ''
  const nature = natures.find((n) => n === params.get('nature')) ?? ''
  const epistemic = trusts.find((t) => t.value === params.get('epistemic'))?.value ?? ''
  const openId = params.get('m')
  const change = (patch: Record<string, string | null>, replace = false) =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      for (const [name, value] of Object.entries(patch)) {
        if (value) next.set(name, value)
        else next.delete(name)
      }
      return next
    }, { replace })

  const [typed, setTyped] = useState({ text: q, from: q })
  // Back and forward change the address without going through the input.
  if (typed.from !== q) setTyped({ text: q, from: q })
  const typing = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(typing.current), [])
  const type = (text: string) => {
    setTyped((t) => ({ ...t, text }))
    window.clearTimeout(typing.current)
    typing.current = window.setTimeout(() => {
      setTyped({ text, from: text.trim() })
      change({ q: text.trim() }, true)
    }, 250)
  }

  const list = useMemoryList({ q, entity, nature, epistemic })
  const { facets, problem: facetsProblem, retry: retryFacets } = useMemoryFacets()
  const opened = useMemory(openId, list.items.find((m) => m.id === openId))
  const [recalling, setRecalling] = useState(false)
  const filtered = Boolean(q || entity || nature || epistemic)
  const pick = (entityId: string) => change({ entity: entityId === entity ? null : entityId, m: null })
  const entityName = entity
    ? [...(facets?.people ?? []), ...(facets?.places ?? [])].find((f) => f.entityId === entity)?.name
      ?? list.items.flatMap((m) => m.mentions ?? []).find((m) => m.entityId === entity)?.name
    : undefined

  // Reading on is automatic: the next page is fetched as the end of the list comes into view.
  const end = useRef<HTMLDivElement>(null)
  const { hasMore, more, loadMore } = list
  const count = list.items.length
  useEffect(() => {
    const el = end.current
    if (!el || !hasMore || more !== 'idle') return
    const observer = new IntersectionObserver((entries) => { if (entries.some((e) => e.isIntersecting)) loadMore() }, { rootMargin: '600px' })
    observer.observe(el)
    return () => observer.disconnect()
  }, [hasMore, more, loadMore, count])

  return (
    <>
      {recalling && <RecallSheet query={typed.text} onClose={() => setRecalling(false)} />}
      <div className="toolbar">
        <label className="search">
          <Search size={15} />
          <input placeholder="搜记忆里的字…" value={typed.text} onChange={(e) => type(e.target.value)} aria-label="搜索记忆" />
        </label>
        <Button icon={<History size={14} />} onClick={() => setRecalling(true)}>
          深入查找
        </Button>
      </div>
      <div className="toolbar">
        <Seg
          label="类型"
          value={nature || 'all'}
          onChange={(v) => change({ nature: v === 'all' ? null : v })}
          items={[{ value: 'all', label: '全部' }, ...natures.map((k) => ({ value: k, label: memoryKindLabel[k] }))]}
        />
        <Seg
          label="可信度"
          value={epistemic || 'all'}
          onChange={(v) => change({ epistemic: v === 'all' ? null : v })}
          items={[{ value: 'all', label: '都看' }, ...trusts]}
        />
      </div>
      {facets && (facets.people.length > 0 || facets.places.length > 0) && (
        <div className="mem-facets">
          <FacetRow label="提到的人" entries={facets.people} active={entity} onPick={pick} />
          <FacetRow label="地点" entries={facets.places} active={entity} onPick={pick} />
        </div>
      )}
      {facetsProblem && (
        <p className="hint-line" role="alert">
          <CircleAlert size={13} />
          <span>
            人和地点没读出来：{facetsProblem}{' '}
            <button type="button" className="link-btn" onClick={retryFacets}>再读一次</button>
          </span>
        </p>
      )}
      <p className="hint-line">
        <Info size={13} />
        「原话有据」保留你刚记录的直接表达，不代表已经核实；波浪下划线是待确认的 AI 理解。长期不用的记忆会变淡；深入查找可翻历史和原文。
      </p>
      {filtered && (
        <p className="mem-summary" role="status">
          {list.phase === 'ready' && <span>{entity ? `提到「${entityName ?? '它'}」的记忆` : '符合的记忆'}有 {list.total} 条</span>}
          <button type="button" className="link-btn" onClick={() => { window.clearTimeout(typing.current); change({ q: null, entity: null, nature: null, epistemic: null }) }}>
            清掉筛选
          </button>
        </p>
      )}
      <Sheet>
        {list.phase === 'loading' ? (
          <div className="mem-state" role="status">
            <Spinner />
            正在读取记忆…
          </div>
        ) : list.phase === 'failed' ? (
          <div className="mem-state failed" role="alert">
            <CircleAlert size={15} />
            <span>记忆没读出来：{list.problem}</span>
            <Button size="sm" icon={<RotateCw size={13} />} onClick={list.retry}>
              重试
            </Button>
          </div>
        ) : count === 0 ? (
          <Empty>{filtered ? '没找到符合的记忆。' : '还没有记忆。跟秘书说点什么，或者导入资料，就会有了。'}</Empty>
        ) : (
          <div className="list">
            {list.items.map((m) => (
              <div key={m.id} className="mem-entry" style={{ opacity: 0.5 + m.exposure * 0.5 }} onClick={() => change({ m: m.id })}>
                <div className="grow">
                  {/* The click is handled by the row; the button gives the keyboard the same way in. */}
                  <button type="button" className={`mem-text${m.epistemic === 'inferred' ? ' guess' : ''}`}>{m.text}</button>
                  {(hasEventTime(m) || (m.mentions?.length ?? 0) > 0) && (
                    <div className="mem-marks">
                      <EventTime from={m.eventFrom} to={m.eventTo} precision={m.eventPrecision} />
                      <Mentions mentions={m.mentions} active={entity} limit={6} onPick={(mention) => pick(mention.entityId)} />
                    </div>
                  )}
                  <div className="meta">
                    <span>{memoryKindLabel[m.kind]}</span>
                    <TrustTag value={m.epistemic} />
                    <SaidAt at={m.expressedAt} />
                    {m.versions.length > 1 && <span>改过 {m.versions.length - 1} 次</span>}
                    <span>{m.visibleTo.length ? `${m.visibleTo.length} 个 AI 能看` : '只有你能看'}</span>
                    <ProjectLink id={m.projectId} />
                  </div>
                </div>
                <Fade value={m.exposure} />
              </div>
            ))}
            {hasMore && (
              <div ref={end} className={`mem-state${more === 'failed' ? ' failed' : ''}`} role={more === 'failed' ? 'alert' : 'status'}>
                {more === 'failed' ? (
                  <>
                    <CircleAlert size={15} />
                    <span>后面的没读出来：{list.moreProblem}</span>
                    <Button size="sm" icon={<RotateCw size={13} />} onClick={loadMore}>
                      接着读
                    </Button>
                  </>
                ) : (
                  <>
                    <Spinner />
                    正在读后面的…
                  </>
                )}
              </div>
            )}
          </div>
        )}
      </Sheet>
      {openId && opened.memory && (
        <MemorySheet
          key={opened.memory.id}
          memory={opened.memory}
          entity={entity}
          onClose={() => change({ m: null })}
          onPick={(mention) => pick(mention.entityId)}
          onDeleted={() => list.remove(opened.memory!.id)}
        />
      )}
      {openId && !opened.memory && (
        <SideSheet title="记忆" onClose={() => change({ m: null })}>
          {opened.phase === 'loading' && <p className="row muted"><Spinner />读取中…</p>}
          {opened.phase === 'gone' && <p className="muted">这条记忆已经不在了：它被删掉了，或者记下它的那一步被撤销了。</p>}
          {opened.phase === 'failed' && (
            <div className="stack-sm">
              <p className="form-error" role="alert"><CircleAlert size={14} />这条记忆没读出来：{opened.problem}</p>
              <div><Button size="sm" icon={<RotateCw size={13} />} onClick={opened.retry}>重试</Button></div>
            </div>
          )}
        </SideSheet>
      )}
    </>
  )
}

const sourceKindMark: Record<Source['kind'], string> = { said: '说', note: '记', telegram: 'T', import: '聊', file: '文' }
const sourceKindText: Record<Source['kind'], string> = { said: '你在这里说过的每一句', note: '手动添加记忆时写下的', telegram: '发给机器人的消息', import: '导入的聊天记录', file: '单份资料' }
const roleText: Record<string, string> = { user: '你', assistant: 'AI', system: '系统', tool: '工具' }

interface SourceItem { id: string; title: string; excerpt: string; role?: string; at: string }

/** The originals behind one library entry, newest first, a page at a time. */
function SourceGroupSheet({ group, onOpen, onClose }: { group: Source; onOpen: (id: string) => void; onClose: () => void }) {
  const { state } = useStore()
  const [items, setItems] = useState<SourceItem[]>([])
  const [next, setNext] = useState('')
  const [busy, setBusy] = useState(true)
  const [error, setError] = useState('')
  const [typed, setTyped] = useState('')
  const [find, setFind] = useState('')
  // Search a moment after typing stops, not on every key.
  useEffect(() => {
    const t = window.setTimeout(() => setFind(typed.trim()), 300)
    return () => window.clearTimeout(t)
  }, [typed])
  const load = useCallback(async (cursor: string) => {
    setBusy(true); setError('')
    try {
      const page = await api<{ items: SourceItem[]; next: string }>(`/v1/workspace/source-groups/${encodeURIComponent(group.id)}/items?limit=50${find ? `&q=${encodeURIComponent(find)}` : ''}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`)
      setItems((old) => (cursor ? [...old, ...page.items] : page.items))
      setNext(page.next)
    } catch (e) { setError(e instanceof Error ? e.message : '没读到，请重试') } finally { setBusy(false) }
  }, [group.id, find])
  // The list is fetched after the sheet opens; nothing is shown from a previous entry.
  // eslint-disable-next-line react-hooks/set-state-in-effect
  useEffect(() => { void load('') }, [load])
  return (
    <SideSheet title={group.name} top={<span className="tiny muted">共 {group.itemCount} 条，新的在前</span>} onClose={onClose}>
      {group.itemCount > 10 && <input className="input" type="search" aria-label="在这里面搜" placeholder="搜这里面的字…" value={typed} onChange={(e) => setTyped(e.target.value)} />}
      <div className="list">
        {items.map((item) => (
          <button key={item.id} type="button" className="item source-item" onClick={() => onOpen(item.id)} aria-label={`查看原文：${[item.title, item.excerpt].filter(Boolean).join(' · ')}`}>
            <div className="grow stack-sm" style={{ gap: 4 }}>
              <span className="item-title">{item.excerpt || item.title || '（没有文字）'}</span>
              <div className="meta" style={{ marginTop: 0 }}>
                {item.role && roleText[item.role] && <span>{roleText[item.role]}</span>}
                {group.kind === 'import' && item.title && <span>{item.title}</span>}
                <span>{formatTimestamp(item.at, state.settings.timezone ?? 'UTC')}</span>
              </div>
            </div>
            <ChevronRight size={14} />
          </button>
        ))}
      </div>
      {error && <p className="form-error" role="alert">{error}</p>}
      {!busy && !error && items.length === 0 && <p className="hall-empty">{find ? '没有搜到。' : '这里现在没有内容。'}</p>}
      {(next || busy) && <Button disabled={busy} onClick={() => void load(next)}>{busy ? '正在读取…' : '再看 50 条'}</Button>}
    </SideSheet>
  )
}

function SourcesTab() {
  const [importing, setImporting] = useState(false)
  const [sourceId, setSourceId] = useState<string | null>(null)
  const [group, setGroup] = useState<Source | null>(null)
  const { state, dispatch } = useStore()
  const rank = { running: 0, failed: 1, waiting: 2, queued: 3, done: 4 }
  const jobs = [...state.jobs].sort((a, b) => rank[a.status] - rank[b.status])
  return (
    <div className="stack">
      <div className="spread">
        <p className="small muted">资料从哪来。同一个出处的合在一起，点开再逐条看。</p>
        <Button variant="primary" icon={<Upload size={14} />} onClick={() => setImporting(true)}>
          导入资料
        </Button>
      </div>
      {sourceId && <SourceSheet id={sourceId} onClose={() => setSourceId(null)} />}
      {importing && <ImportSheet onClose={() => setImporting(false)} />}
      {group && <SourceGroupSheet group={group} onOpen={setSourceId} onClose={() => setGroup(null)} />}
      {state.sources.length === 0 && <p className="hall-empty">还没有资料。跟秘书说话、导入聊天记录或文件之后，会出现在这里。</p>}
      <div className="sources-grid">
        {state.sources.map((s) => (
          <button key={s.id} type="button" className="source-card" onClick={() => (s.single ? setSourceId(s.id) : setGroup(s))} aria-label={s.single ? `查看原文：${s.name}` : `查看：${s.name}，共 ${s.itemCount} 条`}>
            <div className="source-card-head">
              <span className="stamp">{sourceKindMark[s.kind] ?? s.name.slice(0, 1)}</span>
              <div className="grow">
                <div className="source-name">{s.name}</div>
                <div className="tiny muted">{sourceKindText[s.kind]}</div>
              </div>
              {s.status !== 'connected' && <Tag tone={sourceStatusLabel[s.status].tone}>{sourceStatusLabel[s.status].text}</Tag>}
            </div>
            {s.note && <p className="source-note">{s.note}</p>}
            <div className="source-card-foot">
              {!s.single && <span>{s.itemCount} 条</span>}
              {s.lastSyncAt && <span>{s.single ? `${formatAgo(s.lastSyncAt, state.settings.timezone ?? 'UTC')}更新` : `最近 ${formatAgo(s.lastSyncAt, state.settings.timezone ?? 'UTC')}`}</span>}
              <span className="source-open">
                {s.single ? '查看原文' : '逐条查看'}
                <ChevronRight size={13} />
              </span>
            </div>
          </button>
        ))}
      </div>

      <Sheet title={<h3>后台在做的事</h3>} aside={<span className="tiny muted">和你的待办分开；重复导入不会产生重复内容</span>}>
        <div className="list">
          {jobs.map((job) => (
            <div key={job.id} className="item">
              <div className="grow stack-sm" style={{ gap: 4 }}>
                <div className="spread">
                  <span className="item-title">{job.title}</span>
                  <Tag tone={jobStatusLabel[job.status].tone}>{jobStatusLabel[job.status].text}</Tag>
                </div>
                <div className="meta" style={{ marginTop: 0 }}>
                  <span>{triggerLabel[job.trigger]}</span>
                  <span>{job.detail}</span>
                  {job.nextRunAt && job.status !== 'done' && <span>下次：{formatWhen(job.nextRunAt, state.settings.timezone ?? 'UTC')}</span>}
                </div>
                {job.status === 'running' && job.progress !== undefined && <Progress value={job.progress} />}
                {job.status === 'failed' && (
                  <div className="row">
                    {job.recovery && <span className="small">建议：{job.recovery}</span>}
                    <Button size="sm" icon={<RotateCw size={13} />} onClick={() => dispatch({ type: 'retryJob', id: job.id })}>
                      重试
                    </Button>
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      </Sheet>
    </div>
  )
}

const sampleKind: Record<TrainingSample['kind'], string> = { correction: '你的纠正', 'adopted-result': '采纳的结果', conversation: '对话' }

function exportable(s: TrainingSample, confirmedOnly: boolean) {
  return s.state === 'included' && !s.stale && (!confirmedOnly || s.epistemic === 'confirmed')
}


function TrainingTab() {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [confirmedOnly, setConfirmedOnly] = useState(true)
  const ready = state.samples.filter((s) => exportable(s, confirmedOnly))
  return (
    <div className="stack">
      <Sheet pad>
        <div className="spread" style={{ flexWrap: 'wrap' }}>
          <div className="row-nowrap">
            <Switch label="只导出已确认的内容" checked={confirmedOnly} onChange={setConfirmedOnly} />
            <span>只导出已确认的内容</span>
          </div>
          <div className="row-nowrap">
            <span className="small muted">{ready.length} 条可以导出</span>
            <Button variant="primary" size="sm" icon={<Download size={14} />} disabled={!ready.length} onClick={() => void downloadExport(true, confirmedOnly).catch((e: Error) => toast.show(e.message))}>
              导出 JSONL
            </Button>
          </div>
        </div>
      </Sheet>
      <Sheet>
        {state.samples.length === 0 ? (
          <Empty>还没有样本。采纳一次交接结果，或者纠正一条记忆，就会有了。</Empty>
        ) : (
          <div className="list">
            {state.samples.map((s) => (
              <div key={s.id} className="item">
                <div className="grow stack-sm" style={{ gap: 6 }}>
                  <div className="row">
                    <Tag tone={sampleStateLabel[s.state].tone}>{sampleStateLabel[s.state].text}</Tag>
                    <Tag>{sampleKind[s.kind]}</Tag>
                    <TrustTag value={s.epistemic} />
                    {s.stale && <Tag tone="danger">来源变了</Tag>}
                  </div>
                  <div className="ink">问：{s.prompt}</div>
                  <div className="quote pre">{s.response}</div>
                  <div className="meta" style={{ marginTop: 0 }}>
                    <span>{s.origin.label}</span>
                    <span>第 {s.version} 版</span>
                    <span>{formatAgo(s.createdAt, state.settings.timezone ?? 'UTC')}</span>
                  </div>
                </div>
                <div className="stack-sm" style={{ flex: 'none' }}>
                  {s.state !== 'included' && (
                    <Button size="sm" disabled={s.stale} onClick={() => dispatch({ type: 'setSampleState', id: s.id, state: 'included' })}>
                      纳入
                    </Button>
                  )}
                  {s.state !== 'excluded' && (
                    <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'setSampleState', id: s.id, state: 'excluded' })}>
                      排除
                    </Button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
      </Sheet>
    </div>
  )
}

export function LibraryPage() {
  const { state } = useStore()
  const [params, setParams] = useSearchParams()
  const tab = (params.get('tab') as Tab) || 'memory'
  const failed = state.jobs.filter((j) => j.status === 'failed').length
  const pending = state.candidates.filter((c) => c.state === 'pending').length
  // The sheet's open state lives in the URL, so the settings page can link straight to it.
  const sorting = params.has('pending')
  const setSorting = (on: boolean) =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      if (on) next.set('pending', '1')
      else next.delete('pending')
      return next
    }, { replace: true })

  return (
    <main className="page page-narrow">
      <div className="page-head">
        <div>
          <h1>资料库</h1>
          <p>系统记住的东西、资料的来处，和攒下的训练数据。都归你，可以看、改、删、导出。</p>
        </div>
      </div>
      {pending > 0 && (
        <div className="sheet" style={{ marginBottom: 18 }}>
          <div className="setting">
            <div>
              <div className="ink">待确认内容 · {pending} 条</div>
              <div className="small muted">从资料里读到，但拿不准是待办、想法还是记忆。</div>
            </div>
            <Button size="sm" variant="primary" onClick={() => setSorting(true)}>
              逐条处理
            </Button>
          </div>
        </div>
      )}
      <div style={{ marginBottom: 18 }}>
        <Seg
          label="资料库"
          value={tab}
          onChange={(v) => setParams(v === 'memory' ? {} : { tab: v })}
          items={[
            { value: 'memory', label: '记忆', count: state.memoryTotal ?? state.memories.length },
            { value: 'sources', label: failed ? `来源 · ${failed} 项出错` : '来源' },
            { value: 'training', label: '训练数据', count: state.samples.length },
          ]}
        />
      </div>
      {tab === 'memory' && <MemoryTab />}
      {tab === 'sources' && <SourcesTab />}
      {tab === 'training' && <TrainingTab />}
      {sorting && <UnsureSheet onClose={() => setSorting(false)} />}
    </main>
  )
}
