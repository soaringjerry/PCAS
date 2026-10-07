import { test, expect } from '@playwright/test'
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import type { Deadline, Handover } from '../src/domain/status'
import type { AssistantRequirement, Memory, MemoryGroupEntry } from '../src/domain/types'
import { command, evidence, fixture, login, snapshot } from './support/real'

const databaseURL = process.env.PCAS_TEST_DATABASE_URL
const owner = process.env.PCAS_OWNER_ID
if (!databaseURL || !owner) throw new Error('The About page backend test needs the disposable real-backend runner')
if (databaseURL !== process.env.PCAS_DATABASE_URL || !['localhost', '127.0.0.1'].includes(new URL(databaseURL).hostname)) throw new Error('The About page backend test needs the same local disposable database as the backend')
const repo = fileURLToPath(new URL('../../', import.meta.url))

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 390, height: 844 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase2_6-library') })

// Everything here is made up. The memories go straight into the schema, already
// sorted into their groups, with the dates and the scope of each requirement the
// sorting would have drawn from them. The background work left is the real one
// under test: writing the handover note from them. The small temporary Go
// program avoids requiring an extra browser dependency or psql.
const seeder = `package main
import("context";"encoding/json";"os";"github.com/jackc/pgx/v5")
func main(){
 ctx:=context.Background(); db,err:=pgx.Connect(ctx,os.Getenv("PCAS_TEST_DATABASE_URL"));if err!=nil{panic(err)};defer db.Close(ctx)
 var f struct{Owner,Self,Project,ProjectName,Said string;Version int;Claims []struct{ID,Source,Chunk,Text,Category string;Project bool};Deadlines []struct{Claim,Kind,Recurrence,Title,Note,Original string;At *string};Requirements []struct{Claim,Scope string;Unrestricted bool}}
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
 x("INSERT INTO claims(owner_id,id,organized,compared) VALUES($1,$2,$3,1)",f.Owner,c.ID,f.Version)
 value,_:=json.Marshal(c.Text)
 x("INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type,category,durable) VALUES($1,$2,1,$3,'虚构的事',$4::jsonb,'fact','direct','adopted','initial',$5,true)",f.Owner,c.ID,f.Self,string(value),c.Category)
 if c.Project{x("INSERT INTO claim_mentions(owner_id,claim_id,claim_version,entity_id,role) VALUES($1,$2,1,$3,'project')",f.Owner,c.ID,f.Project)}
 x("INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,gen_random_uuid(),$2,1,$3,1,'{}','direct','supports')",f.Owner,c.Source,c.ID)
 }
 for _,d:=range f.Deadlines{x("INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title,time_note,original_text) VALUES($1,gen_random_uuid(),$2,1,$3,$4::timestamptz,$5,$6,$7,$8)",f.Owner,d.Claim,d.Kind,d.At,d.Recurrence,d.Title,d.Note,d.Original)}
 for _,r:=range f.Requirements{x("INSERT INTO assistant_requirements(owner_id,claim_id,claim_version,unrestricted,scope) VALUES($1,$2,1,$3,$4)",f.Owner,r.Claim,r.Unrestricted,r.Scope)}
 if err=tx.Commit(ctx);err!=nil{panic(err)}
}`

const titles = ['他是谁和现在的处境', '怎么跟他配合', '现在手上的事', '时间和节奏', '资源和限制', '口味和标准', '重要的人', '他的叫法', '他看重什么']
const projectName = '阳台菜园改造'
const projectTexts = ['菜园的滴灌定时器装好了，早晚各十分钟', '阳台朝西，夏天下午晒得厉害', '遮阳网还没下单，要先量尺寸', '番茄苗要在四月前移到大盆里']
const ruleTexts = ['回答先给结论，再给理由', '要花钱的事先问我', '发出去的东西先给我看']

