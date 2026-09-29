import { useState } from 'react'
import { Link } from 'react-router'
import { Plus } from 'lucide-react'
import { Badge, Button, Card, Empty } from '../components/ui'
import { handoffStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import { useStore } from '../store/context'
import { useCreateHandoff } from '../store/hooks'

export function HandoffsPage() {
  const { state } = useStore()
  const createHandoff = useCreateHandoff()
  const [project, setProject] = useState(state.projects[0]?.id ?? '')

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>交接</h1>
          <p>把一项工作连同目标、背景、进度和决定交给外部 AI；结果回来后写回原事项。</p>
        </div>
        <div className="row">
          <select className="select" style={{ width: 'auto' }} value={project} onChange={(e) => setProject(e.target.value)} aria-label="项目">
            {state.projects.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}
              </option>
            ))}
          </select>
          <Button variant="primary" icon={<Plus size={15} />} onClick={() => createHandoff({ projectId: project })}>
            新建交接
          </Button>
        </div>
      </div>

      <Card>
        {state.handoffs.length === 0 ? (
          <Empty>还没有交接。也可以从任务、IDEA 或项目页面直接发起。</Empty>
        ) : (
          <div className="list">
            {state.handoffs.map((h) => (
              <Link key={h.id} to={`/handoffs/${h.id}`} className="list-item clickable" style={{ color: 'inherit', textDecoration: 'none' }}>
                <div className="grow">
                  <div className="item-title">{h.title}</div>
                  <div className="meta">
                    <span>交给 {state.agents.find((a) => a.id === h.agentId)?.name}</span>
                    <span>{state.projects.find((p) => p.id === h.projectId)?.name}</span>
                    <span>{formatAgo(h.updatedAt)}</span>
                    {h.stale && <Badge tone="warning">记忆有变化</Badge>}
                  </div>
                </div>
                <Badge tone={handoffStatusLabel[h.status].tone}>{handoffStatusLabel[h.status].text}</Badge>
              </Link>
            ))}
          </div>
        )}
      </Card>
    </main>
  )
}
