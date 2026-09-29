import { Link, useParams } from 'react-router'
import { ArrowLeft, Lightbulb, Send } from 'lucide-react'
import { EpistemicBadge, TaskRow } from '../components/bits'
import { Badge, Button, Card, Empty } from '../components/ui'
import { handoffStatusLabel, ideaStatusLabel, projectStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import { useStore } from '../store/context'
import { useCreateHandoff, useDetail } from '../store/hooks'
import { NotFound } from './NotFound'

export function ProjectPage() {
  const { id } = useParams()
  const { state } = useStore()
  const { openTask, openIdea } = useDetail()
  const createHandoff = useCreateHandoff()
  const project = state.projects.find((p) => p.id === id)
  if (!project) return <NotFound />

  const tasks = state.tasks.filter((t) => t.projectId === project.id && t.status !== 'done' && t.status !== 'cancelled')
  const ideas = state.ideas.filter((i) => i.projectId === project.id && i.status !== 'dropped' && i.status !== 'promoted')
  const decisions = state.memories.filter((m) => m.projectId === project.id && m.kind === 'decision')
  const handoffs = state.handoffs.filter((h) => h.projectId === project.id)

  return (
    <main className="page">
      <Link to="/projects" className="chip" style={{ marginBottom: 10 }}>
        <ArrowLeft size={14} /> 全部项目
      </Link>
      <div className="page-head">
        <div>
          <div className="row">
            <h1>{project.name}</h1>
            <Badge tone={projectStatusLabel[project.status].tone}>{projectStatusLabel[project.status].text}</Badge>
          </div>
          <p>{project.goal}</p>
        </div>
        <Button variant="primary" icon={<Send size={15} />} onClick={() => createHandoff({ projectId: project.id })}>
          交给外部 AI
        </Button>
      </div>

      <div className="stack">
        <div className="grid-2">
          <Card title="当前进度" hint={`${formatAgo(project.updatedAt)}更新`} pad>
            <p className="pre">{project.progress}</p>
          </Card>
          <Card title="下一步" pad>
            <ol style={{ margin: 0, paddingLeft: 18 }} className="stack-sm">
              {project.nextSteps.map((s) => (
                <li key={s}>{s}</li>
              ))}
            </ol>
          </Card>
        </div>

        <Card title="已有结论" hint="项目里的决定；AI 推测和计划单独标出">
          {decisions.length === 0 ? (
            <Empty>还没有记录决定。</Empty>
          ) : (
            <div className="list">
              {decisions.map((m) => (
                <div key={m.id} className="list-item">
                  <div className="grow">
                    <div className="ink">{m.text}</div>
                  </div>
                  <EpistemicBadge value={m.epistemic} />
                </div>
              ))}
            </div>
          )}
        </Card>

        <div className="grid-2">
          <Card title={`任务 · ${tasks.length}`}>
            {tasks.length === 0 ? (
              <Empty>没有未完成的任务。</Empty>
            ) : (
              <div className="list">
                {tasks.map((t) => (
                  <TaskRow key={t.id} task={t} onOpen={() => openTask(t.id)} showProject={false} />
                ))}
              </div>
            )}
          </Card>
          <Card title={`想法 · ${ideas.length}`}>
            {ideas.length === 0 ? (
              <Empty>没有进行中的想法。</Empty>
            ) : (
              <div className="list">
                {ideas.map((i) => (
                  <div key={i.id} className="list-item clickable" onClick={() => openIdea(i.id)}>
                    <Lightbulb size={16} style={{ flex: 'none', marginTop: 3, color: 'var(--faint)' }} />
                    <div className="grow">
                      <div className="item-title">{i.title}</div>
                    </div>
                    <Badge tone={ideaStatusLabel[i.status].tone}>{ideaStatusLabel[i.status].text}</Badge>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>

        {handoffs.length > 0 && (
          <Card title="交接记录">
            <div className="list">
              {handoffs.map((h) => (
                <Link key={h.id} to={`/handoffs/${h.id}`} className="list-item clickable" style={{ color: 'inherit', textDecoration: 'none' }}>
                  <div className="grow">
                    <div className="item-title">{h.title}</div>
                    <div className="meta">
                      <span>{state.agents.find((a) => a.id === h.agentId)?.name}</span>
                      <span>{formatAgo(h.updatedAt)}</span>
                    </div>
                  </div>
                  <Badge tone={handoffStatusLabel[h.status].tone}>{handoffStatusLabel[h.status].text}</Badge>
                </Link>
              ))}
            </div>
          </Card>
        )}
      </div>
    </main>
  )
}
