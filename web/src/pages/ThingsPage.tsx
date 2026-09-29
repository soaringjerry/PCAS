import { useState } from 'react'
import { Link, useSearchParams } from 'react-router'
import { Search } from 'lucide-react'
import { KindLabel, ProjectLink } from '../components/Marks'
import { Empty, Seg, Sheet, Tag } from '../components/ui'
import { allThings, thingProjectId, thingStatus, thingTitle, type Thing } from '../domain/things'
import { formatAgo } from '../domain/time'
import { useStore } from '../store/context'

type View = 'moving' | 'shelved' | 'closed'
type KindFilter = 'all' | Thing['kind']

function viewOf(thing: Thing): View {
  if (thing.kind === 'project') return thing.item.status === 'done' ? 'closed' : thing.item.status === 'paused' ? 'shelved' : 'moving'
  if (thing.kind === 'idea') {
    if (thing.item.status === 'shelved') return 'shelved'
    if (thing.item.status === 'dropped' || thing.item.status === 'promoted') return 'closed'
    return 'moving'
  }
  if (thing.item.status === 'done' || thing.item.status === 'cancelled') return 'closed'
  return 'moving'
}

function subline(thing: Thing, state: ReturnType<typeof useStore>['state']): string {
  if (thing.kind === 'project') {
    const open = state.tasks.filter((t) => t.projectId === thing.id && t.status !== 'done' && t.status !== 'cancelled').length
    return `${open} 件待办${thing.item.nextSteps[0] ? ` · 下一步：${thing.item.nextSteps[0]}` : ''}`
  }
  if (thing.kind === 'idea') {
    const idea = thing.item
    if (idea.status === 'shelved') {
      const waiting = idea.conditions.filter((c) => !c.met).map((c) => c.description)
      return waiting.length ? `等：${waiting[0]}` : (idea.shelvedReason ?? '')
    }
    return idea.body
  }
  return thing.item.status === 'waiting' ? `等：${thing.item.waitingFor ?? ''}` : (thing.item.notes ?? '')
}

export function ThingsPage() {
  const { state } = useStore()
  const [params, setParams] = useSearchParams()
  const view = (params.get('view') as View) || 'moving'
  const [kind, setKind] = useState<KindFilter>('all')
  const [query, setQuery] = useState('')

  const things = allThings(state)
  const counts = { moving: 0, shelved: 0, closed: 0 }
  for (const t of things) counts[viewOf(t)] += 1

  const shown = things
    .filter((t) => viewOf(t) === view)
    .filter((t) => kind === 'all' || t.kind === kind)
    .filter((t) => !query.trim() || thingTitle(t).includes(query.trim()))
    .sort((a, b) => b.item.updatedAt.localeCompare(a.item.updatedAt))

  return (
    <main className="page page-narrow">
      <div className="page-head">
        <div>
          <h1>事情</h1>
          <p>项目、想法和待办都在这里。它们会变：想法可以变成待办，待办会聚成项目。</p>
        </div>
      </div>

      <div className="toolbar">
        <Seg
          label="范围"
          value={view}
          onChange={(v) => setParams(v === 'moving' ? {} : { view: v })}
          items={[
            { value: 'moving', label: '推进中', count: counts.moving },
            { value: 'shelved', label: '放着的', count: counts.shelved },
            { value: 'closed', label: '结束了', count: counts.closed },
          ]}
        />
        <Seg
          label="类型"
          value={kind}
          onChange={setKind}
          items={[
            { value: 'all', label: '全部' },
            { value: 'project', label: '项目' },
            { value: 'idea', label: '想法' },
            { value: 'task', label: '待办' },
          ]}
        />
        <label className="search">
          <Search size={15} />
          <input placeholder="找一件事…" value={query} onChange={(e) => setQuery(e.target.value)} aria-label="搜索事情" />
        </label>
      </div>

      <Sheet>
        {shown.length === 0 ? (
          <Empty>{view === 'shelved' ? '没有放着的事。' : view === 'closed' ? '还没有结束的事。' : '这里空空的。按 ⌘K 记一笔吧。'}</Empty>
        ) : (
          <div className="list">
            {shown.map((thing) => {
              const status = thingStatus(thing)
              const sub = subline(thing, state)
              return (
                <Link key={thing.id} to={`/t/${thing.id}`} className="item link">
                  <div className="grow">
                    <div className="row-nowrap">
                      <KindLabel kind={thing.kind} />
                      <span className={`item-title${view === 'closed' ? ' done' : ''}`} style={{ fontSize: thing.kind === 'project' ? 16 : 15 }}>
                        {thingTitle(thing)}
                      </span>
                    </div>
                    {sub && (
                      <p className="small muted" style={{ marginTop: 2, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {sub}
                      </p>
                    )}
                    <div className="meta">
                      <ProjectLink id={thingProjectId(thing)} />
                      <span>{formatAgo(thing.item.updatedAt)}</span>
                    </div>
                  </div>
                  <Tag tone={status.tone}>{status.text}</Tag>
                </Link>
              )
            })}
          </div>
        )}
      </Sheet>
    </main>
  )
}
