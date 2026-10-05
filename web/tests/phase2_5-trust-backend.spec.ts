import { test, expect } from '@playwright/test'
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import type { Memory, MemoryPage } from '../src/domain/types'
import { command, evidence, login } from './support/real'

const databaseURL = process.env.PCAS_TEST_DATABASE_URL
const owner = process.env.PCAS_OWNER_ID
if (!databaseURL || !owner) throw new Error('The trust backend test needs the disposable real-backend runner')
if (databaseURL !== process.env.PCAS_DATABASE_URL || !['localhost', '127.0.0.1'].includes(new URL(databaseURL).hostname)) throw new Error('The trust backend test needs the same local disposable database as the backend')
const repo = fileURLToPath(new URL('../../', import.meta.url))

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2_5-trust') })

// Everything here is made up. The memories go straight into the schema as the
// comparing would have left them: two merged into one, one replaced by another.
// The small temporary Go program avoids requiring an extra browser dependency or psql.
const seeder = `package main
import("context";"encoding/json";"os";"github.com/jackc/pgx/v5")
func main(){
 ctx:=context.Background(); db,err:=pgx.Connect(ctx,os.Getenv("PCAS_TEST_DATABASE_URL"));if err!=nil{panic(err)};defer db.Close(ctx)
 var f struct{Owner,Self,Said string;Claims []struct{ID,Source,Chunk,Text,Retired,RetiredBy string}}
 if err=json.Unmarshal([]byte(os.Getenv("PCAS_TRUST_FIXTURE")),&f);err!=nil{panic(err)}
 tx,err:=db.Begin(ctx);if err!=nil{panic(err)};defer tx.Rollback(ctx)
 x:=func(q string,a ...any){if _,e:=tx.Exec(ctx,q,a...);e!=nil{panic(e)}}
 record:=func(id,kind string){
 x("INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,1)",f.Owner,id,kind)
 x("INSERT INTO record_versions(owner_id,record_id,version,expressed_at) VALUES($1,$2,1,$3::timestamptz)",f.Owner,id,f.Said)
 }
 // Reuse an existing self if startup created it, preserving one self per user.
 var existing string
 err=tx.QueryRow(ctx,"SELECT ev.entity_id::text FROM entity_versions ev JOIN memory_records r ON r.owner_id=ev.owner_id AND r.id=ev.entity_id AND r.version=ev.version WHERE ev.owner_id=$1 AND ev.entity_type='self' AND r.state='active'",f.Owner).Scan(&existing)
 if err==nil{f.Self=existing}else if err!=pgx.ErrNoRows{panic(err)}else{
 record(f.Self,"entity")
 x("INSERT INTO entities(owner_id,id) VALUES($1,$2)",f.Owner,f.Self)
 x("INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,'self','本人')",f.Owner,f.Self)
 x("INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,'本人')",f.Owner,f.Self)
 }
 for _,c:=range f.Claims{
 record(c.Source,"source");record(c.Chunk,"chunk");record(c.ID,"claim")
 x("INSERT INTO sources(owner_id,id,connector,external_id) VALUES($1,$2,'manual',$3)",f.Owner,c.Source,c.Source)
 x("INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type) VALUES($1,$2,1,'1',sha256(convert_to($3,'UTF8')),'虚构的随手记',$4,'text/plain')",f.Owner,c.Source,c.Source,c.Text)
 x("INSERT INTO chunks(owner_id,id,version,source_id,source_version,ordinal,start_rune,end_rune,body,search_text) VALUES($1,$2,1,$3,1,0,0,$4,$5,$5)",f.Owner,c.Chunk,c.Source,len([]rune(c.Text)),c.Text)
 x("INSERT INTO claims(owner_id,id,organized,compared) VALUES($1,$2,1,1)",f.Owner,c.ID)
 value,_:=json.Marshal(c.Text)
 x("INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type) VALUES($1,$2,1,$3,'虚构的事',$4::jsonb,'fact','direct','candidate','initial')",f.Owner,c.ID,f.Self,string(value))
 x("INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,gen_random_uuid(),$2,1,$3,1,'{}','direct','supports')",f.Owner,c.Source,c.ID)
 }
 // Only once every memory exists can one point at another.
 for _,c:=range f.Claims{
 if c.Retired!=""{x("UPDATE claims SET retired=$3,retired_by=$4,retired_at=now() WHERE owner_id=$1 AND id=$2",f.Owner,c.ID,c.Retired,c.RetiredBy)}
 }
 if err=tx.Commit(ctx);err!=nil{panic(err)}
}`

