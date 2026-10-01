# 稳定化与二阶段准备：执行分工

2026-10-01。上位依据是 [白皮书](../../whitepaper.md)、[设计原则](../../design/principles.md)、[状态序列表](README.md) 和 [任务流程](../process.md)。本页安排执行，不把尚未完成的测试、PR 或部署写成已完成。

## 目标与角色

用户要求同时打磨稳定性、使用体验与记忆体验，然后进入第二阶段；协调者不写产品实现或测试，只负责任务、契约协调、审查和验收安排。执行者承担实现、测试和技术调查。

模型分配：Sol 默认承担实现、调查与独立回归，使用 `gpt-6.1-sol`、`high` 思考程度；Opus 仅用于有视觉与交互判断价值的整批界面工作；Astra 用于跨模块疑难和最后一次集成审查。Sol 同一问题两次仍无法推进，提交复现、已排除项和最小问题后才升级 Astra。

协作工具可调用 Sol 和 Astra；Opus 经本机已登录 Claude CLI 执行，启动元数据确认 claude-opus-5-5，界面任务使用 high。各入口按同一份文件归属表协调，同时最多三个执行者，不把其他模型的工作标为 Opus。

## 当前交付

所有修复仍在独立 PR / 候选分支，除用户此前授权的 #19 外，没有合并 main 或部署。

| 项目 | 最终已交付 SHA / PR | 当前验证状态 |
|---|---|---|
| F8 时区 | 2e6ebc1 / #23 | 最终远端全部通过；32 个完整真实后端用例通过 |
| F9 提醒 | 26dba86 / #25 | 最终远端全部通过；R 边界零 finding skip |
| F10 撤销有效期 | 8d2a70d / #28 | 最终远端全部通过；当时保留的4个契约待决项现已由F13/F14/T4关闭 |
| F11 对话 | d38a0b4 / #29 | 原超时浏览器旧文案已补齐；G8/G9/F7 各三轮共 9/9，最终远端全部通过 |
| F12 Telegram | ae9f69b / #30 | 浏览器假 Bot 的 getMe 格式与回调 ID 已补齐，G6/G7 各三轮 6/6；不放宽真实身份与绑定；已停止写入 |
| U1 界面 | b0386f9 / #31 | Opus 5.5/high 交付 5 项修复、15 张截图，62/62 mock 与 lint/type/build 通过；最终远端全部通过；Claude CLI已停止 |
| M0 记忆准备 | 21faec7 / #22 | 静态调查与待审二阶段方案，未开始产品实现 |
| A1 历史总审 | 产品候选 7aae834 / 报告59e25da | 当时Go race、mock62/62、真实后端41/41通过，4 pending和3可选live skip单列；本轮后续交付见下方，不能把该历史SHA的结果当成最新组合验证 |
| F13 撤销规则 | f771e2c / #36 | 动作先后顺序、错误分类与历史兼容；T4已独立验证 |
| F14 同轮新建后继续操作 | 32724b3 / #35 | N引用按原actions位置绑定，仅更早成功创建可用；T4已独立验证 |
| U2 设置与事项体验 | 56f1e3c / #38 | Opus最终81个mock分轮全部通过，lint/type/build通过；21张截图；已退出并清理预览 |
| F15 连续输入顺序 | 9b17700 / #37 | 已提交接受票据FIFO、故障原话恢复、删除传播；make check和完整PG race147.380秒通过 |
| Q2 独立顺序验收 | 69bbdc6 | 5组独立race通过，旧契约7顶层/12叶通过，零finding skip；已停止并清理自有库 |
| T4 最新汇总 | 产品/独立测试组合4aa2746；最终测试适配a92bd8c；报告92ccab5 / Draft #32 | 本地验证已分轮闭合，详见下文与准确报告；远端结果以#32对应head的checks为准。未合main、未部署 |

T1 的 U3/U4-user/U5/U10 已于 2026-10-01 获用户确认，并经 F13/F14 修补、T4 独立完整后端验证全部通过。A1-L1 后由 Q1 实际复现：三个已进入服务端的连续修正可能按 1→3→2 执行，使最后的五点被早先的四点覆盖；已安排 F15 修复。线上 11 项、真机与一天试用仍无新证据。后文分批记录是历史经过，状态以本表和最新报告为准。

