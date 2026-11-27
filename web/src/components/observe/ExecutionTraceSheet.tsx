import { useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, GitBranch, RotateCw, Undo2 } from 'lucide-react'
import { findThing, thingTitle } from '../../domain/things'
import { formatShortWhen } from '../../domain/time'
import {
  accountingLabel,
  canOfferTraceActionUndo,
  formatCost,
  formatDurationMs,
  formatTokens,
  functionLabel,
  isTerminalRecovery,
  outcomeBadge,
  reasonLabel,
  recoveryStateLabel,
  traceGapLabel,
  triggerSourceLabel,
  type TraceCall,
} from '../../domain/observe'
import { useStore } from '../../store/context'
import { useExecutionTrace } from '../../store/observe'
import { useToast } from '../../store/toast'
import { SideSheet } from '../Overlay'
import { Button, Empty, Fold, Spinner, Tag } from '../ui'

function shortHash(hash?: string): string {
  if (!hash) return ''
  return hash.slice(0, 10)
}

function ManifestPreview({ manifest }: { manifest?: Record<string, unknown> }) {
  if (!manifest || Object.keys(manifest).length === 0) return null
  return (
    <Fold title="输入清单元数据（仅 ID 与统计，不含原文）" summary={`${Object.keys(manifest).length} 个字段`}>
      <pre className="obs-json">{JSON.stringify(manifest, null, 2)}</pre>
    </Fold>
  )
}

