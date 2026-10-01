import { expect, type Page, type TestInfo } from '@playwright/test'
import type { State } from '../../src/domain/types'

export const fixtureURL = process.env.PCAS_TEST_FIXTURE_URL
if (!fixtureURL) throw new Error('Start web/tests/support/real-backend.sh; PCAS_TEST_FIXTURE_URL is required')
export async function login(page: Page, path = '/') {
  await page.goto(path)
  await page.getByLabel('访问令牌').fill(process.env.PCAS_TEST_API_TOKEN ?? 'pcas-browser-check-secret-123456789012')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByLabel('访问令牌')).toHaveCount(0)
}
export async function snapshot(page: Page): Promise<State> {
  const response = await page.request.get('/v1/workspace')
  expect(response.ok()).toBeTruthy()
  return response.json()
}
export async function command(page: Page, action: Record<string, unknown>): Promise<State> {
  const state = await snapshot(page)
  const response = await page.request.post('/v1/workspace/commands', { data: { ...action, requestId: crypto.randomUUID(), expectedRevision: state.revision } })
  expect(response.ok(), await response.text()).toBeTruthy()
  return response.json()
}
export type Rule = { kind: 'secretary' | 'assistant' | 'extraction'; match: string; content?: string; delay?: number; status?: number; once?: boolean }
export async function fixture(page: Page, rules: Rule[] = [], updates: Record<string, unknown>[] = []) {
  const response = await page.request.post(`${fixtureURL}/control`, { data: { rules, updates } })
  expect(response.ok()).toBeTruthy()
}
export async function events(page: Page): Promise<Record<string, unknown>[]> {
  const response = await page.request.get(`${fixtureURL}/control`)
  return (await response.json()).events ?? []
}
export const input = (page: Page) => page.getByRole('textbox', { name: '跟秘书说' })
export async function say(page: Page, text: string) {
  const response = page.waitForResponse(r => r.url().endsWith('/v1/desk/turn') && r.request().postDataJSON()?.text === text)
  await input(page).fill(text)
  await input(page).press('Enter')
  const result = await response
  expect(result.ok(), await result.text()).toBeTruthy()
  return result.json()
}
export function reply(match: string, actions: unknown[], extra = {}): Rule {
  return { kind: 'secretary', match, content: JSON.stringify({ reply: '安排好了。', actions, ...extra }) }
}
// UTC date arithmetic describes the calendar in the configured Shanghai zone.
export function localTime(days: number, hour: number, minute = 0) {
  const now = new Date(Date.now() + 8 * 3600_000)
  now.setUTCDate(now.getUTCDate() + days)
  return `${now.toISOString().slice(0, 10)}T${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
}
export function nextWeekday(day: number, hour: number) {
  const today = new Date(Date.now() + 8 * 3600_000).getUTCDay()
  return localTime((day - today + 7) % 7 || 7, hour)
}
// A bare weekday such as "周五" said on that very day still means today while
// the time is ahead, which is how the real secretary reads it; nextWeekday is
// for "下周一", which is never today. One hour of margin keeps a "-30m"
// reminder in the future for the length of a test.
export function upcomingWeekday(day: number, hour: number) {
  const now = new Date(Date.now() + 8 * 3600_000)
  const days = (day - now.getUTCDay() + 7) % 7
  return localTime(days === 0 && now.getUTCHours() >= hour - 1 ? 7 : days, hour)
}
// Calendar days from today to an instant, in the configured Shanghai zone.
export function daysFromToday(iso: string) {
  const day = (ms: number) => Math.floor((ms + 8 * 3600_000) / 86_400_000)
  return day(new Date(iso).getTime()) - day(Date.now())
}
export const utc = (local: string) => new Date(`${local}+08:00`).toISOString().replace('.000Z', 'Z')
export async function evidence(page: Page, info: TestInfo, name = 'browser') {
  const path = info.outputPath(`${name}.png`)
  await page.screenshot({ path, fullPage: true, animations: 'disabled' })
  await info.attach(name, { path, contentType: 'image/png' })
}
