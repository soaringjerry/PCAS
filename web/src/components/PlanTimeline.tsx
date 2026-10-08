import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { Check } from 'lucide-react'
import { effortText, layoutTimeline, type TimelineBar } from '../domain/studio'
import { formatShortWhen } from '../domain/time'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { readTimeline, useRead } from '../store/studio'

// The plan as a picture: every item with a due time is a bar from the day to
// start by to the day it is due, on one axis with a line at now. Start days are
// worked back from the due time by the server; here they are only drawn. How
// much work an item is gets changed by telling the secretary.

/** The current time, moving once a minute so the line keeps up. */
function useNow(): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60 * 1000)
    return () => window.clearInterval(timer)
  }, [])
  return now
}

const toneText = { done: '已完成', cancelled: '已取消', late: '已过截止', open: '' } as const

/** A day of the axis is never narrower than this; a long plan scrolls sideways inside its own box. */
const DAY_WIDTH = 34

function Row({ projectId, bar }: { projectId: string; bar: TimelineBar }) {
  const { state } = useStore()
  const { prefill } = useShell()
  const { item } = bar
  // A label starts with its bar; near the right edge it ends with it instead, so it stays on the axis.
  const fromRight = bar.left > 0.6
  return (
    <li className={`plan-row ${bar.tone}`}>
      <div className="plan-label" style={fromRight ? { right: `${(1 - bar.left - bar.width) * 100}%` } : { left: `${bar.left * 100}%` }}>
        <Link to={`/t/${item.id}`} className="plan-title">
          {bar.tone === 'done' && <Check size={12} strokeWidth={3} aria-hidden />}
          {item.title}
        </Link>
        {toneText[bar.tone] && <span className="plan-tone">{toneText[bar.tone]}</span>}
        <span className="plan-due">{formatShortWhen(item.due, state.settings.timezone ?? 'UTC')} 截止</span>
        {item.estimatedHours != null ? (
          <button type="button" className="plan-effort" title="点一下，跟秘书说怎么改" onClick={() => prefill(projectId, `「${item.title}」的工作量改成：`)}>
            {effortText(item.estimatedHours)}
          </button>
        ) : (
          bar.tone === 'open' && (
            <button type="button" className="plan-effort add" title="点一下，跟秘书说" onClick={() => prefill(projectId, `「${item.title}」大概要做：`)}>
              说个工作量
            </button>
          )
        )}
      </div>
      <Link
        to={`/t/${item.id}`}
        className={`plan-bar${bar.started ? '' : ' due-only'}`}
        style={{ left: `${bar.left * 100}%`, width: `${bar.width * 100}%` }}
        tabIndex={-1}
        aria-hidden
      />
    </li>
  )
}

export function PlanTimeline({ projectId }: { projectId: string }) {
  const { state } = useStore()
  const now = useNow()
  const [read, retry] = useRead(projectId, state.revision, () => readTimeline(projectId))
  const scroller = useRef<HTMLDivElement>(null)
  const timeline = read.phase === 'ready' ? read.value : undefined
  const layout = timeline && timeline.items.length > 0 ? layoutTimeline(timeline.items, now, state.settings.timezone) : undefined
  const ready = Boolean(layout)
  // Opens with now in view: a third of the way in, the past behind it.
  useLayoutEffect(() => {
    const box = scroller.current
    const line = box?.querySelector<HTMLElement>('.plan-now')
    if (box && line) box.scrollLeft = Math.max(0, line.offsetLeft - box.clientWidth / 3)
  }, [ready])

  if (read.phase === 'failed') {
    return (
      <section className="section" aria-label="计划">
        <div className="section-label">计划</div>
        <p className="status-note" role="alert">
          计划没读出来：{read.problem}
          <button type="button" className="act-btn" onClick={retry}>
            再读一次
          </button>
        </p>
      </section>
    )
  }
  // A project with nothing in it has no plan to draw.
  if (!timeline || timeline.items.length + timeline.withoutDue.length === 0) return null

  return (
    <section className="section plan" aria-label="计划">
      <div className="section-label">计划</div>
      {layout ? (
        <div className="group plan-scroll" ref={scroller} tabIndex={0} role="group" aria-label="计划时间轴，可以左右滑动">
          <div className="plan-axis" style={{ minWidth: layout.days * DAY_WIDTH }}>
            <div className="plan-ticks" aria-hidden>
              {layout.ticks.map((t) => (
                <span key={t.at} className={`plan-tick${t.weekend ? ' weekend' : ''}`} style={{ left: `${t.at * 100}%`, width: `${100 / layout.days}%` }}>
                  {t.label}
                </span>
              ))}
            </div>
            <ol className="plan-rows">
              {layout.bars.map((b) => (
                <Row key={b.item.id} projectId={projectId} bar={b} />
              ))}
            </ol>
            <div className="plan-now" style={{ left: `${layout.now * 100}%` }}>
              <span>现在</span>
            </div>
          </div>
        </div>
      ) : (
        <p className="status-note">还没有定了截止的事。</p>
      )}
      {timeline.withoutDue.length > 0 && (
        <div className="plan-undated" role="group" aria-label="没定截止的">
          <span className="plan-undated-label">没定截止的</span>
          {timeline.withoutDue.map((t) => (
            <Link key={t.id} to={`/t/${t.id}`} className={`fact${t.status === 'done' || t.status === 'cancelled' ? ' closed' : ''}`}>
              {t.title}
            </Link>
          ))}
        </div>
      )}
    </section>
  )
}
