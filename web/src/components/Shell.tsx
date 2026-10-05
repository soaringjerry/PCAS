import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, NavLink, Outlet, useLocation } from 'react-router'
import { BookOpen, Search, Settings } from 'lucide-react'
import { findThing, thingTitle } from '../domain/things'
import { useStore } from '../store/context'
import { ShellContext, type ShellApi } from '../store/shell'
import type { ToastOptions } from '../store/toast'
import { CommandPalette } from './CommandPalette'

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

const titles: Record<string, string> = { '/library': '资料库', '/about': '关于你', '/settings': '设置' }
const isMac = /Mac|iPhone|iPad/.test(navigator.platform)

export function Shell() {
  const { state } = useStore()
  const { pathname } = useLocation()
  const [saved, setSaved] = useState<Persisted>(load)
  const [palette, setPalette] = useState(false)
  const prefillListeners = useRef(new Set<(key: string, text: string) => void>())

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

  const api = useMemo<ShellApi>(
    () => ({
      openPalette: () => setPalette(true),
      draft: (id) => saved.drafts[id] ?? '',
      setDraft: (id, text) => setSaved((s) => ({ ...s, drafts: { ...s.drafts, [id]: text } })),
      prefill: (key, text) => {
        setSaved((s) => ({ ...s, drafts: { ...s.drafts, [key]: text } }))
        prefillListeners.current.forEach((listener) => listener(key, text))
      },
      onPrefill: (listener) => {
        prefillListeners.current.add(listener)
        return () => void prefillListeners.current.delete(listener)
      },
      agentFor: (id) => state.agents.find((a) => a.id === saved.agents[id] && a.enabled)?.id ?? state.agents.find((a) => a.enabled && a.default)?.id ?? state.agents.find((a) => a.enabled && a.available && a.protocol !== 'siwc')?.id ?? 'manual',
      setAgentFor: (id, agentId) => setSaved((s) => ({ ...s, agents: { ...s.agents, [id]: agentId } })),
    }),
    [saved, state.agents],
  )
  const closePalette = useCallback(() => setPalette(false), [])

  const thingId = pathname.match(/^\/t\/(.+)$/)?.[1]
  const thing = thingId ? findThing(state, thingId) : undefined
  // The hall needs no title; everywhere else says where you are.
  const title = thing ? thingTitle(thing) : titles[pathname]
  const tab = ({ isActive }: { isActive: boolean }) => `bar-btn${isActive ? ' active' : ''}`

  return (
    <ShellContext.Provider value={api}>
      <div className="shell">
        <header className="bar">
          <Link to="/" className="bar-mark" aria-label="回到大厅">
            <img src="/favicon.svg" alt="" width={22} height={22} />
            PCAS
          </Link>
          {title && (
            <>
              <span className="bar-sep" aria-hidden="true">
                /
              </span>
              <span className="bar-title">{title}</span>
            </>
          )}
          <nav className="bar-end" aria-label="导航">
            <button type="button" className="bar-btn bar-search" aria-label="搜索" aria-keyshortcuts={isMac ? 'Meta+K' : 'Control+K'} onClick={() => setPalette(true)}>
              <Search size={16} />
              <span className="label">搜索</span>
              <kbd>{isMac ? '⌘K' : 'Ctrl K'}</kbd>
            </button>
            <NavLink to="/library" className={tab} aria-label="资料库">
              <BookOpen size={16} />
              <span className="label">资料库</span>
            </NavLink>
            <NavLink to="/settings" className={tab} aria-label="设置">
              <Settings size={16} />
              <span className="label">设置</span>
            </NavLink>
          </nav>
        </header>
        <main className="main">
          <Outlet />
        </main>
      </div>
      {palette && <CommandPalette onClose={closePalette} />}
    </ShellContext.Provider>
  )
}

export interface ToastEntry extends ToastOptions {
  text: string
  key: number
}

/** A one-line note at the bottom. With 【撤销】 it stays 8 seconds, and pointing at it pauses the clock. */
export function Toast({ toast, onClose }: { toast: ToastEntry; onClose: () => void }) {
  const [paused, setPaused] = useState(false)
  const left = useRef(toast.undo ? 8000 : 2800)
  useEffect(() => {
    if (paused) return
    const started = Date.now()
    const t = window.setTimeout(onClose, left.current)
    return () => {
      window.clearTimeout(t)
      left.current -= Date.now() - started
    }
  }, [paused, onClose])

  return (
    <div className="toast" role="status" onMouseEnter={() => setPaused(true)} onMouseLeave={() => setPaused(false)} onFocus={() => setPaused(true)} onBlur={() => setPaused(false)}>
      {toast.text}
      {toast.link && (
        <Link to={toast.link.to} onClick={onClose}>
          {toast.link.label}
        </Link>
      )}
      {toast.undo && (
        <button
          type="button"
          onClick={() => {
            onClose()
            void toast.undo?.()
          }}
        >
          撤销
        </button>
      )}
    </div>
  )
}
