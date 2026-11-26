import { useState } from 'react'
import { Link } from 'react-router'
import { AlertTriangle, ArrowUpRight, RotateCw } from 'lucide-react'
import { jobStatusLabel, sourceStatusLabel, triggerLabel } from '../../domain/labels'
import { functionLabel, reasonLabel } from '../../domain/observe'
import { formatAgo, formatWhen } from '../../domain/time'
import { useStore } from '../../store/context'
import { useBackgroundHealth } from '../../store/observe'
import { SourceSheet } from '../SourceSheet'
import { Button, Empty, Progress, Sheet, Spinner, Tag } from '../ui'

const jobRank = { failed: 0, running: 1, waiting: 2, queued: 3, done: 4 } as const

export function ObserveBackgroundTab() {
  const { state, dispatch } = useStore()
  const tz = state.settings.timezone ?? 'UTC'
  const health = useBackgroundHealth(true)
  const [showDone, setShowDone] = useState(false)
  const [sourceId, setSourceId] = useState<string | null>(null)
  const [retryingId, setRetryingId] = useState<string | null>(null)

  const finishedCount = state.jobs.filter((j) => j.status === 'done').length
  const failedCount = state.jobs.filter((j) => j.status === 'failed').length
  const runningCount = state.jobs.filter((j) => j.status === 'running' || j.status === 'queued' || j.status === 'waiting').length
  const jobs = [...state.jobs].filter((j) => showDone || j.status !== 'done').sort((a, b) => jobRank[a.status] - jobRank[b.status])
  const attentionSources = state.sources.filter((s) => s.status === 'failed' || s.status === 'syncing' || s.status === 'unverified')

  const totalDeferred = health.data?.stages.reduce((sum, s) => sum + s.deferred, 0) ?? 0
  const totalStageFailures = health.data?.stages.reduce((sum, s) => sum + s.failure, 0) ?? 0
  const totalOverflow = health.data?.overflow.reduce((sum, o) => sum + o.count, 0) ?? 0

  const handleRetry = async (jobId: string) => {
    setRetryingId(jobId)
    await dispatch({ type: 'retryJob', id: jobId })
    setRetryingId(null)
    health.reload()
  }

  return (
    <div className="stack">
      <div className="obs-toolbar">
        <span className="small muted">最近 1 小时各后台阶段执行情况与队列恢复；延后等待与真实失败分开显示。</span>
        <Button size="sm" variant="quiet" icon={<RotateCw size={13} />} onClick={health.reload}>
          刷新健康状态
        </Button>
      </div>

      <div className="obs-overview" role="group" aria-label="后台状态概览">
        <div className={`obs-tile${failedCount > 0 ? ' danger' : ''}`}>
          <span className="k">队列失败任务</span>
          <span className="v">{failedCount} 项</span>
          <span className="d">{failedCount > 0 ? '可在下方原地重试' : '当前没有卡住的任务'}</span>
        </div>
        <div className="obs-tile">
          <span className="k">进行 / 排队中</span>
          <span className="v">{runningCount} 项</span>
          <span className="d">已完成 {finishedCount} 项</span>
        </div>
        <div className={`obs-tile${totalDeferred > 0 ? ' warn' : ''}`}>
          <span className="k">近 1 小时延后等待</span>
          <span className="v">{totalDeferred} 次</span>
          <span className="d">受小时限额或日预算保护</span>
        </div>
        <div className={`obs-tile${totalStageFailures > 0 || totalOverflow > 0 ? ' danger' : ''}`}>
          <span className="k">近 1 小时阶段失败 / 溢出</span>
          <span className="v">
            {totalStageFailures} / {totalOverflow}
          </span>
          <span className="d">{state.organize ? `整理进度 ${state.organize.done}/${state.organize.total}` : '按阶段限流运行'}</span>
        </div>
      </div>

      {health.phase === 'failed' && (
        <div className="obs-problem" role="alert">
          <span>{health.problem}</span>
          <Button size="sm" onClick={health.reload}>
            重试
          </Button>
        </div>
      )}

      {health.data && health.data.overflow.length > 0 && (
        <div className="callout callout-danger" role="alert">
          <AlertTriangle size={15} />
          <div>
            最近 1 小时出现超出上限记录：
            {health.data.overflow.map((o) => `${functionLabel(o.stage)}（${reasonLabel(o.reason)} × ${o.count}）`).join('；')}
          </div>
        </div>
      )}

      <Sheet title={<h3>后台在做的事</h3>} aside={<span className="tiny muted">和你的待办分开；失败任务可原地重试</span>}>
        {jobs.length === 0 ? (
          <Empty>现在没有在做或卡住的后台任务。</Empty>
        ) : (
          <div className="list">
            {jobs.map((job) => (
              <div key={job.id} className="item">
                <div className="grow stack-sm" style={{ gap: 4 }}>
                  <div className="spread">
                    <span className="item-title">{job.title}</span>
                    <Tag tone={jobStatusLabel[job.status].tone}>{jobStatusLabel[job.status].text}</Tag>
                  </div>
                  <div className="meta" style={{ marginTop: 0 }}>
                    <span>{triggerLabel[job.trigger]}</span>
                    <span>{job.detail}</span>
                    {job.nextRunAt && job.status !== 'done' && <span>下次：{formatWhen(job.nextRunAt, tz)}</span>}
                  </div>
                  {job.status === 'running' && job.progress !== undefined && <Progress value={job.progress} />}
                  {job.status === 'failed' && (
                    <div className="row">
                      {job.recovery && <span className="small">建议：{job.recovery}</span>}
                      <Button
                        size="sm"
                        disabled={retryingId === job.id}
                        icon={<RotateCw size={13} />}
                        onClick={() => void handleRetry(job.id)}
                      >
                        {retryingId === job.id ? '正在重试…' : '重试'}
                      </Button>
                    </div>
                  )}
                </div>
              </div>
            ))}
          </div>
        )}
        {finishedCount > 0 && (
          <button
            type="button"
            className="fact add"
            style={{ margin: '8px 16px 12px' }}
            aria-expanded={showDone}
            onClick={() => setShowDone((v) => !v)}
          >
            {showDone ? '收起已完成的' : `已完成 ${finishedCount} 项，点开看`}
          </button>
        )}
      </Sheet>

      <Sheet title={<h3>受限阶段每小时健康（近 1 小时）</h3>} aside={<span className="tiny muted">成功、延后等待与失败分开统计</span>}>
        {health.phase === 'loading' && !health.data && (
          <div className="sheet-pad">
            <p className="row muted">
              <Spinner /> 正在读取阶段健康…
            </p>
          </div>
        )}
        {health.data && health.data.stages.length === 0 && <Empty>暂无已登记的受限后台阶段。</Empty>}
        {health.data && health.data.stages.length > 0 && (
          <div className="list">
            {health.data.stages.map((s) => {
              const deferredEntries = Object.entries(s.deferredBy ?? {})
              const ratio = s.hourlyBudget > 0 ? Math.min(1, s.calls / s.hourlyBudget) : 0
              return (
                <div key={s.stage} className="item">
                  <div className="grow obs-stage-row">
                    <div className="spread">
                      <span className="item-title">
                        {functionLabel(s.stage)}
                        <span className="obs-mono muted aside">{s.stage}</span>
                      </span>
                      <div className="obs-stage-nums">
                        <Tag tone={s.success > 0 ? 'success' : 'neutral'}>成功 {s.success}</Tag>
                        <Tag tone={s.deferred > 0 ? 'warning' : 'neutral'}>延后等待 {s.deferred}</Tag>
                        <Tag tone={s.failure > 0 ? 'danger' : 'neutral'}>失败 {s.failure}</Tag>
                        {s.overflow > 0 && <Tag tone="danger">溢出 {s.overflow}</Tag>}
                      </div>
                    </div>
                    <div className="obs-stage-budget">
                      <span>
                        小时调用 {s.calls} / {s.hourlyBudget}
                      </span>
                      <Progress value={ratio} />
                    </div>
                    {deferredEntries.length > 0 && (
                      <div className="small muted">
                        延后原因：{deferredEntries.map(([r, n]) => `${reasonLabel(r)}（${n}）`).join('、')}
                      </div>
                    )}
                    {s.lastFailure && <div className="small warn-text">最近失败：{reasonLabel(s.lastFailure)}</div>}
                    {(s.withdrawn.deadlines > 0 || s.withdrawn.requirements > 0) && (
                      <div className="tiny muted">
                        重整理撤回：期限 {s.withdrawn.deadlines} · 助手要求 {s.withdrawn.requirements}
                      </div>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </Sheet>

      {attentionSources.length > 0 && (
        <Sheet
          title={<h3>需要留意的输入来源</h3>}
          aside={
            <Link className="obs-link-pill" to="/library?tab=sources">
              全部来源
              <ArrowUpRight size={12} />
            </Link>
          }
        >
          <div className="list">
            {attentionSources.map((src) => (
              <div key={src.id} className="item">
                <div className="grow stack-sm" style={{ gap: 2 }}>
                  <div className="spread">
                    <span className="item-title">{src.name}</span>
                    <Tag tone={sourceStatusLabel[src.status].tone}>{sourceStatusLabel[src.status].text}</Tag>
                  </div>
                  {src.note && <div className="small muted">{src.note}</div>}
                  <div className="meta" style={{ marginTop: 0 }}>
                    {!src.single && <span>{src.itemCount} 条</span>}
                    {src.lastSyncAt && <span>更新于 {formatAgo(src.lastSyncAt, tz)}</span>}
                  </div>
                </div>
                {src.single ? (
                  <Button size="sm" variant="quiet" onClick={() => setSourceId(src.id)}>
                    查看原文
                  </Button>
                ) : (
                  <Link className="btn btn-quiet btn-sm" to="/library?tab=sources">
                    去来源列表
                  </Link>
                )}
              </div>
            ))}
          </div>
        </Sheet>
      )}

      {sourceId && <SourceSheet id={sourceId} onClose={() => setSourceId(null)} />}
    </div>
  )
}
