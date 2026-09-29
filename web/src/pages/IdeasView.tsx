import { useCallback } from 'react'
import { useNavigate } from 'react-router'
import { Empty, Tag } from '../components/ui'
import { ideaStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import { ideaGroups } from '../domain/views'
import { useStore } from '../store/context'
import { useListNav } from '../store/useListNav'
import { useToast } from '../store/toast'
import { useWorkspace } from '../store/workspace'

export function IdeasView() {
  const { state, dispatch } = useStore()
  const { setSelected } = useWorkspace()
  const navigate = useNavigate()
  const toast = useToast()
  const groups = ideaGroups(state)
  const ids = groups.filter((g) => g.key !== 'closed').flatMap((g) => g.ideas.map((i) => i.id))

  const onKey = useCallback(
    (key: string, targets: string[]) => {
      if (key === 'p') {
        for (const id of targets) dispatch({ type: 'ideaPromote', id })
        toast.show(`${targets.length} 个想法转成了待办`)
        return true
      }
      return false
    },
    [dispatch, toast],
  )
  const onOpen = useCallback((id: string) => navigate(`/t/${id}`), [navigate])
  const nav = useListNav(ids, { onOpen, onKey, onCursor: setSelected })

  return (
    <div className="view">
      <div className="view-head">
        <div>
          <h1>想法</h1>
          <p>放着的想法写明了再看的条件，条件满足会自己回到“今天”。</p>
        </div>
      </div>
      {groups.length === 0 && <Empty>还没有想法。</Empty>}
      {groups.map((g) => (
        <div key={g.key} className="group">
          <div className="group-head">
            {g.label}
            <span className="n">{g.ideas.length}</span>
          </div>
          {g.ideas.map((i) => {
            const pending = i.conditions.filter((c) => !c.met)
            const sub =
              i.status === 'awakened' ? i.wake?.reason : i.status === 'shelved' ? (pending[0] ? `等：${pending[0].description}` : i.shelvedReason) : i.body
            return (
              <div
                key={i.id}
                className={`lrow${nav.cursor === i.id ? ' cursor' : ''}`}
                onClick={() => {
                  nav.setCursor(i.id)
                  navigate(`/t/${i.id}`)
                }}
              >
                <span className="kind kind-idea" />
                <span className={`title${g.key === 'closed' ? ' done' : ''}`} style={{ flex: '0 1 auto' }}>
                  {i.title}
                </span>
                <span className="sub grow" style={{ maxWidth: 'none' }}>
                  {sub}
                </span>
                {i.status === 'awakened' && <Tag tone="accent">{ideaStatusLabel.awakened.text}</Tag>}
                <span className="when">{formatAgo(i.updatedAt)}</span>
              </div>
            )
          })}
        </div>
      ))}
      <div className="keys">
        <span>
          <kbd>J</kbd>
          <kbd>K</kbd> 移动
        </span>
        <span>
          <kbd>↵</kbd> 打开
        </span>
        <span>
          <kbd>P</kbd> 转成待办
        </span>
      </div>
    </div>
  )
}