本轮新增 [F13 撤销规则](F13-approved-undo.md)、[F14 同轮连续操作](F14-same-turn-actions.md)、[T4 独立验收](T4-approved-rules-acceptance.md)，均从已通过远端全部 CI 的 #32 / `59e25da` 开始。F13 独占 actions_log，F14 独占秘书执行/解析，T4 独占原 U 测试并最终接手候选集成；没有共享写入文件。HTTP 分别预留 18152/53、18154/55、18156/57，数据库端口由 Docker 分配。协调者的任务/契约文档将随本轮一起集成入 #32，之后 #21 作为已包含的文档输入处理，不重复合并。此次用户确认是行为规则确认，不代表授权合 main 或部署。

后续已交付：F13 `f771e2c` / Draft #36（顺序、错误提示、历史兼容）；F14 `32724b3` / Draft #35（本轮新建事项继续操作）；独立 T4 测试 `877eed3`。T4 将三者及协调文档无冲突合入后端组合 `210144a`，make check 和完整隔离数据库 Go race 通过，637 个 Test pass 事件、零失败，仅三项可选真实模型 skip；U3/U4-user/U5/U10 归零、U9 与 50×20 保留并通过。A1 本地报告提交后为 `33c1efa`，没有推候选或部署；最终前端验证等待下述 UX 交付。

用户追加设置页、事项页体验打磨，特别是设置功能开关与「收下」含义不清：[U2](U2-settings-things-ux.md) 由 Opus 5.5/high 经本机 Claude CLI 执行，最终 `56f1e3c` / #38 已交付。设置按用途分组，技术表单按需展开；最终默认输入框15→2、开关11→7（取最终报告，早期checkpoint数字已被替代）。原 UnsureSheet 未挂载，现资料库提供最小待确认入口；按钮直接写创建待办、保存为记忆、加入子任务、保存为文档等具体动作，成功反馈给真实去向。S1最终 `6ab5c6f` 的源码事实报告已纳入候选。

T4独立首版18场景通过后又确认挂载标题/说明不同步的既有缺陷，作者修复，独立新checkpoint2/2复验通过；root还提出仅focus未改字却可能反写旧值的场景，Opus先复现再修复并新增回归。81个作者mock与20个独立mock均进入最终7文件CI列表。协调者只审查、协调和写任务/契约，不写产品或测试；正式二阶段实现仍未开始。

[Q1](Q1-conversation-order-audit.md) 的 `98b9a01` 保留原始失败诊断，不合入默认候选。独立 [F15](F15-conversation-order.md) 已修复；接受点是元数据票据短事务提交，不声称HTTP网络到达全序。终结旧请求重试仅补存原话，不晚于后轮执行业务，也不自动提取旧指令；来源可读/导出且删除不复活。[Q2](Q2-conversation-order-acceptance.md) 非实现者用真实入口独立验证并只交付新增测试/报告。T4已合入最终F15、U2、S1和Q2，对 `4aa2746` 运行新的完整Go/PG与真实后端浏览器验收；旧 `210144a` 的结果只作历史证据。

