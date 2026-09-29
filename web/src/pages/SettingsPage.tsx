import { memoriesFor } from '../domain/agent'
import { memoryKindLabel } from '../domain/labels'
import { spentToday } from '../domain/lines'
import type { MemoryKind } from '../domain/types'
import { Button, Sheet, Switch, Tag } from '../components/ui'
import { useStore } from '../store/context'
import { ChatGPTConnection } from '../components/ChatGPTConnection'
import { api, downloadExport } from '../store/api'

const channelText = { mcp: 'MCP', api: 'API', manual: '手动' } as const

export function SettingsPage() {
  const { state, dispatch } = useStore()
  const s = state.settings



  return (
    <main className="page page-narrow">
      <div className="page-head">
        <div>
          <h1>设置</h1>
          <p>后台自己做多少、副手能花多少、能看到什么，都由你定。</p>
        </div>
      </div>

      <div className="stack">
        <ChatGPTConnection />
        <section className="section">
          <h2 className="section-title">
            后台
          </h2>
          <Sheet>
            <div className="setting">
              <div>
                <div className="ink">明确要求的待办自动收下</div>
                <div className="small muted">仅对明确的待办指令自动创建任务，其他线索保留供你确认。</div>
              </div>
              <Switch label="自动收下" checked={s.autoAccept} onChange={(v) => dispatch({ type: 'updateSettings', patch: { autoAccept: v } })} />
            </div>
            <div className="setting">
              <div>
                <div className="ink">有相关线索时唤醒放下的想法</div>
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
                <div className="ink">副手每天最多花</div>
                <div className="small muted">API 副手、抽取、向量与转录共同受预算约束。今天已用（含预留） ¥{spentToday(state).toFixed(2)}。</div>
              </div>
              <label className="row-nowrap small">
                ¥
                <input
                  type="number"
                  min={0}
                  step={1}
                  className="inline-select"
                  style={{ width: 64 }}
                  value={s.dailyBudget}
                  onChange={(e) => dispatch({ type: 'updateSettings', patch: { dailyBudget: Math.max(0, Number(e.target.value) || 0) } })}
                  aria-label="每日额度"
                />
              </label>
            </div>
            <div className="setting">
              <div>
                <div className="ink">每日整理</div>
                <div className="small muted">每天汇总待确认线索；新资料到达后即开始整理。</div>
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
            副手
          </h2>
          <p className="small muted">不同的 AI 用同一份记忆，但只看得到你给的部分。换模型不会丢掉积累。</p>
          <div className="agent-grid">
            {state.agents.map((agent) => (
              <Sheet
                key={agent.id}
                title={
                  <div className="row-nowrap">
                    <h3>{agent.name}</h3>
                    <Tag tone="info">{agent.protocol === 'codex' ? '订阅' : channelText[agent.channel]}</Tag>
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
            数据
          </h2>
          <Sheet>
            <div className="setting">
              <div>
                <div className="ink">导出全部数据</div>
                <div className="small muted">事情、记忆、来源和训练数据，一个 JSON 文件。</div>
              </div>
              <Button size="sm" onClick={() => void downloadExport().catch((e: Error) => window.alert(e.message))}>
                导出
              </Button>
            </div>
            <div className="setting"><div><div className="ink">退出 PCAS</div><div className="small muted">结束此浏览器会话，服务端资料继续保留。</div></div>
              <Button size="sm" onClick={async () => { await api('/v1/session', undefined, 'DELETE'); window.location.reload() }}>退出</Button>
            </div>
          </Sheet>
        </section>
      </div>
    </main>
  )
}
