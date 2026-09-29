import { NavLink, useNavigate } from 'react-router'
import { BookOpen, House, Plus, Search, Settings } from 'lucide-react'
import { newId } from '../domain/ids'
import { urgentLine } from '../domain/lines'
import { isOpenTask } from '../domain/things'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'

export function Sidebar({ open }: { open: boolean }) {
  const { state, dispatch } = useStore()
  const { openPalette, closeDrawer } = useShell()
  const navigate = useNavigate()
  const urgent = urgentLine(state).length
  const isMac = /Mac|iPhone|iPad/.test(navigator.platform)
  const item = ({ isActive }: { isActive: boolean }) => `nav-item${isActive ? ' active' : ''}`

  return (
    <nav className={`sidebar${open ? ' open' : ''}`} aria-label="导航">
      <div className="wordmark">PCAS</div>
      <button type="button" className="find" onClick={openPalette}>
        <Search size={14} />
        搜索
        <kbd>{isMac ? '⌘K' : 'Ctrl K'}</kbd>
      </button>

      <div className="nav-scroll">
        <NavLink to="/" end className={item} onClick={closeDrawer}>
          <House size={16} />
          首页
          {urgent > 0 && <span className="n">{urgent}</span>}
        </NavLink>

        <div className="nav-title">项目</div>
        {state.projects
          .filter((p) => p.status !== 'done')
          .map((p) => {
            const open = state.tasks.filter((t) => t.projectId === p.id && isOpenTask(t)).length
            return (
              <NavLink key={p.id} to={`/t/${p.id}`} className={item} onClick={closeDrawer}>
                <span className="grow ellipsis" style={{ color: p.status === 'paused' ? 'var(--label-2)' : undefined }}>
                  {p.name}
                </span>
                {open > 0 && <span className="n">{open}</span>}
              </NavLink>
            )
          })}
        <button
          type="button"
          className="nav-item add"
          onClick={() => {
            const name = window.prompt('新项目的名字')?.trim()
            if (!name) return
            const id = newId()
            dispatch({ type: 'addProject', id, name })
            closeDrawer()
            navigate(`/t/${id}`)
          }}
        >
          <Plus size={15} style={{ color: 'var(--label-3)' }} />
          新项目
        </button>
      </div>

      <div className="nav-foot">
        <NavLink to="/library" className={item} onClick={closeDrawer}>
          <BookOpen size={16} />
          资料库
        </NavLink>
        <NavLink to="/settings" className={item} onClick={closeDrawer}>
          <Settings size={16} />
          设置
        </NavLink>
      </div>
    </nav>
  )
}
