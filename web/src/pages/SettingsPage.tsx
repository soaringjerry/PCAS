import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { NotifySettings, type NotifySummary } from '../components/NotifySettings'
import { TimezoneSettings } from '../components/TimezoneSettings'
import { MemoryActivitySettings } from '../components/MemoryActivitySettings'
import { ConnectorSettings } from '../components/ConnectorSettings'
import { memoriesFor } from '../domain/agent'
import { memoryKindLabel } from '../domain/labels'
import { spentToday } from '../domain/lines'
import { clockTime } from '../domain/time'
import type { Agent, MemoryKind } from '../domain/types'
import { ChevronDown, Clock } from 'lucide-react'
import { Button, Progress, SaveMark, Sheet, Switch, Tag, type SaveState } from '../components/ui'
import { Chip, Select, Stepper } from '../components/controls'
import { useToast } from '../store/toast'
import { useStore } from '../store/context'
import { ChatGPTConnection } from '../components/ChatGPTConnection'
import { OpenAIConnection } from '../components/OpenAIConnection'
import { api, downloadExport } from '../store/api'
import '../styles/settings.css'

// The page is ordered by what someone comes here to do: check the time, get
// reminders, connect a model and cap its cost, bring material in. Keys, URLs and
// protocols sit in rows that say their state while closed and open in place.

/** Half-hour slots, keeping an off-grid saved value selectable. */
function reviewTimes(current: string) {
  const slots = Array.from({ length: 48 }, (_, i) => `${String(Math.floor(i / 2)).padStart(2, '0')}:${i % 2 ? '30' : '00'}`)
  if (!slots.includes(current)) slots.push(current)
  return slots.sort().map((t) => ({ value: t, label: t }))
}

/** Follows one change to the server, so the row that made it can say what happened. */
function useSaving(): [SaveState, (work: () => Promise<boolean>) => Promise<void>] {
  const [state, setState] = useState<SaveState>('idle')
  const timer = useRef<number>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return [
    state,
    async (work) => {
      window.clearTimeout(timer.current)
      setState('saving')
      const ok = await work()
      setState(ok ? 'saved' : 'failed')
      if (ok) timer.current = window.setTimeout(() => setState('idle'), 2500)
    },
  ]
}

function Setting({ title, now, mark = 'idle', children }: { title: ReactNode; now?: ReactNode; mark?: SaveState; children: ReactNode }) {
  return (
    <div className="setting">
      <div className="setting-text">
        <div className="ink">
          {title}
          <SaveMark state={mark} />
        </div>
        {now && <div className="small muted">{now}</div>}
      </div>
      {children}
    </div>
  )
}

/** A switch whose second line says what the current position does. */
function Toggle({ title, label, checked, on, off, save }: {
  title: string
  label: string
  checked: boolean
  on: string
  off: string
  save: (v: boolean) => Promise<boolean>
}) {
  const [mark, run] = useSaving()
  return (
    <Setting title={title} now={checked ? on : off} mark={mark}>
      <Switch label={label} checked={checked} disabled={mark === 'saving'} onChange={(v) => void run(() => save(v))} />
    </Setting>
  )
}

function CityInput({ value, onSave }: { value: string; onSave: (city: string) => void }) {
  const [draft, setDraft] = useState(value)
  const save = () => {
    const city = draft.trim()
    if (city !== value) onSave(city)
  }
  return (
    <input
      className="input city-input"
      aria-label="所在城市"
      placeholder="比如 上海"
      maxLength={60}
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={save}
      onKeyDown={(e) => {
        if (e.key === 'Enter') e.currentTarget.blur()
      }}
    />
  )
}

function useNow(): string {
  const [now, setNow] = useState(() => new Date().toISOString())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(new Date().toISOString()), 30_000)
    return () => window.clearInterval(timer)
  }, [])
  return now
}

const money = (n: number) => `¥${Number.isInteger(n) ? n : n.toFixed(2)}`

/** Models that are switched on and configured. Configured is what the server reports; nobody has test-called them. */
function usable(agents: Agent[]) {
  return agents.filter((a) => a.enabled && a.available && a.channel !== 'manual')
}

