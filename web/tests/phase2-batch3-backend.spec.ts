import { test, expect } from '@playwright/test'
import { readFileSync, mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import { command, fixture, login, say, snapshot, evidence } from './support/real'

const gold = JSON.parse(readFileSync(new URL('../../testdata/phase2/b3-gold.json', import.meta.url), 'utf8'))
const captureURL = process.env.PCAS_TEST_B1_CAPTURE_URL
const databaseURL = process.env.PCAS_TEST_DATABASE_URL
const owner = process.env.PCAS_OWNER_ID
if (!captureURL || !databaseURL || !owner) throw new Error('V3 requires the disposable real-backend runner and full local fake-model request capture')
if (databaseURL !== process.env.PCAS_DATABASE_URL || !['localhost', '127.0.0.1'].includes(new URL(databaseURL).hostname)) throw new Error('V3 requires the same local disposable database as the backend')
const repo = fileURLToPath(new URL('../../', import.meta.url))

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2-b3') })

// Seed the frozen schema directly through pgx; extraction is absent. The small
// temporary Go program avoids requiring an extra browser dependency or psql.
const seeder = `package main
import("context";"encoding/json";"os";"github.com/jackc/pgx/v5")
func main(){
 ctx:=context.Background(); db,err:=pgx.Connect(ctx,os.Getenv("PCAS_TEST_DATABASE_URL"));if err!=nil{panic(err)};defer db.Close(ctx)
 var f struct{Owner,Self,Place,Wang,Source,Chunk,Claim,Said,EventFrom,EventTo,Raw,Text string}
 if err=json.Unmarshal([]byte(os.Getenv("PCAS_B3_FIXTURE")),&f);err!=nil{panic(err)}
 tx,err:=db.Begin(ctx);if err!=nil{panic(err)};defer tx.Rollback(ctx)
 x:=func(q string,a ...any){if _,e:=tx.Exec(ctx,q,a...);e!=nil{panic(e)}}
 // Reuse an existing self if startup created it, preserving one self per user.
 var existing string
 err=tx.QueryRow(ctx,"SELECT ev.entity_id::text FROM entity_versions ev JOIN memory_records r ON r.owner_id=ev.owner_id AND r.id=ev.entity_id AND r.version=ev.version WHERE ev.owner_id=$1 AND ev.entity_type='self' AND r.state='active'",f.Owner).Scan(&existing)
 if err==nil{f.Self=existing}else if err!=pgx.ErrNoRows{panic(err)}
 records:=[]struct{ID,Kind string}{{f.Place,"entity"},{f.Wang,"entity"},{f.Source,"source"},{f.Chunk,"chunk"},{f.Claim,"claim"}}
 if existing==""{records=append(records,struct{ID,Kind string}{f.Self,"entity"})}
 for _,r:=range records{
 x("INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,1)",f.Owner,r.ID,r.Kind)
 x("INSERT INTO record_versions(owner_id,record_id,version,expressed_at) VALUES($1,$2,1,$3::timestamptz)",f.Owner,r.ID,f.Said)
 }
 entities:=[]struct{ID,Kind,Name string}{{f.Place,"place","成都"},{f.Wang,"person","老王"}}
 if existing==""{entities=append(entities,struct{ID,Kind,Name string}{f.Self,"self","本人"})}
 for _,e:=range entities{
 x("INSERT INTO entities(owner_id,id) VALUES($1,$2)",f.Owner,e.ID)
 x("INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,$3,$4)",f.Owner,e.ID,e.Kind,e.Name)
 x("INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)",f.Owner,e.ID,e.Name)
 }
 x("INSERT INTO sources(owner_id,id,connector,external_id) VALUES($1,$2,'manual',$2::text)",f.Owner,f.Source)
 x("INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type) VALUES($1,$2,1,'1',decode(repeat('22',32),'hex'),'当时的对话',$3,'text/plain')",f.Owner,f.Source,f.Raw)
 x("INSERT INTO chunks(owner_id,id,version,source_id,source_version,ordinal,start_rune,end_rune,body,search_text) VALUES($1,$2,1,$3,1,0,0,$4,$5,'成都 老王 计划')",f.Owner,f.Chunk,f.Source,len([]rune(f.Raw)),f.Raw)
 x("INSERT INTO claims(owner_id,id) VALUES($1,$2)",f.Owner,f.Claim)
 value,_:=json.Marshal(f.Text)
 x("INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type,event_from,event_to,event_precision) VALUES($1,$2,1,$3,'验收安排',$4::jsonb,'plan','direct','adopted','initial',$5::timestamptz,$6::timestamptz,'month')",f.Owner,f.Claim,f.Self,string(value),f.EventFrom,f.EventTo)
 x("INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'place'),($1,$2,1,$4,'person')",f.Owner,f.Claim,f.Place,f.Wang)
 x("INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,acquisition,stance) VALUES($1,gen_random_uuid(),$2,1,$3,1,'direct','supports')",f.Owner,f.Source,f.Claim)
 if err=tx.Commit(ctx);err!=nil{panic(err)}
}`

test('V3 真实结构化记忆→秘书实际请求→去年的时间轴→当时原话', async ({ page }) => {
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai' } })
  const agent = (await snapshot(page)).agents.find(a => a.default && a.available && a.enabled)
  expect(agent).toBeDefined()
  await command(page, { type: 'updateAgent', id: agent!.id, patch: { memoryKinds: ['fact', 'preference', 'intention', 'plan', 'decision'], includeInferred: false } })
  const year = Number(new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric' }).format(new Date())) - 1
  const seed = { Owner: owner, Self: crypto.randomUUID(), Place: crypto.randomUUID(), Wang: crypto.randomUUID(), Source: crypto.randomUUID(), Chunk: crypto.randomUUID(), Claim: crypto.randomUUID(), Said: `${year}-03-12T15:30:00Z`, EventFrom: `${year}-11-01T00:00:00+08:00`, EventTo: `${year}-12-01T00:00:00+08:00`, Raw: gold.browser.sourceText, Text: '独立验收：准备吃火锅、见老王' }
  const directory = mkdtempSync(join(tmpdir(), 'pcas-b3-browser-'))
  try {
    const path = join(directory, 'seed.go')
    writeFileSync(path, seeder)
    execFileSync('go', ['run', path], { cwd: repo, env: { ...process.env, PCAS_B3_FIXTURE: JSON.stringify(seed) }, stdio: ['ignore', 'pipe', 'pipe'] })
  } finally { rmSync(directory, { recursive: true, force: true }) }
  await command(page, { type: 'setMemoryVisibility', id: seed.Claim, agentIds: [agent!.id, 'manual'] })
  await fixture(page, [{ kind: 'secretary', match: gold.fixtures.query, content: JSON.stringify({ reply: gold.browser.reply, used: ['M1'], actions: [] }) }])
  await page.request.delete(`${captureURL}/b1/requests`)
  const result = await say(page, gold.fixtures.query)
  await expect(page.locator('.sec-reply').last()).toHaveText(gold.browser.reply)
  const timeline = result.turn.cards.find((card: { kind: string }) => card.kind === 'timeline')
  expect(timeline).toBeDefined()
  expect(timeline.items).toHaveLength(1)
  expect(timeline.items[0]).toMatchObject({ memoryId: seed.Claim, text: seed.Text, eventPrecision: 'month' })
  expect(new Date(timeline.items[0].at).toISOString()).toBe(new Date(seed.Said).toISOString())
  expect(timeline.items[0].mentions.map((m: { name: string }) => m.name).sort()).toEqual(['成都', '老王'])
  const captured = await (await page.request.get(`${captureURL}/b1/requests`)).json()
  const request = captured.requests.find((r: { messages?: { content: string }[] }) => r.messages?.some(m => m.content.includes('这句话：' + gold.fixtures.query)))
  expect(request, 'actual model HTTP request must have been captured').toBeDefined()
  const prompt = request.messages.map((m: { content: string }) => m.content).join('\n')
  expect(prompt).toContain(seed.Text)
  expect(prompt).toContain(`说于 ${year}-03-12`)
  expect(prompt).toContain('成都')
  expect(prompt).toContain('老王')
  const row = page.locator('.sec-timeline li').filter({ hasText: seed.Text })
  await expect(row).toBeVisible()
  await expect(row.locator('time').first()).toHaveText(new RegExp(`${year}(?:年|[-/.])0?3(?:月|[-/.])12`))
  await row.getByRole('button', { name: seed.Text, exact: true }).click()
  await expect(page.getByRole('dialog').locator('pre')).toHaveText(seed.Raw)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
})
