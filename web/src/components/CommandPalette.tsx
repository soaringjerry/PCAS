import { useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import { BookOpen, CalendarCheck, FolderPlus, Layers, PenLine, Settings } from 'lucide-react'
import { newId } from '../domain/ids'
import { allThings, thingTitle } from '../domain/things'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'
import { KindLabel } from './Marks'

interface Entry {
  key: string
  group: string
  icon?: ReactNode
  label: ReactNode
  hint?: string
  run: () => void
}

/** One box for everything: jot a line down, jump to a thing, or go somewhere. */
export function CommandPalette({ onClose }: { onClose: () => void }) {
  const { state, dispatch } = useStore()
  const navigate = useNavigate()
  const toast = useToast()
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const q = query.trim()

  const entries = useMemo<Entry[]>(() => {
    const go = (to: string) => () => {
      navigate(to)
      onClose()
    }
    if (!q) {
      return [
        { key: 'now', group: '去', icon: <CalendarCheck size={16} />, label: '现在', run: go('/') },
        { key: 'things', group: '去', icon: <Layers size={16} />, label: '事情', run: go('/things') },
        { key: 'lib', group: '去', icon: <BookOpen size={16} />, label: '资料库', run: go('/library') },
        { key: 'set', group: '去', icon: <Settings size={16} />, label: '设置', run: go('/settings') },
      ]
    }
    const list: Entry[] = [
      {
        key: 'capture',
        group: '记下',
        icon: <PenLine size={16} />,
        label: <span className="hand" style={{ fontSize: 16 }}>“{q}”</span>,
        hint: '先存着，之后整理',
        run: () => {
          dispatch({ type: 'capture', text: q })
          toast.show('记下了，等你有空再确认', { to: '/', label: '去看看' })
          onClose()
        },
      },
      {
        key: 'project',
        group: '记下',
        icon: <FolderPlus size={16} />,
        label: `新建项目「${q}」`,
        run: () => {
          const id = newId('p')
          dispatch({ type: 'addProject', id, name: q })
          navigate(`/t/${id}`)
          onClose()
        },
      },
    ]
    for (const thing of allThings(state).filter((t) => thingTitle(t).includes(q)).slice(0, 6)) {
      list.push({ key: thing.id, group: '事情', icon: <KindLabel kind={thing.kind} />, label: thingTitle(thing), run: go(`/t/${thing.id}`) })
    }
    for (const m of state.memories.filter((m) => m.text.includes(q)).slice(0, 4)) {
      list.push({ key: m.id, group: '记忆', label: m.text, run: go(`/library?m=${m.id}`) })
    }
    return list
  }, [q, state, dispatch, navigate, onClose, toast])

  const current = Math.min(active, entries.length - 1)

  return (
    <>
      <div className="scrim palette-scrim" onClick={onClose} />
      <div className="palette" role="dialog" aria-modal="true" aria-label="记一笔或查找">
        <div className="palette-input">
          <PenLine size={18} className="faint" />
          <input
            autoFocus
            value={query}
            placeholder="一句话先记下，或者找点什么…"
            aria-label="记一笔或查找"
            onChange={(e) => {
              setQuery(e.target.value)
              setActive(0)
            }}
            onKeyDown={(e) => {
              if (e.key === 'Escape') onClose()
              if (e.key === 'ArrowDown') {
                e.preventDefault()
                setActive((i) => Math.min(i + 1, entries.length - 1))
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault()
                setActive((i) => Math.max(i - 1, 0))
              }
              if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                e.preventDefault()
                entries[current]?.run()
              }
            }}
          />
          <kbd>Esc</kbd>
        </div>
        <div className="palette-list" role="listbox">
          {entries.map((entry, i) => (
            <div key={entry.key}>
              {(i === 0 || entries[i - 1].group !== entry.group) && <div className="palette-group">{entry.group}</div>}
              <button
                type="button"
                role="option"
                aria-selected={i === current}
                className={`palette-item${i === current ? ' on' : ''}`}
                onMouseEnter={() => setActive(i)}
                onClick={entry.run}
              >
                {entry.icon}
                <span className="grow" style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                  {entry.label}
                </span>
                {entry.hint && <span className="hint">{entry.hint}</span>}
              </button>
            </div>
          ))}
        </div>
        <div className="palette-foot">
          <span>↵ 确定</span>
          <span>↑↓ 选择</span>
          <span>记下的内容会先放进“等你确认”，点一下才算数</span>
        </div>
      </div>
    </>
  )
}