function CallTraceCard({
  call,
  timezone,
  onInspectCall,
}: {
  call: TraceCall
  timezone: string
  onInspectCall: (usageId: string) => void
}) {
  const badge = outcomeBadge(call.outcome)
  const fallback = call.actualMode && 'unsupportedFallback' in call.actualMode
  const appOutcome = typeof call.actualMode?.applicationOutcome === 'string' ? call.actualMode.applicationOutcome : ''
  const appReason = typeof call.actualMode?.applicationReason === 'string' ? call.actualMode.applicationReason : ''

  return (
    <div className="obs-trace-card">
      <div className="obs-event-head">
        <div className="obs-event-title">
          <Tag tone={badge.tone}>{badge.text}</Tag>
          <span className="item-title">
            {functionLabel(call.function)} · {call.stage}
          </span>
          {fallback && <Tag tone="info">兼容模式</Tag>}
          {call.recoveryState !== 'active' && (
            <Tag tone={isTerminalRecovery(call.recoveryState) ? 'danger' : 'neutral'}>
              {recoveryStateLabel(call.recoveryState)}
            </Tag>
          )}
        </div>
        <div className="row" style={{ gap: 8 }}>
          <span className="obs-event-cost">
            {formatCost(call.cost, call.costKnown)}
            {call.costEstimated ? '（估）' : ''}
          </span>
          <span className="obs-event-time">{formatShortWhen(call.createdAt, timezone)}</span>
        </div>
      </div>

      <div className="meta" style={{ marginTop: 0 }}>
        <span>渠道：{call.provider}</span>
        <span>模型：{call.model}</span>
        <span>记账：{accountingLabel(call.accountingState)}</span>
        <span>
          尝试：{call.attempt}/{call.attemptLimit}
        </span>
        {(call.inputTokens !== undefined || call.outputTokens !== undefined) && (
          <span>
            Token：{formatTokens(call.inputTokens)} / {formatTokens(call.outputTokens)}
          </span>
        )}
        {call.durationMs !== undefined && <span>耗时：{formatDurationMs(call.durationMs)}</span>}
        {call.heldReservation !== undefined && call.heldReservation > 0 && <span>预留：{formatCost(call.heldReservation)}</span>}
      </div>

      {(call.promptName || call.schemaName || call.contextBuilderVersion) && (
        <div className="meta" style={{ marginTop: 0 }}>
          {call.promptName && (
            <span>
              提示词：{call.promptName}
              {call.instructionHash && <span className="obs-mono muted"> #{shortHash(call.instructionHash)}</span>}
            </span>
          )}
          {call.schemaName && (
            <span>
              Schema：{call.schemaName}
              {call.schemaHash && <span className="obs-mono muted"> #{shortHash(call.schemaHash)}</span>}
            </span>
          )}
          {call.contextBuilderVersion && <span>上下文构建：{call.contextBuilderVersion}</span>}
        </div>
      )}

      {(call.errorCode || call.recoveryReason || appOutcome) && (
        <div className="small muted">
          {[
            call.errorCode ? `错误：${reasonLabel(call.errorCode)}` : '',
            call.recoveryReason ? `恢复说明：${reasonLabel(call.recoveryReason)}` : '',
            appOutcome ? `应用结果：${appOutcome}${appReason ? `（${reasonLabel(appReason)}）` : ''}` : '',
          ]
            .filter(Boolean)
            .join(' · ')}
        </div>
      )}

      <div className="obs-event-actions">
        {call.usageId && (
          <button type="button" className="obs-link-pill" onClick={() => onInspectCall(call.usageId!)}>
            查看引用原文与记忆
            <ArrowUpRight size={12} />
          </button>
        )}
      </div>

      <ManifestPreview manifest={call.inputManifest} />
    </div>
  )
}

export function ExecutionTraceSheet({
  anchorId,
  onClose,
  onInspectCall,
  onMutated,
}: {
  anchorId: string
  onClose: () => void
  onInspectCall: (usageId: string) => void
  onMutated?: () => void
}) {
  const { state, undo } = useStore()
  const toast = useToast()
  const tz = state.settings.timezone ?? 'UTC'
  const { phase, data, problem, reload } = useExecutionTrace(anchorId)
  const [undoingId, setUndoingId] = useState<string | null>(null)

  const handleUndo = async (actionId: string) => {
    setUndoingId(actionId)
    const ok = await undo(actionId)
    setUndoingId(null)
    if (ok) {
      toast.show('已撤销这项操作')
      reload()
      onMutated?.()
    }
  }

  const hasAny =
    Boolean(data) &&
    (data!.actions.length > 0 ||
      data!.calls.length > 0 ||
      data!.events.length > 0 ||
      data!.runs.length > 0 ||
      data!.stages.length > 0)

  return (
    <SideSheet
      title="执行与因果溯源"
      onClose={onClose}
      top={
        <>
          <span className="obs-mono muted">{anchorId.slice(0, 8)}</span>
          <Button size="sm" variant="quiet" icon={<RotateCw size={12} />} onClick={reload}>
            刷新
          </Button>
        </>
      }
    >
      <div className="stack">
        {phase === 'loading' && !data && (
          <p className="row muted">
            <Spinner /> 正在加载溯源链…
          </p>
        )}

        {phase === 'failed' && (
          <div className="obs-problem" role="alert">
            <span>{problem}</span>
            <Button size="sm" onClick={reload}>
              重试
            </Button>
          </div>
        )}

        {phase === 'ready' && data && !hasAny && (
          <Empty>没有找到编号 {anchorId.slice(0, 8)} 对应的执行、调用或因果链记录（可能已超过保留期或已被清理）。</Empty>
        )}

        {data && data.actions.length > 0 && (
          <div className="stack-sm">
            <h3 className="sheet-subtitle">关联执行动作（{data.actions.length}）</h3>
            {data.actions.map((act) => {
              const badge = outcomeBadge(act.outcome)
              const thingIds = [...new Set([...(act.items ?? []), ...(act.thingId ? [act.thingId] : [])])]
              return (
                <div key={act.id} className="obs-trace-card">
                  <div className="obs-event-head">
                    <div className="obs-event-title">
                      <Tag tone={badge.tone}>{badge.text}</Tag>
                      <span className="item-title">{act.summary}</span>
                    </div>
                    <span className="obs-event-time">{formatShortWhen(act.createdAt, tz)}</span>
                  </div>
                  <div className="meta" style={{ marginTop: 0 }}>
                    <span>来源：{functionLabel(act.source)}</span>
                    <span>变更行：{act.changes}</span>
                    {act.undoneAt && <span>撤销于：{formatShortWhen(act.undoneAt, tz)}</span>}
                    {act.expiredAt && <span>撤销快照已过期</span>}
                  </div>
                  <div className="obs-event-actions">
                    {thingIds.map((id) => {
                      const found = findThing(state, id)
                      if (!found) {
                        return (
                          <span key={id} className="obs-dead-ref">
                            原事项已删除（{id.slice(0, 6)}）
                          </span>
                        )
                      }
                      return (
                        <Link key={id} className="obs-link-pill" to={`/t/${id}`} onClick={onClose}>
                          事项：{thingTitle(found)}
                          <ArrowUpRight size={12} />
                        </Link>
                      )
                    })}
                    {canOfferTraceActionUndo(act) && (
                      <Button
                        size="sm"
                        variant="quiet"
                        disabled={undoingId === act.id}
                        icon={<Undo2 size={13} />}
                        onClick={() => void handleUndo(act.id)}
                      >
                        {undoingId === act.id ? '正在撤销…' : '撤销'}
                      </Button>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}

        {data && data.runs.length > 0 && (
          <div className="stack-sm">
            <h3 className="sheet-subtitle">副手运行（{data.runs.length}）</h3>
            {data.runs.map((run) => {
              const badge = outcomeBadge(run.status)
              const found = findThing(state, run.thingId)
              return (
                <div key={run.id} className="obs-trace-card">
                  <div className="obs-event-head">
                    <div className="obs-event-title">
                      <Tag tone={badge.tone}>{badge.text}</Tag>
                      <span className="item-title">{run.prompt || run.kind || '副手工作'}</span>
                      {run.staleContext && <Tag tone="warning">上下文已过期</Tag>}
                    </div>
                    <span className="obs-event-cost">{formatCost(run.cost)}</span>
                  </div>
                  {run.error && <div className="small warn-text">错误：{run.error}</div>}
                  <div className="obs-event-actions">
                    {found ? (
                      <Link className="obs-link-pill" to={`/t/${run.thingId}`} onClick={onClose}>
                        事项：{thingTitle(found)}
                        <ArrowUpRight size={12} />
                      </Link>
                    ) : (
                      <span className="obs-dead-ref">原事项已删除（{run.thingId.slice(0, 6)}）</span>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}

        {data && data.calls.length > 0 && (
          <div className="stack-sm">
            <h3 className="sheet-subtitle">模型调用记录（{data.calls.length}）</h3>
            {data.calls.map((call) => (
              <CallTraceCard
                key={call.id}
                call={call}
                timezone={tz}
                onInspectCall={onInspectCall}
              />
            ))}
          </div>
        )}

        {data && data.events.length > 0 && (
          <div className="stack-sm">
            <h3 className="sheet-subtitle">因果任务链（{data.events.length}）</h3>
            <div className="obs-chain-list">
              {data.events.map((ev) => {
                const stateBadge = ev.jobState ? outcomeBadge(ev.jobState) : undefined
                const indent = Math.min(ev.chainDepth, 6) * 14
                return (
                  <div key={ev.id} className="obs-chain-node" style={{ marginLeft: indent }}>
                    <div className="obs-event-head">
                      <div className="obs-event-title">
                        <Tag tone={ev.chainDepth > 200 ? 'danger' : 'info'}>
                          <GitBranch size={11} /> L{ev.chainDepth}
                        </Tag>
                        <span className="item-title">{functionLabel(ev.stage)}</span>
                        {stateBadge && <Tag tone={stateBadge.tone}>{stateBadge.text}</Tag>}
                      </div>
                      <span className="obs-event-time">{formatShortWhen(ev.occurredAt, tz)}</span>
                    </div>
                    <div className="meta" style={{ marginTop: 0 }}>
                      <span>{triggerSourceLabel(ev.triggerSource)}</span>
                      <span>对象 v{ev.objectVersion}</span>
                      {ev.jobAttempts !== undefined && ev.jobAttempts > 0 && <span>尝试 {ev.jobAttempts} 次</span>}
                    </div>
                    {ev.jobErrorCode && (
                      <div className="small warn-text">失败原因：{reasonLabel(ev.jobErrorCode)}</div>
                    )}
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {data && data.stages.length > 0 && (
          <div className="stack-sm">
            <h3 className="sheet-subtitle">阶段结果事件（{data.stages.length}）</h3>
            {data.stages.map((st) => {
              const badge = outcomeBadge(st.outcome)
              return (
                <div key={st.id} className="obs-trace-card">
                  <div className="obs-event-head">
                    <div className="obs-event-title">
                      <Tag tone={badge.tone}>{badge.text}</Tag>
                      <span className="item-title">{functionLabel(st.stage)}</span>
                      <span className="tiny muted">× {st.count}</span>
                    </div>
                    <span className="obs-event-time">{formatShortWhen(st.at, tz)}</span>
                  </div>
                  {st.reason && <div className="small muted">原因：{reasonLabel(st.reason)}</div>}
                </div>
              )
            })}
          </div>
        )}

        {data && data.gaps.length > 0 && (
          <div className="stack-sm">
            <h3 className="sheet-subtitle">记录边界与缺口</h3>
            <ul className="obs-gaps">
              {data.gaps.map((g, i) => (
                <li key={i}>{traceGapLabel(g)}</li>
              ))}
            </ul>
          </div>
        )}
      </div>
    </SideSheet>
  )
}
