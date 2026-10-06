import { test, expect } from '@playwright/test'
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import type { Deadline, Handover } from '../src/domain/status'
import { command, evidence, fixture, login } from './support/real'

const databaseURL = process.env.PCAS_TEST_DATABASE_URL
const owner = process.env.PCAS_OWNER_ID
if (!databaseURL || !owner) throw new Error('The About page backend test needs the disposable real-backend runner')
if (databaseURL !== process.env.PCAS_DATABASE_URL || !['localhost', '127.0.0.1'].includes(new URL(databaseURL).hostname)) throw new Error('The About page backend test needs the same local disposable database as the backend')
// The compatibility endpoint carries the direct library note and dates.
interface About { handover: Handover; deadlines: Deadline[] }
const repo = fileURLToPath(new URL('../../', import.meta.url))

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2_5-about') })

// Everything here is made up. The memories go straight into the schema, already
// sorted into their groups, so the only background work left is the real one
// under test: classification and the direct-memory handover note. The small temporary Go
// program avoids requiring an extra browser dependency or psql.
const seeder = `package main
import("context";"encoding/json";"os";"github.com/jackc/pgx/v5")
func main(){
 ctx:=context.Background(); db,err:=pgx.Connect(ctx,os.Getenv("PCAS_TEST_DATABASE_URL"));if err!=nil{panic(err)};defer db.Close(ctx)
 var f struct{Owner,Self,Project,ProjectName,Said string;Claims []struct{ID,Source,Chunk,Text,Category string;Project bool}}
 if err=json.Unmarshal([]byte(os.Getenv("PCAS_ABOUT_FIXTURE")),&f);err!=nil{panic(err)}
 tx,err:=db.Begin(ctx);if err!=nil{panic(err)};defer tx.Rollback(ctx)
 x:=func(q string,a ...any){if _,e:=tx.Exec(ctx,q,a...);e!=nil{panic(e)}}
 record:=func(id,kind string){
 x("INSERT INTO memory_records(owner_id,id,kind,version) VALUES($1,$2,$3,1)",f.Owner,id,kind)
 x("INSERT INTO record_versions(owner_id,record_id,version,expressed_at) VALUES($1,$2,1,$3::timestamptz)",f.Owner,id,f.Said)
 }
 entity:=func(id,kind,name string){
 record(id,"entity")
 x("INSERT INTO entities(owner_id,id) VALUES($1,$2)",f.Owner,id)
 x("INSERT INTO entity_versions(owner_id,entity_id,version,entity_type,name) VALUES($1,$2,1,$3,$4)",f.Owner,id,kind,name)
 x("INSERT INTO aliases(owner_id,entity_id,entity_version,alias) VALUES($1,$2,1,$3)",f.Owner,id,name)
 }
 // Reuse an existing self if startup created it, preserving one self per user.
 var existing string
 err=tx.QueryRow(ctx,"SELECT ev.entity_id::text FROM entity_versions ev JOIN memory_records r ON r.owner_id=ev.owner_id AND r.id=ev.entity_id AND r.version=ev.version WHERE ev.owner_id=$1 AND ev.entity_type='self' AND r.state='active'",f.Owner).Scan(&existing)
 if err==nil{f.Self=existing}else if err!=pgx.ErrNoRows{panic(err)}else{entity(f.Self,"self","本人")}
 entity(f.Project,"project",f.ProjectName)
 for _,c:=range f.Claims{
 record(c.Source,"source");record(c.Chunk,"chunk");record(c.ID,"claim")
 x("INSERT INTO sources(owner_id,id,connector,external_id) VALUES($1,$2,'manual',$3)",f.Owner,c.Source,c.Source)
 x("INSERT INTO source_versions(owner_id,source_id,version,external_version,content_hash,title,body,media_type) VALUES($1,$2,1,'1',sha256(convert_to($3,'UTF8')),'虚构的随手记',$4,'text/plain')",f.Owner,c.Source,c.Source,c.Text)
 x("INSERT INTO chunks(owner_id,id,version,source_id,source_version,ordinal,start_rune,end_rune,body,search_text) VALUES($1,$2,1,$3,1,0,0,$4,$5,$5)",f.Owner,c.Chunk,c.Source,len([]rune(c.Text)),c.Text)
 x("INSERT INTO claims(owner_id,id,organized,compared) VALUES($1,$2,0,0)",f.Owner,c.ID)
 value,_:=json.Marshal(c.Text)
 x("INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type,category,durable) VALUES($1,$2,1,$3,'虚构的事',$4::jsonb,'fact','direct','adopted','initial',$5,true)",f.Owner,c.ID,f.Self,string(value),c.Category)
 if c.Project{x("INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'project')",f.Owner,c.ID,f.Project)}
 x("INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,gen_random_uuid(),$2,1,$3,1,'{}','direct','supports')",f.Owner,c.Source,c.ID)
 }
 if err=tx.Commit(ctx);err!=nil{panic(err)}
}`

const titles = ['他是谁和现在的处境', '怎么跟他配合', '现在手上的事', '时间和节奏', '资源和限制', '口味和标准', '重要的人', '他的叫法', '他看重什么']
const projectName = '阳台菜园改造'
const projectTexts = ['菜园的滴灌定时器装好了，早晚各十分钟', '阳台朝西，夏天下午晒得厉害', '遮阳网还没下单，要先量尺寸', '番茄苗要在四月前移到大盆里']
const ruleTexts = ['回答先给结论，再给理由', '要花钱的事先问我', '发出去的东西先给我看']

