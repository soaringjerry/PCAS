# 第 1 阶段：秘书前台 · 任务总览

上位依据：[白皮书](../../whitepaper.md) 第 3、4、10、16、17 章，[界面与交互原则](../../design/principles.md)。
接口约定：[contracts.md](contracts.md)。**所有执行者开工前必须读完本页和 contracts.md。**

## 状态：已完成（2026-10-01 部署上线）

| 任务 | PR |
|---|---|
| A | #3 |
| B1 | #6、#7（删除原话清理）、#8（历史署名、回复简短） |
| B2 | #5 |
| C1 | #4 |
| C2 | #10 |
| D1 | #9 |
| D2 | #11、#12（遗留清理、一致性测试进 CI） |
| E | #13（[验收报告](../../evaluations/2026-09-30-phase1-acceptance.md)） |
| 修复 | #14（秘书动作历史去重）、#15（后台修订号不再误拒命令） |

未完成和后续事项见 [待办清单](../backlog.md)。

## 目标

一句话就能把事办了：导办台变成秘书，能从一句话生成带时间、项目、提醒的安排，回执附【改】【撤销】；
提醒能真正送到手上；事项页的按钮只剩三种。

## 任务与执行者

| 任务 | 内容 | 执行者 | 依赖 |
|---|---|---|---|
| [A](A-fixes.md) | 修三个硬伤：说明不保存、首页草稿丢失、失败不能重试 | 6.1 Sol | 无 |
| [B1](B1-secretary-backend.md) | 撤销基础设施 + 秘书后端 `/v1/desk/turn` | 6.1 Sol | A 已合并 |
| [B2](B2-secretary-frontend.md) | 秘书前端：连续对话、卡片、回执、撤销提示 | Opus 5.5 | A 已合并；按契约并行，不等 B1 |
| [C1](C1-notify.md) | 提醒通道：通知分发、Web Push（PWA）、Telegram、设置项 | 6.1 Sol | A 已合并；与 B1、B2 并行 |
| [C2](C2-telegram-inbound.md) | Telegram 双向对话：文字、语音、文件都能交给秘书，回执可在 Telegram 撤销 | 6.1 Sol | B1、C1 已合并 |
| [D1](D1-auto-adopt-backend.md) | 副手结果自动采纳（可撤销） | 6.1 Sol | B1 已合并 |
| [D2](D2-buttons-frontend.md) | 删按钮：事项页、首页"今天"置顶提醒、叫号条 | Opus 5.5 | B2、C1 已合并；D1 按契约并行 |
| [E](E-acceptance.md) | 真实浏览器跑黄金路径，收尾修复 | Astra | 以上全部合并（含 C2） |

```mermaid
flowchart LR
  A --> B1 & B2 & C1
  B1 --> D1 & C2
  C1 --> C2 & D2
  B2 --> D2
  D1 & D2 & C2 --> E
```

额度考虑：Opus 只做界面（B2、D2），Astra 只做最后的验收收尾（E），其余都给 Sol。
如果 Sol 在某个任务上连续两次卡住，把卡点写清楚交给 Astra，不要让 Astra 从头重做。

## 共同规则

**分支与提交**
- 每个任务一个分支 `phase1/<任务号>-<短名>`，从最新的 `main` 拉出。建议用 `git worktree` 放在 `/root/PCAS-wt/<任务号>`，互不干扰。
- 开 PR 合到 `main`，PR 标题以 `[phase1/<任务号>]` 开头；描述写清做了什么、怎么验证、哪些没做。
- 由用户决定合并。不要自己合并，不要推 `main`，**不要部署**。
- 提交信息沿用仓库风格（英文、祈使句、说明为什么）。

**文件归属**
- 只改下表中归你的文件。需要改别人的文件时，不要动手，在 PR 描述的「需要协调」一节写明要改什么、为什么。
- 新文件放在自己任务的范围内，命名按下表。
- 不要重复造轮子：先看 contracts.md 和下表，确认需要的东西是否已由别的任务负责。

