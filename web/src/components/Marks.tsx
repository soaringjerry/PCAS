import { SourceSheet } from './SourceSheet'
import { useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, Check, CornerDownRight, FileText, Flag, PenLine, Send, Sparkles } from 'lucide-react'
import { kindText, type Thing, type TimelineEvent } from '../domain/things'
import { formatAgo } from '../domain/time'
import type { Epistemic, SourceRef } from '../domain/types'
import { useStore } from '../store/context'
import { Tag } from './ui'

/** Source-backed wording is distinct from both user review and AI inference. */
export function TrustTag({ value }: { value: Epistemic }) {
  if (value === 'sourced') return <Tag tone="info">原话有据</Tag>
  if (value === 'inferred') return <Tag tone="warning">推测</Tag>
  if (value === 'planned') return <Tag tone="info">计划</Tag>
  return null
}

export function KindLabel({ kind, bare = false }: { kind: Thing['kind']; bare?: boolean }) {
  return (
    <span className={`kind kind-${kind}`} title={kindText[kind]}>
      {bare ? null : kindText[kind]}
    </span>
  )
}

export function ProjectLink({ id }: { id?: string }) {
  const { state } = useStore()
  const project = state.projects.find((p) => p.id === id)
  if (!project) return null
  return (
    <Link to={`/t/${project.id}`} className="from" onClick={(e) => e.stopPropagation()}>
      {project.name}
    </Link>
  )
}

export function FromLine({ source, quote = true }: { source: SourceRef; quote?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="stack-sm" style={{ gap: 4 }}>
      <button type="button" className="from link-btn" onClick={() => setOpen(true)}>
        <CornerDownRight size={12} />
        {source.label} · {formatAgo(source.at)}
      </button>
      {open && <SourceSheet id={source.sourceId} version={source.version} onClose={() => setOpen(false)} />}
      {quote && source.excerpt && <div className="quote">“{source.excerpt}”</div>}
    </div>
  )
}

/** Exposure: how present a memory is. Fades with disuse, never deletes. */
export function Fade({ value }: { value: number }) {
  const bars = Math.max(1, Math.round(value * 5))
  return (
    <span className="fade" title={`曝光度 ${Math.round(value * 100)}%：长期不用会变淡，但不会被删除`}>
      {[0, 1, 2, 3, 4].map((i) => (
        <i key={i} className={i < bars ? 'on' : undefined} />
      ))}
    </span>
  )
}

const tlIcon = {
  note: <PenLine size={13} />,
  source: <FileText size={13} />,
  wake: <Sparkles size={13} />,
  handoff: <Send size={13} />,
  done: <Check size={13} />,
  decision: <Flag size={13} />,
}

export function Timeline({ events, limit = 8 }: { events: TimelineEvent[]; limit?: number }) {
  const [all, setAll] = useState(false)
  const hidden = all ? 0 : Math.max(0, events.length - limit)
  return (
    <>
      {hidden > 0 && (
        <button type="button" className="btn btn-quiet btn-sm" style={{ marginBottom: 10 }} onClick={() => setAll(true)}>
          更早的 {hidden} 条
        </button>
      )}
      <ol className="timeline">
        {events.slice(hidden).map((e, i) => (
          <li key={`${e.at}-${i}`}>
            <span className={`tl-dot ${e.kind}`}>{tlIcon[e.kind]}</span>
            <div className="stack-sm" style={{ gap: 4 }}>
              <div className="ink">
                {e.link ? (
                  <Link to={e.link}>
                    {e.text} <ArrowUpRight size={12} />
                  </Link>
                ) : (
                  e.text
                )}
              </div>
              {e.source?.excerpt && <div className="quote">“{e.source.excerpt}”</div>}
              <div className="tl-when">
                {e.by ? `${e.by} · ` : ''}
                {formatAgo(e.at)}
              </div>
            </div>
          </li>
        ))}
      </ol>
    </>
  )
}
