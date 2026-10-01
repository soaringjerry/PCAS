# 稳定化与二阶段准备：执行分工

2026-10-01。上位依据是 [白皮书](../../whitepaper.md)、[设计原则](../../design/principles.md)、[状态序列表](README.md) 和 [任务流程](../process.md)。本页安排执行，不把尚未完成的测试、PR 或部署写成已完成。

## 目标与角色

用户要求同时打磨稳定性、使用体验与记忆体验，然后进入第二阶段；协调者不写产品实现或测试，只负责任务、契约协调、审查和验收安排。执行者承担实现、测试和技术调查。

模型分配：Sol 默认承担实现、调查与独立回归，使用 `gpt-6.1-sol`、`high` 思考程度；Opus 仅用于有视觉与交互判断价值的整批界面工作；Astra 用于跨模块疑难和最后一次集成审查。Sol 同一问题两次仍无法推进，提交复现、已排除项和最小问题后才升级 Astra。

当前会话可直接调用 Sol 和 Astra，不能直接调用 Opus。Opus 的任务包照常准备，未启动前不得登记为执行中，也不能把其他模型的工作标成 Opus 完成。当前工具最多同时运行三个执行者。

## 已知基线

- main：`c94b496`，已包含 F6 / PR #19；该 PR 四项 CI 通过。
- F7：PR #20，`e63ee5e`，四项 CI 通过但尚未合并。旧指纹已失配的历史动作不能保证被它自动修复。
- F8：审查发现事项详情、秘书任务卡片、项目任务列表仍使用浏览器时区，和首页不一致。
- 生产默认通道、真机推送、Telegram 语音与完整 11 项实测没有新的验收结论。

## 任务与启动顺序

| 任务 | 执行者 | 交付 | 开始条件 |
|---|---|---|---|
| [F8 时区显示收尾](F8-timezone-displays.md) | Sol | 最小修复、跨页面回归、PR | 立即 |
| [T2 提醒与时间](T2-time-tests.md) | 独立 Sol | R1–R11 测试、失败证据与发现清单 | 立即，F6 已合并 |
| [M0 记忆现状与二阶段方案](M0-memory-readiness.md) | Sol | 实现差距、数据路径、评测方案；不写产品代码 | 立即 |
| [T1 撤销序列](T1-undo-tests.md) | 独立 Sol | U1–U16 测试，含固定种子随机序列 | F7 合并或协调者明确指定候选基线；先处理本文的契约冲突 |
| [T3 对话与渠道](T3-secretary-tests.md) | 独立 Sol | S1–S9、T1–T6、D1–D4 的回归与发现 | 空出执行位；基线包含 F7 |
| [U1 界面打磨](U1-ux-polish.md) | Opus | 首页与事项页的连贯体验、桌面/手机截图 | F8 合并后；等待可用 Opus 执行入口 |
| [F9 提醒边界与日志](F9-reminder-repairs.md) | Sol，非 T2 作者 | 修复 R1/R2/R5/R10，移除对应 finding skip | T2 最终交付；明确含 F7 的基线与测试文件交接 |
| [F8 完整浏览器补验](F8-ci-followup.md) | Sol | 定位 #23 完整 CI 的项目别名失败，修复测试数据生命周期 | 已启动，接手 F8 工作区 |
| [F10 撤销有效期](F10-expired-undo.md) | Sol，非 T1 作者 | U12/U13 与 D3 的 expired 判定/映射 | T1 交付；F9 已释放撤销文件 |
| [F11 对话与记忆删除](F11-secretary-repairs.md) | Sol，非 T3 作者 | S1/S8/S9/D1 的最小修复 | T3 最终证据和测试文件交接 |
| [F12 Telegram](F12-telegram-repairs.md) | Sol，非 T3 作者 | T2/T3/T6 的 bot 身份、回调与恢复修复 | T3 交付；F10 释放 poller.go |
| 修复发现项 | Sol | 按发现编号拆独立 PR | 先审查发现及预期，再分配产品文件 |
| [A1 集成审查](A1-acceptance.md) | Astra | 一次完整的集成检查和证据审查 | 实现与序列修复完成 |