/** The four things worth knowing before changing anything; each jumps to its group. */
function Overview({ notify }: { notify: NotifySummary | null }) {
  const { state } = useStore()
  const s = state.settings
  const now = useNow()
  const models = usable(state.agents)
  const channels = [notify?.device && '这台设备', notify?.telegram && 'Telegram'].filter(Boolean)
  const spent = spentToday(state)
  const go = (id: string) => {
    const section = document.getElementById(id)
    section?.scrollIntoView({ block: 'start', behavior: 'smooth' })
    section?.querySelector<HTMLElement>('h2')?.focus({ preventScroll: true })
  }
  const remind = !s.followUps
    ? { value: '事项提醒已停', warn: true }
    : !notify
      ? { value: '正在读取…', warn: false }
      : channels.length
        ? { value: `发到 ${channels.join('、')}`, warn: false }
        : { value: '还没有接收方式', warn: true }
  return (
    <div className="set-overview" role="group" aria-label="当前状态">
      <button type="button" className="set-tile" onClick={() => go('settings-time')}>
        <span className="k">时间</span>
        <span className="v">现在 {clockTime(now, s.timezone ?? 'UTC')}</span>
        <span className="d">{s.timezone ?? 'UTC'}</span>
      </button>
      <button type="button" className={`set-tile${remind.warn ? ' warn' : ''}`} onClick={() => go('settings-remind')}>
        <span className="k">提醒</span>
        <span className="v">{remind.value}</span>
        <span className="d">{remind.warn ? '点这里去处理' : s.followUps ? '事项到时间会提醒' : ''}</span>
      </button>
      <button type="button" className={`set-tile${models.length ? '' : ' warn'}`} onClick={() => go('settings-models')}>
        <span className="k">模型</span>
        <span className="v">{models.length ? `${models.length} 个已配置` : '还没有配置好的模型'}</span>
        <span className="d">{models.length ? models.map((a) => a.name).join('、') : '点这里去连接'}</span>
      </button>
      <button type="button" className={`set-tile${s.dailyBudget > 0 && spent >= s.dailyBudget ? ' warn' : ''}`} onClick={() => go('settings-models')}>
        <span className="k">今天的 API 预算</span>
        <span className="v">
          {money(spent)} <span className="of">/ {money(s.dailyBudget)}</span>
        </span>
        <Progress value={s.dailyBudget > 0 ? Math.min(1, spent / s.dailyBudget) : 0} />
      </button>
    </div>
  )
}

function Group({ id, title, note, children }: { id: string; title: string; note?: ReactNode; children: ReactNode }) {
  return (
    <section className="section" id={id}>
      <h2 className="section-title" tabIndex={-1}>
        {title}
      </h2>
      {note && <p className="section-note">{note}</p>}
      {children}
    </section>
  )
}

const channelText = { mcp: 'MCP', api: '按量计费', manual: '不调用模型' } as const

