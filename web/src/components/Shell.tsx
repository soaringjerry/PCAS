import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, Outlet, useLocation } from 'react-router'
import { Menu, Search } from 'lucide-react'
import { findThing, thingTitle } from '../domain/things'
import { useStore } from '../store/context'
import { ShellContext, type ShellApi } from '../store/shell'
import { ToastContext, type ToastApi } from '../store/toast'
import { CommandPalette } from './CommandPalette'
import { Sidebar } from './Sidebar'

const KEY = 'pcas.shell'

interface Persisted {
  drafts: Record<string, string>
  agents: Record<string, string>
}

function load(): Persisted {
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) return { drafts: {}, agents: {}, ...(JSON.parse(raw) as Partial<Persisted>) }
  } catch {
    // Defaults are fine.
  }
  return { drafts: {}, agents: {} }
}

const titles: Record<string, string> = { '/': '首页', '/library': '资料库', '/settings': '设置' }

export function Shell() {
  const { state } = useStore()
  const { pathname } = useLocation()
  const [saved, setSaved] = useState<Persisted>(load)
  const [palette, setPalette] = useState(false)
  const [drawer, setDrawer] = useState(false)
  const [toast, setToast] = useState<{ text: string; link?: { to: string; label: string }; key: number } | null>(null)

  useEffect(() => {
    try {
      localStorage.setItem(KEY, JSON.stringify(saved))
    } catch {
      // Drafts are a convenience.
    }
  }, [saved])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement
      const typing = ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName) || el.isContentEditable
      if (((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') || (e.key === '/' && !typing)) {
        e.preventDefault()
        setPalette(true)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    if (!toast) return
    const t = window.setTimeout(() => setToast(null), 2800)
    return () => window.clearTimeout(t)
  }, [toast])

  const api = useMemo<ShellApi>(
    () => ({
      openPalette: () => setPalette(true),
      closeDrawer: () => setDrawer(false),
      draft: (id) => saved.drafts[id] ?? '',
      setDraft: (id, text) => setSaved((s) => ({ ...s, drafts: { ...s.drafts, [id]: text } })),
      agentFor: (id) => saved.agents[id] ?? 'a_claude',
      setAgentFor: (id, agentId) => setSaved((s) => ({ ...s, agents: { ...s.agents, [id]: agentId } })),
    }),
    [saved],
  )
  const toastApi = useMemo<ToastApi>(() => ({ show: (text, link) => setToast({ text, link, key: Date.now() }) }), [])
  const closePalette = useCallback(() => setPalette(false), [])

  const thingId = pathname.match(/^\/t\/(.+)$/)?.[1]
  const thing = thingId ? findThing(state, thingId) : undefined
  const title = thing ? thingTitle(thing) : (titles[pathname] ?? 'PCAS')

  return (
    <ShellContext.Provider value={api}>
      <ToastContext.Provider value={toastApi}>
        <div className="shell">
          <Sidebar open={drawer} />
          <main className="main">
            <div className="topbar">
              <button type="button" className="btn btn-quiet btn-icon" aria-label="导航" onClick={() => setDrawer(true)}>
                <Menu size={20} />
              </button>
              <span className="title">{title}</span>
              <button type="button" className="btn btn-quiet btn-icon" aria-label="搜索" onClick={() => setPalette(true)}>
                <Search size={18} />
              </button>
            </div>
            <Outlet />
          </main>
        </div>
        {drawer && <div className="scrim" style={{ zIndex: 44 }} onClick={() => setDrawer(false)} />}
        {palette && <CommandPalette onClose={closePalette} />}
        {toast && (
          <div className="toast" key={toast.key} role="status">
            {toast.text}
            {toast.link && (
              <Link to={toast.link.to} onClick={() => setToast(null)}>
                {toast.link.label}
              </Link>
            )}
          </div>
        )}
      </ToastContext.Provider>
    </ShellContext.Provider>
  )
}
