import { useState } from 'react'
import { Link } from 'react-router'
import { findThing, thingTitle } from '../domain/things'
import type { Evidence, Handover, Sentence } from '../domain/studio'
import { formatAgo, formatShortDate } from '../domain/time'
import { useStore } from '../store/context'
import { readHandover, useRead } from '../store/studio'
import { SourceSheet } from './SourceSheet'

// The status block is the project's handover: three parts the model wrote, each
// sentence opening onto what it rests on. Nothing here edits the text. A wrong
// sentence is put right by telling the secretary, who changes the memory or the
// item under it; that makes the handover stale and it is written again.

/** Where a document version or a piece of assistant work is shown: on its thing's page. */
function placeOf(thingId: string | undefined, fallback: string, query: string): string {
  return `/t/${thingId ?? fallback}?${query}`
}

function EvidenceRow({ projectId, evidence }: { projectId: string; evidence: Evidence }) {
  const { state } = useStore()
  const [source, setSource] = useState(false)
  const timezone = state.settings.timezone ?? 'UTC'
  if (evidence.kind === 'memory') {
    const when = evidence.at ? formatShortDate(evidence.at, timezone) : ''
    const body = (
      <>
        <span className="ev-kind">记忆</span>
        <span className="ev-text">{evidence.text}</span>
        {when && <time className="ev-when">{when}</time>}
      </>
    )
    return evidence.sourceId ? (
      <li>
        {source && <SourceSheet id={evidence.sourceId} version={evidence.sourceVersion} conversation={{ excerpt: evidence.excerpt }} onClose={() => setSource(false)} />}
        <button type="button" title="看原话" onClick={() => setSource(true)}>
          {body}
        </button>
      </li>
    ) : (
      <li>
        <div>{body}</div>
      </li>
    )
  }
  if (evidence.kind === 'item') {
    const thing = findThing(state, evidence.id)
    const title = evidence.title ?? (thing ? thingTitle(thing) : '')
    // An item that is gone can still be named, but there is nowhere to go.
    return (
      <li>
        {thing ? (
          <Link to={`/t/${evidence.id}`}>
            <span className="ev-kind">事项</span>
            <span className="ev-text">{title}</span>
          </Link>
        ) : (
          <div>
            <span className="ev-kind">事项</span>
            <span className="ev-text">{title || '这件事已经不在了'}</span>
          </div>
        )}
      </li>
    )
  }
  if (evidence.kind === 'document') {
    const doc = state.docs.find((d) => d.id === evidence.id)
    return (
      <li>
        <Link to={placeOf(evidence.thingId ?? doc?.thingId, projectId, `doc=${encodeURIComponent(evidence.id)}&v=${evidence.version}`)}>
          <span className="ev-kind">文档</span>
          <span className="ev-text">{evidence.title ?? doc?.title ?? '文档'}</span>
          <span className="ev-when">第 {evidence.version} 版</span>
        </Link>
      </li>
    )
  }
  const run = state.runs.find((r) => r.id === evidence.id)
  return (
    <li>
      <Link to={placeOf(evidence.thingId ?? run?.thingId, projectId, `run=${encodeURIComponent(evidence.id)}`)}>
        <span className="ev-kind">副手</span>
        <span className="ev-text">{evidence.prompt ?? run?.prompt ?? '副手的工作'}</span>
      </Link>
    </li>
  )
}

function SentenceRow({ projectId, sentence }: { projectId: string; sentence: Sentence }) {
  const [open, setOpen] = useState(false)
  return (
    <li className={open ? 'open' : undefined}>
      <button type="button" className="status-sentence" aria-expanded={open} title="点开看依据" onClick={() => setOpen((v) => !v)}>
        {sentence.text}
      </button>
      {open && (
        <ul className="status-evidence" aria-label="依据">
          {sentence.evidence.map((e, i) => (
            <EvidenceRow key={`${e.kind}-${e.id}-${i}`} projectId={projectId} evidence={e} />
          ))}
        </ul>
      )}
    </li>
  )
}

const PARTS = [
  { key: 'conclusion', label: '结论' },
  { key: 'blockers', label: '卡点' },
  { key: 'nextSteps', label: '下一步' },
] as const

function Parts({ projectId, handover }: { projectId: string; handover: Handover }) {
  return (
    <div className="status-parts">
      {PARTS.map(({ key, label }) => (
        <div key={key} className="status-part" role="group" aria-label={label}>
          <div className="status-part-label">{label}</div>
          {handover[key].length > 0 ? (
            <ul className="status-sentences">
              {handover[key].map((s, i) => (
                <SentenceRow key={i} projectId={projectId} sentence={s} />
              ))}
            </ul>
          ) : (
            <p className="status-none">这一段没有内容</p>
          )}
        </div>
      ))}
    </div>
  )
}

export function StatusBlock({ projectId }: { projectId: string }) {
  const { state } = useStore()
  // The snapshot's revision moves when anything the handover is written from does.
  const [read, retry] = useRead(projectId, state.revision, () => readHandover(projectId))
  const handover = read.phase === 'ready' ? read.value : undefined

  return (
    <section className="section status-block" aria-label="现状">
      <div className="section-label">
        现状
        {handover && <span className="faint"> · 写于 {formatAgo(handover.writtenAt, state.settings.timezone ?? 'UTC')}</span>}
      </div>
      {read.phase === 'failed' && (
        <p className="status-note" role="alert">
          现状没读出来：{read.problem}
          <button type="button" className="act-btn" onClick={retry}>
            再读一次
          </button>
        </p>
      )}
      {read.phase === 'ready' && !handover && <p className="status-note">还没有现状。</p>}
      {handover?.stale && (
        <p className="status-note" role="status">
          正在更新，下面是上一份
        </p>
      )}
      {handover && <Parts projectId={projectId} handover={handover} />}
    </section>
  )
}
