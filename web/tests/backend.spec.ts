import { test, expect } from '@playwright/test'
import { evidence, fixture, login, reply, say, snapshot } from './support/real'
import { readFile } from 'node:fs/promises'

test('login, persisted memory, confirmation, correction, source tracing and export', async ({ page }, info) => {
  const errors: string[] = []
  page.on('pageerror', error => errors.push(error.message))
  const suffix = Date.now().toString(36)
  const original = `我喜欢河边旧书店-${suffix}`
  const corrected = `我喜欢山边旧书店-${suffix}`
  await login(page)
  await fixture(page, [reply(original, [], { remember: true }), { kind: 'extraction', match: original, content: JSON.stringify({ items: [{ kind: 'memory', text: original, nature: 'preference', subject: '用户', predicate: '喜欢', quote: original, confidence: 1, explicit: false, acquisition: 'direct', qualification: 'asserted' }] }) }])
  await say(page, original)
  await page.reload()
  await page.goto('/library')
  await page.getByText(original, { exact: true }).click()
  await expect(page.getByLabel('内容')).toHaveValue(original)
  await page.getByRole('button', { name: '没错', exact: true }).click()
  await expect(page.getByRole('button', { name: '没错', exact: true })).toHaveCount(0)
  expect((await snapshot(page)).memories.find(m => m.text === original)!.epistemic).toBe('confirmed')
  await page.getByLabel('内容').fill(corrected)
  await page.getByRole('button', { name: '存为新版本' }).click()
  await expect(page.getByText('改好了，旧版本也留着', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: /用户纠正/ }).last().click()
  await page.getByText('展开原文', { exact: true }).click()
  await expect(page.getByText('展开原文', { exact: true }).locator('..').locator('pre').filter({ hasText: corrected })).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('内容')).toHaveValue(corrected)
  await page.goto('/settings')
  const downloaded = page.waitForEvent('download')
  await page.getByRole('button', { name: '导出', exact: true }).click()
  const file = await downloaded
  expect(file.suggestedFilename()).toBe('pcas-export.json')
  const exported = JSON.parse(await readFile((await file.path())!, 'utf8'))
  expect(JSON.stringify(exported)).toContain(corrected)
  expect(errors).toEqual([])
  await evidence(page, info)
})
