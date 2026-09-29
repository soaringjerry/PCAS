import { useRef } from 'react'
import { ArrowUp } from 'lucide-react'
import { contextFor, quickActions } from '../domain/agent'
import type { Thing } from '../domain/things'
import { useStore } from '../store/context'
import { useWorkspace } from '../store/workspace'
import { Button } from './ui'

/** Tell an AI what to do on this thing. The draft survives tab switches. */
export function Composer({ thing }: { thing: Thing }) {
  const { state, runAgent } = useStore()
  const { ws, setDraft, setAgentFor, toggleContext } = useWorkspace()
  const input = useRef<HTMLTextAreaElement>(null)
  const draft = ws.drafts[thing.id] ?? ''
  const agentId = ws.agentFor[thing.id] ?? 'a_claude'
  const agent = state.agents.find((a) => a.id === agentId) ?? state.agents[0]
  const included = contextFor(state, thing, agent).filter((c) => c.included).length

  const send = (kind: Parameters<typeof runAgent>[0]['kind'], prompt: string) => {
    if (!prompt.trim()) return
    runAgent({ thingId: thing.id, agentId: agent.id, kind, prompt: prompt.trim() })
    setDraft(thing.id, '')
  }

  return (
    <div className="composer">
      <textarea
        ref={input}
        value={draft}
        rows={1}
        placeholder={`让 ${agent.name} 在这件事上做点什么…（Enter 发送，Shift+Enter 换行）`}
        aria-label="给 AI 的指令"
        onChange={(e) => setDraft(thing.id, e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault()
            send('ask', draft)
          }
        }}
      />
      <div className="composer-bar">
        <select className="inline-select" value={agent.id} onChange={(e) => setAgentFor(thing.id, e.target.value)} aria-label="交给哪个 AI">
          {state.agents
            .filter((a) => a.enabled)
            .map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
                {a.channel === 'manual' ? '（复制粘贴）' : ''}
              </option>
            ))}
        </select>
        {quickActions[thing.kind].map((a) => (
          <button key={a.kind} type="button" className="chip-btn" onClick={() => send(a.kind, a.prompt)}>
            {a.label}
          </button>
        ))}
        <span className="grow" />
        <button type="button" className="btn btn-quiet btn-sm" onClick={toggleContext} title="在右侧调整（⌘.）">
          带上 {included} 条记忆
        </button>
        <Button size="sm" variant="primary" icon={<ArrowUp size={13} />} disabled={!draft.trim()} onClick={() => send('ask', draft)} aria-label="发送" />
      </div>
    </div>
  )
}
