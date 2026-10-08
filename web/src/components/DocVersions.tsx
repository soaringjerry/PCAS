import { useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { authorText, defaultPair, foldVersions, pick, type DocVersion, type ParagraphChange, type VersionGroup } from '../domain/studio'
import { formatAgo } from '../domain/time'
import type { Doc } from '../domain/types'
import { useStore } from '../store/context'
import { readDiff, readVersionBody, readVersions, useRead } from '../store/studio'
import { Markdown } from './Markdown'

// A document's versions. Every save is one; what one author saved in a row is
// one line that opens onto all of them. Two picked versions are compared, one
// is read. Nothing is edited here: the document itself is edited when this is
// closed, and that writes the next version.

const MARK = { add: '增', delete: '删', change: '改' } as const

function Change({ change }: { change: ParagraphChange }) {
  return (
    <div className={`diff-block ${change.kind}`}>
      <span className="diff-mark" aria-label={MARK[change.kind]}>
        {MARK[change.kind]}
      </span>
      <div className="diff-text">
        {change.kind !== 'add' && (
          <del>
            <Markdown text={change.before} />
          </del>
        )}
        {change.kind !== 'delete' && (
          <ins>
            <Markdown text={change.after} />
          </ins>
        )}
      </div>
    </div>
  )
}

/** Only the paragraphs that changed, in the document's order. */
function Comparison({ docId, from, to }: { docId: string; from: number; to: number }) {
  const [read, retry] = useRead(`${docId}:${from}:${to}`, 0, () => readDiff(docId, from, to))
  if (read.phase === 'loading') return <p className="ver-note">正在比较…</p>
  if (read.phase === 'failed') {
    return (
      <p className="ver-note" role="alert">
        差异没读出来：{read.problem}
        <button type="button" className="act-btn" onClick={retry}>
          再读一次
        </button>
      </p>
    )
  }
  if (read.value.changes.length === 0) return <p className="ver-note">这两版没有差别</p>
  return (
    <div className="diff" aria-label={`第 ${from} 版到第 ${to} 版的差异`}>
      {read.value.changes.map((c, i) => (
        <Change key={i} change={c} />
      ))}
    </div>
  )
}

function OneVersion({ docId, version }: { docId: string; version: number }) {
  const [read, retry] = useRead(`${docId}:${version}`, 0, () => readVersionBody(docId, version))
  if (read.phase === 'loading') return <p className="ver-note">正在读第 {version} 版…</p>
  if (read.phase === 'failed') {
    return (
      <p className="ver-note" role="alert">
        第 {version} 版没读出来：{read.problem}
        <button type="button" className="act-btn" onClick={retry}>
          再读一次
        </button>
      </p>
    )
  }
  return (
    <div className="diff" aria-label={`第 ${version} 版的正文`}>
      <Markdown text={read.value || '（空）'} />
    </div>
  )
}

function VersionRow({ doc, version, label, picked, onPick, children }: { doc: Doc; version: DocVersion; label: string; picked: boolean; onPick: () => void; children?: ReactNode }) {
  const { state } = useStore()
  return (
    <li className={`ver-row${picked ? ' picked' : ''}`}>
      <button type="button" className="ver-pick" aria-pressed={picked} onClick={onPick}>
        <span className="ver-no">{label}</span>
        <span className="ver-by">{authorText[version.author] ?? '你'}</span>
        <span className="ver-when">{formatAgo(version.writtenAt, state.settings.timezone ?? 'UTC')}</span>
        {version.basedOn != null && version.basedOn !== version.version - 1 && <span className="ver-base">基于第 {version.basedOn} 版</span>}
      </button>
      {version.runId && (
        <Link className="act-btn" to={`/t/${doc.thingId}?run=${encodeURIComponent(version.runId)}`}>
          看这次工作
        </Link>
      )}
      {children}
    </li>
  )
}

function Group({ doc, group, picked, onPick }: { doc: Doc; group: VersionGroup; picked: number[]; onPick: (version: number) => void }) {
  const [newest, ...older] = group.versions
  // A version picked from elsewhere may sit inside the fold; then the fold starts open.
  const [open, setOpen] = useState(() => older.some((v) => picked.includes(v.version)))
  if (older.length === 0) return <VersionRow doc={doc} version={newest} label={`第 ${newest.version} 版`} picked={picked.includes(newest.version)} onPick={() => onPick(newest.version)} />
  return (
    <>
      <VersionRow doc={doc} version={newest} label={open ? `第 ${newest.version} 版` : `第 ${older.at(-1)!.version}–${newest.version} 版`} picked={picked.includes(newest.version)} onPick={() => onPick(newest.version)}>
        <button type="button" className="act-btn" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          {open ? '收起' : `连着改了 ${group.versions.length} 次`}
        </button>
      </VersionRow>
      {open && older.map((v) => <VersionRow key={v.version} doc={doc} version={v} label={`第 ${v.version} 版`} picked={picked.includes(v.version)} onPick={() => onPick(v.version)} />)}
    </>
  )
}

/** What is picked when the list opens: the asked-for version against the one it was written on, else the latest against the one before. */
function firstPick(versions: DocVersion[], asked?: number): number[] {
  const at = asked === undefined ? undefined : versions.find((v) => v.version === asked)
  if (!at) return defaultPair(versions)
  const before = at.basedOn ?? at.version - 1
  return versions.some((v) => v.version === before) ? [before, at.version] : [at.version]
}

function Loaded({ doc, versions, asked }: { doc: Doc; versions: DocVersion[]; asked?: number }) {
  const [picked, setPicked] = useState(() => firstPick(versions, asked))
  const [from, to] = [...picked].sort((a, b) => a - b)
  return (
    <>
      <ol className="ver-list" aria-label={`「${doc.title}」的版本`}>
        {foldVersions(versions).map((g) => (
          <Group key={g.versions[0].version} doc={doc} group={g} picked={picked} onPick={(v) => setPicked((p) => pick(p, v))} />
        ))}
      </ol>
      {picked.length === 2 ? (
        <>
          <p className="ver-note">
            第 {from} 版 → 第 {to} 版
          </p>
          <Comparison docId={doc.id} from={from} to={to} />
        </>
      ) : picked.length === 1 ? (
        <>
          <p className="ver-note">第 {from} 版 · 再点一版看差异</p>
          <OneVersion docId={doc.id} version={from} />
        </>
      ) : (
        <p className="ver-note">点两版看差异</p>
      )}
    </>
  )
}

export function DocVersions({ doc, asked }: { doc: Doc; asked?: number }) {
  // A new version shows up in the list as soon as the document's own record moves.
  const [read, retry] = useRead(doc.id, new Date(doc.updatedAt).getTime(), () => readVersions(doc.id))
  if (read.phase === 'loading') return <p className="ver-note">正在读版本…</p>
  if (read.phase === 'failed') {
    return (
      <p className="ver-note" role="alert">
        版本没读出来：{read.problem}
        <button type="button" className="act-btn" onClick={retry}>
          再读一次
        </button>
      </p>
    )
  }
  if (read.value.length === 0) return <p className="ver-note">还没有版本记录</p>
  return <Loaded key={asked} doc={doc} versions={read.value} asked={asked} />
}