test('资料库读真实后端：「眼下」和分组筛选读的是 2.6 的五个读接口', async ({ page }) => {
  test.setTimeout(8 * 60_000)
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai' } })
  const organize = (await snapshot(page)).organize
  // The model is the runner's fake; this is its answer when asked for the handover note.
  const handover = { sections: titles.map((title, i) => ({ title, body: i === 1 ? '先给结论，再给理由；要花钱的事先问。' : i === 3 ? '遮阳网要在期限前装完；每周日喂酸面团。' : '（暂无依据）', refs: i === 1 || i === 3 ? [1] : [] })) }
  await fixture(page, [{ kind: 'assistant', match: '"inputHash"', content: JSON.stringify(handover) }])
  const claim = (text: string, category: string, project: boolean) => ({ ID: crypto.randomUUID(), Source: crypto.randomUUID(), Chunk: crypto.randomUUID(), Text: text, Category: category, Project: project })
  const projects = projectTexts.map((t) => claim(t, 'progress', true))
  const rules = ruleTexts.map((t) => claim(t, 'rule', false))
  const day = 86400_000
  const soon = new Date(Date.now() + 3 * day).toISOString()
  const gone = new Date(Date.now() - 2 * day).toISOString()
  const seed = {
    Owner: owner, Self: crypto.randomUUID(), Project: crypto.randomUUID(), ProjectName: projectName, Said: new Date(Date.now() - 3 * day).toISOString(),
    // Already sorted under the rules in force, so the sorting stage has nothing to ask the model.
    Version: organize?.version ?? 1,
    Claims: [...projects, ...rules],
    Deadlines: [
      { Claim: projects[2].ID, Kind: 'deadline', At: soon, Recurrence: '', Title: '给遮阳网下单', Note: '', Original: '' },
      { Claim: projects[3].ID, Kind: 'deadline', At: gone, Recurrence: '', Title: '番茄苗移盆', Note: '', Original: '' },
      { Claim: projects[0].ID, Kind: 'recurring', At: null, Recurrence: '每天早晚', Title: '滴灌', Note: '', Original: '' },
      { Claim: projects[1].ID, Kind: 'unclear', At: null, Recurrence: '', Title: '装遮阳', Note: '只说了夏天前', Original: '夏天前把遮阳装上' },
    ],
    Requirements: [
      { Claim: rules[0].ID, Unrestricted: true, Scope: '' },
      { Claim: rules[2].ID, Unrestricted: false, Scope: '起草邮件' },
    ],
  }
  const directory = mkdtempSync(join(tmpdir(), 'pcas-library-browser-'))
  try {
    const path = join(directory, 'seed.go')
    writeFileSync(path, seeder)
    execFileSync('go', ['run', path], { cwd: repo, env: { ...process.env, PCAS_ABOUT_FIXTURE: JSON.stringify(seed) }, stdio: ['ignore', 'pipe', 'pipe'] })
  } finally { rmSync(directory, { recursive: true, force: true }) }

  const read = async <T>(path: string): Promise<T> => {
    const response = await page.request.get(`/v1/workspace/${path}`)
    expect(response.ok(), `${path}: ${await response.text()}`).toBeTruthy()
    return response.json()
  }
  // The worker writes the note on its own schedule, from the memories, the requirements and the dates.
  await expect.poll(async () => (await read<Handover>('handover')).body.includes('先给结论'), { timeout: 6 * 60_000, intervals: [2000] }).toBe(true)

  // What the server sends is what the mocked tests assumed.
  const note = await read<Handover>('handover')
  expect(note.stale).toBe(false)
  expect(Number.isNaN(new Date(note.builtAt).getTime())).toBe(false)
  const dates = (await read<{ items: Deadline[] }>('deadlines')).items
  expect(dates.map((d) => [d.title, d.dateStatus]).sort()).toEqual([['番茄苗移盆', 'expired_unknown'], ['给遮阳网下单', 'upcoming'], ['滴灌', 'recurring'], ['装遮阳', 'unclear']].sort())
  expect(dates.find((d) => d.title === '装遮阳')).toMatchObject({ at: null, originalText: '夏天前把遮阳装上', timeNote: '只说了夏天前', memoryId: projects[1].ID })
  expect((await read<{ items: Deadline[] }>('deadlines?expired=true')).items.map((d) => d.title)).toEqual(['番茄苗移盆'])
  const asked = (await read<{ items: AssistantRequirement[] }>('assistant-requirements')).items
  expect(asked.map((r) => r.text).sort()).toEqual([...ruleTexts].sort())
  expect(asked.find((r) => r.memoryId === rules[0].ID)).toMatchObject({ unrestricted: true })
  expect(asked.find((r) => r.memoryId === rules[2].ID)).toMatchObject({ unrestricted: false, scope: '起草邮件' })
  const groups = (await read<{ items: MemoryGroupEntry[] }>('memory-groups')).items
  const project = groups.find((g) => g.kind === 'project' && g.name === projectName)!
  const rule = groups.find((g) => g.kind === 'self' && g.name === '对助手的要求')!
  expect(project).toMatchObject({ count: 4 })
  expect(rule).toMatchObject({ count: 3 })
  const first = await read<{ items: Memory[]; next: string; total: number }>(`memory-groups/${encodeURIComponent(project.key)}/memories?limit=3`)
  expect(first.total).toBe(4)
  expect(first.items).toHaveLength(3)
  const rest = await read<{ items: Memory[]; next: string }>(`memory-groups/${encodeURIComponent(project.key)}/memories?limit=3&cursor=${encodeURIComponent(first.next)}`)
  expect([...first.items, ...rest.items].map((m) => m.text).sort()).toEqual([...projectTexts].sort())
  expect(rest.next).toBe('')

  await page.goto('/library')
  const now = page.getByRole('region', { name: '眼下', exact: true })
  const shown = now.getByRole('region', { name: '交接说明', exact: true })
  await expect(shown).toContainText('写于')
  await expect(shown).not.toContainText('正在更新')
  await shown.getByRole('button', { name: /看全文/ }).click()
  await expect(shown.getByRole('heading', { level: 4 })).toHaveText(titles)
  await expect(shown).toContainText('先给结论，再给理由；要花钱的事先问。')
  await expect(shown).not.toContainText('#')
  const table = now.getByRole('region', { name: '期限和固定安排', exact: true })
  await expect(table.getByRole('group', { name: '还没到的' }).getByRole('listitem')).toContainText(['给遮阳网下单'])
  await expect(table.getByRole('group', { name: '已过期，不知是否完成' }).getByRole('listitem')).toContainText(['番茄苗移盆'])
  await expect(table.getByRole('group', { name: '固定安排' }).getByRole('listitem')).toContainText(['每天早晚'])
  const vague = table.getByRole('group', { name: '日期没说清的' }).getByRole('listitem')
  await expect(vague).toContainText(['装遮阳'])
  await expect(vague).toContainText(['原话：夏天前把遮阳装上'])
  await expect(page.getByRole('link', { name: '关于你' })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy()

  // The directory's rows; a project narrows the list to what is under it.
  const row = (name: string) => page.getByRole('group', { name, exact: true })
  await expect(row('你本人').getByRole('button', { name: /对助手的要求/ })).toContainText('3')
  await row('项目').getByRole('button', { name: new RegExp(projectName) }).click()
  await expect(page.locator('.mem-summary')).toContainText(`「${projectName}」名下的记忆有 4 条`)
  for (const text of projectTexts) await expect(page.getByRole('button', { name: text, exact: true })).toBeVisible()
  // Narrowed further, over the whole group.
  await page.getByRole('textbox', { name: '搜索记忆' }).fill('番茄')
  await expect(page.locator('.mem-summary')).toContainText('符合其余条件的记忆有 1 条')
  await expect(page.getByRole('button', { name: projectTexts[3], exact: true })).toBeVisible()

  // A link to the project's former card lands on the same group.
  await page.goto(`/about?card=${encodeURIComponent(`entity:${seed.Project}`)}`)
  await expect(page.locator('.mem-summary')).toContainText(`「${projectName}」名下的记忆有 4 条`)
  expect(new URL(page.url()).pathname).toBe('/library')

  // A link to what is asked of the assistant: each one with when it holds.
  await page.goto('/about?card=self%3Arule')
  await expect(page.locator('.mem-summary')).toContainText('「对助手的要求」名下的记忆有 3 条')
  const entry = (text: string) => page.locator('.mem-entry').filter({ hasText: text })
  await expect(entry(ruleTexts[0])).toContainText('不限范围，每一轮都带')
  await expect(entry(ruleTexts[2])).toContainText('范围：起草邮件')
  for (const text of projectTexts) await expect(page.getByRole('button', { name: text, exact: true })).toHaveCount(0)

  // A memory opens in the usual sheet and can be corrected there.
  const target = ruleTexts[1]
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
