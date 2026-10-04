# 文档总入口

PCAS 的所有文档都从这一页找。新增或移动文档时，同时改这一页。

## 1 现在做到哪了

| 阶段 | 状态 | 入口 |
|---|---|---|
| 第 1 阶段：秘书前台 | 已上线（2026-10-01） | [任务总览](tasks/phase1/README.md) |
| 第 1.5 阶段：稳定化 | 修复已合并；线上实测 11 项和试用一天还没做 | [稳定化计划](tasks/stabilization/README.md) |
| 第 2 阶段：记忆核心 | 分四批，**全部已上线**：第 1 批 2026-10-02，第 2 批 2026-10-03，第 3 批、第 4 批和补充批 4b（按整段对话整理）2026-10-03 一起上线。还没做的：用真实模型跑回忆评测并定基线。第一次尝试（2.0）已回滚 | [任务入口](tasks/phase2/README.md) · [方向稿](tasks/phase2/direction.md) · [第 1 批](tasks/phase2/batch1/README.md) · [第 2–4 批并行方案](tasks/phase2/parallel.md) |
| 第 2.5 阶段：记忆整理 | 方向已定（2026-10-04）。分四批加一个评测集；**评测集和第 1 批（分组和类型）的任务包已写，等执行者开工**；第 2–4 批的契约在前一批上线后再写 | [任务入口](tasks/phase2_5/README.md) · [第 1 批契约](tasks/phase2_5/batch1/README.md) · [讨论稿](research/memory-next-direction.md) |
| 第 3 阶段及以后 | 未开始 | [白皮书 §16 路线图](whitepaper.md) |

还没处理的问题统一记在 [待办清单](tasks/backlog.md)。

## 2 按目的找

**想知道产品要做成什么**

| 文档 | 内容 |
|---|---|
| [白皮书](whitepaper.md) | 定位、团队角色、记忆核心、路线图、黄金路径。上位文档，其他文档与它冲突时以它为准 |
| [界面与交互原则](design/principles.md) | 先做后报、按钮只剩三种、出错要说清楚等界面和后台都要遵守的规则 |
| [记忆架构](memory-architecture.md) | 四层记忆的完整设计 |
| [产品需求](prd.md) | 需求清单 |
| [旧版经验记录](legacy-lessons.md) | 上一版踩过的坑 |

**要开始做任务**

| 文档 | 内容 |
|---|---|
| [任务流程规则](tasks/process.md) | 所有阶段通用：先推演状态、测试和实现分开、真实配置验收、部署后冒烟测试 |
| 第 1 节里对应阶段的入口 | 该阶段的范围、任务包、契约 |
| [待办清单](tasks/backlog.md) | 已知但没处理的问题，排任务时从这里取 |
| [H1 说了「尽快」的事排进首页时间线](tasks/improvements/H1-urgent-in-timeline.md) | 阶段之间的小改进：规则、验收序列、上线记录（2026-10-03 已上线） |
| [L1 资料库的「来源」按出处合并](tasks/improvements/L1-library-sources-by-origin.md) | 阶段之间的小改进：之前的问题、规则、数据、测试（2026-10-03 用户确认） |
| [D1 待办、想法、项目可以删除](tasks/improvements/D1-delete-a-thing.md) | 阶段之间的小改进：规则、数据、测试（2026-10-03） |
| [T1 Telegram 里的回复不再缺斤少两](tasks/improvements/T1-telegram-replies.md) | 阶段之间的小改进：交办回执、链接、副手做完后把结果发回来（2026-10-03） |
| [U1 归档上传：分片，压缩包只传对话文件](tasks/improvements/U1-archive-upload-in-pieces.md) | 阶段之间的小改进：上传失败的原因、分片规则、只取对话文件、图片不解析的决定（2026-10-03） |
| [Q3 历史对话及时进入记忆提取](tasks/improvements/Q3-conversation-dispatch.md) | 修复旧入口阻塞领取、记忆提取排在索引之后的问题（2026-10-03） |
| [Q4 后台进程回收与索引持续推进](tasks/improvements/Q4-process-cleanup-and-index-lane.md) | 修复残留 Codex 子进程耗尽内存，增加独立索引通道（2026-10-03） |
| [E4 记忆保留原对话上下文](tasks/improvements/E4-memory-evidence-context.md) | 来源展开原对话、模型补读证据上下文、模糊指代核对（2026-10-03） |
| [M1 秘书看得见图片](tasks/improvements/M1-images-for-the-secretary.md) | 待做：模型看图、发给秘书的图片当成说的话、网页输入框带附件（2026-10-03 用户提出） |
| [M1 image acceptance expectations](tasks/improvements/M1-images-expectations.md) | V1–V10 expectations written before implementation; synthetic fixtures only |
| [M1 image validation](evaluations/2026-10-03-m1-images.md) | Rule coverage, Codex protocol evidence and synthetic desktop/mobile screenshots |
| [R1 检查报告里的九个问题](tasks/improvements/R1-review-1003-fixes.md) | 2026-10-03 独立检查发现的问题：已修的四条、交给执行者的四条规则、没改的一条 |
| [P1 资料多了以后，秘书回话不能变慢](tasks/improvements/P1-recall-speed.md) | 导入 2.7 万条后秘书回一句话要 46 秒：量出的原因、改法、前后对比（2026-10-03） |
| [W1 副手能联网查资料](tasks/improvements/W1-deputy-web-search.md) | 副手查不了网的原因、改法、哪些角色和通道能联网（2026-10-03） |

