import { test, expect, type Page } from '@playwright/test';
import type { Memory, MemoryGroupEntry, State, Task } from '../src/domain/types';
import type { Deadline } from '../src/domain/status';
import { sameTargets } from '../src/store/conflict';
// Independent U1-U4 expectations were frozen before implementation inspection.
// This tests the presentation boundary using synthetic scale DTOs. All APIs
// fail closed; neither a production service nor Vite's backend proxy is reached.
const at = '2026-10-01T08:00:00Z';
const key = (i: number) => `fictitious/group ${i}`;
const id = (i: number) => `00000000-0000-4000-8000-${String(i).padStart(12, '0')}`;
const titles = ['身份与处境', '配合方式', '正在做的事', '时间节奏', '目标', '口味', '要求', '还不确定的事', '来源'];
const body = titles.map((title, i) => `## ${title}\nFictitious ceramics curator section ${i}.`).join('\n');
const task = (i = 1): Task => ({ id: `fictitious-task-${i}`, title: `Fictitious ceramics task ${i}`, status: 'todo', notes: '', checklist: [{ id: 'measure', text: 'Measure fictitious ceramics', done: false }], triggers: [], dependsOn: [], sources: [], history: [], createdAt: at, updatedAt: at });
function scale() {
    const memories: Memory[] = Array.from({ length: 5000 }, (_, i) => ({ id: id(i), recordVersion: 1, kind: 'fact', text: i === 1199 ? 'FictitiousFinalPageNeedle ceramics' : i % 2 ? `Fictitious ceramics claim ${i}` : `虚构陶艺资料 ${i}`, epistemic: 'sourced', confirmation: 'adopted', acquisition: 'direct', sources: [], versions: [], visibleTo: ['phase26'], exposure: 1, lastUsedAt: at, pinned: false, halfLifeDays: 30, reinforcementLimit: 8, mentions: [], groups: [], category: i < 363 ? 'rule' : 'progress', trust: 'stated' }));
    const entities = Array.from({ length: 1000 }, (_, i) => ({ id: id(5000 + i), name: i === 0 ? 'QZ' : i % 2 ? `Fictitious entity ${i}` : `虚构实体 ${i}` }));
    const kinds = ['person', 'project', 'topic', 'area'] as const;
    const directory: MemoryGroupEntry[] = Array.from({ length: 300 }, (_, i) => ({ key: key(i), kind: i >= 296 ? 'self' : kinds[i % 4], name: i === 0 ? 'QZ' : i === 296 ? '对助手的要求' : i >= 296 ? ['身份', '口味', '目标'][i - 297] : entities[i].name, count: i === 0 ? 1200 : i < 13 ? 500 : i === 296 ? 363 : 20 }));
    const members = Object.fromEntries(directory.map((g, i) => [g.key, i === 296 ? memories.slice(0, 363) : i === 0 ? memories.slice(0, 1200) : memories.slice(i * 13, i * 13 + g.count)]));
    const states = ['upcoming', 'expired_unknown', 'recurring', 'unclear'] as const;
    const deadlines: Deadline[] = Array.from({ length: 48 }, (_, i) => ({ id: `fictitious-deadline-${i}`, memoryId: id(i), title: `虚构陶艺安排 ${i}`, kind: i >= 24 && i < 36 ? 'recurring' : 'deadline', at: i < 12 ? new Date(Date.UTC(2026, 10, 20 - i)).toISOString() : i < 24 ? '2026-09-01T08:00:00Z' : null, dateStatus: states[Math.floor(i / 12)], recurrence: i >= 24 && i < 36 ? 'Every Tuesday / 每周二' : '', timeNote: i >= 36 ? 'No exact date / 下个月找时间' : '', originalText: memories[i].text }));
    const requirements = memories.slice(0, 363).map((m, i) => ({ memoryId: m.id, text: m.text, unrestricted: i < 24, scope: i < 24 ? '' : 'Fictitious ceramics planning' }));
    const state: State = { version: 1, revision: 1, memoryRevision: 1, budgetUsage: 0, settings: { dailyBudget: 10, autoAccept: false, wakeIdeas: false, followUps: false, dailyReviewAt: '09:00', timezone: 'UTC' }, tasks: [task(1), task(2)], ideas: [], projects: [], memories: [], memoryTotal: 5000, candidates: [], docs: [], runs: [], samples: [], sources: [], jobs: [], notices: [], activity: [], excludedMemories: {}, agents: [{ id: 'phase26', name: 'Fictitious model', enabled: true, available: true, default: true, channel: 'api', note: '', inputPrice: 0, outputPrice: 0, maxOutput: 100, memoryKinds: ['fact'], includeInferred: false }] };
    return { memories, entities, directory, members, deadlines, requirements, state };
}
async function backend(page: Page) {
    const mock = { ...scale(), asked: [] as string[], commands: [] as Record<string, unknown>[], unexpected: [] as string[], errors: [] as string[], failPath: '', failCursor: '', empty: false, etag: '"fictitious-1"', workspaceTags: [] as (string | undefined)[], unchanged: 0, conflictOnce: false, targetChanged: false, reads: (path: string) => mock.asked.filter(a => a.split('?')[0] === path).length };
    page.on('pageerror', e => mock.errors.push(e.message));
    await page.route('**/*', async (route) => {
        const url = new URL(route.request().url()), path = url.pathname;
        if (!['127.0.0.1', 'localhost'].includes(url.hostname))
            return route.abort();
        if (!path.startsWith('/v1/'))
            return route.continue();
        mock.asked.push(path + url.search);
        if (mock.failPath === path && (!mock.failCursor || mock.failCursor === url.searchParams.get('cursor')))
            return route.fulfill({ status: 503, json: { error: 'fictitious_storage_unavailable' } });
        if (path === '/v1/workspace') {
            mock.workspaceTags.push(route.request().headers()['if-none-match']);
            if (route.request().headers()['if-none-match'] === mock.etag) {
                mock.unchanged++;
                return route.fulfill({ status: 304, headers: { ETag: mock.etag } });
            }
            return route.fulfill({ json: mock.state, headers: { ETag: mock.etag } });
        }
        if (path === '/v1/desk/turns')
            return route.fulfill({ json: { conversationId: '', turns: [] } });
        if (path === '/v1/workspace/handover')
            return route.fulfill({ json: mock.empty ? { body: '', builtAt: '', stale: true } : { body, builtAt: at, stale: true } });
        if (path === '/v1/workspace/deadlines')
            return route.fulfill({ json: { items: mock.empty ? [] : mock.deadlines } });
        if (path === '/v1/workspace/assistant-requirements')
            return route.fulfill({ json: { items: mock.requirements } });
        if (path === '/v1/workspace/memory-groups')
            return route.fulfill({ json: { items: mock.directory } });
        if (path === '/v1/workspace/memory-facets')
            return route.fulfill({ json: { groups: [], people: [], places: [] } });
        if (path === '/v1/notify/config')
            return route.fulfill({ json: { webPush: { publicKey: '', subscriptions: 0 }, telegram: { configured: false, chatId: '' } } });
        if (path === '/v1/connectors')
            return route.fulfill({ json: [] });
        if (path === '/v1/connectors/imports')
            return route.fulfill({ json: { items: [] } });
        if (path === '/v1/models')
            return route.fulfill({ json: { chatgptEnabled: false, chatgptDirectEnabled: false } });
        if (path === '/v1/models/openai')
            return route.fulfill({ json: {} });
        if (path === '/v1/workspace/commands') {
            const c = route.request().postDataJSON();
            mock.commands.push(c);
            if (mock.conflictOnce) {
                mock.conflictOnce = false;
                mock.state = { ...mock.state, revision: mock.state.revision + 1 };
                if (mock.targetChanged)
                    mock.state.tasks[0] = { ...mock.state.tasks[0], checklist: [{ id: 'measure', text: 'Changed fictitious measurement', done: false }] };
                mock.etag = `"fictitious-${mock.state.revision}"`;
                return route.fulfill({ status: 409, json: { error: 'version_conflict' } });
            }
            expect(c.expectedRevision).toBe(mock.state.revision);
            if (c.type === 'toggleCheck')
                mock.state = { ...mock.state, tasks: mock.state.tasks.map(t => t.id === c.taskId ? { ...t, checklist: t.checklist.map(item => item.id === c.itemId ? { ...item, done: !item.done } : item) } : t) };
            if (c.type === 'updateSettings')
                mock.state = { ...mock.state, settings: { ...mock.state.settings, ...c.patch } };
            mock.state = { ...mock.state, revision: mock.state.revision + 1 };
            mock.etag = `"fictitious-${mock.state.revision}"`;
            return route.fulfill({ json: mock.state });
        }
        let items: Memory[] | undefined;
        if (path === '/v1/workspace/memories')
            items = mock.memories;
        const group = path.match(/^\/v1\/workspace\/memory-groups\/(.+)\/memories$/);
        if (group)
            items = mock.members[decodeURIComponent(group[1])] ?? [];
        if (items) {
            const from = Number(url.searchParams.get('cursor') ?? 0), limit = Number(url.searchParams.get('limit') ?? 50);
            return route.fulfill({ json: { items: items.slice(from, from + limit), next: from + limit < items.length ? String(from + limit) : '', total: items.length } });
        }
        const detail = path.match(/^\/v1\/workspace\/memories\/([^/]+)$/);
        if (detail)
            return route.fulfill({ json: mock.memories.find(m => m.id === detail[1]) });
        mock.unexpected.push(path);
        return route.fulfill({ status: 500, json: { error: 'unexpected_fictitious_request' } });
    });
    return mock;
}
const now = (page: Page) => page.getByRole('region', { name: '眼下', exact: true });
const part = (page: Page, name: string) => now(page).getByRole('region', { name, exact: true });
const clean = (mock: Awaited<ReturnType<typeof backend>>) => { expect(mock.errors).toEqual([]); expect(mock.unexpected).toEqual([]); };
test.use({ serviceWorkers: 'block', timezoneId: 'UTC', viewport: { width: 1280, height: 900 } });
test('independent U1/T3: all 48 deadlines and nine stale handover sections survive scale presentation', async ({ page }) => {
    const mock = await backend(page);
    expect(mock.memories).toHaveLength(5000);
    expect(mock.entities).toHaveLength(1000);
    expect(mock.directory).toHaveLength(300);
    expect(mock.directory.filter(g => g.count > 300)).toHaveLength(14);
    await page.goto('/library');
    const handover = part(page, '交接说明');
    await expect(handover).toContainText('正在更新，下面是上一份，写于');
    await expect(handover).toContainText('10月1日');
    await handover.getByRole('button', { name: '看全文（还有 7 节）' }).click();
    await expect(handover.getByRole('heading', { level: 4 })).toHaveText(titles);
    const dates = part(page, '期限和固定安排');
    for (const name of ['还没到的', '已过期，不知是否完成', '固定安排', '日期没说清的']) {
        const group = dates.getByRole('group', { name, exact: true });
        await expect(group.getByRole('listitem')).toHaveCount(5);
        await group.getByRole('button', { name: '还有 7 条' }).click();
        await expect(group.getByRole('listitem')).toHaveCount(12);
    }
    await expect(dates.getByRole('listitem')).toHaveCount(48);
    await expect(dates.getByRole('group', { name: '还没到的', exact: true }).getByRole('listitem').first()).toContainText(mock.deadlines[11].title);
    await dates.getByRole('group', { name: '已过期，不知是否完成', exact: true }).getByRole('listitem').first().getByRole('button').click();
    await expect(page.getByRole('dialog').getByRole('textbox', { name: '内容' })).toHaveValue(mock.memories[12].text);
    await page.goto('/library');
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(now(page)).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    clean(mock);
});
test('independent U2: member 1200, intersections and opaque legacy links use whole groups', async ({ page }) => {
    const mock = await backend(page);
    await page.goto(`/about?card=${encodeURIComponent(key(0))}`);
    await expect(page.locator('.mem-summary')).toContainText('有 1200 条');
    expect(new URL(page.url()).pathname).toBe('/library');
    expect(new URL(page.url()).searchParams.get('in')).toBe(key(0));
    const started = Date.now();
    await page.getByRole('textbox', { name: '搜索记忆' }).fill('FictitiousFinalPageNeedle');
    await expect(page.locator('.mem-entry')).toHaveCount(1);
    await expect(page.locator('.mem-summary')).toContainText('有 1 条');
    expect(mock.asked.filter(a => a.includes(`${encodeURIComponent(key(0))}/memories?limit=100`))).toHaveLength(12);
    console.log(`independent browser 1200-member search elapsed=${Date.now() - started}ms`);
    await page.getByRole('searchbox', { name: '搜索分组' }).fill('qz');
    await expect(page.getByRole('group', { name: '人', exact: true }).getByRole('button')).toHaveText(['QZ1200']);
    await page.goto(`/library?in=${encodeURIComponent(key(0))}&in=${encodeURIComponent(key(4))}`);
    await expect(page.locator('.mem-summary')).toContainText('有 500 条');
    expect(mock.asked.filter(a => a.includes(`${encodeURIComponent(key(4))}/memories?limit=100`))).toHaveLength(5);
    clean(mock);
});
test('independent U2/U3: failed seventh group page cannot masquerade as complete search', async ({ page }) => {
    const mock = await backend(page);
    mock.failPath = `/v1/workspace/memory-groups/${encodeURIComponent(key(0))}/memories`;
    mock.failCursor = '600';
    await page.goto(`/library?in=${encodeURIComponent(key(0))}&q=FictitiousFinalPageNeedle`);
    const alert = page.locator('.sheet .mem-state.failed');
    await expect(alert).toContainText('记忆没读出来');
    await expect(page.locator('.mem-entry')).toHaveCount(0);
    await expect(page.locator('.mem-summary')).not.toContainText('有 0 条');
    mock.failPath = '';
    await alert.getByRole('button', { name: '重试' }).click();
    await expect(page.locator('.mem-entry')).toHaveCount(1);
    await expect(page.locator('.mem-summary')).toContainText('有 1 条');
    clean(mock);
});
test('independent U3: empty, missing and unavailable status tell different truths', async ({ page }) => {
    const mock = await backend(page);
    mock.empty = true;
    await page.goto('/library');
    await expect(part(page, '交接说明')).toContainText('还没有交接说明');
    await expect(part(page, '期限和固定安排')).toContainText('没有记下期限或固定安排');
    await expect(now(page)).not.toContainText(/正在更新|稍后|正在整理/);
    mock.failPath = '/v1/workspace/deadlines';
    await page.reload();
    await expect(now(page).getByRole('alert')).toContainText('期限');
    mock.failPath = '';
    await now(page).getByRole('alert').getByRole('button', { name: '重试' }).click();
    await expect(part(page, '期限和固定安排')).toContainText('没有记下期限或固定安排');
    await page.goto('/about?card=fictitious-gone');
    await expect(page.getByText('没有这个分组，或者它名下已经没有记忆了。')).toBeVisible();
    await page.goto('/library?retired=1');
    await expect(now(page)).toHaveCount(0);
    await expect(page.getByRole('status', { name: '已被替代或合并的' })).toBeVisible();
    await expect(page.getByRole('link', { name: '关于你' })).toHaveCount(0);
    clean(mock);
});
test('independent U2/T3: 363 requirements disclose unrestricted and scoped rules', async ({ page }) => {
    const mock = await backend(page);
    await page.goto(`/library?in=${encodeURIComponent(key(296))}`);
    await expect(page.locator('.mem-summary')).toContainText('有 363 条');
    await expect(page.locator('.mem-applies')).toHaveCount(50);
    await expect(page.locator('.mem-applies').filter({ hasText: '不限范围，每一轮都带' })).toHaveCount(24);
    await expect(page.locator('.mem-applies').filter({ hasText: '范围：Fictitious ceramics planning' })).toHaveCount(26);
    expect(mock.reads('/v1/workspace/assistant-requirements')).toBe(1);
    clean(mock);
});
test('independent U4: unrelated snapshots and 304 polls do not reload memory lists', async ({ page }) => {
    const mock = await backend(page);
    await page.goto('/library');
    await expect(page.locator('.mem-entry')).toHaveCount(50);
    const paths = ['memories', 'handover', 'deadlines', 'memory-groups', 'memory-facets'].map(p => `/v1/workspace/${p}`), counts = () => paths.map(p => mock.reads(p)), before = counts();
    await expect.poll(() => mock.unchanged, { timeout: 10000 }).toBeGreaterThanOrEqual(2);
    mock.state = { ...mock.state };
    mock.etag = '"fictitious-background"';
    await expect.poll(() => mock.workspaceTags.includes('"fictitious-background"'), { timeout: 10000 }).toBe(true);
    expect(counts()).toEqual(before);
    await expect(part(page, '交接说明')).toContainText('Fictitious ceramics curator section 0');
    mock.state = { ...mock.state, memoryRevision: 2 };
    mock.etag = '"fictitious-memory"';
    await expect.poll(counts, { timeout: 10000 }).toEqual(before.map(n => n + 1));
    clean(mock);
});
for (const targetChanged of [false, true])
    test(`independent U4: checklist targetChanged=${targetChanged}`, async ({ page }) => {
        const mock = await backend(page);
        await page.goto('/t/fictitious-task-1');
        const checkbox = page.locator('.check-row').filter({ hasText: 'Measure fictitious ceramics' }).getByRole('button', { name: '完成', exact: true });
        await expect(checkbox).toBeVisible();
        mock.conflictOnce = true;
        mock.targetChanged = targetChanged;
        await checkbox.click();
        if (targetChanged) {
            await expect(page.locator('.connection-banner')).toContainText('已刷新，请检查后重试');
            expect(mock.commands).toHaveLength(1);
            await expect(page.locator('div.check-row')).toContainText('Changed fictitious measurement');
        }
        else {
            await expect(page.locator('.check-row').getByRole('button', { name: '标为未完成', exact: true })).toBeVisible();
            expect(mock.commands).toHaveLength(2);
            expect(new Set(mock.commands.map(c => c.requestId)).size).toBe(1);
            await expect(page.locator('.connection-banner')).toHaveCount(0);
        }
        clean(mock);
    });
