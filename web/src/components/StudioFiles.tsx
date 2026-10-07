import { useRef, useState, type DragEvent } from 'react'
import { Download, File as FileIcon, Plus, X } from 'lucide-react'
import { checkFile, fileSize, opensInPage, type ProjectFile } from '../domain/studio'
import { useStore } from '../store/context'
import { deleteFile, readFiles, uploadFile, useRead } from '../store/studio'
import { useToast } from '../store/toast'
import { ConfirmModal, SideSheet } from './Overlay'

// The files kept with a project: dropped in or chosen, listed with what became
// of them, opened in the page when the browser can show them, and removed only
// after a confirmation. A file is a source like any other, so what is read out
// of it becomes memory and reaches the status block by itself.

/** What became of a file, in the user's words. */
function statusText(file: ProjectFile): string {
  if (file.status === 'failed') return `没读成：${file.failureReason || '原因没有记下来'}`
  return file.status === 'extracted' ? '已读过' : '已存好，还没读'
}

function Viewer({ file, onClose }: { file: ProjectFile; onClose: () => void }) {
  return (
    <SideSheet title={file.name} onClose={onClose}>
      {opensInPage(file) === 'image' ? <img className="file-view" src={file.openUrl} alt={file.name} /> : <iframe className="file-view frame" src={file.openUrl} title={file.name} />}
    </SideSheet>
  )
}

function FileRow({ itemId, file, onChanged }: { itemId: string; file: ProjectFile; onChanged: () => void }) {
  const { dispatch } = useStore()
  const toast = useToast()
  const [viewing, setViewing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [problem, setProblem] = useState('')
  const inPage = opensInPage(file)
  const name = (
    <>
      <FileIcon size={15} aria-hidden />
      <span className="file-name">{file.name}</span>
    </>
  )
  return (
    <li className="file-row">
      <div className="file-line">
        {inPage ? (
          <button type="button" className="file-open" title="打开" onClick={() => setViewing(true)}>
            {name}
          </button>
        ) : (
          // The browser cannot show it, so opening it is saving it.
          <a className="file-open" href={file.openUrl} download={file.name} title="下载">
            {name}
            <Download size={13} aria-hidden />
          </a>
        )}
        <span className={`file-status${file.status === 'failed' ? ' failed' : ''}`}>{statusText(file)}</span>
        {file.status === 'failed' && file.jobId && (
          <button
            type="button"
            className="act-btn"
            onClick={async () => {
              if (await dispatch({ type: 'retryJob', id: file.jobId! })) onChanged()
            }}
          >
            重试
          </button>
        )}
        <span className="file-size">{fileSize(file.size)}</span>
        <button type="button" className="act-btn icon" aria-label={`删除文件：${file.name}`} title="删除" onClick={() => setDeleting(true)}>
          <X size={14} />
        </button>
      </div>
      {problem && (
        <p className="file-problem" role="alert">
          {problem}
        </p>
      )}
      {viewing && <Viewer file={file} onClose={() => setViewing(false)} />}
      {deleting && (
        <ConfirmModal
          title={`删除「${file.name}」？`}
          onClose={() => setDeleting(false)}
          onConfirm={async () => {
            setDeleting(false)
            try {
              await deleteFile(itemId, file.sourceId)
              setProblem('')
              toast.show(`删掉了「${file.name}」`)
              onChanged()
            } catch (e) {
              setProblem(`没删掉：${e instanceof Error ? e.message : '网络连接中断'}`)
            }
          }}
        >
          <p>原件和从它读出来的内容会一起删掉，删了找不回来；同一份文件以后也不会再收进来。</p>
        </ConfirmModal>
      )}
    </li>
  )
}

export function StudioFiles({ itemId }: { itemId: string }) {
  const { state, refresh } = useStore()
  const toast = useToast()
  const [read, retry] = useRead(itemId, state.revision, () => readFiles(itemId))
  const [sending, setSending] = useState<string[]>([])
  const [problems, setProblems] = useState<string[]>([])
  const [over, setOver] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  const files = read.phase === 'ready' ? read.value : []
  const changed = () => {
    retry()
    void refresh()
  }

  /** Files go one after another; one that fails does not stop the rest, and each failure is named. */
  const send = async (chosen: File[]) => {
    if (chosen.length === 0) return
    const failed: string[] = []
    const already: string[] = []
    setProblems([])
    for (const file of chosen) {
      const refused = checkFile(file)
      if (refused) {
        failed.push(`${refused}，没有存`)
        continue
      }
      setSending((names) => [...names, file.name])
      try {
        if ((await uploadFile(itemId, file)).alreadyExists) already.push(file.name)
      } catch (e) {
        failed.push(`「${file.name}」没存上：${e instanceof Error ? e.message : '网络连接中断'}`)
      } finally {
        setSending((names) => names.filter((n) => n !== file.name))
      }
    }
    setProblems(failed)
    if (already.length > 0) toast.show(`已经有了：${already.join('、')}`)
    changed()
  }

  const drop = (e: DragEvent) => {
    e.preventDefault()
    setOver(false)
    void send(Array.from(e.dataTransfer.files))
  }

  return (
    <section
      className={`section files${over ? ' over' : ''}`}
      aria-label="文件"
      onDragOver={(e) => {
        if (!e.dataTransfer.types.includes('Files')) return
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOver(false)
      }}
      onDrop={drop}
    >
      <div className="section-label">文件</div>
      {read.phase === 'failed' && (
        <p className="status-note" role="alert">
          文件没读出来：{read.problem}
          <button type="button" className="act-btn" onClick={retry}>
            再读一次
          </button>
        </p>
      )}
      <div className="group">
        {files.length > 0 && (
          <ul className="file-list">
            {files.map((f) => (
              <FileRow key={f.sourceId} itemId={itemId} file={f} onChanged={changed} />
            ))}
          </ul>
        )}
        {sending.map((name) => (
          <p key={name} className="file-sending" role="status">
            正在存「{name}」…
          </p>
        ))}
        {problems.map((p) => (
          <p key={p} className="file-problem" role="alert">
            {p}
          </p>
        ))}
        <button type="button" className="doc-new file-add" onClick={() => input.current?.click()}>
          <Plus size={14} aria-hidden />
          {over ? '松手就存进来' : '把文件拖到这里，或者点这里选'}
        </button>
        <input
          ref={input}
          type="file"
          multiple
          className="visually-hidden"
          aria-label="选择文件"
          tabIndex={-1}
          onChange={(e) => {
            void send(Array.from(e.currentTarget.files ?? []))
            e.currentTarget.value = ''
          }}
        />
      </div>
    </section>
  )
}
