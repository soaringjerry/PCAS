import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, NavLink, Outlet } from 'react-router'
import { BookOpen, CalendarCheck, Layers, Plus, Settings, Sparkles } from 'lucide-react'
import { attentionFor, isDecision } from '../domain/attention'
import { jobStatusLabel } from '../domain/labels'
import { useStore } from '../store/context'
import { ToastContext, type ToastApi } from '../store/toast'
import { CommandPalette } from './CommandPalette'
import { Progress, Tag } from './ui'

function Mark() {
  return (
    <svg width="26" height="26" viewBox="0 0 32 32" aria-hidden="true">
      <rect x="3" y="4" width="24" height="25" rx="4" fill="var(--page)" stroke="var(--ink)" strokeWidth="1.8" />
      <path d="M9 11h12M9 16h12M9 21h7" stroke="var(--rule-strong)" strokeWidth="1.6" strokeLinecap="round" />
      <path d="M23 3.5v9l2.5-2 2.5 2v-9z" fill="var(--accent)" />
    </svg>
  )
}

function Activity() {
  const { state } = useStore()
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const busy = state.jobs.filter((j) => j.status === 'running' || j.status === 'queued')
  const failed = state.jobs.filter((j) => j.status === 'failed')
  const shown = state.jobs.filter((j) => j.status !== 'done')

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    return () => window.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <div className="activity" ref={box}>
      <button type="button" className="activity-btn" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <span className={`pulse${failed.length ? ' bad' : ''}`} />
        {failed.length ? `${failed.length} 项出错` : busy.length ? `后台 ${busy.length} 项` : '后台空闲'}
      </button>
      {open && (
        <div className="popover">
          <div className="palette-group">后台在做的事（不是你的待办）</div>
          {shown.map((job) => (
            <div key={job.id} className="stack-sm" style={{ gap: 4, padding: '8px 12px' }}>
              <div className="spread">
                <span className="ink small">{job.title}</span>
                <Tag tone={jobStatusLabel[job.status].tone}>{jobStatusLabel[job.status].text}</Tag>
              </div>
              <span className="tiny muted">{job.detail}</span>
              {job.status === 'running' && job.progress !== undefined && <Progress value={job.progress} />}
            </div>
          ))}
          <div style={{ padding: '6px 12px 4px' }}>
            <Link to="/library?tab=sources" className="small" onClick={() => setOpen(false)}>
              全部来源与作业
            </Link>
          </div>
        </div>
      )}
    </div>
  )
}

export function Shell() {
  const { state } = useStore()
  const [palette, setPalette] = useState(false)
  const [toast, setToast] = useState<{ text: string; link?: { to: string; label: string }; key: number } | null>(null)
  const needs = attentionFor(state).filter(isDecision).length

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement
      const typing = ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable
      if ((e.key === 'k' && (e.metaKey || e.ctrlKey)) || (e.key === '/' && !typing)) {
        e.preventDefault()
        setPalette(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    if (!toast) return
    const t = window.setTimeout(() => setToast(null), 3200)
    return () => window.clearTimeout(t)
  }, [toast])

  const toastApi = useMemo<ToastApi>(() => ({ show: (text, link) => setToast({ text, link, key: Date.now() }) }), [])
  const closePalette = useCallback(() => setPalette(false), [])
  const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

  return (
    <ToastContext.Provider value={toastApi}>
      <header className="masthead">
        <div className="masthead-inner">
          <Link to="/" className="brand" aria-label="PCAS 首页">
            <Mark />
            PCAS
          </Link>
          <nav className="nav" aria-label="主导航">
            <NavLink to="/" end className={({ isActive }) => (isActive ? 'active' : undefined)}>
              现在{needs > 0 && <span className="count">{needs}</span>}
            </NavLink>
            <NavLink to="/things" className={({ isActive }) => (isActive ? 'active' : undefined)}>
              事情
            </NavLink>
            <NavLink to="/library" className={({ isActive }) => (isActive ? 'active' : undefined)}>
              资料库
            </NavLink>
          </nav>
          <div className="masthead-tools">
            <Activity />
            <button type="button" className="capture-btn" onClick={() => setPalette(true)}>
              <Plus size={15} />
              记一笔
              <kbd>{isMac ? '⌘K' : 'Ctrl K'}</kbd>
            </button>
            <NavLink to="/settings" className={({ isActive }) => `icon-link${isActive ? ' active' : ''}`} aria-label="设置">
              <Settings size={18} />
            </NavLink>
          </div>
        </div>
      </header>

      <Outlet />

      <nav className="tabbar" aria-label="主导航">
        <NavLink to="/" end className={({ isActive }) => (isActive ? 'active' : undefined)}>
          <span className="tab-icon">
            <CalendarCheck size={20} />
            {needs > 0 && <span className="count">{needs}</span>}
          </span>
          现在
        </NavLink>
        <NavLink to="/things" className={({ isActive }) => (isActive ? 'active' : undefined)}>
          <Layers size={20} />
          事情
        </NavLink>
        <button type="button" onClick={() => setPalette(true)} aria-label="记一笔">
          <span className="capture-fab">
            <Plus size={24} />
          </span>
        </button>
        <NavLink to="/library" className={({ isActive }) => (isActive ? 'active' : undefined)}>
          <BookOpen size={20} />
          资料库
        </NavLink>
        <NavLink to="/settings" className={({ isActive }) => (isActive ? 'active' : undefined)}>
          <Settings size={20} />
          设置
        </NavLink>
      </nav>

      {palette && <CommandPalette onClose={closePalette} />}
      {toast && (
        <div className="toast" key={toast.key} role="status">
          <Sparkles size={15} />
          {toast.text}
          {toast.link && (
            <Link to={toast.link.to} style={{ color: 'var(--highlight)' }} onClick={() => setToast(null)}>
              {toast.link.label}
            </Link>
          )}
        </div>
      )}
    </ToastContext.Provider>
  )
}
