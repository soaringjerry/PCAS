import { useState, type ReactNode } from 'react'
import { CalendarClock, ChevronDown, ChevronRight, CircleAlert, CircleHelp, Repeat, RotateCw } from 'lucide-react'
import { deadlineGroups, deadlineKindLabel, groupDeadlines, handoverSections, type DeadlineGroup } from '../domain/now'
import type { Deadline, Handover } from '../domain/status'
import { formatDateTime } from '../domain/time'
import { useStore } from '../store/context'
import { useNow } from '../store/now'
import { Button, Spinner } from './ui'

const KEY = 'pcas.now.closed'

/** Parts of the note shown before 「看全文」; the rest is one click away and counted on the button. */
const PARTS_SHOWN = 2
/** Dates shown in each part before 「还有 N 条」. */
const DATES_SHOWN = 5

function HandoverNote({ handover }: { handover: Handover }) {
  const timeZone = useStore().state.settings.timezone ?? 'UTC'
  const [all, setAll] = useState(false)
  const sections = handoverSections(handover.body)
  const written = handover.builtAt ? formatDateTime(handover.builtAt, timeZone) : ''
  return (
    <section className="now-block" aria-labelledby="now-handover">
      <div className="now-block-head">
        <h3 id="now-handover">交接说明</h3>
        {sections.length > 0 && (
          <span className="now-written">
            {handover.stale ? '正在更新，下面是上一份' : ''}
            {handover.stale && written ? '，' : ''}
            {written && `写于 ${written}`}
          </span>
        )}
      </div>
      {sections.length === 0 ? (
        <p className="now-none">还没有交接说明。</p>
      ) : (
        <>
          <div className="now-handover">
            {(all ? sections : sections.slice(0, PARTS_SHOWN)).map((s, i) => (
              <div key={i} className="now-part">
                {s.title && <h4>{s.title}</h4>}
                {s.text && <p>{s.text}</p>}
              </div>
            ))}
          </div>
          {sections.length > PARTS_SHOWN && (
            <button type="button" className="link-btn now-more" aria-expanded={all} onClick={() => setAll((v) => !v)}>
              {all ? '收起' : `看全文（还有 ${sections.length - PARTS_SHOWN} 节）`}
            </button>
          )}
        </>
      )}
    </section>
  )
}

const groupIcon: Record<DeadlineGroup, ReactNode> = {
  upcoming: <CalendarClock size={13} />,
  overdue: <CalendarClock size={13} />,
  recurring: <Repeat size={13} />,
  unclear: <CircleHelp size={13} />,
}

function DateGroup({ group, label, items, onOpen }: { group: DeadlineGroup; label: string; items: Deadline[]; onOpen: (memoryId: string) => void }) {
  const timeZone = useStore().state.settings.timezone ?? 'UTC'
  const [all, setAll] = useState(false)
  const shown = all ? items : items.slice(0, DATES_SHOWN)
  return (
    <div className={`now-dates now-dates-${group}`} role="group" aria-label={label}>
      <h4>
        {label}
        <span className="n">{items.length}</span>
      </h4>
      <ul>
        {shown.map((d) => (
          <li key={d.id}>
            <button type="button" onClick={() => onOpen(d.memoryId)}>
              <span className="now-when">
                {groupIcon[group]}
                {group === 'recurring' ? d.recurrence || '周期没说清' : group === 'unclear' ? '日期没说清' : formatDateTime(d.at!, timeZone)}
              </span>
              <span className="now-what">
                {d.title}
                {d.timeNote && <span className="now-note">{d.timeNote}</span>}
              </span>
              {d.kind !== 'recurring' && <span className="now-kind">{deadlineKindLabel[d.kind]}</span>}
            </button>
          </li>
        ))}
      </ul>
      {items.length > DATES_SHOWN && (
        <button type="button" className="link-btn now-more" aria-expanded={all} onClick={() => setAll((v) => !v)}>
          {all ? '收起' : `还有 ${items.length - DATES_SHOWN} 条`}
        </button>
      )}
    </div>
  )
}

/** `at`: when the dates were read, which is what decides the ones gone by. */
function Dates({ items, at, onOpen }: { items: Deadline[]; at: number; onOpen: (memoryId: string) => void }) {
  const groups = groupDeadlines(items, at)
  return (
    <section className="now-block" aria-labelledby="now-deadlines">
      <div className="now-block-head">
        <h3 id="now-deadlines">期限和固定安排</h3>
      </div>
      {items.length === 0 ? (
        <p className="now-none">没有记下期限或固定安排。</p>
      ) : (
        deadlineGroups.map(({ group, label }) => groups[group].length > 0 && <DateGroup key={group} group={group} label={label} items={groups[group]} onOpen={onOpen} />)
      )}
    </section>
  )
}

function wasClosed(): boolean {
  try { return localStorage.getItem(KEY) === '1' } catch { return false }
}

/** What matters right now, above everything the library holds: the handover note and the dates to keep. */
export function NowPanel({ onOpen }: { onOpen: (memoryId: string) => void }) {
  const { now, readAt, phase, problem, retry } = useNow()
  const [closed, setClosed] = useState(wasClosed)
  const toggle = () => {
    const next = !closed
    setClosed(next)
    try { localStorage.setItem(KEY, next ? '1' : '0') } catch { /* Open next time; nothing is lost. */ }
  }
  return (
    <section className="sheet now" aria-labelledby="now-title">
      <div className="now-bar">
        <h2 id="now-title">眼下</h2>
        <span className="now-about">交接说明、期限和固定安排</span>
        <button type="button" className="link-btn" aria-expanded={!closed} onClick={toggle}>
          {closed ? '展开' : '收起'}
          {closed ? <ChevronRight size={13} /> : <ChevronDown size={13} />}
        </button>
      </div>
      {!closed && (
        phase === 'loading' ? (
          <div className="mem-state" role="status">
            <Spinner />
            正在读取…
          </div>
        ) : phase === 'failed' || !now ? (
          <div className="mem-state failed" role="alert">
            <CircleAlert size={15} />
            <span>没读出来：{problem}</span>
            <Button size="sm" icon={<RotateCw size={13} />} onClick={retry}>
              重试
            </Button>
          </div>
        ) : (
          <div className="now-body">
            <HandoverNote handover={now.handover} />
            <Dates items={now.deadlines} at={readAt} onOpen={onOpen} />
          </div>
        )
      )}
    </section>
  )
}
