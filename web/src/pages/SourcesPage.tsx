import { RotateCw, Upload } from 'lucide-react'
import { Badge, Button, Card, Progress } from '../components/ui'
import { jobStatusLabel, sourceStatusLabel, triggerLabel } from '../domain/labels'
import { formatAgo, formatWhen } from '../domain/time'
import { useStore } from '../store/context'

export function SourcesPage() {
  const { state, dispatch, runDemoImport } = useStore()
  const jobs = [...state.jobs].sort((a, b) => {
    const rank = { running: 0, failed: 1, waiting: 2, queued: 3, done: 4 }
    return rank[a.status] - rank[b.status]
  })

  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>来源与作业</h1>
          <p>资料从哪里来，以及后台正在做什么。后台作业和你的待办是分开的。</p>
        </div>
        <Button icon={<Upload size={15} />} disabled={state.demo.costReportImported} onClick={runDemoImport} title={state.demo.costReportImported ? '原型里只能演示一次导入' : undefined}>
          导入资料
        </Button>
      </div>

      <div className="stack">
        <div className="grid-3">
          {state.sources.map((s) => (
            <div key={s.id} className="card card-pad source-card">
              <div className="row-between">
                <div className="row">
                  <div className="source-icon">{s.name.slice(0, 1)}</div>
                  <div>
                    <div className="ink" style={{ fontWeight: 600 }}>{s.name}</div>
                    <div className="small muted">{s.method}</div>
                  </div>
                </div>
                <Badge tone={sourceStatusLabel[s.status].tone}>{sourceStatusLabel[s.status].text}</Badge>
              </div>
              <p className="small">{s.note}</p>
              <div className="meta">
                <span>{s.itemCount} 条资料</span>
                {s.lastSyncAt && <span>{formatAgo(s.lastSyncAt)}更新</span>}
              </div>
            </div>
          ))}
        </div>

        <Card title="后台作业" hint="事件驱动和时间驱动共用一套作业；重复导入或重试不会产生重复内容">
          <div className="list">
            {jobs.map((job) => (
              <div key={job.id} className="list-item">
                <div className="grow stack-sm" style={{ gap: 4 }}>
                  <div className="row-between">
                    <span className="item-title">{job.title}</span>
                    <Badge tone={jobStatusLabel[job.status].tone}>{jobStatusLabel[job.status].text}</Badge>
                  </div>
                  <div className="meta" style={{ marginTop: 0 }}>
                    <span>{triggerLabel[job.trigger]}</span>
                    <span>{job.detail}</span>
                    {job.nextRunAt && job.status !== 'done' && <span>下次运行：{formatWhen(job.nextRunAt)}</span>}
                  </div>
                  {job.status === 'running' && job.progress !== undefined && <Progress value={job.progress} />}
                  {job.status === 'failed' && (
                    <div className="row">
                      {job.recovery && <span className="small">建议：{job.recovery}</span>}
                      <Button size="sm" icon={<RotateCw size={13} />} onClick={() => dispatch({ type: 'retryJob', id: job.id })}>
                        重试
                      </Button>
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>
        </Card>
      </div>
    </main>
  )
}
