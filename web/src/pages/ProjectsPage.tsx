import { Link } from 'react-router'
import { Badge } from '../components/ui'
import { projectStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import { useStore } from '../store/context'

export function ProjectsPage() {
  const { state } = useStore()
  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>项目</h1>
          <p>每个项目有自己的入口：随时切走，回来还能看到进度、结论和下一步。</p>
        </div>
      </div>
      <div className="grid-3">
        {state.projects.map((p) => {
          const open = state.tasks.filter((t) => t.projectId === p.id && t.status !== 'done' && t.status !== 'cancelled').length
          const ideas = state.ideas.filter((i) => i.projectId === p.id && (i.status === 'active' || i.status === 'awakened' || i.status === 'shelved')).length
          const decisions = state.memories.filter((m) => m.projectId === p.id && m.kind === 'decision' && m.epistemic === 'confirmed').length
          return (
            <Link key={p.id} to={`/projects/${p.id}`} className="card card-pad stack-sm" style={{ textDecoration: 'none', color: 'inherit' }}>
              <div className="row-between">
                <h2>{p.name}</h2>
                <Badge tone={projectStatusLabel[p.status].tone}>{projectStatusLabel[p.status].text}</Badge>
              </div>
              <p className="muted">{p.goal}</p>
              {p.nextSteps[0] && (
                <p className="small">
                  <span className="muted">下一步：</span>
                  {p.nextSteps[0]}
                </p>
              )}
              <div className="meta">
                <span>{open} 项任务</span>
                <span>{ideas} 个想法</span>
                <span>{decisions} 个决定</span>
                <span>{formatAgo(p.updatedAt)}更新</span>
              </div>
            </Link>
          )
        })}
      </div>
    </main>
  )
}