最终整合发现并修正三处测试适配，未降低业务预期、未改变产品：独立mock仍用旧开关名；Go前后端等价harness漏载U2的新源常量；golden正常改名后撤销旧动作仍期待用户已否定的旧错误码。准确本地结果为mock首轮100通过+唯一定位补跑1通过；Go原生首轮652个Test pass事件/唯一harness失败/3项可选live skip，适配后make check及真实Node等价40个Test事件通过；默认真实浏览器首轮38通过/同一旧契约三次失败，授权更新后该场景三轮通过，完整覆盖41次执行。原失败日志保留，不声称这些本地检查均为单次全绿。最终产品树不变、没有未处理finding；报告见 [T4最终汇总](../../evaluations/2026-10-01-approved-rules-acceptance.md)。执行者各自资源已清理；上线11项、真机和一天试用仍未完成，正式二阶段实现未开始。

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
| [U1 界面打磨](U1-ux-polish.md) | Opus | 首页与事项页的连贯体验、桌面/手机截图 | F8 最终候选交付并停止写入；已通过 CLI 启动 |
| [F9 提醒边界与日志](F9-reminder-repairs.md) | Sol，非 T2 作者 | 修复 R1/R2/R5/R10，移除对应 finding skip | T2 最终交付；明确含 F7 的基线与测试文件交接 |
| [F8 完整浏览器补验](F8-ci-followup.md) | Sol | 定位 #23 完整 CI 的项目别名失败，修复测试数据生命周期 | 已启动，接手 F8 工作区 |
| [F10 撤销有效期](F10-expired-undo.md) | Sol，非 T1 作者 | U12/U13 与 D3 的 expired 判定/映射 | T1 交付；F9 已释放撤销文件 |
| [F11 对话与记忆删除](F11-secretary-repairs.md) | Sol，非 T3 作者 | S1/S8/S9/D1 的最小修复 | T3 最终证据和测试文件交接 |
| [F12 Telegram](F12-telegram-repairs.md) | Sol，非 T3 作者 | T2/T3/T6 的 bot 身份、回调与恢复修复 | T3 交付；F10 释放 poller.go |
| 修复发现项 | Sol | 按发现编号拆独立 PR | 先审查发现及预期，再分配产品文件 |
| [M1 最新上下文路径审计](M1-context-path-audit.md) | Sol / high | 对照独立调查，真实入口抓输入、最小复现与证据报告 | 用户新调查已收到；产品候选7aae834 |
| [M2 二阶段范围与验收](M2-phase2-scope-review.md) | Sol / high | 核查官方资料，更新既有readiness及验收方案 | 与M1并行研究，消费其证据后定稿 |
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

