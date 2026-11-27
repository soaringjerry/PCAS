import { useEffect, useMemo, useRef, useState } from 'react'
import { Link } from 'react-router'
import { ArrowUpRight, FileText, RotateCw } from 'lucide-react'
import { formatShortWhen } from '../../domain/time'
import { formatCost, formatDurationMs, formatTokens, functionLabel, shiftCivilDate, type UsageCall, type UsageCallRef } from '../../domain/observe'
import { useStore } from '../../store/context'
import { useUsageCallById, useUsageCalls, useUsageSummary } from '../../store/observe'
import { SourceSheet } from '../SourceSheet'
import { Button, Empty, Seg, Sheet, Spinner, Tag } from '../ui'
import { DailyCostChart, ExecutionTimingSheet, PurposeBreakdownChart, type DailyCostPoint, type PurposeAggregate } from './ObserveCharts'

type RangeDays = '7' | '14' | '30'

function buildDateRange(from: string, to: string): string[] {
  if (!from || !to || from > to) return []
  const out: string[] = []
  let cur = from
  while (cur <= to && out.length < 90) {
    out.push(cur)
    cur = shiftCivilDate(cur, 1)
  }
  return out
}

function UsageCallCard({
  call,
  tz,
  focused,
  cardRef,
  onOpenSource,
}: {
  call: UsageCall
  tz: string
  focused: boolean
  cardRef?: React.Ref<HTMLDivElement>
  onOpenSource: (id: string, version: number) => void
}) {
  return (
    <div ref={cardRef} className={`item${focused ? ' obs-call-highlight' : ''}`}>
      <div className="grow stack-sm" style={{ gap: 4 }}>
        <div className="obs-event-head">
          <div className="obs-event-title">
            <span className="item-title">{functionLabel(call.purpose)}</span>
            <Tag tone="info">{call.model || '未记录模型'}</Tag>
            {call.tier && <Tag>{call.tier}</Tag>}
            {(call.costEstimated || call.inputEstimated || call.outputEstimated) && <Tag tone="warning">含估算</Tag>}
          </div>
          <div className="row" style={{ gap: 8 }}>
            <span className="obs-event-cost">{formatCost(call.cost)}</span>
            <span className="obs-event-time">{formatShortWhen(call.at, tz)}</span>
          </div>
        </div>

        <div className="meta" style={{ marginTop: 0 }}>
          <span>
            Token：{formatTokens(call.inputTokens)} / {formatTokens(call.outputTokens)}
          </span>
          {call.durationMs !== null && <span>耗时：{formatDurationMs(call.durationMs)}</span>}
          {call.executionTiming && (
            <span>
              准备 {formatDurationMs(call.executionTiming.prepareMs)} · 回答 {formatDurationMs(call.executionTiming.answerMs)}
              {call.executionTiming.selfcheckMs > 0 ? ` · 自查 ${formatDurationMs(call.executionTiming.selfcheckMs)}` : ''}
            </span>
          )}
          {call.agentId && <span>渠道：{call.agentId}</span>}
        </div>

        {call.refs.length > 0 && (
          <div className="obs-refs">
            {call.refs.map((ref) => (
              <CallRefRow
                key={`${ref.kind}:${ref.id}:${ref.version}`}
                refItem={ref}
                onOpenSource={onOpenSource}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  )
}

export function ObserveUsageTab({
  highlightedCallId,
  onSelectDate,
  onClearHighlight,
}: {
  highlightedCallId: string
  onSelectDate: (date: string) => void
  onClearHighlight: () => void
}) {
  const { state } = useStore()
  const tz = state.settings.timezone ?? 'UTC'
  const [range, setRange] = useState<RangeDays>('14')
  const [sourceRef, setSourceRef] = useState<{ id: string; version: number } | null>(null)
  const focusedRef = useRef<HTMLDivElement | null>(null)

  const summary = useUsageSummary(Number(range), tz, true)
  const calls = useUsageCalls(true)

  const inLoadedPage = useMemo(
    () => (highlightedCallId ? calls.items.find((c) => c.id === highlightedCallId) ?? null : null),
    [calls.items, highlightedCallId],
  )
  const needDirectLookup = Boolean(highlightedCallId) && calls.phase !== 'loading' && !inLoadedPage
  const lookedUp = useUsageCallById(highlightedCallId, needDirectLookup)
  const locatedCall = inLoadedPage ?? (needDirectLookup ? lookedUp.data : null)

  useEffect(() => {
    if (locatedCall && focusedRef.current) {
      focusedRef.current.scrollIntoView({ behavior: 'smooth', block: 'center' })
    }
  }, [locatedCall])

  const aggregated = useMemo(() => {
    const days = summary.data?.days ?? []
    const byDate = new Map(days.map((d) => [d.date, d]))
    const dates = buildDateRange(summary.from, summary.to)
    for (const d of days) {
      if (!dates.includes(d.date)) dates.push(d.date)
    }
    dates.sort()

    const points: DailyCostPoint[] = []
    const byPurpose = new Map<string, PurposeAggregate & { medians: number[] }>()
    let totalCost = 0
    let totalCalls = 0
    let totalInput = 0
    let totalOutput = 0
    let estimatedCostCalls = 0
    let activeDays = 0

    for (const date of dates) {
      const day = byDate.get(date)
      let dCost = 0
      let dCalls = 0
      let dIn = 0
      let dOut = 0
      let dEst = 0
      if (day) {
        for (const p of day.purposes) {
          dCost += p.cost
          dCalls += p.calls
          dIn += p.inputTokens
          dOut += p.outputTokens
          dEst += p.estimatedCostCalls

          const cur = byPurpose.get(p.purpose) ?? {
            purpose: p.purpose,
            calls: 0,
            cost: 0,
            inputTokens: 0,
            outputTokens: 0,
            estimatedCostCalls: 0,
            medianMs: null,
            maxMs: null,
            isRangeMedian: false,
            medians: [],
          }
          cur.calls += p.calls
          cur.cost += p.cost
          cur.inputTokens += p.inputTokens
          cur.outputTokens += p.outputTokens
          cur.estimatedCostCalls += p.estimatedCostCalls
          if (p.durationMs?.median !== null && p.durationMs?.median !== undefined) cur.medians.push(p.durationMs.median)
          if (p.durationMs?.max !== null && p.durationMs?.max !== undefined) {
            cur.maxMs = cur.maxMs === null ? p.durationMs.max : Math.max(cur.maxMs, p.durationMs.max)
          }
          byPurpose.set(p.purpose, cur)
        }
      }
      if (dCalls > 0) activeDays++
      totalCost += dCost
      totalCalls += dCalls
      totalInput += dIn
      totalOutput += dOut
      estimatedCostCalls += dEst
      points.push({ date, cost: dCost, calls: dCalls, inputTokens: dIn, outputTokens: dOut, estimatedCalls: dEst })
    }

    const serverPurposes = summary.data?.purposes
    const purposes: PurposeAggregate[] =
      serverPurposes && serverPurposes.length > 0
        ? serverPurposes.map((p) => ({
            purpose: p.purpose,
            calls: p.calls,
            cost: p.cost,
            inputTokens: p.inputTokens,
            outputTokens: p.outputTokens,
            estimatedCostCalls: p.estimatedCostCalls,
            medianMs: p.durationMs?.median ?? null,
            maxMs: p.durationMs?.max ?? null,
            isRangeMedian: true,
          }))
        : [...byPurpose.values()]
            .map(({ medians, ...rest }) => ({
              ...rest,
              medianMs: medians.length ? medians.reduce((a, b) => a + b, 0) / medians.length : null,
              isRangeMedian: false,
            }))
            .sort((a, b) => b.cost - a.cost || b.calls - a.calls)

    return {
      points: activeDays > 0 ? points : [],
      activeDays,
      windowDays: points.length,
      purposes,
      totalCost,
      totalCalls,
      totalInput,
      totalOutput,
      estimatedCostCalls,
    }
  }, [summary.data, summary.from, summary.to])

  const displayCalls = useMemo(() => {
    if (locatedCall && !inLoadedPage) {
      return [locatedCall, ...calls.items]
    }
    return calls.items
  }, [calls.items, inLoadedPage, locatedCall])

  return (
    <div className="stack">
      <div className="obs-toolbar">
        <Seg<RangeDays>
          label="统计时间范围"
          value={range}
          onChange={setRange}
          items={[
            { value: '7', label: '近 7 天' },
            { value: '14', label: '近 14 天' },
            { value: '30', label: '近 30 天' },
          ]}
        />
        <Button
          size="sm"
          variant="quiet"
          icon={<RotateCw size={13} />}
          onClick={() => {
            summary.reload()
            calls.reload()
            if (needDirectLookup) lookedUp.reload()
          }}
        >
          刷新
        </Button>
      </div>

      {summary.phase === 'failed' && (
        <div className="obs-problem" role="alert">
          <span>{summary.problem}</span>
          <Button size="sm" onClick={summary.reload}>
            重试
          </Button>
        </div>
      )}

      {summary.phase === 'loading' && !summary.data && (
        <Sheet pad>
          <p className="row muted">
            <Spinner /> 正在读取用量统计…
          </p>
        </Sheet>
      )}

      {summary.data && (
        <>
          <div className="obs-overview" role="group" aria-label="区间花费概览">
            <div className="obs-tile">
              <span className="k">近 {range} 天总花费</span>
              <span className="v">{formatCost(aggregated.totalCost)}</span>
              <span className="d">
                {aggregated.estimatedCostCalls > 0 ? `含 ${aggregated.estimatedCostCalls} 次单价估算` : '按记录单价汇总'}
              </span>
            </div>
            <div className="obs-tile">
              <span className="k">记录调用次数</span>
              <span className="v">{aggregated.totalCalls} 次</span>
              <span className="d">
                {aggregated.activeDays} 个活跃日（共 {aggregated.windowDays} 天）
              </span>
            </div>
            <div className="obs-tile">
              <span className="k">输入 Token</span>
              <span className="v">{formatTokens(aggregated.totalInput)}</span>
              <span className="d">累计上下文读取</span>
            </div>
            <div className="obs-tile">
              <span className="k">输出 Token</span>
              <span className="v">{formatTokens(aggregated.totalOutput)}</span>
              <span className="d">累计模型生成</span>
            </div>
          </div>

          <DailyCostChart points={aggregated.points} onSelectDate={onSelectDate} />
          <PurposeBreakdownChart items={aggregated.purposes} title={`近 ${range} 天按功能分摊`} />
          <ExecutionTimingSheet days={summary.data.days} rangeTimings={summary.data.timings} />
        </>
      )}

      {highlightedCallId && (
        <div className="obs-trace-banner" role="status">
          <span>
            {locatedCall ? (
              <>
                已定位调用记录：<span className="obs-mono">{highlightedCallId.slice(0, 8)}</span>
                {!inLoadedPage && <span className="tiny muted aside">（早于最近 50 条，已按 ID 直接调取）</span>}
              </>
            ) : needDirectLookup && lookedUp.phase === 'failed' ? (
              lookedUp.notFound ? (
                <>
                  未找到编号 <span className="obs-mono">{highlightedCallId.slice(0, 8)}</span> 的调用记录（可能已清理或无权限）。
                </>
              ) : (
                <>定位调用记录失败：{lookedUp.problem}</>
              )
            ) : (
              <>
                正在定位调用记录：<span className="obs-mono">{highlightedCallId.slice(0, 8)}</span>
              </>
            )}
          </span>
          <Button size="sm" variant="quiet" onClick={onClearHighlight}>
            取消高亮
          </Button>
        </div>
      )}

      <Sheet
        title={<h3>模型调用与引用依据</h3>}
        aside={<span className="tiny muted">引用版本号为调用当时记录，预览为当前文本</span>}
      >
        {calls.phase === 'failed' && (
          <div className="sheet-pad">
            <div className="obs-problem" role="alert">
              <span>{calls.problem}</span>
              <Button size="sm" onClick={calls.reload}>
                重试
              </Button>
            </div>
          </div>
        )}
        {calls.phase === 'loading' && displayCalls.length === 0 && (
          <div className="sheet-pad">
            <p className="row muted">
              <Spinner /> 正在读取调用明细…
            </p>
          </div>
        )}
        {calls.phase === 'ready' && displayCalls.length === 0 && <Empty>还没有记录到模型调用。</Empty>}
        {displayCalls.length > 0 && (
          <div className="list">
            {displayCalls.map((call) => {
              const focused = highlightedCallId === call.id
              return (
                <UsageCallCard
                  key={call.id}
                  call={call}
                  tz={tz}
                  focused={focused}
                  cardRef={focused ? focusedRef : undefined}
                  onOpenSource={(id, version) => setSourceRef({ id, version })}
                />
              )
            })}
          </div>
        )}
        {calls.hasMore && (
          <div className="obs-page-footer row">
            <Button size="sm" disabled={calls.more === 'loading'} onClick={calls.loadMore}>
              {calls.more === 'loading' ? '正在加载…' : '再看 50 条'}
            </Button>
            {calls.more === 'failed' && (
              <span className="small warn-text" role="alert">
                {calls.moreProblem}
              </span>
            )}
          </div>
        )}
      </Sheet>

      {sourceRef && <SourceSheet id={sourceRef.id} version={sourceRef.version} onClose={() => setSourceRef(null)} />}
    </div>
  )
}

function CallRefRow({
  refItem,
  onOpenSource,
}: {
  refItem: UsageCallRef
  onOpenSource: (id: string, version: number) => void
}) {
  const isSource = refItem.kind === 'source'
  return (
    <div className="obs-ref-card">
      <div className="grow stack-sm" style={{ gap: 2 }}>
        <div className="row" style={{ gap: 6 }}>
          <Tag tone={isSource ? 'info' : 'neutral'}>{isSource ? '来源原文' : '记忆'}</Tag>
          <span className="tiny muted">调用时第 {refItem.version} 版</span>
          {refItem.deleted && <Tag tone="danger">当前已删除</Tag>}
        </div>
        <div className="obs-ref-text">
          {refItem.deleted ? '原记录已删除，仅保留当时引用编号。' : refItem.text || '（当前无预览文本）'}
        </div>
      </div>
      {!refItem.deleted &&
        (isSource ? (
          <button type="button" className="obs-link-pill" onClick={() => onOpenSource(refItem.id, refItem.version)}>
            <FileText size={12} />
            看原文
          </button>
        ) : (
          <Link className="obs-link-pill" to={`/library?m=${encodeURIComponent(refItem.id)}`}>
            看记忆
            <ArrowUpRight size={12} />
          </Link>
        ))}
    </div>
  )
}
