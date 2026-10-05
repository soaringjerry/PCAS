import { useNavigate, useSearchParams } from 'react-router'
import { CalendarClock, ChevronDown, ChevronRight, CircleAlert, Info, Repeat, RotateCw } from 'lucide-react'
import { SideSheet } from '../components/Overlay'
import { Button, Empty, Sheet, Spinner } from '../components/ui'
import { cardFieldLabel, cardKinds, deadlineKindLabel, handoverSections } from '../domain/about'
import type { Deadline, StatusCardRef } from '../domain/status'
import { formatDateTime } from '../domain/time'
import type { Memory } from '../domain/types'
import { useAbout, useAboutCard } from '../store/about'
import { useStore } from '../store/context'
import { useMemory } from '../store/memories'
import { MemorySheet } from './LibraryPage'

/** The note a new helper would be handed, one part after another. */
function Handover({ body }: { body: string }) {
  const sections = handoverSections(body)
  return (
    <section className="about-block" aria-labelledby="about-handover">
      <h2 id="about-handover" className="about-title">交接说明</h2>
      <Sheet>
        <div className="about-handover">
          {sections.map((s, i) => (
            <div key={i} className="about-part">
              {s.title && <h3>{s.title}</h3>}
              {s.text && <p>{s.text}</p>}
            </div>
          ))}
        </div>
      </Sheet>
    </section>
  )
}

function Deadlines({ items, onOpen }: { items: Deadline[]; onOpen: (memoryId: string) => void }) {
  const timeZone = useStore().state.settings.timezone ?? 'UTC'
  return (
    <section className="about-block" aria-labelledby="about-deadlines">
      <h2 id="about-deadlines" className="about-title">期限和固定安排</h2>
      <Sheet>
        <ul className="about-dates">
          {items.map((d) => (
            <li key={d.id}>
              <button type="button" onClick={() => onOpen(d.memoryId)}>
                <span className="about-when">
                  {d.kind === 'recurring' ? <Repeat size={13} /> : <CalendarClock size={13} />}
                  {d.at ? formatDateTime(d.at, timeZone) : d.recurrence || '时间没说'}
                </span>
                <span className="about-what">
                  {d.title}
                  {d.timeNote && <span className="about-note">{d.timeNote}</span>}
                </span>
                <span className="about-kind">{deadlineKindLabel[d.kind]}</span>
              </button>
            </li>
          ))}
        </ul>
      </Sheet>
    </section>
  )
}