test('independent U4: reminder conflict retries the same command', async ({ page }) => {
    const mock = await backend(page);
    await page.goto('/settings');
    const toggle = page.getByRole('switch', { name: '事项提醒', exact: true });
    await expect(toggle).toBeVisible();
    mock.conflictOnce = true;
    await toggle.click();
    await expect(toggle).toBeChecked();
    const commands = mock.commands.filter(c => c.type === 'updateSettings');
    expect(commands).toHaveLength(2);
    expect(new Set(commands.map(c => c.requestId)).size).toBe(1);
    await expect(page.locator('.connection-banner')).toHaveCount(0);
    clean(mock);
});
test('independent U4: bulk conflict gate checks selected targets at scale', () => {
    // Current App routes have no bulk dispatch control; this is the actual conflict
    // gate, not browser dispatch coverage. Background versions must not veto it.
    const before = scale().state, unrelated = { ...before, revision: 9, memoryRevision: 17 };
    for (const action of [{ type: 'bulkStatus', ids: ['fictitious-task-1', 'fictitious-task-2'], status: 'done' }, { type: 'bulkDefer', ids: ['fictitious-task-1'], days: 2 }, { type: 'bulkMove', ids: ['fictitious-task-1'], projectId: undefined }]) {
        expect(sameTargets(action, before, unrelated)).toBe(true);
        expect(sameTargets(action, before, { ...unrelated, tasks: unrelated.tasks.map(t => t.id === 'fictitious-task-1' ? { ...t, title: 'Changed selected fictitious task' } : t) })).toBe(false);
    }
});
