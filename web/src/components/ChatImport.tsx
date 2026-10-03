import { useEffect, useRef, useState } from 'react'
import { CalendarDays, CircleAlert, FileText, Pause, Play, RotateCw, Sparkles, Trash2 } from 'lucide-react'
import { useStore } from '../store/context'
import { ConfirmModal } from './Overlay'
import { Button, Progress, Spinner, Tag } from './ui'
import { ImportProblem, sendArchive, type ArchivePreview, type ImportBatch, type Imports, type Organize } from './useImports'

const count = (n: number) => n.toLocaleString('zh-CN')

function megabytes(bytes: number): string {
  return bytes < 1024 * 1024 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / 1024 / 1024).toFixed(bytes < 10 * 1024 * 1024 ? 1 : 0)} MB`
}

/** "2023年2月11日 至 2026年9月30日", in the workspace zone; one date when both fall on the same day. */
function Span({ from, to }: { from?: string; to?: string }) {
  const { state } = useStore()
  const day = (iso?: string) => (iso && !Number.isNaN(new Date(iso).getTime()) ? new Date(iso).toLocaleDateString('zh-CN', { timeZone: state.settings.timezone ?? 'UTC', year: 'numeric', month: 'long', day: 'numeric' }) : '')
  const a = day(from)
  const b = day(to)
  if (!a && !b) return null
  return <>{a && b && a !== b ? `${a} 至 ${b}` : a || b}</>
}

const organizeChoices: { value: Organize; title: string; detail: string }[] = [
  { value: 'later', title: '先存着，以后再整理', detail: '现在不调用 AI' },
  { value: 'now', title: '现在就整理', detail: '存好后由 AI 在后台慢慢整理' },
]

/** The one choice before an import: store only, or also go through it for memories. */
function OrganizeChoice({ value, onChange }: { value: Organize; onChange: (v: Organize) => void }) {
  return (
    <div className="imp-choice" role="radiogroup" aria-label="什么时候整理">
      {organizeChoices.map((c) => (
        <button key={c.value} type="button" role="radio" aria-checked={value === c.value} className={value === c.value ? 'on' : ''} onClick={() => onChange(c.value)}>
          <i aria-hidden />
          <span>
            <strong>{c.title}</strong>
            <small>{c.detail}</small>
          </span>
        </button>
      ))}
    </div>
  )
}

type Step =
  | { at: 'reading'; sent: number; total: number }
  | { at: 'preview'; preview: ArchivePreview }
  | { at: 'starting'; preview: ArchivePreview; sent: number; total: number }
  | { at: 'failed'; problem: string; again: boolean; preview?: ArchivePreview }

/**
 * A chat export on its way in: it is read first, so the user sees how much it
 * holds and what would be new, and nothing is stored until they say so.
 */
export function ChatImportFlow({ file, onCancel, onStarted }: { file: File; onCancel: () => void; onStarted: () => void }) {
  const [step, setStep] = useState<Step>({ at: 'reading', sent: 0, total: file.size })
  const [attempt, setAttempt] = useState(0)
  const [organize, setOrganize] = useState<Organize>('later')
  const sending = useRef<AbortController | null>(null)

  useEffect(() => {
    const control = new AbortController()
    sendArchive<Partial<ArchivePreview>>('/v1/connectors/archive/preview', file, '读取这个文件', (sent, total) => setStep({ at: 'reading', sent, total }), control.signal)
      .then((p) => setStep({ at: 'preview', preview: { name: p.name ?? file.name, conversations: p.conversations ?? 0, messages: p.messages ?? 0, fromUser: p.fromUser ?? 0, earliest: p.earliest, latest: p.latest, alreadyImported: p.alreadyImported ?? 0, leftOut: p.leftOut ?? 0, blocked: p.blocked ?? 0, gaps: p.gaps ?? [] } }))
      .catch((e: unknown) => { if (!control.signal.aborted) setStep({ at: 'failed', again: worthRetrying(e), problem: e instanceof Error ? e.message : '读取这个文件没有完成。' }) })
    return () => control.abort()
  }, [file, attempt])
  useEffect(() => () => sending.current?.abort(), [])

  const start = (preview: ArchivePreview) => {
    const control = new AbortController()
    sending.current = control
    setStep({ at: 'starting', preview, sent: 0, total: file.size })
    sendArchive('/v1/connectors/archive', file, '导入', (sent, total) => setStep({ at: 'starting', preview, sent, total }), control.signal, { organize })
      .then(onStarted)
      .catch((e: unknown) => { if (!control.signal.aborted) setStep({ at: 'failed', again: worthRetrying(e), preview, problem: e instanceof Error ? e.message : '导入没有完成。' }) })
  }

  const preview = step.at === 'reading' ? undefined : step.preview
  // What a confirmed import would add: not what is here already, not what is beyond the limit, not what the user said never to bring back.
  const fresh = preview ? Math.max(0, preview.messages - preview.alreadyImported - preview.leftOut - preview.blocked) : 0
  const share = (n: number) => (preview && preview.messages > 0 ? `${(n / preview.messages) * 100}%` : '0%')
  const sent = step.at === 'reading' || step.at === 'starting' ? step : undefined

  return (
    <div className="imp-flow">
      <div className="file-chip">
        <span className="file-icon">
          <FileText size={16} />
        </span>
        <div className="grow">
          <div className="ellipsis ink">{file.name}</div>
          <div className="tiny muted">{megabytes(file.size)}</div>
        </div>
      </div>

      {step.at === 'reading' && (
        <div className="imp-wait" role="status">
          <Progress value={step.total ? step.sent / step.total : 0} />
          <span className="small muted">
            {step.sent < step.total ? `正在上传 ${Math.floor((step.sent / step.total) * 100)}% · ${megabytes(step.sent)} / ${megabytes(step.total)}` : <><Spinner size={12} /> 传完了，正在数里面有多少对话…</>}
          </span>
        </div>
      )}

      {preview && (
        <div className="imp-preview">
          <div className="imp-stats">
            <div>
              <strong>{count(preview.conversations)}</strong>
              <span>段对话</span>
            </div>
            <div>
              <strong>{count(preview.messages)}</strong>
              <span>条消息</span>
            </div>
            <div>
              <strong>{count(preview.fromUser)}</strong>
              <span>条是你说的</span>
            </div>
          </div>
          {(preview.earliest || preview.latest) && (
            <p className="imp-span">
              <CalendarDays size={13} />
              <Span from={preview.earliest} to={preview.latest} />
            </p>
          )}
          {preview.messages > 0 && (
            <>
              <div className="imp-split" aria-hidden>
                <i className="new" style={{ width: share(fresh) }} />
                <i className="had" style={{ width: share(preview.alreadyImported) }} />
                <i className="out" style={{ width: share(preview.leftOut) }} />
              </div>
              <ul className="imp-legend">
                <li className="new">这次会导入 {count(fresh)} 条</li>
                {preview.alreadyImported > 0 && <li className="had">以前导过 {count(preview.alreadyImported)} 条，不会重复存</li>}
                {preview.leftOut > 0 && <li className="out">太多了放不下 {count(preview.leftOut)} 条，留下的是最新的</li>}
              </ul>
            </>
          )}
          {preview.gaps.length > 0 && (
            <ul className="imp-gaps">
              {preview.gaps.map((gap, i) => (
                <li key={i}>{gap}</li>
              ))}
            </ul>
          )}
        </div>
      )}

      {step.at === 'starting' && (
        <div className="imp-wait" role="status">
          <Progress value={step.total ? step.sent / step.total : 0} />
          <span className="small muted">
            {step.sent < step.total ? `正在上传 ${Math.floor((step.sent / step.total) * 100)}% · ${megabytes(step.sent)} / ${megabytes(step.total)}` : <><Spinner size={12} /> 传完了，正在开始导入…</>}
          </span>
        </div>
      )}

      {step.at === 'failed' && (
        <p className="form-error" role="alert">
          <CircleAlert size={14} />
          {step.problem}
        </p>
      )}

      {step.at === 'preview' && fresh > 0 && <OrganizeChoice value={organize} onChange={setOrganize} />}

      {step.at === 'preview' && <p className="small muted">{fresh > 0 ? '现在还什么都没存。两种都是存好就能问到里面的话；整理成带人、地点、时间的记忆可以以后再做。' : preview?.blocked ? '这份里没有新的可以导入：不是以前导过，就是你说过不要再导入的。' : '这份里的消息以前都导过了，不用再导。'}</p>}

      <div className="row">
        {step.at === 'preview' && fresh > 0 && (
          <Button variant="primary" onClick={() => start(step.preview)}>
            导入 {count(fresh)} 条
          </Button>
        )}
        {step.at === 'failed' && step.again && (
          <Button icon={<RotateCw size={13} />} onClick={() => (step.preview ? start(step.preview) : (setStep({ at: 'reading', sent: 0, total: file.size }), setAttempt((n) => n + 1)))}>
            再试一次
          </Button>
        )}
        {/* Once the whole file is with the server the import is under way; it is stopped from its row, not from here. */}
        {!(step.at === 'starting' && step.sent >= step.total) && (
          <Button variant="quiet" onClick={onCancel}>
            {step.at === 'failed' ? '换一个文件' : step.at === 'preview' && fresh === 0 ? '好' : sent ? '取消上传' : '取消'}
          </Button>
        )}
      </div>
    </div>
  )
}

/** Trying the same file again only helps when the file was not the trouble: no connection, a server fault, a lapsed session. */
const worthRetrying = (e: unknown) => !(e instanceof ImportProblem) || e.status === 0 || e.status >= 500 || e.status === 401 || e.status === 429

const stateTag: Record<ImportBatch['state'], { text: string; tone: 'info' | 'warning' | 'success' | 'danger' }> = {
  importing: { text: '正在导入', tone: 'info' },
  paused: { text: '已暂停', tone: 'warning' },
  done: { text: '已存好', tone: 'success' },
  failed: { text: '中途出错', tone: 'danger' },
}

/** One of the two counts of an import, as a bar. */
function Meter({ label, value, total, kind }: { label: string; value: number; total: number; kind: 'stored' | 'organized' }) {
  return (
    <div className={`imp-meter ${kind}`}>
      <span className="imp-meter-label">{label}</span>
      <Progress value={total > 0 ? Math.min(1, value / total) : 0} />
      {/* As wide as the count can get, so both bars of an import end at the same place. */}
      <span className="imp-meter-count" style={{ minWidth: `${count(total).length * 2 + 3}ch` }}>
        {count(value)} / {count(total)}
      </span>
    </div>
  )
}

function ImportRow({ batch, imports }: { batch: ImportBatch; imports: Imports }) {
  const { refresh } = useStore()
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState('')
  const [deleting, setDeleting] = useState(false)
  const run = async (work: () => Promise<void>) => {
    setBusy(true)
    setProblem('')
    try {
      await work()
    } catch (e) {
      setProblem(e instanceof Error ? e.message : '没有完成，请重试。')
      // The import may have moved on meanwhile; show where it is now.
      imports.reload()
    } finally {
      setBusy(false)
    }
  }
  const finished = batch.state === 'done' && batch.organized >= batch.total
  // Stored only, by the user's choice: there is no second bar to watch until they start it.
  const held = batch.organizeLater === true && !finished
  const waiting = held && batch.state === 'done'
  return (
    <div className="imp-item">
      <div className="imp-head">
        <strong className="ellipsis">{batch.name}</strong>
        <Tag tone={stateTag[batch.state].tone}>{finished ? '已完成' : stateTag[batch.state].text}</Tag>
      </div>
      <div className="tiny muted">
        <Span from={batch.earliest} to={batch.latest} />
        {(batch.earliest || batch.latest) && ' · '}
        {count(batch.total)} 条消息
      </div>
      {finished ? (
        <p className="small muted">都存好了，也整理完了。</p>
      ) : waiting ? (
        <p className="small">已存好，还没开始整理。里面的话现在就能问到。</p>
      ) : (
        <>
          <Meter kind="stored" label="已存好" value={batch.stored} total={batch.total} />
          {held ? <p className="tiny muted">存好的话现在就能问到。存完之后先不整理，等你点「开始整理」。</p> : <Meter kind="organized" label="已整理" value={batch.organized} total={batch.total} />}
        </>
      )}
      {batch.leftOut > 0 && <p className="tiny muted">另有 {count(batch.leftOut)} 条太多了没有导入，留下的是最新的。</p>}
      {batch.state === 'failed' && (
        <p className="form-error" role="alert">
          <CircleAlert size={14} />
          {batch.error || `导入中途停了（${batch.errorCode || '原因没有记录'}）。`}
        </p>
      )}
      {batch.state === 'failed' && <p className="tiny muted">已经存好的还在，可以接着导。</p>}
      {problem && (
        <p className="form-error" role="alert">
          <CircleAlert size={14} />
          {problem}
        </p>
      )}
      <div className="row">
        {waiting && (
          <Button size="sm" variant="primary" icon={<Sparkles size={13} />} disabled={busy} onClick={() => void run(() => imports.organize(batch))}>
            开始整理
          </Button>
        )}
        {batch.state === 'importing' && (
          <Button size="sm" icon={<Pause size={13} />} disabled={busy} onClick={() => void run(() => imports.pause(batch))}>
            暂停
          </Button>
        )}
        {(batch.state === 'paused' || batch.state === 'failed') && (
          <Button size="sm" variant={batch.state === 'failed' ? 'primary' : 'default'} icon={<Play size={13} />} disabled={busy} onClick={() => void run(() => imports.resume(batch))}>
            {batch.state === 'failed' ? '接着导' : '继续'}
          </Button>
        )}
        <Button size="sm" variant="danger" icon={<Trash2 size={13} />} disabled={busy} onClick={() => setDeleting(true)}>
          删掉这次导入
        </Button>
      </div>
      {deleting && (
        <ConfirmModal
          title="删掉这次导入？"
          onClose={() => setDeleting(false)}
          onConfirm={async () => {
            setDeleting(false)
            await run(async () => {
              await imports.remove(batch)
              await refresh()
            })
          }}
        >
          <p className="muted">
            会删掉「{batch.name}」这次导入的全部消息（已存好 {count(batch.stored)} 条），以及从它们整理出的记忆。删掉之后就问不到这些话了。
          </p>
        </ConfirmModal>
      )}
    </div>
  )
}

/** Every chat import with how far it has got. Draws nothing while there are none. */
export function ImportList({ imports, title }: { imports: Imports; title?: string }) {
  if (!imports.items?.length && !imports.problem) return null
  return (
    <div className="imp-list">
      {title && Boolean(imports.items?.length) && <h3 className="sheet-subtitle">{title}</h3>}
      {/* Said once for the whole list: the two bars count two different things. */}
      {imports.items?.some((b) => !(b.state === 'done' && b.organized >= b.total) && !b.organizeLater) && <p className="small muted imp-note">存好的话现在就能问到；整理成记忆在后台慢慢做，不用等。</p>}
      {imports.items?.map((batch) => <ImportRow key={batch.id} batch={batch} imports={imports} />)}
      {imports.problem && (
        <p className="small muted" role="status">
          {imports.problem}{' '}
          <button type="button" className="link-btn" onClick={imports.reload}>
            再读一次
          </button>
        </p>
      )}
    </div>
  )
}