**要部署、接模型、接数据**

| 文档 | 内容 |
|---|---|
| [部署与模型接入](deployment.md) | 部署方式、模型配置 |
| [Sign in with ChatGPT 套餐授权](chatgpt-plan-auth.md) | ChatGPT 订阅直连通道 |
| [通用资料接入](connectors.md) | 连接器、归档导入、摘要 |
| [统一记忆与工作台服务](memory-service.md) | 后端接口、配置、验证边界 |
| [前端说明](../web/README.md) | 前端开发和浏览器测试 |

线上实例的发布目录和操作说明不在仓库里，在部署主机的 `/root/PCAS-deploy/current/`。

**要查某次验收或评测的证据**：见第 4 节。

## 3 目录约定

| 目录 | 放什么 |
|---|---|
| `docs/` 顶层 | 长期有效的说明：白皮书、架构、需求、部署、接入 |
| `docs/design/` | 界面与交互原则 |
| `docs/tasks/<阶段>/` | 该阶段的任务包和契约。每个阶段有一个 `README.md` 作为入口 |
| `docs/tasks/` 顶层 | 跨阶段的流程规则和待办清单 |
| `docs/evaluations/` | 验收、评测、复盘记录。文件名以日期开头，写完之后不再改写结论 |
| `docs/research/` | 外部方案的研究笔记 |

- 依据的先后：白皮书 → 交互原则、记忆架构、产品需求 → 阶段契约 → 任务包。下游与上游冲突时先改上游。
- 执行者不修改白皮书和记忆架构，除非用户明确要求。
- 每份验收记录都要能从它所属阶段的入口或本页找到。
- 作废的方案不留在 `docs/` 里当作可用文档；需要保留的，打成 git 标签并在阶段入口里写明位置。

## 4 全部文档

### 第 1 阶段

入口：[任务总览](tasks/phase1/README.md) · [接口契约](tasks/phase1/contracts.md) · [上线后修复与线上实测清单](tasks/phase1/postlaunch.md)

| 任务包 | 说明 |
|---|---|
| [A](tasks/phase1/A-fixes.md) | 修三个硬伤 |
| [B1](tasks/phase1/B1-secretary-backend.md) · [B2](tasks/phase1/B2-secretary-frontend.md) | 撤销基础设施和秘书后端；秘书前端 |
| [C1](tasks/phase1/C1-notify.md) · [C2](tasks/phase1/C2-telegram-inbound.md) | 提醒通道；Telegram 双向对话 |
| [D1](tasks/phase1/D1-auto-adopt-backend.md) · [D2](tasks/phase1/D2-buttons-frontend.md) | 副手结果自动采纳；删按钮 |
| [E](tasks/phase1/E-acceptance.md) | 黄金路径验收与收尾 |

验收和评测记录：

