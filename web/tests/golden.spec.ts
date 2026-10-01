import { test, expect } from '@playwright/test'
import { createECDH, randomBytes } from 'node:crypto'
import { command, events, evidence, fixture, input, localTime, login, nextWeekday, reply, say, snapshot, utc } from './support/real'

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1440, height: 1000 } })
test.beforeEach(async ({ page }) => {
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai', followUps: true } })
})
test.afterEach(async ({ page }, info) => { await evidence(page, info) })

const schedule = '周五下午三点和张三对方案，算在 A 项目里'
async function arrange(page: Parameters<typeof say>[0], title: string) {
  let state = await snapshot(page)
  if (!state.projects.some(p => p.name === 'A')) state = await command(page, { type: 'addProject', name: 'A' })
  const due = nextWeekday(5, 15)
  await fixture(page, [reply(schedule, [{ op: 'create_task', title, due, project: 'P1', remind: null }])])
  const response = await say(page, schedule)
  const task = response.state.tasks.find((t: { id: string }) => t.id === response.turn.receipts[0].thingId)
  expect(task).toMatchObject({ due: utc(due), projectId: state.projects.find(p => p.name === 'A')!.id })
  expect(task.triggers[0]).toMatchObject({ offset: '-30m', nextAt: utc(due.replace('15:00', '14:30')) })
  return task
}

test('G1 一句话安排：时间、项目、提醒、刷新与撤销', async ({ page }) => {
  const task = await arrange(page, `和张三对方案-${Date.now()}`)
  const receipt = page.locator('.sec-receipt').filter({ hasText: task.title })
  await expect(receipt).toContainText('15:00')
  await expect(receipt).toContainText('A 项目')
  await expect(receipt).toContainText('14:30 提醒')
  await expect(page.locator('.hall-task').filter({ hasText: task.title })).toBeVisible()
  await evidence(page, test.info(), 'arranged')
  await page.reload()
  await expect(receipt).toBeVisible()
  await expect(page.locator('.hall-task').filter({ hasText: task.title })).toBeVisible()
  await receipt.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(receipt).toContainText('已撤销')
  expect((await snapshot(page)).tasks.some(t => t.id === task.id)).toBeFalsy()
  await expect(page.locator('.hall-task').filter({ hasText: task.title })).toHaveCount(0)
})

test('G2 边问边记：回复前连续发送，两轮都处理', async ({ page }) => {
  const title = `交电费-${Date.now()}`
  await fixture(page, [
    { ...reply('今天怎么安排比较好', [], { reply: '先处理到期事项。' }), delay: 2500 },
    reply('顺便记下明天交电费', [{ op: 'create_task', title, due: localTime(1, 9) }]),
  ])
  const first = say(page, '今天怎么安排比较好')
  await expect(page.getByText('正在想…', { exact: true })).toBeVisible()
  await expect(input(page)).toBeEnabled()
  const second = await say(page, '顺便记下明天交电费')
  await first
  await expect(page.getByText('先处理到期事项。', { exact: true })).toBeVisible()
  expect(second.turn.receipts[0].op).toBe('create_task')
  await expect(page.locator('.hall-task').filter({ hasText: title })).toBeVisible()
  await expect(input(page)).toBeEnabled()
})

test('G3 改安排：同一件事改到周一，提醒跟随', async ({ page }) => {
  const task = await arrange(page, `改期方案-${Date.now()}`)
  const due = nextWeekday(1, 10)
  await fixture(page, [reply('改到下周一上午十点', [{ op: 'update', ref: 'R1', set: { due } }])])
  const response = await say(page, '改到下周一上午十点')
  expect(response.turn.receipts[0].thingId).toBe(task.id)
  const changed = response.state.tasks.find((t: { id: string }) => t.id === task.id)
  expect(changed.due).toBe(utc(due))
  expect(changed.triggers[0].nextAt).toBe(utc(due.replace('10:00', '09:30')))
  expect(response.state.tasks.filter((t: { title: string }) => t.title === task.title)).toHaveLength(1)
  await page.goto(`/t/${task.id}`)
  await expect(page.locator('.info-line')).toContainText('10:00')
  await expect(page.locator('.info-line')).toContainText('09:30 提醒')
})

async function taskPage(page: Parameters<typeof say>[0], title: string) {
  const state = await command(page, { type: 'addTask', title })
  const task = state.tasks.find(t => t.title === title)!
  await page.goto(`/t/${task.id}`)
  return task
}
const breakdown = reply('拆成三步', [{ op: 'delegate', ref: 'THIS', kind: 'breakdown', prompt: '拆成三步' }])

test('G4 事项页秘书：自动加入三步、撤销、放回去', async ({ page }) => {
  const task = await taskPage(page, `准备方案-${Date.now()}`)
  await fixture(page, [breakdown])
  await say(page, '拆成三步')
  const adoption = page.locator('.activity > li').filter({ hasText: '已加入 3 个子任务' })
  await expect(adoption).toBeVisible({ timeout: 20000 })
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)!.checklist).toHaveLength(3)
  await adoption.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(page.getByRole('button', { name: '放回去', exact: true })).toBeVisible()
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)!.checklist).toHaveLength(0)
  await page.getByRole('button', { name: '放回去', exact: true }).click()
  await expect(adoption).toBeVisible()
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)!.checklist).toHaveLength(3)
})