/** One card: its name and how much is under it; opened, the memories under each heading. */
function Card({ card, open, onToggle, onOpen }: { card: StatusCardRef; open: boolean; onToggle: () => void; onOpen: (memory: Memory) => void }) {
  const read = useAboutCard(open ? card.key : '')
  const fields = read.card?.fields.filter((f) => f.items.length > 0) ?? []
  return (
    <li className={`about-card${open ? ' open' : ''}`}>
      <button type="button" className="about-card-bar" aria-expanded={open} onClick={onToggle}>
        <span className="about-card-name">{card.name}</span>
        <span className="about-card-count">{card.count} 条</span>
        {open ? <ChevronDown size={15} /> : <ChevronRight size={15} />}
      </button>
      {open && (
        <div className="about-card-body">
          {read.phase === 'loading' ? (
            <div className="mem-state" role="status">
              <Spinner />
              正在读取…
            </div>
          ) : read.phase === 'failed' ? (
            <div className="mem-state failed" role="alert">
              <CircleAlert size={15} />
              <span>这张卡没读出来：{read.problem}</span>
              <Button size="sm" icon={<RotateCw size={13} />} onClick={read.retry}>
                重试
              </Button>
            </div>
          ) : fields.length === 0 ? (
            <p className="about-none">这里的内容刚有变动，稍后再来看。</p>
          ) : (
            fields.map((f) => (
              <div key={f.field} className="about-field" role="group" aria-label={cardFieldLabel[f.field] ?? f.field}>
                <h4>{cardFieldLabel[f.field] ?? f.field}</h4>
                <ul>
                  {f.items.map((m) => (
                    <li key={m.id}>
                      <button type="button" className={`mem-text${m.epistemic === 'inferred' ? ' guess' : ''}`} onClick={() => onOpen(m)}>
                        {m.text}
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            ))
          )}
        </div>
      )}
    </li>
  )
}

export function AboutPage() {
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  // What is open lives in the address, so a reload or the back button keeps it.
  const openCard = params.get('card') ?? ''
  const openId = params.get('m')
  const change = (patch: Record<string, string | null>) =>
    setParams((current) => {
      const next = new URLSearchParams(current)
      for (const [name, value] of Object.entries(patch)) {
        if (value) next.set(name, value)
        else next.delete(name)
      }
      return next
    })
  const { about, phase, problem, retry } = useAbout()
  const opened = useMemory(openId)
  const building = about && about.building.done < about.building.total ? about.building : undefined
  const nothing = about && !about.handover.body.trim() && about.cards.length === 0 && about.deadlines.length === 0

  return (
    <main className="page page-narrow">
      <div className="page-head">
        <div>
          <h1>关于你</h1>
          <p>我现在对你的了解：你的处境、手上的事、要守的时间，和一直以来的习惯。</p>
        </div>
      </div>
      <p className="hint-line">
        <Info size={13} />
        这些是从你说过的话整理出来的，哪里不对就改那条记忆，或者直接告诉我。
      </p>
      {building && (
        <p className="mem-summary" role="status" aria-label="整理进度">
          正在整理 {building.done} / {building.total}
        </p>
      )}
      {phase === 'loading' ? (
        <Sheet>
          <div className="mem-state" role="status">
            <Spinner />
            正在读取…
          </div>
        </Sheet>
      ) : phase === 'failed' || !about ? (
        <Sheet>
          <div className="mem-state failed" role="alert">
            <CircleAlert size={15} />
            <span>没读出来：{problem}</span>
            <Button size="sm" icon={<RotateCw size={13} />} onClick={retry}>
              重试
            </Button>
          </div>
        </Sheet>
      ) : nothing ? (
        <Sheet>
          <Empty>{building ? '还在整理，过一会儿这里就有内容了。' : '还没有整理出什么。多跟秘书说说你的事，这里会慢慢长出来。'}</Empty>
        </Sheet>
      ) : (
        <>
          {about.handover.body.trim() && <Handover body={about.handover.body} />}
          {about.deadlines.length > 0 && <Deadlines items={about.deadlines} onOpen={(id) => change({ m: id })} />}
          {cardKinds.map(({ kind, label }) => {
            const cards = about.cards.filter((c) => c.kind === kind)
            if (!cards.length) return null
            return (
              <section key={kind} className="about-block" aria-label={label}>
                <h2 className="about-title">{label}</h2>
                <Sheet>
                  <ul className="about-cards">
                    {cards.map((c) => (
                      <Card key={c.key} card={c} open={c.key === openCard} onToggle={() => change({ card: c.key === openCard ? null : c.key })} onOpen={(m) => change({ m: m.id })} />
                    ))}
                  </ul>
                </Sheet>
              </section>
            )
          })}
        </>
      )}
      {openId && opened.memory && (
        <MemorySheet
          key={opened.memory.id}
          memory={opened.memory}
          entity=""
          group=""
          onClose={() => change({ m: null })}
          onPick={(mention) => navigate(`/library?entity=${encodeURIComponent(mention.entityId)}`)}
          onPickGroup={(g) => navigate(`/library?group=${encodeURIComponent(g.entityId)}`)}
          onDeleted={() => undefined}
        />
      )}
      {openId && !opened.memory && (
        <SideSheet title="记忆" onClose={() => change({ m: null })}>
          {opened.phase === 'loading' ? (
            <div className="mem-state" role="status">
              <Spinner />
              正在读取这条记忆…
            </div>
          ) : opened.phase === 'failed' ? (
            <div className="mem-state failed" role="alert">
              <CircleAlert size={15} />
              <span>这条记忆没读出来：{opened.problem}</span>
              <Button size="sm" icon={<RotateCw size={13} />} onClick={opened.retry}>
                重试
              </Button>
            </div>
          ) : (
            <Empty>这条记忆已经不在了。</Empty>
          )}
        </SideSheet>
      )}
    </main>
  )
}
