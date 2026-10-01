import { useEffect, useState, type ReactNode } from 'react'
import { api } from '../store/api'
import { Button, Fold, Sheet, Switch } from './ui'

type Config = {
  webPush: { publicKey: string; subscriptions: number }
  telegram: { configured: boolean; chatId: string }
}

function pushUnavailable() {
  if (!window.isSecureContext) return '浏览器提醒需要 HTTPS 或本机地址。'
  if (!('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window)) return '此浏览器不支持推送提醒。'
  if (Notification.permission === 'denied') return '通知权限已关闭，请在浏览器设置中允许 PCAS 发送通知。'
  return ''
}

function publicKey(value: string): ArrayBuffer {
  const raw = atob(value.replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(raw, char => char.charCodeAt(0)).buffer
}

async function registration() {
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    return await Promise.race([
      navigator.serviceWorker.ready,
      new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error('提醒服务未能启动，请刷新页面后重试。')), 10000) }),
    ])
  } finally { clearTimeout(timer) }
}

type Place = 'device' | 'telegram' | 'test'

/** What the overview at the top of the settings page needs to say about reminders. */
export type NotifySummary = { device: boolean; telegram: boolean }

/** 提醒: what gets reminded (the rows passed in), then where reminders are sent. */
export function NotifySettings({ id, paused = false, onSummary, children }: {
  id?: string
  /** The workspace switch for task reminders is off, so the channels below receive none. */
  paused?: boolean
  onSummary?: (summary: NotifySummary) => void
  children?: ReactNode
}) {
  const [config, setConfig] = useState<Config | null>(null)
  const [enabled, setEnabled] = useState(false)
  const [token, setToken] = useState('')
  const [chatId, setChatId] = useState('')
  const [busy, setBusy] = useState(false)
  /** Shown beside the control it is about: the device switch, the Telegram form, or the test button. */
  const [message, setMessage] = useState<{ at: Place; text: string; failed?: boolean } | null>(null)
  const unavailable = pushUnavailable()
  const iphone = /iPhone|iPad|iPod/.test(navigator.userAgent)
  const fail = (at: Place, error: unknown) => setMessage({ at, text: (error as Error).message, failed: true })
  const said = (at: Place) => message?.at === at && <p role={message.failed ? 'alert' : 'status'} className={`small${message.failed ? ' warn-text' : ''}`}>{message.text}</p>

  useEffect(() => {
    let alive = true
    void api<Config>('/v1/notify/config').then(async value => {
      if (!alive) return
      setConfig(value)
      setChatId(value.telegram.chatId)
      if (window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window) {
        const worker = await registration()
        const sub = await worker.pushManager.getSubscription()
        if (alive) setEnabled(!!sub)
      }
    }).catch((error: Error) => { if (alive) fail('device', error) })
    return () => { alive = false }
  }, [])

  const telegram = !!config?.telegram.configured
  useEffect(() => { if (config) onSummary?.({ device: enabled, telegram }) }, [config, enabled, telegram, onSummary])

  async function togglePush(on: boolean) {
    if (busy || !config) return
    setBusy(true)
    setMessage(null)
    let created: PushSubscription | null = null
    try {
      // Request permission directly from the click gesture, before any await.
      if (on && await Notification.requestPermission() !== 'granted') throw new Error('未获得通知权限，请在浏览器设置中允许通知。')
      const worker = await registration()
      const current = await worker.pushManager.getSubscription()
      if (on) {
        const sub = current ?? (created = await worker.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: publicKey(config.webPush.publicKey) }))
        const json = sub.toJSON()
        await api('/v1/notify/push-subscriptions', { endpoint: sub.endpoint, keys: json.keys })
      } else if (current) {
        await api('/v1/notify/push-subscriptions', { endpoint: current.endpoint }, 'DELETE')
        if (!await current.unsubscribe()) throw new Error('设备订阅未能关闭，请重试。')
      }
      setEnabled(on)
      setMessage({ at: 'device', text: on ? '这台设备已开启提醒。' : '这台设备已关闭提醒。' })
    } catch (error) {
      if (created) await created.unsubscribe().catch(() => false)
      fail('device', error)
    } finally { setBusy(false) }
  }

  async function saveTelegram(remove = false) {
    setBusy(true)
    setMessage(null)
    try {
      await api('/v1/notify/telegram', { botToken: remove ? '' : token.trim(), chatId: remove ? '' : chatId.trim() }, 'PUT')
      setToken('')
      const next = await api<Config>('/v1/notify/config')
      setConfig(next)
      setChatId(next.telegram.chatId)
      setMessage({ at: 'telegram', text: remove ? 'Telegram 已断开。' : 'Telegram 已连接，已发送连接测试消息。' })
    } catch (error) { fail('telegram', error) } finally { setBusy(false) }
  }

  return <section className="section" id={id} aria-label="通知设置">
    <h2 className="section-title" tabIndex={-1}>提醒</h2>
    <div className="stack-sm">
      {children && <Sheet>{children}</Sheet>}
      <div className="set-sub">发到哪里{paused && <span className="warn-text aside">事项提醒已停，下面开着也收不到事项提醒</span>}</div>
      <Sheet>
        <div className="setting">
          <div className="setting-text"><div className="ink">这台设备</div>
            <p className="small muted">{!config ? '正在读取…' : enabled ? '已开通，提醒会弹在这台设备上。' : unavailable || '还没开通。打开后浏览器会问你是否允许通知。'}</p>
            {iphone && <p className="small muted">iPhone 需要先把 PCAS 添加到主屏幕</p>}
            {said('device')}
          </div>
          <fieldset disabled={busy || !config || (!!unavailable && !enabled)} style={{ border: 0, padding: 0, margin: 0 }}>
            <Switch label="在这台设备上接收提醒" checked={enabled} onChange={value => void togglePush(value)} />
          </fieldset>
        </div>
        <Fold title="Telegram" summary={config ? (telegram ? '已连接' : '未连接') : '正在读取…'} tone={telegram ? 'ok' : undefined}>
          <form className="stack-sm" onSubmit={event => { event.preventDefault(); void saveTelegram() }}>
            <p className="small muted">{telegram
              ? '提醒会发到你的 bot。也可以直接给 bot 发消息或语音，等于在首页对秘书说话；发 /new 开始新对话。'
              : '用你自己的 Telegram bot 收提醒：在 Telegram 里找 BotFather 建一个 bot，把它给的 token 贴到这里，保存时会先发一条消息试试。'}</p>
            <label className="field"><span className="field-label">{telegram ? '换一个 bot：新的 Bot token' : 'Bot token'}</span>
              <input className="input" type="password" autoComplete="off" value={token} onChange={event => setToken(event.target.value)} aria-label="Telegram bot token" placeholder="BotFather 给的 token" maxLength={256} />
            </label>
            <label className="field"><span className="field-label">Chat ID（可选）</span>
              <input className="input" value={chatId} onChange={event => setChatId(event.target.value)} aria-label="Telegram chat ID" maxLength={32} />
              <span className="small muted">留空会自动检测，请先给你的 bot 发一句话</span>
            </label>
            <div className="row">
              <Button type="submit" disabled={busy || !config || !token.trim()}>保存 Telegram</Button>
              {telegram && <Button type="button" disabled={busy} onClick={() => void saveTelegram(true)}>断开 Telegram</Button>}
            </div>
            {said('telegram')}
          </form>
        </Fold>
        <div className="setting set-test">
          <Button size="sm" disabled={busy || !config} onClick={async () => {
            setBusy(true); setMessage(null)
            try {
              const result = await api<{ sent: string[] }>('/v1/notify/test', {}, 'POST')
              const names: Record<string, string> = { webpush: '浏览器设备（Web Push）', telegram: 'Telegram' }
              setMessage(result.sent.length ? { at: 'test', text: `测试提醒已发到：${result.sent.map(name => names[name] ?? name).join('、')}` } : { at: 'test', text: '测试提醒没有送达，请检查通道设置和通知权限。', failed: true })
            } catch (error) { fail('test', error) } finally { setBusy(false) }
          }}>发一条测试提醒</Button>
          {said('test')}
        </div>
      </Sheet>
    </div>
  </section>
}
