# 已批准规则的独立验收

2026-10-01，T4，Sol/high。用户确认同一事项逆序撤销，以及一句话新建任务后继续加步骤。**四个已批准 U 待决项已在集成后端候选实际通过，原候选真实失败仍保留。完整 Go 隔离 race 通过；整体候选的 UX 新提交与前端浏览器验证仍待完成，真实线上/真机/一天试用没有本次证据。**

## 独立预期与原候选证据

产品基线 `59e25daa67df7447f0a09b7f0bd9e5d65edab65e`。先读协调工作区正式契约 §1.4/§2.1，再编写测试；没有阅读算法来反推 oracle，也没有修改产品、共享夹具或其他工作区。新别名按原 actions 数组 1-based 位置 `N1`/`N2`，不是按成功次数编号。

保留 U3/U4-user/U5 的全业务数据比较、拒绝时 revision/undone_at 检查，增加精确错误码与后续逆序成功断言。U10 通过实际 `Store.DeskTurn` 和真实本地假模型 HTTP 新建「交作业」并经 `N1` 增加「查资料」「写提纲」，验证目标、独立 action_log 和先撤销步骤再撤销创建。原 THIS update/add_steps 用例保留为独立回归，原 U16 50 种子 × 20 操作及其业务预期均未改。

U9 正常 `toggleCheck` 会写 action_log，因此按已确认规则要求 `newer_action`；保留原业务场景并扩展逆序成功验证。另一个直接 SQL 修改 checklist 的子例模拟未记录外部业务更改，仍要求 `changed_since`。这不是把正常接口夹具改成外部写入来回避新规则。

新独立覆盖还包括多个新对象隔离、`project`/`set.project`、新想法引用、解析失败/提交失败后保持原位置、前向/越界/零/非创建别名、附带项目与 `delegate:new` 不绑定 N、类型错误与 THIS 隔离、同 requestId 重放、同轮 R1/N1 分别绑定旧/新事项、下一轮 N 清空及 R1 保持既有语义、Used/Links/Show 不接受 N、原数组前十动作上限。三次公开命令及同轮三动作的 first→second→first 场景，要求内容恢复原值时仍不能跳过后续动作。

| 原候选检查 | 真实结果 |
|---|---|
| U3、U4-user、U5 | 应 HTTP 409 `newer_action`，实得 409 `changed_since` |
| U4 未记录外部 title、U9 未记录外部 checklist | HTTP 409 `changed_since`；全业务状态不变，revision 不变，动作未标 undone |
| U9 正常 checklist 接口、原 THIS 同轮回归 | 应 HTTP 409 `newer_action`，实得 409 `changed_since` |
| U10 create_task + N1 add_steps | 创建成功但步骤没有执行，checklist 为空 |
| 三公开命令 first→second→first 后跳撤创建 | 应 HTTP 409 `newer_action`，实得 HTTP 200，任务被删除 |
| 多对象、原位置解析/提交失败、N 重放和动作上限 | 合法 N 依赖动作被跳过，独立契约断言失败；不放宽预期 |

失败已交 F13/F14 作者；没有修改实现。原候选日志保留于 `/tmp/pcas-test-T4/original-failures.jsonl`、`original-new-rules.jsonl`、`original-returned-content.jsonl`、`original-invalid-aliases.jsonl`、`original-r-n-isolation.jsonl`，相应 `.stderr` 同目录。第一轮 U/THIS 选择运行 2 个 pass、8 个 fail 测试事件；新增 N 选择运行 7 个 pass、5 个 fail；返回原内容选择运行 3 个 fail。事件包含父测试和子测试，不能当作独立场景总数。

命令：

```sh
PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33268/postgres?sslmode=disable' \
 go test -race -count=1 -json ./internal/postgres \
 -run 'TestStabilizationUndo(U3|U4|U5|U9|U10)|TestStabilizationUndoExistingTHIS'
# 同一隔离数据库，另外独立选择 ^TestApprovedRules_、返回原内容和非法别名回归。
make check
```

测试分支 `make check` 退出 0，fmt/vet/全包无 DB 的 race/build 通过；没有设置数据库的此条命令会跳过 PG 集成，不能代替上述实际数据库复现或最终完整集成。非法 N/THIS 隔离的七个子例在原候选通过，含附带项目与 delegate:new。

