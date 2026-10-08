import { test, expect, type Page } from '@playwright/test'

const at = '2026-10-03T04:00:00Z'
const sourceId = '22222222-2222-4222-8222-222222222222'
const image = { name: '模拟缴费通知.png', mimeType: 'image/png', buffer: Buffer.from('synthetic image') }

async function mock(page: Page, failUpload = false) {
  const requests: Record<string, unknown>[] = []
  const uploads: string[] = []
  const errors: string[] = []
  const task = { id: 'task', title: '交物业费 800 元', notes: '', status: 'todo', due: '2027-10-15T00:00:00Z', dependsOn: [], checklist: [], triggers: [], sources: [], history: [], createdAt: at, updatedAt: at }
  const state = { version: 1, revision: 1, budgetUsage: 0, notices: [], settings: { timezone: 'Asia/Shanghai', city: '上海', dailyBudget: 10, autoAccept: false, wakeIdeas: true, followUps: true, dailyReviewAt: '09:00' }, tasks: [task], ideas: [], projects: [], agents: [{ id: 'fake', name: '模拟秘书', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {} }
  page.on('pageerror', (error) => errors.push(error.message))
  await page.route('**/v1/**', (route) => route.fulfill({ status: 500, json: { error: 'unexpected_mock_request' } }))
  // The hall's own reads (phase 3.5): no dates and nothing under 在推进.
  await page.route('**/v1/workspace/schedule?*', (route) => route.fulfill({ json: { days: [], unclear: [], overdue: [] } }))
  await page.route('**/v1/workspace/in-progress', (route) => route.fulfill({ json: { items: [], total: 0, remaining: 0 } }))
  await page.route('**/v1/workspace', (route) => route.fulfill({ json: state }))
  await page.route('**/v1/desk/turns?*', (route) => route.fulfill({ json: { turns: [] } }))
  await page.route('**/v1/memory/summary*', (route) => route.fulfill({ json: { text: '', coverage: { gaps: [] }, dependencies: [] } }))
  await page.route('**/v1/memory/attachments', (route) => {
    uploads.push(route.request().postData() ?? '')
    if (failUpload) { failUpload = false; return route.fulfill({ status: 500, json: { error: 'unavailable' } }) }
    return route.fulfill({ status: 201, json: { id: sourceId, version: 1, kind: 'source' } })
  })
  await page.route('**/v1/desk/turn', (route) => {
    const request = route.request().postDataJSON() as Record<string, unknown>
    requests.push(request)
    return route.fulfill({ json: { conversationId: request.conversationId, state, turn: { id: `turn-${request.requestId}`, text: request.text, reply: '看到了缴费通知，已安排。', cards: [], receipts: [{ actionId: 'image-action', sourceId, sourceVersion: 1, op: 'attachment', text: '存了一张图片', thingId: null, status: 'done', undoable: true }, { actionId: 'task-action', op: 'create_task', text: '已建：交物业费 800 元', thingId: 'task', status: 'done', undoable: true }], ask: null, agent: '模拟秘书', createdAt: at } } })
  })
  await page.route(`**/v1/memory/sources/${sourceId}?*`, (route) => route.fulfill({ json: { source: { id: sourceId, version: 1, title: image.name, text: '', has_attachment: true, attachment_missing: false, representation: 'original', recorded_at: at }, derived: [], processing: [{ id: 'parse', stage: 'source.parse', state: 'done', method: 'vision' }] } }))
  return { requests, uploads, errors }
}

async function pasteImage(page: Page) {
  await page.getByRole('textbox', { name: '跟秘书说' }).evaluate((element) => {
    const data = new DataTransfer()
    data.items.add(new File(['synthetic image'], '粘贴图片.png', { type: 'image/png' }))
    element.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }))
  })
}
async function dropImage(page: Page) {
  await page.locator('.sec-dock').evaluate((element) => {
    const data = new DataTransfer()
    data.items.add(new File(['synthetic image'], '拖入图片.png', { type: 'image/png' }))
    element.dispatchEvent(new DragEvent('drop', { dataTransfer: data, bubbles: true, cancelable: true }))
  })
}

