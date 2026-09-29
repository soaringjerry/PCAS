import { Link } from 'react-router'
import { Check, Lightbulb, Sparkles } from 'lucide-react'
import { spentToday, type LineItem } from '../domain/lines'
import { thingTitle } from '../domain/things'
import { useStore } from '../store/context'
import { useShell } from '../store/shell'
import { useToast } from '../store/toast'

/** One row on a line: what it is, why it is here, and at most one action. */
export function LineRow({ item }: { item: LineItem }) {
  const { state, dispatch, runAgent } = useStore()
  const { agentFor } = useShell()
  const toast = useToast()
  const { thing, next } = item
  const overBudget = next ? spentToday(state) + next.cost > state.settings.dailyBudget : false

  return (
    <Link to={`/t/${thing.id}`} className="lrow">
      {thing.kind === 'task' ? (
        <button
          type="button"
          className={`circle${thing.item.status === 'doing' ? ' doing' : ''}${thing.item.status === 'waiting' ? ' waiting' : ''}`}
          aria-label="完成"
          title="完成"
          onClick={(e) => {
            e.preventDefault()
            e.stopPropagation()
            dispatch({ type: 'setTaskStatus', id: thing.id, status: 'done' })
            toast.show(`完成：${thing.item.title}`)
          }}
        >
          <Check size={12} strokeWidth={3} style={{ opacity: 0 }} />
        </button>
      ) : (
        <span className="glyph-idea" aria-label="想法">
          <Lightbulb size={17} />
        </span>
      )}
      <div className="body">
        <div className="title">{thingTitle(thing)}</div>
        <div className={`reason ${item.tone}`}>{item.reason}</div>
      </div>
      {item.result ? (
        <span className="act show link-btn">查看</span>
      ) : (
        next && (
          <span className="act">
            <button
              type="button"
              className="ai-btn"
              disabled={overBudget}
              title={overBudget ? '今天的额度用完了' : `让副手${next.label}`}
              onClick={(e) => {
                e.preventDefault()
                e.stopPropagation()
                if (runAgent({ thingId: thing.id, agentId: agentFor(thing.id), kind: next.kind, prompt: next.prompt })) {
                  toast.show(`副手开始${next.label}了`)
                }
              }}
            >
              <Sparkles size={12} />
              {next.label}
              <span className="cost">¥{next.cost.toFixed(2)}</span>
            </button>
          </span>
        )
      )}
    </Link>
  )
}
