import { useState } from 'react'
import { Plus } from 'lucide-react'
import { TaskRow } from '../components/bits'
import { Button, Card, Empty, Tabs } from '../components/ui'
import { taskStatusLabel } from '../domain/labels'
import type { TaskStatus } from '../domain/types'
import { useStore } from '../store/context'
import { useDetail } from '../store/hooks'

const openColumns: { status: TaskStatus; hint: string }[] = [
  { status: 'doing', hint: '手上正在推进' },
  { status: 'todo', hint: '接下来要做' },
  { status: 'waiting', hint: '在等别人或别的事' },
]

export function TasksPage() {
  const { state, dispatch } = useStore()
  const { openTask } = useDetail()
  const [tab, setTab] = useState<'open' | 'closed'>('open')
  const [project, setProject] = useState('')
  const [title, setTitle] = useState('')

  const tasks = state.tasks.filter((t) => !project || t.projectId === project)
  const closed = tasks.filter((t) => t.status === 'done' || t.status === 'cancelled')

  const add = () => {
    if (!title.trim()) return
    dispatch({ type: 'addTask', title: title.trim(), projectId: project || undefined })
    setTitle('')
  }

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>任务</h1>
          <p>你自己的待办。后台作业在“来源与作业”里，不会混在这里。</p>
        </div>
        <div className="row">
          <select className="select" style={{ width: 'auto' }} value={project} onChange={(e) => setProject(e.target.value)} aria-label="按项目筛选">
            <option value="">全部项目</option>
            {state.projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <Tabs
            value={tab}
            onChange={setTab}
            items={[
              { value: 'open', label: '未完成', count: tasks.length - closed.length },
              { value: 'closed', label: '已结束', count: closed.length },
            ]}
          />
        </div>
      </div>

      {tab === 'open' ? (
        <div className="stack">
          <form
            className="row"
            style={{ flexWrap: 'nowrap' }}
            onSubmit={(e) => {
              e.preventDefault()
              add()
            }}
          >
            <input className="input" placeholder="添加任务…" value={title} onChange={(e) => setTitle(e.target.value)} />
            <Button type="submit" icon={<Plus size={15} />} disabled={!title.trim()}>
              添加
            </Button>
          </form>
          <div className="board">
            {openColumns.map((col) => {
              const items = tasks.filter((t) => t.status === col.status)
              return (
                <Card key={col.status} title={`${taskStatusLabel[col.status].text} · ${items.length}`} hint={col.hint}>
                  {items.length === 0 ? (
                    <Empty>没有</Empty>
                  ) : (
                    <div className="list">
                      {items.map((t) => (
                        <TaskRow key={t.id} task={t} onOpen={() => openTask(t.id)} showProject={!project} />
                      ))}
                    </div>
                  )}
                </Card>
              )
            })}
          </div>
        </div>
      ) : (
        <Card>
          {closed.length === 0 ? (
            <Empty>还没有结束的任务。</Empty>
          ) : (
            <div className="list">
              {closed.map((t) => (
                <TaskRow key={t.id} task={t} onOpen={() => openTask(t.id)} showProject={!project} />
              ))}
            </div>
          )}
        </Card>
      )}
    </main>
  )
}
