import { Link, useLocation } from 'react-router'
import { PanelLeft, PanelRight, Plus, X } from 'lucide-react'
import { tabInfo } from '../domain/views'
import { useStore } from '../store/context'
import { useWorkspace } from '../store/workspace'
import { KindLabel } from './Marks'

export function TabStrip() {
  const { state } = useStore()
  const { ws, closeTab, openPalette, toggleSidebar, toggleContext } = useWorkspace()
  const { pathname } = useLocation()

  return (
    <div className="tabstrip" role="tablist">
      {ws.tabs.map((path) => {
        const info = tabInfo(state, path)
        if (!info) return null
        const active = path === pathname
        return (
          <Link
            key={path}
            to={path}
            role="tab"
            aria-selected={active}
            className={`tab${active ? ' active' : ''}`}
            title={info.title}
            onAuxClick={(e) => {
              if (e.button === 1) {
                e.preventDefault()
                closeTab(path)
              }
            }}
          >
            {info.kind && <KindLabel kind={info.kind} bare />}
            <span className="ellipsis">{info.title}</span>
            <button
              type="button"
              className="x"
              aria-label={`关闭 ${info.title}`}
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                closeTab(path)
              }}
            >
              <X size={12} />
            </button>
          </Link>
        )
      })}
      <button type="button" className="tab-add" aria-label="打开更多" title="打开或新建（⌘K）" onClick={openPalette}>
        <Plus size={15} />
      </button>
      <div className="tab-tools">
        <button type="button" className={`btn btn-quiet btn-sm btn-icon${ws.sidebar ? '' : ' faint'}`} aria-label="切换侧栏" title="侧栏（⌘\）" onClick={toggleSidebar}>
          <PanelLeft size={14} />
        </button>
        <button type="button" className={`btn btn-quiet btn-sm btn-icon${ws.context ? '' : ' faint'}`} aria-label="切换上下文" title="上下文（⌘.）" onClick={toggleContext}>
          <PanelRight size={14} />
        </button>
      </div>
    </div>
  )
}
