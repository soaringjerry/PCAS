# 第 1 阶段黄金路径验收（任务 E）

## 环境与范围

- 日期：2026-09-30（UTC）。浏览器和工作区时区均为 `Asia/Shanghai`，真实模型走查时当地日期已是 10 月 1 日。
- 分支：`phase1/E-acceptance`。本地初验代码：`5ab8c01`；初验基线 main：`a3adb4b`（已包含 D2-followup #12）。采用 rebase 更新基线，并重新 build、启动隔离实例、运行三轮黄金路径。报告中的初验提交号保留 rebase 前的值。
- 数据库：PostgreSQL 16.14 / pgvector 0.8.2，镜像 `pgvector/pgvector:0.8.2-pg16-bookworm`；独立测试容器，数据目录为 1 GiB tmpfs。
- 浏览器：Playwright 1.63.0 / Chromium 153.0.8010.12，无头真实浏览器，1440×1000；Go 1.26.8、Node 22.23.3。
- 前端先执行 production build，由真实 `pcas serve` 提供；另起真实 `pcas worker`。所有黄金路径及四个旧文件均访问真实 API，没有 `page.route` 或伪造 workspace 响应。
- 只有外部服务使用 `httptest`：模型按场景返回固定 JSON，Telegram Bot API 接收/投递消息与回调，Web Push 接收加密推送。测试进程的本地 HTTPS 代理只接受两个测试目的地址并转发到本地 TLS 服务。
- 未连接或操作线上 `pcas-api-1`、`pcas-worker-1`、`pcas-db-1`，未操作其他项目容器；未部署、推 main 或合并 PR。

## 自动化结果

三轮连续通过，**27/27**，无重试、跳过或预期失败。以下耗时含浏览器操作及真实服务等待。

| 用例 | 结果 | 第 1 / 2 / 3 轮耗时（秒） | 证据 |
|---|---|---|---|
| G1 一句话安排：时间、项目、提醒、刷新与撤销 | 通过 × 3 | 1.636 / 1.466 / 1.376 | [截图](2026-09-30-phase1-acceptance/G1.png) |
| G2 边问边记：回复前连续发送，两轮都处理 | 通过 × 3 | 3.224 / 3.136 / 3.038 | [截图](2026-09-30-phase1-acceptance/G2.png) |
| G3 改安排：同一件事改到周一，提醒跟随 | 通过 × 3 | 0.88 / 0.808 / 0.825 | [截图](2026-09-30-phase1-acceptance/G3.png) |
| G4 事项页秘书：自动加入三步、撤销、放回去 | 通过 × 3 | 3.902 / 4.208 / 3.894 | [截图](2026-09-30-phase1-acceptance/G4.png) |
| G5 失败恢复：副手首请求报错，原地重试成功 | 通过 × 3 | 7.129 / 6.84 / 6.879 | [截图](2026-09-30-phase1-acceptance/G5.png) |
| G6 提醒送达：真实等待一分钟、首页、Web Push、Telegram | 通过 × 3 | 85.922 / 93.957 / 93.991 | [截图](2026-09-30-phase1-acceptance/G6.png) |
| G7 Telegram 对话：入站文字、首页、回执按钮和撤销回调 | 通过 × 3 | 6.581 / 6.646 / 6.639 | [截图](2026-09-30-phase1-acceptance/G7.png) |
| G8 完成即撤销：首页勾选、toast、回到原位 | 通过 × 3 | 1.018 / 1.162 / 1.158 | [截图](2026-09-30-phase1-acceptance/G8.png) |
| G9 不丢话：模型真实超时后收到保存回执并能查原话 | 通过 × 3 | 91.263 / 91.461 / 91.633 | [截图](2026-09-30-phase1-acceptance/G9.png) |

送达请求：[Web Push / Telegram 记录](2026-09-30-phase1-acceptance/delivery.json)；超时回执：[已记下原话](2026-09-30-phase1-acceptance/G9-captured.png)；全部耗时与状态：[结构化结果](2026-09-30-phase1-acceptance/results.json)。

G1 检查服务端时间换算、项目 ID、`-30m` 提醒，刷新恢复回执，并在撤销后检查数据库快照和首页均没有该事项。G2 在首轮假模型延迟 2.5 秒期间发送第二句话，并验证回复乱序返回后新事项仍在。

G3 检查事项 ID 不变且 `nextAt` 跟随新的截止时间；G4 经事项页秘书发起真实副手 run，验证三条 checklist 的自动采纳、撤销和重新采纳。G5 首次副手请求返回 503，原地重试后成功，外部模型端点恰好收到两次副手请求。

