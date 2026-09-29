import { useNavigate } from 'react-router'
import { FileUp } from 'lucide-react'
import { TaskList } from '../components/TaskList'
import { Button, Progress } from '../components/ui'
import { attentionFor, isDecision, type Attention } from '../domain/attention'
import { findThing, thingTitle } from '../domain/things'
import { todayGroups } from '../domain/views'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'

function Decision({ item }: { item: Attention }) {
  const { state, dispatch } = useStore()
  const navigate = useNavigate()
  const toast = useToast()

  if (item.kind === 'wake') {
    return (
      <div className="decision">
        <span className="glyph glyph-wake">✦</span>
        <div className="grow">
          <div className="row-nowrap">
            <a href={`/t/${item.idea.id}`} onClick={(e) => (e.preventDefault(), navigate(`/t/${item.idea.id}`))} className="ink" style={{ fontWeight: 600 }}>
              {item.idea.title}
            </a>
            <span className="tiny muted">想法回来了</span>
          </div>
          <p className="small" style={{ marginTop: 2 }}>
            {item.idea.wake?.reason}
          </p>
        </div>
        <div className="row" style={{ flex: 'none' }}>
          <Button
            size="sm"
            variant="primary"
            onClick={() => {
              dispatch({ type: 'ideaPromote', id: item.idea.id })
              const task = state.tasks.find((t) => t.ideaId === item.idea.id)
              toast.show('转成待办了', task ? { to: `/t/${task.id}`, label: '打开' } : { to: `/t/${item.idea.id}`, label: '打开' })
            }}
          >
            转成待办
          </Button>
          <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ideaSnooze', id: item.idea.id, days: 7 })}>
            一周后
          </Button>
          <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ideaStopReminders', id: item.idea.id })}>
            别提醒
          </Button>
        </div>
      </div>
    )
  }

  if (item.kind === 'result') {
    const thing = findThing(state, item.run.thingId)
    const agent = state.agents.find((a) => a.id === item.run.agentId)?.name
    return (
      <div className="decision">
        <span className="glyph glyph-result">↩</span>
        <div className="grow">
          <div className="ink" style={{ fontWeight: 600 }}>
            {agent} 交回了结果
          </div>
          <p className="small muted" style={{ marginTop: 2 }}>
            {item.run.prompt} · {thing ? thingTitle(thing) : ''}
          </p>
        </div>
        <Button size="sm" variant="primary" onClick={() => navigate(`/t/${item.run.thingId}#run-${item.run.id}`)}>
          去看
        </Button>
      </div>
    )
  }

  if (item.kind === 'job') {
    return (
      <div className="decision">
        <span className="glyph glyph-due">!</span>
        <div className="grow">
          <div className="ink" style={{ fontWeight: 600 }}>
            {item.job.title}没成功
          </div>
          <p className="small muted" style={{ marginTop: 2 }}>
            {item.job.detail}
            {item.job.recovery && `。建议：${item.job.recovery}`}
          </p>
        </div>
        <Button size="sm" onClick={() => dispatch({ type: 'retryJob', id: item.job.id })}>
          重试
        </Button>
      </div>
    )
  }
  return null
}

export function DemoBanner() {
  const { state, runDemoImport } = useStore()
  const job = state.jobs.find((j) => j.id === 'j_demo')
  const running = job?.status === 'running'
  if (state.demo.costReportImported && !running) return null
  return (
    <div className="banner">
      <FileUp size={18} style={{ color: 'var(--accent)', flex: 'none' }} />
      <div className="grow">
        <div className="ink" style={{ fontWeight: 600 }}>
          演示：导入资料后，搁置的想法自己回来
        </div>
        {running ? (
          <div className="stack-sm" style={{ marginTop: 4 }}>
            <span className="small muted">{job.detail}</span>
            <Progress value={job.progress ?? 0} />
          </div>
        ) : (
          <p className="small muted">「用本地小模型做记忆提取」因为缺成本数据被放下了。导入一份成本测算试试。</p>
        )}
      </div>
      {!running && (
        <Button variant="primary" size="sm" onClick={runDemoImport}>
          导入成本测算
        </Button>
      )}
    </div>
  )
}

export function TodayView() {
  const { state } = useStore()
  const decisions = attentionFor(state).filter((a) => isDecision(a) && a.kind !== 'due')
  const date = new Date().toLocaleDateString('zh-CN', { month: 'long', day: 'numeric', weekday: 'long' })

  return (
    <div className="view">
      <div className="view-head">
        <div>
          <h1>今天</h1>
          <p>{date}</p>
        </div>
      </div>
      <DemoBanner />
      {decisions.length > 0 && (
        <div className="group">
          <div className="group-head">
            需要你拿主意 <span className="n">{decisions.length}</span>
          </div>
          {decisions.map((d) => (
            <Decision key={d.key} item={d} />
          ))}
        </div>
      )}
      <TaskList groups={todayGroups(state)} empty="今天没有安排。" />
    </div>
  )
}
