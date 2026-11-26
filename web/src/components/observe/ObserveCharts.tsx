import { formatCost, formatDurationMs, formatTokens, functionLabel, type UsageDay, type UsageTiming } from '../../domain/observe'
import { Empty, Sheet } from '../ui'

export interface DailyCostPoint {
  date: string
  cost: number
  calls: number
  inputTokens: number
  outputTokens: number
  estimatedCalls: number
}

export interface PurposeAggregate {
  purpose: string
  calls: number
  cost: number
  inputTokens: number
  outputTokens: number
  estimatedCostCalls: number
  medianMs: number | null
  maxMs: number | null
  isRangeMedian?: boolean
}

/** Vertical SVG bar chart of daily settled/recorded cost. Clicking a day opens that day's activity stream. */
export function DailyCostChart({
  points,
  onSelectDate,
}: {
  points: DailyCostPoint[]
  onSelectDate: (date: string) => void
}) {
  if (points.length === 0) {
    return (
      <Sheet title={<h3>每日花费趋势</h3>}>
        <Empty>这个时间段里还没有模型用量记录。</Empty>
      </Sheet>
    )
  }

  const maxCost = Math.max(...points.map((p) => p.cost), 0)
  const ceiling = maxCost > 0 ? maxCost : 1
  const width = Math.max(460, points.length * 36 + 56)
  const height = 176
  const padLeft = 52
  const padRight = 14
  const padTop = 16
  const padBottom = 28
  const plotW = width - padLeft - padRight
  const plotH = height - padTop - padBottom
  const slot = plotW / points.length
  const barW = Math.max(8, Math.min(26, slot - 8))
  const labelStep = points.length > 16 ? 4 : points.length > 9 ? 2 : 1

  return (
    <Sheet
      title={<h3>每日花费趋势</h3>}
      aside={<span className="tiny muted">点某一天柱子可直达当日活动</span>}
      pad
    >
      <div className="obs-chart-wrap">
        <svg
          className="obs-chart-svg"
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          aria-label={`近 ${points.length} 天每日花费趋势柱状图`}
        >
          {[0, 0.5, 1].map((ratio) => {
            const y = padTop + plotH * (1 - ratio)
            const val = maxCost > 0 ? ceiling * ratio : 0
            return (
              <g key={ratio}>
                <line x1={padLeft} y1={y} x2={width - padRight} y2={y} className="obs-chart-grid" />
                <text x={padLeft - 6} y={y + 3.5} textAnchor="end" className="obs-chart-axis">
                  {formatCost(val)}
                </text>
              </g>
            )
          })}
          {points.map((pt, i) => {
            const x = padLeft + i * slot + (slot - barW) / 2
            const barH = pt.cost > 0 ? Math.max(4, (pt.cost / ceiling) * plotH) : 3
            const y = padTop + plotH - barH
            const shortDate = pt.date.slice(5)
            const showTick = i % labelStep === 0 || i === points.length - 1
            return (
              <g
                key={pt.date}
                className="obs-bar-hit"
                role="button"
                tabIndex={0}
                aria-label={`${pt.date}：${formatCost(pt.cost)}，${pt.calls} 次调用`}
                onClick={() => onSelectDate(pt.date)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault()
                    onSelectDate(pt.date)
                  }
                }}
              >
                <title>
                  {`${pt.date} · ${formatCost(pt.cost)} · ${pt.calls} 次调用（输入 ${formatTokens(pt.inputTokens)} / 输出 ${formatTokens(pt.outputTokens)}）`}
                </title>
                <rect x={padLeft + i * slot} y={padTop} width={slot} height={plotH + padBottom} fill="transparent" />
                <rect x={x} y={y} width={barW} height={barH} className={`obs-bar-rect${pt.cost > 0 ? '' : ' zero'}`} />
                {showTick && (
                  <text x={x + barW / 2} y={height - 8} textAnchor="middle" className="obs-chart-axis">
                    {shortDate}
                  </text>
                )}
              </g>
            )
          })}
        </svg>
      </div>
    </Sheet>
  )
}