test('G5 失败恢复：副手首请求报错，原地重试成功', async ({ page }) => {
  await taskPage(page, `失败恢复-${Date.now()}`)
  await fixture(page, [breakdown, { kind: 'assistant', match: '', status: 503, once: true }])
  await say(page, '拆成三步')
  const retry = page.getByRole('button', { name: '重试', exact: true })
  await expect(retry).toBeVisible({ timeout: 20000 })
  await expect(page.locator('.activity')).toContainText('没做成：')
  await retry.click()
  await expect(page.locator('.activity')).toContainText('已加入 3 个子任务', { timeout: 20000 })
  expect((await events(page)).filter(e => e.kind === 'model' && e.role === 'assistant')).toHaveLength(2)
})

test('G6 提醒送达：真实等待一分钟、首页、Web Push、Telegram', async ({ page, context }) => {
  test.setTimeout(130000)
  await context.grantPermissions(['notifications'], { origin: process.env.PCAS_TEST_BASE_URL })
  await page.reload()
  test.info().annotations.push({ type: 'push-permission', description: await page.evaluate(() => Notification.permission) })
  await page.evaluate(() => navigator.serviceWorker.ready.then(r => r.active?.state))
  const key = createECDH('prime256v1'); key.generateKeys()
  const subscription = { endpoint: 'https://push.pcas.test/push', keys: { p256dh: key.getPublicKey().toString('base64url'), auth: randomBytes(16).toString('base64url') } }
  expect((await page.request.post('/v1/notify/push-subscriptions', { data: subscription })).status()).toBe(204)
  expect((await page.request.put('/v1/notify/telegram', { data: { botToken: '123456:acceptance-fixture', chatId: '123' } })).ok()).toBeTruthy()
  await fixture(page)
  const title = `一分钟后提醒-${Date.now()}`
  const due = new Date(Date.now() + 60000).toISOString()
  await fixture(page, [reply(title, [{ op: 'create_task', title, due, remind: 'at' }])])
  const response = await say(page, title)
  expect(response.turn.receipts[0]).toMatchObject({ status: 'done', op: 'create_task' })
  await page.goto('/')
  await expect(page.locator('.hall-rang-group').filter({ hasText: title })).toBeVisible({ timeout: 105000 })
  await expect.poll(async () => (await events(page)).some(e => e.kind === 'sendMessage' && String(e.text).includes(title)), { timeout: 25000 }).toBeTruthy()
  await expect.poll(async () => (await events(page)).some(e => e.kind === 'push' && Number(e.bytes) > 0 && e.encoding === 'aes128gcm' && e.authorization === true), { timeout: 25000 }).toBeTruthy()
  await test.info().attach('delivery', { body: JSON.stringify((await events(page)).filter(e => e.kind !== 'model'), null, 2), contentType: 'application/json' })
})

test('G7 Telegram 对话：入站文字、首页、回执按钮和撤销回调', async ({ page }) => {
  test.setTimeout(40000)
  expect((await page.request.put('/v1/notify/telegram', { data: { botToken: '123456:acceptance-fixture', chatId: '123' } })).ok()).toBeTruthy()
  const title = `去银行-${Date.now()}`
  const id = Date.now()
  await fixture(page, [reply('后天上午九点去银行', [{ op: 'create_task', title, due: localTime(2, 9) }])], [{ update_id: id, message: { message_id: id, chat: { id: 123, type: 'private' }, text: '后天上午九点去银行' } }])
  await expect(page.locator('.hall-task').filter({ hasText: title })).toBeVisible({ timeout: 20000 })
  await expect.poll(async () => (await events(page)).some(e => e.kind === 'sendMessage' && e.reply_markup), { timeout: 15000 }).toBeTruthy()
  const sent = (await events(page)).find(e => e.kind === 'sendMessage' && e.reply_markup)!
  expect(String(sent.text)).toContain(title)
  const markup = sent.reply_markup as { inline_keyboard: { text: string; callback_data: string }[][] }
  const undo = markup.inline_keyboard.flat().find(b => b.text.includes('撤销'))!
  expect(undo).toBeTruthy()
  await fixture(page, [], [{ update_id: id + 1, callback_query: { id: `undo-${id}`, from: { id: 123 }, message: { message_id: 101, chat: { id: 123, type: 'private' } }, data: undo.callback_data } }])
  await expect(page.locator('.hall-task').filter({ hasText: title })).toHaveCount(0, { timeout: 15000 })
  expect((await snapshot(page)).tasks.some(t => t.title === title)).toBeFalsy()
  await expect.poll(async () => (await events(page)).some(e => e.kind === 'answerCallbackQuery')).toBeTruthy()
})

