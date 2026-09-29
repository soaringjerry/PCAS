import { useState } from 'react'
import { useStore } from '../store/context'
import { SideSheet } from './Overlay'
import { Button } from './ui'

export function ImportSheet({ onClose }: { onClose: () => void }) {
  const { importText, importAttachment } = useStore()
  const [file, setFile] = useState<File | null>(null)
  const [title, setTitle] = useState('导入资料')
  const [text, setText] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  return <SideSheet title="导入资料" onClose={onClose}><form className="stack" onSubmit={async (e) => {
    e.preventDefault(); setBusy(true)
    try { if (await (file ? importAttachment(file) : importText(title, text))) onClose() } finally { setBusy(false) }
  }}>
    <label>标题<input value={title} onChange={(e) => setTitle(e.target.value)} required /></label>
    <label>选择文件<input type="file" accept=".txt,.md,.markdown,.json,.csv,.pdf,.png,.jpg,.jpeg,.webp,.tiff,.mp3,.wav,.m4a" onChange={async (e) => {
      const file = e.target.files?.[0]; if (!file) return
      const binary = !/\.(txt|md|markdown|json|csv)$/i.test(file.name)
      if (binary) { if (file.size > 20 * 1024 * 1024) { setError('附件上限为 20 MB。'); return }; setFile(file); setTitle(file.name); setText(''); setError(''); return }
      setFile(null)
      if (file.size > 1024 * 1024) { setError('单次文本导入上限为 1 MB，请拆分后导入。'); return }
      setTitle(file.name); setText(await file.text()); setError('')
    }} /></label>
    <label>或粘贴原文<textarea rows={14} value={text} onChange={(e) => { setText(e.target.value); setFile(null) }} required={!file} /></label>
    <p className="small muted">原件先保存，再进行解析和索引。PDF 和图片可在后台识别；音频使用所配置的转录服务。缺少解析器或模型时，会保留原件并标明处理缺口。</p>
    {error && <p role="alert">{error}</p>}
    <Button type="submit" variant="primary" disabled={busy || !title.trim() || (!file && !text.trim())}>{busy ? '保存中…' : '导入'}</Button>
  </form></SideSheet>
}