## 已集成后端的独立验收

协调者于 2026-10-01 明确移交 A1。确认 `/root/PCAS-wt/A1` / `stabilization/acceptance-candidate` 为干净 `59e25da` 后，依次 `merge --no-ff` F13 `f771e2cd8d559c023d25733a877d8f0a435ad5a6`、F14 `32724b3ee90c982d08a8ac0f5364b187169fd291`、T4 独立测试 `877eed3ee2b26f07ba85451e9b0bc689f6b788e3` 和完整协调分支 `docs/stabilization-dispatch` / `ecc7f2a4125ee952bbdee0db1e1db0170ab7c394`。全部无冲突，后端组合 SHA **`210144a93753bed736d2d5561a454f8d07185eb0`**。没有自行改产品。

本组合实际执行：

```sh
env -u PCAS_TEST_CODEX_BINARY -u PCAS_LIVE_CODEX_HOME \
 -u PCAS_LIVE_EMBEDDING_URL -u PCAS_LIVE_CODEX_BINARY \
 -u PCAS_TEST_DATABASE_URL make check

env -u PCAS_TEST_CODEX_BINARY -u PCAS_LIVE_CODEX_HOME \
 -u PCAS_LIVE_EMBEDDING_URL -u PCAS_LIVE_CODEX_BINARY \
 PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33268/postgres?sslmode=disable' \
 go test -race -count=1 -json ./cmd/... ./internal/...
```

| 检查 | 本组合结果 |
|---|---|
| `make check` | 退出 0，fmt/vet/无 DB 全包 race/build 通过 |
| 带隔离 DB、禁缓存的全包 Go race | 退出 0；11 个有测试包全部通过；PG 139.387s，Telegram 7.620s，notify 5.308s |
| U3/U4-user/U5/U10 | 全部通过，四个 pending 归零；精确 `newer_action`、拒绝时全业务/revision/log 不变与逆序成功均执行 |
| U9 两条路径与原 THIS 同轮回归 | 全部通过，正常后续 action 为 `newer_action`，无记录外部变化为 `changed_since` |
| 新独立 N/动作顺序测试 | 8 个顶层全部通过，包含内容改回原值仍不得跳撤、两种事务路径及 R/N 隔离 |
| U16 | 原 50 种子 × 20 操作全部通过，业务预期未改 |
| Go Test 事件口径 | 637 pass、0 fail、3 skip；包含父测试/子测试，不能当 637 个独立场景；无测试的包级 skip 未计入 |
| 新 UX 与前端全套 | 等待 U2 最终交接；尚未启动 mock/default browser，无新前端通过声明 |

准确的三项 skip 为 `TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`。live 环境开关显式清空，三项未授权而未运行，没有用 fake 来宣称其通过。日志 `/tmp/pcas-test-T4/backend-make-check.log`、`backend-go-race.jsonl`、`backend-go-race.stderr` 和结构化 `backend-results.json` 保留。撤销四项的关闭依据是实际集成后端运行，不是删 skip。

F13 迁移前 `action_order=NULL` 的历史并列，只在回执唯一且能证明先后时重建顺序；未知并列保守 `changed_since`。这项已写入正式契约与作者报告，不能把新链全部通过推广为任意旧链可自动恢复。A1 原报告的三轮执行顺序风险、多行草稿/键盘边界仍保留为历史与后续 UX 验证输入。

## 并行 UX 独立专项：冻结构建，不是最终候选

用户要求继续并行后，T4 只新增 `web/tests/usability-acceptance.spec.ts` 和获分配的 mock CI 列表；未读 U2 作者新测试来照抄 oracle，没有改产品。CI 保留原五文件并追加 `settings-things-ux.spec.ts` 与独立 `usability-acceptance.spec.ts`；真实 runner、超时与业务断言未改。

按协调者授权，只读复制 U2 尚未提交的第一版 `web/dist` 至 `/tmp/pcas-test-T4/ux-checkpoint-dist`，复制前/后/副本的九文件 asset 清单与 SHA256 完全一致。checkpoint 指纹 **`05e3270ea93cd5f54832729cabb3738478d95a07c2bab4585ac69216765f2478`**，复制时间 `2026-10-01T08:16:48 UTC`，index 构建 `08:15:18 UTC`。唯一晚于构建的源文件为 `thing.css`（08:16:21 UTC），因此这里只评价冻结的第一版构建，不能推广为当前源或最终候选。清单与时间在 `ux-checkpoint-manifest.json`。

