import { useState } from 'react'
import { CheckCircle2, Circle, Send } from 'lucide-react'
import { ideaStatusLabel } from '../domain/labels'
import { formatAgo, formatWhen } from '../domain/time'
import type { Idea } from '../domain/types'
import { useStore } from '../store/context'
import { useCreateHandoff, useDetail } from '../store/hooks'
import { ProjectName, SourceLine, Timeline } from './bits'
import { Drawer, Modal } from './Overlay'
import { Badge, Button, Field } from './ui'
import { WakeActions, WakeReason } from './Wake'

const conditionKindText = { time: '时间', info: '新信息', event: '事件' } as const

export function IdeaDrawer({ idea }: { idea: Idea }) {
  const { state, dispatch } = useStore()
  const { close, openTask } = useDetail()
  const createHandoff = useCreateHandoff()
  const [note, setNote] = useState('')
  const [shelving, setShelving] = useState(false)
  const [reason, setReason] = useState('')
  const [condition, setCondition] = useState('')
  const task = state.tasks.find((t) => t.ideaId === idea.id)

  return (
    <Drawer
      title={idea.title}
      onClose={close}
      badges={
        <>
          <Badge tone={ideaStatusLabel[idea.status].tone}>{ideaStatusLabel[idea.status].text}</Badge>
          <ProjectName id={idea.projectId} />
          {!idea.remindersOn && idea.status !== 'dropped' && <Badge>提醒已关闭</Badge>}
        </>
      }
    >
      <div className="drawer-section">
        {idea.body && <p className="pre">{idea.body}</p>}
        {idea.status === 'awakened' && idea.wake && (
          <>
            <WakeReason wake={idea.wake} />
            <WakeActions idea={idea} />
          </>
        )}
        {idea.status === 'active' && (
          <div className="row">
            <Button variant="primary" size="sm" onClick={() => dispatch({ type: 'ideaPromote', id: idea.id })}>
              转成任务
            </Button>
            <Button size="sm" icon={<Send size={14} />} onClick={() => createHandoff({ ideaId: idea.id })}>
              交给外部 AI
            </Button>
            <Button size="sm" onClick={() => setShelving(true)}>
              搁置
            </Button>
          </div>
        )}
        {idea.status === 'shelved' && (
          <div className="row">
            <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
              现在就继续
            </Button>
            {idea.remindersOn && (
              <Button size="sm" variant="ghost" onClick={() => dispatch({ type: 'ideaStopReminders', id: idea.id })}>
                停止提醒
              </Button>
            )}
            <Button size="sm" variant="ghost" className="btn-danger" onClick={() => dispatch({ type: 'ideaDrop', id: idea.id })}>
              放弃
            </Button>
          </div>
        )}
        {idea.status === 'dropped' && (
          <div className="row">
            <Button size="sm" onClick={() => dispatch({ type: 'ideaContinue', id: idea.id })}>
              重新拾起
            </Button>
          </div>
        )}
        {idea.status === 'promoted' && task && (
          <div className="small">
            已转成任务：
            <a href="#" onClick={(e) => (e.preventDefault(), openTask(task.id))}>
              {task.title}
            </a>
          </div>
        )}
      </div>

      {(idea.shelvedReason || idea.conditions.length > 0) && (
        <div className="drawer-section">
          <h3>搁置原因与重新考虑的条件</h3>
          {idea.shelvedReason && <p>{idea.shelvedReason}</p>}
          {idea.conditions.map((c) => (
            <div key={c.id} className="row" style={{ alignItems: 'flex-start', flexWrap: 'nowrap' }}>
              {c.met ? (
                <CheckCircle2 size={16} style={{ color: 'var(--success)', flex: 'none', marginTop: 2 }} />
              ) : (
                <Circle size={16} style={{ color: 'var(--faint)', flex: 'none', marginTop: 2 }} />
              )}
              <div className="grow">
                <div className="ink">{c.description}</div>
                <div className="small muted">
                  {conditionKindText[c.kind]}
                  {c.dueAt && !c.met && ` · ${formatWhen(c.dueAt)}`}
                  {c.met && c.metAt && ` · ${formatAgo(c.metAt)}满足`}
                </div>
                {c.metBy && <div style={{ marginTop: 6 }}><SourceLine source={c.metBy} /></div>}
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="drawer-section">
        <h3>演变</h3>
        <Timeline items={idea.evolution} />
        <div className="row" style={{ flexWrap: 'nowrap' }}>
          <input
            className="input"
            placeholder="补充一点新的想法…"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && note.trim()) {
                dispatch({ type: 'ideaNote', id: idea.id, note: note.trim() })
                setNote('')
              }
            }}
          />
          <Button
            size="sm"
            disabled={!note.trim()}
            onClick={() => {
              dispatch({ type: 'ideaNote', id: idea.id, note: note.trim() })
              setNote('')
            }}
          >
            添加
          </Button>
        </div>
      </div>

      {idea.sources.length > 0 && (
        <div className="drawer-section">
          <h3>来源</h3>
          {idea.sources.map((s, i) => (
            <SourceLine key={i} source={s} />
          ))}
        </div>
      )}

      {shelving && (
        <Modal
          title="搁置这个想法"
          onClose={() => setShelving(false)}
          actions={
            <>
              <Button variant="ghost" onClick={() => setShelving(false)}>
                取消
              </Button>
              <Button
                variant="primary"
                disabled={!reason.trim()}
                onClick={() => {
                  dispatch({ type: 'ideaShelve', id: idea.id, reason: reason.trim(), condition: condition.trim() })
                  setShelving(false)
                }}
              >
                搁置
              </Button>
            </>
          }
        >
          <p className="muted small">写下为什么先放一放，以及什么情况下值得重新考虑。条件满足时系统会把它带回来。</p>
          <Field label="搁置原因">
            <input className="input" value={reason} onChange={(e) => setReason(e.target.value)} autoFocus />
          </Field>
          <Field label="重新考虑的条件（可选）">
            <input
              className="input"
              value={condition}
              placeholder="例如：拿到成本数据、下个月初"
              onChange={(e) => setCondition(e.target.value)}
            />
          </Field>
        </Modal>
      )}
    </Drawer>
  )
}
