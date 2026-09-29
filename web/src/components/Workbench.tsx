import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router'
import { Command, Info, Menu, Sparkles } from 'lucide-react'
import { tabInfo } from '../domain/views'
import { useStore } from '../store/context'
import { ToastContext, type ToastApi } from '../store/toast'
import { WorkspaceContext, type WorkspaceApi, type WorkspaceState } from '../store/workspace'
import { CommandPalette } from './CommandPalette'
import { ContextPane } from './ContextPane'
import { Sidebar } from './Sidebar'
import { StatusBar } from './StatusBar'
import { TabStrip } from './TabStrip'

const WS_KEY = 'pcas.workspace'

const initialWs: WorkspaceState = {
  tabs: ['/today'],
  sidebar: true,
  context: true,
  expanded: {},
  drafts: {},
  agentFor: {},
  drawer: null,
}

function loadWs(): WorkspaceState {
  try {
    const raw = localStorage.getItem(WS_KEY)
    if (raw) return { ...initialWs, ...(JSON.parse(raw) as Partial<WorkspaceState>), drawer: null, selected: undefined }
  } catch {
    // Fall back to defaults.
  }
  return initialWs
}

function typingTarget(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  return !!el && (['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable)
}

export function Workbench() {
  const { state } = useStore()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const [ws, setWs] = useState<WorkspaceState>(loadWs)
  const [palette, setPalette] = useState(false)
  const [toast, setToast] = useState<{ text: string; link?: { to: string; label: string }; key: number } | null>(null)

  useEffect(() => {
    try {
      const { drawer: _d, selected: _s, ...persist } = ws
      void _d
      void _s
      localStorage.setItem(WS_KEY, JSON.stringify(persist))
    } catch {
      // Layout is a convenience; losing it is fine.
    }
  }, [ws])

  const current = tabInfo(state, pathname) ? pathname : undefined
  // Every place you visit becomes a tab (adjusted during render when the route changes).
  const [seenPath, setSeenPath] = useState<string | undefined>(undefined)
  if (seenPath !== pathname) {
    setSeenPath(pathname)
    if (current && !ws.tabs.includes(current)) setWs((w) => (w.tabs.includes(current) ? w : { ...w, tabs: [...w.tabs, current] }))
  }
  // Tabs pointing at deleted things drop out.
  const shownTabs = useMemo(() => ws.tabs.filter((p) => tabInfo(state, p)), [ws.tabs, state])

  const closeTab = useCallback(
    (path: string) => {
      const index = shownTabs.indexOf(path)
      const rest = shownTabs.filter((p) => p !== path)
      setWs((w) => ({ ...w, tabs: rest }))
      if (path === pathname) navigate(rest[Math.min(index, rest.length - 1)] ?? '/today')
    },
    [shownTabs, pathname, navigate],
  )

  const api = useMemo<WorkspaceApi>(
    () => ({
      ws: { ...ws, tabs: shownTabs },
      closeTab,
      toggleSidebar: () => setWs((w) => ({ ...w, sidebar: !w.sidebar })),
      toggleContext: () => setWs((w) => ({ ...w, context: !w.context })),
      toggleExpanded: (id) => setWs((w) => ({ ...w, expanded: { ...w.expanded, [id]: !(w.expanded[id] ?? true) } })),
      setDraft: (id, text) => setWs((w) => ({ ...w, drafts: { ...w.drafts, [id]: text } })),
      setAgentFor: (id, agentId) => setWs((w) => ({ ...w, agentFor: { ...w.agentFor, [id]: agentId } })),
      setSelected: (id) => setWs((w) => (w.selected === id ? w : { ...w, selected: id })),
      setDrawer: (drawer) => setWs((w) => ({ ...w, drawer })),
      openPalette: () => setPalette(true),
    }),
    [ws, shownTabs, closeTab],
  )

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPalette(true)
      } else if (e.key === '/' && !mod && !typingTarget(e.target)) {
        e.preventDefault()
        setPalette(true)
      } else if (mod && e.key === '\\') {
        e.preventDefault()
        api.toggleSidebar()
      } else if (mod && e.key === '.') {
        e.preventDefault()
        api.toggleContext()
      } else if (e.altKey && (e.code === 'KeyW' || e.key === 'w')) {
        e.preventDefault()
        if (current) closeTab(current)
      } else if (e.altKey && (e.code === 'BracketLeft' || e.code === 'BracketRight')) {
        e.preventDefault()
        const i = shownTabs.indexOf(pathname)
        const next = shownTabs[(i + (e.code === 'BracketRight' ? 1 : shownTabs.length - 1)) % shownTabs.length]
        if (next) navigate(next)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [api, current, closeTab, shownTabs, pathname, navigate])

  useEffect(() => {
    if (!toast) return
    const t = window.setTimeout(() => setToast(null), 3000)
    return () => window.clearTimeout(t)
  }, [toast])

  const toastApi = useMemo<ToastApi>(() => ({ show: (text, link) => setToast({ text, link, key: Date.now() }) }), [])
  const closePalette = useCallback(() => setPalette(false), [])
  const title = tabInfo(state, pathname)?.title ?? 'PCAS'

  return (
    <WorkspaceContext.Provider value={api}>
      <ToastContext.Provider value={toastApi}>
        <div className={`workbench${ws.sidebar ? '' : ' no-sidebar'}${ws.context ? '' : ' no-context'}`}>
          <Sidebar />
          <main className="main">
            <div className="mobile-bar">
              <button type="button" className="btn btn-quiet btn-icon" aria-label="导航" onClick={() => api.setDrawer('sidebar')}>
                <Menu size={18} />
              </button>
              <span className="title">{title}</span>
              <button type="button" className="btn btn-quiet btn-icon" aria-label="命令" onClick={() => setPalette(true)}>
                <Command size={16} />
              </button>
              <button type="button" className="btn btn-quiet btn-icon" aria-label="上下文" onClick={() => api.setDrawer('context')}>
                <Info size={17} />
              </button>
            </div>
            <TabStrip />
            <div className="content">
              <Outlet />
            </div>
          </main>
          <ContextPane />
          <StatusBar />
        </div>
        {ws.drawer && <div className="scrim" style={{ zIndex: 44 }} onClick={() => api.setDrawer(null)} />}
        {palette && <CommandPalette onClose={closePalette} />}
        {toast && (
          <div className="toast" key={toast.key} role="status">
            <Sparkles size={14} />
            {toast.text}
            {toast.link && (
              <Link to={toast.link.to} onClick={() => setToast(null)}>
                {toast.link.label}
              </Link>
            )}
          </div>
        )}
      </ToastContext.Provider>
    </WorkspaceContext.Provider>
  )
}
