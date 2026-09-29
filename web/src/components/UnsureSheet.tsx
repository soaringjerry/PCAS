import { useState } from 'react'
import { unsure } from '../domain/lines'
import { memoryKindLabel } from '../domain/labels'
import type { Candidate, CandidateKind } from '../domain/types'
import { useStore } from '../store/context'
import { SideSheet } from './Overlay'
import { Button, Empty } from './ui'

const kindWords: Record<CandidateKind, string> = { unknown: '待分类', task: '待办', idea: '想法', memory: '记忆' }

function CandidateItem({ c }: { c: Candidate }) {
  const { dispatch } = useStore()
  const [kind, setKind] = useState<CandidateKind>(c.kind)
  return (
    <div className="item">
      <div className="grow">
        <div className="item-title">{c.text}</div>
        <div className="meta">
          <span>来自{c.source.label}</span>
        </div>
        <div className="row" style={{ marginTop: 8 }}>
          <select className="inline-select" value={kind} onChange={(e) => setKind(e.target.value as CandidateKind)} aria-label="这是">
            {(Object.keys(kindWords) as CandidateKind[]).map((k) => (
              <option key={k} value={k}>
                作为{kindWords[k]}
              </option>
            ))}
          </select>
          <Button
            size="sm"
            variant="primary"
            disabled={kind === 'unknown'}
            onClick={() => dispatch({ type: 'acceptCandidate', id: c.id, kind, text: c.text, memoryKind: c.memoryKind, projectId: c.projectId, due: c.due })}
          >
            收下
          </Button>
          <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'ignoreCandidate', id: c.id })}>
            不要
          </Button>
        </div>
      </div>
    </div>
  )
}

/** The few things the background could not decide on its own. */
export function UnsureSheet({ onClose }: { onClose: () => void }) {
  const { state, dispatch } = useStore()
  const { candidates, guesses } = unsure(state)
  return (
    <SideSheet title="需要你确认" onClose={onClose}>
      <p className="small muted" style={{ marginBottom: 14 }}>
        把握大的，后台已经自己整理好了。下面这些它拿不准。
      </p>
      {candidates.length + guesses.length === 0 && <Empty>都确认完了。</Empty>}
      {candidates.length > 0 && (
        <div className="sheet" style={{ marginBottom: 16 }}>
          <div className="list">
            {candidates.map((c) => (
              <CandidateItem key={c.id} c={c} />
            ))}
          </div>
        </div>
      )}
      {guesses.length > 0 && (
        <>
          <div className="section-title" style={{ marginBottom: 6 }}>
            从资料里读到的，对吗？
          </div>
          <div className="sheet">
            <div className="list">
              {guesses.map((m) => (
                <div key={m.id} className="item">
                  <div className="grow">
                    <div className="item-title">{m.text}</div>
                    <div className="meta">
                      <span>{memoryKindLabel[m.kind]}</span>
                      {m.sources[0] && <span>来自{m.sources[0].label}</span>}
                    </div>
                    <div className="row" style={{ marginTop: 8 }}>
                      <Button size="sm" variant="primary" onClick={() => dispatch({ type: 'confirmMemory', id: m.id })}>
                        对
                      </Button>
                      <Button size="sm" variant="quiet" onClick={() => dispatch({ type: 'deleteMemory', id: m.id })}>
                        不对
                      </Button>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        </>
      )}
    </SideSheet>
  )
}