for (const path of ['/', '/t/task']) {
  for (const width of [1280, 390]) {
    test(`V9 selection paste drop and send at ${width}px on ${path}`, async ({ page }) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
      const m = await mock(page)
      await page.goto(path)
      const input = page.getByRole('textbox', { name: '跟秘书说' })
      const chooserPromise = page.waitForEvent('filechooser')
      await page.getByRole('button', { name: '添加附件' }).click()
      await (await chooserPromise).setFiles(image)
      await expect(page.getByRole('list', { name: '待发送的附件' })).toContainText(image.name)
      await page.getByRole('button', { name: `去掉附件：${image.name}` }).click()
      await expect(page.getByRole('list', { name: '待发送的附件' })).toHaveCount(0)
      await pasteImage(page)
      await expect(page.getByRole('list', { name: '待发送的附件' })).toContainText('粘贴图片.png')
      await page.getByRole('button', { name: '去掉附件：粘贴图片.png' }).click()
      await dropImage(page)
      await expect(page.getByRole('list', { name: '待发送的附件' })).toContainText('拖入图片.png')
      await input.fill('帮我记一下')
      await page.getByRole('button', { name: '发送', exact: true }).click()
      await expect(page.getByText('看到了缴费通知，已安排。')).toBeVisible()
      await expect(input).toHaveValue('')
      await expect(page.getByRole('list', { name: '待发送的附件' })).toHaveCount(0)
      expect(m.requests).toHaveLength(1)
      expect(m.requests[0]).toMatchObject({ text: '帮我记一下', thingId: path === '/' ? null : 'task', attachments: [{ id: sourceId, version: 1, kind: 'source' }] })
      await page.getByRole('button', { name: '存了一张图片' }).click()
      await expect(page.getByRole('dialog')).toContainText('模型读图')
      await expect(page.getByRole('link', { name: /原件|下载|打开/ }).first()).toHaveAttribute('href', new RegExp(`/v1/memory/sources/${sourceId}/attachment`))
      await page.keyboard.press('Escape')
      await page.getByLabel('选择附件').setInputFiles(image)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
      if (path === '/' && process.env.PCAS_M1_SCREENSHOT_DIR) await page.screenshot({ path: `${process.env.PCAS_M1_SCREENSHOT_DIR}/mock-${width === 390 ? 'mobile' : 'desktop'}.png`, fullPage: true })
      expect(m.errors).toEqual([])
    })
  }
}

test('V9 invalid files show a reason without uploading or sending', async ({ page }) => {
  const m = await mock(page)
  await page.goto('/')
  await page.getByLabel('选择附件').setInputFiles({ name: 'too-big.png', mimeType: 'image/png', buffer: Buffer.alloc(20 * 1024 * 1024 + 1) })
  await expect(page.getByRole('alert')).toContainText('超过 20 MB')
  await page.getByLabel('选择附件').setInputFiles({ name: 'unsupported.exe', mimeType: 'application/octet-stream', buffer: Buffer.from('file') })
  await expect(page.getByRole('alert')).toContainText('暂不支持')
  expect(m.uploads).toHaveLength(0)
  expect(m.requests).toHaveLength(0)
})

test('image-only, PDF and audio share the stored attachment path', async ({ page }) => {
  const m = await mock(page)
  await page.goto('/')
  for (const file of [image, { name: 'synthetic.pdf', mimeType: 'application/pdf', buffer: Buffer.from('synthetic PDF') }, { name: 'synthetic.ogg', mimeType: 'audio/ogg', buffer: Buffer.from('synthetic audio') }]) {
    await page.getByLabel('选择附件').setInputFiles(file)
    await expect(page.getByRole('button', { name: '发送', exact: true })).toBeEnabled()
    await page.getByRole('button', { name: '发送', exact: true }).click()
    await expect(page.getByRole('list', { name: '待发送的附件' })).toHaveCount(0)
  }
  expect(m.uploads).toHaveLength(3)
  expect(m.requests).toHaveLength(3)
  expect(m.requests.every((request) => request.text === '' && Array.isArray(request.attachments))).toBe(true)
})

test('a failed upload keeps the draft and uses the same upload identity on retry', async ({ page }) => {
  const m = await mock(page, true)
  await page.goto('/')
  await page.getByLabel('选择附件').setInputFiles(image)
  const input = page.getByRole('textbox', { name: '跟秘书说' })
  await input.fill('帮我记一下')
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('附件没传好')
  await expect(input).toHaveValue('帮我记一下')
  await expect(page.getByRole('list', { name: '待发送的附件' })).toContainText(image.name)
  expect(m.requests).toHaveLength(0)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(input).toHaveValue('')
  const id = (body: string) => /name="external_id"\r\n\r\n([^\r]+)/.exec(body)?.[1]
  expect(id(m.uploads[0])).toBeTruthy()
  expect(id(m.uploads[0])).toBe(id(m.uploads[1]))
  expect(m.requests).toHaveLength(1)
})
