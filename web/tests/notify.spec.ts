import { test, expect } from '@playwright/test'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'

function emptyState() {
  return { version: 1, revision: 1, budgetUsage: 0, settings: { dailyBudget: 10, timezone: 'UTC', dailyReviewAt: '09:00' }, tasks: [], ideas: [], projects: [], memories: [], candidates: [], agents: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], notices: [], excludedMemories: {} }
}

test('Telegram saves the requested body, reports delivery, and disconnects', async ({ page }) => {
  let configured = false
  const requests: unknown[] = []
  const fakeToken = crypto.randomUUID()
  await page.route('**/v1/workspace', route => route.fulfill({ json: emptyState() }))
  await page.route('**/v1/notify/config', route => route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured, chatId: configured ? '123' : '' } } }))
  await page.route('**/v1/notify/telegram', route => {
    expect(route.request().method()).toBe('PUT')
    const body = route.request().postDataJSON()
    requests.push(body); configured = !!body.botToken
    return route.fulfill({ json: { configured } })
  })
  await page.route('**/v1/notify/test', route => route.fulfill({ json: { sent: ['webpush', 'telegram'] } }))
  await page.goto('/settings')
  const section = page.getByRole('region', { name: '通知设置' })
  await expect(section).toBeVisible()
  await expect(section.getByText('未连接', { exact: true })).toBeVisible()
  await expect(page.getByLabel('Telegram bot token')).toHaveAttribute('type', 'password')
  await page.getByLabel('Telegram bot token').fill(fakeToken)
  await page.getByLabel('Telegram chat ID').fill('123')
  await page.getByRole('button', { name: '保存 Telegram', exact: true }).click()
  await expect(section.getByText('已连接', { exact: true })).toBeVisible()
  expect(requests[0]).toEqual({ botToken: fakeToken, chatId: '123' })
  await expect(page.getByLabel('Telegram bot token')).toHaveValue('')
  await page.getByRole('button', { name: '发一条测试提醒' }).click()
  await expect(section.getByRole('status')).toHaveText('测试提醒已发到：浏览器设备（Web Push）、Telegram')
  await page.getByRole('button', { name: '断开 Telegram' }).click()
  await expect(section.getByText('未连接', { exact: true })).toBeVisible()
  expect(requests[1]).toEqual({ botToken: '', chatId: '' })
})


test('Telegram save errors show actionable Chinese prompts', async ({ page }) => {
  const cases = [
    ['telegram_token_invalid', 'token 不对，请从 BotFather 重新复制'],
    ['telegram_webhook_active', '这个 bot 设置过 webhook，请换一个 bot 或先删除 webhook'],
    ['telegram_no_chat', '请先在 Telegram 里给你的 bot 发一句话，再保存'],
    ['telegram_send_failed', '测试消息没发出去，请检查 chat ID'],
  ]
  let errorCode = cases[0][0]
  await page.route('**/v1/workspace', route => route.fulfill({ json: emptyState() }))
  await page.route('**/v1/notify/config', route => route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } }))
  await page.route('**/v1/notify/telegram', route => {
    expect(route.request().method()).toBe('PUT')
    return route.fulfill({ status: 400, json: { error: errorCode } })
  })
  await page.goto('/settings')
  const section = page.getByRole('region', { name: '通知设置' })
  const token = '123456:synthetic_test_token'
  await page.getByLabel('Telegram bot token').fill(token)
  for (const [code, message] of cases) {
    errorCode = code
    await page.getByRole('button', { name: '保存 Telegram', exact: true }).click()
    await expect(section.getByRole('status')).toHaveText(message)
    await expect(section.getByText('未连接', { exact: true })).toBeVisible()
    await expect(page.getByLabel('Telegram bot token')).toHaveValue(token)
  }
})

