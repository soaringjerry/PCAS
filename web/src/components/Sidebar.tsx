import { NavLink, useNavigate } from 'react-router'
import { BookOpen, CalendarDays, ChevronRight, Inbox, Lightbulb, Plus, Search, Settings, Sun } from 'lucide-react'
import { newId } from '../domain/ids'
import { isOpenTask } from '../domain/things'
import { inboxCount, todayCount } from '../domain/views'
import { useStore } from '../store/context'
import { useWorkspace } from '../store/workspace'
import { KindLabel } from './Marks'

function Item({ to, icon, label, count, hot }: { to: string; icon: React.ReactNode; label: string; count?: number; hot?: boolean }) {
  const { setDrawer } = useWorkspace()
  return (
    <NavLink to={to} className={({ isActive }) => `sb-item${isActive ? ' active' : ''}`} onClick={() => setDrawer(null)}>
      {icon}
      <span className="grow ellipsis">{label}</span>
      {!!count && <span className={`count${hot ? ' hot' : ''}`}>{count}</span>}
    </NavLink>
  )
}

export function Sidebar() {
  const { state, dispatch } = useStore()
  const { ws, toggleExpanded, openPalette, setDrawer } = useWorkspace()
  const navigate = useNavigate()
  const awakened = state.ideas.filter((i) => i.status === 'awakened').length
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform)

  return (
    <nav className={`sidebar${ws.drawer === 'sidebar' ? ' open' : ''}`} aria-label="导航">
      <div className="sb-top">
        <div className="brand">
          <svg width="18" height="18" viewBox="0 0 32 32" aria-hidden="true">
            <rect x="2" y="2" width="28" height="28" rx="7" fill="var(--accent)" />
            <path d="M10 11h12M10 16h12M10 21h7" stroke="var(--on-accent)" strokeWidth="2.4" strokeLinecap="round" />
          </svg>
          PCAS
          <small>工作台</small>
        </div>
        <button type="button" className="search-btn" onClick={openPalette}>
          <Search size={13} />
          搜索或命令
          <kbd>{isMac ? '⌘K' : 'Ctrl K'}</kbd>
        </button>
      </div>

      <div className="sb-scroll">
        <div className="sb-group">
          <Item to="/inbox" icon={<Inbox size={15} />} label="收件" count={inboxCount(state)} />
          <Item to="/today" icon={<Sun size={15} />} label="今天" count={todayCount(state)} hot />
          <Item to="/upcoming" icon={<CalendarDays size={15} />} label="接下来" />
          <Item to="/ideas" icon={<Lightbulb size={15} />} label="想法" count={awakened} hot />
        </div>

        <div className="sb-group">
          <div className="sb-label">
            项目
            <button
              type="button"
              aria-label="新建项目"
              title="新建项目"
              onClick={() => {
                const name = window.prompt('项目名称')
                if (!name?.trim()) return
                const id = newId('p')
                dispatch({ type: 'addProject', id, name: name.trim() })
                navigate(`/t/${id}`)
              }}
            >
              <Plus size={13} />
            </button>
          </div>
          {state.projects
            .filter((p) => p.status !== 'done')
            .map((p) => {
              const open = ws.expanded[p.id] ?? p.status === 'active'
              const tasks = state.tasks.filter((t) => t.projectId === p.id && isOpenTask(t))
              const ideas = state.ideas.filter((i) => i.projectId === p.id && (i.status === 'active' || i.status === 'awakened'))
              return (
                <div key={p.id}>
                  <NavLink to={`/t/${p.id}`} className={({ isActive }) => `sb-item${isActive ? ' active' : ''}`} onClick={() => setDrawer(null)}>
                    <button
                      type="button"
                      className={`chev${open ? ' open' : ''}`}
                      aria-label={open ? '收起' : '展开'}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        toggleExpanded(p.id)
                      }}
                    >
                      <ChevronRight size={12} />
                    </button>
                    <span className="grow ellipsis" style={{ opacity: p.status === 'paused' ? 0.6 : 1 }}>
                      {p.name}
                    </span>
                    <span className="count">{tasks.length || ''}</span>
                  </NavLink>
                  {open &&
                    [...tasks, ...ideas].map((item) => (
                      <NavLink
                        key={item.id}
                        to={`/t/${item.id}`}
                        className={({ isActive }) => `sb-item sb-child${isActive ? ' active' : ''}`}
                        onClick={() => setDrawer(null)}
                      >
                        <KindLabel kind={'checklist' in item ? 'task' : 'idea'} bare />
                        <span className="grow ellipsis">{item.title}</span>
                        {'status' in item && item.status === 'doing' && <span className="count" style={{ color: 'var(--accent)' }}>●</span>}
                      </NavLink>
                    ))}
                </div>
              )
            })}
        </div>
      </div>

      <div className="sb-foot">
        <Item to="/library" icon={<BookOpen size={15} />} label="资料库" />
        <Item to="/settings" icon={<Settings size={15} />} label="设置" />
      </div>
    </nav>
  )
}
