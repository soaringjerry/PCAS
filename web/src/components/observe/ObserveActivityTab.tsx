import { useMemo, useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, ChevronLeft, ChevronRight, GitBranch, RotateCw, Undo2, X } from 'lucide-react'
import { findThing, thingTitle } from '../../domain/things'
import { civilDate, clockTime, formatCivil } from '../../domain/time'
import {
  accountingLabel,
  activityKindLabel,
  activityKindOrder,
  canOfferUndo,
  formatCost,
  formatDurationMs,
  formatTokens,
  functionLabel,
  gapLabel,
  isEventDeferred,
  isEventProblem,
  outcomeBadge,
  reasonLabel,
  shiftCivilDate,
  type ActivityEvent,
  type ActivityEventKind,
} from '../../domain/observe'
import { useStore } from '../../store/context'
import { useDayActivity } from '../../store/observe'
import { useToast } from '../../store/toast'
import { Chip } from '../controls'
import { Button, Empty, Fold, Sheet, Spinner, Tag } from '../ui'

export function ObserveActivityTab({
  date,
  onDateChange,
  onInspectCall,
  onOpenTrace,
}: {
  date: string
  onDateChange: (nextDate: string) => void
  onInspectCall: (usageId: string) => void
  onOpenTrace: (anchorId: string) => void
}) {
  const { state, undo } = useStore()
  const toast = useToast()
  const tz = state.settings.timezone ?? 'UTC'
  const today = civilDate(0, tz)
  const yesterday = civilDate(-1, tz)
  const { phase, data, problem, more, moreProblem, loadMore, reload } = useDayActivity(date, true)

  const [kindFilter, setKindFilter] = useState<ActivityEventKind | 'all'>('all')
  const [onlyIssues, setOnlyIssues] = useState(false)
  const [traceExecution, setTraceExecution] = useState<string | null>(null)
  const [undoingId, setUndoingId] = useState<string | null>(null)

  const events = useMemo(() => data?.events ?? [], [data])

  const counts = useMemo(() => {
    let failures = 0
    let deferrals = 0
    for (const e of events) {
      if (isEventProblem(e)) failures++
      if (isEventDeferred(e)) deferrals++
    }
    return { failures, deferrals }
  }, [events])

  const visibleEvents = useMemo(() => {
    return events.filter((e) => {
      if (traceExecution && e.executionId !== traceExecution && e.rootExecutionId !== traceExecution) return false
      if (kindFilter !== 'all' && e.kind !== kindFilter) return false
      if (onlyIssues && !isEventProblem(e) && !isEventDeferred(e)) return false
      return true
    })
  }, [events, kindFilter, onlyIssues, traceExecution])

  const handleUndo = async (actionId: string) => {
    setUndoingId(actionId)
    const ok = await undo(actionId)
    setUndoingId(null)
    if (ok) {
      toast.show('已撤销这项操作')
      reload()
    }
  }

  return (
    <div className="stack">
      <div className="obs-toolbar">
        <div className="obs-date-nav">
          <Button
            size="sm"
            aria-label="前一天"
            icon={<ChevronLeft size={14} />}
            onClick={() => {
              setTraceExecution(null)
              onDateChange(shiftCivilDate(date, -1))
            }}
          />
          <input
            type="date"
            className="input obs-date-input"
            aria-label="选择观察日期"
            value={date}
            max={today}
            onChange={(e) => {
              if (e.target.value) {
                setTraceExecution(null)
                onDateChange(e.target.value)
              }
            }}
          />
          <Button
            size="sm"
            aria-label="后一天"
            disabled={date >= today}
            icon={<ChevronRight size={14} />}
            onClick={() => {
              setTraceExecution(null)
              onDateChange(shiftCivilDate(date, 1))
            }}
          />
          <Button
            size="sm"
            variant={date === yesterday ? 'primary' : 'default'}
            onClick={() => {
              setTraceExecution(null)
              onDateChange(yesterday)
            }}
          >
            昨天
          </Button>
          <Button
            size="sm"
            variant={date === today ? 'primary' : 'default'}
            onClick={() => {
              setTraceExecution(null)
              onDateChange(today)
            }}
          >
            今天
          </Button>
        </div>
        <div className="row">
          <span className="tiny muted">
            {formatCivil(date, tz)} · 时区 {data?.timezone ?? tz}
          </span>
          <Button size="sm" variant="quiet" icon={<RotateCw size={13} />} onClick={reload}>
            刷新
          </Button>
        </div>
      </div>

      {phase === 'failed' && (
        <div className="obs-problem" role="alert">
          <span>{problem}</span>
          <Button size="sm" onClick={reload}>
            重试
          </Button>
        </div>
      )}

      {phase === 'loading' && !data && (
        <Sheet pad>
          <p className="row muted">
            <Spinner /> 正在读取 {date} 的活动记录…
          </p>
        </Sheet>
      )}

      {data && (
        <>
          <div className="obs-overview" role="group" aria-label="当日概览">
            <div className="obs-tile">
              <span className="k">当日记录</span>
              <span className="v">{data.eventCount} 条</span>
              <span className="d">
                {data.omittedEvents > 0 ? `已加载 ${data.events.length} 条，还有 ${data.omittedEvents} 条` : `调用 ${data.cost.invocations} 次`}
              </span>
            </div>
            <div className={`obs-tile${data.cost.unknownCostInvocations > 0 ? ' warn' : ''}`}>
              <span className="k">已结算花费</span>
              <span className="v">{formatCost(data.cost.settled)}</span>
              <span className="d">
                {data.cost.unknownCostInvocations > 0
                  ? `${data.cost.unknownCostInvocations} 次花费未知`
                  : data.cost.estimatedCostInvocations > 0
                    ? `含 ${data.cost.estimatedCostInvocations} 次估算`
                    : '按每次调用去重计费'}
              </span>
            </div>
            <div className={`obs-tile${data.cost.heldReservations > 0 ? ' warn' : ''}`}>
              <span className="k">预留未结算</span>
              <span className="v">{formatCost(data.cost.heldReservations)}</span>
              <span className="d">{data.cost.heldReservations > 0 ? '额度保护，非实际账单' : '当前无未结算预占'}</span>
            </div>
            <div className={`obs-tile${counts.failures > 0 ? ' danger' : counts.deferrals > 0 ? ' warn' : ''}`}>
              <span className="k">异常与延后</span>
              <span className="v">
                {counts.failures > 0 ? `${counts.failures} 项异常` : counts.deferrals > 0 ? `${counts.deferrals} 项延后` : '全部正常'}
              </span>
              <span className="d">
                失败/未知 {counts.failures} · 延后等待 {counts.deferrals}
              </span>
            </div>
          </div>

          {data.cost.byFunction.length > 0 && (
            <Sheet title={<h3>当日按功能汇总</h3>} aside={<span className="tiny muted">按调用汇总，不随动作数重复计算</span>}>
              <div className="list">
                {data.cost.byFunction.map((fn) => (
                  <div key={fn.function} className="item">
                    <div className="grow">
                      <div className="item-title">{functionLabel(fn.function)}</div>
                      <div className="meta">
                        <span>{fn.invocations} 次调用（返回 {fn.returned}）</span>
                        <span>输入 {formatTokens(fn.inputTokens)} / 输出 {formatTokens(fn.outputTokens)}</span>
                        {fn.unknownCost > 0 && <Tag tone="warning">{fn.unknownCost} 次花费未知</Tag>}
                      </div>
                    </div>
                    <span className="obs-event-cost">{formatCost(fn.settled)}</span>
                  </div>
                ))}
              </div>
            </Sheet>
          )}

          <div className="obs-filters" role="group" aria-label="按类型筛选活动">
            <Chip on={kindFilter === 'all'} onToggle={() => setKindFilter('all')}>
              全部（{data.eventCount}）
            </Chip>
            {activityKindOrder.map((kind) => {
              const n = data.kinds[kind] ?? 0
              if (n === 0 && kindFilter !== kind) return null
              return (
                <Chip key={kind} on={kindFilter === kind} onToggle={() => setKindFilter(kindFilter === kind ? 'all' : kind)}>
                  {activityKindLabel[kind]}（{n}）
                </Chip>
              )
            })}
            <Chip on={onlyIssues} onToggle={() => setOnlyIssues((v) => !v)}>
              只看异常与延后
            </Chip>
          </div>

          {traceExecution && (
            <div className="obs-trace-banner" role="status">
              <span>
                <GitBranch size={14} style={{ verticalAlign: '-2px', marginRight: 6 }} />
                正在按执行 ID 关联查看：<span className="obs-mono">{traceExecution.slice(0, 8)}</span>（共 {visibleEvents.length} 条记录）
              </span>
              <Button size="sm" variant="quiet" icon={<X size={13} />} onClick={() => setTraceExecution(null)}>
                清除关联筛选
              </Button>
            </div>
          )}

          <Sheet
            title={<h3>活动明细</h3>}
            aside={<span className="tiny muted">显示 {visibleEvents.length} 条</span>}
          >
            {visibleEvents.length === 0 ? (
              <Empty>{data.eventCount === 0 ? `${date} 这一天没有记录到系统活动。` : '当前筛选条件下没有匹配的记录。'}</Empty>
            ) : (
              <div className="list">
                {visibleEvents.map((ev) => (
                  <ActivityRow
                    key={`${ev.kind}:${ev.eventId}`}
                    event={ev}
                    timezone={data.timezone || tz}
                    undoing={undoingId === ev.eventId}
                    resolveThingTitle={(id) => {
                      const found = findThing(state, id)
                      return found ? thingTitle(found) : undefined
                    }}
                    onTrace={(execId) => setTraceExecution(execId)}
                    onOpenTrace={onOpenTrace}
                    onInspectCall={onInspectCall}
                    onUndo={(id) => void handleUndo(id)}
                  />
                ))}
              </div>
            )}
            {data.omittedEvents > 0 && (
              <div className="obs-page-footer row">
                <Button size="sm" disabled={more === 'loading'} onClick={loadMore}>
                  {more === 'loading' ? '正在加载…' : `再加载更多（还有 ${data.omittedEvents} 条）`}
                </Button>
                {more === 'failed' && (
                  <span className="small warn-text" role="alert">
                    {moreProblem}
                  </span>
                )}
              </div>
            )}
          </Sheet>

          {data.gaps.length > 0 && (
            <Sheet>
              <Fold title="已知记录缺口说明" summary={`${data.gaps.length} 条（不按时间猜测关联）`}>
                <ul className="obs-gaps">
                  {data.gaps.map((g, i) => (
                    <li key={i}>{gapLabel(g)}</li>
                  ))}
                </ul>
              </Fold>
            </Sheet>
          )}
        </>
      )}
    </div>
  )
}

