import { Link } from 'react-router'
import { FileUp } from 'lucide-react'
import { TaskRow } from '../components/bits'
import { Button, Card, Empty, Progress } from '../components/ui'
import { WakeCard } from '../components/Wake'
import { dayOffset } from '../domain/time'
import type { Task } from '../domain/types'
import { useStore } from '../store/context'
import { useDetail } from '../store/hooks'

function isToday(task: Task): boolean {
  if (task.status === 'done' || task.status === 'cancelled' || task.status === 'waiting') return false
  if (task.status === 'doing') return true
  const due = task.due ? dayOffset(task.due) : Infinity
  const scheduled = task.scheduled ? dayOffset(task.scheduled) : Infinity
  return due <= 0 || scheduled <= 0
}

function DemoBanner() {
  const { state, runDemoImport } = useStore()
  const job = state.jobs.find((j) => j.id === 'j_demo')
  const running = job && job.status === 'running'
  if (state.demo.costReportImported && !running) return null
  return (
    <div className="demo-banner">
      <FileUp size={22} style={{ color: 'var(--accent)', flex: 'none' }} />
      <div className="grow">
        <div className="ink" style={{ fontWeight: 600 }}>演示：搁置的想法被新资料唤醒</div>
        {running ? (
          <div className="stack-sm" style={{ marginTop: 6 }}>
            <div className="small muted">{job.detail}</div>
            <Progress value={job.progress ?? 0} />
          </div>
        ) : (
          <div className="small muted">
            IDEA「用本地小模型做记忆提取」因为缺少成本数据被搁置。导入一份成本测算，看看它会不会回来。
          </div>
        )}
      </div>
      {!running && (
        <Button variant="primary" onClick={runDemoImport}>
          模拟导入成本测算
        </Button>
      )}
    </div>
  )
}

export function TodayPage() {
  const { state } = useStore()
  const { openTask } = useDetail()
  const awakened = state.ideas.filter((i) => i.status === 'awakened')
  const today = state.tasks.filter(isToday).sort((a, b) => (a.status === 'doing' ? -1 : 0) - (b.status === 'doing' ? -1 : 0))
  const waiting = state.tasks.filter((t) => t.status === 'waiting')
  const pending = state.candidates.filter((c) => c.state === 'pending').length
  const runningJobs = state.jobs.filter((j) => j.status === 'running' || j.status === 'queued').length
  const failedJobs = state.jobs.filter((j) => j.status === 'failed').length
  const date = new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>今天</h1>
          <p>{date}</p>
        </div>
      </div>

      <div className="stack">
        <DemoBanner />

        {awakened.length > 0 && (
          <section className="stack-sm">
            <h2>重新唤醒的想法</h2>
            {awakened.map((idea) => (
              <WakeCard key={idea.id} idea={idea} />
            ))}
          </section>
        )}

        <div className="grid-3">
          <Link to="/inbox" className="card stat" style={{ textDecoration: 'none' }}>
            <div className="stat-value">{pending}</div>
            <div className="stat-label">条候选待整理</div>
          </Link>
          <Link to="/sources" className="card stat" style={{ textDecoration: 'none' }}>
            <div className="stat-value">{runningJobs}</div>
            <div className="stat-label">个后台作业进行中</div>
          </Link>
          <Link to="/sources" className="card stat" style={{ textDecoration: 'none' }}>
            <div className="stat-value" style={{ color: failedJobs ? 'var(--danger)' : undefined }}>
              {failedJobs}
            </div>
            <div className="stat-label">个作业需要处理</div>
          </Link>
        </div>

        <div className="grid-2">
          <Card title="今天要做" hint="正在做、今天安排或到期的事">
            {today.length === 0 ? (
              <Empty>今天没有安排。</Empty>
            ) : (
              <div className="list">
                {today.map((task) => (
                  <TaskRow key={task.id} task={task} onOpen={() => openTask(task.id)} />
                ))}
              </div>
            )}
          </Card>
          <Card title="等待中" hint="在等别人或别的事，系统会按规则提醒">
            {waiting.length === 0 ? (
              <Empty>没有在等的事。</Empty>
            ) : (
              <div className="list">
                {waiting.map((task) => (
                  <TaskRow key={task.id} task={task} onOpen={() => openTask(task.id)} />
                ))}
              </div>
            )}
          </Card>
        </div>
      </div>
    </main>
  )
}