| 文件 / 目录 | 归属 |
|---|---|
| `web/src/pages/ThingPage.tsx` | A（Header 说明保存、RunCard 失败重试）→ B2（只替换 `Composer`）→ D2（其余全部） |
| `web/src/pages/HallPage.tsx` | A（导办台草稿）→ B2（`Desk` 及其子组件）→ D2（`TodayWall`、`DecisionStrip`、`IdeaWall`） |
| `web/src/components/Secretary.tsx`、`web/src/components/SecretaryCards.tsx`、`web/src/domain/desk.ts` | B2（新建） |
| `web/src/store/StoreProvider.tsx`、`web/src/store/actions.ts`、`web/src/store/context.ts`、`web/src/store/toast.ts`、`web/src/store/api.ts`、`web/src/store/shell.ts`、`web/src/components/Shell.tsx` | B2 |
| `web/src/styles/*` | B2 → D2（先后进行） |
| `web/src/store/pendingDelegations.ts` | B2（删除） |
| `web/src/domain/hall.ts` | B2（删除 `looksLike*`、`ambiguousDelegation`）→ D2（其余） |
| `web/src/domain/types.ts` | C1（加 `Notice`、`State.notices`）→ D2（加 `Trigger.offset`、`Run.adopted.actionId/auto`）；其他人不改，新类型放自己的新文件 |
| `web/src/components/NotifySettings.tsx`、`web/public/sw.js`、`web/public/manifest.webmanifest`、`web/index.html` | C1（C2 只在 `NotifySettings.tsx` 加一句说明） |
| `web/src/pages/SettingsPage.tsx` | C1（只加一行挂载 `NotifySettings`） |
| `web/tests/desk-routing.spec.ts`、`web/tests/hall-ux.spec.ts` | B2（删除或改写） |
| `web/tests/fixes.spec.ts` | A 新建；B2 删掉了其中针对旧导办台的 3 个用例 |
| `web/src/domain/agent.ts`、`web/src/domain/lines.ts`（清理无调用方代码） | D2 |
| `web/tests/secretary.spec.ts` | B2；`web/tests/notify.spec.ts`：C1；`web/tests/buttons.spec.ts`：D2；`web/tests/golden.spec.ts`：E |
| `internal/postgres/migrations/016_action_log.sql` | B1 |
| `internal/postgres/migrations/017_notify.sql` | C1 |
| `internal/postgres/actions_log.go`、`internal/postgres/desk_turn.go`、`internal/postgres/desk_actions.go` | B1（新建） |
| `internal/postgres/commands.go`、`internal/postgres/workspace.go` 中的 `saveItem`、`saveDoc`、`Execute` | B1 |
| `internal/postgres/desk.go`（抽出共用上下文）、`internal/postgres/processing.go`（desk 来源跳过 task/idea） | B1 |
| `internal/postgres/editing.go`（只加删除传播：desk_turns.response 清理、action_log 快照过期） | B1 |
| `internal/postgres/workspace.go` 中的 `snapshotTx`（加 notices） | C1 |
| `internal/postgres/reminders.go`、`internal/postgres/notify.go`、`internal/notify/`、`internal/httpapi/notify.go`（均为新建或 C1 独有） | C1 |
| `web/src/main.tsx`（注册 Service Worker）、`web/public/icon-*.png`、`web/public/apple-touch-icon.png` | C1 |
| `internal/postgres/runs.go`（run 完成后的处理） | D1 |
| `internal/postgres/artifacts_test.go`、`desk_test.go`、`replay_test.go`、`ux_regressions_test.go`（只加 `undoAutoAdoption` 调用） | D1 |
| `internal/postgres/migrations/018_*.sql`、`internal/postgres/actions_log.go`（撤销白名单加 `training_samples`） | D1 |
| `internal/workspace/desk.go`（新建） | B1；`internal/workspace/notify.go`（新建）：C1 |
| `internal/workspace/model.go` | B1（`Trigger.Offset`、撤销相关 error）、C1（`State.Notices`）、D1（`Adoption.ActionID/Auto`）；各自只加不改 |
| `internal/httpapi/workspace.go`（注册 desk 路由） | B1 |
| `internal/httpapi/server.go` | B1（`fail()` 里加撤销错误映射）、C1（加一行注册通知路由） |
| `cmd/pcas/main.go` | C1（加一行启动通知分发循环）→ C2（加一行启动 Telegram 轮询） |
| `internal/telegram/`（新建） | C2 |
| `internal/notify/settings.go`（只加 telegramOffset、telegramConversation 字段） | C1 → C2 |
| `internal/postgres/attachments.go`（只在允许的媒体类型里加 `audio/ogg`） | C2 |
| `internal/ai/jev.go`、`/v1/desk/route`、`/v1/desk/answer` | **不要删**。第 1 阶段只是前端不再调用它们 |
| `web/src/components/CommandPalette.tsx`、`web/src/components/LineRow.tsx`、`web/src/domain/things.ts`、`web/src/domain/agent.ts`、`web/src/domain/lines.ts`、`web/src/domain/types.ts`（只做 D2 遗留清理：删「让副手… 约 ¥x」入口和无调用方代码，补 `Revision.by` 类型） | D2-followup |
| `internal/postgres/auto_adopt_test.go`（只改前端 adoptAs 的调用方式）、`.github/workflows/memory.yml`（加 setup-node，让前后端一致性测试在 CI 真正运行） | D2-followup |
| `web/tests/support/`、`internal/testsupport/`、`docs/evaluations/*-phase1-acceptance.md`、`web/tests/backend.spec.ts` 等依赖真实后端的旧用例 | E |
| `.github/workflows/browser-regression.yml` | B2（改为跑 mock 用例）→ D2（加 buttons.spec.ts）→ E（加 golden.spec.ts） |
| `docs/` | 各任务只改自己在任务文档里被要求改的文档 |