独立 18 场景覆盖：三设置开关只 patch 各自字段，预算/模型默认/记忆权限不被静默修改；每模型 enabled/includeInferred 显式修改仅写对应字段；连接、接入与记忆范围折叠用键盘可开合且不发写入；1440/390 的保存拒绝/重试、等待期间快速点击只发一次、说明草稿与原目标保留、焦点及横向溢出；真实 Library 待确认入口；task/idea/memory 精确命令与 memoryKind；unknown 在没有明确选择时零写入，点具体去向按钮才提交对应有效 kind；忽略使用 ignoreCandidate、不删除来源、不提供假撤销；接受后仅服务端 resolvedInto 与事项匹配时有正确入口，无可靠目标不猜 ID 导航。直接结果按钮是有效选择，不机械要求旧下拉/disabled 流程，也不固定作者的同义词偏好。

测试开发的差异已分类并保留日志：首轮 16 场景 12 pass/4 fail，失败是不可用订阅 fixture 不应要求 ChatGPT heading，以及先验文案句子并非契约要求；按可见公开定位校准后，字段/来源/无假撤销断言保持。扩展 18 场景运行为 17 pass/1 fail，最后一项因 fixture 把模型设为停用、权限控件合法 disabled；显式权限修改改用已启用模型后，仅该变动例重跑 1/1 pass。停用模型原默认仍由其他保存/折叠用例验证。**这是分轮专项覆盖，不声称单次 18/18 或最终候选全绿；没有确认产品缺陷。**

```sh
# A1/web，自有静态冻结预览 18156，所有 v1 请求均由独立 mock 拦截。
PCAS_TEST_BASE_URL=http://127.0.0.1:18156 \
 PLAYWRIGHT_JSON_OUTPUT_NAME=/tmp/pcas-test-T4/ux-checkpoint-18.json \
 npx playwright test tests/usability-acceptance.spec.ts \
 --output=test-results/T4-ux-checkpoint-18 --reporter=list,json
# 同一冻结构建，fixture 修正仅重跑 explicit includeInferred。
npx eslint tests/usability-acceptance.spec.ts
npx tsc --noEmit --target ES2023 --module ESNext --moduleResolution Bundler \
 --skipLibCheck tests/usability-acceptance.spec.ts
```

独立 spec eslint/TypeScript 检查退出 0；浏览器原始日志 `ux-checkpoint-16.log`、`ux-checkpoint-18.log/json`、`ux-checkpoint-permission.log/json` 保留，trace 在 A1/web/test-results。预览使用自有持续进程、记录 PID 后精确停止，18156/18157 无监听。桌面 Chromium 的 390 宽与键盘焦点测试不等于真实手机软键盘验收。

只读审查 U2 当前四个既有真实 spec：chatgpt-direct 加两个连接展开；continuity 加接入展开；model-api 初次/重载各加 API 展开；golden 三处按钮名定位更新。现有权限、数据、secret redaction、错误、状态、撤销断言均保留，未改 skip/retry/timeout/runner。最终提交仍须核对完整 diff。

## 后续验证与资源

用户追加设置页/事项页 UX 工作后，协调者要求完整前端检查、mock 与默认真实后端 browser 留待 U2 最终输入，避免重复验证同一前端候选。因此当前没有推中间候选或更新 #32；待整体最终检查后一次推送并更新现有 Draft PR。Q1 后续确认三轮执行顺序问题，F15 正在独立修复并将改变后端；最终合入后会合理重跑完整 Go 隔离验收，210144a 的既有绿色只属于该早期后端组合。

线上 11 项、真机和一天试用未完成。没有生产、秘密、真实账号、外发、main 合并或部署。

本任务自建 `pcas-test-T4-integration`，pgvector PostgreSQL16，1GiB tmpfs，Docker 动态 localhost 33268；全部后端测试完成后按记录先 stop 再 rm -v，已删除，只清理自有资源。每个集成测试仍使用共享 helper 的独立随机 schema。HTTP 18156/18157 尚未启动固定服务，浏览器 runner 的自有 tmpfs/HTTP 资源在最终整体检查后补记清理证据。
