import { crc32, deflateRawSync, inflateRawSync } from 'node:zlib'
import { test, expect, type Page } from '@playwright/test'
import { closeSync, mkdtempSync, openSync, readFileSync, rmSync, writeSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { randomBytes } from 'node:crypto'
import type { State } from '../src/domain/types'

// Independent UI acceptance. Mock payloads follow batch4 section 6; no
// product import component is read to construct these expectations.
const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b4-gold.json', import.meta.url), 'utf8'))
const preview = gold.browser.preview
const at = '2025-05-14T03:10:00Z'
type Batch = {
  id: string; name: string; state: 'importing' | 'paused' | 'done' | 'failed'
  archiveId: string; archiveVersion: number
  total: number; stored: number; organized: number; leftOut: number
  organizeLater: boolean
  earliest: string; latest: string; errorCode: string; error: string; createdAt: string; updatedAt: string
}
function batch(state: Batch['state']): Batch {
  return { id: 'b4-synthetic-batch', archiveId: '11111111-1111-4111-8111-111111111111', archiveVersion: 1, name: 'chatgpt-export.zip', state, total: 500, stored: state === 'done' ? 500 : 250,
    organized: state === 'done' ? 300 : 25, organizeLater: false, leftOut: 2, earliest: preview.earliest, latest: preview.latest,
    errorCode: state === 'failed' ? 'invalid_json' : '', error: state === 'failed' ? '聊天文件没有读完，请继续导入。' : '', createdAt: at, updatedAt: at }
}
async function mock(page: Page, initial: Batch[] = [], failure?: { error: string; message: string; status: number }, importPreview = { ...preview, blocked: 0 }, completeImport = false) {
  const state: State = {
    version: 1, revision: 1, budgetUsage: 0, notices: [],
    settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'Asia/Shanghai' },
    tasks: [], ideas: [], projects: [], agents: [], memories: [], candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], activity: [], excludedMemories: {},
  }
  const items = initial.map(item => ({ ...item }))
  const requests: { path: string; method: string }[] = []
  const errors: string[] = []
  const submittedModes: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/v1/**', route => {
    const path = new URL(route.request().url()).pathname
    const method = route.request().method()
    requests.push({ path, method })
    if (path === '/v1/workspace') return route.fulfill({ json: state })
    if (path === '/v1/desk/turns') return route.fulfill({ json: { turns: [] } })
    if (path === '/v1/connectors') return route.fulfill({ json: [] })
    if (path === '/v1/connectors/imports') return route.fulfill({ json: { items } })
    if (path === '/v1/notify/config') return route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } })
    if (path === '/v1/models') return route.fulfill({ json: { chatgptEnabled: false, chatgptDirectEnabled: false } })
    if (path === '/v1/chatgpt/account') return route.fulfill({ json: { account: null } })
    if (path === '/v1/chatgpt/direct/account') return route.fulfill({ json: { accounts: [], active_client_id: '', pending: false, default_ready: false } })
    if (path === '/v1/models/openai') {
      const model = { base_url: '', model: '', input_cny_per_million: 2, output_cny_per_million: 4, default: false, key_configured: false }
      return route.fulfill({ json: { editable: true, text: model, embedding: model, decision: { key_configured: false, saved: false } } })
    }
    if (path === '/v1/connectors/archive/preview' && method === 'POST') {
      if (failure) return route.fulfill({ status: failure.status, json: { error: failure.error, message: failure.message } })
      return route.fulfill({ json: importPreview })
    }
    if (path === '/v1/connectors/archive' && method === 'POST') {
      const form = route.request().postDataBuffer()?.toString('utf8') ?? ''
      const organize = form.match(/name="organize"\r?\n\r?\n([^\r\n]+)/)?.[1] ?? 'later'
      submittedModes.push(organize)
      const total = importPreview.messages - importPreview.leftOut - importPreview.blocked
      items.unshift({ ...batch(completeImport ? 'done' : 'importing'), total, stored: completeImport ? total : importPreview.alreadyImported, organized: 0, organizeLater: organize !== 'now', leftOut: importPreview.leftOut })
      return route.fulfill({ status: 202, json: { batchId: items[0].id, imported: 0, refs: [], gaps: importPreview.gaps } })
    }
    const organizeAction = path.match(/^\/v1\/connectors\/imports\/([^/]+)\/organize$/)
    if (organizeAction && method === 'POST') {
      const item = items.find(item => item.id === organizeAction[1])
      if (!item) return route.fulfill({ status: 404, json: { error: 'not_found' } })
      item.organizeLater = false
      item.organized = 1
      return route.fulfill({ json: item })
    }
    const action = path.match(/^\/v1\/connectors\/imports\/([^/]+)\/(pause|resume)$/)
    if (action && method === 'POST') {
      const item = items.find(item => item.id === action[1])
      if (!item) return route.fulfill({ status: 404, json: { error: 'not_found' } })
      item.state = action[2] === 'pause' ? 'paused' : 'importing'
      return route.fulfill({ json: item })
    }
    return route.fulfill({ status: 500, json: { error: 'unexpected_b4_request' } })
  })
  return { requests, errors, submittedModes }
}
async function open(page: Page) {
  await page.goto('/settings')
  await expect(page.locator('input[type="file"]').last()).toBeAttached()
}
async function choose(page: Page) {
  // A synthetic official mapping file; preview numbers are controlled by the
  // mock, while production decoding is exercised by I1 and real-backend W4.
  const id = 'b4-ui-conversation'
  const conversation = [{ id, current_node: 'user', mapping: { user: { id: 'user', parent: null, children: [], message: { id: 'user', author: { role: 'user' }, create_time: 1718161800, content: { content_type: 'text', parts: ['纯合成历史资料。'] } } } } }]
  await page.locator('input[type="file"]').last().setInputFiles({ name: 'conversations.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(conversation)) })
}
async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
}
async function noInternals(page: Page) {
  // Section 13 scopes the check to importing, including its preview dialog.
  const importArea = page.locator('#settings-material .set-block').filter({ has: page.getByText('导入聊天记录', { exact: true }) })
  await expect(importArea).toBeVisible()
  let visible = await importArea.innerText()
  const dialog = page.getByRole('dialog')
  if (await dialog.count()) visible += `\n${await dialog.innerText()}`
  for (const word of gold.browser.forbiddenTerms as string[]) expect(visible).not.toContain(word)
}
test.use({ viewport: { width: 390, height: 844 }, timezoneId: 'Asia/Shanghai' })
test.afterEach(async ({ page }, info) => {
  await info.attach('b4-ui', { body: await page.screenshot({ fullPage: true, animations: 'disabled' }), contentType: 'image/png' })
})

test('W1 gaps 说明禁止重新导入的消息，显示这次实际新存数量', async ({ page }) => {
  const supplement = gold.coordinator_amendment_96c6cb1.browser
  const p = supplement.preview
  const m = await mock(page, [], undefined, p)
  await open(page)
  await choose(page)
  await expect.poll(() => m.requests.filter(r => r.path === '/v1/connectors/archive/preview' && r.method === 'POST').length).toBe(1)
  const body = page.locator('body')
  await expect(body).toContainText(p.gaps[1])
  await expect(page.getByText(/(?:本次|这次|将|可).*8.*(?:条|消息)|(?:本次|这次|将|可).*导入.*8/).first()).toBeVisible()
  expect(p.messages - p.alreadyImported - p.leftOut - p.blocked).toBe(supplement.new)
  expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(0)
  await noOverflow(page)
  await noInternals(page)
  expect(m.errors).toEqual([])
})

test('W1 文件选择先预览：数量、时间、已导过和放不下；确认前无导入', async ({ page }) => {
  const m = await mock(page)
  await open(page)
  await choose(page)
  await expect.poll(() => m.requests.filter(r => r.path === '/v1/connectors/archive/preview' && r.method === 'POST').length).toBe(1)
  for (const count of [3, 12, 6, 4, 2]) await expect(page.getByText(new RegExp(`\\b${count}\\b`)).first()).toBeVisible()
  const body = page.locator('body')
  await expect(body).toContainText(/2024[年/.-]\s*0?6[月/.-]\s*12/)
  await expect(body).toContainText(/2025[年/.-]\s*0?5[月/.-]\s*14/)
  await expect(body).toContainText(/对话/)
  await expect(body).toContainText(/消息|原话/)
  await expect(body).toContainText(/已.*导|重复/)
  await expect(body).toContainText(/放不下|未导入|超过|超出|跳过/)
  expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(0)
  await noOverflow(page)
  await noInternals(page)
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/v1/connectors/archive' && r.request().method() === 'POST')
  await page.getByRole('button', { name: /确认导入|开始导入|^确认$|^导入\s*[\d,]+\s*条$/ }).click()
  expect((await response).ok()).toBeTruthy()
  expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(1)
  expect(m.errors).toEqual([])
})

// W5 expectations were appended and committed in 631ab49 before this code.
test('W5 默认先存着；存好后开始整理，进度随接口返回增长', async ({ page }) => {
  const expected = gold.coordinator_amendment_b526328.fixtures
  const m = await mock(page, [], undefined, { ...preview, blocked: 0 }, true)
  await open(page)
  await choose(page)
  await expect(page.getByText(expected.laterLabel, { exact: false }).first()).toBeVisible()
  expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(0)
  const importing = page.waitForResponse(r => new URL(r.url()).pathname === '/v1/connectors/archive' && r.request().method() === 'POST')
  await page.getByRole('button', { name: /确认导入|开始导入|^确认$|^导入\s*[\d,]+\s*条$/ }).click()
  expect((await importing).ok()).toBeTruthy()
  expect(m.submittedModes).toEqual(['later'])
  await expect(page.locator('body')).toContainText(expected.heldStatus)
  const start = page.getByRole('button', { name: expected.startLabel, exact: true })
  await expect(start).toBeVisible()
  const organizing = page.waitForResponse(r => new URL(r.url()).pathname.endsWith('/b4-synthetic-batch/organize') && r.request().method() === 'POST')
  await start.click()
  expect((await organizing).ok()).toBeTruthy()
  expect(m.requests.filter(r => r.path.endsWith('/organize') && r.method === 'POST')).toHaveLength(1)
  await expect(page.getByText(/已整理\s*1|整理[^\n]*1\s*\/|1\s*\/\s*10/).first()).toBeVisible()
  await expect(page.locator('body')).not.toContainText(expected.heldStatus)
  await noOverflow(page)
  expect(m.errors).toEqual([])
})

test('W5 确认前可以改为现在整理，表单实际发送 now', async ({ page }) => {
  const m = await mock(page)
  await open(page)
  await choose(page)
  const nowName = /现在(?:就)?整理|立即整理|马上整理|边存边整理|导入时整理|同时整理/
  const dialog = page.getByRole('dialog')
  const option = dialog.getByRole('option', { name: nowName })
  if (await option.count()) {
    const label = (await option.first().textContent())!.trim()
    await dialog.getByRole('combobox').filter({ has: option }).selectOption({ label })
  } else {
    await dialog.getByRole('radio', { name: nowName }).click()
  }
  const importing = page.waitForResponse(r => new URL(r.url()).pathname === '/v1/connectors/archive' && r.request().method() === 'POST')
  await page.getByRole('button', { name: /确认导入|开始导入|^确认$|^导入\s*[\d,]+\s*条$/ }).click()
  expect((await importing).ok()).toBeTruthy()
  expect(m.submittedModes).toEqual(['now'])
  await noOverflow(page)
  expect(m.errors).toEqual([])
})

for (const kind of ['importing', 'paused', 'done', 'failed'] as const) {
  test(`W2 ${kind} 的存好/整理进度和操作正确，390px不溢出`, async ({ page }) => {
    const item = batch(kind)
    const m = await mock(page, [item])
    await open(page)
    await expect(page.getByText(item.name, { exact: true })).toBeVisible()
    await expect(page.getByText(new RegExp(`\\b${item.stored}\\b`)).first()).toBeVisible()
    await expect(page.getByText(new RegExp(`\\b${item.organized}\\b`)).first()).toBeVisible()
    await expect(page.locator('body')).toContainText(/整理/)
    await noOverflow(page)
    await noInternals(page)
    if (kind === 'importing') {
      const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(`/${item.id}/pause`))
      await page.getByRole('button', { name: /^暂停/ }).click()
      expect((await response).ok()).toBeTruthy()
      await expect(page.getByRole('button', { name: /继续|恢复/ })).toBeVisible()
    } else if (kind === 'paused' || kind === 'failed') {
      if (kind === 'failed') await expect(page.locator('body')).toContainText(/没有读完|继续导入/)
      const response = page.waitForResponse(r => new URL(r.url()).pathname.endsWith(`/${item.id}/resume`))
      await page.getByRole('button', { name: /继续|恢复|重试|接着导/ }).click()
      expect((await response).ok()).toBeTruthy()
      await expect(page.getByRole('button', { name: /^暂停/ })).toBeVisible()
    } else {
      await expect(page.locator('body')).toContainText(/完成|已导入|已存好/)
      await expect(page.getByRole('button', { name: /^暂停|继续|恢复/ })).toHaveCount(0)
    }
    expect(m.errors).toEqual([])
  })
}

const failures = [
  { error: 'invalid_zip', status: 400, message: '这不是有效的压缩包，请重新选择 ChatGPT 导出文件。' },
  { error: 'no_supported_records', status: 400, message: '压缩包里没有找到聊天记录，请检查导出内容。' },
  { error: 'invalid_json', status: 400, message: '聊天记录文件没有读完，请重新下载后再导入。' },
  { error: 'archive_too_large', status: 413, message: '文件太大，请拆成小一些的文件后再导入。' },
]
test('W3 四种导入失败各有不同的人话，无内部错误码和确认动作', async ({ page }) => {
  const descriptions = new Set<string>()
  for (const failure of failures) {
    await page.unrouteAll({ behavior: 'wait' })
    const m = await mock(page, [], failure)
    await open(page)
    await choose(page)
    const alert = page.getByRole('alert')
    await expect(alert).toBeVisible()
    const description = (await alert.innerText()).trim()
    expect(description).not.toBe('')
    expect(description).not.toContain(failure.error)
    expect(description).toMatch(/文件|压缩包|聊天|记录/)
    expect(description).toMatch(/重新|选择|检查|下载|拆|导出|再/)
    expect(descriptions.has(description)).toBeFalsy()
    descriptions.add(description)
    expect(m.requests.filter(r => r.path === '/v1/connectors/archive' && r.method === 'POST')).toHaveLength(0)
    await noOverflow(page)
    expect(m.errors).toEqual([])
  }
  expect(descriptions.size).toBe(4)
})

test('a large archive goes up in pieces once, carries on after a dropped piece, and is imported from what the server holds', async ({ page }) => {
  test.setTimeout(90_000)
  const m = await mock(page, [], undefined, { ...preview, blocked: 0 }, true)
  const size = 40 * 1024 * 1024
  const piece = 8 * 1024 * 1024
  const puts: { offset: number; bytes: number }[] = []
  const posted: { path: string; body: unknown }[] = []
  let received = 0
  let dropped = false
  await page.route((url) => url.pathname.startsWith('/v1/connectors/archive'), (route) => {
    const request = route.request()
    const url = new URL(request.url())
    if (url.pathname === '/v1/connectors/archive/uploads' && request.method() === 'POST') {
      posted.push({ path: url.pathname, body: request.postDataJSON() })
      return route.fulfill({ status: 201, json: { id: 'up-1', pieceBytes: piece } })
    }
    if (url.pathname === '/v1/connectors/archive/uploads/up-1' && request.method() === 'PUT') {
      const offset = Number(url.searchParams.get('offset'))
      // The third piece is lost once, as a proxy or a flaky connection would lose it.
      if (offset === 2 * piece && !dropped) { dropped = true; return route.abort('connectionreset') }
      if (offset !== received) return route.fulfill({ status: 409, json: { error: 'upload_offset', received } })
      const bytes = request.postDataBuffer()?.length ?? 0
      puts.push({ offset, bytes })
      received += bytes
      return route.fulfill({ json: { received } })
    }
    if (request.method() === 'POST' && request.headers()['content-type']?.startsWith('application/json')) {
      posted.push({ path: url.pathname, body: request.postDataJSON() })
    }
    return route.fallback()
  })
  await open(page)
  await page.locator('input[type="file"]').last().setInputFiles({ name: 'chatgpt-export.zip', mimeType: 'application/zip', buffer: Buffer.alloc(size, 1) })
  await expect(page.getByRole('button', { name: /确认导入|开始导入|^确认$|^导入\s*[\d,]+\s*条$/ })).toBeVisible({ timeout: 60_000 })
  expect(dropped).toBe(true)
  expect(puts.map((p) => p.offset)).toEqual([0, piece, 2 * piece, 3 * piece, 4 * piece])
  expect(puts.reduce((n, p) => n + p.bytes, 0)).toBe(size)
  const importing = page.waitForResponse((r) => new URL(r.url()).pathname === '/v1/connectors/archive' && r.request().method() === 'POST')
  await page.getByRole('button', { name: /确认导入|开始导入|^确认$|^导入\s*[\d,]+\s*条$/ }).click()
  expect((await importing).ok()).toBeTruthy()
  // Reading and importing used the same pieces: nothing was sent a second time.
  expect(puts).toHaveLength(5)
  expect(posted).toEqual([
    { path: '/v1/connectors/archive/uploads', body: { name: 'chatgpt-export.zip', size } },
    { path: '/v1/connectors/archive/preview', body: { upload: 'up-1' } },
    { path: '/v1/connectors/archive', body: { upload: 'up-1', organize: 'later' } },
  ])
  expect(m.errors).toEqual([])
})

/** A zip as an export tool writes it: entries one after another, then the directory, then the end record. */
function zipOf(entries: { name: string; data: Buffer; deflate: boolean }[]): Buffer {
  const crcTable = Array.from({ length: 256 }, (_, n) => { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c >>> 0 })
  const crc = (b: Buffer) => { let c = 0xffffffff; for (const x of b) c = crcTable[(c ^ x) & 0xff] ^ (c >>> 8); return (c ^ 0xffffffff) >>> 0 }
  const parts: Buffer[] = []
  const directory: Buffer[] = []
  let offset = 0
  for (const e of entries) {
    const name = Buffer.from(e.name, 'utf8')
    const packed = e.deflate ? deflateRawSync(e.data) : e.data
    const sum = crc(e.data)
    const local = Buffer.alloc(30)
    local.writeUInt32LE(0x04034b50, 0); local.writeUInt16LE(20, 4); local.writeUInt16LE(0x0800, 6); local.writeUInt16LE(e.deflate ? 8 : 0, 8)
    local.writeUInt32LE(sum, 14); local.writeUInt32LE(packed.length, 18); local.writeUInt32LE(e.data.length, 22); local.writeUInt16LE(name.length, 26)
    const central = Buffer.alloc(46)
    central.writeUInt32LE(0x02014b50, 0); central.writeUInt16LE(20, 4); central.writeUInt16LE(20, 6); central.writeUInt16LE(0x0800, 8); central.writeUInt16LE(e.deflate ? 8 : 0, 10)
    central.writeUInt32LE(sum, 16); central.writeUInt32LE(packed.length, 20); central.writeUInt32LE(e.data.length, 24); central.writeUInt16LE(name.length, 28); central.writeUInt32LE(offset, 42)
    parts.push(local, name, packed)
    directory.push(central, name)
    offset += 30 + name.length + packed.length
  }
  const dir = Buffer.concat(directory)
  const end = Buffer.alloc(22)
  end.writeUInt32LE(0x06054b50, 0); end.writeUInt16LE(entries.length, 8); end.writeUInt16LE(entries.length, 10); end.writeUInt32LE(dir.length, 12); end.writeUInt32LE(offset, 16)
  return Buffer.concat([...parts, dir, end])
}