第一批只启动 F8、T2、M0。这样一个修界面、一个独立测时间行为、一个调查记忆，文件互不重叠。

第一批已交付，执行者均停止写入：

| 任务 / 执行者 | 工作区 | 交付与实际状态 |
|---|---|---|
| F8 / `f8_timezone_displays` | `/root/PCAS` | [PR #23](https://github.com/soaringjerry/PCAS/pull/23)，`bddc14b`；前端时区 18/18、相关 mock 39/39、真实后端专项 1/1；完整远端 CI 的 golden 6/27 失败，由 `f8_ci_followup` 接手，未整体验收 |
| T2 / `t2_time_tests` | `/root/PCAS-wt/T2` | [PR #24](https://github.com/soaringjerry/PCAS/pull/24)，`a113988`；远端 CI 通过，但 R1/R2/R5 与 R10 日志为明确 finding skip，阶段未通过；测试写入权已交 F9 |
| M0 / `m0_memory_readiness` | `/root/PCAS-wt/M0` | [PR #22](https://github.com/soaringjerry/PCAS/pull/22)，`21faec7`；静态调查与待审方案，不代表二阶段实现或评测完成 |

第二批分派 F9、T3、T1，仍使用 Sol / high：

- F9 / `f9_reminder_repairs`：`/root/PCAS-wt/F9`。候选基线由 main `c94b496`、F7 `e63ee5e` 与 T2 `a113988` 组成；执行者在自己的隔离目录合并这两个已知输入，建立 `stabilization/reminder-candidate`（实际集成 SHA `64f0f7826830364635b20ad9ef9e6221e709897e`），再从其创建修复分支。PR 以该候选分支为 base，标题/正文说明依赖 #20、#24。这不是向 main 合并，依赖合并后须重新对 main 验收与调整 PR base。
- T3 / `t3_secretary_tests`：`/root/PCAS-wt/T3`，基线 F7 `e63ee5e`（已含 main `c94b496`）；PR 以 `fix/undo-chain` 为 base，明确依赖 #20。只读 F9 预留产品文件，发现交回协调者。
- T1 / `t1_undo_tests`：`/root/PCAS-wt/T1`，同 T3 基线与 PR base；先测契约明确部分，U3/U4/U5 的错误码重叠保持待裁定，不擅自选定新语义。该状态是待裁定，不能写成产品失败或通过。

执行者自行创建其尚不存在的 worktree，不改他人目录；若输入出现冲突，停止集成并报告，不能自行改产品解决。候选集成和分支推送均不得改动 main 或生产。

F9 已交付并停止写入：[PR #25](https://github.com/soaringjerry/PCAS/pull/25)，`26dba8672b578cf1baadd71c0ba257f7691d8109`，base 为上述 `64f0f78` 候选。全部 R 及新增边界 21 个顶层用例零 finding skip；最后一次提前量交叉修复后跑相关回归和 fmt/vet/build，完整 make check / DB integration 的已通过证据属于前一实现提交，具体见修复报告。远端 CI 仍待完成。actions_log.go 已移交 F10。

T1 已交付 [PR #26](https://github.com/soaringjerry/PCAS/pull/26) / `6d0c27e70ede296cb0247151ffc31f239051de3a`，50 种子 × 20 操作逆序通过；U12/U13 为 3 个 finding 叶用例，U3/U4/U5/U10 为 4 个待裁定叶用例，阶段未通过。T3 已交付 [PR #27](https://github.com/soaringjerry/PCAS/pull/27) / `071e87dc3dd40f08c834797900e68842180d41f9`，19 编号有测试，S1/S8/S9/D1/D3、Telegram T2/T3/T6 合计 8 项 finding skip。两位均停止写入。D3 与 T1-U12 合并归 F10，U10 当前轮新对象引用协议缺口另列，不擅自扩展 JSON。

第三批已启动：

- `f8_ci_followup` 接手 F8 工作区，已复现测试项目占用 P1 导致后续 golden 6 项失败，修复仅在测试数据收尾，正在完整 runner 补验。
- `f10_expired_undo` 使用 `/root/PCAS-wt/F10`，F9 + T1 候选 `stabilization/expiry-candidate` = `912ffad5189d73dd75fc09555d080c1b8d3609eb`，独占 F10 文档所列撤销错误/映射文件与已移交的 U12/U13 测试。
- `f11_secretary_repairs` 使用 `/root/PCAS-wt/F11`，先以 F9 + T3 候选实现 S1/S8/S9/D1；F10 最终提交并入候选后再验证同源 D3。独占两份 T3 PostgreSQL 测试及秘书/删除/provider 文件，不能与 F10 交叉修改。
- F12 仍排队，等待 F10 交付并释放 Telegram poller。Opus U1 和 Astra A1 均未启动。

## 文件归属与协作规则

- 每个任务一个 worktree 和分支；不得在他人的工作目录切分支、清理、格式化或提交。F8 接手 `/root/PCAS` 的 `fix/timezone-displays`，那里只有协调者在用户明确分工前留下的一处未验证测试草稿。
- 协调者使用 `/root/PCAS-wt/coordination` / `docs/stabilization-dispatch`，独占本目录的任务文档、阶段 README、契约和状态表。执行者只修改自己任务指定的报告。
- F8 独占前端时间显示及对应时区用例；T1/T2/T3 只新增各自编号的测试文件，不改共享测试夹具或产品代码；M0 只写 `docs/tasks/phase2/readiness.md`。
- 共享接口变化先向协调者报告：需要改什么、为什么、涉及谁、怎样兼容。协调者更新归属和契约后，由一个执行者改；其他人只消费接口。
- 不另建记忆库、状态库、模型适配层、提醒队列或日期处理依赖。先用现有模块；确有缺口列出最小扩展建议。
- 未实现的新规则以 finding 记录。测试暂时 skip 必须有真实失败记录和编号；阶段通过必须零 skip。不能降低预期来获得绿色结果。
- 测试执行者可读契约、公开类型、数据库迁移与现有测试夹具；不得按被测算法实现反推预期。遇到夹具缺口报告协调者，不越界改共享文件。
- 开 PR，提交信息用英文。可以推自己的分支，不推 main、不自行合并、不部署。用户对 #19 的合并授权不扩大到其他 PR。

## 环境与资源

- 遵守 [第一阶段环境规则](../phase1/README.md)：不连接、重启或改动生产 PCAS 及其他项目容器，不读取生产秘密。
- 测试用自建 tmpfs PostgreSQL；容器名包含自己的任务号。端口由 Docker 分配；F8 如需 HTTP 用 18138/18139，其他任务未经登记不要使用。
- 每个执行者自有依赖、日志和临时目录；不得并行改同一个 node_modules 或构建输出。Node 可用 `/root/.nvm/versions/node/v22.23.3/bin`，不要修改系统默认版本。
- 只终止自己记录的 PID；不按进程名匹配。任务结束清理自己的容器、卷和进程。
- 普通测试使用本地假模型和通知服务，不调用真实账户。真实通道与设备验收另排，不能由模拟测试代替。

## 开始 T1 前需要裁定的契约冲突

现有 U3/U5 要求跳过后续动作时提示先撤销后面的，最新契约新增 `newer_action`；但 U4 要求用户改标题后返回 `changed_since`，而界面修改标题本身也可能是一条可撤销动作。测试不能仅按“用户/秘书”猜测不同语义。

待裁定的具体规则是：有可追溯的、尚未撤销的后续动作统一返回 `newer_action`；无法归入后续动作的内容变化才返回 `changed_since`。这是提案，尚未修改正式契约。T1 先对该项输出歧义，不把任一解释当成已获确认的新规则。`expired` 的新规则已明确，旧文档中的 `changed_since` 描述应在契约收尾时统一。

## 交付格式与二阶段入口

每个执行者返回：分支/提交/PR、改动范围、测试命令与结果、发现编号和复现、未覆盖边界、资源清理结果。报告应区分观察、推断、计划和已通过证据。

二阶段设计和评测集准备可与稳定化并行；正式实现仍按现有入口条件：序列测试全部通过、线上实测 11/11、用户试用一天没有阻碍使用的问题。当前没有把任何一项标成完成。