const label = { stated: '你说的', repeated: '多次说过', tentative: '带保留', reported: '转述', inferred: '推断' }

test('可信度、已退出的记忆和合并读真实后端：列表、恢复和撤销、并入的那几条', async ({ page }) => {
  test.setTimeout(3 * 60_000)
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai' } })
  const claim = (text: string, retired = '', by = '') => ({ ID: crypto.randomUUID(), Source: crypto.randomUUID(), Chunk: crypto.randomUUID(), Text: text, Retired: retired, RetiredBy: by })
  const friday = claim('遮阳网的安装改到周五')
  const maybe = claim('可能明年把摊位搬到河堤那边')
  const wednesday = claim('遮阳网周三装', 'superseded', friday.ID)
  const merged = [claim('遮阳网安装挪到周五了', 'duplicate', friday.ID), claim('周五装遮阳网', 'duplicate', friday.ID)]
  const seed = { Owner: owner, Self: crypto.randomUUID(), Said: new Date(Date.now() - 3 * 86400_000).toISOString(), Claims: [friday, maybe, wednesday, ...merged] }
  const directory = mkdtempSync(join(tmpdir(), 'pcas-trust-browser-'))
  try {
    const path = join(directory, 'seed.go')
    writeFileSync(path, seeder)
    execFileSync('go', ['run', path], { cwd: repo, env: { ...process.env, PCAS_TRUST_FIXTURE: JSON.stringify(seed) }, stdio: ['ignore', 'pipe', 'pipe'] })
  } finally { rmSync(directory, { recursive: true, force: true }) }

  const list = async (retired = false): Promise<Memory[]> => {
    const response = await page.request.get(`/v1/workspace/memories?limit=100${retired ? '&retired=1' : ''}`)
    expect(response.ok(), await response.text()).toBeTruthy()
    return ((await response.json()) as MemoryPage).items
  }
  const mine = new Set(seed.Claims.map((c) => c.ID))
  const ids = async (retired = false) => (await list(retired)).filter((m) => mine.has(m.id)).map((m) => m.id).sort()

  // What the server sends is what the mocked tests assumed.
  const current = await list()
  expect(await ids()).toEqual([friday.ID, maybe.ID].sort())
  expect(await ids(true)).toEqual([wednesday.ID, ...merged.map((m) => m.ID)].sort())
  const kept = current.find((m) => m.id === friday.ID)!
  const hedged = current.find((m) => m.id === maybe.ID)!
  expect(kept.mergedFrom).toBe(2)
  expect(kept.trust).toBe('stated')
  expect(hedged.trust).toBe('tentative')
  const gone = await list(true)
  expect(gone.find((m) => m.id === wednesday.ID)).toMatchObject({ retired: 'superseded', retiredBy: friday.ID })
  expect(gone.find((m) => m.id === merged[0].ID)).toMatchObject({ retired: 'duplicate', retiredBy: friday.ID })

  // The two list parameters the page relies on.
  const ask = async (query: string): Promise<Memory[]> => {
    const response = await page.request.get(`/v1/workspace/memories?limit=100&${query}`)
    expect(response.ok(), await response.text()).toBeTruthy()
    return ((await response.json()) as MemoryPage).items
  }
  const hedgedOnly = await ask('trust=tentative')
  expect(hedgedOnly.every((m) => m.trust === 'tentative')).toBeTruthy()
  expect(hedgedOnly.filter((m) => mine.has(m.id)).map((m) => m.id)).toEqual([maybe.ID])
  expect((await ask('trust=stated')).filter((m) => mine.has(m.id)).map((m) => m.id)).toEqual([friday.ID])
  expect((await ask(`retired=1&retiredBy=${friday.ID}`)).map((m) => m.id).sort()).toEqual([wednesday.ID, ...merged.map((m) => m.ID)].sort())
  expect(await ask(`retired=1&retiredBy=${maybe.ID}`)).toEqual([])

  await page.goto('/library?tab=memory')
  const card = (text: string) => page.locator('.mem-entry').filter({ has: page.getByText(text, { exact: true }) })
  await expect(card(friday.Text).locator('.tag')).toHaveText([label[kept.trust!]])
  await expect(card(maybe.Text).locator('.tag')).toHaveText([label[hedged.trust!]])
  for (const c of [wednesday, ...merged]) await expect(page.getByText(c.Text, { exact: true })).toHaveCount(0)
  await expect(page.locator('main.page')).not.toContainText(/待确认|原话有据|推测/)

  // Narrowing by trust is answered by the server.
  const filter = page.getByRole('radiogroup', { name: '可信度' })
  const narrowed = page.waitForResponse((r) => new URL(r.url()).pathname === '/v1/workspace/memories' && new URL(r.url()).searchParams.get('trust') === 'tentative')
  await filter.getByRole('radio', { name: '带保留' }).click()
  const answer = (await (await narrowed).json()) as MemoryPage
  expect(answer.items.every((m) => m.trust === 'tentative')).toBeTruthy()
  await expect(card(maybe.Text)).toHaveCount(1)
  await expect(card(friday.Text)).toHaveCount(0)
  await expect(page.locator('.mem-entry')).toHaveCount(answer.total)
  await expect(page.getByText(`符合的记忆有 ${answer.total} 条`)).toBeVisible()
  await filter.getByRole('radio', { name: '都看' }).click()
  await expect(card(friday.Text)).toHaveCount(1)

  // The memories merged into one, each with where it came from; one merged by mistake comes back.
  const forMerged: string[] = []
  page.on('request', (r) => { if (new URL(r.url()).searchParams.has('retiredBy')) forMerged.push(r.url()) })
  await card(friday.Text).getByRole('button', { name: '合并了 2 条' }).click()
  const sheet = page.getByRole('dialog')
  const rows = sheet.getByRole('group', { name: '合并了 2 条' }).getByRole('listitem')
  await expect(rows).toHaveCount(2)
  // Asked for once, by the memory they were merged into.
  expect(forMerged).toHaveLength(1)
  expect(new URL(forMerged[0]).searchParams.get('retiredBy')).toBe(friday.ID)
  expect(new URL(forMerged[0]).searchParams.get('retired')).toBe('1')
  for (const m of merged) {
    const row = rows.filter({ hasText: m.Text })
    await expect(row).toHaveCount(1)
    const sources = gone.find((g) => g.id === m.ID)!.sources
    expect(sources.length).toBeGreaterThan(0)
    await expect(row.locator('.from')).toHaveCount(sources.length)
    for (const s of sources) if (s.excerpt) await expect(row).toContainText(s.excerpt)
  }
  await rows.filter({ hasText: merged[1].Text }).getByRole('button', { name: '恢复', exact: true }).click()
  await expect(sheet.getByRole('group', { name: '合并了 1 条' }).getByRole('listitem')).toHaveCount(1)
  expect(await ids()).toEqual([friday.ID, maybe.ID, merged[1].ID].sort())
  await sheet.getByRole('button', { name: '关闭' }).click()
  await expect(card(merged[1].Text)).toHaveCount(1)
  await expect(card(friday.Text).getByRole('button', { name: '合并了 1 条' })).toBeVisible()

  // The replaced one: listed apart, says what replaced it, comes back, and that can be undone.
  await page.getByRole('button', { name: '看已被替代或合并的' }).click()
  await expect(page.locator('.mem-entry')).toHaveCount((await list(true)).length)
  const old = card(wednesday.Text)
  await expect(old).toContainText(`被后来的这条替代：${friday.Text}`)
  await expect(card(merged[0].Text)).toContainText(`和这条说的是一回事，已经并进去：${friday.Text}`)
  await old.getByRole('button', { name: '恢复', exact: true }).click()
  await expect(old).toHaveCount(0)
  expect(await ids(true)).toEqual([merged[0].ID])
  await page.locator('.toast').filter({ hasText: '恢复了' }).getByRole('button', { name: '撤销' }).click()
  await expect(card(wednesday.Text)).toHaveCount(1)
  expect(await ids(true)).toEqual([wednesday.ID, merged[0].ID].sort())
  expect(await ids()).not.toContain(wednesday.ID)

  await card(wednesday.Text).getByRole('button', { name: '恢复', exact: true }).click()
  await expect(card(wednesday.Text)).toHaveCount(0)
  await page.getByRole('button', { name: '回到现在的记忆' }).click()
  await expect(card(wednesday.Text)).toHaveCount(1)
  await expect(card(wednesday.Text)).not.toContainText('替代')
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
})