test('资料库读真实后端：「眼下」显示后台写出的交接说明；原来卡片的链接落到同一批记忆上', async ({ page }) => {
  test.setTimeout(8 * 60_000)
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai' } })
  const handover = { sections: titles.map((title, i) => ({ title, body: i === 1 ? '先给结论，再给理由；要花钱的事先问。' : '（暂无依据）', refs: i === 1 ? [1] : [] })) }
  const claim = (text: string, category: string, project: boolean) => ({ ID: crypto.randomUUID(), Source: crypto.randomUUID(), Chunk: crypto.randomUUID(), Text: text, Category: category, Project: project })
  const seed = {
    Owner: owner, Self: crypto.randomUUID(), Project: crypto.randomUUID(), ProjectName: projectName, Said: new Date(Date.now() - 3 * 86400_000).toISOString(),
    Claims: [...projectTexts.map((t) => claim(t, 'progress', true)), ...ruleTexts.map((t) => claim(t, 'rule', false))],
  }
  // The numbered fixture follows the scheduler's stable UUID order for this
  // single transaction; classifications preserve each planted group and rule.
  const sorted = [...seed.Claims].sort((a, b) => a.ID.localeCompare(b.ID))
  await fixture(page, [
    { kind: 'assistant', match: '"inputHash"', content: JSON.stringify(handover) },
    { kind: 'assistant', match: '"timezone"', content: JSON.stringify({ items: sorted.map((c, i) => ({
      n: i + 1, category: c.Category, durable: true, project: c.Project ? projectName : '',
      ...(c.Category === 'rule' ? { unrestricted: true, scope: '' } : {}),
      deadlines: c.Text === projectTexts[0] ? [{ kind: 'recurring', at: null, recurrence: '每天早晚各十分钟', title: '菜园滴灌', timeNote: '' }] : [],
    })), new: [] }) },
  ])
  const directory = mkdtempSync(join(tmpdir(), 'pcas-about-browser-'))
  try {
    const path = join(directory, 'seed.go')
    writeFileSync(path, seeder)
    execFileSync('go', ['run', path], { cwd: repo, env: { ...process.env, PCAS_ABOUT_FIXTURE: JSON.stringify(seed) }, stdio: ['ignore', 'pipe', 'pipe'] })
  } finally { rmSync(directory, { recursive: true, force: true }) }

  const read = async (): Promise<About> => {
    const response = await page.request.get('/v1/workspace/about')
    expect(response.ok(), await response.text()).toBeTruthy()
    return response.json()
  }
  const projectKey = `entity:${seed.Project}`
  await expect.poll(async () => (await read()).handover.body.includes('先给结论'), { timeout: 3 * 60_000, intervals: [1000] }).toBe(true)
  const index = await read()
  expect(index.deadlines).toHaveLength(1)
  expect(index.deadlines[0]).toMatchObject({ kind: 'recurring', title: '菜园滴灌' })
  const groups = await page.request.get('/v1/workspace/memory-groups')
  expect(groups.ok()).toBeTruthy()
  const project = { count: projectTexts.length }
  const rules = { count: ruleTexts.length }

  await page.goto('/library')
  const now = page.getByRole('region', { name: '眼下', exact: true })
  const note = now.getByRole('region', { name: '交接说明', exact: true })
  await expect(note).toContainText('写于')
  await note.getByRole('button', { name: /看全文/ }).click()
  await expect(note.getByRole('heading', { level: 4 })).toHaveText(titles)
  await expect(note).toContainText('先给结论，再给理由；要花钱的事先问。')
  await expect(note).not.toContainText('#')
  const dates = now.getByRole('region', { name: '期限和固定安排', exact: true })
  if (index.deadlines.length) await expect(dates.getByRole('listitem')).toHaveCount(index.deadlines.length)
  else await expect(dates).toContainText('没有记下期限或固定安排。')
  await expect(page.getByRole('link', { name: '关于你' })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()

  // A link to the project's card: the library narrowed to that project, holding what the card held.
  await page.goto(`/about?card=${encodeURIComponent(projectKey)}`)
  await expect(page).toHaveURL(new RegExp(`/library\\?group=${seed.Project}$`))
  await expect(page.locator('.mem-summary')).toContainText(`「${projectName}」下面的记忆有 ${project.count} 条`)
  for (const text of projectTexts) await expect(page.getByRole('button', { name: text, exact: true })).toBeVisible()
  await expect(page.getByRole('group', { name: '项目', exact: true }).getByRole('button', { name: new RegExp(projectName) })).toHaveAttribute('aria-pressed', 'true')

  // A link to what is asked of the assistant: that kind of memory.
  await page.goto('/about?card=self%3Arule')
  await expect(page).toHaveURL(/\/library\?category=rule$/)
  await expect(page.locator('.mem-summary')).toContainText(`「对助手的要求」这一类的记忆有 ${rules.count} 条`)
  for (const text of ruleTexts) await expect(page.getByRole('button', { name: text, exact: true })).toBeVisible()
  for (const text of projectTexts) await expect(page.getByRole('button', { name: text, exact: true })).toHaveCount(0)

  // A memory opens in the usual sheet and can be corrected there.
  const target = ruleTexts[0]
  await page.getByRole('button', { name: target, exact: true }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.getByRole('textbox', { name: '内容' })).toHaveValue(target)
  await sheet.getByRole('textbox', { name: '内容' }).fill(`${target}（改过）`)
  await sheet.getByRole('button', { name: '存为新版本' }).click()
  await expect(sheet.getByRole('button', { name: '存为新版本' })).toBeDisabled()
  await sheet.getByRole('button', { name: '关闭' }).click()
  await expect(page.getByRole('button', { name: `${target}（改过）`, exact: true })).toBeVisible()
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
})
