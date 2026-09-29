import { memoriesFor } from '../domain/agent'
import { memoryKindLabel } from '../domain/labels'
import type { MemoryKind } from '../domain/types'
import { Button, Sheet, Switch, Tag } from '../components/ui'
import { useStore } from '../store/context'
import { useToast } from '../store/toast'

const channelText = { mcp: 'MCP', api: 'API', manual: '手动' } as const

export function SettingsPage() {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const s = state.settings

  const exportAll = () => {
    const url = URL.createObjectURL(new Blob([JSON.stringify(state, null, 2)], { type: 'application/json' }))
    const a = document.createElement('a')
    a.href = url
    a.download = `pcas-export-${new Date().toISOString().slice(0, 10)}.json`
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <main className="page page-narrow">
      <div className="page-head">
        <div>
          <h1>设置</h1>
          <p>系统能自己做多少、哪个 AI 能看到什么，都由你定。</p>
        </div>
      </div>

      <div className="stack">
        <section className="section">
          <h2 className="section-title">
            <span className="squiggle">自己动手的范围</span>
          </h2>
          <Sheet>
            <div className="setting">
              <div>
                <div className="ink">把握很大的记录直接收下</div>
                <div className="small muted">关掉时，所有记下的东西都先等你确认。</div>
              </div>
              <Switch label="自动收下" checked={s.autoAccept} onChange={(v) => dispatch({ type: 'updateSettings', patch: { autoAccept: v } })} />
            </div>
            <div className="setting">
              <div>
                <div className="ink">条件满足时唤醒放下的想法</div>
                <div className="small muted">会告诉你是哪份资料、哪个条件让它回来的。</div>
              </div>
              <Switch label="自动唤醒" checked={s.wakeIdeas} onChange={(v) => dispatch({ type: 'updateSettings', patch: { wakeIdeas: v } })} />
            </div>
            <div className="setting">
              <div>
                <div className="ink">按规则发跟进提醒</div>
                <div className="small muted">比如“收到邮件三天后还没回就提醒”。发之前会先核对，已经处理的就不提醒。</div>
              </div>
              <Switch label="跟进提醒" checked={s.followUps} onChange={(v) => dispatch({ type: 'updateSettings', patch: { followUps: v } })} />
            </div>
            <div className="setting">
              <div>
                <div className="ink">每日整理</div>
                <div className="small muted">每天这个时间整理一次新资料，并检查放下的想法。</div>
              </div>
              <input
                type="time"
                className="inline-select"
                value={s.dailyReviewAt}
                onChange={(e) => dispatch({ type: 'updateSettings', patch: { dailyReviewAt: e.target.value } })}
                aria-label="每日整理时间"
              />
            </div>
          </Sheet>
        </section>

        <section className="section">
          <h2 className="section-title">
            <span className="squiggle">AI 接入</span>
          </h2>
          <p className="small muted">不同的 AI 用同一份记忆，但只看得到你给的部分。换模型不会丢掉积累。</p>
          <div className="agent-grid">
            {state.agents.map((agent) => (
              <Sheet
                key={agent.id}
                title={
                  <div className="row-nowrap">
                    <h3>{agent.name}</h3>
                    <Tag tone="info">{channelText[agent.channel]}</Tag>
                  </div>
                }
                aside={<Switch label={`启用 ${agent.name}`} checked={agent.enabled} onChange={(v) => dispatch({ type: 'updateAgent', id: agent.id, patch: { enabled: v } })} />}
                pad
              >
                <div className="stack-sm" style={{ opacity: agent.enabled ? 1 : 0.5 }}>
                  <p className="small muted">{agent.note}</p>
                  <div className="row">
                    <span className="small muted">能看：</span>
                    {(Object.keys(memoryKindLabel) as MemoryKind[]).map((kind) => {
                      const on = agent.memoryKinds.includes(kind)
                      return (
                        <label key={kind} className="check small">
                          <input
                            type="checkbox"
                            checked={on}
                            disabled={!agent.enabled}
                            onChange={() =>
                              dispatch({
                                type: 'updateAgent',
                                id: agent.id,
                                patch: { memoryKinds: on ? agent.memoryKinds.filter((k) => k !== kind) : [...agent.memoryKinds, kind] },
                              })
                            }
                          />
                          {memoryKindLabel[kind]}
                        </label>
                      )
                    })}
                  </div>
                  <div className="spread">
                    <span className="small">也给没确认的推测</span>
                    <Switch
                      label="包含推测"
                      checked={agent.includeInferred}
                      onChange={(v) => agent.enabled && dispatch({ type: 'updateAgent', id: agent.id, patch: { includeInferred: v } })}
                    />
                  </div>
                  <span className="tiny muted">现在能看到 {memoriesFor(state, agent).length} 条记忆</span>
                </div>
              </Sheet>
            ))}
          </div>
        </section>

        <section className="section">
          <h2 className="section-title">
            <span className="squiggle">数据</span>
          </h2>
          <Sheet>
            <div className="setting">
              <div>
                <div className="ink">导出全部数据</div>
                <div className="small muted">事情、记忆、来源和训练数据，一个 JSON 文件。</div>
              </div>
              <Button size="sm" onClick={exportAll}>
                导出
              </Button>
            </div>
            <div className="setting">
              <div>
                <div className="ink">恢复示例数据</div>
                <div className="small muted">这是交互原型，数据只存在这个浏览器里。</div>
              </div>
              <Button
                size="sm"
                variant="danger"
                onClick={() => {
                  if (window.confirm('恢复到初始示例数据？现在的改动会丢掉。')) {
                    dispatch({ type: 'reset' })
                    toast.show('恢复好了')
                  }
                }}
              >
                恢复
              </Button>
            </div>
          </Sheet>
        </section>
      </div>
    </main>
  )
}
