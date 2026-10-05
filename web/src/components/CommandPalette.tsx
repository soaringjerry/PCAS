import { useMemo, useState, type ReactNode } from 'react'
import { useMatch, useNavigate } from 'react-router'
import { BookOpen, FolderPlus, House, Lightbulb, ListPlus, PenLine, Settings, UserRound } from 'lucide-react'
import { newId } from '../domain/ids'
import { allThings, findThing, thingProjectId, thingTitle } from '../domain/things'
import { useStore } from '../store/context'
import { useMemorySearch } from '../store/memories'
import { useToast } from '../store/toast'
import { KindLabel } from './Marks'

interface Entry {
  key: string
  group: string
  icon?: ReactNode
  label: ReactNode
  text: string
  hint?: string
  run: () => void
}

/** One box, Spotlight-style: find, create, or go. */
export function CommandPalette({ onClose }: { onClose: () => void }) {
  const { state, dispatch } = useStore()
  const navigate = useNavigate()
  const toast = useToast()
  const match = useMatch('/t/:id')
  const current = findThing(state, match?.params.id ?? '')
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const q = query.trim()
  // The snapshot holds only the latest memories; an older one is found by asking for it.
  const found = useMemorySearch(q, 3)

  const entries = useMemo<Entry[]>(() => {
    const done = (fn: () => void | Promise<void>) => async () => {
      await fn()
      onClose()
    }
    const go = (to: string) => done(() => navigate(to))

    const nav: Entry[] = [
      { key: 'home', group: '前往', icon: <House size={16} />, label: '大厅', text: '大厅 首页 home', run: go('/') },
      { key: 'lib', group: '前往', icon: <BookOpen size={16} />, label: '资料库', text: '资料库 记忆 来源 训练', run: go('/library') },
      { key: 'about', group: '前往', icon: <UserRound size={16} />, label: '关于你', text: '关于你 现状 期限 交接', run: go('/about') },
      { key: 'set', group: '前往', icon: <Settings size={16} />, label: '设置', text: '设置 额度 AI', run: go('/settings') },
    ]

    if (!q) return nav

    const projectId = current ? (current.kind === 'project' ? current.id : thingProjectId(current)) : undefined
    const create: Entry[] = [
      {
        key: 'capture',
        group: '新建',
        icon: <PenLine size={16} />,
        label: <>记下“{q}”</>,
        text: q,
        hint: '后台会整理',
        run: done(async () => {
          if (!await dispatch({ type: 'capture', text: q })) return
          toast.show('记下了')
        }),
      },
      {
        key: 'task',
        group: '新建',
        icon: <ListPlus size={16} />,
        label: `待办「${q}」`,
        text: q,
        run: done(async () => {
          if (!await dispatch({ type: 'addTask', title: q, projectId })) return
          toast.show('加好了')
        }),
      },
      {
        key: 'idea',
        group: '新建',
        icon: <Lightbulb size={16} />,
        label: `想法「${q}」`,
        text: q,
        run: done(async () => {
          const id = newId()
          if (!await dispatch({ type: 'addIdea', id, title: q, projectId })) return
          navigate(`/t/${id}`)
        }),
      },
      {
        key: 'project',
        group: '新建',
        icon: <FolderPlus size={16} />,
        label: `项目「${q}」`,
        text: q,
        run: done(async () => {
          const id = newId()
          if (!await dispatch({ type: 'addProject', id, name: q })) return
          navigate(`/t/${id}`)
        }),
      },
    ]
    const things = allThings(state)
      .filter((t) => thingTitle(t).includes(q))
      .slice(0, 6)
      .map((t) => ({ key: t.id, group: '事情', icon: <KindLabel kind={t.kind} bare />, label: thingTitle(t), text: '', run: go(`/t/${t.id}`) }))
    const memories = found
      .map((m) => ({ key: m.id, group: '记忆', label: m.text, text: '', run: go(`/library?m=${m.id}`) }))
    const commands = nav.filter((c) => c.text.toLowerCase().includes(q.toLowerCase()))
    return [...things, ...commands, ...create, ...memories]
  }, [q, state, found, current, dispatch, navigate, onClose, toast])

  const selected = Math.min(active, entries.length - 1)

  return (
    <>
      <div className="scrim palette-scrim" onClick={onClose} />
      <div className="palette" role="dialog" aria-modal="true" aria-label="搜索">
        <div className="palette-input">
          <PenLine size={18} className="faint" />
          <input
            autoFocus
            value={query}
            placeholder="搜索，或者直接写一句记下来"
            aria-label="搜索"
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
                entries[selected]?.run()
              }
            }}
          />
        </div>
        <div className="palette-list" role="listbox">
          {entries.map((entry, i) => (
            <div key={entry.key}>
              {(i === 0 || entries[i - 1].group !== entry.group) && <div className="palette-group">{entry.group}</div>}
              <button
                type="button"
                role="option"
                aria-selected={i === selected}
                className={`palette-item${i === selected ? ' on' : ''}`}
                onMouseEnter={() => setActive(i)}
                onClick={entry.run}
              >
                {entry.icon}
                <span className="grow ellipsis">{entry.label}</span>
                {entry.hint && <span className="hint">{entry.hint}</span>}
              </button>
            </div>
          ))}
        </div>
      </div>
    </>
  )
}
