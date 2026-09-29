import { useState } from 'react'
import { Check, Copy, FileText, ListChecks, RotateCcw, Trash2, TriangleAlert } from 'lucide-react'
import { parseChecklist } from '../domain/agent'
import { runStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import type { Run } from '../domain/types'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'
import { Markdown } from './Markdown'
import { Button, Tag } from './ui'

const adoptedText = { doc: '存成了文档', subtasks: '拆成了子任务', progress: '写回了进度' } as const

export function RunCard({ run, focus }: { run: Run; focus?: boolean }) {
  const { state, dispatch, runAgent } = useStore()
  const toast = useToast()
  const [showBrief, setShowBrief] = useState(false)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(run.output ?? '')
  const [pasted, setPasted] = useState('')
  const [copied, setCopied] = useState(false)
  const agent = state.agents.find((a) => a.id === run.agentId)
  const text = editing ? draft : (run.output ?? '')
  const checklist = parseChecklist(text)

  const adopt = (as: 'doc' | 'subtasks' | 'progress') => {
    dispatch({ type: 'adoptRun', id: run.id, as, text })
    setEditing(false)
    toast.show(as === 'doc' ? '存成文档了' : as === 'subtasks' ? `加了 ${checklist.length} 个子任务` : '写回进度了')
  }

  return (
    <div id={`run-${run.id}`} className={`run${run.adopted ? ' adopted' : ''}`} style={focus ? { borderColor: 'var(--accent)' } : undefined}>
      <div className="run-head">
        <span className="agent-chip">
          <span className="agent-dot">{agent?.name.slice(0, 1)}</span>
          {agent?.name}
        </span>
        <span className="grow ellipsis muted">{run.prompt}</span>
        {run.adopted ? <Tag tone="success">{adoptedText[run.adopted.as]}</Tag> : <Tag tone={runStatusLabel[run.status].tone}>{runStatusLabel[run.status].text}</Tag>}
        <span className="tiny faint">{formatAgo(run.createdAt)}</span>
      </div>

      {run.staleContext && !run.adopted && (
        <div className="row small" style={{ padding: '6px 12px', background: 'var(--amber-soft)', color: 'var(--amber)' }}>
          <TriangleAlert size={13} /> 它用到的记忆后来改过或删了，结果可能过时。
        </div>
      )}

      {run.status === 'running' && (
        <div className="run-body">
          <div className="skeleton" aria-label={`${agent?.name} 正在工作`}>
            <i style={{ width: '62%' }} />
            <i style={{ width: '88%' }} />
            <i style={{ width: '74%' }} />
          </div>
        </div>
      )}

      {run.status === 'waiting' && (
        <div className="run-body stack-sm">
          <p className="small muted">把下面的内容复制给任意 AI，再把它的回复贴回来。</p>
          <pre className="brief" style={{ borderTop: 0, borderRadius: 6 }}>
            {run.brief}
          </pre>
          <div className="row">
            <Button
              size="sm"
              icon={copied ? <Check size={12} /> : <Copy size={12} />}
              onClick={() => {
                navigator.clipboard?.writeText(run.brief).catch(() => undefined)
                setCopied(true)
                window.setTimeout(() => setCopied(false), 1500)
              }}
            >
              {copied ? '复制了' : '复制内容'}
            </Button>
          </div>
          <textarea value={pasted} onChange={(e) => setPasted(e.target.value)} placeholder="把回复贴在这里…" aria-label="贴回结果" style={{ minHeight: 100 }} />
          <div>
            <Button size="sm" variant="primary" disabled={!pasted.trim()} onClick={() => dispatch({ type: 'pasteRunResult', id: run.id, output: pasted.trim() })}>
              贴回结果
            </Button>
          </div>
        </div>
      )}

      {run.status === 'done' && (
        <div className="run-body">
          {editing ? (
            <textarea value={draft} onChange={(e) => setDraft(e.target.value)} aria-label="修改结果" autoFocus />
          ) : (
            <Markdown text={run.output ?? ''} />
          )}
        </div>
      )}

      {showBrief && run.status !== 'waiting' && <pre className="brief">{run.brief}</pre>}

      <div className="run-foot">
        {run.status === 'done' && !run.adopted && (
          <>
            {checklist.length > 0 && (
              <Button size="sm" variant="primary" icon={<ListChecks size={13} />} onClick={() => adopt('subtasks')}>
                加成 {checklist.length} 个子任务
              </Button>
            )}
            <Button size="sm" variant={checklist.length ? 'default' : 'primary'} icon={<FileText size={13} />} onClick={() => adopt('doc')}>
              存成文档
            </Button>
            <Button size="sm" onClick={() => adopt('progress')}>
              写回进度
            </Button>
            <Button
              size="sm"
              variant="quiet"
              onClick={() => {
                setDraft(run.output ?? '')
                setEditing((v) => !v)
              }}
            >
              {editing ? '取消修改' : '改一下'}
            </Button>
            <Button
              size="sm"
              variant="quiet"
              icon={<RotateCcw size={12} />}
              onClick={() => runAgent({ thingId: run.thingId, agentId: run.agentId, kind: run.kind, prompt: run.prompt })}
            >
              重来
            </Button>
          </>
        )}
        <span className="grow" />
        <button type="button" className="btn btn-quiet btn-sm" onClick={() => setShowBrief((v) => !v)}>
          {run.contextMemoryIds.length} 条记忆 · {showBrief ? '收起' : '看发了什么'}
        </button>
        {!run.adopted && run.status !== 'running' && (
          <Button size="sm" variant="quiet" icon={<Trash2 size={12} />} aria-label="丢掉" title="丢掉" onClick={() => dispatch({ type: 'discardRun', id: run.id })} />
        )}
      </div>
    </div>
  )
}