/** One model: whether work may be handed to it, and which memories it is given. */
function AgentRow({ agent }: { agent: Agent }) {
  const { state, dispatch } = useStore()
  const [open, setOpen] = useState(false)
  const [mark, run] = useSaving()
  const update = (patch: Partial<Agent>) => run(() => dispatch({ type: 'updateAgent', id: agent.id, patch }))
  const kinds = (Object.keys(memoryKindLabel) as MemoryKind[]).filter((k) => agent.memoryKinds.includes(k))
  const seen = memoriesFor(state, agent).length
  const scope = kinds.length ? `能看 ${kinds.map((k) => memoryKindLabel[k]).join('、')}${agent.includeInferred ? '，含未确认的推测' : ''}` : '不给它看记忆'
  const use = !agent.enabled
    ? '已停用，秘书和副手不会用它'
    : agent.channel === 'manual'
      ? '由你复制给别的 AI，再把回答贴回来'
      : agent.available
        ? '已启用，秘书和副手可以把事交给它'
        : '已启用，但它的连接还没配置好，看下面的「连接」'
  return (
    <div className={`agent-row${agent.enabled ? '' : ' off'}`}>
      <div className="setting">
        <div className="setting-text">
          <div className="ink">
            <h3>{agent.name}</h3>
            <Tag tone="info">{agent.protocol === 'codex' || agent.protocol === 'siwc' ? '订阅' : channelText[agent.channel]}</Tag>
            <SaveMark state={mark} />
          </div>
          <div className={`small ${agent.enabled && !agent.available && agent.channel !== 'manual' ? 'warn-text' : 'muted'}`}>{use}</div>
        </div>
        <Switch label={`启用 ${agent.name}`} checked={agent.enabled} disabled={mark === 'saving'} onChange={(v) => void update({ enabled: v })} />
      </div>
      <button type="button" className="scope-toggle" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
        <span>记忆范围</span>
        <span className="muted">{scope}</span>
        <ChevronDown size={14} aria-hidden />
      </button>
      {open && (
        <div className="scope-body">
          {agent.note && <p className="small muted">{agent.note}</p>}
          <div className="row" style={{ gap: 6 }} role="group" aria-label={`${agent.name} 能看到的记忆`}>
            {(Object.keys(memoryKindLabel) as MemoryKind[]).map((kind) => {
              const on = agent.memoryKinds.includes(kind)
              return (
                <Chip
                  key={kind}
                  on={on}
                  disabled={!agent.enabled || mark === 'saving'}
                  onToggle={() => void update({ memoryKinds: on ? agent.memoryKinds.filter((k) => k !== kind) : [...agent.memoryKinds, kind] })}
                >
                  {memoryKindLabel[kind]}
                </Chip>
              )
            })}
          </div>
          <div className="spread">
            <span className="small">
              也给还没确认的推测
              <span className="muted aside">{agent.includeInferred ? '现在：连推测出来、你还没确认的也给' : '现在：只给你确认过或亲口说过的'}</span>
            </span>
            <Switch label="包含推测" checked={agent.includeInferred} disabled={!agent.enabled || mark === 'saving'} onChange={(v) => void update({ includeInferred: v })} />
          </div>
          <span className="tiny muted">按现在的范围，它最多能用到 {seen} 条记忆。</span>
        </div>
      )}
    </div>
  )
}

function Budget() {
  const { state, dispatch } = useStore()
  const [mark, run] = useSaving()
  const s = state.settings
  const spent = spentToday(state)
  return (
    <div className="budget">
      <div className="setting">
        <div className="setting-text">
          <div className="ink">
            按量计费每天最多用
            <SaveMark state={mark} />
          </div>
          <div className="small muted">今天已用和已预留 {money(spent)}，按你填的单价估算，不是供应商账单。</div>
        </div>
        <Stepper label="每日额度" prefix="¥" value={s.dailyBudget} onChange={(v) => void run(() => dispatch({ type: 'updateSettings', patch: { dailyBudget: v } }))} />
      </div>
      <Progress value={s.dailyBudget > 0 ? Math.min(1, spent / s.dailyBudget) : 0} />
      <p className="tiny muted">到上限就先停，每天 0 点（{s.timezone ?? 'UTC'}）重新算。ChatGPT 订阅不算在内，它的额度看账户。</p>
    </div>
  )
}

