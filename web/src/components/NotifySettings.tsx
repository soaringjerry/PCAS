import { useEffect, useState } from 'react'
import { api } from '../store/api'
import { Button, Sheet, Switch } from './ui'

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

export function NotifySettings() {
  const [config, setConfig] = useState<Config | null>(null)
  const [enabled, setEnabled] = useState(false)
  const [token, setToken] = useState('')
  const [chatId, setChatId] = useState('')
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState('')
  const unavailable = pushUnavailable()
  const iphone = /iPhone|iPad|iPod/.test(navigator.userAgent)

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
    }).catch((error: Error) => { if (alive) setMessage(error.message) })
    return () => { alive = false }
  }, [])

  async function togglePush(on: boolean) {
    if (busy || !config) return
    setBusy(true)
    setMessage('')
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
      setMessage(on ? '这台设备已开启提醒。' : '这台设备已关闭提醒。')
    } catch (error) {
      if (created) await created.unsubscribe().catch(() => false)
      setMessage((error as Error).message)
    } finally { setBusy(false) }
  }

  async function saveTelegram(remove = false) {
    setBusy(true)
    setMessage('')
    try {
      await api('/v1/notify/telegram', { botToken: remove ? '' : token.trim(), chatId: remove ? '' : chatId.trim() }, 'PUT')
      setToken('')
      const next = await api<Config>('/v1/notify/config')
      setConfig(next)
      setChatId(next.telegram.chatId)
      setMessage(remove ? 'Telegram 已断开。' : 'Telegram 已连接，已发送连接测试消息。')
    } catch (error) { setMessage((error as Error).message) } finally { setBusy(false) }
  }

  return <section className="section" aria-label="通知设置">
    <h2 className="section-title">通知设置</h2>
    <Sheet pad>
      <div className="stack-sm">
        <div className="setting">
          <div><div className="ink">在这台设备上接收提醒</div>
            {unavailable && <p className="small muted">{unavailable}</p>}
            {iphone && <p className="small muted">iPhone 需要先把 PCAS 添加到主屏幕</p>}
          </div>
          <fieldset disabled={busy || !config || (!!unavailable && !enabled)} style={{ border: 0, padding: 0, margin: 0 }}>
            <Switch label="在这台设备上接收提醒" checked={enabled} onChange={value => void togglePush(value)} />
          </fieldset>
        </div>
        <form className="stack-sm" onSubmit={event => { event.preventDefault(); void saveTelegram() }}>
          <div className="spread"><h3>Telegram</h3><span className="small muted">{config ? (config.telegram.configured ? '已连接' : '未连接') : '正在读取…'}</span></div>
          {config?.telegram.configured && <p className="small muted">现在也可以直接给 bot 发消息或语音，等于在首页对秘书说话；发 /new 开始新对话</p>}
          <label className="field"><span className="field-label">Bot token</span>
            <input className="input" type="password" autoComplete="off" value={token} onChange={event => setToken(event.target.value)} aria-label="Telegram bot token" placeholder="测试 bot 的 token" maxLength={256} />
          </label>
          <label className="field"><span className="field-label">Chat ID（可选）</span>
            <input className="input" value={chatId} onChange={event => setChatId(event.target.value)} aria-label="Telegram chat ID" maxLength={32} />
            <span className="small muted">留空会自动检测，请先给你的 bot 发一句话</span>
          </label>
          <div className="row">
            <Button type="submit" disabled={busy || !config || !token.trim()}>保存 Telegram</Button>
            {config?.telegram.configured && <Button type="button" disabled={busy} onClick={() => void saveTelegram(true)}>断开 Telegram</Button>}
          </div>
        </form>
        <div><Button disabled={busy || !config} onClick={async () => {
          setBusy(true); setMessage('')
          try {
            const result = await api<{ sent: string[] }>('/v1/notify/test', {}, 'POST')
            const names: Record<string, string> = { webpush: '浏览器设备（Web Push）', telegram: 'Telegram' }
            setMessage(result.sent.length ? `测试提醒已发到：${result.sent.map(name => names[name] ?? name).join('、')}` : '测试提醒没有送达，请检查通道设置和通知权限。')
          } catch (error) { setMessage((error as Error).message) } finally { setBusy(false) }
        }}>发一条测试提醒</Button></div>
        {message && <p role="status" className="small">{message}</p>}
      </div>
    </Sheet>
  </section>
}