function ActivityRow({
  event,
  timezone,
  undoing,
  resolveThingTitle,
  onTrace,
  onOpenTrace,
  onInspectCall,
  onUndo,
}: {
  event: ActivityEvent
  timezone: string
  undoing: boolean
  resolveThingTitle: (id: string) => string | undefined
  onTrace: (execId: string) => void
  onOpenTrace: (anchorId: string) => void
  onInspectCall: (usageId: string) => void
  onUndo: (actionId: string) => void
}) {
  const badge = outcomeBadge(event.outcome)
  const ev = event.evidence
  const reason = reasonLabel(event.reason)
  const items = Array.isArray(ev.items) ? ev.items : []
  const thingIds = [...new Set([...items, ...(ev.thing ? [ev.thing] : []), ...(ev.item ? [ev.item] : [])])]
  const showUndo = canOfferUndo(event)
  const traceAnchor =
    event.executionId ||
    (event.kind === 'action' || event.kind === 'call' || event.kind === 'run' ? event.eventId : event.kind === 'undo' && ev.undoOf ? ev.undoOf : '')

  const headline =
    event.kind === 'action' || event.kind === 'undo' || event.kind === 'decision'
      ? event.summary || functionLabel(event.function)
      : `${functionLabel(event.function)}${event.summary && event.summary !== event.function ? ` · ${event.summary}` : ''}`

  return (
    <div className="item">
      <div className="grow stack-sm" style={{ gap: 4 }}>
        <div className="obs-event-head">
          <div className="obs-event-title">
            <Tag tone={event.kind === 'call' ? 'info' : 'neutral'}>{activityKindLabel[event.kind]}</Tag>
            <Tag tone={badge.tone}>{badge.text}</Tag>
            <span className="item-title">{headline}</span>
            {ev.unsupportedFallback !== undefined && <Tag tone="warning">降级模式</Tag>}
            {ev.recoveryState && <Tag tone="danger">恢复：{ev.recoveryState}</Tag>}
          </div>
          <div className="row" style={{ gap: 8 }}>
            {(event.kind === 'call' || event.kind === 'usage') && (
              <span className="obs-event-cost">
                {formatCost(event.cost, event.costKnown)}
                {ev.costEstimated ? '（估）' : ''}
              </span>
            )}
            <span className="obs-event-time">{clockTime(event.at, timezone)}</span>
          </div>
        </div>

        <div className="meta" style={{ marginTop: 0 }}>
          {event.kind === 'action' && <span>来源：{functionLabel(event.function)}</span>}
          {ev.provider && <span>渠道：{ev.provider}</span>}
          {ev.model && <span>模型：{ev.model}</span>}
          {ev.prompt && <span>提示词：{ev.prompt}</span>}
          {ev.accounting && <span>记账：{accountingLabel(ev.accounting)}</span>}
          {(ev.inputTokens !== undefined || ev.outputTokens !== undefined) && (
            <span>
              Token：{formatTokens(ev.inputTokens)} / {formatTokens(ev.outputTokens)}
            </span>
          )}
          {ev.durationMs !== undefined && <span>耗时：{formatDurationMs(ev.durationMs)}</span>}
          {ev.heldReservation !== undefined && ev.heldReservation > 0 && <span>预留：{formatCost(ev.heldReservation)}</span>}
          {ev.attempt !== undefined && ev.attemptLimit !== undefined && (
            <span>
              第 {ev.attempt}/{ev.attemptLimit} 次尝试
            </span>
          )}
          {ev.count !== undefined && <span>次数：{ev.count}</span>}
          {ev.changes !== undefined && <span>变更行：{ev.changes}</span>}
          {ev.class && <span>日期类别：{ev.class}</span>}
          {ev.applicationOutcome && <span>应用状态：{ev.applicationOutcome}</span>}
        </div>

        {(reason || ev.recoveryReason || ev.applicationReason) && (
          <div className="small muted">
            原因：{[reason, ev.recoveryReason ? reasonLabel(ev.recoveryReason) : '', ev.applicationReason ? reasonLabel(ev.applicationReason) : ''].filter(Boolean).join('；')}
          </div>
        )}

        <div className="obs-event-actions">
          {event.executionId && (
            <button type="button" className="obs-link-pill" onClick={() => onTrace(event.executionId!)}>
              <GitBranch size={12} />
              追踪同次执行
            </button>
          )}
          {traceAnchor && (
            <button type="button" className="obs-link-pill" onClick={() => onOpenTrace(traceAnchor)}>
              详细溯源
              <ArrowUpRight size={12} />
            </button>
          )}
          {ev.usageId && (
            <button type="button" className="obs-link-pill" onClick={() => onInspectCall(ev.usageId!)}>
              查看引用记忆
              <ArrowUpRight size={12} />
            </button>
          )}
          {thingIds.map((id) => {
            const label = resolveThingTitle(id)
            return (
              <Link key={id} className="obs-link-pill" to={`/t/${id}`}>
                {label ? `事项：${label}` : `打开事项 ${id.slice(0, 6)}`}
                <ArrowUpRight size={12} />
              </Link>
            )
          })}
          {ev.claim && (
            <Link className="obs-link-pill" to={`/library?m=${encodeURIComponent(ev.claim)}`}>
              查看记忆{ev.claimVersion ? `（v${ev.claimVersion}）` : ''}
              <ArrowUpRight size={12} />
            </Link>
          )}
          {showUndo && (
            <Button size="sm" variant="quiet" disabled={undoing} icon={<Undo2 size={13} />} onClick={() => onUndo(event.eventId)}>
              {undoing ? '正在撤销…' : '撤销'}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