F9 已交付并停止写入：[PR #25](https://github.com/soaringjerry/PCAS/pull/25)，`26dba8672b578cf1baadd71c0ba257f7691d8109`，base 为上述 `64f0f78` 候选。全部 R 及新增边界 21 个顶层用例零 finding skip；最后一次提前量交叉修复后跑相关回归和 fmt/vet/build，完整 make check / DB integration 的已通过证据属于前一实现提交，具体见修复报告。最终远端 CI 全部通过。actions_log.go 已移交 F10。

T1 已交付 [PR #26](https://github.com/soaringjerry/PCAS/pull/26) / `6d0c27e70ede296cb0247151ffc31f239051de3a`，50 种子 × 20 操作逆序通过；U12/U13 为 3 个 finding 叶用例，U3/U4/U5/U10 为 4 个待裁定叶用例，阶段未通过。T3 已交付 [PR #27](https://github.com/soaringjerry/PCAS/pull/27) / `071e87dc3dd40f08c834797900e68842180d41f9`，19 编号有测试，S1/S8/S9/D1/D3、Telegram T2/T3/T6 合计 8 项 finding skip。两位均停止写入。D3 与 T1-U12 合并归 F10，U10 当前轮新对象引用协议缺口另列，不擅自扩展 JSON。

第三批已启动：

- `f8_ci_followup` 接手 F8 工作区，已复现测试项目占用 P1 导致后续 golden 6 项失败，修复仅在测试数据收尾，正在完整 runner 补验。
- `f10_expired_undo` 使用 `/root/PCAS-wt/F10`，F9 + T1 候选 `stabilization/expiry-candidate` = `912ffad5189d73dd75fc09555d080c1b8d3609eb`，独占 F10 文档所列撤销错误/映射文件与已移交的 U12/U13 测试。
- `f11_secretary_repairs` 使用 `/root/PCAS-wt/F11`，先以 F9 + T3 候选实现 S1/S8/S9/D1；F10 最终提交并入候选后再验证同源 D3。独占两份 T3 PostgreSQL 测试及秘书/删除/provider 文件，不能与 F10 交叉修改。
- F10 已交付 [PR #28](https://github.com/soaringjerry/PCAS/pull/28) / `8d2a70df721dd433d45d2232b0405280f0156ca7` 并停止写入；U12/U13 原 3 叶失败均修复，完整 PG、Telegram、前端及 22 个 mock 通过，4 个 pending 叶仍保留。F11 已把它合入最终候选 `7cc48813ffd0906c6b003c4500e5a0cf40b4e322`。
- F12 / `f12_telegram_repairs` 已接手 `/root/PCAS-wt/F12`，F10 + T3 候选 `stabilization/telegram-candidate` = `e9e5edb0cc1658dfaf04a80d6265a0dd77cbef24`。Telegram 文件、获授权的新只读查询文件与 notify 身份/关联存储归其所有。
- D1 产品 finding 已在 F11 复核撤回：原夹具未实际授权/引用 claim，改正并加强引用断言后，原删除实现通过；详见 F11 任务与原 T3 报告后续更正。其他 S1/S8/S9、Telegram 三项仍为真实修复范围。
- F8 完整 CI 补验最终交付 2e6ebc1：修正项目别名污染后完整 runner 32/32，最终 thingId 选择器专项 1/1。已经停止写入；最终远端完整浏览器仍在运行。
- F11 已交付 [PR #29](https://github.com/soaringjerry/PCAS/pull/29) / e5841a2abc01319ffe3e06c08953ae20976c8665 并停止写入；18 个 S/D 顶层用例零 finding skip、make check、完整 PG race 通过。并发保证互斥和整轮提交，不保证多个排队请求严格 FIFO，交 A1 评估。
- F12 已交付 [PR #30](https://github.com/soaringjerry/PCAS/pull/30) / c1a0173629568c4588ed1f942af28e6010dd4f04 并停止写入；Telegram/notify 全包 race、make check、完整 DB integration 通过，三个渠道 finding 正常执行；该分支未含 F11，继承的 5 个 S/D skip 与 4 个待裁定 U 叶不算通过。
- U1 已于 06:46 UTC 经 Claude CLI 启动，模型 claude-opus-5-5 / high；工作区 /root/PCAS-wt/U1，从 F8 最终 2e6ebc1 开始。预览端口 18142，范围和证据归 U1 任务。
- Astra A1 接手最终后端候选审查，先做跨模块只读审查，待 U1 交接后在最终候选执行一次完整验收。已知四个 U 待决叶和线上验收不虚报通过。

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

## T1 原契约冲突与本轮确认

现有 U3/U5 要求跳过后续动作时提示先撤销后面的，最新契约新增 `newer_action`；但 U4 要求用户改标题后返回 `changed_since`，而界面修改标题本身也可能是一条可撤销动作。测试不能仅按“用户/秘书”猜测不同语义。

2026-10-01 用户在读过「同一事项从最后一次修改往回撤销；一句话新建任务再给它加步骤」的说明后确认「对啊 这没问题啊」。据此将已有提案写入正式契约：有可追溯且尚未撤销的后续动作统一返回 `newer_action`；无法归入后续动作的业务内容变化才返回 `changed_since`，不按用户/秘书区分。同步统一已实现的 `expired` 文档。四项从待裁定变为待执行验证，不能直接改成验收通过。

## 交付格式与二阶段入口

每个执行者返回：分支/提交/PR、改动范围、测试命令与结果、发现编号和复现、未覆盖边界、资源清理结果。报告应区分观察、推断、计划和已通过证据。

二阶段设计和评测集准备可与稳定化并行；正式实现仍按现有入口条件：序列测试全部通过、线上实测 11/11、用户试用一天没有阻碍使用的问题。当前没有把任何一项标成完成。

用户独立记忆调查纳入M1/M2评审：累计新增两个Sol/high并行，只做代码核查、最小实证与文档，不启动二阶段功能实现。“1.8”仅沟通用里程碑，正式仍为1.5收尾，入口条件不自动更改。

## 二阶段独立调查评审交付

- M1：Draft [PR #33](https://github.com/soaringjerry/PCAS/pull/33)，7e4895a31a912cf66916136f6af26ad34350cb91；只新增审计报告与opt-in复现测试。最新产品7aae834上原文已授权召回仍在三个消费入口丢失已实证；公开Ingest无source grant与公开授权入口缺口分开记录。同conversationId已有claim纠正/连源删除后，真实请求与历史旧marker正确失效。没有实现修补。
- M2：Draft [PR #34](https://github.com/soaringjerry/PCAS/pull/34)，b1c50fdb58e2e031de47338cbd25d55bf96708e1；只更新既有readiness与新增配套acceptance。五份官方来源已核查，范围分已实现/P0修补/P1核心/P2与后续实验，验收从真实入口与持续任务计量，阈值待基线校准。
- A1、M1、M2及其他执行者均已停止写入。累计14名执行者（12 Sol、1 Opus、1 Astra），所有执行任务high；后续补验复用原执行者。工作区/服务已按各报告清理，原始证据保留。二阶段只有调查、复现与设计文档，未开始产品实现；第一阶段正式入口仍未全部满足。
