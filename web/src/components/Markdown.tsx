import type { ReactNode } from 'react'
import { Check } from 'lucide-react'

// A deliberately small renderer for AI output and docs: headings, lists,
// checkboxes, bold and inline code. It builds React elements, so no HTML from
// a model is ever injected into the page.

function inline(text: string): ReactNode[] {
  const parts: ReactNode[] = []
  const re = /(\*\*[^*]+\*\*|`[^`]+`)/g
  let last = 0
  let m: RegExpExecArray | null
  while ((m = re.exec(text))) {
    if (m.index > last) parts.push(text.slice(last, m.index))
    const token = m[0]
    parts.push(token.startsWith('**') ? <strong key={m.index}>{token.slice(2, -2)}</strong> : <code key={m.index} className="mono">{token.slice(1, -1)}</code>)
    last = m.index + token.length
  }
  if (last < text.length) parts.push(text.slice(last))
  return parts
}

export function Markdown({ text }: { text: string }) {
  const blocks: ReactNode[] = []
  const lines = text.split('\n')
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    const heading = line.match(/^(#{1,3})\s+(.*)$/)
    if (heading) {
      const level = heading[1].length
      const content = inline(heading[2])
      blocks.push(level === 1 ? <h1 key={i}>{content}</h1> : level === 2 ? <h2 key={i}>{content}</h2> : <h3 key={i}>{content}</h3>)
      i += 1
      continue
    }
    if (/^\s*([-*]|\d+\.)\s+/.test(line)) {
      const ordered = /^\s*\d+\./.test(line)
      const items: ReactNode[] = []
      while (i < lines.length && /^\s*([-*]|\d+\.)\s+/.test(lines[i])) {
        const raw = lines[i].replace(/^\s*([-*]|\d+\.)\s+/, '')
        const task = raw.match(/^\[([ x])\]\s+(.*)$/)
        items.push(
          task ? (
            <li key={i} className="task-li">
              <span className={`checkbox-box${task[1] === 'x' ? ' on' : ''}`} role="img" aria-label={task[1] === 'x' ? '已完成' : '未完成'}>
                <Check size={11} strokeWidth={3.5} />
              </span>
              {inline(task[2])}
            </li>
          ) : (
            <li key={i}>{inline(raw)}</li>
          ),
        )
        i += 1
      }
      blocks.push(ordered ? <ol key={`l${i}`}>{items}</ol> : <ul key={`l${i}`}>{items}</ul>)
      continue
    }
    if (line.trim()) blocks.push(<p key={i}>{inline(line)}</p>)
    i += 1
  }
  return <div className="md">{blocks}</div>
}
