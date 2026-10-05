import { test, expect } from '@playwright/test'
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import type { About } from '../src/domain/status'
import { command, evidence, fixture, login } from './support/real'

const databaseURL = process.env.PCAS_TEST_DATABASE_URL
const owner = process.env.PCAS_OWNER_ID
if (!databaseURL || !owner) throw new Error('The About page backend test needs the disposable real-backend runner')
if (databaseURL !== process.env.PCAS_DATABASE_URL || !['localhost', '127.0.0.1'].includes(new URL(databaseURL).hostname)) throw new Error('The About page backend test needs the same local disposable database as the backend')
const repo = fileURLToPath(new URL('../../', import.meta.url))

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2_5-about') })

// Everything here is made up. The memories go straight into the schema, already
// sorted into their groups, so the only background work left is the real one
// under test: building the cards and the handover note. The small temporary Go
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
 x("INSERT INTO claims(owner_id,id,organized,compared) VALUES($1,$2,1,1)",f.Owner,c.ID)
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

test('「关于你」读真实后端：后台建出的卡片和交接说明显示出来，改一条记忆后它从卡里退出', async ({ page }) => {
  test.setTimeout(8 * 60_000)
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai' } })
  // The model is the runner's fake; these are its answers to the card and handover requests.
  const handover = { sections: titles.map((title, i) => ({ title, body: i === 1 ? '先给结论，再给理由；要花钱的事先问。' : i === 2 ? '阳台菜园改造，还差遮阳网。' : '（暂无依据）', refs: i === 1 || i === 2 ? [1] : [] })) }
  await fixture(page, [
    // First, because the note's request quotes the cards and so contains their keys too.
    { kind: 'assistant', match: '"cards"', content: JSON.stringify(handover) },
    { kind: 'assistant', match: '"key":"self:rule"', content: JSON.stringify({ fields: { preference: [1, 2, 3] }, rules: [{ n: 1, appliesTo: '起草邮件' }, { n: 2, appliesTo: '起草邮件' }, { n: 3, appliesTo: '起草邮件' }], deadlines: [] }) },
    { kind: 'assistant', match: '"key":"entity:', content: JSON.stringify({ fields: { status: [1, 2], next: [3], blocker: [4] }, rules: [], deadlines: [] }) },
  ])
  const claim = (text: string, category: string, project: boolean) => ({ ID: crypto.randomUUID(), Source: crypto.randomUUID(), Chunk: crypto.randomUUID(), Text: text, Category: category, Project: project })
  const seed = {
    Owner: owner, Self: crypto.randomUUID(), Project: crypto.randomUUID(), ProjectName: projectName, Said: new Date(Date.now() - 3 * 86400_000).toISOString(),
    Claims: [...projectTexts.map((t) => claim(t, 'progress', true)), ...ruleTexts.map((t) => claim(t, 'rule', false))],
  }
  const directory = mkdtempSync(join(tmpdir(), 'pcas-about-browser-'))
  try {
    const path = join(directory, 'seed.go')
    writeFileSync(path, seeder)
    execFileSync('go', ['run', path], { cwd: repo, env: { ...process.env, PCAS_ABOUT_FIXTURE: JSON.stringify(seed) }, stdio: ['ignore', 'pipe', 'pipe'] })
  } finally { rmSync(directory, { recursive: true, force: true }) }

  const read = async (key = ''): Promise<About> => {
    const response = await page.request.get(`/v1/workspace/about${key ? `?key=${encodeURIComponent(key)}` : ''}`)
    expect(response.ok(), await response.text()).toBeTruthy()
    return response.json()
  }
  // The worker builds the cards on its own schedule, then writes the note once they are all there.
  const projectKey = `entity:${seed.Project}`
  await expect.poll(async () => {
    const about = await read()
    return [about.cards.some((c) => c.key === projectKey), about.cards.some((c) => c.key === 'self:rule'), about.handover.body.includes('先给结论'), about.building.done === about.building.total].join()
  }, { timeout: 6 * 60_000, intervals: [2000] }).toBe('true,true,true,true')

  const index = await read()
  const project = (await read(projectKey)).cards.find((c) => c.key === projectKey)!
  const rules = (await read('self:rule')).cards.find((c) => c.key === 'self:rule')!
  // What the server sends is what the mocked tests assumed.
  expect(index.cards.find((c) => c.key === projectKey)).toMatchObject({ kind: 'project', name: projectName, count: 4, fields: [] })
  expect(index.cards.find((c) => c.key === 'self:rule')).toMatchObject({ kind: 'self', name: '对助手的要求', count: 3 })
  expect(project.fields.flatMap((f) => f.items.map((m) => m.text)).sort()).toEqual([...projectTexts].sort())
  expect(rules.fields.flatMap((f) => f.items.map((m) => m.appliesTo))).toEqual(['起草邮件', '起草邮件', '起草邮件'])

  await page.goto('/about')
  const region = (name: string) => page.getByRole('region', { name, exact: true })
  await expect(region('交接说明').getByRole('heading', { level: 3 })).toHaveText(titles)
  await expect(region('交接说明')).toContainText('先给结论，再给理由；要花钱的事先问。')
  await expect(region('交接说明')).not.toContainText('#')
  await expect(region('关于你').getByRole('button', { name: /对助手的要求/ })).toContainText('3 条')
  await expect(page.getByRole('status', { name: '整理进度' })).toHaveCount(0)
  await expect(region('期限和固定安排')).toHaveCount(index.deadlines.length ? 1 : 0)

  // The rules card: each memory with the kind of task it is for.
  await region('关于你').getByRole('button', { name: /对助手的要求/ }).click()
  const asked = region('关于你').getByRole('group', { name: '偏好' }).getByRole('listitem')
  await expect(asked).toHaveCount(3)
  for (const text of ruleTexts) await expect(asked.filter({ hasText: text })).toContainText('适用于：起草邮件')

  // The project card: the same memories under the same headings as the server's answer.
  const bar = region('项目').getByRole('button', { name: new RegExp(projectName) })
  await expect(bar).toContainText('4 条')
  await bar.click()
  const label = { status: '现状', deadline: '期限', decided: '已经定的', blocker: '卡点', next: '下一步', preference: '偏好', people: '相关的人' }
  for (const field of project.fields.filter((f) => f.items.length)) {
    await expect(region('项目').getByRole('group', { name: label[field.field] }).getByRole('listitem')).toHaveText(field.items.map((m) => m.text))
  }

  // A memory opens in the usual sheet; correcting it there takes the old wording off the card (R3-5).
  const target = project.fields.find((f) => f.items.length)!.items[0]
  await region('项目').getByRole('button', { name: target.text, exact: true }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.getByRole('textbox', { name: '内容' })).toHaveValue(target.text)
  await sheet.getByRole('textbox', { name: '内容' }).fill(`${target.text}（改过）`)
  await sheet.getByRole('button', { name: '存为新版本' }).click()
  await expect(sheet.getByRole('button', { name: '存为新版本' })).toBeDisabled()
  await sheet.getByRole('button', { name: '关闭' }).click()
  await expect(region('项目').getByRole('button', { name: target.text, exact: true })).toHaveCount(0)
  await expect(bar).toContainText('3 条')
  const after = (await read(projectKey)).cards.find((c) => c.key === projectKey)!
  expect(after.fields.flatMap((f) => f.items.map((m) => m.id))).not.toContain(target.id)
  // A card that is behind carries no mark, and none of the inner words show.
  await expect(page.locator('main.page')).not.toContainText(/规则版本|过期|重建|实体/)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()
})
