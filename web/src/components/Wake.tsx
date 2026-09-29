import { Send, Sparkles } from 'lucide-react'
import { formatAgo } from '../domain/time'
import type { Idea, Wake } from '../domain/types'
import { useStore } from '../store/context'
import { useCreateHandoff, useDetail } from '../store/hooks'
import { ProjectName } from './bits'
import { Button } from './ui'

export function WakeReason({ wake }: { wake: Wake }) {
  return (
    <div className="wake-reason">
      <Sparkles size={16} />
      <div>
        <div>{wake.reason}</div>
        <div className="small muted">{formatAgo(wake.at)}唤醒</div>
      </div>
    </div>
  )
}

/** PRD §5: continue, turn into a task, snooze or stop reminding. */
export function WakeActions({ idea }: { idea: Idea }) {
  const { dispatch } = useStore()
  const createHandoff = useCreateHandoff()
  return (
    <div className="row">
      <Button variant="primary" size="sm" onClick={() => dispatch({ type: 'ideaPromote', id: idea.id })}>
        转成任务
      </Button>
      <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
        继续推进
      </Button>
      <Button size="sm" icon={<Send size={14} />} onClick={() => createHandoff({ ideaId: idea.id })}>
        交给外部 AI
      </Button>
      <Button size="sm" variant="ghost" onClick={() => dispatch({ type: 'ideaSnooze', id: idea.id, days: 7 })}>
        延期一周
      </Button>
      <Button size="sm" variant="ghost" onClick={() => dispatch({ type: 'ideaStopReminders', id: idea.id })}>
        停止提醒
      </Button>
    </div>
  )
}

export function WakeCard({ idea }: { idea: Idea }) {
  const { openIdea } = useDetail()
  return (
    <div className="wake-card">
      <div className="row-between">
        <div className="row">
          <span className="badge badge-accent">
            <Sparkles size={12} /> 重新唤醒
          </span>
          <ProjectName id={idea.projectId} />
        </div>
        <Button size="sm" variant="ghost" onClick={() => openIdea(idea.id)}>
          详情
        </Button>
      </div>
      <h3 style={{ marginTop: 8, fontSize: 15 }}>{idea.title}</h3>
      {idea.wake && <WakeReason wake={idea.wake} />}
      <WakeActions idea={idea} />
    </div>
  )
}
