import { RecallSheet } from '../components/RecallSheet'
import { SourceSheet } from '../components/SourceSheet'
import { ImportSheet } from '../components/ImportSheet'
import { downloadExport } from '../store/api'
import { useState } from 'react'
import { useSearchParams } from 'react-router'
import { ChevronRight, Download, History, Info, RotateCw, Search, Trash2, Upload } from 'lucide-react'
import { Checkbox, Chip } from '../components/controls'
import { Fade, FromLine, ProjectLink, TrustTag } from '../components/Marks'
import { ConfirmModal, SideSheet } from '../components/Overlay'
import { Button, Empty, Progress, Seg, Sheet, Switch, Tag } from '../components/ui'
import { jobStatusLabel, memoryKindLabel, sampleStateLabel, sourceStatusLabel, triggerLabel } from '../domain/labels'
import { formatAgo, formatWhen } from '../domain/time'
import type { Epistemic, Memory, MemoryKind, TrainingSample } from '../domain/types'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'

type Tab = 'memory' | 'sources' | 'training'

const actor = { user: '你', ai: 'AI', import: '导入', system: '系统' } as const

function MemorySheet({ memory, onClose }: { memory: Memory; onClose: () => void }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const [text, setText] = useState(memory.text)
  const [reason, setReason] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [includeSources, setIncludeSources] = useState(false)
  const runs = state.runs.filter((r) => r.contextMemoryIds.includes(memory.id))
  const samples = state.samples.filter((s) => s.origin.memoryId === memory.id)
  const changed = text.trim() !== memory.text

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
            <span>{memory.exposure < 0.4 ? '很久没用到，已经变淡，但还在' : `最近用到：${formatAgo(memory.lastUsedAt)}`}</span>
          </div>
        </div>

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
                    {actor[v.by]} · {formatAgo(v.at)}
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

function MemoryTab() {
  const { state } = useStore()
  const [params, setParams] = useSearchParams()
  const [kind, setKind] = useState<MemoryKind | 'all'>('all')
  const [trust, setTrust] = useState<Epistemic | 'all'>('all')
  const [query, setQuery] = useState('')
  const [recalling, setRecalling] = useState(false)
  const open = state.memories.find((m) => m.id === params.get('m'))
  const shown = state.memories
    .filter((m) => kind === 'all' || m.kind === kind)
    .filter((m) => trust === 'all' || m.epistemic === trust)
    .filter((m) => !query.trim() || m.text.includes(query.trim()))
    .sort((a, b) => b.exposure - a.exposure)

  return (
    <>
      {recalling && <RecallSheet query={query} onClose={() => setRecalling(false)} />}
      <div className="toolbar">
        <label className="search">
          <Search size={15} />
          <input placeholder="筛选当前记忆…" value={query} onChange={(e) => setQuery(e.target.value)} aria-label="搜索记忆" />
        </label>
        <Button icon={<History size={14} />} onClick={() => setRecalling(true)}>
          深入查找
        </Button>
      </div>
      <div className="toolbar">
        <Seg
          label="类型"
          value={kind}
          onChange={setKind}
          items={[{ value: 'all', label: '全部' }, ...(Object.keys(memoryKindLabel) as MemoryKind[]).map((k) => ({ value: k, label: memoryKindLabel[k] }))]}
        />
        <Seg
          label="可信度"
          value={trust}
          onChange={setTrust}
          items={[
            { value: 'all', label: '都看' },
            { value: 'confirmed', label: '已确认' },
            { value: 'sourced', label: '原话有据' },
            { value: 'inferred', label: '推测' },
            { value: 'planned', label: '计划' },
          ]}
        />
      </div>
      <p className="hint-line">
        <Info size={13} />
        「原话有据」保留你刚记录的直接表达，不代表已经核实；波浪下划线是待确认的 AI 理解。长期不用的记忆会变淡；深入查找可翻历史和原文。
      </p>
      <Sheet>
        {shown.length === 0 ? (
          <Empty>没找到。</Empty>
        ) : (
          <div className="list">
            {shown.map((m) => (
              <div
                key={m.id}
                className="mem-entry"
                style={{ opacity: 0.5 + m.exposure * 0.5 }}
                onClick={() => setParams({ m: m.id })}
                role="button"
                tabIndex={0}
                onKeyDown={(e) => e.key === 'Enter' && setParams({ m: m.id })}
              >
                <div className="grow">
                  <div className={`text${m.epistemic === 'inferred' ? ' guess' : ''}`}>{m.text}</div>
                  <div className="meta">
                    <span>{memoryKindLabel[m.kind]}</span>
                    <TrustTag value={m.epistemic} />
                    {m.versions.length > 1 && <span>改过 {m.versions.length - 1} 次</span>}
                    <span>{m.visibleTo.length ? `${m.visibleTo.length} 个 AI 能看` : '只有你能看'}</span>
                    <ProjectLink id={m.projectId} />
                  </div>
                </div>
                <Fade value={m.exposure} />
              </div>
            ))}
          </div>
        )}
      </Sheet>
      {open && <MemorySheet key={open.id} memory={open} onClose={() => setParams({})} />}
    </>
  )
}

function SourcesTab() {
  const [importing, setImporting] = useState(false)
  const [sourceId, setSourceId] = useState<string | null>(null)
  const { state, dispatch } = useStore()
  const rank = { running: 0, failed: 1, waiting: 2, queued: 3, done: 4 }
  const jobs = [...state.jobs].sort((a, b) => rank[a.status] - rank[b.status])
  return (
    <div className="stack">
      <div className="spread">
        <p className="small muted">资料从哪来。各平台能用的导入方式还在逐个验证。</p>
        <Button variant="primary" icon={<Upload size={14} />} onClick={() => setImporting(true)}>
          导入资料
        </Button>
      </div>
      {sourceId && <SourceSheet id={sourceId} onClose={() => setSourceId(null)} />}
      {importing && <ImportSheet onClose={() => setImporting(false)} />}
      <div className="sources-grid">
        {state.sources.map((s) => (
          <button key={s.id} type="button" className="source-card" onClick={() => setSourceId(s.id)} aria-label={`查看原文：${s.name}`}>
            <div className="source-card-head">
              <span className="stamp">{s.name.slice(0, 1)}</span>
              <div className="grow">
                <div className="source-name">{s.name}</div>
                <div className="tiny muted">{s.method}</div>
              </div>
              <Tag tone={sourceStatusLabel[s.status].tone}>{sourceStatusLabel[s.status].text}</Tag>
            </div>
            {s.note && <p className="source-note">{s.note}</p>}
            <div className="source-card-foot">
              <span>{s.itemCount} 条</span>
              {s.lastSyncAt && <span>{formatAgo(s.lastSyncAt)}更新</span>}
              <span className="source-open">
                查看原文
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
                  {job.nextRunAt && job.status !== 'done' && <span>下次：{formatWhen(job.nextRunAt)}</span>}
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
                    <span>{formatAgo(s.createdAt)}</span>
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

  return (
    <main className="page page-narrow">
      <div className="page-head">
        <div>
          <h1>资料库</h1>
          <p>系统记住的东西、资料的来处，和攒下的训练数据。都归你，可以看、改、删、导出。</p>
        </div>
      </div>
      <div style={{ marginBottom: 18 }}>
        <Seg
          label="资料库"
          value={tab}
          onChange={(v) => setParams(v === 'memory' ? {} : { tab: v })}
          items={[
            { value: 'memory', label: '记忆', count: state.memories.length },
            { value: 'sources', label: failed ? `来源 · ${failed} 项出错` : '来源' },
            { value: 'training', label: '训练数据', count: state.samples.length },
          ]}
        />
      </div>
      {tab === 'memory' && <MemoryTab />}
      {tab === 'sources' && <SourcesTab />}
      {tab === 'training' && <TrainingTab />}
    </main>
  )
}