G6 不快进时间：安排一分钟后到点，等待真实提醒循环和通知分发循环；首页“到点了”出现事项，Bot API 收到包含该事项的 `sendMessage`，推送端点收到带 VAPID 授权的非空 `aes128gcm` 请求。G7 投递文字消息，核对首页事项及回执中的撤销按钮，再投递真实回调协议并核对事项消失。

G8 验证首页完成按钮、toast 撤销以及同一事项恢复为 `todo`。G9 假模型延迟 95 秒，实际触发秘书 90 秒超时，检查“已记下原话”回执，再从资料库的“来源 → 展开原文”核对逐字保存的输入。

### Web Push 验证边界

已调用 Chromium 的 `grantPermissions(['notifications'])` 并按站点指定 origin，重新加载后 `Notification.permission` 仍为 `denied`；Service Worker 注册成功。依任务 E 明确允许的替代方案，使用有效的 P-256/auth 测试订阅，验证真实后端向本地推送端点发出加密请求。本次**没有证明 Service Worker 收到 push 或系统通知实际弹出**；通知权限结果和送达请求随测试产物保存。

## 旧的真实后端用例

四个文件均通过，**4/4**。

| 文件 | 结果 / 耗时 | 证据 |
|---|---|---|
| `backend.spec.ts` | 通过 / 5.193 秒 | [截图](2026-09-30-phase1-acceptance/backend.png) |
| `chatgpt-direct.spec.ts` | 通过 / 0.45 秒 | [截图](2026-09-30-phase1-acceptance/chatgpt-direct.png) |
| `continuity.spec.ts` | 通过 / 0.732 秒 | [截图](2026-09-30-phase1-acceptance/continuity.png) |
| `model-api.spec.ts` | 通过 / 0.892 秒 | [截图](2026-09-30-phase1-acceptance/model-api.png) |

- `backend.spec.ts`：删除已移除的“导办台 / 逐条确认 / 交给谁”入口与旧手动交接操作；保留并改写登录、秘书记事、worker 提取记忆、用户确认、纠正为新版本、查看纠正来源、刷新持久化，补上真实文件导出断言。副手链路由 G4/G5 覆盖。
- `continuity.spec.ts`：原场景仍有效，保留连接器持久化、受限 webhook 无权读 workspace、归档上传 202 等断言，仅补成功截图。
- `chatgpt-direct.spec.ts`：原场景仍有效，仅补截图；隔离实例启用空的 direct-ChatGPT 目录，验证未登录状态、授权入口说明和响应不含 token，不发起真实账户授权。
- `model-api.spec.ts`：删除全部模型设置/向量补建的浏览器 mock，改为真实 API 保存、刷新持久化、分开的文本/向量 key 状态、响应不泄露密钥，以及按实际返回的 queued 数断言补建排队。

## 修复与提交

本次没有修改产品代码。本地初验没有暴露必须跨文件归属修复的功能阻塞；两项旧用例兼容修复分别提交。

| 提交 | 暴露问题的用例 / 变更 |
|---|---|
| `da60a11` | `backend.spec.ts` 仍依赖 B2/D2 已删除界面；迁移至秘书和资料库流程，保留记忆确认/纠正/来源/持久化并补导出 |
| `eb0399b` | `model-api.spec.ts` 用 mock 固定配置和 queued=3，无法证明真实设置保存；改为实际 API 和返回值断言 |
| `6cc5a9d` | 新增 G1–G9、httptest 外部服务、临时实例 runner 及真实后端 CI job |
| `93e0d84` | 保留的 `continuity.spec.ts` / `chatgpt-direct.spec.ts` 补成功截图 |
| `5ab8c01` | 加强 G1：明确断言首页事项行在创建后及刷新后均存在，不能只依赖回执链接 |

新增的 `browser-real-backend` CI job 用同一 runner 运行黄金路径三轮和四个旧文件；始终上传截图、JSON 结果、失败 trace 与服务日志。原 `browser-mocked` job 保留。

