import { useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, GitBranch, RotateCw, ShieldAlert } from 'lucide-react'
import { jobStatusLabel, sourceStatusLabel, triggerLabel } from '../../domain/labels'
import { findThing } from '../../domain/things'
import {
  dateClassLabel,
  dateDecisionBadge,
  functionLabel,
  reasonLabel,
  recoveryStateLabel,
  topicDecisionBadge,
  type PausedJob,
} from '../../domain/observe'
import { formatAgo, formatShortWhen, formatWhen } from '../../domain/time'
import { useStore } from '../../store/context'
import { useActivityExplanations, useBackgroundHealth } from '../../store/observe'
import { useToast } from '../../store/toast'
import { Modal } from '../Overlay'
import { SourceSheet } from '../SourceSheet'
import { Button, Empty, Fold, Progress, Seg, Sheet, Spinner, Tag } from '../ui'

const jobRank = { failed: 0, running: 1, waiting: 2, queued: 3, done: 4 } as const

type DecisionView = 'topic' | 'date'

export function ObserveBackgroundTab({ onOpenTrace }: { onOpenTrace: (id: string) => void }) {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const tz = state.settings.timezone ?? 'UTC'
  const health = useBackgroundHealth(true)
  const explanations = useActivityExplanations(true)

  const [showDone, setShowDone] = useState(false)
  const [sourceId, setSourceId] = useState<string | null>(null)
  const [retryingId, setRetryingId] = useState<string | null>(null)
  const [authorizingJob, setAuthorizingJob] = useState<PausedJob | null>(null)
  const [decisionView, setDecisionView] = useState<DecisionView>('topic')

  const finishedCount = state.jobs.filter((j) => j.status === 'done').length
  const failedCount = state.jobs.filter((j) => j.status === 'failed').length
  const runningCount = state.jobs.filter((j) => j.status === 'running' || j.status === 'queued' || j.status === 'waiting').length
  const jobs = [...state.jobs].filter((j) => showDone || j.status !== 'done').sort((a, b) => jobRank[a.status] - jobRank[b.status])
  const attentionSources = state.sources.filter((s) => s.status === 'failed' || s.status === 'syncing' || s.status === 'unverified')

  const pausedJobs = explanations.data?.pausedJobs ?? []
  const recoveryPausedCount = pausedJobs.filter((j) => j.requiresOwnerRecovery).length
  const topicDecisions = explanations.data?.topicDecisions ?? []
  const dateDecisions = explanations.data?.dateDecisions ?? []

  const totalDeferred = health.data?.stages.reduce((sum, s) => sum + s.deferred, 0) ?? 0
  const totalStageFailures = health.data?.stages.reduce((sum, s) => sum + s.failure, 0) ?? 0
  const totalOverflow = health.data?.overflow.reduce((sum, o) => sum + o.count, 0) ?? 0

  const reloadAll = () => {
    health.reload()
    explanations.reload()
  }

  const handleRetry = async (jobId: string, authorizedLabel?: string) => {
    setRetryingId(jobId)
    const ok = await dispatch({ type: 'retryJob', id: jobId })
    setRetryingId(null)
    if (ok && authorizedLabel) toast.show(authorizedLabel)
    reloadAll()
  }

  return (
    <div className="stack">
      <div className="obs-toolbar">
        <span className="small muted">后台阶段健康、需要你授权恢复的暂停链路，以及「为什么没建项目 / 如何处理日期」的判定记录；正常预算裁剪与真实失败分开显示。</span>
        <Button size="sm" variant="quiet" icon={<RotateCw size={13} />} onClick={reloadAll}>
          刷新状态
        </Button>
      </div>

      <div className="obs-overview" role="group" aria-label="后台状态概览">
        <div className={`obs-tile${failedCount > 0 || recoveryPausedCount > 0 ? ' danger' : ''}`}>
          <span className="k">失败 / 待授权恢复</span>
          <span className="v">
            {failedCount} / {recoveryPausedCount}
          </span>
          <span className="d">{recoveryPausedCount > 0 ? '暂停链路需你显式授权重启' : failedCount > 0 ? '可在下方原地重试' : '当前没有卡住的任务'}</span>
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
        <div className={`obs-tile${totalStageFailures > 0 ? ' danger' : ''}`}>
          <span className="k">近 1 小时阶段失败</span>
          <span className="v">{totalStageFailures} 次</span>
          <span className="d">
            {totalOverflow > 0
              ? `另有 ${totalOverflow} 次正常预算裁剪`
              : state.organize
                ? `整理进度 ${state.organize.done}/${state.organize.total}`
                : '按阶段限流运行'}
          </span>
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

      {explanations.phase === 'failed' && (
        <div className="obs-problem" role="alert">
          <span>{explanations.problem}</span>
          <Button size="sm" onClick={explanations.reload}>
            重试
          </Button>
        </div>
      )}

      {pausedJobs.length > 0 && (
        <Sheet
          title={<h3>暂停与失败的后台链路（{pausedJobs.length}）</h3>}
          aside={<span className="tiny muted">自动恢复耗尽或超链深的任务不会自动重跑</span>}
        >
          <div className="list">
            {pausedJobs.map((job) => (
              <div key={job.jobId} className={`item${job.requiresOwnerRecovery ? ' obs-paused-item' : ''}`}>
                <div className="grow stack-sm" style={{ gap: 4 }}>
                  <div className="obs-event-head">
                    <div className="obs-event-title">
                      <Tag tone="danger">{job.requiresOwnerRecovery ? '需你授权恢复' : job.state === 'blocked' ? '已阻塞' : '失败'}</Tag>
                      <span className="item-title">{functionLabel(job.baseStage)}</span>
                      {job.chainDepth > 0 && <Tag tone={job.chainDepth > 200 ? 'danger' : 'neutral'}>链深 L{job.chainDepth}</Tag>}
                    </div>
                    <span className="obs-event-time">{formatShortWhen(job.updatedAt, tz)}</span>
                  </div>
                  <div className="meta" style={{ marginTop: 0 }}>
                    <span className="obs-mono">{job.baseStage}</span>
                    <span>队列尝试 {job.attempts} 次</span>
                    {job.attempt !== undefined && job.attemptLimit !== undefined && (
                      <span>
                        调用恢复 {job.attempt}/{job.attemptLimit}
                      </span>
                    )}
                    {job.recoveryState && <span>状态：{recoveryStateLabel(job.recoveryState)}</span>}
                  </div>
                  {(job.errorCode || job.recoveryReason) && (
                    <div className="small warn-text">
                      原因：{[job.errorCode ? reasonLabel(job.errorCode) : '', job.recoveryReason ? reasonLabel(job.recoveryReason) : ''].filter(Boolean).join('；')}
                    </div>
                  )}
                  <div className="obs-event-actions">
                    <button type="button" className="obs-link-pill" onClick={() => onOpenTrace(job.jobId)}>
                      <GitBranch size={12} />
                      查看因果链
                    </button>
                    {job.requiresOwnerRecovery ? (
                      <Button
                        size="sm"
                        variant="primary"
                        disabled={retryingId === job.jobId}
                        icon={<ShieldAlert size={13} />}
                        onClick={() => setAuthorizingJob(job)}
                      >
                        {retryingId === job.jobId ? '正在授权…' : '授权新一轮恢复重试'}
                      </Button>
                    ) : (
                      <Button
                        size="sm"
                        disabled={retryingId === job.jobId}
                        icon={<RotateCw size={13} />}
                        onClick={() => void handleRetry(job.jobId, '已重新排队')}
                      >
                        {retryingId === job.jobId ? '正在重试…' : '重试任务'}
                      </Button>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </Sheet>
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
                  <div className="obs-event-actions">
                    <button type="button" className="obs-link-pill" onClick={() => onOpenTrace(job.id)}>
                      <GitBranch size={12} />
                      查看因果链
                    </button>
                    {job.status === 'failed' && (
                      <>
                        {job.recovery && <span className="small muted">建议：{job.recovery}</span>}
                        <Button
                          size="sm"
                          disabled={retryingId === job.id}
                          icon={<RotateCw size={13} />}
                          onClick={() => void handleRetry(job.id)}
                        >
                          {retryingId === job.id ? '正在重试…' : '重试'}
                        </Button>
                      </>
                    )}
                  </div>
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

      <Sheet
        title={<h3>自动判定与未触发原因</h3>}
        aside={
          <Seg<DecisionView>
            label="自动判定类型"
            value={decisionView}
            onChange={setDecisionView}
            items={[
              { value: 'topic', label: '自动主题项目', count: topicDecisions.length },
              { value: 'date', label: '日期复查', count: dateDecisions.length },
            ]}
          />
        }
      >
        {explanations.phase === 'loading' && !explanations.data && (
          <div className="sheet-pad">
            <p className="row muted">
              <Spinner /> 正在读取自动判定记录…
            </p>
          </div>
        )}
        {decisionView === 'topic' && explanations.data && (
          topicDecisions.length === 0 ? (
            <Empty>还没有自动主题项目的判定或跳过记录。</Empty>
          ) : (
            <div className="list">
              {topicDecisions.map((item, idx) => {
                const badge = topicDecisionBadge(item.decision)
                return (
                  <div key={`${item.topicId}:${item.page}:${idx}`} className="item">
                    <div className="grow stack-sm" style={{ gap: 4 }}>
                      <div className="obs-event-head">
                        <div className="obs-event-title">
                          <Tag tone={badge.tone}>{badge.text}</Tag>
                          <span className="item-title">{item.topicName || `主题 ${item.topicId.slice(0, 8)}`}</span>
                          {item.proposedName && item.proposedName !== item.topicName && (
                            <span className="tiny muted">提案名：{item.proposedName}</span>
                          )}
                          {item.undoneAt && <Tag tone="neutral">已撤销</Tag>}
                        </div>
                        {item.linkedAt && <span className="obs-event-time">{formatShortWhen(item.linkedAt, tz)}</span>}
                      </div>
                      {item.reason && <div className="small muted">判定理由：{item.reason}</div>}
                      <div className="obs-event-actions">
                        <Link className="obs-link-pill" to={`/library?group=${encodeURIComponent(item.topicId)}`}>
                          看主题记忆
                          <ArrowUpRight size={12} />
                        </Link>
                        {item.projectId &&
                          (findThing(state, item.projectId) ? (
                            <Link className="obs-link-pill" to={`/t/${item.projectId}`}>
                              打开对应项目
                              <ArrowUpRight size={12} />
                            </Link>
                          ) : (
                            <span className="obs-dead-ref">原项目已删除（{item.projectId.slice(0, 6)}）</span>
                          ))}
                        {item.actionId && (
                          <button type="button" className="obs-link-pill" onClick={() => onOpenTrace(item.actionId!)}>
                            <GitBranch size={12} />
                            溯源动作
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )
        )}
        {decisionView === 'date' && explanations.data && (
          dateDecisions.length === 0 ? (
            <Empty>还没有后台日期复查的判定记录。</Empty>
          ) : (
            <div className="list">
              {dateDecisions.map((d) => {
                const badge = dateDecisionBadge(d.decision)
                return (
                  <div key={`${d.claimId}:${d.claimVersion}:${d.class}`} className="item">
                    <div className="grow stack-sm" style={{ gap: 4 }}>
                      <div className="obs-event-head">
                        <div className="obs-event-title">
                          <Tag tone={badge.tone}>{badge.text}</Tag>
                          <Tag>{dateClassLabel(d.class)}</Tag>
                          <span className="item-title">{d.title || d.entryTitle || `记忆 ${d.claimId.slice(0, 8)}`}</span>
                        </div>
                        <span className="obs-event-time">{formatShortWhen(d.createdAt, tz)}</span>
                      </div>
                      {d.reason && <div className="small muted">判定理由：{d.reason}</div>}
                      <div className="obs-event-actions">
                        <Link className="obs-link-pill" to={`/library?m=${encodeURIComponent(d.claimId)}`}>
                          看来源记忆（v{d.claimVersion}）
                          <ArrowUpRight size={12} />
                        </Link>
                        {d.itemId &&
                          (findThing(state, d.itemId) ? (
                            <Link className="obs-link-pill" to={`/t/${d.itemId}`}>
                              打开生成的事项
                              <ArrowUpRight size={12} />
                            </Link>
                          ) : (
                            <span className="obs-dead-ref">原事项已删除（{d.itemId.slice(0, 6)}）</span>
                          ))}
                        {d.actionId && (
                          <button type="button" className="obs-link-pill" onClick={() => onOpenTrace(d.actionId!)}>
                            <GitBranch size={12} />
                            溯源动作
                          </button>
                        )}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )
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
                        {s.overflow > 0 && <Tag tone="neutral">预算裁剪 {s.overflow}</Tag>}
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
                    {s.lastFailure && <div className="small warn-text">近 1 小时最近失败：{reasonLabel(s.lastFailure)}</div>}
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

      {health.data && health.data.overflow.length > 0 && (
        <Sheet>
          <Fold title="近 1 小时预算与上下文裁剪记录" summary={`${totalOverflow} 次（正常上下文配额保护，非异常）`}>
            <ul className="obs-gaps">
              {health.data.overflow.map((o, i) => (
                <li key={`${o.stage}:${o.reason}:${i}`}>
                  {functionLabel(o.stage)}：{reasonLabel(o.reason)}（× {o.count}）
                </li>
              ))}
            </ul>
          </Fold>
        </Sheet>
      )}

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

      {authorizingJob && (
        <Modal
          title="授权新一轮恢复重试？"
          onClose={() => setAuthorizingJob(null)}
          actions={
            <>
              <Button variant="quiet" onClick={() => setAuthorizingJob(null)}>
                取消
              </Button>
              <Button
                variant="primary"
                autoFocus
                onClick={async () => {
                  const target = authorizingJob
                  setAuthorizingJob(null)
                  await handleRetry(target.jobId, '已授权新一轮恢复重试并重新入队')
                }}
              >
                确认授权并重排
              </Button>
            </>
          }
        >
          <p className="muted">
            任务「{functionLabel(authorizingJob.baseStage)}」因「
            {reasonLabel(authorizingJob.errorCode || authorizingJob.recoveryReason || '') || '恢复上限'}
            」已停止自动重试。
          </p>
          <p className="small muted">
            确认后会保留原有历史调用与计费记录，并以本次请求 ID 关联授权新一轮有界恢复重试。
          </p>
          {authorizingJob.errorCode === 'chain_limit' && (
            <p className="small warn-text">
              注意：该任务链深已超过 200，重新入队仍保留原链深；请先排查循环原因，否则下轮巡检会再次按链深上限拦截。
            </p>
          )}
        </Modal>
      )}

      {sourceId && <SourceSheet id={sourceId} onClose={() => setSourceId(null)} />}
    </div>
  )
}
