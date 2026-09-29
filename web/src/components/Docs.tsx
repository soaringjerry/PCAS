import { useState } from 'react'
import { Bot, Eye, FilePlus, FileText, PenLine, Trash2 } from 'lucide-react'
import { newId } from '../domain/ids'
import { formatAgo, nowIso } from '../domain/time'
import type { Doc } from '../domain/types'
import { useStore } from '../store/context'
import { Markdown } from './Markdown'
import { Button, Tag } from './ui'

function DocCard({ doc, open, onToggle }: { doc: Doc; open: boolean; onToggle: () => void }) {
  const { dispatch } = useStore()
  const [preview, setPreview] = useState(doc.body.length > 0)
  return (
    <div className="doc-card">
      <div className="doc-card-head" onClick={onToggle}>
        <FileText size={14} className="muted" />
        <span className="grow ink ellipsis" style={{ fontWeight: 500 }}>
          {doc.title}
        </span>
        {doc.by === 'ai' && (
          <Tag>
            <Bot size={11} /> AI 起草
          </Tag>
        )}
        <span className="tiny faint">{formatAgo(doc.updatedAt)}</span>
      </div>
      {open && (
        <div className="doc-edit">
          <div className="doc-edit-bar">
            <input value={doc.title} onChange={(e) => dispatch({ type: 'updateDoc', id: doc.id, patch: { title: e.target.value } })} aria-label="文档标题" />
            <Button size="sm" variant="quiet" icon={preview ? <PenLine size={12} /> : <Eye size={12} />} onClick={() => setPreview((v) => !v)}>
              {preview ? '编辑' : '预览'}
            </Button>
            <Button
              size="sm"
              variant="quiet"
              icon={<Trash2 size={12} />}
              aria-label="删除文档"
              onClick={() => window.confirm(`删除「${doc.title}」？`) && dispatch({ type: 'deleteDoc', id: doc.id })}
            />
          </div>
          {preview ? (
            <div className="md-pad" onDoubleClick={() => setPreview(false)} title="双击编辑">
              {doc.body ? <Markdown text={doc.body} /> : <p className="muted">空文档。</p>}
            </div>
          ) : (
            <textarea
              className="doc-body"
              value={doc.body}
              placeholder="用 Markdown 写…"
              onChange={(e) => dispatch({ type: 'updateDoc', id: doc.id, patch: { body: e.target.value } })}
              aria-label="文档内容"
              autoFocus
            />
          )}
        </div>
      )}
    </div>
  )
}

export function DocsBlock({ thingId }: { thingId: string }) {
  const { state, dispatch } = useStore()
  const docs = state.docs.filter((d) => d.thingId === thingId)
  const [open, setOpen] = useState<string | null>(null)

  return (
    <section className="block">
      <div className="block-head">
        <h2>文档</h2>
        <span className="n">{docs.length}</span>
        <div className="tools">
          <Button
            size="sm"
            variant="quiet"
            icon={<FilePlus size={13} />}
            onClick={() => {
              const at = nowIso()
              const id = newId('d')
              dispatch({ type: 'createDoc', doc: { id, thingId, title: '未命名文档', body: '', by: 'user', createdAt: at, updatedAt: at } })
              setOpen(id)
            }}
          >
            新建
          </Button>
        </div>
      </div>
      {docs.length === 0 && <p className="small muted" style={{ padding: '4px 4px 0' }}>还没有文档。自己写一份，或者让 AI 起草后“存成文档”。</p>}
      {docs.map((d) => (
        <DocCard key={d.id} doc={d} open={open === d.id} onToggle={() => setOpen((o) => (o === d.id ? null : d.id))} />
      ))}
    </section>
  )
}