test('device switch registers and removes the current subscription', async ({ page }) => {
  await page.addInitScript(() => {
    let sub: { endpoint: string; toJSON: () => unknown; unsubscribe: () => Promise<boolean> } | null = null
    Object.defineProperty(window, 'Notification', { value: { permission: 'default', requestPermission: async () => 'granted' } })
    Object.defineProperty(window, 'PushManager', { value: function () {} })
    const worker = { pushManager: {
      getSubscription: async () => sub,
      subscribe: async (options: { userVisibleOnly: boolean; applicationServerKey: ArrayBuffer }) => {
        if (!options.userVisibleOnly || !(options.applicationServerKey instanceof ArrayBuffer)) throw new Error('wrong push options')
        sub = { endpoint: 'https://push.example.com/device', toJSON: () => ({ keys: { p256dh: 'mock-public', auth: 'mock-auth' } }), unsubscribe: async () => { sub = null; return true } }
        return sub
      },
    } }
    Object.defineProperty(navigator, 'serviceWorker', { value: { ready: Promise.resolve(worker), register: async () => worker } })
  })
  const requests: { method: string; body: unknown }[] = []
  await page.route('**/v1/workspace', route => route.fulfill({ json: emptyState() }))
  await page.route('**/v1/notify/config', route => route.fulfill({ json: { webPush: { publicKey: 'AQID', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } }))
  await page.route('**/v1/notify/push-subscriptions', route => {
    requests.push({ method: route.request().method(), body: route.request().postDataJSON() })
    return route.fulfill({ status: 204 })
  })
  await page.goto('/settings')
  const toggle = page.getByRole('switch', { name: '在这台设备上接收提醒' })
  await expect(toggle).toBeEnabled()
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-checked', 'true')
  expect(requests[0]).toEqual({ method: 'POST', body: { endpoint: 'https://push.example.com/device', keys: { p256dh: 'mock-public', auth: 'mock-auth' } } })
  await toggle.click()
  await expect(toggle).toHaveAttribute('aria-checked', 'false')
  expect(requests[1]).toEqual({ method: 'DELETE', body: { endpoint: 'https://push.example.com/device' } })
})

test('unsupported browser explains the limitation and empty delivery result', async ({ page }) => {
  await page.addInitScript(() => { delete (window as unknown as { PushManager?: unknown }).PushManager })
  await page.route('**/v1/workspace', route => route.fulfill({ json: emptyState() }))
  await page.route('**/v1/notify/config', route => route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } }))
  await page.route('**/v1/notify/test', route => route.fulfill({ json: { sent: [] } }))
  await page.goto('/settings')
  await expect(page.getByRole('switch', { name: '在这台设备上接收提醒' })).toBeDisabled()
  await expect(page.getByText('此浏览器不支持推送提醒。')).toBeVisible()
  await page.getByRole('button', { name: '发一条测试提醒' }).click()
  await expect(page.getByRole('status').filter({ hasText: '测试提醒没有送达' })).toBeVisible()
})

test('service worker displays notifications and navigates existing or new windows', async () => {
  type Event = { data?: { json: () => unknown }; notification?: { data: { url: string }; close: () => void }; waitUntil: (promise: Promise<unknown>) => void }
  const listeners: Record<string, (event: Event) => void> = {}
  const notifications: unknown[] = [], opened: string[] = [], navigated: string[] = []
  let focused = 0, closed = 0
  let windows: unknown[] = []
  const origin = 'https://pcas.example.com'
  const self = {
    location: { origin },
    addEventListener: (name: string, callback: (event: Event) => void) => { listeners[name] = callback },
    registration: { showNotification: async (title: string, options: unknown) => { notifications.push({ title, options }) } },
    clients: { matchAll: async () => windows, openWindow: async (url: string) => { opened.push(url) } },
  }
  runInNewContext(readFileSync(new URL('../public/sw.js', import.meta.url), 'utf8'), { self, URL })
  expect(listeners.fetch).toBeUndefined()
  let work: Promise<unknown> = Promise.resolve()
  const waitUntil = (promise: Promise<unknown>) => { work = promise }
  listeners.push({ data: { json: () => ({ title: '提醒事项', body: '15:00 · 截止前提醒', noticeId: 'notice-1', url: origin + '/t/task-1' }) }, waitUntil })
  await work
  expect(notifications).toEqual([{ title: '提醒事项', options: { body: '15:00 · 截止前提醒', tag: 'notice-1', icon: '/icon-192.png', data: { url: origin + '/t/task-1' } } }])
  const event = { notification: { data: { url: origin + '/t/task-1' }, close: () => { closed++ } }, waitUntil }
  const client = { url: origin + '/', navigate: async (url: string) => { navigated.push(url); return client }, focus: async () => { focused++ } }
  windows = [client]
  listeners.notificationclick(event); await work
  expect(navigated).toEqual([origin + '/t/task-1']); expect(focused).toBe(1); expect(opened).toEqual([])
  windows = []
  listeners.notificationclick(event); await work
  expect(opened).toEqual([origin + '/t/task-1']); expect(closed).toBe(2)
  listeners.notificationclick({ ...event, notification: { ...event.notification, data: { url: 'https://outside.example.com' } } }); await work
  expect(opened).toHaveLength(1)
})
