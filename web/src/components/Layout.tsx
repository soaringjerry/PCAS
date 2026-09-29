import { useEffect, useRef, useState, type ReactNode } from 'react'
import { NavLink, Outlet } from 'react-router'
import {
  Bot,
  Brain,
  CalendarCheck,
  Database,
  FlaskConical,
  FolderKanban,
  Inbox,
  Lightbulb,
  ListTodo,
  PenLine,
  RotateCcw,
  Send,
} from 'lucide-react'
import { useStore } from '../store/context'
import { useDetail } from '../store/hooks'
import { IdeaDrawer } from './IdeaDrawer'
import { TaskDrawer } from './TaskDrawer'

interface NavItem {
  to: string
  label: string
  icon: ReactNode
  count?: number
}

function BrandMark() {
  return (
    <svg className="brand-mark" viewBox="0 0 32 32" aria-hidden="true">
      <rect width="32" height="32" rx="8" fill="var(--accent)" />
      <circle cx="16" cy="16" r="6.5" fill="none" stroke="#fff" strokeWidth="2.5" />
      <circle cx="16" cy="16" r="2" fill="#fff" />
    </svg>
  )
}

function Capture() {
  const { dispatch } = useStore()
  const [text, setText] = useState('')
  const [saved, setSaved] = useState(false)
  const input = useRef<HTMLInputElement>(null)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement
      if (e.key === '/' && !['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)) {
        e.preventDefault()
        input.current?.focus()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    if (!saved) return
    const t = window.setTimeout(() => setSaved(false), 2200)
    return () => window.clearTimeout(t)
  }, [saved])

  return (
    <form
      className="capture"
      onSubmit={(e) => {
        e.preventDefault()
        if (!text.trim()) return
        dispatch({ type: 'capture', text: text.trim() })
        setText('')
        setSaved(true)
      }}
    >
      <PenLine size={16} />
      <input
        ref={input}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="一句话先记下，之后再整理…"
        aria-label="快速记录"
      />
      {saved ? <span className="capture-note">已放进收件箱</span> : <kbd>/</kbd>}
    </form>
  )
}

function NavLinks({ items }: { items: NavItem[] }) {
  return (
    <>
      {items.map((item) => (
        <NavLink key={item.to} to={item.to} end={item.to === '/'} className={({ isActive }) => `nav-link${isActive ? ' active' : ''}`}>
          {item.icon}
          <span>{item.label}</span>
          {!!item.count && <span className="count">{item.count}</span>}
        </NavLink>
      ))}
    </>
  )
}

export function Layout() {
  const { state, dispatch } = useStore()
  const { taskId, ideaId } = useDetail()
  const task = state.tasks.find((t) => t.id === taskId)
  const idea = state.ideas.find((i) => i.id === ideaId)

  const pending = state.candidates.filter((c) => c.state === 'pending').length
  const awakened = state.ideas.filter((i) => i.status === 'awakened').length
  const size = 17

  const groups: { label: string; items: NavItem[] }[] = [
    {
      label: '工作',
      items: [
        { to: '/', label: '今天', icon: <CalendarCheck size={size} /> },
        { to: '/inbox', label: '收件箱', icon: <Inbox size={size} />, count: pending },
        { to: '/tasks', label: '任务', icon: <ListTodo size={size} /> },
        { to: '/ideas', label: 'IDEA', icon: <Lightbulb size={size} />, count: awakened },
        { to: '/projects', label: '项目', icon: <FolderKanban size={size} /> },
      ],
    },
    {
      label: '记忆与协作',
      items: [
        { to: '/memory', label: '记忆', icon: <Brain size={size} /> },
        { to: '/handoffs', label: '交接', icon: <Send size={size} /> },
        { to: '/agents', label: 'AI 接入', icon: <Bot size={size} /> },
      ],
    },
    {
      label: '数据',
      items: [
        { to: '/sources', label: '来源与作业', icon: <Database size={size} /> },
        { to: '/training', label: '训练数据', icon: <FlaskConical size={size} /> },
      ],
    },
  ]

  return (
    <div className="shell">
      <nav className="sidebar" aria-label="主导航">
        <div className="brand">
          <BrandMark />
          <div>
            PCAS
            <small>个人长期记忆</small>
          </div>
        </div>
        {groups.map((group) => (
          <div key={group.label} className="nav-group">
            <div className="nav-label">{group.label}</div>
            <NavLinks items={group.items} />
          </div>
        ))}
        <div className="sidebar-foot">
          <p>交互原型 · 数据只保存在本机浏览器</p>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            style={{ marginTop: 6, paddingLeft: 0 }}
            onClick={() => window.confirm('恢复到初始示例数据？') && dispatch({ type: 'reset' })}
          >
            <RotateCcw size={13} /> 重置示例数据
          </button>
        </div>
      </nav>

      <div className="main">
        <header className="topbar">
          <div className="brand">
            <BrandMark />
            PCAS
          </div>
          <Capture />
          <nav className="mobile-nav" aria-label="主导航">
            <NavLinks items={groups.flatMap((g) => g.items)} />
          </nav>
        </header>
        <Outlet />
      </div>

      {task && <TaskDrawer key={task.id} task={task} />}
      {idea && <IdeaDrawer key={idea.id} idea={idea} />}
    </div>
  )
}
