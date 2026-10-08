import { test, expect } from '@playwright/test'
import { mkdtempSync, writeFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'
import type { Deadline } from '../src/domain/status'
import type { InProgress, Schedule } from '../src/domain/schedule'
import { command, evidence, login, snapshot } from './support/real'

const databaseURL = process.env.PCAS_TEST_DATABASE_URL
const owner = process.env.PCAS_OWNER_ID
if (!databaseURL || !owner) throw new Error('The hall backend test needs the disposable real-backend runner')
if (databaseURL !== process.env.PCAS_DATABASE_URL || !['localhost', '127.0.0.1'].includes(new URL(databaseURL).hostname)) throw new Error('The hall backend test needs the same local disposable database as the backend')
const repo = fileURLToPath(new URL('../../', import.meta.url))

test.use({ timezoneId: 'Asia/Shanghai', viewport: { width: 1440, height: 1000 } })
test.afterEach(async ({ page }, info) => { await evidence(page, info, 'phase3_5-hall') })

// Everything here is made up. The memories and their dates go straight into the
// schema, as the sorting would have left them; what is under test is the hall
// reading the real schedule and 在推进, and 做完了 landing on the memory.
const seeder = `package main
import("context";"encoding/json";"os";"github.com/jackc/pgx/v5")
func main(){
 ctx:=context.Background(); db,err:=pgx.Connect(ctx,os.Getenv("PCAS_TEST_DATABASE_URL"));if err!=nil{panic(err)};defer db.Close(ctx)
 var f struct{Owner,Self,Said string;Version int;Claims []struct{ID,Source,Chunk,Text string};Deadlines []struct{Claim,Kind,Recurrence,Title,Note,Original string;At *string}}
 if err=json.Unmarshal([]byte(os.Getenv("PCAS_HALL_FIXTURE")),&f);err!=nil{panic(err)}
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
 x("INSERT INTO claims(owner_id,id,organized,compared) VALUES($1,$2,$3,1)",f.Owner,c.ID,f.Version)
 value,_:=json.Marshal(c.Text)
 x("INSERT INTO claim_revisions(owner_id,claim_id,version,subject_id,predicate,value,nature,acquisition,confirmation,change_type,category,durable) VALUES($1,$2,1,$3,'虚构的事',$4::jsonb,'fact','direct','adopted','initial','event',true)",f.Owner,c.ID,f.Self,string(value))
 x("INSERT INTO evidence(owner_id,id,source_id,source_version,target_id,target_version,locator,acquisition,stance) VALUES($1,gen_random_uuid(),$2,1,$3,1,'{}','direct','supports')",f.Owner,c.Source,c.ID)
 }
 for _,d:=range f.Deadlines{x("INSERT INTO deadlines(owner_id,id,claim_id,claim_version,kind,at,recurrence,title,time_note,original_text) VALUES($1,gen_random_uuid(),$2,1,$3,$4::timestamptz,$5,$6,$7,$8)",f.Owner,d.Claim,d.Kind,d.At,d.Recurrence,d.Title,d.Note,d.Original)}
 if err=tx.Commit(ctx);err!=nil{panic(err)}
}`

test('大厅读真实后端：日程进「今天」和「这几天」，做完了落在记忆上，没时间的待办在「在推进」', async ({ page }) => {
  test.setTimeout(3 * 60_000)
  await login(page)
  await command(page, { type: 'updateSettings', patch: { timezone: 'Asia/Shanghai' } })
  const organize = (await snapshot(page)).organize
  // Shanghai has no daylight saving, so its calendar is UTC+8 all year.
  const local = (days: number, clock: string) => new Date(`${new Date(Date.now() + 8 * 3600_000 + days * 86400_000).toISOString().slice(0, 10)}T${clock}:00+08:00`).toISOString()
  const texts = ['今晚十一点五十五分和林栖通电话', '烤箱的保修前天到期，要问一下续保', '每天给酸面团喂一次', '说好了找个时间去看林栖的新店', '后天下午三点去市集办续约']
  const claims = texts.map((Text) => ({ ID: crypto.randomUUID(), Source: crypto.randomUUID(), Chunk: crypto.randomUUID(), Text }))
  const seed = {
    Owner: owner, Self: crypto.randomUUID(), Said: new Date(Date.now() - 3 * 86400_000).toISOString(), Version: organize?.version ?? 1, Claims: claims,
    Deadlines: [
      { Claim: claims[0].ID, Kind: 'appointment', At: local(0, '23:55'), Recurrence: '', Title: '和林栖通电话', Note: '', Original: texts[0] },
      { Claim: claims[1].ID, Kind: 'deadline', At: local(-2, '10:00'), Recurrence: '', Title: '给烤箱续保', Note: '', Original: texts[1] },
      { Claim: claims[2].ID, Kind: 'recurring', At: null, Recurrence: '每天', Title: '喂酸面团', Note: '', Original: texts[2] },
      { Claim: claims[3].ID, Kind: 'unclear', At: null, Recurrence: '', Title: '去看林栖的新店', Note: '没说哪天', Original: texts[3] },
      { Claim: claims[4].ID, Kind: 'appointment', At: local(2, '15:00'), Recurrence: '', Title: '去市集办续约', Note: '', Original: texts[4] },
    ],
  }
  const directory = mkdtempSync(join(tmpdir(), 'pcas-hall-browser-'))
  try {
    const path = join(directory, 'seed.go')
    writeFileSync(path, seeder)
    execFileSync('go', ['run', path], { cwd: repo, env: { ...process.env, PCAS_HALL_FIXTURE: JSON.stringify(seed) }, stdio: ['ignore', 'pipe', 'pipe'] })
  } finally { rmSync(directory, { recursive: true, force: true }) }
  // One to-do with no time, one due tomorrow.
  await command(page, { type: 'addTask', id: crypto.randomUUID(), title: '重写春季菜单' })
  const rent = crypto.randomUUID()
  await command(page, { type: 'addTask', id: rent, title: '交房租' })
  await command(page, { type: 'updateTask', id: rent, patch: { due: local(1, '18:00') }, summary: '定了截止时间' })

  const read = async <T>(path: string): Promise<T> => {
    const response = await page.request.get(`/v1/workspace/${path}`)
    expect(response.ok(), `${path}: ${await response.text()}`).toBeTruthy()
    return response.json()
  }
  const day = (n: number) => local(n, '12:00').slice(0, 10)
  // What the server sends is what the mocked tests assumed.
  const schedule = await read<Schedule>(`schedule?from=${day(0)}&to=${day(3)}`)
  expect(schedule.days.map((d) => d.date)).toEqual([0, 1, 2, 3].map(day))
  const call = schedule.days[0].items.find((e) => e.title === '和林栖通电话')!
  expect(call).toMatchObject({ kind: 'appointment', dateOnly: false, source: { kind: 'deadline', memoryId: claims[0].ID } })
  expect(new Date(call.at!).getTime()).toBe(new Date(local(0, '23:55')).getTime())
  expect(schedule.days.every((d) => d.items.some((e) => e.title === '喂酸面团' && e.kind === 'recurring'))).toBe(true)
  expect(schedule.unclear.map((e) => [e.title, e.originalText])).toEqual([['去看林栖的新店', texts[3]]])
  expect(schedule.overdue.map((e) => [e.title, e.kind, e.source.memoryId])).toEqual([['给烤箱续保', 'deadline', claims[1].ID]])
  expect(schedule.overdue[0].source.deadlineId).toBeTruthy()
  const progress = await read<InProgress>('in-progress')
  expect(progress.items.map((t) => t.title)).toEqual(['重写春季菜单'])
  expect(progress).toMatchObject({ total: 1, remaining: 0 })

  await page.goto('/')
  const today = page.getByRole('region', { name: '今天', exact: true })
  const group = (name: string) => today.locator('.hall-group').filter({ has: page.getByRole('heading', { name, exact: true }) }).first()
  await expect(group('按时间').locator('.hall-task').filter({ hasText: '和林栖通电话' }).locator('.hall-time')).toHaveText('23:55')
  await expect(group('按时间').locator('.hall-task').filter({ hasText: '喂酸面团' }).locator('.h-note')).toContainText('固定安排')
  await expect(group('这几天').locator('.h-title')).toHaveText(['交房租', '去市集办续约', '去看林栖的新店'])
  await expect(group('这几天').getByRole('group', { name: '日期没说清的' }).locator('.h-note')).toHaveText(`原话：${texts[3]}`)
  await expect(page.getByRole('region', { name: '在推进' }).locator('.h-title')).toHaveText(['重写春季菜单'])
  await expect(page.getByRole('alert')).toHaveCount(0)

  // It opens onto what was said, and from there onto the memory.
  const late = group('在等你，或已经晚了')
  await late.getByRole('button', { name: /^给烤箱续保/ }).click()
  const sheet = page.getByRole('dialog')
  await expect(sheet.locator('.source-text')).toHaveText(texts[1])
  await expect(sheet.getByRole('link', { name: '看这条记忆和它的来源' })).toHaveAttribute('href', `/library?m=${claims[1].ID}`)
  // 做完了 is marked on the memory: the date leaves the hall and the table, and no to-do is made.
  const before = (await snapshot(page)).tasks.length
  await sheet.getByRole('button', { name: '做完了：给烤箱续保' }).click()
  await expect(sheet).toHaveCount(0)
  await expect(today.getByText('给烤箱续保')).toHaveCount(0)
  expect((await snapshot(page)).tasks).toHaveLength(before)
  expect((await read<{ items: Deadline[] }>('deadlines?expired=true')).items).toEqual([])
  // And it can be taken back.
  await page.getByRole('button', { name: '撤销' }).click()
  await expect(late.getByText('给烤箱续保')).toBeVisible()
  expect((await read<{ items: Deadline[] }>('deadlines?expired=true')).items.map((d) => d.title)).toEqual(['给烤箱续保'])

  // A to-do with no time is finished from 在推进.
  await page.getByRole('region', { name: '在推进' }).getByRole('button', { name: '做完了：重写春季菜单' }).click()
  await expect(page.getByRole('region', { name: '在推进' })).toHaveCount(0)
})