| 记录 | 内容 |
|---|---|
| [第 1 阶段黄金路径验收](evaluations/2026-09-30-phase1-acceptance.md) | 任务 E 的验收报告（截图在同名目录） |
| [第 1 阶段上线后问题复盘](evaluations/2026-10-01-phase1-postlaunch.md) | 上线后暴露的问题和原因 |
| [连续记忆验收](evaluations/2026-09-29.md) | 固定场景的真实模型验收（数据在 `2026-09-29-continuity.json`） |
| [记忆与权限边界修复](evaluations/2026-09-30-memory-boundaries.md) | 2026-09-30 的边界修复 |
| [界面回归修复](evaluations/2026-10-01-ux-repairs.md) | PR #1 之后的界面回归 |

### 稳定化

入口：[稳定化计划](tasks/stabilization/README.md)（状态序列表和进入第 2 阶段的条件）· [执行分工](tasks/stabilization/dispatch.md)

| 任务包 | 说明 | 验收记录 |
|---|---|---|
| [F8](tasks/stabilization/F8-timezone-displays.md)（[补验](tasks/stabilization/F8-ci-followup.md)） | 各页面按工作区时区显示 | [时区显示验收](evaluations/2026-10-01-timezone-displays.md) |
| [F9](tasks/stabilization/F9-reminder-repairs.md) | 提醒边界与失败日志 | [提醒修复](evaluations/2026-10-01-reminder-repairs.md) |
| [F10](tasks/stabilization/F10-expired-undo.md) | 撤销有效期 | [撤销有效期评测](evaluations/2026-10-01-expired-undo.md) |
| [F11](tasks/stabilization/F11-secretary-repairs.md) | 对话串行、删除传播、失败说明 | [秘书修复](evaluations/2026-10-01-secretary-repairs.md) |
| [F12](tasks/stabilization/F12-telegram-repairs.md) | Telegram 身份、按钮绑定、语音重试 | [Telegram 修复](evaluations/2026-10-01-telegram-repairs.md) |
| [F13](tasks/stabilization/F13-approved-undo.md) | 逆序撤销规则 | [逆序撤销](evaluations/2026-10-01-approved-undo.md) |
| [F14](tasks/stabilization/F14-same-turn-actions.md) | 一句话新建并继续操作 | [同轮动作](evaluations/2026-10-01-same-turn-actions.md) |
| [F15](tasks/stabilization/F15-conversation-order.md) | 连续输入按接受顺序执行 | [顺序修复](evaluations/2026-10-01-conversation-order-repair.md) |
| [Q1](tasks/stabilization/Q1-conversation-order-audit.md) · [Q2](tasks/stabilization/Q2-conversation-order-acceptance.md) | 连续输入顺序的复现和独立验收 | [顺序独立验收](evaluations/2026-10-01-conversation-order-acceptance.md) |
| [T1](tasks/stabilization/T1-undo-tests.md) | 独立检验撤销与动作序列 | [撤销序列评测](evaluations/2026-10-01-stabilization-undo.md) |
| [T2](tasks/stabilization/T2-time-tests.md) | 独立检验提醒与时间 | [时间测试](evaluations/2026-10-01-stabilization-time.md) |
| [T3](tasks/stabilization/T3-secretary-tests.md) | 独立检验秘书、Telegram、删除传播 | [秘书测试](evaluations/2026-10-01-stabilization-secretary.md) |
| [T4](tasks/stabilization/T4-approved-rules-acceptance.md) | 独立验证四个已确认用例 | [已批准规则验收](evaluations/2026-10-01-approved-rules-acceptance.md) |
| [U1](tasks/stabilization/U1-ux-polish.md) | 首页、事项页、回执的体验 | [体验打磨](evaluations/2026-10-01-ux-polish.md) |
| [U2](tasks/stabilization/U2-settings-things-ux.md) | 设置和事项的日常使用体验 | [设置与事项体验](evaluations/2026-10-01-settings-things-ux.md) |
| [S1](tasks/stabilization/S1-settings-audit.md) | 设置开关实际作用清单 | [设置控件核对](evaluations/2026-10-01-settings-control-audit.md) |
| [A1](tasks/stabilization/A1-acceptance.md) | 集成审查与进入第 2 阶段的判定 | [集成审查与候选验收](evaluations/2026-10-01-stabilization-acceptance.md) |
| [C1](tasks/stabilization/C1-ci-parallel-real-backend.md) | 真实后端浏览器测试并行 | [并行 CI 评测](evaluations/2026-10-01-ci-parallel-real-backend.md) |
| [M0](tasks/stabilization/M0-memory-readiness.md) · [M1](tasks/stabilization/M1-context-path-audit.md) · [M2](tasks/stabilization/M2-phase2-scope-review.md) | 第 2 阶段开工前的三项调查 | M0 的产出是 [记忆核心调查](tasks/phase2/readiness.md)；M1、M2 的产出没有合并，见第 2 阶段入口第 6 节 |