test('G8 完成即撤销：首页勾选、toast、回到原位', async ({ page }) => {
  const title = `完成即撤销-${Date.now()}`
  const task = await taskPage(page, title)
  await command(page, { type: 'updateTask', id: task.id, patch: { due: new Date(Date.now() + 3600000).toISOString() } })
  await page.goto('/')
  const row = page.locator('.hall-task').filter({ hasText: title })
  await row.getByRole('button', { name: `做完了：${title}` }).click()
  await expect(page.locator('.toast')).toContainText('做完了')
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)!.status).toBe('done')
  await page.locator('.toast').getByRole('button', { name: '撤销', exact: true }).click()
  await expect(row).toBeVisible()
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)!.status).toBe('todo')
})

test('G9 不丢话：模型真实超时后收到保存回执并能查原话', async ({ page }) => {
  test.setTimeout(115000)
  const text = `超时也要保留这句原话-${Date.now()}`
  await fixture(page, [{ kind: 'secretary', match: text, delay: 95000, content: '{}' }])
  await input(page).fill(text)
  await input(page).press('Enter')
  await expect(page.getByText('已记下原话；模型响应超时，稍后会自动整理', { exact: true })).toBeVisible({ timeout: 105000 })
  await evidence(page, test.info(), 'captured')
  const state = await snapshot(page)
  const sourceID = state.candidates.find(c => c.text === text)!.source.sourceId
  const index = state.sources.findIndex(s => s.id === sourceID)
  expect(index).toBeGreaterThanOrEqual(0)
  await page.goto('/library')
  await page.getByRole('radio', { name: /^来源/ }).click()
  await page.locator('.source-card').nth(index).click()
  await page.getByText('展开原文', { exact: true }).click()
  await expect(page.getByText('展开原文', { exact: true }).locator('..').locator('pre')).toHaveText(text)
})

test('F7 连续撤销：改期、新建依次撤销，刷新保持已撤销', async ({ page }) => {
  const task = await arrange(page, `连续撤销-${Date.now()}`)
  const created = page.locator('.sec-receipt').filter({ hasText: task.title }).first()
  const due = nextWeekday(1, 16)
  await fixture(page, [reply('改到下周一四点', [{ op: 'update', ref: 'R1', set: { due } }])])
  const response = await say(page, '改到下周一四点')
  expect(response.turn.receipts[0].thingId).toBe(task.id)
  const updated = page.locator('.sec-receipt').filter({ hasText: task.title }).last()
  await updated.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(updated).toContainText('已撤销')
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)).toMatchObject({ due: task.due, triggers: task.triggers })
  await created.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(created).toContainText('已撤销')
  expect((await snapshot(page)).tasks.some(t => t.id === task.id)).toBeFalsy()
  await expect(page.locator('.hall-task').filter({ hasText: task.title })).toHaveCount(0)
  await page.reload()
  const receipts = page.locator('.sec-receipt').filter({ hasText: task.title })
  await expect(receipts).toHaveCount(2)
  await expect(receipts.nth(0)).toContainText('已撤销')
  await expect(receipts.nth(1)).toContainText('已撤销')
})

test('F7 内容冲突：改标题后撤销新建返回 changed_since', async ({ page }) => {
  const task = await arrange(page, `撤销冲突-${Date.now()}`)
  await command(page, { type: 'renameThing', id: task.id, title: `${task.title}-用户修改` })
  const receipt = page.locator('.sec-receipt').filter({ hasText: task.title })
  const response = page.waitForResponse(r => r.url().endsWith('/v1/workspace/commands') && r.request().postDataJSON()?.type === 'undoAction')
  await receipt.getByRole('button', { name: '撤销', exact: true }).click()
  const conflict = await response
  expect(conflict.status()).toBe(409)
  expect(await conflict.json()).toEqual({ error: 'changed_since' })
  await expect(receipt).toContainText('这件事之后又改过，没法直接撤销。')
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)?.title).toBe(`${task.title}-用户修改`)
})

test('F7 采纳链：撤销副手采纳后可以撤销更早的改标题', async ({ page }) => {
  const original = `采纳前-${Date.now()}`
  const task = await taskPage(page, original)
  await fixture(page, [reply('改名为准备方案', [{ op: 'update', ref: 'THIS', set: { title: `${original}-准备方案` } }])])
  await say(page, '改名为准备方案')
  await fixture(page, [breakdown])
  await say(page, '拆成三步')
  const adoption = page.locator('.activity > li').filter({ hasText: '已加入 3 个子任务' })
  await expect(adoption).toBeVisible({ timeout: 20000 })
  await adoption.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(page.getByRole('button', { name: '放回去', exact: true })).toBeVisible()
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)!.checklist).toHaveLength(0)
  await page.getByRole('button', { name: /展开对话/ }).click()
  const earlier = page.locator('.sec-turn').filter({ hasText: '改名为准备方案' }).locator('.sec-receipt')
  await earlier.getByRole('button', { name: '撤销', exact: true }).click()
  await expect(earlier).toContainText('已撤销')
  expect((await snapshot(page)).tasks.find(t => t.id === task.id)?.title).toBe(original)
})