/** Horizontal breakdown of cost and invocation counts by function/purpose. */
export function PurposeBreakdownChart({
  items,
  title = '按功能分摊',
}: {
  items: PurposeAggregate[]
  title?: string
}) {
  if (items.length === 0) return null
  const maxCost = Math.max(...items.map((x) => x.cost), 0)
  const maxCalls = Math.max(...items.map((x) => x.calls), 1)
  return (
    <Sheet title={<h3>{title}</h3>} pad>
      <div className="obs-prop-list">
        {items.map((item) => {
          const pct = maxCost > 0 ? Math.max(item.cost > 0 ? 4 : 0, (item.cost / maxCost) * 100) : (item.calls / maxCalls) * 100
          return (
            <div key={item.purpose} className="obs-prop-row">
              <div className="obs-prop-head">
                <span className="ink">
                  {functionLabel(item.purpose)}
                  <span className="muted aside">{item.calls} 次</span>
                  {item.estimatedCostCalls > 0 && <span className="tiny muted aside">含 {item.estimatedCostCalls} 次估算</span>}
                </span>
                <span className="obs-event-cost">{formatCost(item.cost)}</span>
              </div>
              <div className="obs-prop-track" aria-hidden="true">
                <div className="obs-prop-fill" style={{ width: `${Math.min(100, Math.round(pct))}%` }} />
              </div>
              <div className="tiny muted">
                输入 {formatTokens(item.inputTokens)} · 输出 {formatTokens(item.outputTokens)}
                {item.medianMs !== null &&
                  ` · ${item.isRangeMedian === false ? '日均中位耗时' : '中位耗时'} ${formatDurationMs(item.medianMs)}`}
                {item.maxMs !== null && ` · 最慢 ${formatDurationMs(item.maxMs)}`}
              </div>
            </div>
          )
        })}
      </div>
    </Sheet>
  )
}

/** Displays multi-day execution timings using true range medians when supplied by the server. */
export function ExecutionTimingSheet({ days, rangeTimings }: { days: UsageDay[]; rangeTimings?: UsageTiming[] }) {
  const list =
    rangeTimings && rangeTimings.length > 0
      ? rangeTimings.map((t) => ({
          kind: t.kind,
          tier: t.tier,
          executions: t.executions,
          medianMs: t.totalMs.median,
          isRangeMedian: true,
          totalMax: t.totalMs.max,
          modelMax: t.modelMs.max,
        }))
      : (() => {
          const rows = new Map<
            string,
            { kind: string; tier: string; executions: number; totalMax: number | null; modelMax: number | null; totalMedians: number[] }
          >()
          for (const day of days) {
            for (const t of day.timings ?? []) {
              const key = `${t.kind}:${t.tier}`
              const cur = rows.get(key) ?? { kind: t.kind, tier: t.tier, executions: 0, totalMax: null, modelMax: null, totalMedians: [] }
              cur.executions += t.executions
              if (t.totalMs.max !== null) cur.totalMax = cur.totalMax === null ? t.totalMs.max : Math.max(cur.totalMax, t.totalMs.max)
              if (t.modelMs.max !== null) cur.modelMax = cur.modelMax === null ? t.modelMs.max : Math.max(cur.modelMax, t.modelMs.max)
              if (t.totalMs.median !== null) cur.totalMedians.push(t.totalMs.median)
              rows.set(key, cur)
            }
          }
          return [...rows.values()].map((r) => ({
            kind: r.kind,
            tier: r.tier,
            executions: r.executions,
            medianMs: r.totalMedians.length ? r.totalMedians.reduce((a, b) => a + b, 0) / r.totalMedians.length : null,
            isRangeMedian: false,
            totalMax: r.totalMax,
            modelMax: r.modelMax,
          }))
        })()

  if (list.length === 0) return null
  return (
    <Sheet title={<h3>执行耗时分布</h3>} aside={<span className="tiny muted">按角色与档位汇总</span>}>
      <div className="list">
        {list.map((r) => (
          <div key={`${r.kind}:${r.tier}`} className="item">
            <div className="grow">
              <div className="item-title">
                {functionLabel(r.kind)}
                {r.tier && <span className="muted aside">档位：{r.tier}</span>}
              </div>
              <div className="meta">
                <span>{r.executions} 次执行</span>
                <span>
                  {r.isRangeMedian ? '中位总耗时' : '日均中位总耗时'} {formatDurationMs(r.medianMs)}
                </span>
                <span>最慢总耗时 {formatDurationMs(r.totalMax)}</span>
                <span>最慢模型耗时 {formatDurationMs(r.modelMax)}</span>
              </div>
            </div>
          </div>
        ))}
      </div>
    </Sheet>
  )
}
