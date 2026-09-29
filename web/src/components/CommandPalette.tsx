import { useMemo, useState, type ReactNode } from 'react'
import { useMatch, useNavigate } from 'react-router'
import {
  BookOpen,
  Bot,
  CalendarDays,
  FilePlus,
  FileUp,
  FolderPlus,
  Inbox,
  Lightbulb,
  ListPlus,
  PanelLeft,
  PanelRight,
  PenLine,
  Settings,
  Sun,
} from 'lucide-react'
import { quickActions } from '../domain/agent'
import { newId } from '../domain/ids'
import { allThings, findThing, thingProjectId, thingTitle } from '../domain/things'
import { nowIso } from '../domain/time'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'
import { useWorkspace } from '../store/workspace'
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

export function CommandPalette({ onClose }: { onClose: () => void }) {
  const { state, dispatch, runAgent, runDemoImport } = useStore()
  const { ws, toggleSidebar, toggleContext } = useWorkspace()
  const navigate = useNavigate()
  const toast = useToast()
  const match = useMatch('/t/:id')
  const current = findThing(state, match?.params.id ?? '')
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const q = query.trim()

  const entries = useMemo<Entry[]>(() => {
    const done = (fn: () => void) => () => {
      fn()
      onClose()
    }
    const go = (to: string) => done(() => navigate(to))

    const commands: Entry[] = [
      { key: 'v-today', group: '跳转', icon: <Sun size={15} />, label: '今天', text: '今天 today', run: go('/today') },
      { key: 'v-inbox', group: '跳转', icon: <Inbox size={15} />, label: '收件', text: '收件 inbox', run: go('/inbox') },
      { key: 'v-up', group: '跳转', icon: <CalendarDays size={15} />, label: '接下来', text: '接下来 upcoming', run: go('/upcoming') },
      { key: 'v-ideas', group: '跳转', icon: <Lightbulb size={15} />, label: '想法', text: '想法 ideas', run: go('/ideas') },
      { key: 'v-lib', group: '跳转', icon: <BookOpen size={15} />, label: '资料库', text: '资料库 记忆 library', run: go('/library') },
      { key: 'v-set', group: '跳转', icon: <Settings size={15} />, label: '设置', text: '设置 settings', run: go('/settings') },
      { key: 'c-sb', group: '界面', icon: <PanelLeft size={15} />, label: ws.sidebar ? '收起侧栏' : '展开侧栏', text: '侧栏 sidebar', hint: '⌘\\', run: done(toggleSidebar) },
      { key: 'c-ctx', group: '界面', icon: <PanelRight size={15} />, label: ws.context ? '收起上下文' : '展开上下文', text: '上下文 context', hint: '⌘.', run: done(toggleContext) },
    ]
    if (!state.demo.costReportImported) {
      commands.push({ key: 'c-demo', group: '演示', icon: <FileUp size={15} />, label: '演示：导入一份成本测算', text: '演示 导入 demo', run: done(runDemoImport) })
    }

    const here: Entry[] = []
    if (current) {
      for (const a of quickActions[current.kind]) {
        here.push({
          key: `ai-${a.kind}`,
          group: `对「${thingTitle(current)}」`,
          icon: <Bot size={15} />,
          label: `让 Claude ${a.label}`,
          text: `${a.label} ai claude`,
          run: done(() => runAgent({ thingId: current.id, agentId: ws.agentFor[current.id] ?? 'a_claude', kind: a.kind, prompt: a.prompt })),
        })
      }
      here.push({
        key: 'doc',
        group: `对「${thingTitle(current)}」`,
        icon: <FilePlus size={15} />,
        label: '新建文档',
        text: '新建文档 doc',
        run: done(() => {
          const at = nowIso()
          dispatch({ type: 'createDoc', doc: { id: newId('d'), thingId: current.id, title: '未命名文档', body: '', by: 'user', createdAt: at, updatedAt: at } })
        }),
      })
    }

    if (!q) {
      const recent = ws.tabs
        .map((p) => p.match(/^\/t\/(.+)$/)?.[1])
        .map((id) => (id ? findThing(state, id) : undefined))
        .filter((t) => t && t.id !== current?.id)
        .slice(-5)
        .reverse()
        .map((t) => ({ key: `r-${t!.id}`, group: '打开过的', icon: <KindLabel kind={t!.kind} bare />, label: thingTitle(t!), text: '', run: go(`/t/${t!.id}`) }))
      return [...here, ...recent, ...commands]
    }

    const projectId = current ? (current.kind === 'project' ? current.id : thingProjectId(current)) : undefined
    const projectName = state.projects.find((p) => p.id === projectId)?.name
    const create: Entry[] = [
      {
        key: 'capture',
        group: '新建',
        icon: <PenLine size={15} />,
        label: <>记下“{q}”</>,
        text: q,
        hint: '先放进收件',
        run: done(() => {
          dispatch({ type: 'capture', text: q })
          toast.show('记下了，在收件里等你确认', { to: '/inbox', label: '去看' })
        }),
      },
      {
        key: 'task',
        group: '新建',
        icon: <ListPlus size={15} />,
        label: <>新建待办「{q}」{projectName && <span className="muted">· {projectName}</span>}</>,
        text: q,
        run: done(() => {
          dispatch({ type: 'addTask', title: q, projectId })
          toast.show('建好了')
        }),
      },
      {
        key: 'idea',
        group: '新建',
        icon: <Lightbulb size={15} />,
        label: `新建想法「${q}」`,
        text: q,
        run: done(() => {
          const id = newId('i')
          dispatch({ type: 'addIdea', id, title: q, projectId })
          navigate(`/t/${id}`)
        }),
      },
      {
        key: 'project',
        group: '新建',
        icon: <FolderPlus size={15} />,
        label: `新建项目「${q}」`,
        text: q,
        run: done(() => {
          const id = newId('p')
          dispatch({ type: 'addProject', id, name: q })
          navigate(`/t/${id}`)
        }),
      },
    ]
    const things = allThings(state)
      .filter((t) => thingTitle(t).includes(q))
      .slice(0, 6)
      .map((t) => ({ key: t.id, group: '事情', icon: <KindLabel kind={t.kind} bare />, label: thingTitle(t), text: '', run: go(`/t/${t.id}`) }))
    const memories = state.memories
      .filter((m) => m.text.includes(q))
      .slice(0, 3)
      .map((m) => ({ key: m.id, group: '记忆', label: m.text, text: '', run: go(`/library?m=${m.id}`) }))
    const matching = [...here, ...commands].filter((c) => c.text.toLowerCase().includes(q.toLowerCase()) || String(c.label).includes(q))
    return [...things, ...matching, ...create, ...memories]
  }, [q, state, current, ws, dispatch, navigate, onClose, toast, runAgent, runDemoImport, toggleSidebar, toggleContext])

  const selected = Math.min(active, entries.length - 1)

  return (
    <>
      <div className="scrim palette-scrim" onClick={onClose} />
      <div className="palette" role="dialog" aria-modal="true" aria-label="命令">
        <div className="palette-input">
          <PenLine size={16} className="faint" />
          <input
            autoFocus
            value={query}
            placeholder="搜索事情、输入命令，或者直接写一句记下来…"
            aria-label="命令"
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
          <kbd>Esc</kbd>
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
        <div className="palette-foot">
          <span>
            <kbd>↵</kbd> 执行
          </span>
          <span>
            <kbd>↑</kbd>
            <kbd>↓</kbd> 选择
          </span>
          <span>在一件事里打开，可以直接让 AI 动手</span>
        </div>
      </div>
    </>
  )
}