后续 [CI](https://github.com/soaringjerry/PCAS/actions/runs/36780118367/job/110107960824) 的 G4 第三轮在 `beforeEach` 的 `updateSettings` 命令处因后台 worker 推进 revision 而返回 `version_conflict`；该普通命令被全局 revision 校验误拒绝的问题由 [F2 #15](https://github.com/soaringjerry/PCAS/pull/15) 修复。E 已 rebase 到包含 F1/F2 的 main `bafa5a9`；`golden.spec.ts` 的四处命令辅助调用均为普通命令，现有辅助函数读取快照、携带 `expectedRevision` 与独立 `requestId` 的写法兼容新契约，无需调整。

## 检查与清理

| 检查 | 结果 |
|---|---|
| `make check` | 通过；[日志](2026-09-30-phase1-acceptance/make-check.log) |
| `make test-integration` | 通过，PostgreSQL 集成测试 43.035 秒；[日志](2026-09-30-phase1-acceptance/integration.log) |
| 前端 `npm run lint && npm run type-check && npm run build` | 通过 |
| 初验基线上的本地黄金路径 | 27/27，连续三轮约 10.4 分钟，无重试、跳过或预期失败 |
| 四个旧后端文件 | 4/4 |
| `secretary / fixes / notify / buttons` 原有回归 | 38/38，27.4 秒 |
| 临时资源清理 | runner 正常退出；全部 E 测试容器及其卷已删除；启动的 serve / worker / httptest 进程均已等待退出；临时配置、测试 CA、真实凭据副本已删除 |

[完整浏览器运行日志](2026-09-30-phase1-acceptance/browser-runs.log)。初验时 `make check` / 集成测试暴露的前后端采纳规则对照测试失败，由 D2-followup #12 修复；E 已 rebase 到该合并提交后重跑全部检查，未越过归属边界代修。

截至报告提交，E 测试容器查询为空，runner 的 `data/golden-*` 临时目录为空。未在 `/tmp` 留存大体积缓存。

复现命令（仓库根目录，Node 22.12+）：

```sh
(cd web && npm ci && npm run build && npx playwright install chromium)
bash web/tests/support/real-backend.sh
```

runner 自建测试库并在退出时删除自己的容器及卷、临时配置、CA 和二进制；只按记录的子进程 PID 结束进程。用例结果留在 `web/test-results/`，支持脚本说明见 [support/README.md](../../web/tests/support/README.md)。

## 真实模型走查（已获用户同意）

用户明确回复“同意”，允许使用本机已有的 PCAS Codex/API 配置后才执行。只从宿主机读取现有文本 API 配置，复制到隔离目录；使用另一个临时测试数据库与独立 serve，后台 worker 仍用假模型。没有进入线上容器，也没有改写线上配置。实际只发出 4 次真实模型请求，覆盖以下 3 个场景，模型为配置中的 `gpt-6.1-sol`。

| 场景 | 观察 | 模型请求耗时 | 截图 |
|---|---|---|---|
| 一句话安排 | 匹配现有 A 项目；周五 15:00 → `2026-10-02T07:00:00Z`，提醒为当天 14:30；回执读作“明天”，与上海当日 10 月 1 日一致 | 4.819 秒 | [完整对话](2026-09-30-phase1-acceptance/real-conversation.png) |
| 边问边记 | 问“我今天有什么事？”尚未返回时发送电费输入，输入框可用，两轮均处理；电费先返回，未被随后问答覆盖 | 问答 5.177 秒；电费 3.822 秒 | [完整对话](2026-09-30-phase1-acceptance/real-conversation.png) |
| 改安排 | 修改同一个电费 ID 为周一 10:00；原日期型提醒 `offset=09:00` 保留，`nextAt` 移到周一 09:00，事项页可见 | 6.692 秒 | [完整对话](2026-09-30-phase1-acceptance/real-conversation.png)、[事项提醒](2026-09-30-phase1-acceptance/real-reminder.png) |

四次回复都为一句短话，没有变成长文；回执由后端生成，安排/改期均自然可读。合成测试数据的[响应观察记录](2026-09-30-phase1-acceptance/real-observations.json)随报告保存，不含账号或密钥。截图为对话完成后从真实后端恢复的完整对话及事项页；没有再次调用真实模型。

## 遗留问题与需要协调

1. **已由 [F1 #14](https://github.com/soaringjerry/PCAS/pull/14) 修复 · 原 P2 · B1（D2 配合展示）：一次秘书创建暴露多条内部修改历史。** 真实走查中，一次创建电费任务的 `history` 包含两条“创建”和一条“更新”；随后改期又增加一条“更新”。事项页因此显示重复/难以区分的活动行。原始 history 与[事项截图](2026-09-30-phase1-acceptance/real-reminder.png)保留为初验记录；F1 已将同一个秘书动作合为一条历史，E 通过 rebase 纳入修复。
2. **P3 · A/D2（错误契约由后端配合）：失败原因分类较粗。** G5 的真实后端将上游错误转换为“模型调用未完成，结果和用量可能未确认；请检查登录、额度与服务配置”，前端 `failureText` 仅匹配英文关键字，最终显示“没做成：出了点问题”，详细原因在 title 中。这符合 A 文档的兜底规则且重试成功，但不能直接区分服务异常/登录/额度。建议后续统一稳定错误码和简短原因，保留避免重复付费请求的行为。
3. **验证覆盖 · C1：补做实际设备通知验收。** 自动化满足 E 允许的后端推送请求替代断言；真实浏览器 push 服务、系统通知和真实 Telegram 客户端显示仍需有权限的设备验收，不在本次假外部服务证明范围内。