export function SettingsPage() {
  const { state, dispatch } = useStore()
  const toast = useToast()
  const s = state.settings
  const [notify, setNotify] = useState<NotifySummary | null>(null)
  const [cityMark, saveCity] = useSaving()
  const [reviewMark, saveReview] = useSaving()
  const waiting = state.candidates.filter((c) => c.state === 'pending').length
  const set = (patch: Partial<typeof s>) => dispatch({ type: 'updateSettings', patch })

  return (
    <main className="page page-narrow settings">
      <div className="page-head">
        <h1>设置</h1>
      </div>
      <Overview notify={notify} />

      <div className="stack">
        <Group id="settings-time" title="时间和地点">
          <Sheet>
            <TimezoneSettings />
            <Setting title="所在城市" now="问天气、附近的地方而没说在哪时，按这里查。" mark={cityMark}>
              <CityInput value={s.city ?? ''} onSave={(city) => void saveCity(() => set({ city }))} />
            </Setting>
          </Sheet>
        </Group>

        <NotifySettings id="settings-remind" paused={!s.followUps} onSummary={setNotify}>
          <Toggle
            title="事项到时间提醒我"
            label="事项提醒"
            checked={s.followUps}
            on="现在：按每件事定好的时间提醒，发到下面开通的地方。"
            off="现在：已停。到时间不再提醒，也不往设备和 Telegram 发；定好的时间都还留着。"
            save={(v) => set({ followUps: v })}
          />
          <Toggle
            title="搁置的想法，等的事来了就带回来"
            label="带回搁置的想法"
            checked={s.wakeIdeas}
            on="现在：到了它等的时间，或新资料里提到它等的事，就回到眼前让你核对。"
            off="现在：已停。搁置的想法一直放着，直到你自己去翻。"
            save={(v) => set({ wakeIdeas: v })}
          />
        </NotifySettings>

        <Group id="settings-models" title="模型和预算">
          <div className="stack-sm">
            <Sheet>
              <Budget />
            </Sheet>
            {state.agents.length > 0 && (
              <Sheet>
                {usable(state.agents).length === 0 && <p className="set-next">还没有配置好的模型，秘书没法回答。在下面「连接」里连一个。</p>}
                {state.agents.map((agent) => (
                  <AgentRow key={agent.id} agent={agent} />
                ))}
              </Sheet>
            )}
            <div className="set-sub">连接</div>
            <Sheet>
              {state.agents.length === 0 && <p className="set-next">还没有模型。连上下面任意一种，秘书就能开始工作。</p>}
              <ChatGPTConnection />
              <OpenAIConnection />
            </Sheet>
          </div>
        </Group>

        <Group id="settings-material" title="资料">
          <Sheet>
            <ConnectorSettings>
              {waiting > 0 && (
                <Setting title={`${waiting} 条读到了但拿不准`} now="可能是待办、想法，或一条关于你的记忆，等你定。">
                  <Link className="btn btn-primary btn-sm" to="/library?pending=1">
                    去资料库处理
                  </Link>
                </Setting>
              )}
              <Toggle
                title="资料里说得很明确的待办，直接创建待办"
                label="资料里的明确待办直接创建"
                checked={s.autoAccept}
                on="现在：十分明确的直接创建，其余仍等你确认。你直接跟秘书说的不受影响。"
                off="现在：资料里读到的待办都先等你确认。你直接跟秘书说的不受影响。"
                save={(v) => set({ autoAccept: v })}
              />
              <Setting title="每天几点数一次还有多少没确认" now="到点只记一笔数量，不会重新整理资料。" mark={reviewMark}>
                <Select
                  label="每天汇总时间"
                  icon={<Clock size={13} />}
                  value={s.dailyReviewAt}
                  onChange={(v) => void saveReview(() => set({ dailyReviewAt: v }))}
                  options={reviewTimes(s.dailyReviewAt)}
                />
              </Setting>
            </ConnectorSettings>
          </Sheet>
        </Group>

        <Group id="settings-data" title="数据">
          <Sheet>
            <Setting title="导出全部数据" now="事项、记忆和来源的文字，一个 JSON 文件；原始附件不在里面。">
              <Button size="sm" onClick={() => void downloadExport().catch((e: Error) => toast.show(e.message))}>
                导出
              </Button>
            </Setting>
            <Setting title="退出 PCAS" now="只退出这个浏览器。资料都还在，ChatGPT 和 Telegram 的连接不受影响。">
              <Button size="sm" onClick={async () => { await api('/v1/session', undefined, 'DELETE'); window.location.reload() }}>退出</Button>
            </Setting>
          </Sheet>
        </Group>

        <Group id="settings-advanced" title="高级">
          <Sheet>
            <MemoryActivitySettings />
          </Sheet>
        </Group>
      </div>
    </main>
  )
}