**环境（这台机器）**
- 线上 PCAS 就在这台机器上：`pcas-api-1`、`pcas-worker-1`、`pcas-db-1`，对外是 `https://pcas.coyumelabs.com`。
  **不要重启、重建或连接它们**；部署只在用户明确要求时进行。
- 机器上的其他容器（`dreamtrans-*`、`dt-*`、`glowtype-*`）属于别的项目，不要动。
- **结束进程只能按你自己记下的 PID**（启动时保存 `$!`，或写进 pid 文件）。禁止使用 `pkill`、`killall`、`kill $(pgrep …)` 这类按名字或模式匹配的命令：线上容器里的进程同样叫 `pcas`，宿主机上能匹配到（2026-09-30 发生过一次误杀）。
- 磁盘空间有限：测试库用 tmpfs，跑完立即删除容器和卷；不要在 `/tmp` 留下大体积缓存。
- 测试数据库自己起一个，用完删掉：
  ```sh
  docker run -d --name pcas-test-<任务号> -e POSTGRES_PASSWORD=test -p 127.0.0.1::5432 pgvector/pgvector:0.8.2-pg16-bookworm
  docker port pcas-test-<任务号> 5432   # 拿到随机端口
  export PCAS_TEST_DATABASE_URL="postgres://postgres:test@127.0.0.1:<端口>/postgres?sslmode=disable"
  ```
- 前端需要 Node 22.12+，系统里是 20。在 PCAS 目录里用 nvm 装一个本地版本，**不要改系统默认版本**：
  `nvm install 22 && nvm use 22`。
- 浏览器测试需要临时 API 实例：`pcas migrate` 之后执行 `pcas serve`，环境变量用 `PCAS_DATABASE_URL`（指向你的测试库）、`PCAS_OWNER_ID`（随机 UUID）、
  `PCAS_API_TOKEN`（至少 32 字符的测试值）、`PCAS_HTTP_ADDR=127.0.0.1:<空闲端口>`、`PCAS_WEB_DIR=web/dist`。
  然后按 `web/README.md` 跑 Playwright，同时设置 `PCAS_TEST_BASE_URL` 和 `PCAS_TEST_API_TOKEN`。
- 需要模型时，用 httptest 假服务器（写法见 `internal/postgres/desk_test.go`）。不要调用真实模型账号。

**验收与部署（2026-10-01 补充）**
- 用真实模型做验收时，必须走用户的**默认通道**（目前是 ChatGPT 订阅 / Codex），不能只测 API 通道。
- 每次部署后，在线上用默认通道对秘书说三句话（一个问题、一句带时间的安排、一句修改），回执和事项都正确，才算部署完成。

**完成标准（每个任务）**
- `make check` 通过；涉及数据库的改动，`make test-integration` 在自己的测试库上通过。
- 前端改动：`npm run lint && npm run type-check && npm run build` 通过，本任务的 Playwright 用例通过。
- 任务文档里的「验收」逐条满足，并在 PR 描述里逐条勾选。
- 没有改动归属表之外的文件。
- 测试容器和临时进程已清理。