### 第 2 阶段

| 文档 | 内容 |
|---|---|
| [任务入口](tasks/phase2/README.md) | 要交付什么、用户已经定下的事、约束、现在的基线、可参考的归档 |
| [方向稿](tasks/phase2/direction.md) | 分四批怎么做、每批上线后能感受到什么。用户已确认 |
| [2.0 回滚记录](evaluations/2026-10-01-phase2-0-rollback.md) | 第一次尝试做了什么、为什么回滚、回滚后的状态 |
| [记忆核心调查](tasks/phase2/readiness.md) | 2.0 之前的代码现状调查。方案部分未批准 |

第 1 批：原话直达。入口：[任务总览与契约](tasks/phase2/batch1/README.md)

| 任务包 | 说明 | 执行者 |
|---|---|---|
| [A](tasks/phase2/batch1/A-raw-text.md) | 原话供给、依赖校验、依据标记、依据卡片（后端） | 6.1 Sol |
| [B](tasks/phase2/batch1/B-undo-memory.md) | 撤销连带记忆、抽取跳过、清理 2.0 遗留 | 6.1 Sol |
| [C](tasks/phase2/batch1/C-frontend.md) | 依据卡片里的原话、「依据已更新」标记（前端） | Opus 5.5 |
| [C2](tasks/phase2/batch1/C2-source-sheet.md) | 从依据卡片点开，直接看到当时那句话（前端） | Opus 5.5 |
| [D](tasks/phase2/batch1/D-date-fixtures.md) | 旧测试里写死的日期（从 main 修） | 6.1 Sol |
| [T](tasks/phase2/batch1/T-acceptance.md) | 独立验收 | 6.1 Sol（另一个执行者） |

第 1 批的验收报告：[最终一轮（38 条序列全部通过）](evaluations/2026-10-02-phase2-batch1-acceptance.md) · [首次交付时的基线记录](evaluations/2026-10-01-phase2-batch1-acceptance.md)

第 2–4 批（并行开发，2026-10-02 起）。入口：[并行方案与数据约定](tasks/phase2/parallel.md)

| 批 | 契约 | 任务包 |
|---|---|---|
| 共用 | [并行方案与数据约定](tasks/phase2/parallel.md) | [S0 骨架](tasks/phase2/S0-skeleton.md) |
| 第 2 批：结构化抽取 | [契约](tasks/phase2/batch2/README.md) | [E1 抽取](tasks/phase2/batch2/E1-extraction.md) · [E2 队列与补做](tasks/phase2/batch2/E2-backfill.md) · [M 记忆库](tasks/phase2/batch2/M-memory-store.md) · [U2 前端](tasks/phase2/batch2/U2-frontend.md) · [E3 秘书对话上文](tasks/phase2/batch2/E3-conversation-context.md) · [T2 验收](tasks/phase2/batch2/T2-acceptance.md) · [验收报告](evaluations/2026-10-02-phase2-batch2-acceptance.md) |
| 第 3 批：查询规划和时间轴 | [契约](tasks/phase2/batch3/README.md) | [Q1 拆条件](tasks/phase2/batch3/Q1-planner.md) · [Q2 按条件找](tasks/phase2/batch3/Q2-recall.md) · [K 时间轴](tasks/phase2/batch3/K-timeline.md) · [U3 前端](tasks/phase2/batch3/U3-frontend.md) · [T3 验收](tasks/phase2/batch3/T3-acceptance.md) |
| 第 4 批：评测、花费、导入 | [契约](tasks/phase2/batch4/README.md) | [I 导入](tasks/phase2/batch4/I-import.md) · [L 花费记录](tasks/phase2/batch4/L-usage.md) · [U4 前端](tasks/phase2/batch4/U4-frontend.md) · [V 评测](tasks/phase2/batch4/V-eval.md) · [T4 验收](tasks/phase2/batch4/T4-acceptance.md) · [E4 按整段对话整理](tasks/phase2/batch4/E4-conversation-extraction.md) |

