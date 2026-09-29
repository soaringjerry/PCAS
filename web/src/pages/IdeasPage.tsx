import { useState } from 'react'
import { Lightbulb } from 'lucide-react'
import { ProjectName } from '../components/bits'
import { Badge, Card, Empty, Tabs } from '../components/ui'
import { ideaStatusLabel } from '../domain/labels'
import { formatAgo } from '../domain/time'
import type { IdeaStatus } from '../domain/types'
import { useStore } from '../store/context'
import { useDetail } from '../store/hooks'

const order: IdeaStatus[] = ['awakened', 'active', 'shelved', 'promoted', 'dropped']

export function IdeasPage() {
  const { state } = useStore()
  const { openIdea } = useDetail()
  const [tab, setTab] = useState<IdeaStatus | 'all'>('all')
  const ideas = state.ideas
    .filter((i) => tab === 'all' || i.status === tab)
    .sort((a, b) => order.indexOf(a.status) - order.indexOf(b.status))

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>IDEA</h1>
          <p>想法和它的演变。搁置的想法写下条件，到时间或条件满足时会被带回来。</p>
        </div>
        <Tabs
          value={tab}
          onChange={setTab}
          items={[
            { value: 'all', label: '全部', count: state.ideas.length },
            ...order.map((s) => ({
              value: s,
              label: ideaStatusLabel[s].text,
              count: state.ideas.filter((i) => i.status === s).length,
            })),
          ]}
        />
      </div>

      <Card>
        {ideas.length === 0 ? (
          <Empty>这里还没有想法。</Empty>
        ) : (
          <div className="list">
            {ideas.map((idea) => {
              const met = idea.conditions.filter((c) => c.met).length
              return (
                <div key={idea.id} className="list-item clickable" onClick={() => openIdea(idea.id)}>
                  <Lightbulb size={18} style={{ flex: 'none', marginTop: 2, color: idea.status === 'awakened' ? 'var(--accent)' : 'var(--faint)' }} />
                  <div className="grow">
                    <div className={`item-title${idea.status === 'dropped' ? ' done' : ''}`}>{idea.title}</div>
                    <div className="meta">
                      <Badge tone={ideaStatusLabel[idea.status].tone}>{ideaStatusLabel[idea.status].text}</Badge>
                      {idea.conditions.length > 0 && (
                        <span>
                          条件 {met}/{idea.conditions.length} 满足
                        </span>
                      )}
                      {idea.status === 'shelved' && idea.shelvedReason && <span>搁置：{idea.shelvedReason}</span>}
                      <span>{formatAgo(idea.updatedAt)}更新</span>
                      <ProjectName id={idea.projectId} />
                    </div>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </Card>
    </main>
  )
}