test('from an export zip full of media only the conversations file is sent, whatever the zip weighs', async ({ page }) => {
  test.setTimeout(90_000)
  const m = await mock(page, [], undefined, { ...preview, blocked: 0 }, true)
  const conversations = Buffer.from(JSON.stringify([{ id: 'c1', title: '只传对话', current_node: 'u', mapping: { u: { id: 'u', parent: null, children: [], message: { id: 'u', author: { role: 'user' }, create_time: 1718161800, content: { content_type: 'text', parts: ['压缩包里只有这一句是对话。'.repeat(200)] } } } } }]))
  const zip = zipOf([
    { name: 'export/audio/voice-001.wav', data: Buffer.alloc(45 * 1024 * 1024, 7), deflate: false },
    { name: 'export/conversations.json', data: conversations, deflate: true },
    { name: 'export/image-002.png', data: Buffer.alloc(1024 * 1024, 9), deflate: false },
  ])
  expect(zip.length).toBeGreaterThan(45 * 1024 * 1024)
  const sent: { path: string; file: string; bytes: number; whole: boolean }[] = []
  await page.route((url) => url.pathname.startsWith('/v1/connectors/archive'), (route) => {
    const request = route.request()
    const body = request.postDataBuffer() ?? Buffer.alloc(0)
    if (request.method() === 'POST') sent.push({ path: new URL(request.url()).pathname, file: body.toString('latin1').match(/filename="([^"]+)"/)?.[1] ?? '', bytes: body.length, whole: body.includes(conversations) })
    return route.fallback()
  })
  await open(page)
  await page.locator('input[type="file"]').last().setInputFiles({ name: 'chatgpt-2026-10-03.zip', mimeType: 'application/zip', buffer: zip })
  const confirm = page.getByRole('button', { name: /确认导入|开始导入|^确认$|^导入\s*[\d,]+\s*条$/ })
  await expect(confirm).toBeVisible({ timeout: 60_000 })
  // The page still calls it by the name of the file that was picked.
  await expect(page.locator('body')).toContainText('chatgpt-2026-10-03.zip')
  const importing = page.waitForResponse((r) => new URL(r.url()).pathname === '/v1/connectors/archive' && r.request().method() === 'POST')
  await confirm.click()
  expect((await importing).ok()).toBeTruthy()
  // Both requests carried the unpacked conversations file and nothing else: no pieces, no media.
  expect(sent.map((s) => s.path)).toEqual(['/v1/connectors/archive/preview', '/v1/connectors/archive'])
  for (const s of sent) {
    expect(s.file).toBe('chatgpt-2026-10-03.conversations.json')
    expect(s.whole).toBe(true)
    expect(s.bytes).toBeLessThan(conversations.length + 4096)
  }
  expect(m.errors).toEqual([])
})

// A real ZIP directory past 1 GB, backed by a sparse media file so the test
// does not allocate or transfer a gigabyte just to exercise File.slice.
function largeExport(conversations: Buffer, name = 'export/conversations.json', extra: { name: string; data: Buffer; deflate: boolean }[] = []): { directory: string; path: string } {
  const mediaBytes = 1250 * 1024 * 1024
  const mediaName = 'export/voice.wav'
  const small = zipOf([
    { name: mediaName, data: Buffer.alloc(0), deflate: false },
    { name, data: conversations, deflate: true },
    ...extra,
  ])
  const dataAt = 30 + Buffer.byteLength(mediaName)
  const end = small.length - 22
  const central = small.readUInt32LE(end + 16)
  const zero = Buffer.alloc(1024 * 1024)
  let sum = 0
  for (let i = 0; i < 1250; i++) sum = crc32(zero, sum)
  for (const at of [14, central + 16]) small.writeUInt32LE(sum, at)
  for (const at of [18, 22, central + 20, central + 24]) small.writeUInt32LE(mediaBytes, at)
  for (let at = central + 46 + Buffer.byteLength(mediaName); at < end; ) {
    small.writeUInt32LE(small.readUInt32LE(at + 42) + mediaBytes, at + 42)
    at += 46 + small.readUInt16LE(at + 28) + small.readUInt16LE(at + 30) + small.readUInt16LE(at + 32)
  }
  small.writeUInt32LE(central + mediaBytes, end + 16)
  const directory = mkdtempSync(join(tmpdir(), 'pcas-large-export-'))
  const path = join(directory, 'gpt20261004.zip')
  const fd = openSync(path, 'wx')
  try {
    writeSync(fd, small.subarray(0, dataAt), 0, dataAt, 0)
    writeSync(fd, small.subarray(dataAt), 0, small.length - dataAt, dataAt + mediaBytes)
  } finally { closeSync(fd) }
  return { directory, path }
}

function unpackConversationZip(body: Buffer): { name: string; data: Buffer }[] {
  const start = body.indexOf(Buffer.from('504b0304', 'hex'))
  const end = body.lastIndexOf(Buffer.from('504b0506', 'hex'))
  expect(start).toBeGreaterThanOrEqual(0)
  expect(end).toBeGreaterThan(start)
  const entries: { name: string; data: Buffer }[] = []
  let at = start + body.readUInt32LE(end + 16)
  for (let i = 0; i < body.readUInt16LE(end + 10); i++) {
    expect(body.readUInt32LE(at)).toBe(0x02014b50)
    const nameLength = body.readUInt16LE(at + 28)
    const name = body.subarray(at + 46, at + 46 + nameLength).toString()
    const local = start + body.readUInt32LE(at + 42)
    expect(body.readUInt32LE(local)).toBe(0x04034b50)
    const dataAt = local + 30 + body.readUInt16LE(local + 26) + body.readUInt16LE(local + 28)
    const packed = body.subarray(dataAt, dataAt + body.readUInt32LE(at + 20))
    const data = body.readUInt16LE(at + 10) === 8 ? inflateRawSync(packed) : packed
    expect(data.length).toBe(body.readUInt32LE(at + 24))
    expect(crc32(data)).toBe(body.readUInt32LE(at + 16))
    entries.push({ name, data })
    at += 46 + nameLength + body.readUInt16LE(at + 30) + body.readUInt16LE(at + 32)
  }
  return entries
}

test('a numbered JSON selected on its own is previewed before importing', async ({ page }) => {
  const m = await mock(page, [], undefined, { ...preview, blocked: 0 }, true)
  await open(page)
  await page.locator('input[type="file"]').last().setInputFiles({
    name: 'conversations-000.json', mimeType: 'application/json',
    buffer: Buffer.from(JSON.stringify([{ id: 'numbered-json', mapping: {} }])),
  })
  const confirm = page.getByRole('button', { name: /^导入\s*[\d,]+\s*条$/ })
  await expect(confirm).toBeVisible()
  expect(m.requests.filter(r => r.method === 'POST' && r.path === '/v1/connectors/archive')).toEqual([])
  expect(m.requests.filter(r => r.method === 'POST' && r.path === '/v1/connectors/archive/preview')).toHaveLength(1)
  await confirm.click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(m.requests.filter(r => r.method === 'POST' && r.path === '/v1/connectors/archive')).toHaveLength(1)
  expect(m.errors).toEqual([])
})

for (const compression of ['native', 'missing', 'raw-unsupported'] as const) {
  test(`a 1250 MB numbered export keeps every conversation file with browser decompression ${compression}`, async ({ page }) => {
    const m = await mock(page, [], undefined, { ...preview, conversations: 2, messages: 2, fromUser: 2, alreadyImported: 0, leftOut: 0, blocked: 0 }, true)
    await page.addInitScript(mode => {
      if (mode === 'missing') Object.defineProperty(window, 'DecompressionStream', { value: undefined })
      if (mode === 'raw-unsupported') Object.defineProperty(window, 'DecompressionStream', { value: class {
        constructor() { throw new TypeError('unsupported compression format') }
      } })
    }, compression)
    const first = Buffer.from(JSON.stringify([{ id: 'numbered-old', title: '第一份', mapping: {} }]))
    const second = Buffer.from(JSON.stringify([{ id: 'numbered-new', title: '第二份', mapping: {} }]))
    const names = ['export/conversations-000.json', 'export/conversations-001.json']
    const archive = largeExport(first, names[0], [
      { name: names[1], data: second, deflate: false },
      { name: 'export/user.json', data: Buffer.from('{"private":"account metadata"}'), deflate: true },
      { name: 'export/conversations-draft.json', data: Buffer.from('not a conversation export'), deflate: true },
    ])
    const sent: { path: string; bytes: number; entries: { name: string; data: Buffer }[]; filename: string }[] = []
    await page.route(url => url.pathname.startsWith('/v1/connectors/archive'), route => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      if (path === '/v1/connectors/archive/uploads') return route.fulfill({ status: 413, json: { error: 'archive_too_large', message: '文件超过可读取的大小，请拆成几份后再导入。' } })
      const body = request.postDataBuffer() ?? Buffer.alloc(0)
      sent.push({ path, bytes: body.length, entries: unpackConversationZip(body), filename: body.toString().match(/filename="([^"]+)"/)?.[1] ?? '' })
      return route.fallback()
    })
    try {
      await open(page)
      await page.locator('input[type="file"]').last().setInputFiles(archive.path)
      const confirm = page.getByRole('button', { name: /^导入\s*2\s*条$/ })
      await expect(confirm).toBeVisible({ timeout: 10000 })
      await expect(page.getByRole('dialog')).toContainText('1250 MB')
      await confirm.click()
      await expect(page.getByRole('dialog')).toHaveCount(0)
      expect(sent.map(s => s.path)).toEqual(['/v1/connectors/archive/preview', '/v1/connectors/archive'])
      for (const s of sent) {
        expect(s.filename).toBe('gpt20261004.conversations.zip')
        expect(s.bytes).toBeLessThan(first.length + second.length + 4096)
        expect(s.entries.map(e => e.name)).toEqual(names)
        expect(s.entries[0].data.equals(first)).toBe(true)
        expect(s.entries[1].data.equals(second)).toBe(true)
      }
      expect(m.errors).toEqual([])
    } finally { rmSync(archive.directory, { recursive: true }) }
  })
}

for (const compression of ['native', 'missing', 'raw-unsupported'] as const) {
  test(`a 1250 MB export imports only conversations when browser decompression is ${compression}`, async ({ page }) => {
    const m = await mock(page, [], undefined, { ...preview, blocked: 0 }, true)
    await page.addInitScript((mode) => {
      if (mode === 'missing') Object.defineProperty(window, 'DecompressionStream', { value: undefined })
      if (mode === 'raw-unsupported') {
        const Native = DecompressionStream
        Object.defineProperty(window, 'DecompressionStream', { value: class extends Native {
          constructor(format: CompressionFormat) {
            if (format === 'deflate-raw') throw new TypeError('unsupported compression format')
            super(format)
          }
        } })
      }
    }, compression)
    // Incompressible text makes the entry span several streamed input chunks.
    const conversations = Buffer.from(JSON.stringify([{ id: 'large-c1', title: '大归档', messages: [{ role: 'user', text: '这是对话原文，媒体不上传。' + randomBytes(192 * 1024).toString('base64') }] }]))
    const archive = largeExport(conversations)
    const sent: { path: string; bytes: number; text: boolean; filename: string }[] = []
    await page.route((url) => url.pathname.startsWith('/v1/connectors/archive'), (route) => {
      const request = route.request()
      const path = new URL(request.url()).pathname
      if (path === '/v1/connectors/archive/uploads') return route.fulfill({ status: 413, json: { error: 'archive_too_large', message: '文件超过可读取的大小，请拆成几份后再导入。' } })
      const body = request.postDataBuffer() ?? Buffer.alloc(0)
      sent.push({ path, bytes: body.length, text: body.includes(conversations), filename: body.toString().match(/filename="([^"]+)"/)?.[1] ?? '' })
      return route.fallback()
    })
    try {
      await open(page)
      await page.locator('input[type="file"]').last().setInputFiles(archive.path)
      const confirm = page.getByRole('button', { name: /^导入\s*[\d,]+\s*条$/ })
      await expect(confirm).toBeVisible({ timeout: 5000 })
      await expect(page.getByRole('dialog')).toContainText('gpt20261004.zip')
      await confirm.click()
      await expect(page.getByRole('dialog')).toHaveCount(0)
      expect(sent.map((s) => s.path)).toEqual(['/v1/connectors/archive/preview', '/v1/connectors/archive'])
      for (const s of sent) {
        expect(s.filename).toBe('gpt20261004.conversations.json')
        expect(s.bytes).toBeLessThan(conversations.length + 4096)
        expect(s.text).toBe(true)
      }
      expect(m.errors).toEqual([])
    } finally { rmSync(archive.directory, { recursive: true }) }
  })
}
