import { memoriesFor } from '../domain/handoff'
import { memoryKindLabel } from '../domain/labels'
import type { MemoryKind } from '../domain/types'
import { Badge, Card, Switch } from '../components/ui'
import { useStore } from '../store/context'

const channelText = { mcp: 'MCP', api: 'API', manual: '手动' } as const

export function AgentsPage() {
  const { state, dispatch } = useStore()
  return (
    <main className="page">
      <div className="page-head">
        <div>
          <h1>AI 接入</h1>
          <p>不同的 AI 共用一份记忆，但每个 AI 只能看到你授权的范围。切换模型不会丢失积累。</p>
        </div>
      </div>
      <div className="grid-2">
        {state.agents.map((agent) => {
          const visible = memoriesFor(state, agent).length
          return (
            <Card
              key={agent.id}
              title={
                <span className="row">
                  {agent.name}
                  <Badge tone="info">{channelText[agent.channel]}</Badge>
                </span>
              }
              hint={agent.note}
              action={<Switch label={`启用 ${agent.name}`} checked={agent.enabled} onChange={(v) => dispatch({ type: 'updateAgent', id: agent.id, patch: { enabled: v } })} />}
              pad
            >
              <div style={{ opacity: agent.enabled ? 1 : 0.5 }}>
                <div className="toggle-row">
                  <span>可读取的记忆</span>
                  <div className="row">
                    {(Object.keys(memoryKindLabel) as MemoryKind[]).map((kind) => {
                      const checked = agent.memoryKinds.includes(kind)
                      return (
                        <label key={kind} className="check">
                          <input
                            type="checkbox"
                            checked={checked}
                            disabled={!agent.enabled}
                            onChange={() =>
                              dispatch({
                                type: 'updateAgent',
                                id: agent.id,
                                patch: { memoryKinds: checked ? agent.memoryKinds.filter((k) => k !== kind) : [...agent.memoryKinds, kind] },
                              })
                            }
                          />
                          {memoryKindLabel[kind]}
                        </label>
                      )
                    })}
                  </div>
                </div>
                <div className="toggle-row">
                  <div>
                    <div>包含 AI 推测</div>
                    <div className="small muted">默认只提供你确认过的内容</div>
                  </div>
                  <Switch
                    label="包含 AI 推测"
                    checked={agent.includeInferred}
                    onChange={(v) => agent.enabled && dispatch({ type: 'updateAgent', id: agent.id, patch: { includeInferred: v } })}
                  />
                </div>
                <div className="toggle-row">
                  <span className="muted">当前可见</span>
                  <span className="ink">{visible} 条记忆</span>
                </div>
              </div>
            </Card>
          )
        })}
      </div>
    </main>
  )
}
