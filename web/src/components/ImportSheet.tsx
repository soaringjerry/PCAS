import { useState } from 'react'
import { CircleAlert } from 'lucide-react'
import { useStore } from '../store/context'
import { ChatImportFlow, ImportList } from './ChatImport'
import { FileDrop } from './controls'
import { SideSheet } from './Overlay'
import { Button, Field, Spinner } from './ui'
import { isChatExport, useImports } from './useImports'

const accept = '.txt,.md,.markdown,.json,.csv,.pdf,.png,.jpg,.jpeg,.webp,.tiff,.mp3,.wav,.m4a,.zip'

export function ImportSheet({ onClose }: { onClose: () => void }) {
  const { importText, importAttachment } = useStore()
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState('导入资料')
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // A chat export takes its own path: read first, stored only once confirmed.
  const [chat, setChat] = useState<File | null>(null)
  const imports = useImports()

  const choose = async (f: File) => {
    if (isChatExport(f)) {
      setFile(null); setError(''); setChat(f)
      return
    }
    const binary = !/\.(txt|md|markdown|json|csv)$/i.test(f.name)
    if (binary) {
      if (f.size > 20 * 1024 * 1024) return setError('附件上限为 20 MB。')
      setFile(f); setTitle(f.name); setText(''); setError('')
      return
    }
    setFile(null)
    if (f.size > 1024 * 1024) return setError('单次文本导入上限为 1 MB，请拆分后导入。')
    setTitle(f.name); setText(await f.text()); setError('')
  }

  if (chat) return <SideSheet title="导入聊天记录" onClose={onClose}>
    <ChatImportFlow file={chat} onCancel={() => setChat(null)} onStarted={() => { setChat(null); imports.reload() }} />
  </SideSheet>

  return <SideSheet title="导入资料" onClose={onClose}><div className="stack">
    {/* Imports under way come first: whoever comes back to this sheet is looking for them. */}
    <ImportList imports={imports} title="聊天记录" />
    <form className="stack" onSubmit={async (e) => {
    e.preventDefault(); setBusy(true)
    try { if (await (file ? importAttachment(file) : importText(title, text))) onClose() } finally { setBusy(false) }
  }}>
    <Field label="文件" as="div" hint="文本、Markdown、JSON、CSV、PDF、图片或音频；ChatGPT 导出的聊天记录（zip）会先让你看看里面有多少">
      <FileDrop file={file} accept={accept} hint="文本 1 MB 以内，附件 20 MB 以内" onFile={(f) => void choose(f)} onClear={() => { setFile(null); setError('') }} />
    </Field>
    {!file && <>
      <div className="or-rule"><span>或者直接粘贴</span></div>
      <Field label="标题"><input className="input" value={title} onChange={(e) => setTitle(e.target.value)} required /></Field>
      <Field label="原文"><textarea className="textarea" rows={12} value={text} onChange={(e) => setText(e.target.value)} placeholder="把要保存的内容贴在这里" required /></Field>
    </>}
    <p className="note">原件先保存，再进行解析和索引。PDF 和图片可在后台识别；音频使用所配置的转录服务。缺少解析器或模型时，会保留原件并标明处理缺口。</p>
    {error && <p className="form-error" role="alert"><CircleAlert size={14} />{error}</p>}
    <Button type="submit" variant="primary" className="btn-lg" disabled={busy || !title.trim() || (!file && !text.trim())}>{busy && <Spinner />}{busy ? '保存中…' : '导入'}</Button>
  </form></div></SideSheet>
}