第 2–4 批的验收和评测报告：[第 2 批](evaluations/2026-10-02-phase2-batch2-acceptance.md) · [第 2 批补充：秘书原话带对话上文](evaluations/2026-10-03-phase2-batch2-conversation-context-acceptance.md) · [第 3 批](evaluations/2026-10-02-phase2-batch3-acceptance.md) · [第 4 批](evaluations/2026-10-03-phase2-batch4-acceptance.md)（[准备阶段的记录](evaluations/2026-10-02-phase2-batch4-acceptance.md)） · [4b：按整段对话整理](evaluations/2026-10-03-phase2-batch4b-acceptance.md) · [回忆评测](evaluations/2026-10-02-phase2-eval.md)

2.0 的代码和研究稿不在 `docs/` 里，归档为 git 标签 `archive/phase2-0/*`，位置和用法见任务入口第 6 节。

### 第 2.5 阶段

| 文档 | 内容 |
|---|---|
| [任务入口](tasks/phase2_5/README.md) | 要交付什么、用户定下的事、分几批、约束 |
| [第 1 批契约：分组和类型](tasks/phase2_5/batch1/README.md) | 数据约定、规则、操作序列、文件归属 |
| [O1 整理后端](tasks/phase2_5/batch1/O1-organize-backend.md) · [T1 独立验收](tasks/phase2_5/batch1/T1-acceptance.md) · [U1 界面](tasks/phase2_5/batch1/U1-frontend.md) | 第 1 批的任务包 |
| [V2 办事评测集](tasks/phase2_5/V2-eval-set.md) | 一百个以上的办事任务、固定的跑法和打分、真实数据上的人工抽查 |
| [V2 办事题集与基线](evaluations/2026-10-04-phase2_5-v2-doing.md) | 671 条虚构记忆、120 题、真实通道三次结果/波动、私有抽查流程 |

### 研究

| 文档 | 内容 |
|---|---|
| [研究笔记：Hermes Agent](research/hermes-agent.md) | 外部方案调研 |

### 记忆整理与查找（2026-10-04 的讨论和实验）

方向已写入白皮书 5.6–5.8 和记忆架构 1.2。下面是依据和细节；研究笔记里的各办法仍是候选技术。建议按这个顺序读：

| 顺序 | 文档 | 内容 |
|---|---|---|
| 1 | [讨论稿：记忆下一步怎么做](research/memory-next-direction.md) | 总览，方向已确认。四层记忆（新增现状层）、全自动整理的原则、结构化层要补什么、办事时的三档、实验否掉的办法、没解决的问题、建议的顺序 |
| 2 | [研究笔记：现状卡、期限表与自查](research/memory-status-cards-and-self-check.md) | 目前最看好的做法：每个分组一张放原样记忆的现状卡、一张期限表、办完后对照记忆自查一次 |
| 3 | [研究笔记：看目录、分头细读](research/memory-navigate-and-read.md) | 质量最高但最慢的做法：模型先看分组目录，再分组细读挑出要用的记忆 |
| 4 | [研究笔记：用途标注与事先备料](research/memory-use-phrases-and-packets.md) | 两种没有显出优势的做法，以及原因 |
| 5 | [评测记录：第一轮](evaluations/2026-10-04-memory-lookup-experiment.md) | 十一种做法、12 个任务的设计和数字；丢分在办事那一步的发现 |
| 6 | [评测记录：第二轮](evaluations/2026-10-04-memory-lookup-round2.md) | 固定办事方式、调强基础线后重测二十多种组合；丢分归因 |
